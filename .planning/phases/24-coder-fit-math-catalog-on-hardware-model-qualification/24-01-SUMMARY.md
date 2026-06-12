---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
plan: 01
subsystem: catalog
tags: [go, catalog, json-schema, gguf, huggingface, fail-closed, asvs-v5]

# Dependency graph
requires:
  - phase: 02-recommend-catalog
    provides: schema-v2 catalog (Shards download metadata, exact-match schema window, DisallowUnknownFields external decode)
provides:
  - Catalog schema 3 (SupportedSchema = 3) with five optional coder-role fields on CatalogModel (Role, AgentCtx, CacheReuseSafe, AgentSampling, TemplateProvenance) + exported AgentSampling struct
  - Three role:"coder" seed entries with revision-pinned shard URLs and template provenance — qwen3-coder-30b-a3b (64 GB, cache_reuse_safe true expected), qwen3-coder-next-q4 (128 GB), qwen3-coder-next-q3 (96 GB)
  - External-catalog coder-entry validation (refuse-with-warning + embedded-seed fallback, never clamp) and 2-vs-3 schema window proven both directions
affects: [24-02 coder fit math, 24-03 on-hardware qualification, 24-04 reconciliation, phase-25 render]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Append-only catalog schema evolution: struct fields + constant bump + seed bump + test-pin flip land in ONE commit (DisallowUnknownFields trap, Pitfall 5)"
    - "Refuse-never-clamp validation on the external-catalog trust boundary (whole-catalog refusal + seed fallback, ASVS V5)"

key-files:
  created:
    - internal/catalog/testdata/schema3-external.json
    - internal/catalog/testdata/schema2-catalog.json
  modified:
    - internal/catalog/catalog.go
    - internal/catalog/seed.json
    - internal/catalog/load.go
    - internal/catalog/catalog_test.go
    - internal/catalog/testdata/good-catalog.json
    - internal/catalog/testdata/multishard-catalog.json

key-decisions:
  - "AgentSampling is a pointer field (*AgentSampling) so absent blocks stay absent on re-encode — chat entries emit no agent_sampling key (D-03)"
  - "Existing schema-2 external fixtures (good-catalog, multishard) bumped to 3 so warning-free external-load tests keep exercising the accept path; a dedicated schema2-catalog.json fixture covers the falls-back branch"
  - "qwen3-coder-next-* hybrid-arch rationale (n_layers 12 = full-attention layers, not 48) recorded in display_name per RESEARCH Pattern 3"

patterns-established:
  - "Coder-entry validation bounds: agent_ctx > 0; temperature (0,2]; top_p (0,1]; top_k >= 0; repeat_penalty (0,3] — refuse whole external catalog on violation"

requirements-completed: [CODER-01]

# Metrics
duration: 9min
completed: 2026-06-12
---

# Phase 24 Plan 01: Catalog Schema 3 + Coder Seed Entries Summary

**Catalog schema 2→3 with three revision-pinned role:"coder" seed entries (Qwen3-Coder-30B-A3B + Qwen3-Coder-Next Q4/Q3), fail-closed optional coder fields, and refuse-never-clamp validation on the external-catalog trust boundary**

## Performance

- **Duration:** ~9 min
- **Started:** 2026-06-12T21:17:58Z
- **Completed:** 2026-06-12T21:27:00Z
- **Tasks:** 2
- **Files modified:** 8

## Accomplishments

- `SupportedSchema = 3` with a v3 history paragraph; `CatalogModel` gained the five optional coder fields (`role`, `agent_ctx`, `cache_reuse_safe`, `agent_sampling`, `template_provenance`) each doc-commented with its decision ID; new exported `AgentSampling` struct (temperature/top_p/top_k/repeat_penalty)
- seed.json ships 3 coder entries with `resolve/{40-hex-revision}` shard URLs (never `resolve/main`), per-shard sha256/size, and repo@revision template provenance (D-02, T-24-01); the 4 chat entries are untouched apart from the two top-level version bumps (D-03, verified via git diff)
- Fail-closed defaults proven by tests: absent `role` decodes as chat (""), absent `cache_reuse_safe` decodes false, absent `agent_sampling` stays nil — only qwen3-coder-30b-a3b carries `cache_reuse_safe: true` (expected value; plan 24-03's probe licenses it), both hybrid Next entries omit it
- External-catalog window proven both directions for 2-vs-3: schema-2 file warns + falls back; schema-3 file with all five coder keys round-trips under `DisallowUnknownFields` (Pitfall 5 regression guard)
- `validateCoderEntries` on the external path: any `role:"coder"` entry with `agent_ctx <= 0` or out-of-range sampling refuses the WHOLE external catalog with a warning naming the entry id + embedded-seed fallback — never clamped, never partially accepted (T-24-02, ASVS V5); 1 MiB bounded reader and `DisallowUnknownFields` untouched (T-24-03)
- `make check` green (1070+ tests across 22 packages, incl. `TestSeamGrepGate` — no backend/image literals added)

## Task Commits

1. **Task 1: CatalogModel schema-3 fields + SupportedSchema=3 + seed coder entries (one commit per Pitfall 5)** - `1509717` (feat)
2. **Task 2 RED: external validation tests + schema window fixtures** - `7168524` (test)
3. **Task 2 GREEN: refuse-never-clamp validation for external coder entries** - `b38c7eb` (feat)

## Files Created/Modified

- `internal/catalog/catalog.go` - SupportedSchema 3 + five coder fields + AgentSampling struct
- `internal/catalog/seed.json` - schema_version 3, catalog_version 2026.06.2, three coder entries appended
- `internal/catalog/load.go` - validateCoderEntries pass between schema check and accept on the external path
- `internal/catalog/catalog_test.go` - 7 new tests (coder seed pin, fail-closed defaults, chat-untouched, verified dims, schema-2 fallback, schema-3 round-trip, refuse-never-clamp matrix); schema pin flipped 2→3
- `internal/catalog/testdata/schema3-external.json` - schema-3 round-trip fixture (one coder entry, all five keys)
- `internal/catalog/testdata/schema2-catalog.json` - previous-schema external fixture for the falls-back branch
- `internal/catalog/testdata/good-catalog.json`, `multishard-catalog.json` - schema bumped 2→3 (warning-free accept-path fixtures)

## Decisions Made

- `AgentSampling` implemented as pointer (`*AgentSampling`) per the plan's primary recommendation — absent blocks stay absent on re-encode, keeping chat entries key-free
- Invalid-sampling test cases generated in-test via `t.TempDir()` rather than six static fixtures (keeps testdata/ minimal; the round-trip and schema-2 cases remain static fixtures per the existing convention)
- Hybrid-arch `n_layers: 12` rationale embedded in both Qwen3-Coder-Next display_names so nobody "fixes" it back to 48

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Bumped schema-2 accept-path fixtures to schema 3**
- **Found during:** Task 1 (schema bump)
- **Issue:** `testdata/good-catalog.json` and `testdata/multishard-catalog.json` carried `schema_version: 2`; after the bump, `TestLoadPrefersExternal` and `TestLoadMultiShardParses` (which assert warning-free external acceptance) would fail against the exact-match window
- **Fix:** Bumped both fixtures to `schema_version: 3`; Task 2 added a dedicated `schema2-catalog.json` fixture so the 2-falls-back branch stays covered
- **Files modified:** internal/catalog/testdata/good-catalog.json, internal/catalog/testdata/multishard-catalog.json
- **Verification:** Full catalog suite green (23 tests)
- **Committed in:** 1509717 (Task 1 commit)

**2. [Rule 1 - Bug] Reworded doc comment to satisfy the no-clamp grep gate**
- **Found during:** Task 2 (validation implementation)
- **Issue:** The acceptance criterion `grep -n "clamp" internal/catalog/load.go` returns no matches; my validation doc comment contained the word "clamped"
- **Fix:** Reworded to "silently coerced into range"; behavior unchanged
- **Files modified:** internal/catalog/load.go
- **Verification:** grep count 0; tests green
- **Committed in:** b38c7eb (Task 2 commit)

---

**Total deviations:** 2 auto-fixed (1 blocking, 1 acceptance-criterion compliance)
**Impact on plan:** Both fixes required for green acceptance criteria. No scope creep.

## TDD Gate Compliance

- Task 1 (tdd): RED observed as compile failure (new fields undefined), then implemented; landed as ONE commit per the task's explicit "(one commit)" mandate (Pitfall 5: DisallowUnknownFields rejects seed keys the struct lacks — a test-only commit cannot compile/pass)
- Task 2 (tdd): full RED (`7168524`, 7 failing validation subtests) → GREEN (`b38c7eb`) commit sequence

## Issues Encountered

None beyond the documented deviations.

## User Setup Required

None - no external service configuration required.

## Next Phase Readiness

- Plan 24-02 (coder fit math) can consume the three seed entries via `Role == "coder"` filtering and compute fit at `AgentCtx`
- Plan 24-03 (on-hardware qualification) has revision-pinned artifacts + expected `cache_reuse_safe` values to probe; plan 24-04 reconciles
- Phase 25 render inputs (`agent_ctx`, `cache_reuse_safe`, `agent_sampling`, `template_provenance`) are in place

---
*Phase: 24-coder-fit-math-catalog-on-hardware-model-qualification*
*Completed: 2026-06-12*

## Self-Check: PASSED

- All created files verified on disk (schema3-external.json, schema2-catalog.json, 24-01-SUMMARY.md)
- All task commits verified in git log (1509717, 7168524, b38c7eb)
