// Command villa is the VillaStraylight control plane: a single static Go binary
// that detects an AMD Strix Halo (gfx1151) host, recommends a memory-fitting
// model/quant/context, and gates installs behind a preflight check. Phase 1
// delivers the read-only `detect` slice.
package main

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/spf13/cobra"
)

// run executes the command tree under an interrupt-cancelled context and returns
// the process exit code. It is separated from main so the signal wiring is
// exercisable without spawning a subprocess (main_test.go).
//
// The context is what makes Ctrl-C recoverable rather than fatal. Cobra's
// Execute() fills an un-cancellable context.Background(), so before this the
// cmd.Context() threaded through bench.Run, download.PullModel and every other
// long-running seam could never fire — despite those call sites documenting a
// "SIGINT-cancelled" context. A Ctrl-C during `villa bench --ab` therefore killed
// the process outright, skipping the pure core's `defer d.Restore` and stranding
// the stack on the OTHER inference backend with no indication. Cancelling the
// context instead unwinds through that defer, so the original backend is restored.
//
// stop() is deliberately called before Execute returns rather than only deferred:
// once the tree is done, reverting to the default disposition means a SECOND
// Ctrl-C (during a hung restore, say) still kills the process the way a user
// expects. NotifyContext already gives that for free — the first signal cancels,
// and the handler is unregistered, so the next one terminates.
func run(args []string) int {
	return runTree(newRoot(), args)
}

// runTree is run's body with the command tree injected, so a test can assert the
// signal/exit wiring against a stub command instead of the real tree (whose
// bodies probe the host and call os.Exit).
func runTree(root *cobra.Command, args []string) int {
	ctx, stop := signalContext()
	defer stop()

	root.SetArgs(args)

	err := root.ExecuteContext(ctx)
	// Distinguish a signal from stop()'s own cancellation by CAUSE, not by
	// ctx.Err(): both stop() and the re-arming goroutine in signalContext cancel
	// this same context, so a bare ctx.Err() check would misreport ordinary
	// failures as interrupts. NotifyContext sets a "<signal> signal received"
	// cause only on the signal path, and context.Cause returns the FIRST cause
	// recorded, so a later stop() cannot mask it.
	interrupted := interruptedBySignal(ctx)
	stop()
	if err != nil {
		// A cancelled run is the user's own Ctrl-C, not a failure to explain: report
		// it as the conventional 130 (128+SIGINT) without an error line, so a piped
		// caller can tell "interrupted" from "the command genuinely failed".
		if interrupted {
			return exitInterrupted
		}
		fmt.Fprintln(os.Stderr, "villa:", err)
		return 1
	}
	return 0
}

// exitInterrupted is the conventional shell code for a SIGINT-terminated process
// (128 + SIGINT). It is distinct from villa's own exit codes, so a caller can
// tell an interrupt apart from a BLOCK/WARN verdict.
const exitInterrupted = 130

// errInterrupted is the cancellation CAUSE signalContext attaches when a signal
// (rather than a normal teardown) cancels the run. Owning the sentinel here means
// the interrupt check is an errors.Is against our own value, not a match on
// stdlib's unexported "<signal> signal received" error text.
var errInterrupted = errors.New("interrupted by signal")

// interruptedBySignal reports whether ctx was cancelled by a SIGINT/SIGTERM as
// opposed to an ordinary teardown. context.Cause returns the FIRST cause
// recorded, so the later stop() cannot mask a real interrupt.
func interruptedBySignal(ctx context.Context) bool {
	return errors.Is(context.Cause(ctx), errInterrupted)
}

// signalContext returns a context cancelled by the first SIGINT (Ctrl-C) or
// SIGTERM, plus the stop func that unregisters the handler.
//
// SIGTERM is included alongside SIGINT because `villa dashboard` and
// `villa websafe-serve` run as long-lived systemd units, and systemd stops a unit
// with SIGTERM. Handling only SIGINT would leave those two with no graceful path
// at all.
//
// The handler is unregistered (signal.Stop) as soon as the first signal lands,
// restoring the DEFAULT disposition. Without that, a second Ctrl-C would also be
// swallowed — leaving a user with no way out of a graceful shutdown that is itself
// hung (a wedged backend restore, say) short of a SIGKILL from another terminal.
// First signal cancels and unwinds; second signal terminates, which is what
// pressing Ctrl-C twice means.
//
// This is spelled out rather than delegated to signal.NotifyContext because both
// behaviours above are ones NotifyContext does not offer: it keeps its handler
// registered until stop(), and it records a cause we would have to string-match.
func signalContext() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancelCause(context.Background())

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, os.Interrupt, syscall.SIGTERM)

	go func() {
		select {
		case <-ch:
			// Restore the default disposition FIRST, so the next signal kills the
			// process even if the cancellation below unwinds into a hung cleanup.
			signal.Stop(ch)
			cancel(errInterrupted)
		case <-ctx.Done():
			signal.Stop(ch)
		}
	}()

	// The returned stop cancels with a nil cause, so context.Cause keeps reporting
	// errInterrupted when a signal got there first.
	return ctx, func() { cancel(nil) }
}

func main() {
	os.Exit(run(os.Args[1:]))
}
