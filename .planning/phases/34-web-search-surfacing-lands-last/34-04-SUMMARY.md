---
phase: 34-web-search-surfacing-lands-last
plan: 04
subsystem: doctor
tags: [surfacing, doctor, schema-bump, golden, verifystate, web-search, offload-asserting, SURF-06, honesty-by-construction]
status: complete

# Dependency graph
requires:
  - phase: 34-01
    provides: "internal/verifystate (State{Verdict,CheckedAt}, fail-closed Load, VerifyStatePath) — the cached verify-search result the egress-proof finding derives from"
  - phase: 34-03
    provides: "status.Run villa-searxng/villa-websafe service rows (folded for free by doctor's existing healthFinding loop) + the 24h freshness-window value mirrored in the egress mapping"
provides:
  - "doctor.reportSchemaVersion 2→3 (doctor's OWN version, INDEPENDENT of status's 5)"
  - "doctor.Deps additions (nil-safe): SearchEgressProof func() inference.Verdict, SearchResidencyUnderLoad func() inference.Verdict"
  - "searchEgressFinding (tri-state egress proof) + searchResidencyFinding (offload-asserting) finding builders"
  - "cmd/villa runSearchResidencyUnderLoad (clone of runAgentResidencyUnderLoad; bounded chat-completion drive; villa-searxng+villa-websafe precondition gate)"
  - "cmd/villa liveSearchEgressProof (cached verifystate.Load → tri-state, 24h freshness gate) + liveSearchResidencyUnderLoad live seams, wired only when web_search_enabled"
  - "re-frozen doctor JSON goldens (doctor.json.golden + doctor-memory.json.golden + doctor-agent.json.golden) at schema 3 — schema-only diff"
affects: ["34-05 (dashboard Web Search panel — independent of doctor; no contract coupling)"]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Append-only + schema-bump + golden re-freeze (doctor 2→3, SchemaVersion stays last; nil seams keep web-off byte-identical except the bump)"
    - "Egress-proof finding tri-state derived from a cached security proof with a freshness gate (never green by default, never from a config bool)"
    - "Offload-asserting residency-under-load applied to the search path: in-flight-only sampling, confident CPU fallback → FAIL dominating HTTP-200, not-in-flight → typed-Unknown"
    - "Clone-don't-import live residency proof: runSearchResidencyUnderLoad is a verbatim-in-shape clone of runAgentResidencyUnderLoad with ONLY the drive swapped + precondition gate extended"

key-files:
  created: []
  modified:
    - internal/doctor/doctor.go
    - internal/doctor/doctor_test.go
    - cmd/villa/doctor.go
    - cmd/villa/doctor_test.go
    - cmd/villa/testdata/doctor.json.golden
    - cmd/villa/testdata/doctor-memory.json.golden
    - cmd/villa/testdata/doctor-agent.json.golden

key-decisions:
  - "DEFINED SEARCH-LOAD DRIVE (Open Q2 resolution): a bounded chat completion (max_tokens=16, stream=false) POSTed to the in-network villa-llama /v1/chat/completions while villa-searxng/villa-websafe are up — the cheapest honest drive that keeps villa-llama DECODING under search load. The model id is JSON-marshaled, never interpolated (T-34-09)."
  - "searxng/websafe READINESS needs NO new doctor code: status.Run already emits dedicated villa-searxng/villa-websafe rows (Plan 03) which flow through doctor's existing healthFinding loop (composition, RESEARCH A1). Nil-safe by construction (web off → no rows → no findings)."
  - "GUARD HEALTH is a documented OMISSION (accepted scope limit, SURF-06): no host-side source exists (per-request guard metadata is in-container only). Building a guard counter/health pipeline = NEW behavior = OUT OF SCOPE for this surfacing phase. NO guard-health finding is emitted — never a fabricated PASS/0."
  - "searchVerifyFreshnessWindow = 24h is duplicated in cmd/villa (the status core's verifyFreshnessWindow is unexported) so the doctor egress finding and the status outbound_bounded indicator apply the SAME gate; a security property is never trusted indefinitely from a stale cache (T-34-12)."
  - "Both web-search goldens beyond doctor.json.golden (memory + agent JSON) are re-frozen too: reportSchemaVersion is ONE const stamped on EVERY doctor Report, so all three JSON goldens carry it — each diff is schema-only (the same mechanical downstream consequence Plan 03 had with the status goldens)."

check-IDs:
  - "search-egress (Web-search outbound-bounded proof) — tri-state from the cached verify result"
  - "search-residency (Chat-model residency under search load) — offload-asserting"
  - "(searxng/websafe surface as health:villa-searxng.service / health:villa-websafe.service via the existing status fold)"

requirements-completed: [SURF-06]

# Metrics
duration: 11min
completed: 2026-06-21
tasks: 2
files: 7
---

# Phase 34 Plan 04: Doctor web-search fold (schema 2→3) Summary

**`villa doctor` folds web-search findings on doctor's OWN append-only schema bump (reportSchemaVersion 2→3, independent of status's 5): a tri-state egress-proof finding derived from the cached `villa verify search` result with a 24h freshness gate, an offload-asserting chat-model-residency-under-search-load check, searxng/websafe readiness folded for free from the status rows, every non-PASS carrying a remediation — with a single intentional re-freeze of the doctor JSON goldens.**

## Performance
- **Duration:** ~11 min
- **Tasks:** 2
- **Files modified:** 7

## Accomplishments
- doctor at schema 3 (doctor's OWN const, INDEPENDENT of status's 5); web-search-OFF doctor output is byte-identical to v2 EXCEPT the schema bump (nil seams emit no findings).
- `searchEgressFinding` maps the cached verify result tri-state (T-34-12): a fresh cached PASS → ready (PASS), a real recent non-PASS → degraded-with-reason (FAIL + remediation), stale/absent → typed-Unknown WARN + remediation. NEVER a config-bool PASS.
- `searchResidencyFinding` is offload-asserting (T-34-13): a confident CPU fallback under search load is a BLOCK-class FAIL that DOMINATES a healthy-looking HTTP-200; a not-in-flight / unevaluable signal → typed-Unknown WARN (never an idle-sampled false-green).
- `runSearchResidencyUnderLoad` is a verbatim-in-shape clone of `runAgentResidencyUnderLoad`: drive swapped to a bounded chat completion, precondition gate extended with villa-searxng + villa-websafe; in-flight-only sampling (settle→sample-if-still-running→join); strictly read-only (doctor never starts a service).
- Every non-PASS web-search finding carries a non-empty Remediation (D-11).
- Single intentional golden re-freeze: the web-off `doctor.json.golden` diff is exactly schema_version 2→3 (no field reorder, no web_search block — the fixture is web-off). The memory + agent JSON goldens carry the same const and re-froze with schema-only diffs.

## Task Commits
1. **Task 1: doctor core web-search fold + reportSchemaVersion 2→3 (nil-safe, remediation-on-every-non-PASS) [TDD]** — `e8d2e31` (feat)
2. **Task 2: live residency-under-search-load proof + egress-proof seam + doctor golden re-freeze [TDD]** — `fbcda72` (feat)

## Open Q2 Resolution — Defined Search-Load Drive
The chosen drive is **a bounded chat completion** (`max_tokens=16`, `stream=false`) POSTed to the in-network `villa-llama /v1/chat/completions` (via `orchestrate.LlamaInNetworkEndpoint()`) while villa-searxng/villa-websafe are up — the cheapest honest drive that keeps villa-llama **decoding** so the residency sample observes the served model under real load, without an unbounded generation. The model id is JSON-marshaled (never interpolated). Precondition gate = villa-llama + villa-searxng + villa-websafe active AND BackendFor + ModelFile resolvable, else typed-Unknown.

## Accepted Scope Limit (stated, not silent)
- **Guard health** has NO host-side persisted source (per-request guard metadata lives in-container only; no host aggregate). Building a guard counter/health pipeline = NEW behavior = OUT OF SCOPE for this surfacing phase (SURF-06). **NO guard-health finding is emitted** — a documented omission, never a fabricated PASS/0.

## On-Hardware Exercise (live Strix Halo, full web stack up)
With `web_search_enabled = true` and villa-llama/villa-searxng/villa-websafe all `active`, `./villa doctor --json` reported:
- `schema_version: 3`, `overall: WARN`
- `health:villa-searxng.service` PASS, `health:villa-websafe.service` PASS (readiness folded from the status rows — no new doctor code)
- `search-egress` WARN — typed-Unknown ("the last `villa verify search` is stale or absent"): there is NO cached verify-search store (confirmed by inspecting `$XDG_DATA_HOME/villa/verify-search-state.json`), so the egress proof honestly refuses to read green (T-34-12 confirmed live — never config-bool-derived).
- `search-residency` WARN — typed-Unknown ("no search-augmented chat round stayed in flight long enough to sample"): the in-flight discipline correctly refused to idle-sample a false-green (T-34-13 confirmed live). A confident FAIL on a real CPU fallback is exercised by the off-hardware truth-table tests.

## Verification
- `go test ./internal/doctor/ ./cmd/villa/ ./internal/inference/ -run 'TestAggregateWebSearch|TestSearchEgressFinding|TestSearchResidencyFinding|TestSearchResidencyFoldedFailDominatesHealth|TestWebSearchFindingsHaveRemediation|TestDoctorSchemaVersionIsThree|TestDoctor|TestSearchResidency|TestSeamGrepGate|TestRunSearchResidency|TestLiveDoctorDepsWiresWebSearchSeams'` — all green.
- `make check` (vet + full test) — green across all packages.
- `./villa doctor --json` on the live Strix Halo box — schema 3, honest web-search findings (see above).

### Acceptance gates
- `grep -v '^//' internal/doctor/doctor.go | grep -c 'reportSchemaVersion = 3'` → 1 (no non-comment `= 2`). ✓
- `grep -c 'ROCm0\|Vulkan0\|HSA_OVERRIDE' cmd/villa/doctor.go` → 0 (no marker literal in the new seam). ✓ (the 2 matches in internal/doctor/doctor.go are pre-existing NEGATION doc comments — "never typing Vulkan0/ROCm0"; TestSeamGrepGate, the authoritative gate, is green.)
- `grep -n 'runSearchResidencyUnderLoad' cmd/villa/doctor.go` matches; `grep -n 'SearXNGContainerUnitName\|WebsafeContainerUnitName' cmd/villa/doctor.go` matches (precondition gate via accessors, not literals). ✓
- `git diff cmd/villa/testdata/doctor.json.golden` shows ONLY the schema_version 2→3 bump (web-off fixture) — single intentional re-freeze, no field reorder. ✓
- `TestWebSearchFindingsHaveRemediation` asserts a non-empty Remediation for every non-PASS web-search finding; `TestSearchResidencyFinding` / `TestSearchResidencyFoldedFailDominatesHealth` prove a confident CPU-fallback Verdict produces a FAIL NOT masked by a health-200. ✓

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Updated doctor schema-version assertions 2→3 (test fixtures + JSON asserts + memory/agent JSON goldens)**
- **Found during:** Task 1 (the const bump) and Task 2 (the golden re-freeze)
- **Issue:** doctor's `reportSchemaVersion` is ONE const stamped on EVERY doctor Report. Bumping it 2→3 broke pre-existing tests/goldens that asserted the v2 contract: `TestDoctorSchemaVersionIsTwo`, the `schema_version": 2` `bytes.Contains` asserts in `TestDoctorJSON`/`TestDoctorMemoryJSON`/`TestDoctorAgentRender`, the `SchemaVersion: 2` test fixtures (`healthyReport`/`rocmSupersededReport`/`memoryHealthyReport`), and the `doctor-memory.json.golden`/`doctor-agent.json.golden` (which also serialize the doctor Report).
- **Fix:** Re-pointed `TestDoctorSchemaVersionIsTwo` → `TestDoctorSchemaVersionAgentFold` asserting `reportSchemaVersion` (the single source of truth, can't desync); bumped the fixtures + JSON asserts 2→3; re-froze all three doctor JSON goldens (each diff schema-only). No logic changed — these are contract-version asserts directly tracking the planned bump (the mechanical downstream consequence Plan 03 had with the status goldens).
- **Files modified:** internal/doctor/doctor_test.go, cmd/villa/doctor_test.go, cmd/villa/testdata/doctor-memory.json.golden, cmd/villa/testdata/doctor-agent.json.golden
- **Committed in:** `e8d2e31` (test asserts) and `fbcda72` (goldens)

---

**Total deviations:** 1 auto-fixed (1 blocking). **Impact:** mechanical consequence of the planned 2→3 contract bump; no scope creep, no new behavior.

## Known Stubs
None.

## Threat Flags
None — the new surface (two read-only doctor findings + a bounded in-network chat drive + a read-only cached-verify read) is fully covered by the plan's threat register (T-34-12..15). No new endpoint, auth path, or trust boundary was introduced; the egress-proof indicator is honest-by-construction (tri-state from the cached proof with a freshness gate, never a config bool) and the residency check is offload-asserting (confident CPU fallback → FAIL, never a false-green over a health-200).

## Self-Check: PASSED
- Modified files verified present: internal/doctor/doctor.go, internal/doctor/doctor_test.go, cmd/villa/doctor.go, cmd/villa/doctor_test.go, the 3 doctor*.json.golden, this SUMMARY.
- Commits verified in history: `e8d2e31` (Task 1), `fbcda72` (Task 2).

---
*Phase: 34-web-search-surfacing-lands-last*
*Completed: 2026-06-21*
