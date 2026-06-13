---
phase: 26-agent-delivery-core-lockdown-launcher
verified: 2026-06-13T00:00:00Z
status: passed
score: 4/4 must-haves verified
overrides_applied: 0
re_verification:
  previous_status: none
  previous_score: n/a
---

# Phase 26: Agent Delivery Core & Lockdown Launcher Verification Report

**Phase Goal:** villa installs a pinned, checksum-verified Crush binary and renders its config as a derived artifact of `config.toml` — kill switches set, loopback-only provider, launched through a locked-down `villa code` verb, with drift detected and never auto-corrected.
**Verified:** 2026-06-13
**Status:** passed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| # | Truth (Success Criterion) | Status | Evidence |
|---|---------------------------|--------|----------|
| 1 | villa installs the pinned Crush release (go:embed pin policy: version + per-platform asset + SHA-256, rocm-policy pattern); checksum verified BEFORE install — mismatch refuses with remediation — autoupdate forced off | ✓ VERIFIED | `crush-policy.json` carries `version v0.76.0`, `linux/amd64` asset `name`/`sha256`/`size`/`binarySha256`, `urlTemplate`. `policy.go:20` `//go:embed crush-policy.json`; `loadCrushPolicy` panics build-time on malformed embed (T-26-05). `VerifyTarball` (`policy.go:88`) asserts size THEN SHA-256 (`EqualFold`), refuse-with-remediation on mismatch. `Install` (`install.go:52`) buffers bounded-by-size, calls `VerifyTarball` BEFORE `extractCrushBinary` (`install.go:65-73`), extracts ONLY the base-name `crush` entry, traversal-guarded (`assertInsideBinDir`). Real binary hash `4fd811f6…8342b4` pinned (NOT sentinel; `policy.json:8`). Autoupdate off: `disable_provider_auto_update:true` in render + `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1` env. Tests: `TestChecksumGate`, `TestInstallVerifyBeforeExtract` (mismatch → no binary placed; size-mismatch refuse-before-extract; traversal rejected; clean extract-only-crush), `TestPolicyLoad` asserts sentinel replaced. |
| 2 | crush.json rendered as derived artifact of config.toml: both kill switches, exactly one loopback villa provider, villa-unique model id (#2649), LSP detected-only → WARN not BLOCK | ✓ VERIFIED | `Render` (`render.go:136`) deterministic (`MarshalIndent` + fixed trailing newline). Golden (`testdata/crush.json.golden`): `disable_metrics:true`, `disable_provider_auto_update:true`, `disable_default_providers:true`, `auto_lsp:false`; exactly one `villa` provider `type:openai-compat` at `http://127.0.0.1:8080/v1`; model id `villa-qwen3-coder-30b-a3b` (modelIDPrefix `villa-`, D-09); `lsp.go.command=gopls`. `renderLSP` (`render.go:191`) emits entry per FOUND probe, WARN+omit per not-found — never BLOCK. Tests: `TestRenderGolden`, `TestRenderContract`, `TestLSPMissingWarn`, `TestRenderNoMetachars`. Byte-determinism backs config-drift compare (`canonicalize`/`semanticallyEqualConfig`). |
| 3 | User launches via `villa code`, applying belt-and-braces env lockdown (CRUSH_DISABLE_METRICS=1, DO_NOT_TRACK=1) before exec | ✓ VERIFIED | `lockdownEnv` (`agent.go:229`) appends all THREE vars (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`, `CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1`) to inherited env. `Run` resolves `LaunchEnv`+`ReadyToLaunch` and does NOT exec; `runCode` (`code.go:74`) prints Warnings THEN calls the single `d.Launch` (the 26-03 D-12 fix — WARN-before-exec). `liveAgentDeps.Launch` (`code.go:181`) `syscall.Exec` of the EXPLICIT `agentBinPath()` (no PATH lookup, D-05). `newCode()` registered in `root.go:36`. Tests: `TestCodeLockdownEnv` (env carries all three), `TestCodeFirstRunRenders`, `TestCodeCodingModeOffWarns` (WARN + still launches), `TestCodeRegistered`, `TestRunCodingModeOffWarns`. |
| 4 | Drift of agent binary or rendered config detected + surfaced with remediation — never silently auto-corrected | ✓ VERIFIED | `DetectDrift` (`drift.go:80`) is report-only (no write/repair path), distinguishing: BinaryAbsent → Phase-27 install remediation; BinaryDrift (confident, hash pinned) → refuse; BinaryDriftUnknown (sentinel) → typed-Unknown WARN; ConfigAbsent → first-run render (NOT drift); ConfigDrift → refuse-no-overwrite. `Run` (`agent.go:147-201`) routes: binary-drift/config-drift return WITHOUT launching/writing; config-absent is the ONLY auto-write path. `runCode` surfaces all with remediation. Tests: `TestBinaryDrift` (mismatch confident, equal clean, sentinel→Unknown), `TestConfigDrift`, `TestConfigAbsent`, `TestCodeDriftSurfaced` (Launch+WriteConfig NOT called), `TestRunDriftSurfaced`. On-hardware (26-03): hand-edit negative control refused with no auto-correct (sha unchanged). |

**Score:** 4/4 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
|----------|----------|--------|---------|
| `internal/agent/crush-policy.json` | go:embed pin policy (version/asset/sha256/size/binarySha256/url) | ✓ VERIFIED | Real binary hash pinned, not sentinel |
| `internal/agent/policy.go` | embed loader + VerifyTarball gate | ✓ VERIFIED | size-then-SHA-256, fail-closed, refuse-with-remediation |
| `internal/agent/install.go` | checksum-before-extract install seam | ✓ VERIFIED | VerifyTarball before extract; stdlib tar; traversal guard; extract-only-crush |
| `internal/agent/render.go` | deterministic crush.json renderer | ✓ VERIFIED | both kill switches + disable_default_providers; one loopback provider; villa- id; LSP WARN |
| `internal/agent/drift.go` | report-only drift detector | ✓ VERIFIED | 4-way signal split; no auto-correct path |
| `internal/agent/agent.go` | pure Run orchestration + lockdownEnv + Deps/Result | ✓ VERIFIED | Run resolves env, does not exec (D-12 fix); caller launches |
| `internal/agent/testdata/crush.json.golden` | determinism golden | ✓ VERIFIED | Matches render contract; no shell metachars |
| `cmd/villa/code.go` | live wiring + Result→exit mapping | ✓ VERIFIED | WARN-before-launch; explicit villa-owned exec; no CodingMode literal |
| `cmd/villa/root.go` | `villa code` registration | ✓ VERIFIED | `newCode()` in AddCommand (line 36) |

### Key Link Verification

| From | To | Via | Status | Details |
|------|----|----|--------|---------|
| `code.go` runCode | `agent.Run` | `agent.Run(*d)` | ✓ WIRED | Result mapped to exit + messages |
| `agent.Run` | `Render` + `DetectDrift` | direct calls | ✓ WIRED | reference bytes feed both drift-compare + first-run write |
| `liveAgentDeps.Launch` | villa-owned binary | `syscall.Exec(agentBinPath(), …)` | ✓ WIRED | explicit path, no PATH hijack |
| `crush.json` provider | loopback inference | `base_url http://127.0.0.1:8080/v1` | ✓ WIRED | loopback-only literal (not a backend marker) |
| `Install` | `VerifyTarball` | call before `extractCrushBinary` | ✓ WIRED | checksum-before-extract proven by test |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
|----------|---------|--------|--------|
| Full suite green | `make check` (vet + go test ./...) | all packages ok / cached; cmd/villa 3.581s, internal/agent ok | ✓ PASS |
| Seam grep gate (no marker leak in agent/code.go) | `go test ./internal/inference -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| No-auto-flip structural guard | `go test ./cmd/villa -run TestNoAutoFlipStructuralGuard` | 1 passed | ✓ PASS |
| Rendered config has no shell metachars | grep `$(` / backtick / `${` in golden | none found | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
|-------------|-------------|-------------|--------|----------|
| AGENT-01 | 26-01/02/03 | pinned go:embed policy, SHA-256 verified before install, autoupdate off | ✓ SATISFIED | policy.go + install.go + real binary hash pinned (26-03) |
| AGENT-02 | 26-01 | crush.json derived: kill switches, one loopback provider, villa- id, LSP WARN | ✓ SATISFIED | render.go + golden + on-hardware first-run render |
| AGENT-03 | 26-02/03 | villa code launcher, belt-and-braces env lockdown | ✓ SATISFIED | lockdownEnv + WARN-before-exec D-12 fix |
| AGENT-04 | 26-01/02/03 | drift detected + surfaced, never auto-corrected | ✓ SATISFIED | drift.go report-only + confident binary signal + on-hardware negative control |

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
|------|------|---------|----------|--------|
| (none) | — | No TBD/FIXME/XXX/HACK/PLACEHOLDER in `internal/agent/*.go` or `cmd/villa/code.go` | ℹ️ Info | Clean |

### Honesty Review (26-03 caveats)

- **ROCm backend at acceptance:** Honest scoping note. Launcher/render/drift are backend-agnostic (point at loopback endpoint + model id); does not affect AGENT-01..04. Not a masked failure.
- **TUI via non-interactive `crush run`:** Honest. Model round-trip + lockdown env + provider wiring proven; interactive TUI requires a TTY (expected). Not a masked failure.
- **D-12 defect found+fixed on-hardware:** Genuine — the WARN-before-exec gap was a real test-vs-reality finding, fixed (`Run` no longer execs; caller prints then launches) and re-verified. Reflected in current code.
- **Deferred scope confirmed genuinely out of Phase 26's 4 criteria:** install addon + preflight + `villa verify agent` egress proof → Phase 27; status/dashboard/doctor/backup surfacing → Phase 28. None of these are required by the Phase 26 success criteria.

### Human Verification Required

None. The on-hardware interactive TUI launch is a Phase-26 caveat already exercised via non-interactive `crush run` (model round-trip + lockdown + provider wiring proven); the remaining TTY-attach behavior is standard Crush behavior, not a villa contract. No outstanding human-only checks block the phase goal.

### Gaps Summary

No gaps. All four success criteria are achieved with concrete code, test, and on-hardware evidence. The pin policy carries the real extracted-binary SHA-256 (sentinel replaced), checksum is verified before extraction (fail-closed, refuse-with-remediation), crush.json is a deterministic derived artifact with both kill switches and a single loopback provider, `villa code` applies all three lockdown env vars and surfaces WARNs before the single exec, and drift is detected report-only with no auto-correct path. `make check`, `TestSeamGrepGate`, and `TestNoAutoFlipStructuralGuard` are all green.

---

_Verified: 2026-06-13_
_Verifier: Claude (gsd-verifier)_
