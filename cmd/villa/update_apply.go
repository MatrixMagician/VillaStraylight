package main

// update_apply.go wires the transactional core to the real host, and renders the
// narration.
//
// The three refusals are the thing to get right here. They must not read alike,
// because they are three different situations with three different remedies:
//
//	stopped stack       "villa up first" — nothing was attempted
//	already unhealthy   "memory was not healthy before the update; nothing changed"
//	proof Reject        "this image may be fine, but villa cannot show it is"
//
// And `update` REFUSES; `doctor` DIAGNOSES. This file must not grow diagnostics: it
// points at doctor instead, which keeps one verb's job one job.

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/prune"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updatecheck"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// liveUpdateFlowDeps wires the state machine to the real host.
//
// The proofs are the ones each subsystem ALREADY has, reused verbatim rather than
// reimplemented: the residency proof plus a real generation probe for inference,
// the Open WebUI protocol probes for chat, and verify memory / search / agent for
// the addons. A second implementation of any of them would be a second opinion
// about whether the stack works.
func liveUpdateFlowDeps(context.Context) updateflow.Deps {
	sys := orchestrate.NewSystemd()

	return updateflow.Deps{
		ProveCurrent:  func(c context.Context, k subsystem.Kind) updateflow.Proof { return liveSubsystemProof(c, k) },
		ProveNew:      func(c context.Context, k subsystem.Kind) updateflow.Proof { return liveSubsystemProof(c, k) },
		ProveRestored: func(c context.Context, k subsystem.Kind) updateflow.Proof { return liveSubsystemProof(c, k) },

		CaptureState: func(k subsystem.Kind) (updateflow.Capture, error) {
			return liveCapture(k)
		},

		Pull: func(c context.Context, refs map[string]string) error {
			for _, ref := range refs {
				if err := livePullImage(c, ref); err != nil {
					return err
				}
			}
			return nil
		},

		Mutate: func(c context.Context, k subsystem.Kind, refs map[string]string) error {
			return liveMutate(c, sys, k, refs)
		},

		// The stopped window, wired only because the CORE decides when to use it:
		// these three run for a subsystem that owns persistent state and are never
		// reached for one whose image is the state being changed.
		Stop: func(c context.Context, k subsystem.Kind) error {
			return liveSubsystemStop(c, sys, k)
		},

		SnapshotData: func(c context.Context, k subsystem.Kind) (pinstate.DataSnapshot, error) {
			return liveSnapshotData(c, k)
		},

		Start: func(c context.Context, k subsystem.Kind) error {
			return liveSubsystemStart(c, sys, k)
		},

		Restore: func(c context.Context, k subsystem.Kind, snapshot updateflow.Capture) error {
			return liveRestoreSubsystem(c, sys, k, snapshot)
		},

		RestoreData: func(c context.Context, k subsystem.Kind, snap pinstate.DataSnapshot) error {
			return liveRestoreData(c, k, snap)
		},

		Commit: func(k subsystem.Kind, refs map[string]string, previous pinstate.Previous) error {
			return liveCommit(k, refs, previous)
		},

		// The budget derives from the RUN's context, which is the SIGINT-cancelled
		// one main installs, so a Ctrl-C still cuts through every subsystem's
		// deadline. The core defers the cancel, so a run that halts early releases
		// the remaining timers instead of leaking one per subsystem.
		Budget: func(c context.Context, k subsystem.Kind) (context.Context, context.CancelFunc) {
			return context.WithTimeout(c, k.UpdateBudget())
		},

		Now: func() string { return time.Now().UTC().Format(time.RFC3339) },
	}
}

// liveSubsystemProof runs the proof each subsystem already has.
//
// The Reject-versus-Fail mapping happens here, and it is the load-bearing part: a
// proof that could not be CONDUCTED is not evidence of anything, so it becomes a
// Reject and the caller says "villa cannot show this works". A proof that ran and
// failed becomes a Fail, and the caller is confident.
func liveSubsystemProof(ctx context.Context, k subsystem.Kind) updateflow.Proof {
	if ctx.Err() != nil {
		// The budget expired or the user interrupted. Either way villa OBSERVED
		// slowness, not brokenness — a Reject, never a Fail.
		return updateflow.Proof{
			Status: updateflow.ProofReject,
			Detail: fmt.Sprintf("the %s proof did not complete within its budget", k),
		}
	}
	fn := liveProofFuncs[k]
	if fn == nil {
		return updateflow.Proof{
			Status: updateflow.ProofReject,
			Detail: fmt.Sprintf("no proof is wired for %s", k),
		}
	}
	return fn(ctx)
}

// liveProofFuncs binds each subsystem to its existing proof. It is a var so a test
// can substitute the whole set without a live host.
var liveProofFuncs = map[subsystem.Kind]func(context.Context) updateflow.Proof{}

// liveCapture reads the rollback tuple: the verbatim prior unit bytes, the pins
// each component was running, and the config they were proven under.
//
// Verbatim, because a re-render is not a restore: it would reproduce today's
// template against today's config, which is not what was proven.
func liveCapture(k subsystem.Kind) (updateflow.Capture, error) {
	snapshot := updateflow.Capture{
		Refs:  map[string]string{},
		Units: map[string][]byte{},
	}

	state, err := pinstate.Load(livePinStateDeps())
	if err != nil {
		state = pinstate.State{}
	}
	r := resolverFor(state)
	for _, res := range r.For(k) {
		snapshot.Refs[string(res.Component)] = res.Current.Ref
	}
	// The snapshot the CURRENT retained tuple points at, read now because by the
	// time cleanup runs the store holds this update's tuple instead and the
	// displaced path would be unrecoverable.
	if prev, ok := state.PreviousFor(k); ok {
		snapshot.PriorSnapshot = prev.Data
	}

	dir, err := quadletUnitDir()
	if err != nil {
		return updateflow.Capture{}, err
	}
	units, _ := k.Units()
	for _, name := range units {
		data, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			if os.IsNotExist(err) {
				// A unit that is not on disk is not part of this host's footprint.
				// Skipping it is correct; failing would refuse an update to a
				// subsystem whose optional half was never installed.
				continue
			}
			return updateflow.Capture{}, fmt.Errorf("capture %s: %w", name, err)
		}
		snapshot.Units[name] = data
	}

	cfg, err := config.LoadVilla()
	if err != nil {
		return updateflow.Capture{}, fmt.Errorf("capture config: %w", err)
	}
	snapshot.Config = fmt.Sprintf("%+v", cfg)

	return snapshot, nil
}

// liveMutate records the new pins, re-renders, and restarts the subsystem's
// services.
//
// The pin is written to the store BEFORE the render, because the render reads it —
// that is the whole loop the resolver migration closed. On any later failure the
// rollback rewrites the store from the captured tuple.
func liveMutate(ctx context.Context, sys orchestrate.Systemd, k subsystem.Kind, refs map[string]string) error {
	// The agent's pin is a checksummed binary, not an image in a unit, so its
	// mutation is a file move rather than a render-and-restart. The superseded
	// binary is retained as a sibling BEFORE the new one lands, which is the
	// file-shaped version of capture-before-mutate.
	if k == subsystem.Agent {
		if err := retainCrushPrevious(); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("retain the previous Crush binary: %w", err)
		}
	}

	if err := writeEffectivePins(refs); err != nil {
		return fmt.Errorf("record the new pins: %w", err)
	}

	cfg, err := config.LoadVilla()
	if err != nil {
		return err
	}
	if err := renderAndWrite(cfg); err != nil {
		return err
	}
	if err := sys.DaemonReload(); err != nil {
		return err
	}

	_, services := k.Units()
	for _, svc := range services {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := sys.Restart(svc); err != nil {
			return fmt.Errorf("restart %s: %w", svc, err)
		}
	}
	return nil
}

// liveRestore puts the captured tuple back, verbatim, and restarts.
func liveRestoreSubsystem(ctx context.Context, sys orchestrate.Systemd, k subsystem.Kind, snapshot updateflow.Capture) error {
	if err := writeEffectivePins(snapshot.Refs); err != nil {
		return fmt.Errorf("restore the prior pins: %w", err)
	}

	dir, err := quadletUnitDir()
	if err != nil {
		return err
	}
	var changed []orchestrate.Unit
	for name, data := range snapshot.Units {
		changed = append(changed, orchestrate.Unit{Name: name, Text: string(data)})
	}
	if len(changed) > 0 {
		if err := orchestrate.WriteUnits(orchestrate.Plan{Changed: changed}, dir); err != nil {
			return err
		}
	}
	if err := sys.DaemonReload(); err != nil {
		return err
	}

	_, services := k.Units()
	for _, svc := range services {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if err := sys.Restart(svc); err != nil {
			return fmt.Errorf("restart %s: %w", svc, err)
		}
	}
	return nil
}

// writeEffectivePins records the pins a subsystem is running.
func writeEffectivePins(refs map[string]string) error {
	deps := livePinStateDeps()
	state, err := pinstate.Load(deps)
	if err != nil {
		state = pinstate.State{}
	}
	if state.Pins == nil {
		state.Pins = map[string]pinstate.Effective{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for id, ref := range refs {
		state.Pins[id] = pinstate.Effective{Ref: ref, UpdatedAt: now}
	}
	return pinstate.Save(deps, state)
}

// liveCommit records the retained previous alongside the already-written pins.
func liveCommit(k subsystem.Kind, refs map[string]string, previous pinstate.Previous) error {
	deps := livePinStateDeps()
	state, err := pinstate.Load(deps)
	if err != nil {
		state = pinstate.State{}
	}
	if state.Pins == nil {
		state.Pins = map[string]pinstate.Effective{}
	}
	if state.Previous == nil {
		state.Previous = map[string]pinstate.Previous{}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	for id, ref := range refs {
		state.Pins[id] = pinstate.Effective{Ref: ref, UpdatedAt: now}
	}
	// ONE previous per subsystem. The one it displaces becomes a prune candidate,
	// which is the next ticket's job — until then it simply stays on disk, which is
	// safe and is exactly ADR-0003's inert-leftover state.
	state.Previous[k.String()] = previous
	return pinstate.Save(deps, state)
}

// renderAndWrite re-renders every unit from the persisted config and writes the
// changed ones.
func renderAndWrite(cfg config.VillaConfig) error {
	dir, err := quadletUnitDir()
	if err != nil {
		return err
	}
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return err
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return err
	}
	resident, err := liveResidentUnits(cfg)
	if err != nil {
		return err
	}
	units, err := livePinnedRender(orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     modelFile,
		ModelsDir:     modelsDir(),
		HostVillaPath: hostVillaPath(),
		Resident:      resident,
	})
	if err != nil {
		return err
	}
	plan, err := orchestrate.Reconcile(units, dir)
	if err != nil {
		return err
	}
	if len(plan.Changed) == 0 {
		return nil
	}
	return orchestrate.WriteUnits(plan, dir)
}

// ---------------------------------------------------------------------------
// Narration
// ---------------------------------------------------------------------------

// printApplyResult renders the run and returns the exit code.
//
// The HALT SUMMARY is the most important output in the verb, because "what state am
// I in?" is the question a user asks immediately after a halt. Exit 1 on a run that
// committed two subsystems could otherwise read as "everything is broken", so the
// summary says explicitly that the committed ones were each proven and are running
// normally.
func printApplyResult(out, errOut io.Writer, res updateflow.Result) int {
	w := out
	if res.Halted {
		w = errOut
	}

	for _, s := range res.Subsystems {
		switch s.Outcome {
		case updateflow.NothingToDo, updateflow.NotTried:
			continue
		}
		fmt.Fprintf(w, "\n%s\n", s.Subsystem)
		printSubsystemNarration(w, s)
	}

	if !res.Halted {
		fmt.Fprintf(out, "\nUpdated %d subsystem(s).\n", res.CommittedCount())
		return exitPass
	}

	printHaltSummary(errOut, res)
	return exitBlocked
}

// printSubsystemNarration renders one subsystem's steps.
func printSubsystemNarration(w io.Writer, s updateflow.SubsystemResult) {
	switch s.Outcome {
	case updateflow.Committed:
		fmt.Fprint(w, "  proving current state ............................ pass\n")
		fmt.Fprint(w, "  capturing rollback point ......................... done\n")
		printSnapshotLine(w, s)
		fmt.Fprint(w, "  applying the new pins ............................ done\n")
		fmt.Fprint(w, "  proving new state ................................ pass\n")
		fmt.Fprint(w, "  committing effective pin ......................... done\n")
		if s.Err != nil {
			fmt.Fprintf(w, "\n  WARNING: the update is proven and running, but villa could not record it: %v\n"+
				"  The stack is fine. `villa update --check` may report this subsystem as updatable again.\n", s.Err)
		}

	case updateflow.RefusedUnhealthy:
		// REFUSAL #2: an already-unhealthy subsystem — or, for a stateful one, data
		// villa could not snapshot. Both leave the stack untouched, and both are
		// emphatically not update failures.
		if s.FailedStep == "snapshot" || s.FailedStep == "stop" {
			printSnapshotRefusal(w, s)
			return
		}
		fmt.Fprintf(w, "  proving current state ............................ REFUSED\n\n")
		fmt.Fprintf(w, "    %s was not healthy before the update was attempted.\n"+
			"    Nothing has been changed.\n\n", s.Subsystem)
		if s.Proof.Detail != "" {
			fmt.Fprintf(w, "    %s\n\n", s.Proof.Detail)
		}
		fmt.Fprint(w, "    Villa refuses rather than reporting an update failure it did not cause,\n"+
			"    and rather than rolling back to a state that was already broken.\n\n"+
			"    villa doctor      diagnose the current stack\n")

	case updateflow.RolledBackFail:
		// A FAIL: the proof RAN and the property did not hold. The wording is
		// confident, because villa observed something broken.
		fmt.Fprint(w, "  proving current state ............................ pass\n")
		fmt.Fprint(w, "  capturing rollback point ......................... done\n")
		printSnapshotLine(w, s)
		fmt.Fprint(w, "  applying the new pins ............................ done\n")
		fmt.Fprint(w, "  proving new state ................................ FAIL\n\n")
		if s.Proof.Detail != "" {
			fmt.Fprintf(w, "    %s\n\n", s.Proof.Detail)
		}
		printRollbackLines(w, s)

	case updateflow.RolledBackReject:
		// REFUSAL #3: the proof could not be conducted. THIS IMAGE MAY BE PERFECTLY
		// FINE. Villa must not tell a user that upstream shipped a broken image when
		// what actually happened is that villa could not tell.
		fmt.Fprint(w, "  proving current state ............................ pass\n")
		fmt.Fprint(w, "  capturing rollback point ......................... done\n")
		printSnapshotLine(w, s)
		fmt.Fprint(w, "  applying the new pins ............................ done\n")
		fmt.Fprint(w, "  proving new state ................................ REJECT\n\n")
		fmt.Fprint(w, "    The new state could not be proven.\n\n")
		if s.Proof.Detail != "" {
			fmt.Fprintf(w, "    %s\n\n", s.Proof.Detail)
		}
		fmt.Fprint(w, "    This may be perfectly fine. Villa cannot show that it is, and will\n"+
			"    not commit a pin on evidence it does not have.\n\n")
		printRollbackLines(w, s)
		printMarkerDriftPointer(w, s)
	}
}

// printSnapshotLine reports the data snapshot for a subsystem that owns persistent
// state, and prints NOTHING for one that does not.
//
// The size is stated because it is a real disk cost the user just paid — memory's
// measured snapshot is 2.8 GB — and because a snapshot nobody can see is a safety
// property nobody can check.
func printSnapshotLine(w io.Writer, s updateflow.SubsystemResult) {
	if !s.Snapshot.Taken() {
		return
	}
	fmt.Fprintf(w, "  snapshotting %s data (service stopped) ......... %s\n",
		s.Subsystem, humanBytes(s.Snapshot.Bytes))
}

// printSnapshotRefusal is the refusal a stateful subsystem gets when villa could
// not take its data snapshot.
//
// It reads differently from the already-unhealthy refusal on purpose: nothing was
// wrong with the subsystem, and nothing is wrong with the new image. Villa declined
// because it could not build a rollback target, and the remedy is disk or podman
// rather than `villa doctor`.
func printSnapshotRefusal(w io.Writer, s updateflow.SubsystemResult) {
	fmt.Fprint(w, "  proving current state ............................ pass\n")
	fmt.Fprint(w, "  capturing rollback point ......................... done\n")
	fmt.Fprintf(w, "  snapshotting %s data ............................ REFUSED\n\n", s.Subsystem)
	if s.Proof.Detail != "" {
		fmt.Fprintf(w, "    %s\n\n", s.Proof.Detail)
	}
	fmt.Fprintf(w, "    %s keeps its state in a data volume, so the image alone is not a\n"+
		"    rollback target: an update can migrate the data forward, and the old\n"+
		"    image can no longer read it. Villa will not mutate data it could not\n"+
		"    snapshot.\n\n", s.Subsystem)
	if s.RollbackIncomplete {
		// Villa stopped the services and could not start them again. That is worse
		// than the refusal it accompanies, so it is stated last and loudest.
		fmt.Fprintf(w, "    THIS SUBSYSTEM IS STOPPED. Villa stopped it to take the snapshot and\n"+
			"    could not start it again: %v\n\n"+
			"    villa up          start the stack\n"+
			"    villa doctor      diagnose it first if that fails\n\n", s.Err)
		return
	}
	fmt.Fprintf(w, "    Nothing has been changed and %s is running again.\n\n"+
		"    df -h ~           the snapshot needs room for the whole data volume\n"+
		"    villa doctor      diagnose the current stack\n", s.Subsystem)
}

// printRollbackLines renders the rollback and its re-proof.
//
// The re-proof is what makes "rolled back" a demonstrated claim rather than an
// assumption, and an incomplete rollback is stated plainly: putting the bytes back
// is not the same as showing the subsystem works (ADR-0003).
func printRollbackLines(w io.Writer, s updateflow.SubsystemResult) {
	if s.RollbackIncomplete {
		fmt.Fprint(w, "  rolling back ..................................... INCOMPLETE\n\n")
		fmt.Fprintf(w, "    Villa could not fully restore the previous state: %v\n\n"+
			"    THIS SUBSYSTEM MAY NOT BE RUNNING. Run `villa doctor` before anything else.\n\n", s.Err)
		return
	}
	if s.Snapshot.Taken() {
		// Said explicitly, because "rolled back" used to mean the pin alone. A user
		// whose schema was migrated forward needs to know the DATA went back too —
		// that is the difference between this rollback and the one that crash-looped.
		fmt.Fprintf(w, "  restoring %s data (service stopped) ............ %s\n",
			s.Subsystem, humanBytes(s.Snapshot.Bytes))
	}
	fmt.Fprint(w, "  rolling back ..................................... done\n")
	fmt.Fprint(w, "  re-proving restored state ........................ pass\n")
}

// printMarkerDriftPointer points at the issue tracker for a Reject.
//
// Unusual for this CLI, and deliberate: a vetted pin that cannot be proven is a
// RELEASE-BLOCKING DEFECT the operator cannot fix locally. Every other refusal in
// villa hands the user something to do; this one genuinely cannot, so the honest
// action is to report it.
func printMarkerDriftPointer(w io.Writer, s updateflow.SubsystemResult) {
	if s.Subsystem != subsystem.Inference {
		return
	}
	fmt.Fprint(w, "    If this is residency-marker drift, it is a defect in the shipped pin\n"+
		"    rather than something you can fix locally — the llama.cpp log format\n"+
		"    changes as the upstream image is rebuilt. Please report it:\n"+
		"    https://github.com/MatrixMagician/VillaStraylight/issues/new\n\n")
}

// printHaltSummary is the answer to "what state am I in?".
func printHaltSummary(w io.Writer, res updateflow.Result) {
	fmt.Fprint(w, "\nStopped.\n\n")

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	committed, incomplete := 0, 0
	for _, s := range res.Subsystems {
		if s.RollbackIncomplete {
			incomplete++
		}
		switch s.Outcome {
		case updateflow.Committed:
			committed++
			fmt.Fprintf(tw, "  COMMITTED\t%s\n", s.Subsystem)
		case updateflow.RolledBackFail, updateflow.RolledBackReject:
			label := "ROLLED BACK"
			if s.RollbackIncomplete {
				label = "ROLLBACK INCOMPLETE"
			}
			fmt.Fprintf(tw, "  %s\t%s\n", label, s.Subsystem)
		case updateflow.RefusedUnhealthy:
			fmt.Fprintf(tw, "  REFUSED\t%s\n", s.Subsystem)
		case updateflow.NotTried:
			fmt.Fprintf(tw, "  NOT TRIED\t%s\n", s.Subsystem)
		}
	}
	_ = tw.Flush()

	if committed > 0 {
		// Without this, exit 1 on a run that committed two subsystems reads as
		// "everything is broken". It is not, and the output says so explicitly.
		fmt.Fprintf(w, "\n  The %d committed subsystem(s) were each PROVEN before commit and are\n"+
			"  running normally. Re-run `villa update` after investigating; the committed\n"+
			"  subsystems will be skipped as already current.\n", committed)
	} else if incomplete > 0 {
		// NEVER claim the stack is untouched when a rollback did not complete.
		//
		// Found on hardware: an Open WebUI update migrated its SQLite schema
		// forward, the old image could not read the migrated database, and the
		// restore put the old digest back onto a database it could no longer
		// open. Villa correctly reported ROLLBACK INCOMPLETE — and then printed
		// "your stack is running exactly what it was before this command", which
		// was false and was the most reassuring line on the screen.
		//
		// A rollback that could not be proven means villa does NOT know what the
		// subsystem is running. Saying so is the whole point of ADR-0003.
		//
		// Villa now snapshots and restores the data for a subsystem that owns it,
		// so THIS path means the data restore itself did not complete. That is a
		// worse state than the incident's, not a better one: the volume holds
		// whatever a failed import left.
		fmt.Fprint(w, "\n  A ROLLBACK DID NOT COMPLETE, so villa cannot tell you what state this\n"+
			"  stack is in. Do not assume it is running what it was before.\n\n"+
			"  villa doctor      diagnose the current stack, before anything else\n\n"+
			"  A subsystem that keeps its state in a data volume is snapshotted before\n"+
			"  it is changed, and that snapshot is restored on rollback. When the restore\n"+
			"  itself does not complete, the data is whatever the failed restore left —\n"+
			"  the snapshot is still on disk and is the last known-good copy.\n")
	} else {
		fmt.Fprint(w, "\n  Your stack is running exactly what it was before this command.\n"+
			"  Nothing was committed.\n")
	}
}

// ---------------------------------------------------------------------------
// Refusals that happen before anything is attempted
// ---------------------------------------------------------------------------

// printStoppedStackRefusal explains why apply needs a running stack and check does
// not.
//
// The asymmetry is the whole message: `update` proves each subsystem before AND
// after it changes it, and it cannot prove a subsystem that is not running.
// Starting services the operator deliberately stopped, proving against them, then
// stopping them again is a lot of unrequested state change — and if the
// restore-to-stopped step failed, `update` would have left the stack running when
// it found it down.
func printStoppedStackRefusal(w io.Writer) {
	fmt.Fprint(w, "Refused: the stack is not running.\n\n"+
		"  villa update proves each subsystem before and after it changes it, and\n"+
		"  it cannot prove a subsystem that is not running.\n\n"+
		"  villa up                start the stack, then re-run\n"+
		"  villa update --check    works on a stopped stack — it changes nothing\n")
}

// targetsFor turns a check report into the ordered work list.
//
// It derives from the SAME report `--check` prints, so the two verbs can never
// disagree about what would be updated or in what order.
func targetsFor(r updatecheck.Report, selected map[subsystem.Kind]bool) []updateflow.Target {
	var out []updateflow.Target
	for _, k := range subsystem.Every {
		if len(selected) > 0 && !selected[k] {
			continue
		}
		var row *updatecheck.Subsystem
		for i := range r.Subsystems {
			if r.Subsystems[i].Name == k.String() {
				row = &r.Subsystems[i]
				break
			}
		}
		if row == nil || row.State != updatecheck.StateUpdateAvailable {
			continue
		}
		pinsFor := map[string]string{}
		for _, c := range row.Components {
			if c.Available != "" {
				pinsFor[c.Name] = c.Available
			}
		}
		if len(pinsFor) > 0 {
			out = append(out, updateflow.Target{Subsystem: k, Pins: pinsFor})
		}
	}
	return out
}

// printDryRun shows the ordered plan without changing anything.
func printUpdateDryRun(w io.Writer, targets []updateflow.Target, refs map[string]bool, sizes map[subsystem.Kind]int64) {
	if len(targets) == 0 {
		fmt.Fprint(w, "\nNothing to update. Every installed subsystem is at the pin the manifest offers.\n")
		return
	}

	fmt.Fprintf(w, "\nWould update %d subsystem(s), in this order:\n\n", len(targets))
	var snapshotTotal int64
	var measured bool
	for i, t := range targets {
		fmt.Fprintf(w, "  %d. %s\n", i+1, t.Subsystem)
		for id, ref := range t.Pins {
			fmt.Fprintf(w, "       pull    %s  %s\n", id, shortRef(ref))
		}
		// The stopped window and its disk cost, stated BEFORE it is spent. On a
		// small disk the snapshot size is a decision input, and discovering it
		// afterwards is discovering it too late.
		if t.Subsystem.OwnsPersistentState() {
			if n, ok := sizes[t.Subsystem]; ok {
				snapshotTotal += n
				measured = true
				fmt.Fprintf(w, "       stop    %s, so its data can be copied cleanly\n", t.Subsystem)
				fmt.Fprintf(w, "       snapshot the %s data volume  (about %s of disk)\n", t.Subsystem, humanBytes(n))
			} else {
				// OMITTED, not zero. Zero is a claim about a cost, and villa did
				// not measure one.
				fmt.Fprintf(w, "       stop    %s, so its data can be copied cleanly\n", t.Subsystem)
				fmt.Fprintf(w, "       snapshot the %s data volume  (size unknown — villa could not measure it)\n", t.Subsystem)
			}
		}
		fmt.Fprintf(w, "       prove   before and after (the proof runs twice)\n")
		fmt.Fprintf(w, "       retain  the current pins as the known-good previous\n")
		// The reference-counted prune outcome is shown BEFORE it happens, which
		// pre-empts "why is the old image still there?" — the shared-digest case is
		// otherwise baffling.
		for id := range t.Pins {
			fmt.Fprintf(w, "       prune   %s\n", pruneForecast(id, refs))
		}
	}
	if measured {
		// The total, because a per-subsystem figure does not answer "do I have room
		// for this run?" — which is the question the number exists to answer.
		fmt.Fprintf(w, "\nSnapshots would need about %s of disk while this run is in flight.\n"+
			"Each superseded snapshot is released once its update is proven and committed.\n", humanBytes(snapshotTotal))
	}
	fmt.Fprint(w, "\nNothing has been changed.\n")
}

// pruneForecast describes what prune would do to one component's superseded image.
//
// Reference counting is a SAFETY property, not tidiness: the embedder and the
// vulkan backend are the same digest today, so a per-component prune could delete an
// image a RUNNING backend depends on. Saying "still referenced" up front is what
// stops the no-op reading as a bug.
func pruneForecast(id string, referenced map[string]bool) string {
	entry, ok := pins.Lookup(pins.ComponentID(id))
	if !ok {
		return "nothing"
	}
	ref := entry.Vetted().Ref
	shared := 0
	for _, e := range pins.Table() {
		if e.Vetted().Ref == ref {
			shared++
		}
	}
	if shared > 1 {
		return "nothing — the superseded image is still referenced by another component"
	}
	if referenced[ref] {
		return "nothing — the superseded image is still referenced"
	}
	return "the superseded image becomes a removal candidate"
}

// selectedSubsystems turns CLI arguments into a selection set. An empty set means
// every updatable subsystem.
func selectedSubsystems(args []string) (map[subsystem.Kind]bool, error) {
	if len(args) == 0 {
		return nil, nil
	}
	out := map[subsystem.Kind]bool{}
	for _, arg := range args {
		k, ok := subsystemByName(arg)
		if !ok {
			return nil, fmt.Errorf("unknown subsystem %q", strings.TrimSpace(arg))
		}
		out[k] = true
	}
	return out, nil
}

// livePullImage fetches one digest-pinned image reference.
//
// It lives in the cmd tier because that is where podman invocations belong: the
// backend seam gate forbids them under internal/, and the cmd tier is the
// OS-orchestration layer that legitimately drives podman for lifecycle work
// (up/down/logs, volume rm, the probe containers).
//
// FIXED ARGS, never interpolated into a shell string. The reference is also bounded
// before it arrives: it came from a signed manifest whose registry host had to
// match the compiled-in allowlist, so this cannot be pointed at an arbitrary host.
//
// The FETCH is podman's outbound footprint, not villa's — gigabytes across several
// hosts, against villa's own single bodiless GET for the check. The two are
// reported as separate claims because one blended figure would be true of villa's
// process and false of the operation.
//
// exec.CommandContext, so a Ctrl-C during a multi-gigabyte pull kills the child
// rather than leaving the user watching it finish.
func livePullImage(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "podman", "pull", ref) // fixed args; no shell
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ctx.Err() != nil {
			return fmt.Errorf("pull %s: %w", ref, ctx.Err())
		}
		return fmt.Errorf("podman pull %s: %w: %s", ref, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// ---------------------------------------------------------------------------
// Prune: the only step in this project that deletes a container image
// ---------------------------------------------------------------------------

// runPrune releases superseded images after a committed update.
//
// A FAILED PRUNE IS A WARN, NEVER A ROLLBACK. This is the one step in the lifecycle
// where fail-soft is correct, and the reason is precise: prune runs AFTER the
// post-mutation proof has already passed, so the update has succeeded before prune
// is attempted. Rolling back a proven-good update because a cleanup step failed
// would be perverse — and the failure leaves MORE safety, not less. An image that
// could not be removed is exactly the inert leftover ADR-0003 says harms nothing.
//
// Everywhere else in this lifecycle the posture is the opposite: an unprovable
// component rolls back rather than committing on evidence villa does not have.
func runPrune(ctx context.Context, w io.Writer, res updateflow.Result) {
	state, err := pinstate.Load(livePinStateDeps())
	known := err == nil
	if err != nil {
		state = pinstate.State{}
	}
	// An EMPTY store is not a known reference set either — see ADR-0004 and
	// pinstate.ReferencedRefs. Reusing that reading here keeps the two packages
	// from drifting into different answers about the same document.
	if _, refsKnown, rerr := pinstate.ReferencedRefs(livePinStateDeps()); rerr != nil || !refsKnown {
		known = false
	}

	for _, s := range prunable(res) {
		// The agent's previous is a FILE, not an image: the Crush binary has no
		// digest and no image store, so it is retained as a sibling file by the
		// existing install seam rather than reference-counted here. Reference
		// counting over image references would have nothing to count.
		if s.Subsystem == subsystem.Agent {
			printAgentRetention(w, s)
			continue
		}
		plan := prune.Decide(prune.Input{
			State:      state,
			StateKnown: known,
			Superseded: s.Previous,
			Subsystem:  s.Subsystem,
			Present:    liveImagePresent,
		})
		printPrunePlan(ctx, w, s.Subsystem, plan)
	}
}

// prunable is the subsystems whose superseded images may be considered.
//
// ONLY the committed ones, and only those with a recorded previous. A rolled-back
// subsystem's "previous" is the image it was just RESTORED to — removing that would
// delete what the stack is now running. An untried one has no previous at all.
//
// It is a named function rather than a condition inside the loop so a test can
// assert the selection directly. A test that re-derived the rule would pass against
// a version that had the rule backwards.
func prunable(res updateflow.Result) []updateflow.SubsystemResult {
	var out []updateflow.SubsystemResult
	for _, s := range res.Subsystems {
		if s.Outcome.Committed() && len(s.Previous) > 0 {
			out = append(out, s)
		}
	}
	return out
}

// printPrunePlan acts on a plan and narrates it.
//
// EVERY outcome is printed, including the no-ops. A prune that silently does
// nothing right after a successful update looks like a bug — the user sees the old
// image still on disk and has no way to know that keeping it was the correct and
// deliberate answer.
func printPrunePlan(ctx context.Context, w io.Writer, k subsystem.Kind, plan prune.Plan) {
	if plan.Blocked {
		fmt.Fprintf(w, "\n  pruning previous (%s) ............................ skipped\n", k)
		fmt.Fprintf(w, "    %s\n", plan.BlockedReason)
		return
	}

	for _, d := range plan.Decisions {
		switch d.Action {
		case prune.Retain:
			fmt.Fprintf(w, "\n  pruning previous (%s) ............................ retained\n", k)
			fmt.Fprintf(w, "    %s — %s\n", shortRef(d.Ref), d.Reason)

		case prune.MissingPrevious:
			// Surfaced, not fail-softed: rollback protection is incomplete and the
			// user is the only one who can decide what to do about it.
			fmt.Fprintf(w, "\n  WARNING: rollback protection for %s is incomplete.\n", k)
			fmt.Fprintf(w, "    %s is %s\n", shortRef(d.Ref), d.Reason)

		case prune.Remove:
			if err := liveRemoveImage(ctx, d.Ref); err != nil {
				// A WARN. The update succeeded and is running; villa merely could
				// not reclaim the disk. It must not read as a failed update.
				fmt.Fprintf(w, "\n  pruning previous (%s) ............................ WARN\n", k)
				fmt.Fprintf(w, "    could not remove %s: %v\n"+
					"    The update itself succeeded and is running normally. The image is still\n"+
					"    on disk, which is harmless — it is simply disk villa could not reclaim.\n",
					shortRef(d.Ref), err)
				continue
			}
			fmt.Fprintf(w, "\n  pruning previous (%s) ............................ removed\n", k)
			fmt.Fprintf(w, "    %s — %s\n", shortRef(d.Ref), d.Reason)
		}
	}
}

// liveImagePresent reports whether an image reference is in the local store.
//
// A failure to ask is treated as PRESENT, not absent. Reporting "your rollback
// image is gone" because podman was momentarily unavailable would be a false alarm
// about a safety property, and a false alarm about safety is worse than silence:
// it teaches the user to ignore the real one.
func liveImagePresent(ref string) bool {
	cmd := exec.Command("podman", "image", "exists", ref) // fixed args; no shell
	if err := cmd.Run(); err != nil {
		var exitErr *exec.ExitError
		if errors.As(err, &exitErr) {
			// A clean non-zero exit is podman's confident "no".
			return false
		}
		// podman missing, or some other failure to ASK. Not evidence of absence.
		return true
	}
	return true
}

// liveRemoveImage deletes one image.
//
// THE ONLY IMAGE DELETION IN THIS PROJECT. It is reached only for a reference the
// pure core has decided nothing in the store references — never on a bare digest, a
// pattern, or anything villa did not itself record.
//
// Deliberately NOT --force. A force would remove an image a container is still
// using, which is the exact failure reference counting exists to prevent; if podman
// says the image is in use, that is a signal villa's own record was wrong, and the
// right response is to leave it alone and warn.
func liveRemoveImage(ctx context.Context, ref string) error {
	cmd := exec.CommandContext(ctx, "podman", "image", "rm", ref) // fixed args; no shell, no --force
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("%w: %s", err, strings.TrimSpace(string(out)))
	}
	return nil
}

// crushPreviousPath is where the superseded Crush binary is retained.
//
// A SIBLING FILE beside the live one, in villa's own bin directory: the agent's
// pin is a checksummed release asset rather than a container image, so there is no
// image store to reference-count and no digest to count references over. Keeping
// the prior binary next to the current one is the file-shaped version of the same
// one-previous rule.
func crushPreviousPath() string { return agentBinPath() + ".previous" }

// retainCrushPrevious moves the superseded Crush binary aside as the known-good
// previous.
//
// It is a RENAME, not a copy, so there is never a moment where the retention has
// doubled the disk cost, and never a torn half-copy if the process dies midway. A
// pre-existing .previous is replaced: one previous, exactly as for the images.
func retainCrushPrevious() error {
	live := agentBinPath()
	if _, err := os.Stat(live); err != nil {
		return err
	}
	prev := crushPreviousPath()
	_ = os.Remove(prev) // the one it displaces; absence is not an error
	return os.Rename(live, prev)
}

// printAgentRetention narrates the agent's file-shaped retention.
func printAgentRetention(w io.Writer, s updateflow.SubsystemResult) {
	if _, err := os.Stat(crushPreviousPath()); err == nil {
		fmt.Fprintf(w, "\n  retaining previous (%s) .......................... done\n", s.Subsystem)
		fmt.Fprintf(w, "    the superseded binary is kept beside the current one as the known-good previous\n")
		return
	}
	// SURFACED, not silent: the same honesty the image path applies to a missing
	// recorded previous. A user whose rollback binary is absent should know.
	fmt.Fprintf(w, "\n  WARNING: rollback protection for %s is incomplete.\n", s.Subsystem)
	fmt.Fprintf(w, "    No previous Crush binary was retained, so there is nothing to roll back to.\n")
}
