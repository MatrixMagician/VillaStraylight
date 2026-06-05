package preflight

import (
	"io"
	"os/exec"
	"strings"
)

// maxToolOutput bounds untrusted tool stdout (podman/loginctl) so a runaway or
// hostile process cannot exhaust memory (threat T-03-04). Mirrors the same bound
// in internal/detect.
const maxToolOutput = 8 << 10 // 8 KiB

// runTool invokes a tool with a FIXED argument slice — never `sh -c` with
// interpolation (threat T-03-01) — and returns its stdout bounded to
// maxToolOutput bytes via io.LimitReader. A missing binary yields found=false so
// the caller can downgrade an unevaluable BLOCK to WARN (D-15). A non-zero exit
// yields ok=false with whatever bounded output was produced (for raw capture).
func runTool(name string, args ...string) (out string, found, ok bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false, false
	}
	cmd := exec.Command(name, args...) // fixed args; no shell interpolation
	raw, err := cmd.Output()
	bounded, _ := io.ReadAll(io.LimitReader(strings.NewReader(string(raw)), maxToolOutput))
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
