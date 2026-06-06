---
phase: 10-backend-tok-s-surfacing
plan: 02
subsystem: recommend
tags: [recommend, rocm-advice, honesty-contract, golden, append-only]
requires:
  - "detect.HostProfile.rocm_readiness (read-only, in hand — no new I/O)"
  - "internal/status.foldROCmReadiness (Known-first worst-wins discipline mirrored)"
provides:
  - "recommend.ROCmAdvice typed enum (ready/worth-trying/verify-with-bench) + honesty-safe ROCmNote"
  - "recommend.Recommendation tail fields: ROCmAdvice, ROCmNote, SchemaVersion(=1)"
  - "recommend.deriveROCmAdvice — pure fold of the 5 rocm_readiness signals into advice + Note"
  - "recommend.finalizeRecommendation — stamps SchemaVersion + advice on every Pick return path"
  - "villa recommend table + --json now surface the ROCm advice (gated, additive)"
affects:
  - "internal/recommend/recommend.go"
  - "cmd/villa/recommend.go"
  - "cmd/villa/testdata/recommend.golden.json"
tech-stack:
  added: []
  patterns:
    - "Append-only struct growth (omitempty advice fields + schema_version last)"
    - "Pure advice derivation inside Pick (no new I/O, no new arg) — table-testable"
    - "Locked honesty-safe copy enforced by test (no faster/guaranteed/speed-up)"
    - "Golden re-frozen exactly once as a reviewed pure-addition diff"
key-files:
  created: []
  modified:
    - "internal/recommend/recommend.go"
    - "internal/recommend/recommend_test.go"
    - "cmd/villa/recommend.go"
    - "cmd/villa/recommend_test.go"
    - "cmd/villa/testdata/recommend.golden.json"
decisions:
  - "Advice derived AFTER Backend is set via finalizeRecommendation; rec.Backend never reassigned (REC-04, no auto-switch)"
  - "Unknown-wins-over-not-ready (D-04): a single unevaluable readiness signal keeps the host at verify-with-bench, never a confidently-withheld not-ready"
  - "ROCmAdviceReady const defined for contract completeness but not emitted by the Phase-10 fold (only worth-trying/verify-with-bench/withheld are derived)"
  - "Withheld-advice Note names the first Known-bad blocker and points to 'villa status' (not bench, since the host is confidently not-ready)"
  - "SchemaVersion pinned to 1 in the golden fixture (fixture bypasses Pick); surfaces unconditionally in --json"
requirements-completed: [REC-05]
metrics:
  duration: "~3m"
  completed: "2026-06-06T20:15:17Z"
  tasks: 2
  files: 5
---

# Phase 10 Plan 02: Backend-aware recommend (ROCm advice) Summary

Made `villa recommend` backend-aware by appending a typed `ROCmAdvice` enum + an honesty-safe Note + an additive `schema_version` to the `recommend.Recommendation` tail, derived purely inside `Pick` from the `HostProfile.rocm_readiness` already in hand — the recommended `Backend` stays `vulkan` and the advice never auto-switches and never promises a speed-up (on-hardware Δtg −11.15), with the recommend golden re-frozen exactly once as a pure-addition diff (REC-05).

## What was built

- **`internal/recommend/recommend.go`**
  - `type ROCmAdvice string` with consts `ROCmAdviceReady="ready"`, `ROCmAdviceWorthTrying="worth-trying"`, `ROCmAdviceVerifyBench="verify-with-bench"`.
  - Tail-appended to `Recommendation` (after `Alternatives`): `ROCmAdvice` (`json:"rocm_advice,omitempty"`), `ROCmNote` (`json:"rocm_note,omitempty"`), and `SchemaVersion int` (`json:"schema_version"`) as the LAST tagged field. `recommendSchemaVersion = 1` const.
  - `deriveROCmAdvice(detect.ROCmReadiness)` — pure fold mirroring `status.foldROCmReadiness` (Known-first, worst-wins, unknown-wins-over-not-ready): all-good → `worth-trying` + locked honesty Note; any unevaluable → `verify-with-bench` + verify Note; all-Known + any-bad → withheld (`""`) + a Note naming the first blocker.
  - `finalizeRecommendation` stamps `SchemaVersion` unconditionally and the advice on **every** `Pick` return path (refusal, no-fit, override, best) — runs AFTER `Backend` is set and never reassigns `rec.Backend`.
  - Locked honesty-safe Note copy: `"ROCm: worth trying for prompt-heavy workloads — token generation may not improve (and can regress vs vulkan). Verify on your model with: villa bench --ab"`.
- **`cmd/villa/recommend.go`** — `renderRecommendTable` renders `ROCm advice: <advice>` + the Note after the Notes loop, gated on `rec.ROCmAdvice != ""`. `--json` carries the new fields automatically (`renderRecommend` already JSON-encodes `rec`).
- **`cmd/villa/testdata/recommend.golden.json`** — re-frozen once: appended `"schema_version": 1` only.
- **Tests** — advice-derivation table (all-good / any-unknown / known-bad / bad+unknown), Backend-stays-vulkan for every advice value, Note-honesty (contains verify/bench, never faster/guaranteed/speed-up), and off-hardware → verify-with-bench.

## Tasks

| Task | Name | Commits | Files |
| ---- | ---- | ------- | ----- |
| 1 (TDD) | ROCmAdvice enum + Note + SchemaVersion; pure derivation in Pick | 0f5ce1b (RED test), a46df22 (GREEN) | internal/recommend/recommend.go, recommend_test.go |
| 2 | Render advice in table; re-freeze golden once | 230b92f | cmd/villa/recommend.go, recommend_test.go, testdata/recommend.golden.json |

## Verification

- `go build ./...` — success; `go vet ./...` — no issues.
- `go test ./...` — **546 passed in 16 packages**.
- `go test ./internal/recommend/ -run Advice` — 7 passed (derivation table + honesty + backend-stays).
- `go test ./cmd/villa/ -run TestRecommendJSONGolden` — passes WITHOUT `-update` (golden matches encoder bytes).
- `go test ./internal/inference/ -run TestSeamGrepGate` — green (no marker literals leaked into recommend code; bare word "ROCm" only).
- `git diff --quiet cmd/villa/testdata/detect.golden.json` — empty (byte-identical).
- `git diff --quiet cmd/villa/testdata/status.json.golden` — empty (Plan 01's golden, untouched).
- `git diff --quiet go.mod` — clean (no new deps).
- Recommend golden diff = pure tail-addition (`schema_version: 1`); no existing key reordered/renamed/retyped.

## Threat mitigations applied

| Threat ID | Mitigation |
| --------- | ---------- |
| T-10-05 (dishonest advice) | Locked Note copy points to `villa bench --ab`; test asserts Note contains verify/bench and NONE of faster/guaranteed/speed-up. |
| T-10-06 (advice changes pick) | `finalizeRecommendation` runs after `Backend` is set and never reassigns `rec.Backend`; test asserts `Backend == "vulkan"` for every advice value. |
| T-10-07 (marker-literal leak) | Bare word "ROCm" only; `grep` confirms no `ROCm0`/`HSA_OVERRIDE`/image-tag literals; `TestSeamGrepGate` green. |
| T-10-SC (package installs) | None; `go.mod` frozen (`git diff` empty). |

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Test correctness] `bad+unknown` advice expectation corrected to verify-with-bench**
- **Found during:** Task 1 (writing the RED derivation table).
- **Issue:** An initial test case expected a mixed Known-bad + unevaluable readiness to yield withheld (`""`) advice. This contradicts the locked D-04 no-false-green rule (UNKNOWN wins over not-ready).
- **Fix:** Corrected the case to expect `verify-with-bench` (unknown-wins), matching `status.foldROCmReadiness` semantics, and added an explicit comment guarding the worst-wins ordering.
- **Files modified:** internal/recommend/recommend_test.go
- **Commit:** a46df22

**2. [Rule 1 - Test robustness] Honesty "verify" check made case-insensitive**
- **Found during:** Task 1 (GREEN).
- **Issue:** The locked Note copy capitalizes "Verify on your model with: ..."; a literal lowercase `strings.Contains(note, "verify")` failed against the locked copy.
- **Fix:** Lower-cased the Note before the verify/bench substring check (intent preserved — the honesty assertion still requires the verification pointer; the banned-word check stays literal).
- **Files modified:** internal/recommend/recommend_test.go
- **Commit:** a46df22

## Known Stubs

None. The withheld-advice path is fully derived; `ROCmAdviceReady` is an intentionally-defined-but-unemitted const documented as contract completeness (the Phase-10 fold derives worth-trying / verify-with-bench / withheld only).

## Self-Check: PASSED

- FOUND: internal/recommend/recommend.go (type ROCmAdvice, deriveROCmAdvice, finalizeRecommendation)
- FOUND: cmd/villa/recommend.go (ROCm advice rendering)
- FOUND: cmd/villa/testdata/recommend.golden.json (schema_version: 1)
- FOUND commit 0f5ce1b (RED test)
- FOUND commit a46df22 (GREEN impl)
- FOUND commit 230b92f (render + golden)
