---
phase: 09-villa-bench-honest-a-b
plan: 01
subsystem: api
tags: [llm, openai-compatible, llama.cpp, timings, bench, throughput, go, stdlib]

# Dependency graph
requires:
  - phase: 02-villa-inference-vulkan
    provides: "internal/llm.OpenAIClient + StreamChat (the request-build/auth/bounded-error scaffolding Complete clones)"
provides:
  - "internal/llm.OpenAIClient.Complete — non-streaming /v1 completion returning per-request server-computed timings (pp/tg separated)"
  - "llm.Timings type — the six llama.cpp /v1 timing extension fields"
  - "completeRequest/completeResponse wire types carrying fixed max_tokens/seed/temperature and the top-level timings block"
affects: [09-02-bench-core, 09-03-bench-live-wiring, bench, throughput-delta]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Non-streaming sibling of an SSE method: same model/messages guards + bounded non-200 error, JSON body instead of SSE"
    - "Response struct deserializes ONLY the numeric timings block (field-set discipline, T-09-01) — never the prompt/sampling params"

key-files:
  created: []
  modified:
    - internal/llm/openai.go
    - internal/llm/openai_test.go

key-decisions:
  - "Complete returns Timings (not content) — its whole purpose is the per-request timings block StreamChat discards; choices/content intentionally not deserialized (T-09-01 info-disclosure mitigation)"
  - "Fixed params (max_tokens=n_predict, seed, temperature) ride the wire on a dedicated completeRequest type so every bench run is reproducible"
  - "Non-200 bounded by io.LimitReader(resp.Body, 2048) verbatim from StreamChat (T-09-02 DoS mitigation); no new dependency — stdlib only, go.mod byte-unchanged"

patterns-established:
  - "Pattern: a measurement leaf stays literal-free of backend markers (internal/llm is inside the TestSeamGrepGate internalRoot walk) — Complete only knows /v1"

requirements-completed: [BENCH-01]

# Metrics
duration: 6min
completed: 2026-06-06
---

# Phase 9 Plan 01: OpenAIClient.Complete (Honest Measurement Leaf) Summary

**Non-streaming `OpenAIClient.Complete` that drives a `stream:false` /v1 completion with fixed (max_tokens, seed, temperature) params and returns the server-computed per-request `timings` block (prompt-processing and token-generation rates already separated) — the honest, per-run pp/tg throughput source Plans 02–03 read from.**

## Performance

- **Duration:** 6 min
- **Started:** 2026-06-06T16:06Z
- **Completed:** 2026-06-06T16:12Z
- **Tasks:** 1 (TDD: RED → GREEN, no REFACTOR needed)
- **Files modified:** 2

## Accomplishments
- `llm.Timings` type with the six llama.cpp `/v1` timing extension fields (`prompt_n`/`prompt_ms`/`prompt_per_second` + `predicted_n`/`predicted_ms`/`predicted_per_second`).
- `OpenAIClient.Complete(ctx, req, nPredict, seed, temp) (Timings, error)` — a non-streaming sibling of `StreamChat` that captures the timings block `StreamChat` discards.
- `completeRequest`/`completeResponse` wire types: fixed params on the request, only the top-level `timings` deserialized on the response (field-set discipline).
- Four-case fixture test (timings-parse, params-on-wire, bounded non-200 error, no-model guard) — all green; backend-neutrality seam (`TestSeamGrepGate`) stays green; `go.mod` byte-unchanged.

## Task Commits

Each task was committed atomically (TDD cycle):

1. **Task 1 (RED): failing tests for Complete timings parse** - `c1b3e7f` (test)
2. **Task 1 (GREEN): Complete non-streaming method + Timings type** - `79bb774` (feat)

**Plan metadata:** _(this commit)_ (docs: complete plan)

## Files Created/Modified
- `internal/llm/openai.go` - Added `Timings` struct, `completeRequest`/`completeResponse` wire types, and `Complete` method (mirrors `StreamChat` guards + bounded error; `Accept: application/json`, `stream:false`, JSON-decode the top-level timings).
- `internal/llm/openai_test.go` - Added `TestCompleteParsesTimings`, `TestCompleteParamsOnWire`, `TestCompleteUpstreamError`, `TestCompleteRequiresModel` (httptest fixtures, mirror the existing `StreamChat` patterns).

## Decisions Made
- **Complete returns `Timings`, not content** — bench needs the per-request pp/tg block, not the assistant text; `completeResponse` deserializes only `timings` (T-09-01 information-disclosure mitigation: the prompt/sampling params are never read back into a report).
- **Fixed params on a dedicated `completeRequest` type** — `max_tokens` (n_predict), `seed`, `temperature` ride the wire so every bench run is reproducible; the existing `wireRequest` (streaming) is left untouched.
- **Bounded non-200 error verbatim from `StreamChat`** (`io.LimitReader(resp.Body, 2048)`, T-09-02) — a wedged-200 timeout is Plan 03's run-context concern, not this leaf's.
- **No new dependency** — stdlib `encoding/json`/`net/http`/`io` only; `go.mod` byte-unchanged (T-09-SC).

## Deviations from Plan

None - plan executed exactly as written.

## Issues Encountered
None. The plan's `<read_first>` analog map (StreamChat lines 55-95) was exact; the implementation cloned the scaffolding with only the documented changes (stream flag, Accept header, JSON body, fixed params).

## User Setup Required
None - no external service configuration required.

## Known Stubs
None. `Complete` is fully wired against a live `/v1` response shape; the fixture test proves the timings parse end-to-end. (UAT note from PATTERNS: the `timings` block is a documented llama.cpp `/v1` extension — confirm presence on the real server at Phase 9 UAT; fall back to `/completion` if absent. This is a host-validation item, not a stub.)

## Threat Surface Scan
No new security-relevant surface beyond the plan's `<threat_model>`. Both mitigate-disposition threats were honored in code:
- **T-09-01 (info disclosure):** `completeResponse` unmarshals only the numeric `timings` fields.
- **T-09-02 (DoS):** non-200 path bounded by `io.LimitReader(resp.Body, 2048)`.

## Next Phase Readiness
- `llm.Complete` + `llm.Timings` are the measurement source ready for Plan 02 (`internal/bench` core, which reads pp/tg from `Timings`) and Plan 03 (live wiring, which clones `liveProve` and swaps `GenerationProbe` for the `Complete` call).
- Backend-neutrality seam green — `internal/bench` and `cmd/villa/bench.go` can compose this leaf without leaking backend markers through `internal/llm`.

## Self-Check: PASSED

- `internal/llm/openai.go` — FOUND (defines `func (c *OpenAIClient) Complete(` + `Timings` with six fields)
- `internal/llm/openai_test.go` — FOUND (four `TestComplete*` cases)
- Commit `c1b3e7f` (RED test) — FOUND
- Commit `79bb774` (GREEN feat) — FOUND
- `go test ./internal/llm/ -run TestComplete -count=1` — 4 passed
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — passed
- `go build ./... && go vet ./...` — clean; `go.mod` byte-unchanged

---
*Phase: 09-villa-bench-honest-a-b*
*Completed: 2026-06-06*
