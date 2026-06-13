// Package agent is the pure, Deps-injected core for VillaStraylight's strictly-
// local terminal coding agent (Crush v0.76.0): the agent-delivery spine behind
// the `villa code` launcher verb. It owns four pure concerns (D-01):
//
//   - policy.go : the go:embed Crush pin policy (version + per-platform asset +
//     tarball/binary SHA-256 + URL template) and the pure VerifyTarball install
//     gate (AGENT-01, D-02/D-03).
//   - render.go : Render(cfg, probes) → a DETERMINISTIC crush.json derived from
//     config.toml (both kill switches, exactly one loopback openai-compat
//     provider, a villa- prefixed non-empty models[], detected-only LSP entries)
//     (AGENT-02, D-06..D-10).
//   - version.go: a verbatim clone of preflight's dotted-version comparator.
//   - drift.go  : DetectDrift(in) → a report-only DriftReport (binary + config
//     drift, plus the config-absent first-run signal) that NEVER auto-corrects
//     (AGENT-04, D-14).
//
// Seam discipline (CLAUDE.md / TestSeamGrepGate, which walks internal/):
// internal/agent imports NEITHER internal/inference NOR internal/detect, and it
// holds NO backend marker tokens (image tags, Vulkan0/ROCm0, HSA_OVERRIDE…,
// device args, --jinja / sampling flags). The ONLY non-config literals the core
// may carry are the loopback inference URL (http://127.0.0.1:8080/v1) and the
// villa- model-id prefix — neither is a backend marker. Backend literals live
// behind internal/inference + internal/orchestrate (Phase 25), and `villa code`
// launches the agent against the ALREADY-served endpoint; it renders no units.
//
// Pure-core + injected-seam (clones internal/codingmode's Deps/Result ergonomics,
// NOT its transactional state machine — Phase 26 has no capture/prove/rollback):
// every host-touching action is an injected func field on Deps so the whole flow
// is driven from agent_test.go without a live host. The live wiring is a
// liveAgentDeps() closure in cmd/villa (Plan 02). The cores return typed values
// (Result / DriftReport / render bytes), NEVER os.Exit, NEVER print.
package agent

import (
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// Warning is a non-blocking, surfaced-with-remediation signal (typed-Unknown
// degradation → WARN, never BLOCK). The primary producer is the LSP render: a
// toolchain server missing from PATH yields a Warning, never an error (D-10).
type Warning struct {
	// Code is a stable machine token (e.g. "lsp_missing", "coding_mode_off").
	Code string
	// Msg is the human-readable WARN line carrying an actionable install/remediation hint.
	Msg string
}

// Deps is the injectable seam set for the agent flow. Every host-touching action
// is a func field so agent_test.go drives the flow without a live host; the live
// wiring (liveAgentDeps) lives in cmd/villa (Plan 02). It clones the
// codingmode.Deps shape (func-field-per-host-effect), not its state machine.
//
// Run() — the orchestration that consumes these — lands with the live wiring in
// Plan 02. Plan 01 defines ONLY the types so Plan 02 has the contract.
type Deps struct {
	// LoadConfig loads the current persisted config (the source of truth feeding
	// Render — Model / CoderModel / CoderAgentCtx, D-06/D-09).
	LoadConfig func() (config.VillaConfig, error)
	// LookPath probes the host PATH for an LSP server (D-10) — exec.LookPath in the
	// live wiring; references only, NEVER auto-installs. Returns (path, found).
	LookPath func(bin string) (string, bool)
	// ReadConfig reads the on-disk ~/.config/crush/crush.json (the config-drift
	// input, D-14b). Used by the live flow to populate DriftInput.OnDiskConfig /
	// ConfigPresent; a not-exist read maps to ConfigPresent=false (first-run).
	ReadConfig func() ([]byte, bool, error)
	// HashBinary returns the SHA-256 of the villa-owned binary at
	// $XDG_DATA_HOME/villa/bin/crush (binary-drift input, D-14a). Returns
	// (hexSum, present, err); present=false → BinaryAbsent (Phase-27 install remediation).
	HashBinary func() (string, bool, error)
	// WriteConfig persists the freshly-rendered crush.json to
	// ~/.config/crush/crush.json (MkdirAll 0700 + WriteFile 0600, traversal-guarded
	// in the live wiring). The render output, written on the first-run absent case.
	WriteConfig func(b []byte) error
	// Launch execs the villa-owned crush binary with the belt-and-braces lockdown
	// env (D-11) — syscall.Exec in the live wiring (fixed-arg, no shell). It is
	// reached ONLY on a clean presence+drift check (D-13).
	Launch func(env []string) error
}

// Result is the typed outcome of the `villa code` flow (NOT an exit code) so the
// cobra caller (Plan 02) can branch on it and map it to an exit code + messages.
// It clones the codingmode.Result shape: discrete bools + a Reason +
// Warnings []Warning, never os.Exit / print.
type Result struct {
	// BinaryAbsent is true when the villa-owned crush binary is not installed →
	// remediation pointing at the Phase-27 install addon (graceful, not a crash,
	// D-13). Parallels ConfigAbsent.
	BinaryAbsent bool
	// BinaryDrift is true when the installed binary SHA-256 != the policy
	// binarySha256 (D-14a) → surface + remediation, NEVER auto-correct.
	BinaryDrift bool
	// ConfigAbsent is true on the FIRST-RUN case: no on-disk crush.json yet. This
	// is the render-then-launch trigger — DISTINCT from ConfigDrift (it parallels
	// BinaryAbsent). The caller renders the reference then launches; it does NOT
	// refuse (D-14).
	ConfigAbsent bool
	// ConfigDrift is true when an on-disk crush.json is PRESENT but differs
	// semantically from the freshly-rendered reference (hand-edit / staleness,
	// D-14b) → surface + remediation, NEVER auto-correct.
	ConfigDrift bool
	// Launched is true when the presence+drift check was clean and Crush was exec'd.
	Launched bool
	// Reason is the human refusal/remediation explanation (empty on a clean launch).
	Reason string
	// Warnings carries non-blocking signals (LSP-missing, coding-mode-off) for the
	// caller to print without blocking the launch (D-10/D-12).
	Warnings []Warning
	// Err is a non-refusal failure (config load / render / write / hash).
	Err error
}
