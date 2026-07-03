---
phase: 31-grounded-fetch-embed-grounding
plan: 02
subsystem: infra
tags: [recommend, config, ctx-reservation, ssrf, websafe, golden, crypto-rand, toml]

# Dependency graph
requires:
  - phase: 22-memory-ctx-reservation
    provides: "MemoryInputs/memoryReservation/EmbeddingReservationBytes reservation-before-fit seam (mirrored here)"
  - phase: 29-searxng-service
    provides: "SearXNG config field discipline (omit-when-off ,omitempty/,omitzero + self-heal + GenerateSearxngSecret) cloned here"
provides:
  - "recommend.WebSearchInputs + webSearchReservation: conservative web-RAG ctx budget reserved BEFORE the chat fit, gated on Enabled (GROUND-03)"
  - "Recommendation.WebSearchReservationBytes (append-only) + recommendSchemaVersion 3->4 + re-frozen recommend golden"
  - "config.VillaConfig WebsafeAddr/WebsafePort/WebLoaderSecret/HostVillaPath (omit-when-off + addr/port self-heal)"
  - "config.GenerateWebLoaderSecret: crypto/rand EXTERNAL_WEB_LOADER_API_KEY bearer (T-31-08/T-31-11)"
affects: [31-03-render-cmd-wiring, 31-04-offload-assert, 33-egress-verify, 34-surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Reservation-before-fit, gated, schema-bumped (cloned from MemoryInputs)"
    - "Config field omit-when-off + addr/port self-heal (cloned from SearXNG fields)"
    - "Saturating combined reservation (addSaturating) with uint64 clamp-to-0"

key-files:
  created: []
  modified:
    - internal/recommend/recommend.go
    - internal/recommend/recommend_test.go
    - internal/config/villaconfig.go
    - internal/config/villaconfig_test.go
    - cmd/villa/testdata/recommend.golden.json

key-decisions:
  - "A6 reservation formula: (TopK x ChunkSizeChars / 3.5 chars-per-token + ResultCount x 64 citation tokens) x 4096 bytes-per-ctx-token x 1.5 safety; deliberately conservative, on-hardware tuning deferred to Phase 33/34"
  - "All reservation-math literals live as named consts in internal/recommend (single home); zero tuning values fall back to OWUI defaults (3/1000/3) so Enabled:true always reserves non-zero"
  - "WebsafePort default 8090; addr/port self-heal but the bearer secret + captured host path never self-heal"
  - "Recommend golden re-frozen via the existing cmd/villa TestRecommendJSONGolden -update path (the actual golden lives in cmd/villa/testdata, not internal/recommend/testdata)"

patterns-established:
  - "Combined memory+web reservation shrinks the envelope via addSaturating then the existing uint64 clamp before pickBest/pickOverride"
  - "combineNotes preserves degraded-note ordering (memory notes first, then web) and returns nil when both empty (byte-identical-off note shape)"

requirements-completed: [GROUND-03, GROUND-01, GROUND-02]

# Metrics
duration: 30min
completed: 2026-06-19
status: complete
---

# Phase 31 Plan 02: recommend web-search ctx reservation + config websafe fields Summary

**recommend.Pick now reserves a conservative web-RAG ctx budget (GROUND-03) before the chat fit behind a gated WebSearchInputs seam (schema 3->4, golden re-frozen), and config gains the villa-websafe loader identity + crypto/rand bearer with byte-identical-off discipline.**

## Performance

- **Duration:** ~30 min
- **Started:** 2026-06-19T17:14Z (approx)
- **Completed:** 2026-06-19
- **Tasks:** 2 (both TDD)
- **Files modified:** 15 (5 source/test of record + 8 cmd/villa call-site updates + 1 golden + 1 coder_test schema bump)

## Accomplishments

- `WebSearchInputs{Enabled,ResultCount,TopK,ChunkSizeChars}` + `webSearchReservation` mirror the shipped `MemoryInputs`/`memoryReservation` seam: `(0, nil)` when off (byte-identical-off), a conservative A6 reservation on, with an honest budget note.
- `Pick` shrinks the envelope by BOTH the embedding and web reservations (saturating add + uint64 clamp-to-0) BEFORE `pickBest`/`pickOverride`; `WebSearchReservationBytes` is stamped on EVERY path including the no-envelope refusal; `recommendSchemaVersion` bumped 3->4; recommend golden re-frozen with exactly `web_search_reservation_bytes: 0` + `schema_version: 4` (the single sanctioned Phase 31 bump).
- `config.VillaConfig` gains `WebsafeAddr`/`WebsafePort`/`WebLoaderSecret`/`HostVillaPath` with SearXNG-mirrored tags: addr/port self-heal from `defaultConfig` (villa-websafe:8090, container-DNS only PRIV-01), the bearer secret + captured host path never self-heal, and all four are zeroed when web search is off so an off install is byte-identical on disk.
- `GenerateWebLoaderSecret` (1:1 crypto/rand clone of `GenerateSearxngSecret`) produces the `EXTERNAL_WEB_LOADER_API_KEY` bearer (T-31-08/T-31-11).

## Task Commits

Each task was committed atomically (both TDD: RED tests written first, then GREEN implementation, combined per task):

1. **Task 1: recommend reservation-before-fit (GROUND-03) + schema 3->4 + golden re-freeze** - `212985c` (feat)
2. **Task 2: config websafe fields + GenerateWebLoaderSecret + omit-when-off + self-heal** - `8a22f12` (feat)

_Note: both tasks bundled their RED test edits and GREEN implementation into a single per-task commit (the test files and source change atomically together)._

## Files Created/Modified

- `internal/recommend/recommend.go` - `WebSearchInputs`, `webSearchReservation`, reservation-math consts, `WebSearchReservationBytes` field, schema 3->4, `web` threaded through `Pick` + `finalizeRecommendation`, `combineNotes` helper.
- `internal/recommend/recommend_test.go` - `TestWebSearchReservation` + `TestPickWebSearchReservation`; existing `Pick` calls + SchemaVersion assertions updated for the new signature/contract.
- `internal/recommend/coder_test.go` - `Pick` calls updated to the new signature; one refusal SchemaVersion assertion 3->4.
- `internal/config/villaconfig.go` - four websafe fields, `defaultConfig`/`normalizeVilla`/`marshalVilla` extensions, `GenerateWebLoaderSecret`.
- `internal/config/villaconfig_test.go` - websafe omit-when-off/self-heal/secret tests; round-trip `want` literal extended with inert websafe defaults.
- `cmd/villa/recommend_test.go` - fixture `WebSearchReservationBytes: 0` + `SchemaVersion: 4`.
- `cmd/villa/testdata/recommend.golden.json` - re-frozen: `web_search_reservation_bytes: 0` + `schema_version: 4` (isolated diff, no other golden touched).
- `cmd/villa/{recommend,inference,backend,dashboard,model,coding-mode,install,status}.go` - production `recommend.Pick` call sites pass a zero-value `WebSearchInputs{}` (Plan 03 supplies the real config-threaded inputs).

## Decisions Made

- A6 conservative reservation formula encoded as named consts in `internal/recommend` (single home): `charsPerToken=3.5` (×10 fixed-point), `citationTokensPerResult=64`, `bytesPerCtxToken=4096`, `safetyFactor=1.5`. Over-reserving is safe; on-hardware tuning is deferred to Phase 33/34 per the STATE.md unmeasured-ctx blocker.
- Zero tuning values (`TopK`/`ChunkSizeChars`/`ResultCount` == 0) fall back to OWUI defaults (3/1000/3) so an `Enabled:true` with unset tuning still reserves a non-zero budget — never a silent 0 when on.
- `WebsafePort` default 8090 (CONTEXT Area 1 fixed internal port). Bearer secret + `HostVillaPath` have no default and are never self-healed (a generated secret / captured path must not be re-hard-coded).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated existing recommend Pick call sites + SchemaVersion assertions for the new signature/contract**
- **Found during:** Task 1 (Pick signature change)
- **Issue:** Adding the `web WebSearchInputs` parameter to `Pick` broke compilation of all existing callers (cmd/villa production + recommend/coder test files), and bumping the schema to 4 broke three `SchemaVersion == 3` test assertions.
- **Fix:** Threaded a zero-value `WebSearchInputs{}` through every production caller (web-off pending Plan 03, as the plan explicitly anticipates) and the test callers; updated the three schema assertions 3->4.
- **Files modified:** cmd/villa/{recommend,inference,backend,dashboard,model,coding-mode,install,status}.go, internal/recommend/recommend_test.go, internal/recommend/coder_test.go
- **Verification:** `go build ./...` clean; `go test ./...` all green.
- **Committed in:** 212985c (Task 1 commit)

**2. [Rule 3 - Blocking] Recommend golden lives in cmd/villa/testdata, not internal/recommend/testdata**
- **Found during:** Task 1 (golden re-freeze)
- **Issue:** The plan's `files_modified` listed `internal/recommend/testdata` and the verify command named `go test ./internal/recommend/ -run TestRecommendationGolden`, but no such golden/test exists there — the recommend `--json` golden is `cmd/villa/testdata/recommend.golden.json`, re-frozen via `cmd/villa`'s `TestRecommendJSONGolden -update`.
- **Fix:** Re-froze the actual golden via `go test ./cmd/villa/ -run TestRecommendJSONGolden -update` after bumping the `fixtureRecommendation` schema to 4. No `internal/recommend/testdata` directory was created (it would be unused).
- **Files modified:** cmd/villa/recommend_test.go, cmd/villa/testdata/recommend.golden.json
- **Verification:** golden diff is exactly the new key + schema bump; no other golden changed.
- **Committed in:** 212985c (Task 1 commit)

**3. [Rule 3 - Blocking] Extended TestSaveLoadRoundTrip want-literal with inert websafe defaults**
- **Found during:** Task 2 (config field add)
- **Issue:** The full-literal round-trip test failed because `normalizeVilla` now self-heals the new websafe addr/port on load, so the `want` literal needed the inert defaults (exactly as the SearXNG fields were handled).
- **Fix:** Added `WebsafeAddr: "villa-websafe"` / `WebsafePort: 8090` to the `want` literal.
- **Files modified:** internal/config/villaconfig_test.go
- **Verification:** `go test ./internal/config/` green.
- **Committed in:** 8a22f12 (Task 2 commit)

---

**Total deviations:** 3 auto-fixed (all Rule 3 - blocking compilation/contract follow-through). Deviation 1 is explicitly anticipated by the plan ("this changes the Pick SIGNATURE — its callers in cmd/villa will not compile until Plan 03... update the call sites minimally to pass a zero-value WebSearchInputs{}").
**Impact on plan:** No scope creep. All three are mechanical follow-through of the two contract changes the plan mandates.

## Issues Encountered

- The plan's verify command pointed at a non-existent `internal/recommend` golden; resolved by using the real `cmd/villa` golden path (see Deviation 2). No functional impact.

## Threat Surface

No new network/auth/file surface introduced beyond the plan's `<threat_model>`. The mitigations the register assigns to this plan are satisfied:
- **T-31-08/T-31-11** (bearer secrecy/quality): `WebLoaderSecret` is `,omitempty`, zeroed off, never self-healed; `GenerateWebLoaderSecret` uses crypto/rand (source-guard pattern mirrors `TestGenerateSearxngSecretUsesCryptoRand`).
- **T-31-09** (DoS / envelope under search load): conservative reservation subtracted before the chat fit (GROUND-03); offload-assert exercise deferred to Plan 04 / Phase 33-34 per the plan.
- **T-31-10** (byte-identical-off regression): `marshalVilla` zeroing + `,omitzero`/`,omitempty` keep the web-off config byte-identical; guarded by the existing round-trip + omit-when-off tests.

## User Setup Required

None - no external service configuration required. (Plan 03 wires the websafe unit + OWUI external-loader env that consume these config fields.)

## Next Phase Readiness

- Plan 03 (render/cmd) can now thread `WebSearchInputs` from `cfg` into `recommend.Pick` and compose the villa-websafe `EXTERNAL_WEB_LOADER_URL` + bearer EnvironmentFile from `WebsafeAddr`/`WebsafePort`/`WebLoaderSecret`/`HostVillaPath`.
- Plan 04 exercises the offload-assert-under-search-load seam against the new reservation.
- Note for Plan 03: the production `recommend.Pick` call sites currently pass `WebSearchInputs{}` (web-off) — Plan 03 must replace these with the config-derived inputs.

## Self-Check: PASSED

- Files verified present: internal/recommend/recommend.go, internal/config/villaconfig.go, cmd/villa/testdata/recommend.golden.json, 31-02-SUMMARY.md
- Commits verified in history: 212985c (Task 1), 8a22f12 (Task 2)
- `go test ./internal/recommend/... ./internal/config/...` green; `go test ./...` green; `go build ./...` clean; recommend golden re-frozen schema 3->4 with isolated diff.

---
*Phase: 31-grounded-fetch-embed-grounding*
*Completed: 2026-06-19*
