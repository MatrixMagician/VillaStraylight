---
phase: 26-agent-delivery-core-lockdown-launcher
plan: 02
subsystem: cli
tags: [crush, agent, villa-code, launcher, syscall-exec, env-lockdown, tar-extract, sha256, drift-detection, first-run-render]

# Dependency graph
requires:
  - phase: 26-agent-delivery-core-lockdown-launcher
    plan: 01
    provides: "the pure internal/agent core (Deps/Result, Render, DetectDrift, VerifyTarball, loadCrushPolicy) this plan wires to the host"
  - phase: 25-coding-mode-render-transactional-swap-verb
    provides: "the no-auto-flip structural guard (TestNoAutoFlipStructuralGuard) `villa code` honors; the codingmode live-wiring shape liveAgentDeps clones"
provides:
  - "agent.Run(Deps) Result — the pre-launch orchestration: load -> LSP probe -> Render -> DetectDrift -> branch (absent / first-run-render / drift / clean), building the D-11 lockdown env and calling the single d.Launch point"
  - "internal/agent/install.go Install(asset, reader, binDir) — checksum-BEFORE-extract install seam (stream -> VerifyTarball -> stdlib tar extract of ONLY the crush entry, traversal-guarded)"
  - "cmd/villa/code.go `villa code` launcher verb + liveAgentDeps host wiring + XDG path helpers (agentBinPath / crushConfigPath)"
affects: [26-03-on-hardware-binary-pin, 27-install-addon, 28-status-dashboard-drift-surfacing]

# Tech tracking
tech-stack:
  added: []  # zero new Go module dependencies — stdlib only (archive/tar, compress/gzip, crypto/sha256, syscall)
  patterns:
    - "pure-core builds the env + decides; injected Launch executes (codingmode-style single launch point)"
    - "checksum-before-extract: buffer the size-bounded tarball, VerifyTarball, THEN stdlib tar extract ONLY the crush entry, assertInsideBinDir-confined"
    - "first-run config-ABSENT is the only auto-write path (WriteConfig the rendered reference, then launch) — present-but-differs drift never auto-corrected"
    - "explicit villa-owned exec via syscall.Exec(agentBinPath()) — never exec.LookPath for the binary (PATH-hijack-proof, D-05)"
    - "osEnviron package-var seam so lockdownEnv is unit-testable (append the three kill switches to the inherited env)"

key-files:
  created:
    - internal/agent/install.go
    - internal/agent/install_test.go
    - cmd/villa/code.go
    - cmd/villa/code_test.go
  modified:
    - internal/agent/agent.go
    - cmd/villa/root.go
    - cmd/villa/coding-mode_test.go

key-decisions:
  - "lockdownEnv lives in the pure core (agent.go) and APPENDS the three D-11 vars (CRUSH_DISABLE_METRICS=1, DO_NOT_TRACK=1, CRUSH_DISABLE_PROVIDER_AUTO_UPDATE=1) to the inherited process env via an osEnviron seam, so Crush still sees PATH/HOME and the lockdown is independently unit-testable."
  - "agentBinPath() = agentBinDir()/crush where agentBinDir() clones recall.storeRootDir's fallback chain: $XDG_DATA_HOME/villa/bin -> ~/.local/share/villa/bin -> /var/tmp/villa/bin. syscall.Exec targets EXACTLY this path (D-05) with argv[0]=base(bin), no shell."
  - "crushConfigPath() = os.UserConfigDir()/crush/crush.json — the GLOBAL Crush config (NOT under the villa config dir) so the rendered reference lands where Crush itself reads it."
  - "ReadConfig returns (bytes, configPresent, err): os.IsNotExist -> (nil,false,nil) is the first-run trigger DetectDrift uses to distinguish ABSENT from DRIFT; a real read error surfaces as Err."
  - "Install signature: Install(asset CrushAsset, r io.Reader, binDir string) (binPath, error). It LimitReader-bounds the read to asset.Size+1 (oversize detected as a size mismatch without unbounded memory), buffers, VerifyTarball, then extracts."
  - "targetPlatformKey is a fixed 'linux/amd64' constant (NOT a compile-time platform branch) to keep agent.go seam-clean; policy.Assets[targetPlatformKey].BinarySHA256 feeds DriftInput.PolicyBinSHA."

patterns-established:
  - "code.go is backend-marker-free AND coding-flag-literal-free: it reads cfg.CodingMode (never assigns it) so TestNoAutoFlipStructuralGuard + TestSeamGrepGate (both walk cmd/villa) stay green. The doc comment was reworded to avoid the guard's boolean-literal regex matching prose."
  - "the stale Phase-25 reservation guard (`villa code` must NOT exist) was updated to the surviving invariant: code is a root-level SIBLING of coding-mode, never a coding-mode subcommand (D-06)."

requirements-completed: [AGENT-01, AGENT-03, AGENT-04]

# Metrics
duration: ~8min
completed: 2026-06-13
---

# Phase 26 Plan 02: Agent Launcher + Lockdown + Install Seam Summary

**Wired the pure `internal/agent` core to the host: `agent.Run` orchestrates the `villa code` pre-launch flow (load -> LSP probe -> render -> drift-check -> first-run render-then-launch / drift-surface / clean), builds the belt-and-braces telemetry/autoupdate lockdown env and calls the single injected `Launch` (syscall.Exec of the EXPLICIT villa-owned binary); plus a checksum-before-extract `Install` seam (stdlib tar, traversal-guarded) and the `villa code` cobra verb — all off-hardware testable, both structural guards green.**

## Performance
- **Duration:** ~8 min
- **Tasks:** 2 (Task 1 TDD)
- **Files created:** 4 / **modified:** 3

## Accomplishments
- **AGENT-01 (live half):** `Install` streams the asset (size-bounded), calls `VerifyTarball` (size THEN SHA-256) BEFORE any extraction, then extracts ONLY the `crush` regular-file entry into the villa bin dir via stdlib `archive/tar`+`compress/gzip`, confined by `assertInsideBinDir` (rejects `..`/absolute entries). Never shells `tar`; never places unverified bytes (T-26-07/T-26-08).
- **AGENT-03:** `villa code` registered as a root-level NoArgs sibling of `coding-mode`. `agent.Run` builds `lockdownEnv()` (the three D-11 vars appended to the inherited env) and calls `d.Launch(env)` — the single launch point; `liveAgentDeps().Launch` execs the EXPLICIT `agentBinPath()` via `syscall.Exec` (D-05, no PATH lookup, no shell — T-26-09/T-26-12). Coding-mode-OFF is a stderr WARN pointing at `villa coding-mode enter` that STILL launches (D-12); no `CodingMode` literal in code.go (TestNoAutoFlipStructuralGuard green).
- **AGENT-04 (live half):** present-but-differs binary OR config drift is surfaced with remediation and EXITs (`exitBlocked`) — never auto-corrected; the config-ABSENT first run renders the reference via `WriteConfig` (the ONLY auto-write path) then launches, never mis-reported as drift; `BinaryDriftUnknown` (unpinned sentinel) degrades to a WARN, never a block (Pitfall 6).
- `make check` green across 23 packages; `TestSeamGrepGate` + `TestNoAutoFlipStructuralGuard` green; `villa code --help` lists the verb after build; zero new Go module dependencies.

## Task Commits
1. **Task 1: agent.Run flow + first-run render + checksum-before-extract install seam (TDD)** — `a00f4e6` (feat)
2. **Task 2: `villa code` thin caller + liveAgentDeps + registration** — `aa64196` (feat)

## Files Created/Modified
- `internal/agent/install.go` — `Install` (stream->verify->tar-extract-only-crush, traversal-guarded), `extractCrushBinary`, `assertInsideBinDir`, `sha256Hex`
- `internal/agent/install_test.go` — `TestRun*` (binary-absent / first-run-render / drift-surfaced / clean-lockdown-env / coding-off) + `TestInstallVerifyBeforeExtract` (checksum-mismatch-refuses / only-crush / traversal-rejected / size-mismatch)
- `internal/agent/agent.go` — added `Run(Deps) Result`, `lockdownEnv()`, the three lockdown env consts, `targetPlatformKey`, `knownLSPServers`, and the `osEnviron` seam var
- `cmd/villa/code.go` — `newCode`, `runCode` (Result->exit mapping), `liveAgentDeps`, `crushConfigPath`, `agentBinDir`, `agentBinPath`, `hashFileSHA256`, `assertWithinDir`
- `cmd/villa/code_test.go` — `TestCode*` (lockdown-env / first-run-renders / binary-absent / drift-surfaced / coding-mode-off / registered) driven via fake `agent.Deps`
- `cmd/villa/root.go` — registered `newCode()`
- `cmd/villa/coding-mode_test.go` — updated the stale Phase-25 `villa code`-reservation guard (see Deviations)

## Decisions Made
See `key-decisions` frontmatter. Most load-bearing: the lockdown env is built in the pure core and APPENDED to the inherited env via the `osEnviron` seam; `syscall.Exec` targets the EXPLICIT `agentBinPath()` ($XDG_DATA_HOME/villa/bin/crush + fallbacks); `crushConfigPath()` is the global `~/.config/crush/crush.json`; `ReadConfig`'s `(bytes, configPresent, err)` triple is what distinguishes first-run ABSENT from DRIFT.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated the stale Phase-25 `villa code` reservation guard**
- **Found during:** Task 2 (`make check`)
- **Issue:** `TestCodingModeNounRegistered` (cmd/villa/coding-mode_test.go) asserted `villa code` must NOT exist on the root tree — a Phase-25 reservation that this plan (which ships `villa code`) necessarily violates.
- **Fix:** Replaced the "must not exist" assertion with the surviving invariant — `villa code` is a root-level SIBLING of `coding-mode`, never a `coding-mode` subcommand (D-06 separation). The `TestCodeRegistered` test in code_test.go is the positive registration assertion.
- **Files modified:** cmd/villa/coding-mode_test.go
- **Commit:** `aa64196`

**2. [Rule 3 - Blocking] Reworded two doc comments to avoid the structural-guard regex**
- **Found during:** Task 1 (seam gate) and Task 2 (no-auto-flip guard)
- **Issue:** (a) agent.go's comment text "runtime.GOOS branch" tripped `TestSeamGrepGate`'s `runtime\.GOOS` regex; (b) code.go's comment literally containing the `CodingMode = true|false` form tripped `TestNoAutoFlipStructuralGuard`'s boolean-literal regex — both in PROSE, not code.
- **Fix:** Reworded both comments to describe the constraint without the matched token sequence (no behavior change).
- **Files modified:** internal/agent/agent.go, cmd/villa/code.go
- **Commits:** `a00f4e6`, `aa64196`

## Known Stubs
None. The `permissions` block remains intentionally omitted from the rendered crush.json (default-prompt; the restrictive allowlist is the Phase-27 STRIDE pass, Open-Q3 — documented in Plan 01). This is not a stub that blocks the plan goal.

## Threat Flags
None — no new security surface beyond the plan's `<threat_model>`. The install seam, explicit exec, lockdown env, drift-surface-no-auto-correct, and no-auto-flip are all mitigations enumerated in the plan register (T-26-07..T-26-13).

## Next Phase Readiness
- **Plan 03 (on-hardware pin):** MUST replace the `binarySha256` sentinel in `internal/agent/crush-policy.json` with the real extracted-binary SHA-256 (extract the verified tarball once via `Install` -> `sha256sum crush`), then confirm `DetectDrift` reports a confident clean/drift (not `BinaryDriftUnknown`) for the correctly-installed binary, AND run a real `villa code` launch smoke (the syscall.Exec path is exercised only on-hardware; tests cover the seam, not the exec itself).
- **Plan 27 (install addon):** wire `agent.Install` into a `villa install` agent addon (download the pinned asset from `policy.URLTmpl`, hand the reader to `Install`).
- **No blockers.**

## Self-Check: PASSED

All 4 created files exist on disk; both task commits (`a00f4e6`, `aa64196`) exist in git history. `go test ./internal/agent/` + `go test ./cmd/villa/ -run TestCode`, `TestNoAutoFlipStructuralGuard`, and `TestSeamGrepGate` all green; `make check` green across 23 packages.

---
*Phase: 26-agent-delivery-core-lockdown-launcher*
*Completed: 2026-06-13*
