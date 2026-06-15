---
phase: 28-agent-surfacing-contracts
plan: 03
subsystem: status read-model + dashboard
tags: [status, dashboard, agent, coding, schema-bump, residency, cache-effectiveness, typed-unknown, SURF-01, USAGE-03, USAGE-04]
requires:
  - "internal/status (Memory *MemoryInfo pointer-block tail-append + memoryInfo() typed-Unknown precedent)"
  - "internal/recommend (CoderFit.Residency + ResidencySwap/ResidencyShared constants)"
  - "internal/agent (DetectDrift pin-compare + LoadCrushPolicy version/asset pin)"
  - "internal/metrics (28-02 CacheSample + ScrapeCacheCounters cache-counter primitive)"
  - "internal/dashboard/assets (renderMemory hidden-until-data panel + metricRow/mutedP/memoryBadgeRow/groupThousands helpers)"
provides:
  - "status.Report Coding *CodingInfo block (enabled/version/pin_match tri-state/model/mode/residency/cache) + reportSchemaVersion 3→4 (the v1.4 milestone's single contract bump)"
  - "status.codingInfo() populator + AgentPinMatch/AgentResidency/AgentCache nil-safe Deps seams"
  - "cmd/villa live seam wiring: liveAgentPinMatch / liveAgentResidency (recomputed) / liveAgentCache"
  - "status-coding.json.golden (NEW coding-on fixture) + both status goldens refrozen to v4 together"
  - "dashboard renderAgent(report) Agent panel (hidden-until-data, fed by the existing /api/status poll)"
affects:
  - "the phase's ONLY byte-frozen contract change lands here (status 3→4); doctor (schema 2) + backup (schema 3) own separate versions"
tech-stack:
  added: []
  patterns:
    - "Pointer-block tail-append above SchemaVersion (Memory clone) — append-only, single 3→4 bump"
    - "Tri-state typed-Unknown string for pin_match (ROCmReadinessIndicator idiom) — never a bare bool"
    - "Derived-not-persisted residency: recomputed at status time via recommend.Pick(...).Coder.Residency, typed-Unknown when the envelope is unevaluable"
    - "Nil-safe Deps seam → typed-Unknown (ReadUsage/ReadRecallState clone)"
    - "Hidden-until-data dashboard panel fed by the existing poll (renderMemory clone) — no new fetch/endpoint"
    - "Cache pct gated on both-Known + prompt_n>0 — never a fabricated 0%"
key-files:
  created:
    - cmd/villa/testdata/status-coding.json.golden
  modified:
    - internal/status/status.go
    - internal/status/status_test.go
    - cmd/villa/status.go
    - cmd/villa/status_test.go
    - cmd/villa/testdata/status.json.golden
    - cmd/villa/testdata/status-memory.json.golden
    - internal/dashboard/assets/dashboard.html
    - internal/dashboard/assets/dashboard.js
    - internal/dashboard/api_test.go
decisions:
  - "status.Report reportSchemaVersion bumped 3→4 EXACTLY ONCE; coding-off + memory-off + coding-on goldens refrozen together in one -update pass"
  - "pin_match is a tri-state STRING (match/mismatch/unknown), NOT a bare bool — typed-Unknown when the policy/binary compare cannot be made (Claude's-discretion field spelling, locked)"
  - "residency is DERIVED (recommend.Pick(...).Coder.Residency) and recomputed at status time — never read from cfg, omitted (typed-Unknown) when UsableEnvelopeBytes is not Known"
  - "version sourced from agent.LoadCrushPolicy() (a pure embedded-bytes read) inside the status core — no host I/O, no import cycle (agent does not import status)"
  - "mode mapped from cfg.CodingMode bool → \"coding\" when on, else omitted; model = cfg.CoderModel"
metrics:
  duration: "~40 min"
  completed: "2026-06-15"
---

# Phase 28 Plan 03: Agent Surfacing in status + Dashboard (v1.4 finale, single 3→4 contract bump) Summary

The v1.4 finale: surfaced the coding agent in `villa status` (human + `--json`) and the dashboard Agent panel, landing the phase's ONLY byte-frozen contract change — `status.Report` `reportSchemaVersion` 3→4 exactly once — by tail-appending an append-only `Coding *CodingInfo` block above `SchemaVersion` (Memory-block clone) and re-freezing all three status goldens together. Residency is recomputed honestly from the live memory envelope (never persisted, never fabricated), pin match is a tri-state typed-Unknown string, and cache effectiveness shows the `cache_n/prompt_n` ratio only when proven — never a fabricated 0%.

## What was built

### Task 1 — status.Report Coding block + 3→4 single bump + populator + live seam wiring (commit `6f364f6`)
- **`internal/status/status.go`:** `type CodingInfo` (enabled / version,omitempty / pin_match tri-state / model,omitempty / mode,omitempty / residency,omitempty / cache_effectiveness_pct *float64,omitempty / cache_n,omitempty / prompt_n,omitempty) cloned from the `MemoryInfo` sidecar; `Coding *CodingInfo` tail-appended BETWEEN `Memory` and `SchemaVersion` (append-only — nothing above moved); `reportSchemaVersion` 3→4 with a v4 version-history line; `PinMatch`/`PinMismatch`/`PinUnknown` tri-state constants; `codingInfo(cfg, pinMatch, residency, cache)` populator honoring the typed-Unknown discipline (nil pin seam → "unknown"; residency "" → omitted; cache pct only when ok AND prompt_n>0); new nil-safe `AgentPinMatch`/`AgentResidency`/`AgentCache` Deps seams; gated population `if cfg.AgentEnabled { report.Coding = codingInfo(...) }`. Version is sourced from the pure `agent.LoadCrushPolicy()` embedded read (no host I/O, no import cycle).
- **`cmd/villa/status.go`:** the `if r.Coding != nil` human-table block (version/pin/model/mode/residency rows — residency OMITTED when "" — plus per-model coder usage selected from `r.Usage.Models[r.Coding.Model]` and the cache-effectiveness row, pct+raw or "unavailable"); `liveStatusDeps` now assigns to a `deps` var and wires the three seams gated on `cfg.AgentEnabled`. `liveAgentPinMatch` runs the pure `agent.DetectDrift` binary signal → tri-state; `liveAgentResidency` clones `liveWeightBytes` (catalog.Load + detect.Probe) and returns `recommend.Pick(...).Coder.Residency` (the `ResidencySwap`/`ResidencyShared` CONSTANT) ONLY when `UsableEnvelopeBytes.Known`, else "" ; `liveAgentCache` reuses `metrics.ScrapeCacheCounters` (no new HTTP request).
- **Goldens:** `status.json.golden` (coding-off) + `status-memory.json.golden` refrozen to v4; NEW `status-coding.json.golden` carries the full coding block. All three refrozen together with one `-update` pass.
- **Tests:** new `TestRunCodingOffReport` / `TestRunCodingSection` / `TestRunCodingPinTriState` / `TestRunCodingResidencyTypedUnknown` / `TestRunCodingCacheGate` (status core) + `TestStatusJSONGoldenCodingOn` (cmd golden). Two existing schema==3 assertions updated to 4 (the single bump).

### Task 2 — Dashboard Agent panel (renderAgent, hidden-until-data) (commit `5486de6`)
- **`internal/dashboard/assets/dashboard.html`:** `#agent-panel` static shell (ships `hidden`), mirroring `#memory-panel` chrome verbatim (`agent-heading` "Agent" / `agent-body`).
- **`internal/dashboard/assets/dashboard.js`:** `agentPanel`/`agentBody` refs; `renderAgent(report)` cloning renderMemory's hidden-until-data gate (`var ag = report && report.coding; if (!ag) { ...hidden = true; return; }`); version/model/mode/residency rows (omitted when absent); tri-state policy-pin badge (match→ready green, mismatch→warn amber + caption, else gray "unavailable" + caption); per-model coder usage keyed on `report.coding.model` (honest "No usage recorded yet" when absent); cache pct+raw when present, else gray Unknown badge + caption. Wired beside `renderMemory(report)` in the existing `/api/status` `.then` (NOT `.catch`). All values via `textContent` (XSS-safe).
- **NO new CSS** — `.panel`/`.panel-heading`/`.metric-row`/`.badge`(+`-ready`/`-warn`/`-unknown`)/`.muted` reused verbatim per the UI-SPEC.
- **`internal/dashboard/api_test.go`:** the passthrough schema_version assertion 3→4 (single-bump propagation).

## Deviations from Plan

None — plan executed exactly as written. Implementation choices worth recording (all within the plan's `<action>` latitude):
- **Version source:** `CodingInfo.Version` is read from `agent.LoadCrushPolicy().Version` inside the status core. The plan's populator signature (`codingInfo(cfg, pinMatch, residency, cacheSample)`) carries no version arg and `VillaConfig` has no version field; the pinned policy version is the authoritative source and `LoadCrushPolicy()` is a pure embedded-bytes read (no host I/O). Confirmed no import cycle — `internal/agent` does not import `internal/status`.
- **Mode mapping:** `Mode` is derived from `cfg.CodingMode` (bool) → `"coding"` when on, else "" (omitted). `Model` = `cfg.CoderModel`. Both honest cfg-sourced identity.
- **Three pre-existing schema==3 assertions updated to 4** (two in `internal/status/status_test.go`, one in `internal/dashboard/api_test.go`). These are intentional contract-version assertions that move with the single bump — they are NOT golden re-freezes.

## Verification

- `go test ./internal/status/... -count=1` — 55 passed (Coding block + populator + typed-Unknown + 3→4 bump).
- `go test ./cmd/villa/... -run 'TestStatus|TestSeamGrepGate' -count=1` — 31 passed (both golden variants + the new coding fixture + seam gate).
- `go test ./internal/inference/... -run TestSeamGrepGate` — passed (no leaked backend marker literal in status/cmd).
- `go build ./...` — success (embedded dashboard assets compile via go:embed).
- `go test ./internal/dashboard/... -count=1` — 55 passed.
- `make check` (vet + full `go test ./...`) — every package green.
- `make lint` — green (go vet fallback; golangci-lint not installed).
- **Append-only confirmed:** `git diff` of `status.json.golden` (coding-off) vs the committed pre-plan version shows EXACTLY ONE changed line — `schema_version` 3→4. Nothing above the coding key moved.
- **Residency honesty:** recomputed via `recommend.Pick(...).Coder.Residency` (the `ResidencySwap`/`ResidencyShared` constants); the `TestRunCodingResidencyTypedUnknown` empty-return + nil-seam cases prove the key is OMITTED (typed-Unknown) — never a fabricated swap/shared, never read from cfg.
- **Cache honesty:** `TestRunCodingCacheGate` proves the pct is set only when ok AND prompt_n>0; prompt_n==0 / not-ok / nil-seam all leave pct nil and omit the counts — never a fabricated 0%.

## Threat-model coverage

- **T-28-03-01 (XSS):** every renderAgent value set via createElement + textContent (reused from renderMemory) — never innerHTML/HTML interpolation of server strings.
- **T-28-03-02 (attack-surface widening):** NO new endpoint/fetch/probe — renderAgent rides the existing `/api/status` poll behind the unchanged loopback bind + same-origin `/api` guard; no new outbound.
- **T-28-03-03 (info disclosure):** the coding block + panel carry counts/identity only (version/model/mode/residency + token counts + cache ratio) — no prompt/response/secret text; pin_match is a tri-state label, not the hash.
- **T-28-03-04 (fabricated data):** cache pct shown only when both counts Known + prompt_n>0 (else gray Unknown badge); per-model usage shows the honest empty state, never a fabricated 0; residency recomputed and OMITTED when the envelope is unevaluable.
- **T-28-03-05 (append-only breach):** Coding tail-appended between Memory and SchemaVersion; coding-off golden differs from pre-plan ONLY in schema_version (verified via git diff); single 3→4 bump.
- **T-28-03-SC (installs):** none — the dashboard SPA is hand-written, no-build, served via go:embed; no JS toolchain.

## GOTCHA (operational note — no action taken)

Per the project dashboard-binary trap: a LIVE box needs `systemctl --user restart villa-dashboard.service` after `make build` for the new Agent panel (embedded `dashboard.html`/`dashboard.js`) to take effect — the long-lived `villa-dashboard.service` does not pick up rebuilt embedded assets until restarted. This was NOT performed here (off-hardware execution); it is the operator step on deploy.

## Known Stubs

None — both surfaces are wired end to end through the existing seams. The binary-pin SHA remains the unpinned sentinel until 26-03 records it on-hardware; `liveAgentPinMatch` correctly degrades that to the tri-state "unknown" (typed-Unknown), which is the honest state, not a stub.

## Self-Check: PASSED

- All modified/created files present on disk (status.go, cmd/villa/status.go, status-coding.json.golden, dashboard.js, dashboard.html — all FOUND).
- Both per-task commits exist in git history: `6f364f6` (Task 1), `5486de6` (Task 2).
- Plan-frontmatter `contains` assertions verified: `type CodingInfo` in status.go, `r.Coding != nil` in cmd/villa/status.go, the coding block in status-coding.json.golden, `renderAgent` in dashboard.js.
