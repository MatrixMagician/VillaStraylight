package main

// update_apply_test.go asserts the things a user actually reads after an apply: the
// three refusals, and the halt summary.
//
// THE THREE REFUSALS MUST NOT READ ALIKE. They are three different situations with
// three different remedies, and a user who cannot tell them apart will do the wrong
// thing — restart a stack that is already up, or go hunting for a broken image that
// is probably fine.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/prune"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updatecheck"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// renderResult runs the narration over a result and returns the text and code.
func renderResult(res updateflow.Result) (string, int) {
	var out, errOut bytes.Buffer
	code := printApplyResult(&out, &errOut, res)
	return out.String() + errOut.String(), code
}

// TestTheThreeRefusalsDoNotReadAlike is the assertion this ticket turns on.
//
// It compares them pairwise rather than checking each in isolation, because "each
// contains its own keyword" would pass even if all three shared a paragraph that
// dominated the reading.
func TestTheThreeRefusalsDoNotReadAlike(t *testing.T) {
	var stopped bytes.Buffer
	printStoppedStackRefusal(&stopped)

	unhealthy, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Memory,
			Outcome:   updateflow.RefusedUnhealthy,
			Proof:     updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory: retrieval returned no citation"},
		}},
	})

	reject, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Inference,
			Outcome:   updateflow.RolledBackReject,
			Proof:     updateflow.Proof{Status: updateflow.ProofReject, Detail: "residency markers not found"},
		}},
	})

	// Each says the one thing only it can say.
	cases := []struct {
		name, text, want string
	}{
		{"stopped stack", stopped.String(), "the stack is not running"},
		{"already unhealthy", unhealthy, "was not healthy before the update was attempted"},
		{"proof reject", reject, "Villa cannot show that it is"},
	}
	for _, tc := range cases {
		if !strings.Contains(tc.text, tc.want) {
			t.Errorf("the %s refusal does not say %q:\n%s", tc.name, tc.want, tc.text)
		}
	}

	// And no two of them share their distinguishing sentence.
	for i, a := range cases {
		for j, b := range cases {
			if i == j {
				continue
			}
			if strings.Contains(b.text, a.want) {
				t.Errorf("the %s refusal contains the %s refusal's distinguishing sentence %q; the two read alike",
					b.name, a.name, a.want)
			}
		}
	}
}

// TestTheStoppedStackRefusalExplainsTheAsymmetry: being told "villa up first" by a
// command is only useful if the user learns WHY, especially when the sibling verb
// they just ran worked fine on the same stopped stack.
func TestTheStoppedStackRefusalExplainsTheAsymmetry(t *testing.T) {
	var b bytes.Buffer
	printStoppedStackRefusal(&b)
	got := b.String()

	for _, want := range []string{"before and after", "cannot prove a subsystem that is not running", "villa up", "--check    works on a stopped stack"} {
		if !strings.Contains(got, want) {
			t.Errorf("the stopped-stack refusal is missing %q:\n%s", want, got)
		}
	}
}

// TestAnAlreadyUnhealthySubsystemPointsAtDoctorAndDoesNotDiagnose.
//
// `update` refuses; `doctor` diagnoses. Keeping that boundary is what stops the
// update verb accreting a second diagnostic surface that drifts from the first.
func TestAnAlreadyUnhealthySubsystemPointsAtDoctorAndDoesNotDiagnose(t *testing.T) {
	got, code := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Memory,
			Outcome:   updateflow.RefusedUnhealthy,
			Proof:     updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory: retrieval returned no citation"},
		}},
	})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(got, "villa doctor") {
		t.Errorf("the refusal does not point at doctor:\n%s", got)
	}
	if !strings.Contains(got, "an update failure it did not cause") {
		t.Errorf("the refusal does not say villa is declining to claim a failure it did not cause:\n%s", got)
	}
	if !strings.Contains(got, "Nothing has been changed") {
		t.Errorf("the refusal does not say nothing changed:\n%s", got)
	}
}

// TestARejectSaysTheImageMayBeFine is the wording ADR-0001's predicted failure
// requires. A user must not conclude upstream shipped a broken image when what
// happened is that villa could not tell.
func TestARejectSaysTheImageMayBeFine(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Inference,
			Outcome:   updateflow.RolledBackReject,
			Proof:     updateflow.Proof{Status: updateflow.ProofReject, Detail: "residency markers not found in the startup log"},
		}},
	})

	if !strings.Contains(got, "This may be perfectly fine") {
		t.Errorf("the Reject does not say the new state may be fine:\n%s", got)
	}
	if !strings.Contains(got, "will\n    not commit a pin on evidence it does not have") {
		t.Errorf("the Reject does not explain why villa declines to commit:\n%s", got)
	}
	if strings.Contains(got, "is broken") {
		t.Errorf("the Reject asserts brokenness, which villa did not observe:\n%s", got)
	}
}

// TestAFailIsConfidentWhereARejectIsCareful: the proof RAN and the property did not
// hold, so the wording is allowed to be certain. If a Fail read as carefully as a
// Reject, the distinction would exist in the types and nowhere a user can see it.
func TestAFailIsConfidentWhereARejectIsCareful(t *testing.T) {
	fail, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Memory,
			Outcome:   updateflow.RolledBackFail,
			Proof:     updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory: upload succeeded, retrieval returned no citation"},
		}},
	})

	if !strings.Contains(fail, "FAIL") {
		t.Errorf("a confident failure is not labelled FAIL:\n%s", fail)
	}
	if strings.Contains(fail, "may be perfectly fine") {
		t.Errorf("a confident failure carries the Reject's hedging:\n%s", fail)
	}
}

// TestMarkerDriftPointsAtTheIssueTracker.
//
// Unusual for this CLI and deliberate: a vetted pin that cannot be proven is a
// RELEASE-BLOCKING DEFECT the operator cannot fix locally. Every other refusal in
// villa hands the user something to do; this one genuinely cannot.
func TestMarkerDriftPointsAtTheIssueTracker(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Inference,
			Outcome:   updateflow.RolledBackReject,
			Proof:     updateflow.Proof{Status: updateflow.ProofReject, Detail: "residency markers not found"},
		}},
	})
	if !strings.Contains(got, "issues/new") {
		t.Errorf("a marker-drift Reject does not point at the issue tracker:\n%s", got)
	}
	if !strings.Contains(got, "rather than something you can fix locally") {
		t.Errorf("the pointer does not explain why the operator cannot fix it:\n%s", got)
	}

	// A non-inference Reject does NOT get the pointer: marker drift is specific to
	// the residency proof, and telling a user to file a bug for an unreachable
	// Qdrant would be noise.
	other, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Memory,
			Outcome:   updateflow.RolledBackReject,
			Proof:     updateflow.Proof{Status: updateflow.ProofReject, Detail: "the proof timed out"},
		}},
	})
	if strings.Contains(other, "issues/new") {
		t.Errorf("a non-inference Reject points at the issue tracker:\n%s", other)
	}
}

// TestTheHaltSummaryReportsAllThreeStatesWithReassurance is the most important
// output in the verb.
//
// "What state am I in?" is the question a user asks immediately after a halt, and
// exit 1 on a run that committed two subsystems could otherwise read as "everything
// is broken". It is not, and the output has to say so.
func TestTheHaltSummaryReportsAllThreeStatesWithReassurance(t *testing.T) {
	got, code := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Inference, Outcome: updateflow.Committed},
			{Subsystem: subsystem.Chat, Outcome: updateflow.Committed},
			{Subsystem: subsystem.Memory, Outcome: updateflow.RolledBackFail,
				Proof: updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory failed"}},
			{Subsystem: subsystem.Agent, Outcome: updateflow.NotTried},
		},
	})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	for _, want := range []string{"COMMITTED", "ROLLED BACK", "NOT TRIED"} {
		if !strings.Contains(got, want) {
			t.Errorf("the halt summary is missing the %s state:\n%s", want, got)
		}
	}
	// The reassurance is the part that stops exit 1 reading as total failure.
	if !strings.Contains(got, "were each PROVEN before commit and are") {
		t.Errorf("the halt summary does not reassure about the committed subsystems:\n%s", got)
	}
	if !strings.Contains(got, "will be skipped as already current") {
		t.Errorf("the halt summary does not say a re-run will skip the committed subsystems:\n%s", got)
	}
	// Every subsystem the run touched or skipped appears by name.
	for _, name := range []string{"inference", "chat", "memory", "coding agent"} {
		if !strings.Contains(got, name) {
			t.Errorf("the halt summary omits %s:\n%s", name, got)
		}
	}
}

// TestAHaltWithNothingCommittedSaysTheStackIsUntouched: the opposite reassurance,
// and the one a user most wants after a scary-looking run.
func TestAHaltWithNothingCommittedSaysTheStackIsUntouched(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Inference, Outcome: updateflow.RolledBackReject,
				Proof: updateflow.Proof{Status: updateflow.ProofReject, Detail: "markers not found"}},
			{Subsystem: subsystem.Chat, Outcome: updateflow.NotTried},
		},
	})
	if !strings.Contains(got, "running exactly what it was before this command") {
		t.Errorf("a halt with nothing committed does not say the stack is untouched:\n%s", got)
	}
	if strings.Contains(got, "PROVEN before commit") {
		t.Errorf("a halt with nothing committed carries the committed-subsystem reassurance:\n%s", got)
	}
}

// TestAnIncompleteRollbackIsNeverReportedAsClean is ADR-0003's honesty requirement
// at the surface. This is the worst state in the whole flow and it must be
// unmissable.
func TestAnIncompleteRollbackIsNeverReportedAsClean(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem:          subsystem.Memory,
			Outcome:            updateflow.RolledBackFail,
			Proof:              updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory failed"},
			RollbackIncomplete: true,
			Err:                errRollbackForTest,
		}},
	})

	if !strings.Contains(got, "INCOMPLETE") {
		t.Errorf("an incomplete rollback is not labelled:\n%s", got)
	}
	if !strings.Contains(got, "MAY NOT BE RUNNING") {
		t.Errorf("an incomplete rollback does not warn that the subsystem may be down:\n%s", got)
	}
	if strings.Contains(got, "re-proving restored state ........................ pass") {
		t.Errorf("an incomplete rollback claims a passing re-proof:\n%s", got)
	}
	if !strings.Contains(got, "ROLLBACK INCOMPLETE") {
		t.Errorf("the halt summary does not distinguish an incomplete rollback:\n%s", got)
	}
}

// TestACleanRollbackReportsItsReProof: "rolled back" is a demonstrated claim, not an
// assumption, and the output is where the demonstration becomes visible.
func TestACleanRollbackReportsItsReProof(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Memory,
			Outcome:   updateflow.RolledBackFail,
			Proof:     updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory failed"},
		}},
	})
	if !strings.Contains(got, "re-proving restored state") {
		t.Errorf("a clean rollback does not report its re-proof:\n%s", got)
	}
}

// TestASuccessfulRunExitsZeroAndSaysWhatItDid.
func TestASuccessfulRunExitsZeroAndSaysWhatItDid(t *testing.T) {
	got, code := renderResult(updateflow.Result{
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Inference, Outcome: updateflow.Committed},
			{Subsystem: subsystem.Chat, Outcome: updateflow.Committed},
			{Subsystem: subsystem.Memory, Outcome: updateflow.NothingToDo},
		},
	})
	if code != exitPass {
		t.Errorf("exit = %d, want %d", code, exitPass)
	}
	if !strings.Contains(got, "Updated 2 subsystem(s)") {
		t.Errorf("a successful run does not report what it did:\n%s", got)
	}
}

// TestACommitFailureWarnsWithoutReadingAsAFailedUpdate: the stack is running proven
// pins and villa could not write its own record. That is a warning about
// bookkeeping, not a failed update, and the wording has to keep them apart.
func TestACommitFailureWarnsWithoutReadingAsAFailedUpdate(t *testing.T) {
	got, code := renderResult(updateflow.Result{
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem:  subsystem.Memory,
			Outcome:    updateflow.Committed,
			Err:        errRollbackForTest,
			FailedStep: "commit",
		}},
	})
	if code != exitPass {
		t.Errorf("exit = %d; a proven, running update must not exit as a failure over bookkeeping", code)
	}
	if !strings.Contains(got, "The stack is fine") {
		t.Errorf("the commit warning does not reassure that the stack is fine:\n%s", got)
	}
}

// TestSubsystemsMoveAsTheirProofUnit: the proof unit is the verify verb's scope, so
// memory's units and services are Qdrant AND the embedder. Splitting them would
// produce a pairing with no proof and no meaning.
func TestSubsystemsMoveAsTheirProofUnit(t *testing.T) {
	units, services := subsystemUnits(subsystem.Memory)
	if len(units) != 2 || len(services) != 2 {
		t.Errorf("memory moves %d units / %d services, want both halves of the pairing", len(units), len(services))
	}
	units, services = subsystemUnits(subsystem.WebSearch)
	if len(units) != 2 || len(services) != 2 {
		t.Errorf("web search moves %d units / %d services, want SearXNG and the web guard together", len(units), len(services))
	}
	// The agent is a binary, not a unit: nothing to render and nothing to restart.
	units, services = subsystemUnits(subsystem.Agent)
	if len(units) != 0 || len(services) != 0 {
		t.Errorf("the agent subsystem renders units (%v/%v); the Crush binary is a file", units, services)
	}
}

// TestBudgetsArePerSubsystemAndNonZero: a total cap would make failures depend on
// ordering, so the last subsystem gets blamed for time the first four spent.
func TestBudgetsArePerSubsystemAndNonZero(t *testing.T) {
	seen := map[string]bool{}
	for _, k := range subsystem.Every {
		if k == subsystem.CodingMode {
			continue
		}
		b := perSubsystemBudget(k)
		if b <= 0 {
			t.Errorf("%v has a non-positive budget", k)
		}
		seen[b.String()] = true
	}
	// Inference gets the longest, because the residency proof is the expensive part
	// and it runs twice.
	if perSubsystemBudget(subsystem.Inference) <= perSubsystemBudget(subsystem.Chat) {
		t.Error("inference does not get a longer budget than chat, despite running the residency proof twice")
	}
}

// TestTheDryRunForecastsTheReferenceCountedPrune.
//
// Reference counting is a SAFETY property, not tidiness: the embedder and the vulkan
// backend are the same digest today, so a per-component prune could delete an image
// a RUNNING backend depends on. Saying "still referenced" before it happens is what
// stops the no-op reading as a bug.
func TestTheDryRunForecastsTheReferenceCountedPrune(t *testing.T) {
	var b bytes.Buffer
	printUpdateDryRun(&b, []updateflow.Target{{
		Subsystem: subsystem.Memory,
		Pins:      map[string]string{"embedder": "example.invalid/embed@sha256:new"},
	}}, nil)
	got := b.String()

	if !strings.Contains(got, "still referenced by another component") {
		t.Errorf("the dry run does not forecast the shared-digest retention:\n%s", got)
	}
	if !strings.Contains(got, "the proof runs twice") {
		t.Errorf("the dry run hides the doubled proof cost:\n%s", got)
	}
	if !strings.Contains(got, "Nothing has been changed") {
		t.Errorf("the dry run does not say it changed nothing:\n%s", got)
	}
}

// TestUpdateDoesNotDiagnose: `update` refuses and `doctor` diagnoses. A second
// diagnostic surface here would drift from the first, so the boundary is enforced
// rather than remembered.
func TestUpdateDoesNotDiagnose(t *testing.T) {
	for _, name := range []string{"update.go", "update_apply.go", "update_proofs.go"} {
		src := readCmdSource(t, name)
		for _, forbidden := range []string{"detect.Probe()", "preflight.Run("} {
			if strings.Contains(src, forbidden) {
				t.Errorf("%s references %q; update refuses and doctor diagnoses, and a second diagnostic surface would drift from the first",
					name, forbidden)
			}
		}
	}
}

// errRollbackForTest is a fixed error for the narration fixtures.
var errRollbackForTest = errTestRollback{}

type errTestRollback struct{}

func (errTestRollback) Error() string { return "could not write the prior unit" }

// applyHarness drives apply() with an already-CHECKED report.
//
// It bypasses the fetch deliberately. This build carries no verification key, so a
// real fetch always produces a Reject and every apply assertion below would pass
// vacuously — which is exactly what happened when this test first went through the
// full command path. Driving apply() directly is what makes these tests real.
func applyHarness(t *testing.T, report updatecheck.Report, d updateDeps, args []string, flags updateFlags) (string, int) {
	t.Helper()
	var out, errOut bytes.Buffer
	cmd := &cobra.Command{}
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	cmd.SetContext(context.Background())

	selected, err := selectedSubsystems(args)
	if err != nil {
		t.Fatalf("select: %v", err)
	}
	code := apply(cmd, d, report, selected, flags)
	return out.String() + errOut.String(), code
}

// updatableReport is a checked report with one subsystem to move.
func updatableReport() updatecheck.Report {
	return updatecheck.Report{
		Result: updatecheck.ResultChecked,
		Subsystems: []updatecheck.Subsystem{{
			Name:  "memory",
			State: updatecheck.StateUpdateAvailable,
			Components: []updatecheck.Component{
				{Name: "qdrant", Effective: "old", Available: "example.invalid/qdrant@sha256:new"},
			},
		}},
	}
}

// TestAStoppedStackRefusesBeforeAnythingIsAttempted is REFUSAL #1, and it fires
// before the state machine runs at all: nothing proven, nothing pulled, nothing
// captured.
//
// Confirmed to fail against a build with the guard removed.
func TestAStoppedStackRefusesBeforeAnythingIsAttempted(t *testing.T) {
	var touched bool
	d := updateDeps{
		StackRunning: func() bool { return false },
		FlowDeps: func(context.Context) updateflow.Deps {
			return updateflow.Deps{
				ProveCurrent: func(context.Context, subsystem.Kind) updateflow.Proof {
					touched = true
					return updateflow.Proof{Status: updateflow.ProofPass}
				},
			}
		},
		ReferencedRefs: func() map[string]bool { return nil },
	}

	got, code := applyHarness(t, updatableReport(), d, nil, updateFlags{})

	if touched {
		t.Error("the state machine ran against a stopped stack")
	}
	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(got, "the stack is not running") {
		t.Errorf("the refusal does not name the stopped stack:\n%s", got)
	}
}

// TestApplyRunsTheStateMachineOnARunningStack is the positive control for the test
// above: with the stack up, the flow actually runs. Without this, a guard that
// refused unconditionally would pass every other assertion here.
//
// The report is memory's, which owns persistent state, so the fake deps wire the
// stopped window too. A fake that omitted it would exercise the refusal path rather
// than the happy one, which is the core failing closed exactly as designed.
func TestApplyRunsTheStateMachineOnARunningStack(t *testing.T) {
	var proven []subsystem.Kind
	d := updateDeps{
		StackRunning: func() bool { return true },
		FlowDeps: func(context.Context) updateflow.Deps {
			return updateflow.Deps{
				ProveCurrent: func(_ context.Context, k subsystem.Kind) updateflow.Proof {
					proven = append(proven, k)
					return updateflow.Proof{Status: updateflow.ProofPass}
				},
				CaptureState: func(subsystem.Kind) (updateflow.Capture, error) { return updateflow.Capture{}, nil },
				Mutate:       func(context.Context, subsystem.Kind, map[string]string) error { return nil },
				Stop:         func(context.Context, subsystem.Kind) error { return nil },
				SnapshotData: func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
					return pinstate.DataSnapshot{Volume: "villa-qdrant", Path: "/snap/memory.tar", Bytes: 2_800_000_000}, nil
				},
				Start: func(context.Context, subsystem.Kind) error { return nil },
				ProveNew: func(context.Context, subsystem.Kind) updateflow.Proof {
					return updateflow.Proof{Status: updateflow.ProofPass}
				},
				Commit: func(subsystem.Kind, map[string]string, pinstate.Previous) error { return nil },
			}
		},
		ReferencedRefs: func() map[string]bool { return nil },
	}

	got, code := applyHarness(t, updatableReport(), d, nil, updateFlags{})

	if len(proven) != 1 || proven[0] != subsystem.Memory {
		t.Errorf("the flow proved %v, want exactly memory", proven)
	}
	if code != exitPass {
		t.Errorf("exit = %d, want %d:\n%s", code, exitPass, got)
	}
	if !strings.Contains(got, "Updated 1 subsystem(s)") {
		t.Errorf("a successful apply does not report what it did:\n%s", got)
	}
	// The snapshot's disk cost is stated, because it is real disk the user just
	// spent and a safety property nobody can see is one nobody can check.
	if !strings.Contains(got, "2.8 GB") {
		t.Errorf("the snapshot's disk cost is not narrated:\n%s", got)
	}
}

// TestDryRunChangesNothingEvenOnARunningStack: --dry-run is the one apply-shaped
// invocation that must never mutate, so it short-circuits BEFORE the state machine
// rather than relying on the machine to behave.
func TestDryRunChangesNothingEvenOnARunningStack(t *testing.T) {
	var touched bool
	d := updateDeps{
		StackRunning: func() bool { return true },
		FlowDeps: func(context.Context) updateflow.Deps {
			return updateflow.Deps{
				ProveCurrent: func(context.Context, subsystem.Kind) updateflow.Proof {
					touched = true
					return updateflow.Proof{Status: updateflow.ProofPass}
				},
			}
		},
		ReferencedRefs: func() map[string]bool { return nil },
	}

	got, code := applyHarness(t, updatableReport(), d, nil, updateFlags{dryRun: true})

	if touched {
		t.Error("--dry-run ran the state machine")
	}
	if code != exitPass {
		t.Errorf("exit = %d, want %d for a dry run", code, exitPass)
	}
	if !strings.Contains(got, "Nothing has been changed") {
		t.Errorf("--dry-run did not say it changed nothing:\n%s", got)
	}
	if !strings.Contains(got, "Would update 1 subsystem(s)") {
		t.Errorf("--dry-run does not show the ordered plan:\n%s", got)
	}
}

// TestSelectingASubsystemNarrowsTheWork: `villa update memory` must not touch
// inference, so a user can move one thing at a time.
func TestSelectingASubsystemNarrowsTheWork(t *testing.T) {
	report := updatecheck.Report{
		Result: updatecheck.ResultChecked,
		Subsystems: []updatecheck.Subsystem{
			{Name: "inference", State: updatecheck.StateUpdateAvailable,
				Components: []updatecheck.Component{{Name: "backend-vulkan-radv", Available: "new-backend"}}},
			{Name: "memory", State: updatecheck.StateUpdateAvailable,
				Components: []updatecheck.Component{{Name: "qdrant", Available: "new-qdrant"}}},
		},
	}

	all := targetsFor(report, nil)
	if len(all) != 2 {
		t.Fatalf("no selection produced %d targets, want both", len(all))
	}
	// Order is the apply sequence, derived from the subsystem enumeration, so
	// --check and apply cannot disagree about what happens when.
	if all[0].Subsystem != subsystem.Inference || all[1].Subsystem != subsystem.Memory {
		t.Errorf("targets are out of apply order: %v, %v", all[0].Subsystem, all[1].Subsystem)
	}

	only := targetsFor(report, map[subsystem.Kind]bool{subsystem.Memory: true})
	if len(only) != 1 || only[0].Subsystem != subsystem.Memory {
		t.Errorf("selecting memory produced %v", only)
	}
}

// TestACurrentSubsystemIsNeverATarget: an already-current subsystem must not be
// proven, restarted or re-pulled. Proving it would spend the expensive residency
// budget to learn nothing.
func TestACurrentSubsystemIsNeverATarget(t *testing.T) {
	report := updatecheck.Report{
		Result: updatecheck.ResultChecked,
		Subsystems: []updatecheck.Subsystem{
			{Name: "memory", State: updatecheck.StateCurrent,
				Components: []updatecheck.Component{{Name: "qdrant"}}},
			{Name: "web search", State: updatecheck.StateSkipped, SkipReason: "web_search_enabled = false"},
		},
	}
	if got := targetsFor(report, nil); len(got) != 0 {
		t.Errorf("a current and a skipped subsystem produced targets: %v", got)
	}
}

// ---------------------------------------------------------------------------
// Prune
// ---------------------------------------------------------------------------

// TestOnlyCommittedSubsystemsArePruned is the guard around the only image-deleting
// code in this project.
//
// A ROLLED-BACK subsystem's "previous" is the image it was just RESTORED to, so
// removing it would delete what the stack is now running. An untried one has no
// previous at all. Only a committed subsystem has genuinely moved off an image.
func TestOnlyCommittedSubsystemsArePruned(t *testing.T) {
	got := prunable(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Inference, Outcome: updateflow.Committed,
				Previous: map[string]string{"backend-vulkan-radv": "old-backend"}},
			{Subsystem: subsystem.Memory, Outcome: updateflow.RolledBackFail,
				// The image memory was just restored TO. Pruning it would delete
				// what the stack is running right now.
				Previous: map[string]string{"qdrant": "old-qdrant"},
				Proof:    updateflow.Proof{Status: updateflow.ProofFail, Detail: "verify memory failed"}},
			{Subsystem: subsystem.Chat, Outcome: updateflow.RolledBackReject,
				Previous: map[string]string{"open-webui": "old-owui"}},
			{Subsystem: subsystem.Agent, Outcome: updateflow.NotTried},
			{Subsystem: subsystem.WebSearch, Outcome: updateflow.RefusedUnhealthy},
		},
	})

	if len(got) != 1 || got[0].Subsystem != subsystem.Inference {
		names := []subsystem.Kind{}
		for _, s := range got {
			names = append(names, s.Subsystem)
		}
		t.Errorf("prune would consider %v, want only the committed subsystem — a rolled-back subsystem's "+
			"previous is the image it was just restored to", names)
	}
}

// TestACommittedSubsystemWithNoPreviousIsNotPruned: nothing was superseded, so
// there is nothing to consider and no plan to print.
func TestACommittedSubsystemWithNoPreviousIsNotPruned(t *testing.T) {
	got := prunable(updateflow.Result{
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Memory, Outcome: updateflow.Committed},
		},
	})
	if len(got) != 0 {
		t.Errorf("a committed subsystem with no recorded previous was offered to prune: %v", got)
	}
}

// TestAFailedRemovalIsAWarnNotAFailedUpdate.
//
// Prune runs AFTER the proof passed and the pin committed, so the update has
// already succeeded. A cleanup failure leaves MORE safety, not less: the image that
// could not be removed is exactly the inert leftover ADR-0003 says harms nothing.
// The wording must not read as a failed update.
func TestAFailedRemovalIsAWarnNotAFailedUpdate(t *testing.T) {
	var b bytes.Buffer
	// Drive the narration with a plan whose removal will fail, by pointing the
	// removal at a reference podman cannot possibly hold.
	printPrunePlan(context.Background(), &b, subsystem.Memory, prune.Plan{
		Decisions: []prune.Decision{{
			Ref:    "example.invalid/never-pulled@sha256:0000",
			Action: prune.Remove,
			Reason: "no current pin and no retained previous references it",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "WARN") {
		t.Errorf("a failed removal is not a WARN:\n%s", got)
	}
	if !strings.Contains(got, "The update itself succeeded") {
		t.Errorf("a failed removal does not say the update succeeded:\n%s", got)
	}
	if strings.Contains(got, "FAIL") || strings.Contains(got, "rolling back") {
		t.Errorf("a failed removal reads as a failed update:\n%s", got)
	}
}

// TestARetentionIsReportedWithItsReason: prune sometimes no-ops right after a
// successful update because the digest is still referenced elsewhere. Silent, that
// looks like a bug — the user sees the old image on disk with no way to know that
// keeping it was correct.
func TestARetentionIsReportedWithItsReason(t *testing.T) {
	var b bytes.Buffer
	printPrunePlan(context.Background(), &b, subsystem.Memory, prune.Plan{
		Decisions: []prune.Decision{{
			Ref:    "docker.io/example/toolboxes@sha256:shared",
			Action: prune.Retain,
			Reason: "still referenced by the current pin for backend-vulkan-radv",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "retained") {
		t.Errorf("the retention is not labelled:\n%s", got)
	}
	if !strings.Contains(got, "still referenced by") {
		t.Errorf("the retention does not say why:\n%s", got)
	}
}

// TestABlockedPlanSkipsRatherThanRemoving: an unreadable store means villa cannot
// tell what is referenced, and the narration must say it skipped rather than imply
// there was nothing to do.
func TestABlockedPlanSkipsRatherThanRemoving(t *testing.T) {
	var b bytes.Buffer
	printPrunePlan(context.Background(), &b, subsystem.Memory, prune.Plan{
		Blocked:       true,
		BlockedReason: "villa could not read its record of which images are in use",
	})
	got := b.String()

	if !strings.Contains(got, "skipped") {
		t.Errorf("a blocked plan is not reported as skipped:\n%s", got)
	}
	if !strings.Contains(got, "could not read") {
		t.Errorf("a blocked plan does not say why:\n%s", got)
	}
}

// TestAMissingPreviousIsSurfacedAsIncompleteProtection: someone running `podman
// image prune` by hand loses rollback protection, and villa proceeding as though it
// still had a safety net would be claiming a guarantee it cannot honour.
func TestAMissingPreviousIsSurfacedAsIncompleteProtection(t *testing.T) {
	var b bytes.Buffer
	printPrunePlan(context.Background(), &b, subsystem.Chat, prune.Plan{
		Decisions: []prune.Decision{{
			Ref:    "ghcr.io/example/owui@sha256:gone",
			Action: prune.MissingPrevious,
			Reason: "recorded as the known-good previous for chat, but it is no longer in the image store — something removed it outside villa",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "rollback protection for chat is incomplete") {
		t.Errorf("a missing previous is not surfaced as incomplete protection:\n%s", got)
	}
	if !strings.Contains(got, "outside villa") {
		t.Errorf("the warning does not say something removed it outside villa:\n%s", got)
	}
}

// TestTheOnlyImageDeletionIsGuarded: `podman image rm` must appear exactly once in
// the tree, must not carry --force, and must not be reachable from any other verb.
//
// --force would remove an image a container is still using, which is the exact
// failure reference counting exists to prevent. If podman says the image is in use,
// that is a signal villa's own record was wrong, and the right response is to leave
// it alone and warn.
func TestTheOnlyImageDeletionIsGuarded(t *testing.T) {
	found := 0
	for _, name := range cmdGoFiles(t) {
		src := readCmdSource(t, name)
		if !strings.Contains(src, `"image", "rm"`) {
			continue
		}
		found++
		if name != "update_apply.go" {
			t.Errorf("%s deletes a container image; the only deletion in this project belongs to the update prune step", name)
		}
		if strings.Contains(src, `"--force"`) || strings.Contains(src, "--force") {
			t.Errorf("%s passes --force to an image removal; that would delete an image a container is still using, "+
				"which is exactly what reference counting exists to prevent", name)
		}
	}
	if found != 1 {
		t.Errorf("found %d image-removal sites, want exactly one", found)
	}

	// And `villa uninstall` still removes no images — ADR-0004 licenses removal for
	// update only.
	if strings.Contains(readCmdSource(t, "uninstall.go"), `"image", "rm"`) {
		t.Error("uninstall removes container images; ADR-0004 licenses removal for `villa update` only")
	}
}

// TestPruneOutputFollowsTheRunsStream: one run is one report, and it must not be
// split across two streams.
//
// A halted run narrates to stderr. Sending prune's lines to stdout regardless would
// mean `villa update > log` captured the retention notes while the failure they
// belong to went to the terminal — the reader gets half a story in each place.
//
// Found on hardware: the call site passed `out` unconditionally.
func TestPruneOutputFollowsTheRunsStream(t *testing.T) {
	run := func(halted bool) (stdout, stderr string) {
		var outBuf, errBuf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetContext(context.Background())

		outcome := updateflow.Committed
		proof := updateflow.Proof{Status: updateflow.ProofPass}
		if halted {
			outcome = updateflow.RolledBackFail
			proof = updateflow.Proof{Status: updateflow.ProofFail, Detail: "probe"}
		}

		d := updateDeps{
			StackRunning: func() bool { return true },
			FlowDeps: func(context.Context) updateflow.Deps {
				return updateflow.Deps{
					ProveCurrent: func(context.Context, subsystem.Kind) updateflow.Proof {
						return updateflow.Proof{Status: updateflow.ProofPass}
					},
					CaptureState: func(subsystem.Kind) (updateflow.Capture, error) {
						return updateflow.Capture{Refs: map[string]string{"qdrant": "old"}}, nil
					},
					Mutate: func(context.Context, subsystem.Kind, map[string]string) error { return nil },
					// The report is memory's, which owns persistent state, so the
					// stopped window is wired: without it the core refuses and this
					// test would exercise a refusal rather than the stream split.
					Stop: func(context.Context, subsystem.Kind) error { return nil },
					SnapshotData: func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
						return pinstate.DataSnapshot{Volume: "villa-qdrant", Path: "/snap/memory.tar", Bytes: 1}, nil
					},
					Start:    func(context.Context, subsystem.Kind) error { return nil },
					ProveNew: func(context.Context, subsystem.Kind) updateflow.Proof { return proof },
					Restore:  func(context.Context, subsystem.Kind, updateflow.Capture) error { return nil },
					ProveRestored: func(context.Context, subsystem.Kind) updateflow.Proof {
						return updateflow.Proof{Status: updateflow.ProofPass}
					},
					Commit: func(subsystem.Kind, map[string]string, pinstate.Previous) error { return nil },
				}
			},
			ReferencedRefs: func() map[string]bool { return nil },
			Prune: func(_ context.Context, w io.Writer, _ updateflow.Result) {
				fmt.Fprint(w, "PRUNE-MARKER\n")
			},
		}
		_ = outcome
		apply(cmd, d, updatableReport(), nil, updateFlags{})
		return outBuf.String(), errBuf.String()
	}

	// A successful run: everything on stdout.
	stdout, stderr := run(false)
	if !strings.Contains(stdout, "PRUNE-MARKER") {
		t.Errorf("on a successful run prune did not write to stdout:\nstdout=%q\nstderr=%q", stdout, stderr)
	}

	// A halted run: prune follows the narration to stderr.
	stdout, stderr = run(true)
	if strings.Contains(stdout, "PRUNE-MARKER") {
		t.Errorf("on a HALTED run prune wrote to stdout while the failure went to stderr; "+
			"one run is one report and must not be split across two streams:\nstdout=%q", stdout)
	}
	if !strings.Contains(stderr, "PRUNE-MARKER") {
		t.Errorf("on a halted run prune did not follow the narration to stderr:\nstderr=%q", stderr)
	}
}

// TestAnIncompleteRollbackNeverClaimsTheStackIsUntouched is a regression test for
// the most dangerous line villa printed on hardware.
//
// A real Open WebUI update migrated its SQLite schema forward. The old image could
// not read the migrated database, so restoring the old pin restored a container
// that crash-looped. Villa correctly reported ROLLBACK INCOMPLETE — and then
// printed "Your stack is running exactly what it was before this command", which
// was false, and was the most reassuring sentence on the screen.
//
// A rollback that could not be proven means villa does NOT know what is running.
// Claiming otherwise is precisely the false-green ADR-0003 exists to forbid.
func TestAnIncompleteRollbackNeverClaimsTheStackIsUntouched(t *testing.T) {
	got, code := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem:          subsystem.Chat,
			Outcome:            updateflow.RolledBackReject,
			Proof:              updateflow.Proof{Status: updateflow.ProofReject, Detail: "health probe did not answer"},
			RollbackIncomplete: true,
			Err:                errRollbackForTest,
		}},
	})

	if code != exitBlocked {
		t.Errorf("exit = %d, want %d", code, exitBlocked)
	}
	if strings.Contains(got, "running exactly what it was before this command") {
		t.Errorf("an INCOMPLETE rollback claimed the stack is untouched — villa cannot know that, "+
			"and this was the most reassuring line on the screen during a real outage:\n%s", got)
	}
	if !strings.Contains(got, "villa cannot tell you what state this") {
		t.Errorf("the summary does not admit villa cannot tell what state the stack is in:\n%s", got)
	}
	if !strings.Contains(got, "villa doctor") {
		t.Errorf("the summary does not point at doctor:\n%s", got)
	}
	// The data-migration hazard is named, because "restore the old pin" is not
	// sufficient advice when the new version migrated the data forward.
	if !strings.Contains(got, "migrate their data forward") {
		t.Errorf("the summary does not warn that a forward data migration can make a pin rollback insufficient:\n%s", got)
	}
}

// TestACleanRollbackStillReassures: the honest reassurance must survive. A rollback
// that COMPLETED and was re-proven genuinely does leave the stack as it was, and
// saying so is what stops exit 1 reading as a disaster.
func TestACleanRollbackStillReassures(t *testing.T) {
	got, _ := renderResult(updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem: subsystem.Chat,
			Outcome:   updateflow.RolledBackFail,
			Proof:     updateflow.Proof{Status: updateflow.ProofFail, Detail: "probe failed"},
		}},
	})
	if !strings.Contains(got, "running exactly what it was before this command") {
		t.Errorf("a CLEAN rollback lost its reassurance; exit 1 would read as a disaster:\n%s", got)
	}
}
