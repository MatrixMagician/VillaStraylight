package main

// update_cleanup_test.go covers the destructive half at the command tier: which
// subsystems are even considered for cleanup, and what a failed removal reads as.
//
// The selection rule is the one that matters. A rolled-back subsystem's snapshot is
// the data the stack was just RESTORED from, so removing it would delete the
// evidence the restore depended on. A test that re-derived that rule would pass
// against a version that had it backwards, so it is asserted against the named
// function directly.

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/snapshotprune"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// TestCleanupNeverRunsOnARolledBackOrUntriedSubsystem.
//
// This is where the live rollback target lives. A rolled-back subsystem was just
// restored FROM its snapshot, and an untried one never took one.
func TestCleanupNeverRunsOnARolledBackOrUntriedSubsystem(t *testing.T) {
	res := updateflow.Result{
		Halted: true,
		Subsystems: []updateflow.SubsystemResult{
			{Subsystem: subsystem.Inference, Outcome: updateflow.Committed},
			{Subsystem: subsystem.Chat, Outcome: updateflow.RolledBackFail},
			{Subsystem: subsystem.Memory, Outcome: updateflow.RolledBackReject},
			{Subsystem: subsystem.WebSearch, Outcome: updateflow.NotTried},
			{Subsystem: subsystem.Agent, Outcome: updateflow.RefusedUnhealthy},
		},
	}

	got := cleanable(res)
	if len(got) != 1 || got[0].Subsystem != subsystem.Inference {
		var names []string
		for _, s := range got {
			names = append(names, s.Subsystem.String())
		}
		t.Errorf("cleanable = %v, want only the committed subsystem", names)
	}
}

// TestCleanupSkipsASubsystemWhoseRollbackDidNotComplete.
//
// A commit that reported rollback-incomplete means villa does not know what the
// subsystem is running. Reclaiming disk from a stack in that state would remove the
// last thing a human could recover from.
func TestCleanupSkipsASubsystemWhoseRollbackDidNotComplete(t *testing.T) {
	res := updateflow.Result{
		Subsystems: []updateflow.SubsystemResult{{
			Subsystem:          subsystem.Chat,
			Outcome:            updateflow.Committed,
			RollbackIncomplete: true,
		}},
	}

	if got := cleanable(res); len(got) != 0 {
		t.Errorf("cleanable = %+v; a subsystem in an uncertain state was considered for deletion", got)
	}
}

// TestAFailedSnapshotRemovalIsAWarnNotAFailedUpdate.
//
// Identical reasoning to the image prune: cleanup runs after the proof has already
// passed, so the update has succeeded before cleanup is attempted. The failure
// leaves MORE safety, not less.
func TestAFailedSnapshotRemovalIsAWarnNotAFailedUpdate(t *testing.T) {
	dir := t.TempDir()
	// A directory where the plan expects a file: os.Remove fails on a non-empty
	// one, which is a real removal failure without needing a permissions dance.
	stuck := filepath.Join(dir, "villa-openwebui-old.tar")
	if err := os.MkdirAll(filepath.Join(stuck, "child"), 0o700); err != nil {
		t.Fatalf("stage a stuck path: %v", err)
	}

	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Chat, snapshotprune.Plan{
		Decisions: []snapshotprune.Decision{{
			Subsystem: subsystem.Chat,
			Path:      stuck,
			Bytes:     267_000_000,
			Action:    snapshotprune.Remove,
			Reason:    "it was displaced",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "WARN") {
		t.Errorf("a failed removal did not read as a warning:\n%s", got)
	}
	if !strings.Contains(got, "The update itself succeeded") {
		t.Errorf("the warning lets a failed cleanup read as a failed update:\n%s", got)
	}
	if strings.Contains(got, "ROLLED BACK") || strings.Contains(got, "rolling back") {
		t.Errorf("a failed cleanup rolled back a proven-good update:\n%s", got)
	}
}

// TestASuccessfulRemovalStatesWhatItReclaimed: disk that silently came back is
// disk the user cannot account for.
func TestASuccessfulRemovalStatesWhatItReclaimed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "villa-qdrant-old.tar")
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatalf("stage a snapshot: %v", err)
	}

	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Memory, snapshotprune.Plan{
		Decisions: []snapshotprune.Decision{{
			Subsystem: subsystem.Memory,
			Path:      path,
			Bytes:     2_800_000_000,
			Action:    snapshotprune.Remove,
			Reason:    "it was displaced by the snapshot taken for this update",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "2.8 GB") {
		t.Errorf("the removal does not state what it reclaimed:\n%s", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("the snapshot was narrated as removed but is still on disk: %v", err)
	}
}

// TestARetainedSnapshotIsReportedRatherThanSilent.
//
// A no-op right after a successful update looks like a bug: the user sees the old
// snapshot still on disk and has no way to know that keeping it was correct.
func TestARetainedSnapshotIsReportedRatherThanSilent(t *testing.T) {
	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Memory, snapshotprune.Plan{
		Decisions: []snapshotprune.Decision{{
			Subsystem: subsystem.Memory,
			Path:      "/snap/villa-qdrant-live.tar",
			Action:    snapshotprune.Retain,
			Reason:    "it is the rollback target the retained previous for memory points at",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "retained") {
		t.Errorf("a retained snapshot was not reported:\n%s", got)
	}
	if !strings.Contains(got, "rollback target") {
		t.Errorf("the retention gives no reason:\n%s", got)
	}
}

// TestAMissingSnapshotIsSurfacedAsIncompleteProtection.
//
// Someone clearing disk by hand loses data rollback protection and should be told,
// in the same words the image path already uses for a missing previous.
func TestAMissingSnapshotIsSurfacedAsIncompleteProtection(t *testing.T) {
	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Chat, snapshotprune.Plan{
		Decisions: []snapshotprune.Decision{{
			Subsystem: subsystem.Chat,
			Path:      "/snap/villa-openwebui.tar",
			Action:    snapshotprune.Missing,
			Reason:    "no longer on disk",
		}},
	})
	got := b.String()

	if !strings.Contains(got, "WARNING") {
		t.Errorf("a missing snapshot was not surfaced:\n%s", got)
	}
	if !strings.Contains(got, "incomplete") {
		t.Errorf("the warning does not say rollback protection is incomplete:\n%s", got)
	}
}

// TestABlockedPlanRemovesNothingAndSaysWhy: an unreadable store is not an empty
// one, and the output must not read as "there was nothing to clean up".
func TestABlockedPlanRemovesNothingAndSaysWhy(t *testing.T) {
	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Chat, snapshotprune.Plan{
		Blocked:       true,
		BlockedReason: "villa could not read its record",
	})
	got := b.String()

	if !strings.Contains(got, "skipped") {
		t.Errorf("a blocked cleanup did not say it was skipped:\n%s", got)
	}
	if !strings.Contains(got, "could not read") {
		t.Errorf("the skip gives no reason:\n%s", got)
	}
}

// TestAVanishedSnapshotIsNotAWarnOnRemoval: something else already removed it, and
// the goal — that file gone — has been met. Warning would be noise.
func TestAVanishedSnapshotIsNotAWarnOnRemoval(t *testing.T) {
	var b bytes.Buffer
	printSnapshotCleanupPlan(&b, subsystem.Chat, snapshotprune.Plan{
		Decisions: []snapshotprune.Decision{{
			Subsystem: subsystem.Chat,
			Path:      filepath.Join(t.TempDir(), "already-gone.tar"),
			Action:    snapshotprune.Remove,
			Reason:    "it was displaced",
		}},
	})

	if strings.Contains(b.String(), "WARN") {
		t.Errorf("an already-absent snapshot produced a warning:\n%s", b.String())
	}
}

// TestSnapshotPresenceFailsTowardsPresent.
//
// Reporting "your rollback snapshot is gone" because a stat momentarily failed
// would be a false alarm about a safety property, and a false alarm about safety is
// worse than silence: it teaches the user to ignore the real one.
func TestSnapshotPresenceFailsTowardsPresent(t *testing.T) {
	dir := t.TempDir()

	present := filepath.Join(dir, "there.tar")
	if err := os.WriteFile(present, []byte("data"), 0o600); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if !liveSnapshotPresent(present) {
		t.Error("an existing snapshot was reported absent")
	}

	if liveSnapshotPresent(filepath.Join(dir, "gone.tar")) {
		t.Error("an absent snapshot was reported present")
	}

	// A zero-byte file is not a snapshot: treating it as present would report
	// rollback protection villa cannot actually provide.
	empty := filepath.Join(dir, "empty.tar")
	if err := os.WriteFile(empty, nil, 0o600); err != nil {
		t.Fatalf("stage: %v", err)
	}
	if liveSnapshotPresent(empty) {
		t.Error("an empty file was reported as a usable snapshot")
	}
}

// TestTheDryRunStatesTheSnapshotCostBeforeItIsSpent.
//
// Measured on real hardware, memory's snapshot is 2.8 GB. On a small disk that is a
// decision input, and discovering it afterwards is discovering it too late.
func TestTheDryRunStatesTheSnapshotCostBeforeItIsSpent(t *testing.T) {
	var b bytes.Buffer
	printUpdateDryRun(&b, []updateflow.Target{
		{Subsystem: subsystem.Memory, Pins: map[string]string{"qdrant": "example.invalid/qdrant@sha256:new"}},
		{Subsystem: subsystem.Chat, Pins: map[string]string{"open-webui": "example.invalid/owui@sha256:new"}},
	}, nil, map[subsystem.Kind]int64{
		subsystem.Memory: 2_800_000_000,
		subsystem.Chat:   267_000_000,
	})
	got := b.String()

	if !strings.Contains(got, "2.8 GB") {
		t.Errorf("the dry run does not state memory's snapshot cost:\n%s", got)
	}
	if !strings.Contains(got, "267 MB") {
		t.Errorf("the dry run does not state chat's snapshot cost:\n%s", got)
	}
	if !strings.Contains(got, "3.1 GB") {
		t.Errorf("the dry run does not total the disk this run would need:\n%s", got)
	}
	// The stopped window is part of the cost, and a user planning a maintenance
	// window needs to know the service goes down.
	if !strings.Contains(got, "stop") {
		t.Errorf("the dry run hides the stopped window:\n%s", got)
	}
	if !strings.Contains(got, "Nothing has been changed") {
		t.Errorf("the dry run did not say it changed nothing:\n%s", got)
	}
}

// TestTheDryRunSaysUnknownRatherThanZeroWhenItCannotMeasure.
//
// Zero is a claim about a cost. "Villa could not tell" is not that claim, and
// printing 0 B for a volume villa could not stat would understate a cost the user
// is about to pay.
func TestTheDryRunSaysUnknownRatherThanZeroWhenItCannotMeasure(t *testing.T) {
	var b bytes.Buffer
	printUpdateDryRun(&b, []updateflow.Target{
		{Subsystem: subsystem.Memory, Pins: map[string]string{"qdrant": "example.invalid/qdrant@sha256:new"}},
	}, nil, nil)
	got := b.String()

	if !strings.Contains(got, "size unknown") {
		t.Errorf("an unmeasurable snapshot was not reported as unknown:\n%s", got)
	}
	if strings.Contains(got, "0 B") {
		t.Errorf("an unmeasurable snapshot was reported as free:\n%s", got)
	}
}

// TestAStatelessSubsystemsDryRunMentionsNoSnapshot: inference is not stopped and
// nothing is copied, so promising either would be wrong.
func TestAStatelessSubsystemsDryRunMentionsNoSnapshot(t *testing.T) {
	var b bytes.Buffer
	printUpdateDryRun(&b, []updateflow.Target{
		{Subsystem: subsystem.Inference, Pins: map[string]string{"backend-vulkan-radv": "example.invalid/b@sha256:new"}},
	}, nil, map[subsystem.Kind]int64{subsystem.Memory: 2_800_000_000})
	got := b.String()

	if strings.Contains(got, "snapshot") {
		t.Errorf("a stateless subsystem's plan promises a snapshot:\n%s", got)
	}
	if strings.Contains(got, "stop") {
		t.Errorf("a stateless subsystem's plan promises a stopped window:\n%s", got)
	}
}

// TestCleanupFollowsTheRunsStream: one run is one report, so the cleanup notes must
// not go to stdout while the failure they belong to went to stderr.
//
// The same property the image prune already has, asserted separately because it is
// a separate seam and a separate call site — and the image prune's version of this
// bug was found on hardware, where the call site passed `out` unconditionally.
func TestCleanupFollowsTheRunsStream(t *testing.T) {
	run := func(halted bool) (stdout, stderr string) {
		var outBuf, errBuf bytes.Buffer
		cmd := &cobra.Command{}
		cmd.SetOut(&outBuf)
		cmd.SetErr(&errBuf)
		cmd.SetContext(context.Background())

		proof := updateflow.Proof{Status: updateflow.ProofPass}
		if halted {
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
					Stop:   func(context.Context, subsystem.Kind) error { return nil },
					SnapshotData: func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
						return pinstate.DataSnapshot{Volume: "villa-qdrant", Path: "/snap/memory.tar", Bytes: 1}, nil
					},
					Start:    func(context.Context, subsystem.Kind) error { return nil },
					ProveNew: func(context.Context, subsystem.Kind) updateflow.Proof { return proof },
					Restore:  func(context.Context, subsystem.Kind, updateflow.Capture) error { return nil },
					RestoreData: func(context.Context, subsystem.Kind, pinstate.DataSnapshot) error {
						return nil
					},
					ProveRestored: func(context.Context, subsystem.Kind) updateflow.Proof {
						return updateflow.Proof{Status: updateflow.ProofPass}
					},
					Commit: func(subsystem.Kind, map[string]string, pinstate.Previous) error { return nil },
				}
			},
			ReferencedRefs: func() map[string]bool { return nil },
			SnapshotCleanup: func(w io.Writer, _ updateflow.Result) {
				fmt.Fprint(w, "CLEANUP-MARKER\n")
			},
		}
		apply(cmd, d, updatableReport(), nil, updateFlags{})
		return outBuf.String(), errBuf.String()
	}

	stdout, stderr := run(false)
	if !strings.Contains(stdout, "CLEANUP-MARKER") {
		t.Errorf("on a successful run cleanup did not write to stdout:\nstdout=%q\nstderr=%q", stdout, stderr)
	}

	stdout, stderr = run(true)
	if strings.Contains(stdout, "CLEANUP-MARKER") {
		t.Errorf("on a HALTED run cleanup wrote to stdout while the failure went to stderr:\nstdout=%q", stdout)
	}
	if !strings.Contains(stderr, "CLEANUP-MARKER") {
		t.Errorf("on a halted run cleanup did not follow the narration to stderr:\nstderr=%q", stderr)
	}
}
