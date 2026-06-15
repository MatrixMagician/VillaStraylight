---
phase: 28-agent-surfacing-contracts
verified: 2026-06-15T00:00:00Z
status: human_needed
score: 5/5 must-haves verified
overrides_applied: 0
human_verification:
  - test: "On the gfx1151 host, enable the coding agent (villa install --coding-agent + coding mode), bring the stack up (villa up), then run `villa doctor`. Confirm the agent-residency-under-load finding samples while a crush-run tool-call round-trip is verifiably IN FLIGHT (not idle), and that a forced CPU-fallback of the coder under load surfaces as a BLOCK FAIL — not a false-green PASS."
    expected: "agent-residency finding reports the under-load residency proof from a genuinely in-flight tool-call drive; an idle/fast-exiting probe degrades to a typed-Unknown WARN (agentUnevaluable), never an idle-sampled PASS. A real coder CPU-fallback dominates the HTTP-200 health and folds doctor to overall FAIL/exitBlocked."
    why_human: "WR-03 is a host-coupled exec path (crush run + RunningOffloadVerdict over GTT/journald) whose in-flight timing (agentResidencySettle vs probe duration) can only be exercised against a live coder model on the gfx1151 box. The in-flight discipline is present and correct in code, but the actual under-load sampling behavior is not provable off-hardware."
  - test: "On a memory-ENABLED host with the agent on, run `villa status --json` and compare coding.residency against the post-embedding-reservation coder fit. Then open the dashboard (after `systemctl --user restart villa-dashboard.service`) and confirm the Agent panel appears with version/pin/model/mode/residency rows, per-model coder usage, and the cache-effectiveness row (pct+raw or gray Unknown badge)."
    expected: "coding.residency reflects recommend.Pick computed against the REAL memory inputs (post-reservation envelope) — e.g. 'swap' when the post-reservation reality is swap, never an optimistic 'shared' from the un-reserved envelope (WR-01). The dashboard Agent panel mirrors the Memory panel, is hidden when the agent is off, and renders no fabricated 0/0%."
    why_human: "Residency-vs-memory-reservation correctness depends on the live envelope and the configured embedding model on the host; the dashboard panel is a visual surface that must be eyeballed for layout/hidden-until-data behavior. Both are off-hardware-unverifiable on this agent-off dev environment."
---

# Phase 28: Agent Surfacing & Contracts Verification Report

**Phase Goal:** The operator can see, diagnose, back up, and measure the coding agent — `status.Report` 3→4 lands once at the end (the proven v1.2/v1.3 single-bump discipline), dashboard Agent panel, doctor agent checks, and honest usage/cache-effectiveness signals.
**Verified:** 2026-06-15
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

| #   | Truth (ROADMAP Success Criterion) | Status | Evidence |
| --- | --------------------------------- | ------ | -------- |
| SC1 | `villa status` (human + `--json`) reports an append-only `coding` block (enabled, version, pin_match, model, mode, residency); `status.Report` 3→4 in exactly ONE golden re-freeze; dashboard renders a matching Agent panel hidden-until-data; coding-off byte-identical except schema_version | ✓ VERIFIED | `CodingInfo` struct (status.go:231-241) with locked snake_case field set; `Coding *CodingInfo` tail-appended between Memory and SchemaVersion (status.go:161); `reportSchemaVersion = 4` (status.go:186); coding-off golden diff vs main is EXACTLY one line `schema_version 3→4`, no `coding` key; status-coding.json.golden carries full block (enabled/version/pin_match/model/mode/residency/cache); `#agent-panel` shell ships `hidden` (dashboard.html:74); `renderAgent` hidden-until-data wired in `.then` (dashboard.js:369,848); live `./villa status --json` (agent-off) emits schema_version 4 and NO coding key |
| SC2 | `villa doctor` folds agent checks (binary/version drift, config drift, tool-call round-trip, under-load residency) — offload/residency FAIL dominates a healthy HTTP probe, never false-green | ✓ VERIFIED | `agentToolCallFinding`/`agentResidencyFinding` map StatusFail → tierBlock/statusFail (BLOCK FAIL dominates HTTP-200), StatusWarn → typed-Unknown WARN (doctor.go:528-582); `agentDriftFindings` (binary/config drift, WARN+remediation, ConfigAbsent-alone → no finding); three nil-safe seams folded worst-wins in Aggregate (doctor.go:310-317); doctor schema stays 2 (own contract, no second status bump); seam gate green |
| SC3 | `villa backup`/`restore` cover the rendered agent config; agent binary identity-recorded and EXCLUDED, exactly like model weights | ✓ VERIFIED | `ExcludedAgent` identity-only (SHA256/Version/PinSHA256, manifest.go:94-107); `EntryCrushConfig = "crush.json"` archive entry (backup.go:227); binary BYTES never an entry, no `EntryCrushBinary` (backup.go:226,300); `backupSchemaVersion = 3` (own contract); restore re-stages + fail-closed identity verify; TestExcludedAgentHasNoContentFields, TestBackupAgentOffIsLayoutIdentical, TestRestoreAgentIdentityDriftFailsClosed all pass |
| SC4 | Agent token usage attributed per-model (coder model id) via v1.2 usage core, in status + dashboard | ✓ VERIFIED | status selects `r.Usage.Models[c.Model]` keyed on coder model (status.go:166-167); dashboard selects `usage.models[ag.model]` keyed on report.coding.model (dashboard.js:414-415); honest "No usage recorded yet" empty state (never fabricated 0) |
| SC5 | Cache effectiveness (`timings.cache_n` vs `prompt_n`) surfaced as honest agent-speed signal | ✓ VERIFIED | `CacheSample` + `ScrapeCacheCounters` read cache_n/prompt_n via `counterFromMap` typed-Unknown (metrics/llamacpp.go:105-290); reuses bounded scrape (no second HTTP request); codingInfo pct computed ONLY when `ok && promptN>0 && cacheN<=promptN` (status.go:715) — never a fabricated 0%, WR-04 inconsistency degraded to Unknown |

**Score:** 5/5 truths verified

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | -------- | ------ | ------- |
| `internal/status/status.go` | CodingInfo block + 3→4 bump + codingInfo() + nil-safe seams | ✓ VERIFIED | CodingInfo:231, Coding field:161, reportSchemaVersion=4:186, codingInfo():679, AgentEnabled gate:506-507 |
| `cmd/villa/status.go` | human-table coding rows + live seam wiring (residency recomputed) | ✓ VERIFIED | `r.Coding != nil` block:147; liveAgentResidency:338 with REAL MemoryInputs:357-358 (WR-01 fix); liveAgentPinMatch:298; liveAgentCache:369 |
| `cmd/villa/testdata/status-coding.json.golden` | coding-on fixture with full block | ✓ VERIFIED | enabled/pin_match/model/mode/residency/cache + schema_version 4 |
| `cmd/villa/testdata/status.json.golden` | coding-off, byte-identical except schema_version | ✓ VERIFIED | git diff vs main = 1 line (schema_version 3→4), no coding key |
| `internal/dashboard/assets/dashboard.{html,js}` | renderAgent hidden-until-data panel | ✓ VERIFIED | #agent-panel hidden shell:74; renderAgent:369 wired in .then:848 |
| `internal/doctor/doctor.go` | agent finding mappers + nil-safe seams | ✓ VERIFIED | agentToolCallFinding:528, agentResidencyFinding:559, agentDriftFindings:598, seams folded:310-317, schema=2 |
| `cmd/villa/doctor.go` | live agent seam wiring gated on agent_enabled + in-flight residency | ✓ VERIFIED | gated:249; runAgentResidencyUnderLoad in-flight discipline:651-717 (WR-03 fix); residencySampleAfter:330 |
| `internal/backup/manifest.go` | ExcludedAgent identity record + EntryCrushConfig + schema 2→3 | ✓ VERIFIED | ExcludedAgent:94, EntryCrushConfig:64, backupSchemaVersion=3:42 |
| `internal/backup/backup.go` | crush.json INTO archive, binary excluded | ✓ VERIFIED | EntryCrushConfig entry:227, binary never archived:300-304 |
| `internal/backup/restore.go` | restore re-stage + honest CrushConfigRestored | ✓ VERIFIED | crushWritten:426, crushSkipped:427, CrushConfigRestored:479 (WR-02 fix) |
| `internal/metrics/llamacpp.go` | cache_n/prompt_n typed-Unknown CacheSample | ✓ VERIFIED | CacheSample:105, ScrapeCacheCounters:271, counterFromMap:289-290 |

### Key Link Verification

| From | To | Via | Status |
| ---- | -- | --- | ------ |
| internal/status/status.go | config.AgentEnabled | `if cfg.AgentEnabled { report.Coding = codingInfo(...) }` (status.go:506-507) | ✓ WIRED |
| cmd/villa/status.go | recommend.Pick(...).Coder.Residency | liveAgentResidency with real MemoryInputs (status.go:357-359) | ✓ WIRED |
| dashboard.js | /api/status | renderAgent(report) in the .then poll (dashboard.js:848) | ✓ WIRED |
| dashboard.js | report.usage.models[report.coding.model] | per-model coder usage selection (dashboard.js:414-415) | ✓ WIRED |
| cmd/villa/doctor.go | liveAgentToolCallProbe | reused for tool-call + residency-under-load drive (doctor.go:262-264,673) | ✓ WIRED |
| backup.go | manifest.ExcludedAgent | identity recorded, bytes excluded (backup.go:301-325) | ✓ WIRED |
| metrics/llamacpp.go | counterFromMap | cache_n/prompt_n typed-Unknown reads (llamacpp.go:289-290) | ✓ WIRED |

### Code-Review Warning Closure (WR-01..WR-04)

| Warning | Fix | Status |
| ------- | --- | ------ |
| WR-01: residency ignored memory reservation → wrong swap/shared | liveAgentResidency now threads `MemoryInputs{Enabled: cfg.MemoryEnabled, EmbeddingModel: cfg.EmbeddingModel}` (status.go:357-358) | ✓ FIXED |
| WR-02: CrushConfigRestored false-green | `crushWritten = ex.crushPresent && DestPath != ""`; CrushConfigRestored=crushWritten + new CrushConfigSkipped flag (restore.go:426-480); regression test at restore_test.go:1220-1238 | ✓ FIXED |
| WR-03: residency sample not guaranteed under load | runAgentResidencyUnderLoad in-flight discipline (settle + sample-only-if-still-running + join + agentUnevaluable fallback, doctor.go:651-717) | ✓ FIXED (code) — on-hardware confirmation required (human item 1) |
| WR-04: cache pct unbounded (>100%) | codingInfo gates pct on `cacheN <= promptN` → degrades to Unknown on inconsistent sample (status.go:715) | ✓ FIXED |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| make check (vet + full test) | `make check` | all packages ok | ✓ PASS |
| Seam grep gate | `go test ./internal/inference/ -run TestSeamGrepGate` | 1 passed | ✓ PASS |
| Doctor suite | `go test ./internal/doctor/...` | 36 passed | ✓ PASS |
| Status core suite | `go test ./internal/status/...` | 56 passed | ✓ PASS |
| cmd Status/Doctor/Seam | `go test ./cmd/villa/... -run 'TestStatus\|TestDoctor\|TestSeamGrepGate'` | 43 passed | ✓ PASS |
| Backup core | `go test ./internal/backup/...` | 84 passed | ✓ PASS |
| cmd Backup/Restore | `go test ./cmd/villa/... -run 'TestBackup\|TestRestore'` | 14 passed | ✓ PASS |
| Metrics | `go test ./internal/metrics/...` | 27 passed | ✓ PASS |
| Dashboard | `go test ./internal/dashboard/...` | 55 passed | ✓ PASS |
| Live coding-off status --json | `./villa status --json` | schema_version 4, NO coding key (agent-off omits it) | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ----------- | ----------- | ------ | -------- |
| SURF-01 | 28-03 | status 3→4 coding block + dashboard Agent panel | ✓ SATISFIED | SC1 |
| SURF-02 | 28-01 | doctor folds agent checks | ✓ SATISFIED | SC2 |
| SURF-03 | 28-02 | backup/restore agent coverage, binary identity-recorded + excluded | ✓ SATISFIED | SC3 |
| USAGE-03 | 28-03 | per-model agent usage via v1.2 usage core | ✓ SATISFIED | SC4 |
| USAGE-04 | 28-02 (core), 28-03 (surface) | cache effectiveness cache_n/prompt_n | ✓ SATISFIED | SC5 |

All 5 declared requirement IDs are covered. No orphaned requirements — REQUIREMENTS.md maps exactly SURF-01/02/03 + USAGE-03/04 to Phase 28, all present in plan frontmatter.

### Anti-Patterns Found

| File | Pattern | Severity | Impact |
| ---- | ------- | -------- | ------ |
| (none) | TBD/FIXME/XXX scan of all modified files | — | No debt markers in any modified file |

Honesty-by-construction discipline verified: typed-Unknown throughout (PinUnknown tri-state, residency "" omitted, cache pct nil-on-Unknown), no fabricated 0/0%, no false-green (offload/residency FAIL dominates HTTP-200), no backend marker literal leak (seam gate green). The two `Vulkan0` literals are in pre-existing help text / golden fixture detail strings, not the new seam-locked paths (seam gate confirms).

### Human Verification Required

#### 1. On-hardware residency-under-load discipline (WR-03)

**Test:** On the gfx1151 host, enable the coding agent + coding mode, `villa up`, then `villa doctor`. Confirm the agent-residency finding samples while a crush-run tool-call round-trip is verifiably IN FLIGHT.
**Expected:** Under-load residency proof from a genuinely in-flight drive; idle/fast-exit degrades to typed-Unknown WARN, never an idle-sampled PASS; a real coder CPU-fallback dominates HTTP-200 → overall FAIL.
**Why human:** Host-coupled exec path (crush run + RunningOffloadVerdict over GTT/journald); in-flight timing only exercisable against a live coder model. Code discipline is present and correct; runtime behavior is off-hardware-unverifiable.

#### 2. Residency-vs-reservation correctness (WR-01) + dashboard Agent panel visual

**Test:** On a memory-enabled host with the agent on, run `villa status --json` and check coding.residency against the post-reservation coder fit; open the dashboard (after `systemctl --user restart villa-dashboard.service`) and inspect the Agent panel.
**Expected:** residency reflects the REAL post-reservation envelope (never an optimistic 'shared'); panel mirrors Memory panel, hidden when agent off, no fabricated 0/0%.
**Why human:** Depends on live envelope + configured embedding model; the panel is a visual surface needing eyeball verification. This dev host is agent-off.

### Gaps Summary

No code gaps. All 5 ROADMAP success criteria are observably true in the codebase, all 5 requirement IDs are satisfied, all 4 code-review warnings (WR-01/02/03/04) are confirmed fixed in actual source (not just SUMMARY claims), the single-bump discipline holds (status=4 once; doctor=2 and backup=3 own separate contracts), the coding-off golden is append-only (one-line schema diff vs main), and every gate is green (`make check`, seam grep gate, all relevant package suites). The phase goal — operator can see (SC1), diagnose (SC2), back up (SC3), and measure (SC4/SC5) the coding agent — is achieved.

Status is `human_needed` (not `passed`) solely because two surfaces are inherently off-hardware-unverifiable on this agent-off dev environment: the WR-03 residency-under-load in-flight exec path (explicitly flagged as requiring on-hardware confirmation) and the residency-vs-reservation / dashboard-panel visual behavior. These are confirmation items, not defects.

---

_Verified: 2026-06-15_
_Verifier: Claude (gsd-verifier)_
