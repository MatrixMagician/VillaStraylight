---
phase: 24-coder-fit-math-catalog-on-hardware-model-qualification
reviewed: 2026-06-13T00:00:00Z
depth: deep
files_reviewed: 15
files_reviewed_list:
  - internal/catalog/catalog.go
  - internal/catalog/load.go
  - internal/catalog/seed.json
  - internal/catalog/catalog_test.go
  - internal/catalog/testdata/good-catalog.json
  - internal/catalog/testdata/multishard-catalog.json
  - internal/catalog/testdata/schema2-catalog.json
  - internal/catalog/testdata/schema3-external.json
  - internal/recommend/recommend.go
  - internal/recommend/coder.go
  - internal/recommend/coder_test.go
  - internal/recommend/recommend_test.go
  - cmd/villa/recommend.go
  - cmd/villa/recommend_test.go
  - cmd/villa/testdata/recommend.golden.json
findings:
  critical: 0
  warning: 3
  info: 3
  total: 6
status: issues_found
---

# Phase 24: Code Review Report

**Reviewed:** 2026-06-13T00:00:00Z
**Depth:** deep
**Files Reviewed:** 15
**Status:** issues_found

## Summary

Reviewed the schema 2→3 catalog evolution and the new `recommend` coder fit stage at deep depth, tracing the call chain `Pick → pickCoder → kvCacheBytes/addSaturating/headroomBytes` and the external-catalog trust boundary `Load → loadExternal → validateCoderEntries`. The core mechanics are sound and well-guarded: the chat pick is provably unaffected by coder entries (D-03 bit-identical test holds), the coder fit is locked to `m.AgentCtx` and is unreachable from the `--ctx` override by construction (D-04), the saturating fit math (`bits.Mul64`/`bits.Add64`) correctly fails closed on overflow, the residency derivation is a pure inequality output, and the coder block is unconditionally stamped on every return path including the no-envelope refusal (Pitfall 6). No backend marker literals leak into these files (TestSeamGrepGate is satisfied). The full suite passes (464 tests).

The defects found cluster on the **external-catalog input-validation boundary** (`validateCoderEntries`), which Phase 24 explicitly introduced to harden untrusted catalog input but which has two gaps that let an attacker-supplied or malformed coder entry produce an optimistic/incorrect fit, plus one overly-strict rejection of a legitimate sampling value. None rise to BLOCKER because the embedded seed (the only catalog shipped by default) is author-validated and correct, and the worst external-catalog outcome is a too-optimistic "swap" recommendation rather than a crash or silent CPU fallback.

## Warnings

### WR-01: `validateCoderEntries` does not validate KV dimensions — a coder entry with zeroed `n_layers`/`n_kv_heads`/`head_dim`/`kv_bytes_per_elem` passes validation and produces an optimistic (under-estimated) fit

**File:** `internal/catalog/load.go:83-105`
**Issue:** `validateCoderEntries` is the input-validation pass on the external-catalog trust boundary (T-24-02). It checks `AgentCtx > 0` and the `AgentSampling` ranges, but it does **not** check that the KV-cache dimensions are positive. A `role:"coder"` entry that omits `n_layers` (or sets it to `0`/missing) decodes to `NLayers == 0`, which drives `kvCacheBytes` (`internal/recommend/kv.go:30-44`) to a product of `0` — the KV term vanishes. `pickCoder` (`internal/recommend/coder.go:83`) then computes `total = weights + 0 + headroom`, which can clear the envelope and qualify the entry for `Residency: "swap"` with a materially under-estimated footprint. Because "swap" is documented as requiring a *proven* standalone fit and feeds the Phase-25 `--ctx-size` render, an optimistic qualification here is a future silent-OOM source — exactly the failure class the saturating math elsewhere exists to prevent. The agent_ctx and sampling ranges are validated but the term that actually sizes KV memory is not.
**Fix:** Extend the coder-entry validation to reject non-positive KV dimensions (the same invariant `TestLoadEmbeddedSeed` asserts for the seed), so a malformed external coder entry is refused whole rather than qualified optimistically:
```go
func validateCoderEntries(c Catalog) error {
	for _, m := range c.Models {
		if m.Role != "coder" {
			continue
		}
		if m.AgentCtx <= 0 {
			return fmt.Errorf("coder entry %q: agent_ctx %d out of range (must be > 0)", m.ID, m.AgentCtx)
		}
		if m.NLayers <= 0 || m.NKVHeads <= 0 || m.HeadDim <= 0 || m.KVBytesPerElem <= 0 {
			return fmt.Errorf("coder entry %q: missing/invalid KV dimension (n_layers=%d n_kv_heads=%d head_dim=%d kv_bytes_per_elem=%d — all must be > 0)",
				m.ID, m.NLayers, m.NKVHeads, m.HeadDim, m.KVBytesPerElem)
		}
		// ... existing AgentSampling checks ...
	}
	return nil
}
```

### WR-02: External coder entry with negative `weight_bytes` / KV dimensions is not rejected; negative KV dims fail closed but a negative-decoded value is silently accepted as a huge `uint64`

**File:** `internal/catalog/load.go:83-105`, `internal/recommend/kv.go:32-37`
**Issue:** `WeightBytes`/`SizeBytes` are `uint64` so JSON cannot supply a negative there, but `NLayers`/`NKVHeads`/`HeadDim`/`KVBytesPerElem`/`AgentCtx` are `int`. A malicious external catalog can set e.g. `"n_layers": -1`; `kvCacheBytes` casts via `uint64(m.NLayers)`, turning `-1` into `18446744073709551615` and saturating the product to `MaxUint64`. That *happens* to fail closed (the entry never fits), so it is not a correctness BLOCKER — but it is accepted silently with no warning naming the offending entry, which contradicts the refuse-whole-and-name-the-entry discipline the rest of `validateCoderEntries` follows (T-24-02). The combined effect with WR-01 is that the validation pass enforces ranges on the cosmetic sampling knobs while leaving every memory-sizing integer unchecked.
**Fix:** Validate the signed dimension fields explicitly (folds into the WR-01 fix above by checking `<= 0`, which catches both zero and negative). A coder entry with any non-positive sizing integer should be refused with a named warning, not silently saturated.

### WR-03: `validateCoderEntries` rejects `temperature == 0`, refusing a legitimate deterministic-decoding preset and falling the whole external catalog back to the seed

**File:** `internal/catalog/load.go:93`
**Issue:** The temperature guard is `s.Temperature <= 0 || s.Temperature > 2` with the documented range `(0, 2]`. `temperature: 0` is greedy/deterministic decoding — a legitimate and common preset for coding agents that want reproducible output — yet it is treated as out-of-range, invalidating the *entire* external catalog and silently falling back to the embedded seed. Because the failure mode is whole-catalog refusal (not a clamp), a user who hand-authors a valid deterministic coder preset silently loses their entire external catalog with only a warning. The lower bound should be inclusive of zero.
**Fix:** Allow `temperature == 0` (llama.cpp treats `temp <= 0` as greedy):
```go
case s.Temperature < 0 || s.Temperature > 2:
	return fmt.Errorf("coder entry %q: agent_sampling temperature %g out of range [0, 2]", m.ID, s.Temperature)
```
Update the accompanying doc comment to `[0, 2]`.

## Info

### IN-01: Golden coder block uses a `headroom_bytes` inconsistent with the chat block for the same envelope (cosmetic, fixture-only)

**File:** `cmd/villa/testdata/recommend.golden.json:8,22`, `cmd/villa/recommend_test.go:42`
**Issue:** The frozen golden has chat `headroom_bytes: 8053063680` and coder `headroom_bytes: 8057925795` against the same `usable_envelope_bytes`. A real `Pick` computes both as `headroomBytes(envelope)` (12% of the *same* post-reservation envelope), so they would be byte-identical. The fixture is hand-built (`fixtureRecommendation()` does not call `Pick`) and its terms sum correctly (`17665334432 + 6442450944 + 8057925795 == 32165711171`), so this is harmless — but it is a subtly misleading frozen contract: a downstream consumer reverse-engineering "headroom = 12% of envelope" from the golden would see two different headrooms for one envelope.
**Fix:** Regenerate the fixture's coder `headroom_bytes`/`total_bytes` from the same `headroomBytes(usable_envelope_bytes)` the chat block uses (or derive the whole fixture from a real `Pick` call) and re-freeze with `-update`, so the golden models a self-consistent envelope.

### IN-02: Embedded-seed coder entries are exempt from `validateCoderEntries`, so a future bad seed agent_ctx/dimension would only be caught by tests, not at load

**File:** `internal/catalog/load.go:74-105`
**Issue:** By design the seed is exempt from runtime validation (it is compiled in and guarded by `TestLoadSeedCoderVerifiedDims` / `TestLoadEmbeddedSeed`). This is a reasonable trust decision, but it means the *only* guard against a future seed-authoring mistake (e.g. an `agent_ctx: 0` coder entry that would `kvCacheBytes`→0 and falsely qualify "swap") is the test suite, not defense-in-depth at load. Worth a note since the validation function already exists and applying it to the seed would be cheap insurance.
**Fix:** Optionally run a `validateCoderEntries`-equivalent assertion over `decodeSeed()` output in a build-time or `init`-adjacent test (some already exist); no production change required.

### IN-03: `schemaMismatchWarning` `got == SupportedSchema` case is unreachable dead branch

**File:** `internal/catalog/load.go:65-72`
**Issue:** `schemaMismatchWarning` is only called from `Load` when `ext.SchemaVersion != SupportedSchema` (load.go:43), so the function never sees an equal value. The `switch` handles `got > SupportedSchema` and a `default` (older). This is correct and harmless — just a minor note that the function's domain is narrower than its signature suggests; the `default` label conflates "older" with the impossible "equal".
**Fix:** None required. Optionally document the precondition (`got != SupportedSchema`) in the doc comment for clarity.

---

_Reviewed: 2026-06-13T00:00:00Z_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
