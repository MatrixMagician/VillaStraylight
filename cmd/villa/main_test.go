package main

import (
	"context"
	"errors"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/spf13/cobra"
)

// main_test.go guards the signal wiring in run()/runTree(): that the context
// reaching a command body is cancellable at all, that a real SIGINT cancels it,
// and that an interrupted run is reported as 130 rather than a generic failure.
//
// The bug these pin: main previously called cobra's Execute(), which fills an
// un-cancellable context.Background(). Every cmd.Context() in the tree was
// therefore permanently un-cancellable, so the documented "Ctrl-C aborts an
// in-flight bench and restores the original backend" could not happen — the
// process died before the pure core's deferred Restore ran.

// stubRoot builds a minimal command tree whose single `probe` subcommand hands its
// context back to the caller. The real newRoot() is unusable here: its bodies probe
// the host and call os.Exit.
func stubRoot(body func(cmd *cobra.Command) error) *cobra.Command {
	root := &cobra.Command{Use: "villa", SilenceUsage: true, SilenceErrors: true}
	root.AddCommand(&cobra.Command{
		Use: "probe",
		RunE: func(cmd *cobra.Command, _ []string) error {
			return body(cmd)
		},
	})
	return root
}

// TestRunTreeContextIsCancellable asserts the context a command body receives
// through the REAL runTree path is wired to a cancel source. A background context
// returns nil from Done(), so a non-nil Done channel is exactly the property that
// was missing before.
func TestRunTreeContextIsCancellable(t *testing.T) {
	var got context.Context
	code := runTree(stubRoot(func(cmd *cobra.Command) error {
		got = cmd.Context()
		return nil
	}), []string{"probe"})

	if code != exitPass {
		t.Fatalf("runTree = %d, want %d", code, exitPass)
	}
	if got == nil {
		t.Fatal("command body received a nil context")
	}
	if got.Done() == nil {
		t.Fatal("command context is not cancellable (Done() == nil) — a Ctrl-C could never " +
			"reach an in-flight bench/download, so their deferred restore/cleanup never runs")
	}
}

// TestRunTreeCancelsBodyOnInterrupt drives the real path end to end: a command
// body raises an actual SIGINT at the process and then blocks on its own context.
// It returns only if that context is cancelled, which is precisely what lets a
// Ctrl-C during `villa bench --ab` unwind through the pure core's deferred Restore
// instead of killing the process mid-flip.
func TestRunTreeCancelsBodyOnInterrupt(t *testing.T) {
	var cancelled bool
	code := runTree(stubRoot(func(cmd *cobra.Command) error {
		if err := syscall.Kill(os.Getpid(), syscall.SIGINT); err != nil {
			t.Errorf("raise SIGINT: %v", err)
			return nil
		}
		select {
		case <-cmd.Context().Done():
			cancelled = true
			return cmd.Context().Err()
		case <-time.After(5 * time.Second):
			t.Error("SIGINT did not cancel the in-flight command context")
			return nil
		}
	}), []string{"probe"})

	if !cancelled {
		t.Fatal("command body was never cancelled by SIGINT")
	}
	// An interrupted run reports 130 (128+SIGINT), not the generic failure 1, so a
	// scripted caller can tell "the user interrupted this" from "this failed".
	if code != exitInterrupted {
		t.Fatalf("runTree = %d, want %d (128+SIGINT)", code, exitInterrupted)
	}
}

// TestRunTreeReportsRealFailureAsOne asserts the 130 path is specific to an
// interrupt: an ordinary command error must still exit 1, or the interrupt code
// would swallow genuine failures.
func TestRunTreeReportsRealFailureAsOne(t *testing.T) {
	code := runTree(stubRoot(func(*cobra.Command) error {
		return errors.New("something genuinely broke")
	}), []string{"probe"})

	if code != 1 {
		t.Fatalf("runTree on a plain error = %d, want 1", code)
	}
}

// TestInterruptCodeIsDistinct keeps the interrupt code from colliding with villa's
// own verdict codes — a caller must be able to tell a preflight BLOCK from a Ctrl-C.
func TestInterruptCodeIsDistinct(t *testing.T) {
	if exitInterrupted != 130 {
		t.Fatalf("exitInterrupted = %d, want 130 (128+SIGINT)", exitInterrupted)
	}
	for name, code := range map[string]int{
		"exitPass":    exitPass,
		"exitWarn":    exitWarn,
		"exitBlocked": exitBlocked,
	} {
		if code == exitInterrupted {
			t.Fatalf("%s collides with exitInterrupted (%d)", name, exitInterrupted)
		}
	}
}
