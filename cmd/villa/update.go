package main

// update.go is the cobra surface for `villa update --check`: the read-only report.
//
// The apply path is not here yet. `villa update` without --check refuses with a
// pointer at --check rather than doing nothing silently, so the verb never
// half-exists.
//
// EXIT CODES reuse doctor's vocabulary rather than inventing one: 0 current, 2
// updates available, 1 could-not-check, 130 interrupted. The 2-means-available
// choice is deliberate — it makes `if villa update --check; then` read as "you are
// current", which is the scriptable question. Note the asymmetry with apply, where
// 1 will mean "villa answered and it went wrong": for --check, 1 means villa could
// not answer at all.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifestverify"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updatecheck"
	"github.com/MatrixMagician/VillaStraylight/internal/updatefetch"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// updateDeps is the injectable host surface for the check path. Every field is a
// host effect, so the whole verb is driven from tests without a network.
type updateDeps struct {
	// LoadConfig reads the persisted config, which decides the installed footprint.
	LoadConfig func() (config.VillaConfig, error)
	// LoadPinState reads what this host runs and when it last checked.
	LoadPinState func() (pinstate.State, error)
	// SavePinState records CheckedAt after a successful check. It is the ONLY
	// mutation `--check` performs, and it is a mutation of villa's own record
	// rather than of the stack.
	SavePinState func(pinstate.State) error
	// Fetch is the single outbound request.
	Fetch func(ctx context.Context) (updatefetch.Fetched, error)
	// Now supplies the clock.
	Now func() time.Time
	// VillaVersion is this binary's version.
	VillaVersion func() string
	// StackRunning reports whether the inference service is up. It gates the apply
	// path only: --check works on a stopped stack because it changes nothing.
	StackRunning func() bool
	// FlowDeps builds the transactional core's seam set for this run.
	FlowDeps func(ctx context.Context) updateflow.Deps
	// ReferencedRefs is the set of pins nothing may remove, used by --dry-run to
	// forecast the reference-counted prune outcome before it happens.
	ReferencedRefs func() map[string]bool
	// Prune releases superseded images after a committed update. It is a seam so a
	// test can assert that prune NEVER runs on a halted run, which is the property
	// that keeps the only image-deleting code in the project away from a stack in
	// an uncertain state.
	Prune func(ctx context.Context, w io.Writer, res updateflow.Result)
	// SnapshotCleanup releases superseded DATA snapshots after a committed update.
	// It is a separate seam from Prune for the same reason it is a separate
	// package: it is the only code in the project that deletes a data snapshot, and
	// a test must be able to assert it never runs on a rolled-back subsystem, whose
	// snapshot is the data the stack was just restored from.
	SnapshotCleanup func(w io.Writer, res updateflow.Result)
	// SnapshotSizes reports the current on-disk size of each stateful subsystem's
	// data volume, so --dry-run can state the disk cost BEFORE it is spent.
	// Discovering that a snapshot needed 2.8 GB afterwards is discovering it too
	// late.
	SnapshotSizes func() map[subsystem.Kind]int64
}

// liveUpdateDeps wires the real host.
func liveUpdateDeps() updateDeps {
	return updateDeps{
		LoadConfig:   config.LoadVilla,
		LoadPinState: func() (pinstate.State, error) { return pinstate.Load(livePinStateDeps()) },
		SavePinState: func(s pinstate.State) error { return pinstate.Save(livePinStateDeps(), s) },
		Fetch: func(ctx context.Context) (updatefetch.Fetched, error) {
			return updatefetch.Fetch(ctx, updatefetch.LiveDeps())
		},
		Now:          time.Now,
		VillaVersion: villaVersion,
		StackRunning: func() bool {
			active, err := orchestrate.NewSystemd().IsActive(installServiceName)
			return err == nil && active == "active"
		},
		FlowDeps:        liveUpdateFlowDeps,
		Prune:           runPrune,
		SnapshotCleanup: runSnapshotCleanup,
		SnapshotSizes:   liveSnapshotSizes,
		ReferencedRefs: func() map[string]bool {
			refs, _, err := pinstate.ReferencedRefs(livePinStateDeps())
			if err != nil {
				return nil
			}
			return refs
		},
	}
}

// newUpdate builds `villa update`.
func newUpdate() *cobra.Command {
	var check bool
	var dryRun bool
	var fromRegistries bool

	cmd := &cobra.Command{
		Use:   "update [subsystem...]",
		Short: "Update the pinned components this host runs, or check what is available",
		Long: "Compare the pins this host runs against the signed pin manifest villa publishes, and report.\n\n" +
			"--check is read-only and works on a STOPPED stack, because it changes nothing. That is a real\n" +
			"asymmetry with applying an update, which needs a running stack to prove each subsystem before\n" +
			"and after it changes it.\n\n" +
			"Checks happen ONLY when you run this command. Nothing polls, and no other verb checks\n" +
			"opportunistically — status is polled by the dashboard, so an opportunistic check there would\n" +
			"mean network access on a UI refresh loop. status and doctor show the LAST RECORDED check and\n" +
			"its age; they never trigger a live one.\n\n" +
			"Applying proves each subsystem BEFORE and after it changes it, and commits one subsystem\n" +
			"before starting the next, halting on the first failure — so a failure reverts only that\n" +
			"subsystem and the ones already committed stay committed.\n\n" +
			"chat and memory keep their state in a data volume, so for them the image is not the thing\n" +
			"being changed. They are STOPPED while their data is copied, then updated, then started — a\n" +
			"copy taken from under a running service is a torn one. That copy is part of the rollback\n" +
			"target, so a rollback restores the data as well as the pin. Villa will not update a\n" +
			"subsystem whose data it could not copy. Use --dry-run to see the disk this needs.\n\n" +
			"Arguments are subsystem names (inference, chat, memory, search, agent), never container names:\n" +
			"the proof unit is what `villa verify` proves, so memory moves as Qdrant plus the embedder.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runUpdate(cmd, args, liveUpdateDeps(), updateFlags{check: check, dryRun: dryRun, fromRegistries: fromRegistries}))
			return nil
		},
	}

	cmd.Flags().BoolVar(&dryRun, "dry-run", false,
		"show the ordered plan, the disk each data snapshot needs, and what would be pruned, changing nothing")
	cmd.Flags().BoolVar(&check, "check", false, "report available updates without changing anything (works on a stopped stack)")
	cmd.Flags().BoolVar(&fromRegistries, "from-registries", false,
		"ask each registry directly instead of reading the signed manifest. THIS REVEALS WHICH ADDONS YOU HAVE ENABLED: "+
			"villa would contact one endpoint per installed component, and the subset it asks about is a fingerprint of your stack. "+
			"The manifest check does not do this. Opt in only if you accept that cost.")

	return cmd
}

// updateFlags carries the parsed flags into the testable body.
type updateFlags struct {
	check          bool
	dryRun         bool
	fromRegistries bool
}

// runUpdate returns the exit code rather than calling os.Exit, so the tests drive
// every path deterministically.
func runUpdate(cmd *cobra.Command, args []string, d updateDeps, flags updateFlags) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	// Subsystem arguments are validated even on the check path, so a typo is caught
	// the same way whichever verb is being run. The error TEACHES the model rather
	// than merely rejecting: "qdrant" is a real thing a user knows about, and the
	// useful reply says where it lives.
	for _, arg := range args {
		if _, ok := subsystemByName(arg); !ok {
			printUnknownSubsystem(errOut, arg)
			return exitBlocked
		}
	}

	selected, serr := selectedSubsystems(args)
	if serr != nil {
		printUnknownSubsystem(errOut, args[0])
		return exitBlocked
	}

	if flags.fromRegistries {
		fmt.Fprint(errOut, "Refused: --from-registries is not implemented yet.\n\n"+
			"  It will contact one endpoint per installed component, which reveals to those\n"+
			"  registries which addons you have enabled. The manifest check does not.\n\n"+
			"  villa update --check    read the signed manifest instead\n")
		return exitBlocked
	}

	cfg, err := d.LoadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "update: could not read the config: %v\n", err)
		return exitBlocked
	}

	// An unreadable pin state is not a fault here: it is the fresh-install path, and
	// it resolves every component to its vetted pin.
	state, err := d.LoadPinState()
	if err != nil {
		state = pinstate.State{}
	}

	fetched, ferr := d.Fetch(ctx)
	if ctx.Err() != nil {
		return exitInterrupted
	}
	if ferr != nil {
		// A network failure is a could-not-check, reported through the same Reject
		// path as every other one — so it cannot read as "you are up to date"
		// either.
		report := updatecheck.Check(updatecheck.Input{
			Cfg:                 cfg,
			Resolver:            resolverFor(state),
			Verdict:             networkVerdict(ferr),
			LastSuccessfulCheck: state.CheckedAt,
			VillaVersion:        d.VillaVersion(),
		})
		return emit(out, errOut, report, jsonOut)
	}

	verdict := manifestverify.Verify(manifestverify.Input{
		Data:        fetched.Manifest,
		Signature:   fetched.Signature,
		PublicKey:   manifestverify.PublicKey(),
		SerialFloor: state.SerialFloor(),
		Now:         d.Now(),
	})

	report := updatecheck.Check(updatecheck.Input{
		Cfg:                 cfg,
		Resolver:            resolverFor(state),
		Verdict:             verdict,
		CheckedAt:           d.Now().UTC().Format(time.RFC3339),
		LastSuccessfulCheck: state.CheckedAt,
		VillaVersion:        d.VillaVersion(),
	})

	// Record the check ONLY when villa actually reached a conclusion. Recording a
	// timestamp for a check that failed would make `status` report freshness villa
	// does not have, which is the same lie as "you are up to date".
	if report.Result == updatecheck.ResultChecked {
		state.CheckedAt = report.CheckedAt
		if verdict.Doc.Serial > state.Serial {
			state.Serial = verdict.Doc.Serial
		}
		if serr := d.SavePinState(state); serr != nil {
			fmt.Fprintf(errOut, "warning: could not record this check (%v); status will keep showing the previous one\n", serr)
		}
	}

	if flags.check {
		return emit(out, errOut, report, jsonOut)
	}
	return apply(cmd, d, report, selected, flags)
}

// apply is the mutating half: it derives the work from the SAME report --check
// prints, so the two verbs can never disagree about what would be updated.
//
// It refuses on a stopped stack. `update` proves each subsystem before and after it
// changes it, and it cannot prove a subsystem that is not running. Starting services
// the operator deliberately stopped, proving against them, then stopping them again
// is a lot of unrequested state change — and if the restore-to-stopped step failed,
// update would have left the stack running when it found it down.
func apply(cmd *cobra.Command, d updateDeps, report updatecheck.Report, selected map[subsystem.Kind]bool, flags updateFlags) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()
	ctx := cmd.Context()
	if ctx == nil {
		ctx = context.Background()
	}

	if report.Result != updatecheck.ResultChecked {
		// Villa does not know what to apply. This takes the same Reject path as
		// --check, because "could not check" is the honest answer either way and
		// inventing a second wording for it would let one of them drift.
		printReject(errOut, report)
		return exitBlocked
	}

	targets := targetsFor(report, selected)
	if len(targets) == 0 {
		fmt.Fprint(out, "\nNothing to update. Every selected subsystem is at the pin the manifest offers.\n")
		return exitPass
	}

	if flags.dryRun {
		printUpdateDryRun(out, targets, d.ReferencedRefs(), snapshotSizes(d))
		return exitPass
	}

	// REFUSAL #1, before anything is attempted.
	if !d.StackRunning() {
		printStoppedStackRefusal(errOut)
		return exitBlocked
	}

	res := updateflow.Run(ctx, d.FlowDeps(ctx), targets)
	if ctx.Err() != nil {
		return exitInterrupted
	}

	code := printApplyResult(out, errOut, res)

	// Prune runs AFTER the proofs have passed and the pins are committed, and its
	// outcome never changes the exit code. That is the one place fail-soft is right
	// in this lifecycle: the update has already succeeded, and a failure to reclaim
	// disk leaves MORE safety, not less.
	//
	// Its output follows the RUN's stream, not stdout unconditionally. A halted run
	// narrates to stderr, so sending prune's lines to stdout would split one report
	// across two streams — and `villa update > log` would capture the retention
	// notes while the failure they belong to went elsewhere.
	if d.Prune != nil {
		pruneOut := out
		if res.Halted {
			pruneOut = errOut
		}
		d.Prune(ctx, pruneOut, res)
	}
	// The snapshot cleanup follows the same rules and the same stream: after the
	// proofs, never changing the exit code, and never on a subsystem that rolled
	// back — which the seam itself enforces, since that is where the live rollback
	// target lives.
	if d.SnapshotCleanup != nil {
		cleanupOut := out
		if res.Halted {
			cleanupOut = errOut
		}
		d.SnapshotCleanup(cleanupOut, res)
	}
	return code
}

// snapshotSizes reads the per-subsystem snapshot cost, tolerating an unwired seam.
//
// A nil map is the honest "villa could not measure it", which --dry-run renders as
// an unknown rather than as zero. Printing "0 B" for a volume villa could not stat
// would understate a cost the user is about to pay.
func snapshotSizes(d updateDeps) map[subsystem.Kind]int64 {
	if d.SnapshotSizes == nil {
		return nil
	}
	return d.SnapshotSizes()
}

// networkVerdict turns a transport failure into a could-not-check verdict.
//
// It is a Refused rather than an Absent because something went wrong that the user
// may be able to act on — being offline is a fact worth stating, unlike "no
// manifest has been published", which is not.
func networkVerdict(err error) manifestverify.Verdict {
	return manifestverify.Verdict{
		Outcome: manifestverify.Refused,
		Reason:  manifestverify.ReasonNotPublished,
		Message: fmt.Sprintf("Villa could not reach the release endpoint to check for updates: %v\n\n"+
			"This usually means this machine is offline or behind a proxy that blocks it.", err),
	}
}

// emit renders the report and returns the exit code.
func emit(out, errOut io.Writer, r updatecheck.Report, asJSON bool) int {
	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(r); err != nil {
			fmt.Fprintf(errOut, "update: encode json: %v\n", err)
			return exitBlocked
		}
		if r.Result == updatecheck.ResultCouldNotCheck {
			return exitBlocked
		}
		if r.Summary != nil && r.Summary.Updatable > 0 {
			return exitWarn
		}
		return exitPass
	}

	if r.Result == updatecheck.ResultCouldNotCheck {
		printReject(errOut, r)
		return exitBlocked
	}
	printReport(out, r)
	if r.Summary != nil && r.Summary.Updatable > 0 {
		return exitWarn
	}
	return exitPass
}

// printReject is the most important output in this verb.
//
// The phrase "This is not 'you are up to date'" names the misreading and refuses
// it, verbosely and on purpose: the cost of being misread here is a user believing
// they are current for months. Everything else in the block exists to make that
// sentence land — what villa is running instead, why nothing is wrong, and how
// stale the last real answer is.
func printReject(w io.Writer, r updatecheck.Report) {
	fmt.Fprint(w, "Could not check for updates.\n\n")
	for _, line := range strings.Split(strings.TrimRight(r.Message, "\n"), "\n") {
		fmt.Fprintf(w, "  %s\n", line)
	}
	fmt.Fprint(w, "\n  This is not \"you are up to date\" — villa could not determine anything.\n\n")
	fmt.Fprint(w, "Your stack is running the pins villa shipped with, which were vetted on\n"+
		"gfx1151 hardware. Nothing is wrong; nothing was checked.\n\n")
	fmt.Fprint(w, "  villa update --check --from-registries\n"+
		"      Ask each registry directly instead. This contacts one endpoint per\n"+
		"      installed component, which reveals to those registries which addons\n"+
		"      you have enabled. The manifest check does not.\n\n")

	if r.LastSuccessfulCheck == "" {
		// Never-checked is its OWN state, not "0 available" and not "checked a long
		// time ago". Same absent-is-not-zero discipline as the JSON.
		fmt.Fprint(w, "Villa has never completed an update check on this host.\n")
		return
	}
	fmt.Fprintf(w, "Last successful check: %s.\n", r.LastSuccessfulCheck)
}

// printReport renders the table.
//
// tabwriter rather than fixed-width verbs, because a reference's rendered width
// varies with its tag, and a fixed column silently raggeds the whole table the
// first time a long tag appears.
func printReport(w io.Writer, r updatecheck.Report) {
	fmt.Fprintf(w, "\nPin manifest   serial %d, valid until %s\n", r.Manifest.Serial, r.Manifest.ValidUntil)
	fmt.Fprintf(w, "Last checked   %s\n\n", r.CheckedAt)

	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprint(tw, "SUBSYSTEM\tCOMPONENT\tEFFECTIVE\tAVAILABLE\tSTATE\n")
	for _, s := range r.Subsystems {
		if s.State == updatecheck.StateSkipped {
			// The skipped row is PRESENT but empty. Omitting it would hide the
			// decision that this subsystem was deliberately not considered.
			fmt.Fprintf(tw, "%s\t—\t—\t—\tskipped: %s\n", s.Name, s.SkipReason)
			continue
		}
		for i, c := range s.Components {
			name := s.Name
			if i > 0 {
				name = "" // components after the first hang under the subsystem
			}
			available := "—"
			if c.Available != "" {
				available = shortRef(c.Available)
			}
			state := "current"
			switch c.Change {
			case updatecheck.ChangeRebuilt:
				state = "rebuilt"
			case updatecheck.ChangeNewVersion:
				state = "new version"
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t%s\n", name, c.Name, shortRef(c.Effective), available, state)
		}
	}
	_ = tw.Flush()

	fmt.Fprintf(w, "\n%s\n", r.Message)

	if r.Villa != nil && r.Villa.Available != "" {
		fmt.Fprintf(w, "\nA newer villa (%s) is available. This command does not apply it:\n"+
			"villa is updated by replacing the binary.\n", r.Villa.Available)
	}
}

// shortRef renders a reference for a table: the TAG plus eight digest characters.
//
// The tag, not the repository path. "amd-strix-halo-toolboxes:vulkan-radv" is forty
// characters of provenance nobody reads in a column, while "vulkan-radv" is the
// part that identifies which image this is. Full references live in --json, where a
// consumer needs them whole.
func shortRef(ref string) string {
	if ref == "" {
		return "—"
	}
	name, digest, found := strings.Cut(ref, "@sha256:")
	if !found {
		return ref
	}
	if colon := strings.LastIndex(name, ":"); colon >= 0 {
		name = name[colon+1:]
	} else if slash := strings.LastIndex(name, "/"); slash >= 0 {
		name = name[slash+1:]
	}
	if len(digest) > 8 {
		digest = digest[:8]
	}
	return name + "  " + digest
}

// subsystemByName resolves a CLI argument to a subsystem.
//
// It accepts the single-word forms a user types ("search", "agent") alongside the
// display names, because the display name for web search is two words and nobody
// types "villa update 'web search'".
func subsystemByName(name string) (subsystem.Kind, bool) {
	switch strings.ToLower(strings.TrimSpace(name)) {
	case "inference":
		return subsystem.Inference, true
	case "chat":
		return subsystem.Chat, true
	case "memory":
		return subsystem.Memory, true
	case "search", "web search", "websearch":
		return subsystem.WebSearch, true
	case "agent", "coding agent":
		return subsystem.Agent, true
	}
	return 0, false
}

// printUnknownSubsystem teaches the subsystem model rather than merely rejecting.
//
// "qdrant" is a real thing a user knows the name of, so the useful reply says where
// it lives and why it moves with its sibling — not just that the word was wrong.
func printUnknownSubsystem(w io.Writer, arg string) {
	fmt.Fprintf(w, "Unknown subsystem %q.\n\n", arg)

	// The container names a user is most likely to reach for, mapped to the
	// subsystem that owns them and the proof that keeps them together.
	partOf := map[string]struct{ subsystem, why string }{
		"qdrant":          {"memory", "verify memory proves Qdrant and the embedder together"},
		"embed":           {"memory", "verify memory proves Qdrant and the embedder together"},
		"embedder":        {"memory", "verify memory proves Qdrant and the embedder together"},
		"villa-qdrant":    {"memory", "verify memory proves Qdrant and the embedder together"},
		"villa-embed":     {"memory", "verify memory proves Qdrant and the embedder together"},
		"searxng":         {"search", "verify search proves SearXNG and the web guard together"},
		"websafe":         {"search", "verify search proves SearXNG and the web guard together"},
		"villa-searxng":   {"search", "verify search proves SearXNG and the web guard together"},
		"villa-websafe":   {"search", "verify search proves SearXNG and the web guard together"},
		"open-webui":      {"chat", "the chat subsystem is Open WebUI"},
		"openwebui":       {"chat", "the chat subsystem is Open WebUI"},
		"villa-openwebui": {"chat", "the chat subsystem is Open WebUI"},
		"llama":           {"inference", "the inference subsystem is llama-server on the active backend"},
		"villa-llama":     {"inference", "the inference subsystem is llama-server on the active backend"},
		"crush":           {"agent", "the agent subsystem is the Crush binary"},
	}

	if hint, ok := partOf[strings.ToLower(strings.TrimSpace(arg))]; ok {
		fmt.Fprintf(w, "  %s is part of the %s subsystem, which villa updates as a unit\n"+
			"  because %s.\n\n  villa update %s\n\n", arg, hint.subsystem, hint.why, hint.subsystem)
	}

	fmt.Fprint(w, "Subsystems: inference, chat, memory, search, agent\n\n"+
		"Arguments are subsystem names, never container names: the proof unit is what\n"+
		"`villa verify` proves, so components that are proven together move together.\n")
}
