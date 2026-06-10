---
phase: 23-surfacing-backup-memory-aware-swap
plan: 01
subsystem: status read-model / control plane surfacing
tags: [status, schema-v3, memory-stack, false-green-fix, golden-refreeze, recall-skew, doctor]
requires:
  - phase 19-22 memory stack (villa-qdrant/villa-embed Quadlet units, recall store, doctor memory fold)
  - internal/recall store + staleness cores (Phase 21)
provides:
  - status.Report schema_version 3 (memory rows reclassified non-GPU + Memory section)
  - per-service health seams QdrantHealth/EmbedHealth + ReadRecallState on status.Deps (dashboard inherits verbatim, D-03)
  - recall.EmbeddingSkew — THE single D-10 comparison (consumed by Plan 23-04 guards)
  - recall.CompleteRun exported single complete-run predicate
  - byte-frozen memory-on v3 fixture (cmd/villa/testdata/status-memory.json.golden)
affects:
  - 23-03 (dashboard memory panel reads report.memory; must NOT re-fix api_test pins — none existed)
  - 23-04 (recall-index refusal + install WARN consume recall.EmbeddingSkew)
  - 23-05 (on-hardware TTL churn proof; 30s fallback is a one-const change to memoryHealthTTL)
tech-stack:
  added: []
  patterns:
    - OWUI non-GPU row branch cloned for memory rows (own health seam, naOffloadVerdict, OffloadApplies=false)
    - pointer-omitempty tail-append above SchemaVersion (Pitfall 10)
    - mutex-guarded time-keyed TTL cache over podman-run probes
    - typed-Unknown probe mapping (HTTP code confident; curl exit<125 down; podman 125/126/127/absent unknown)
key-files:
  created:
    - internal/recall/skew.go
    - internal/recall/skew_test.go
    - cmd/villa/testdata/status-memory.json.golden
  modified:
    - internal/recall/staleness.go (exported CompleteRun; Classify now calls it)
    - internal/status/status.go (Deps seams, memory row branches, MemoryInfo, reportSchemaVersion=3)
    - internal/status/status_test.go
    - cmd/villa/status.go (liveQdrantHealth/liveEmbedHealth, memoryHealthTTL cache, liveReadRecallState, liveStatusDeps wiring)
    - cmd/villa/status_test.go (probe/TTL/recall-seam tests + TestStatusJSONGoldenMemoryOn)
    - internal/doctor/doctor.go (memoryOffloadDownRanked + MemoryEnabled/MemoryServices deleted)
    - internal/doctor/doctor_test.go (v3 fixtures; TestMemoryServiceDownWarns negative control)
    - cmd/villa/doctor.go (dead wiring removed — deviation, see below)
    - cmd/villa/doctor_test.go (fixtures lose memory offload findings; wiring test trimmed)
    - cmd/villa/testdata/status.json.golden (schema_version 2→3 ONLY)
    - cmd/villa/testdata/doctor-memory.json.golden, doctor-memory-pass.golden, doctor-memory-residency-fail.golden
decisions:
  - "OQ2 locked: memoryHealthTTL = 15s mutex-guarded pair cache (both services refreshed together); 30s fallback documented as a one-const change pending Plan 23-05 on-hardware proof"
  - "OQ3 locked: memoryOffloadDownRanked DELETED (unreachable after OffloadApplies=false reclassification), together with the MemoryEnabled/MemoryServices doctor Deps fields that existed only to key it"
  - "RecallState mapping: nil seam/unreadable → unknown; no run recorded → empty; CompleteRun → indexed (+count/timestamps); started-not-completed → incomplete"
  - "embedding_skew set ONLY on confident mismatch; match/unknown omit the field (never a green ok for an unevaluated comparison)"
metrics:
  duration: ~16 min
  completed: 2026-06-10
  tasks: 3
  commits: 5
---

# Phase 23 Plan 01: Status v3 Memory Surfacing Summary

**One-liner:** status.Report v2→3 — memory rows get their OWN per-service in-network health (Phase-22 chat-endpoint false-green fixed, proven by negative-control test), a Memory section with embedding identity + typed recall summary + mismatch-only skew indicator, all behind injectable seams the dashboard inherits verbatim, frozen in exactly one golden re-freeze.

## What was built

### Task 1 — Pure cores (commits f4ba413 RED, 8ab5198 GREEN)
- `internal/recall/skew.go`: `SkewState` (`unknown`/`match`/`mismatch`) + `EmbeddingSkew(st, cfgModel, cfgDim)` — the single D-10 comparison; empty recorded stamp ⇒ `SkewUnknown` (no alarm).
- `internal/recall/staleness.go`: complete-run expression extracted to exported `CompleteRun(State)`; `Classify` now calls it (no re-rolled comparison anywhere).
- `internal/status/status.go`:
  - New Deps: `QdrantService`, `EmbedService`, `QdrantHealth(addr,port)`, `EmbedHealth(addr,port)`, `ReadRecallState() *recall.State`. Nil seams degrade typed-Unknown.
  - Two memory row branches inserted BEFORE the generic GPU branch, cloning the OWUI shape: own health seam, `naOffloadVerdict()`, `OffloadApplies=false` — the false-green fix (T-23-01). `d.Health(chat endpoint)` is never consulted for these rows (poison-probe test asserts it).
  - `MemoryInfo` (pointer, `json:"memory,omitempty"`) between `Usage` and `SchemaVersion`; populated only when `cfg.MemoryEnabled`. `reportSchemaVersion = 3` with extended version history.
  - Aggregate unchanged: a down embed row degrades the verdict via the HEALTH fold; N/A offload never folds (asserted).

### Task 2 — cmd-tier live wiring (commits 6762be8 RED, b5593be GREEN)
- `liveQdrantHealth` (`/readyz`) and `liveEmbedHealth` (`/health`): fixed-arg `runProbeCurl` probes (helper image `orchestrate.EmbedImage()`, `curl -s -o /dev/null -w %{http_code} --max-time 3`, 10s parent context). Mapping: HTTP code confident (200/503/other); curl-level exit <125 → down; podman-level 125/126/127/unstartable → unknown.
- `memoryHealthTTL = 15s` mutex-guarded cache refreshing BOTH services together (one podman pair per window — TestMemoryHealthTTL asserts single execution); injectable `memoryProbeExec` seam keeps tests hermetic.
- `liveReadRecallState`: clones `liveReadUsage` over `recall.Load` — absent file ⇒ pointer to zero State (confident "empty"), corrupt ⇒ fail-closed empty, real read error ⇒ nil ("unknown").
- Service names derived via `unitServiceName(orchestrate.QdrantContainerUnitName()/EmbedContainerUnitName())` — zero literals in `cmd/villa/status.go`; `TestSeamGrepGate` green.

### Task 3 — Doctor ripple + the ONE re-freeze (commit cff505f)
- `memoryOffloadDownRanked` + call site deleted (unreachable dead code after the reclassification — doctor's `if s.OffloadApplies` gate never fires for memory rows). `MemoryEnabled`/`MemoryServices` Deps fields deleted (grep-verified unused); `slices`/`strings` imports dropped.
- Doctor fixtures updated to the v3 row shape; `TestMemoryOffloadFailNotSuppressed` (predicate-dedicated) replaced by `TestMemoryServiceDownWarns` (down embed → WARN via health finding, no offload finding).
- Single `-update` run, diff inspected before commit:
  - `status.json.golden`: exactly one content line changed (`schema_version: 2→3`) — the D-04 proof that memory-off v3 ≡ v2 + version.
  - NEW `status-memory.json.golden`: full memory-on v3 contract (per-service health rows, N/A offload, indexed memory section, no skew field on match).
  - 3 `doctor-memory*.golden`: `offload:villa-qdrant.service`/`offload:villa-embed.service` findings gone (`grep` over testdata: 0 matches).
- `make check` green; this closes the window — no later plan may touch `Report` tagged fields or status/doctor goldens (Pitfall 1).

## Deviations from Plan

### Auto-fixed / plan-directed adjustments

**1. [Plan-directed] `internal/recall/staleness.go` modified (not in `files_modified`)**
- **Found during:** Task 1
- **Issue:** the complete-run predicate existed only as an inline expression in `Classify`; the plan directed "export a thin wrapper if it is unexported, do not re-roll the comparison".
- **Fix:** extracted exported `CompleteRun(State)`; `Classify` calls it.
- **Commit:** 8ab5198

**2. [Rule 3 - Blocking] `cmd/villa/doctor.go` edited (not in `files_modified`)**
- **Found during:** Task 3
- **Issue:** deleting the `MemoryEnabled`/`MemoryServices` Deps fields (the plan's delete-if-unused directive) broke the cmd-tier wiring compile.
- **Fix:** removed the `memEnabled`/`memServices` vars and their Deps assignments; `RunMemoryChecks`/`ResidencyUnderLoad` conditional wiring unchanged.
- **Commit:** cff505f

**3. [Note] `internal/dashboard/api_test.go` ripple was a no-op**
- The plan assigned this plan the schema-version pin updates (candidates :181, :739). Inspection: :181 is a key-presence check on `/api/metrics` (no version pin) and :739 is the usage store's OWN SchemaVersion — no status-v2 pin exists in the dashboard package. No edit made; Plan 23-03 must not "re-fix" anything here either.

### Deferred (out of scope)
- Pre-existing `gofmt` violations in `cmd/villa/bench_compare.go` and `cmd/villa/verify_memory_test.go` — logged in `deferred-items.md`, untouched.

## Known Stubs

None — all seams are live-wired; no placeholder data paths.

## Threat Flags

None — the new probe surface (podman-run curl) and the recall-state read were both pre-registered in the plan's threat model (T-23-03/T-23-04/T-23-05) and carry their mitigations (fixed-arg exec, TTL bound, fail-closed load).

## Verification

- `make check` green (vet + full suite).
- `go test ./internal/inference -run TestSeamGrepGate` green (no leaked literals).
- Memory-off `--json` differs from v2 golden ONLY in `schema_version` (git diff: 1 line).
- Exactly one commit (cff505f) touches goldens.
- False-green negative controls: `TestRunMemoryEmbedDownNoFalseGreen` (core), `TestLiveEmbedHealth/curl connect failure` (cmd), `TestMemoryServiceDownWarns` (doctor).

## Commits

| Commit | Type | Content |
|--------|------|---------|
| f4ba413 | test | RED: skew table + status v3 memory contract tests |
| 8ab5198 | feat | GREEN: skew helper, CompleteRun export, status v3 core |
| 6762be8 | test | RED: cmd-tier probe/TTL/recall-seam tests |
| b5593be | feat | GREEN: live probes, TTL cache, liveStatusDeps wiring |
| cff505f | feat | doctor ripple + single golden re-freeze |

## TDD Gate Compliance

Tasks 1 and 2 each carry a RED `test(...)` commit followed by a GREEN `feat(...)` commit; both RED runs were verified failing (compile errors on the not-yet-existing symbols) before implementation. Task 3 was non-TDD (golden re-freeze) per plan.

## Self-Check: PASSED

All created files and all five task commits verified present (2026-06-10).
