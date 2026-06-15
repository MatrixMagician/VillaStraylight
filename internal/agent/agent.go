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
	"fmt"
	"os"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// osEnviron is the inherited-environment source for lockdownEnv, a package var seam
// so agent_test.go can assert the lockdown vars are APPENDED to (not replacing) the
// process env without depending on the live os.Environ. Defaults to os.Environ.
var osEnviron = os.Environ

// lockdown env vars (D-11): the belt-and-braces telemetry/autoupdate kill switches
// set in the launch env BEFORE exec, redundant with the config kill switches (Plan
// 01 render). They are CONSTANT literals — no user-controlled string is interpolated
// (T-26-12). DO_NOT_TRACK is the cross-tool privacy convention; the two CRUSH_*
// vars disable Crush's metrics + provider auto-update at the process level.
const (
	envCrushDisableMetrics    = "CRUSH_DISABLE_METRICS=1"
	envDoNotTrack             = "DO_NOT_TRACK=1"
	envCrushDisableAutoUpdate = "CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1"
)

// targetPlatformKey is the policy.Assets key for the supported install target. Phase
// 26 targets linux/amd64 (Fedora/Strix Halo); the policy table is structured to allow
// a future darwin/arm64 without a code change here. Kept as a fixed constant (NOT a
// compile-time platform branch) so cmd/villa/code.go and this core stay seam-clean —
// the seam gate forbids a platform branch outside the inference seam.
const targetPlatformKey = "linux/amd64"

// KnownLSPServers is the fixed probe list (D-10) and the SINGLE source of truth for
// the set of LSP servers villa probes and renders an lsp entry for WHEN found on PATH.
// Each pair is {crush.json lsp key, server command}. A missing server is a WARN +
// omitted entry, never a BLOCK (renderLSP / Render owns that degradation).
//
// Exported so the install/doctor live probe seam (cmd/villa liveLSPProbes) consumes the
// SAME list — the launcher (villa code) and the install render MUST agree by
// construction, or every install would render a crush.json that villa code then refuses
// as ConfigDrift (D-14 is report-only; it assumes the install path is authoritative).
// The python entry is pyright-langserver (the LSP entry point), NOT pyright (the
// standalone CLI type-checker — a different binary the Pyright distribution also ships).
var KnownLSPServers = []struct{ Key, Command string }{
	{"go", "gopls"},
	{"python", "pyright-langserver"},
	{"rust", "rust-analyzer"},
	{"typescript", "typescript-language-server"},
}

// Run is the orchestration for `villa code` (AGENT-03/AGENT-04 live half). It mirrors
// codingmode.Run's load-then-guard-then-act SHAPE but carries NO transactional
// capture/prove/rollback: `villa code` only reads host state, optionally renders a
// first-run config when ABSENT, and reports ready-to-launch. It composes the Plan-01
// pure cores (Render + DetectDrift) and the injected Deps host seams, returning a typed
// Result — it NEVER os.Exit, NEVER prints, and NEVER execs (the cobra caller maps Result
// → exit + messages, prints Warnings, THEN performs the single d.Launch).
//
// Ordering (D-13/D-14):
//  1. LoadConfig (source of truth) — on error, a non-refusal Err Result.
//  2. Probe LSP servers via LookPath (references only, never installs — D-10).
//  3. Render(cfg, probes) → the reference bytes (BOTH the drift-compare target AND
//     the first-run write payload) + LSP warnings.
//  4. HashBinary + ReadConfig → DetectDrift.
//  5. BinaryAbsent → graceful remediation, return WITHOUT writing or launching.
//  6. ConfigAbsent (first-run) → WriteConfig(reference) then PROCEED to launch (NOT
//     drift). The ONLY auto-write path, and ONLY when absent (D-14).
//  7. BinaryDrift || ConfigDrift (present-but-differs) → surface + remediation,
//     return WITHOUT launching, WITHOUT writing, WITHOUT auto-correcting (D-14).
//     BinaryDriftUnknown (unpinned sentinel) → carry a WARN, do NOT block (Pitfall 6).
//  8. Clean / first-run-rendered → build the lockdown env (D-11) into res.LaunchEnv and
//     set res.ReadyToLaunch. The caller prints Warnings then performs the SINGLE
//     d.Launch — so a coding-mode-OFF WARN (caller surfaces it; this core NEVER mutates
//     the toggle — D-12) is shown BEFORE the exec replaces the process.
func Run(d Deps) Result {
	var res Result

	// (1) Config is the source of truth feeding Render (D-06).
	cfg, err := d.LoadConfig()
	if err != nil {
		res.Err = fmt.Errorf("agent: load config: %w", err)
		res.Reason = "could not load villa config — fix config.toml and retry"
		return res
	}

	// (2) Probe LSP servers (references only, never installs — D-10).
	probes := make([]LSPProbe, 0, len(KnownLSPServers))
	for _, srv := range KnownLSPServers {
		_, found := d.LookPath(srv.Command)
		probes = append(probes, LSPProbe{Key: srv.Key, Command: srv.Command, Found: found})
	}

	// (3) Freshly render the reference — BOTH the drift-compare target AND the bytes
	// written on the first-run config-absent path.
	reference, warns, err := Render(cfg, probes)
	if err != nil {
		res.Err = fmt.Errorf("agent: render crush.json: %w", err)
		res.Reason = "could not render the Crush config from your config.toml"
		return res
	}
	res.Warnings = append(res.Warnings, warns...)

	// (4) Probe the installed binary + on-disk config, then detect drift (report-only).
	binSHA, binPresent, err := d.HashBinary()
	if err != nil {
		res.Err = fmt.Errorf("agent: hash villa-owned Crush binary: %w", err)
		res.Reason = "could not read the villa-owned Crush binary"
		return res
	}
	onDisk, configPresent, err := d.ReadConfig()
	if err != nil {
		res.Err = fmt.Errorf("agent: read on-disk crush.json: %w", err)
		res.Reason = "could not read the on-disk crush.json"
		return res
	}

	policy := loadCrushPolicy()
	var policyBinSHA string
	if asset, ok := policy.Assets[targetPlatformKey]; ok {
		policyBinSHA = asset.BinarySHA256
	}

	report := DetectDrift(DriftInput{
		BinaryPresent:   binPresent,
		InstalledBinSHA: binSHA,
		PolicyBinSHA:    policyBinSHA,
		ConfigPresent:   configPresent,
		OnDiskConfig:    onDisk,
		RenderedConfig:  reference,
	})

	// (5) BinaryAbsent → graceful Phase-27 install remediation; never crash, never
	// write, never launch (D-13).
	if report.BinaryAbsent {
		res.BinaryAbsent = true
		res.Reason = report.Reason
		return res
	}

	// (7a) Present-but-differs binary drift → surface + remediation, never auto-correct
	// (D-14a). A BinaryDriftUnknown sentinel is NOT a block — it carries a WARN below.
	if report.BinaryDrift {
		res.BinaryDrift = true
		res.Reason = report.Reason
		return res
	}
	if report.BinaryDriftUnknown {
		res.Warnings = append(res.Warnings, Warning{
			Code: "binary_drift_unknown",
			Msg:  report.Reason,
		})
	}

	// (7b) Present-but-differs CONFIG drift → surface + remediation, never auto-correct
	// (D-14b). Checked BEFORE the absent-render path so a present-but-drifting config is
	// never overwritten.
	if report.ConfigDrift {
		res.ConfigDrift = true
		res.Reason = report.Reason
		return res
	}

	// (6) FIRST-RUN config-absent → render-then-launch (NOT drift). The ONLY auto-write
	// path, and ONLY when the config is ABSENT (D-14). Write the freshly-rendered
	// reference, then PROCEED to launch.
	if report.ConfigAbsent {
		if err := d.WriteConfig(reference); err != nil {
			res.Err = fmt.Errorf("agent: write first-run crush.json: %w", err)
			res.Reason = "could not write the first-run crush.json"
			return res
		}
		res.ConfigAbsent = true
		res.Warnings = append(res.Warnings, Warning{
			Code: "config_rendered",
			Msg:  "rendered a fresh crush.json from your config.toml (first run)",
		})
	}

	// (8) Clean / first-run-rendered path. Coding-mode-OFF is a WARN, never a mutation
	// (D-12 — this core holds no toggle write). The caller surfaces it; the launch
	// still proceeds.
	if !cfg.CodingMode {
		res.Warnings = append(res.Warnings, Warning{
			Code: "coding_mode_off",
			Msg:  "coding mode is OFF — the running stack may not be serving the coder model; run `villa coding-mode enter` to flip it. Launching against the current endpoint anyway.",
		})
	}

	// Build the lockdown env (D-11) in the pure core (unit-testable) and hand it back
	// as ReadyToLaunch — the caller prints Warnings, THEN performs the single
	// d.Launch(LaunchEnv) exec (the single launch point, T-26-09/T-26-10). The core
	// does NOT exec here: doing so replaces the process before the command tier can
	// surface the coding-mode-off / first-run-rendered / lsp-missing Warnings, and
	// cores never print or exec (architecture invariant). On a normal exec the caller's
	// d.Launch never returns; a returned error there is a launch failure it maps.
	res.LaunchEnv = lockdownEnv()
	res.ReadyToLaunch = true
	return res
}

// lockdownEnv builds the belt-and-braces launch env (D-11): the three constant
// telemetry/autoupdate kill switches APPENDED to the inherited process env so Crush
// still sees PATH/HOME/etc. The values are constant literals — nothing user-controlled
// is interpolated (T-26-12). Extracted from Run so it is independently unit-testable.
func lockdownEnv() []string {
	base := osEnviron()
	return append(base,
		envCrushDisableMetrics,
		envDoNotTrack,
		envCrushDisableAutoUpdate,
	)
}

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
	// ReadyToLaunch is true when the presence+drift check was clean (and any
	// first-run render completed): the caller should print Warnings, then exec via
	// the SINGLE launch point d.Launch(LaunchEnv). The core does NOT exec itself —
	// the exec is the caller's final action so advisory Warnings (coding-mode-off,
	// first-run-rendered, lsp-missing) are surfaced BEFORE the process is replaced
	// (D-12 surfacing; cores never print or exec — that is the command tier's job).
	ReadyToLaunch bool
	// LaunchEnv is the resolved belt-and-braces lockdown env (D-11) the caller passes
	// to d.Launch. Populated only when ReadyToLaunch is true.
	LaunchEnv []string
	// Reason is the human refusal/remediation explanation (empty on a clean launch).
	Reason string
	// Warnings carries non-blocking signals (LSP-missing, coding-mode-off) for the
	// caller to print without blocking the launch (D-10/D-12).
	Warnings []Warning
	// Err is a non-refusal failure (config load / render / write / hash).
	Err error
}
