package orchestrate

import (
	"fmt"
	"io"
	"os/exec"
	"strings"
)

// systemd.go is the thin os/exec lifecycle seam over the rootless user manager:
// every call is FIXED-ARG (no shell, no interpolation — threat T-03-01), each tool
// is exec.LookPath-probed first and degrades to a typed not-found instead of
// panicking (mirrors internal/preflight runTool), and the journald read is bounded
// by an io.LimitReader so an unbounded container log cannot exhaust memory
// (T-03-06). It deliberately holds NO podman invocation (the backend grep-gate) and
// NO /health HTTP poll — the readiness poll lives in the install/up cmd layer (Plan
// 02/03) so this package stays free of HTTP coupling; only systemd/journal
// primitives are exposed here.

// maxJournalOutput bounds the journald residency-scrape read (still mirrors the
// internal/preflight maxToolOutput / internal/detect bound contract, just larger).
// 256 KiB: the residency proof line (`load_tensors: Vulkan0 model buffer size`) sits
// ~14 KiB into an invocation's oldest-first startup at the `-lv 4` verbosity the
// backend now requests, so the prior 8 KiB head-window truncated it before the
// scraper could see it (F-3). 256 KiB comfortably covers startup while still bounding
// an unbounded container log against memory exhaustion (T-03-06). Other callers are
// unaffected: is-active output is tiny; `villa logs` simply reads more.
const maxJournalOutput = 256 << 10 // 256 KiB

// ErrToolNotFound is the typed degradation when a required host tool is absent on
// PATH — callers downgrade rather than crash (mirrors the preflight found=false
// contract / D-15).
type ErrToolNotFound struct{ Tool string }

func (e ErrToolNotFound) Error() string {
	return fmt.Sprintf("orchestrate: %s not found on PATH", e.Tool)
}

// ErrCommandFailed is the typed degradation when a tool was found on PATH and ran
// but exited non-zero with no parseable output. It is DISTINCT from ErrToolNotFound
// (cannot measure at all) so callers can treat an indeterminate-but-bad runtime
// state as a hard failure while a missing binary stays a can't-measure WARN
// (CR-02 tighten: empty-stdout-with-error must not collapse to a soft WARN).
type ErrCommandFailed struct{ Cmd string }

func (e ErrCommandFailed) Error() string {
	return fmt.Sprintf("orchestrate: %s exited non-zero with no output", e.Cmd)
}

// Systemd is the user-manager lifecycle seam. The exec function is injectable so
// the cmd-layer tests can stub systemctl/loginctl/journalctl without a real host
// (the internal/preflight lingerOutput seam pattern). Construct with NewSystemd.
type Systemd struct {
	// runCmd runs a tool with fixed args, returning bounded stdout, whether the
	// tool was found on PATH, and whether it exited cleanly. Injectable for tests.
	runCmd func(name string, args ...string) (out string, found, ok bool)
}

// NewSystemd returns a Systemd wired to the real host via fixed-arg exec.Command.
func NewSystemd() Systemd {
	return Systemd{runCmd: runTool}
}

// runTool invokes a tool with a FIXED argument slice — never via a shell (T-03-01) —
// returning stdout bounded to maxJournalOutput via io.LimitReader. A missing binary
// yields found=false so the caller degrades; a non-zero exit yields ok=false with
// whatever bounded output was produced. Mirrors internal/preflight.runTool.
func runTool(name string, args ...string) (out string, found, ok bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false, false
	}
	cmd := exec.Command(name, args...) // fixed args; no shell interpolation
	raw, err := cmd.Output()
	bounded, _ := io.ReadAll(io.LimitReader(strings.NewReader(string(raw)), maxJournalOutput))
	return string(bounded), true, err == nil
}

// run is the internal helper: fixed-arg call that returns a typed not-found error
// when the tool is absent, and a generic error when it exits non-zero.
func (s Systemd) run(name string, args ...string) (string, error) {
	out, found, ok := s.runCmd(name, args...)
	if !found {
		return "", ErrToolNotFound{Tool: name}
	}
	if !ok {
		return out, fmt.Errorf("orchestrate: %s %s failed", name, strings.Join(args, " "))
	}
	return out, nil
}

// DaemonReload re-reads the user unit files after units are (re)written:
// `systemctl --user daemon-reload`.
func (s Systemd) DaemonReload() error {
	_, err := s.run("systemctl", "--user", "daemon-reload")
	return err
}

// Start starts a user service: `systemctl --user start <service>`.
func (s Systemd) Start(service string) error {
	_, err := s.run("systemctl", "--user", "start", service)
	return err
}

// Enable enables a user service so its [Install] WantedBy=default.target takes
// effect for boot-survival: `systemctl --user enable <service>` (A4). It is the
// additive unit-level enable seam — DISTINCT from EnableLinger (which enables the
// whole user manager to survive logout via loginctl). Fixed-arg, never a shell
// (T-05-15/T-03-01); a missing systemctl degrades to a typed ErrToolNotFound
// (mirrors Start). Linger is enabled separately by install (no second call, D-04).
func (s Systemd) Enable(service string) error {
	_, err := s.run("systemctl", "--user", "enable", service)
	return err
}

// Disable reverses Enable, revoking a unit's boot-survival so an uninstalled
// dashboard cannot re-spawn its listener on the next login (T-05-18):
// `systemctl --user disable <service>`. Fixed-arg, never a shell; a missing
// systemctl degrades to a typed ErrToolNotFound (mirrors Enable/Stop).
func (s Systemd) Disable(service string) error {
	_, err := s.run("systemctl", "--user", "disable", service)
	return err
}

// Stop stops a user service: `systemctl --user stop <service>`.
func (s Systemd) Stop(service string) error {
	_, err := s.run("systemctl", "--user", "stop", service)
	return err
}

// Restart restarts a user service: `systemctl --user restart <service>`.
func (s Systemd) Restart(service string) error {
	_, err := s.run("systemctl", "--user", "restart", service)
	return err
}

// IsActive reports the activation state of a user service:
// `systemctl --user is-active <service>` → "active"/"inactive"/"failed". A non-zero
// exit is EXPECTED for the inactive/failed states — and those print their state word
// to stdout — so a non-empty trimmed state is returned regardless of exit code. Two
// states are errors: a missing binary (ErrToolNotFound — cannot measure) and an
// empty stdout WITH a non-zero exit (ErrCommandFailed — the manager/unit errored
// with no parseable state; distinct so the caller can FAIL rather than soft-WARN,
// CR-02 tighten).
func (s Systemd) IsActive(service string) (string, error) {
	out, found, ok := s.runCmd("systemctl", "--user", "is-active", service)
	if !found {
		return "", ErrToolNotFound{Tool: "systemctl"}
	}
	state := strings.TrimSpace(out)
	if state == "" && !ok {
		return "", ErrCommandFailed{Cmd: "systemctl --user is-active " + service}
	}
	return state, nil
}

// EnableLinger enables user lingering so the rootless user manager (and the
// Quadlet services) survive logout/reboot: `loginctl enable-linger <user>` (D-04
// consent step).
func (s Systemd) EnableLinger(user string) error {
	_, err := s.run("loginctl", "enable-linger", user)
	return err
}

// DisableLinger reverses EnableLinger: `loginctl disable-linger <user>`.
func (s Systemd) DisableLinger(user string) error {
	_, err := s.run("loginctl", "disable-linger", user)
	return err
}

// JournalText returns the bounded user-journal text for a service:
// `journalctl --user -u <service> --no-pager`, capped at maxJournalOutput via the
// io.LimitReader bound in runCmd. The bool reports whether journalctl was found and
// produced output (residency-log recovery / `logs`); a missing journalctl or empty
// output yields ("", false).
func (s Systemd) JournalText(service string) (string, bool) {
	out, found, _ := s.runCmd("journalctl", "--user", "-u", service, "--no-pager")
	if !found || strings.TrimSpace(out) == "" {
		return "", false
	}
	return out, true
}

// ResidencyJournal returns the CURRENT-INVOCATION user-journal for a service, used by
// `villa status` to scrape the running server's GPU-offload residency proof. The unit
// journal accrues across every restart (crash-loops, swaps, reboots), so a plain
// `-u <service>` read bounded to the oldest maxJournalOutput bytes (JournalText) yields
// stale prior-start output that never contained `load_tensors` — the residency line of
// the CURRENTLY running server is missed (F-3). This scopes the read to the live
// invocation via `systemctl --user show -p InvocationID --value <service>` →
// `journalctl --user _SYSTEMD_INVOCATION_ID=<id> --no-pager`. Head-keeping (the
// io.LimitReader bound in runCmd) is correct here: the residency line is emitted at the
// START of the invocation's oldest-first journal.
//
// It is DISTINCT from JournalText (left unchanged for `villa logs`, which shows the
// whole-unit journal). Degradation mirrors JournalText: a missing InvocationID (service
// never started / systemctl absent) falls back to the unit-scoped read, and a missing
// journalctl or empty output yields ("", false) so the offload assert degrades to a
// typed-Unknown WARN rather than a false PASS.
func (s Systemd) ResidencyJournal(service string) (string, bool) {
	inv := ""
	if out, found, ok := s.runCmd("systemctl", "--user", "show", "-p", "InvocationID", "--value", service); found && ok {
		inv = strings.TrimSpace(out)
	}

	var (
		out   string
		found bool
	)
	if inv != "" {
		out, found, _ = s.runCmd("journalctl", "--user", "_SYSTEMD_INVOCATION_ID="+inv, "--no-pager")
	} else {
		// No invocation id (service not started, or systemctl unavailable): fall back
		// to the whole-unit read so a present-but-unscopable journal is not dropped.
		out, found, _ = s.runCmd("journalctl", "--user", "-u", service, "--no-pager")
	}
	if !found || strings.TrimSpace(out) == "" {
		return "", false
	}
	return out, true
}
