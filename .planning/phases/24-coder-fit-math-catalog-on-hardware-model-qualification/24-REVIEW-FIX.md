---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
fixed_at: 2026-06-13T00:00:00Z
review_path: .planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-REVIEW.md
iteration: 1
findings_in_scope: 3
fixed: 3
skipped: 0
status: all_fixed
---

# Phase 24: Code Review Fix Report

**Fixed at:** 2026-06-13T00:00:00Z
**Source review:** .planning/phases/24-coder-fit-math-catalog-on-hardware-model-qualification/24-REVIEW.md
**Iteration:** 1

**Summary:**
- Findings in scope: 3 (WR-01, WR-02, WR-03 — the three WARNINGs; Info findings IN-01/IN-02/IN-03 explicitly out of scope)
- Fixed: 3
- Skipped: 0

All three warnings cluster on the external-catalog trust boundary
(`validateCoderEntries` in `internal/catalog/load.go`). WR-01 and WR-02 were
folded into a single `<= 0` KV-dimension guard per the review's own
recommendation. WR-03 relaxes the temperature lower bound to inclusive of zero.

## Fixed Issues

### WR-01 + WR-02: `validateCoderEntries` does not validate KV dimensions / negative dims silently saturate

**Files modified:** `internal/catalog/load.go`, `internal/catalog/catalog_test.go`
**Commit:** 3df4687
**Applied fix:** Added a single guard immediately after the `agent_ctx` check:
`if m.NLayers <= 0 || m.NKVHeads <= 0 || m.HeadDim <= 0 || m.KVBytesPerElem <= 0`
returns a `fmt.Errorf` that names the offending entry id and lists all four bad
values (mirroring the existing refuse-whole-and-name discipline). The `<= 0`
check catches both the zeroed/omitted case (WR-01, which would collapse the KV
term to 0 and over-qualify the entry for `Residency:"swap"`) and the
negative-int-decoded-to-huge-uint64 case (WR-02, previously silently saturated
to `MaxUint64`) in one guard. Updated the function doc comment to mention the
KV-dimension invariant.

Test coverage: parameterized the existing `TestLoadCoderValidationRefusesNeverClamps`
fixture builder (`buildCoderCatalog` + `coderDims`/`goodCoderDims` helpers) over
the KV dimensions, and added refusal cases: `n_layers zero`, `n_kv_heads zero`,
`head_dim zero`, `kv_bytes_per_elem zero` (WR-01) and `n_layers negative`,
`head_dim negative` (WR-02). Each asserts the whole external catalog is refused,
the warning names the offending entry, and `Load` falls back to the embedded seed.

### WR-03: `validateCoderEntries` rejects `temperature == 0` (legitimate greedy decoding)

**Files modified:** `internal/catalog/load.go`, `internal/catalog/catalog_test.go`
**Commit:** 3df4687
**Applied fix:** Changed the temperature guard lower bound from
`s.Temperature <= 0` to `s.Temperature < 0` (llama.cpp treats `temp <= 0` as
greedy/deterministic decoding — a legitimate coder preset). Updated the error
message range from `(0, 2]` to `[0, 2]` and added an inline doc comment
explaining the inclusive lower bound.

Test coverage: removed the now-invalid `"temperature zero"` refusal case from
`TestLoadCoderValidationRefusesNeverClamps`, and added a new dedicated test
`TestLoadCoderAcceptsGreedyTemperature` asserting that an external coder entry
with `temperature: 0` is now ACCEPTED (no warnings, the external catalog is used,
and the `0` temperature is preserved on the decoded entry).

## Verification

- gofmt clean on both modified files.
- `make check` (go vet + full `go test ./...`) green across all 22 packages,
  including `internal/inference` (TestSeamGrepGate) and `internal/catalog`.
- No podman/digest/backend-image literal introduced into either file.
- Out-of-scope files untouched: `internal/catalog/seed.json`,
  `cmd/villa/testdata/recommend.golden.json` (no golden re-freeze — rules out IN-01).
- IN-02 and IN-03 left unaddressed per scope (advisory/optional only).

---

_Fixed: 2026-06-13T00:00:00Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
