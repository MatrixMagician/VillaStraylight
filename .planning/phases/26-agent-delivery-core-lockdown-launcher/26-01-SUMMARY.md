---
phase: 26-agent-delivery-core-lockdown-launcher
plan: 01
subsystem: infra
tags: [crush, agent, go-embed, sha256, crush-json, drift-detection, lsp, openai-compat, pure-core]

# Dependency graph
requires:
  - phase: 25-coding-mode-render-transactional-swap-verb
    provides: "the entered coding-mode loopback endpoint Crush points at; the no-auto-flip guard `villa code` (Plan 02) honors; the codingmode Deps/Result pure-core shape this clones"
provides:
  - "internal/agent pure core (policy/render/drift/version + Deps/Result) — the off-hardware spine of the agent-delivery feature"
  - "go:embed crush-policy.json pin policy (v0.76.0, linux/amd64 tarball SHA-256 + size + binarySha256 sentinel) + pure VerifyTarball install gate (D-03)"
  - "Render(cfg, probes) -> deterministic crush.json bytes + LSP warnings (both kill switches, one loopback openai-compat provider, villa- prefixed non-empty models[], detected-only LSP)"
  - "DetectDrift(DriftInput) -> DriftReport: report-only binary + config drift + config-absent first-run signal, never auto-corrects"
  - "internal/agent/testdata/crush.json.golden — render-determinism fixture (NEW append-only)"
affects: [26-02-launcher-verb, 26-03-on-hardware-binary-pin, 27-install-addon, 28-status-dashboard-drift-surfacing]

# Tech tracking
tech-stack:
  added: []  # zero new Go module dependencies — stdlib only (encoding/json, crypto/sha256, encoding/hex)
  patterns:
    - "go:embed policy JSON + panic-on-malformed loader (cloned from internal/preflight/floors.go)"
    - "pure-core + injected Deps func-field seam + typed Result (cloned from internal/codingmode shape, not its state machine)"
    - "checksum-before-place fail-closed verify gate (cloned from internal/download EqualFold discipline)"
    - "deterministic JSON render (ordered structs, fixed indent + trailing newline) golden-frozen for drift compare"
    - "parsed-semantic config drift compare (canonicalize -> bytes.Equal) tolerating whitespace-only re-saves"

key-files:
  created:
    - internal/agent/policy.go
    - internal/agent/crush-policy.json
    - internal/agent/version.go
    - internal/agent/agent.go
    - internal/agent/render.go
    - internal/agent/drift.go
    - internal/agent/agent_test.go
    - internal/agent/testdata/crush.json.golden
  modified: []

key-decisions:
  - "Locked Open-Q1 option (ii): render-only. base_url is the FIXED loopback literal http://127.0.0.1:8080/v1 (serverPort=8080 constant, not a config field); the served model id is matched via llama.cpp single-model leniency, NOT a --jinja/--alias delta into the inference seam."
  - "Locked Open-Q4 the parsed-semantic way: config-drift compares canonicalized JSON (unmarshal -> re-marshal -> bytes.Equal), so a whitespace-only re-save is NOT drift while a semantic edit IS (Pitfall 4)."
  - "villa- model-id prefix; served base id = cfg.CoderModel when set else cfg.Model (e.g. villa-qwen3-coder-30b-a3b). #2649-safe (cannot collide with a Catwalk built-in)."
  - "binarySha256 sentinel = UNPINNED-binary-sha256-set-by-26-03-on-hardware — Plan 03 replaces it with the extracted-binary SHA-256; while sentinel/empty, binary drift degrades to a typed-Unknown WARN (BinaryDriftUnknown), never a false FAIL (Pitfall 6)."
  - "config-ABSENT (ConfigPresent=false) is a DISTINCT first-run render trigger (ConfigAbsent), never compared against the rendered reference and never reported as drift; it parallels BinaryAbsent."
  - "Rendered options also set disable_default_providers=true + auto_lsp=false (Pitfall 2/5) to strengthen cloud-fallback prevention and keep the lsp block authoritative for determinism."

patterns-established:
  - "internal/agent seam discipline: imports NEITHER internal/inference NOR internal/detect; holds only a loopback URL + a villa- prefix as non-config literals (no backend markers) — TestSeamGrepGate green."
  - "Report-only drift core (D-14): DetectDrift has no Deps/io args and no write/repair path anywhere — present-but-differs is surfaced, never silently rewritten."

requirements-completed: [AGENT-01, AGENT-02, AGENT-04]

# Metrics
duration: ~22min
completed: 2026-06-13
---

# Phase 26 Plan 01: Agent Delivery Core Summary

**Pure `internal/agent` core — a `go:embed` Crush v0.76.0 pin policy + fail-closed checksum gate, a byte-deterministic golden-frozen `crush.json` renderer (both kill switches, one loopback openai-compat provider, `villa-` prefixed models), and a report-only binary/config drift detector — all clones of shipped first-party analogs with zero new dependencies.**

## Performance

- **Duration:** ~22 min
- **Tasks:** 3 (all TDD)
- **Files created:** 8
- **Files modified:** 0

## Accomplishments
- **AGENT-01 (pure half):** `crush-policy.json` embeds + decodes (v0.76.0, linux/amd64 tarball SHA-256 `0f66…1ec9`, size 25155696, binary-hash sentinel); `VerifyTarball` is fail-closed on size OR checksum mismatch with refuse-with-remediation (D-03). `compareVersions`/`splitVersion` cloned verbatim from `floors.go`.
- **AGENT-02:** `Render(cfg, probes)` produces byte-deterministic `crush.json` (matches the new golden) with `disable_metrics`+`disable_provider_auto_update` (D-07) plus `disable_default_providers`+`auto_lsp=false`, exactly one `openai-compat` provider at `http://127.0.0.1:8080/v1`, a non-empty `villa-`-prefixed `models[]` (D-08/D-09, Pitfall 3), and detected-only LSP entries (missing server → WARN + omit, never BLOCK, D-10). No `$()`/backtick/`${` in any rendered value (Pitfall 1).
- **AGENT-04 (pure half):** `DetectDrift` flags binary drift (against the BINARY hash, not the tarball checksum — Pitfall 6) and config drift (parsed-semantic compare) with remediation, distinguishes config-ABSENT (first-run render trigger) from config-DRIFT, degrades the unpinned binary hash to a typed-Unknown WARN, and has NO auto-correct path (D-14).
- `internal/agent` holds zero backend markers — `TestSeamGrepGate` green; `make check` green (no regression across 20 packages); zero new Go module dependencies.

## Task Commits

Each task was committed atomically (TDD test+impl landed together per task):

1. **Task 1: Pin policy + version comparator + Deps/Result skeleton (AGENT-01)** - `fb77ed4` (feat)
2. **Task 2: Deterministic crush.json renderer + LSP WARN + golden (AGENT-02)** - `a4249b7` (feat)
3. **Task 3: Pure drift detector (AGENT-04)** - `4858fb8` (feat)

## Files Created/Modified
- `internal/agent/crush-policy.json` - go:embed pin: version + linux/amd64 asset (name/sha256/size/binarySha256) + url template
- `internal/agent/policy.go` - `CrushPolicy`/`CrushAsset` types, `loadCrushPolicy` (panic-on-malformed), pure `VerifyTarball` gate, `unpinnedBinarySentinel`
- `internal/agent/version.go` - verbatim clone of `compareVersions`/`splitVersion`
- `internal/agent/agent.go` - package doc (seam discipline), `Deps` func-field seam, typed `Result` (BinaryAbsent/BinaryDrift/ConfigAbsent/ConfigDrift), `Warning`
- `internal/agent/render.go` - `Render(cfg, probes)` deterministic crush.json + `LSPProbe`/`Warning`, `servedModelID`, `canonicalize` (semantic drift helper)
- `internal/agent/drift.go` - `DetectDrift(DriftInput) DriftReport` pure report-only comparator
- `internal/agent/agent_test.go` - table-driven suite (policy/checksum/version/render/golden/LSP/metachar/binary-drift/config-absent/config-drift)
- `internal/agent/testdata/crush.json.golden` - NEW append-only render-determinism fixture

## Decisions Made
See `key-decisions` frontmatter. Most load-bearing for downstream plans:
- **Endpoint/model-id (Open-Q1 → option ii, render-only):** `base_url` = fixed `http://127.0.0.1:8080/v1`; the model id is `villa-` + (`cfg.CoderModel` else `cfg.Model`). Phase 26 stays render-only — NO `--alias` delta into the `internal/inference` seam; it relies on llama.cpp single-model leniency.
- **Binary-hash sentinel (Open-Q2):** `binarySha256` = `UNPINNED-binary-sha256-set-by-26-03-on-hardware`. **Plan 03 (26-03) MUST replace this on-hardware** (extract the verified tarball once → `sha256sum crush`). Until then drift reports `BinaryDriftUnknown` (typed-Unknown WARN), never a false drift.
- **Config-drift primitive (Open-Q4 → parsed-semantic):** `canonicalize` (unmarshal → re-marshal) then `bytes.Equal`; whitespace-only re-saves are not drift.
- **Config-absent signalling:** `DriftReport.ConfigAbsent` (distinct from `ConfigDrift`) is the first-run render trigger — the caller (Plan 02) renders-then-launches; it never compares an absent config against the rendered reference.
- **permissions (Open-Q3):** rendered omitted in Phase 26 (default-prompt); the restrictive allowlist is the Phase-27 STRIDE pass — no allow-all surface.

## Deviations from Plan

None - plan executed exactly as written. (The only non-test-driven change was `go fmt` reformatting whitespace/alignment in `render.go` + `agent_test.go`, captured within the Task 2/Task 3 commits; no behavior change.)

## Issues Encountered
- `gofmt` is not on PATH in this environment; used `go fmt ./internal/agent/` instead. No impact.

## User Setup Required
None - no external service configuration required. (The actual Crush binary download/install is the Phase-27 addon; this plan ships only the pure verify gate + render + drift core.)

## Next Phase Readiness
- **Plan 02 (launcher verb):** the `Deps`/`Result` contract, `Render`, `DetectDrift`, `VerifyTarball`, and `loadCrushPolicy` are all in place to wire `cmd/villa/code.go` + `liveAgentDeps()`. Plan 02 implements `agent.Run()` consuming these types and the live host seams (LookPath/ReadConfig/HashBinary/WriteConfig/Launch).
- **Plan 03 (on-hardware pin):** must replace the `binarySha256` sentinel in `crush-policy.json` with the real extracted-binary SHA-256, then re-confirm `DetectDrift` reports a confident clean/drift (not `BinaryDriftUnknown`) for a correctly-installed binary.
- **No blockers.**

## Self-Check: PASSED

All 8 created files exist on disk; all 3 task commits (`fb77ed4`, `a4249b7`, `4858fb8`) exist in git history. `go test ./internal/agent/` (22 tests) green, `TestSeamGrepGate` green, `make check` green.

---
*Phase: 26-agent-delivery-core-lockdown-launcher*
*Completed: 2026-06-13*
