package preflight

import (
	"context"
	"io"
	"os/exec"
	"strings"
	"time"
)

// maxToolOutput bounds untrusted tool stdout (podman/loginctl) so a runaway or
// hostile process cannot exhaust memory (threat T-03-04). Mirrors the same bound
// in internal/detect.
const maxToolOutput = 8 << 10 // 8 KiB

// toolTimeout bounds every runTool invocation (phase-22 WR-02): a wedged tool —
// e.g. a hung rootless podman on a stale user socket, a state this product can
// itself produce — must degrade to an unevaluable ok=false (→ typed-Unknown WARN
// at the caller, D-15), never hang preflight/install/doctor indefinitely.
const toolTimeout = 10 * time.Second

// runTool invokes a tool with a FIXED argument slice — never `sh -c` with
// interpolation (threat T-03-01) — bounded BOTH in time (toolTimeout via
// exec.CommandContext, WR-02) and in memory: stdout is read through a pipe-level
// io.LimitReader capped at maxToolOutput, then drained without buffering so a
// chatty tool can neither exhaust memory nor block on a full pipe. A missing
// binary yields found=false so the caller can downgrade an unevaluable BLOCK to
// WARN (D-15). A non-zero exit / timeout yields ok=false with whatever bounded
// output was produced (for raw capture).
func runTool(name string, args ...string) (out string, found, ok bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), toolTimeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, name, args...) // fixed args; no shell interpolation
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return "", true, false
	}
	if err := cmd.Start(); err != nil {
		return "", true, false
	}
	bounded, _ := io.ReadAll(io.LimitReader(stdout, maxToolOutput))
	// Drain the remainder without buffering: the memory bound stays real AND the
	// child can never deadlock writing into a full, unread pipe. A hung child is
	// killed at toolTimeout by the context, which also unblocks this read.
	_, _ = io.Copy(io.Discard, stdout)
	err = cmd.Wait()
	return string(bounded), true, err == nil
}

// joinProvenance combines non-empty provenance strings into one, so a result
// derived from two facts records both sources for -v.
func joinProvenance(parts ...string) string {
	var nonEmpty []string
	for _, p := range parts {
		if strings.TrimSpace(p) != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	return strings.Join(nonEmpty, "; ")
}

// joinRaw combines non-empty raw captures, bounded so the -v surface stays small.
func joinRaw(parts ...string) string {
	joined := joinProvenance(parts...)
	if len(joined) > maxToolOutput {
		return joined[:maxToolOutput]
	}
	return joined
}
