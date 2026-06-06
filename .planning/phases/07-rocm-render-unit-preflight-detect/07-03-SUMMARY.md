---
phase: 07-rocm-render-unit-preflight-detect
plan: 03
subsystem: detect
tags: [detect, rocm-readiness, json-contract, schema-bump, typed-optional]
requires:
  - "internal/detect HostProfile + typed-Optional value.go (Phase 1)"
  - "internal/inference rocm-7.2.4 pinned image (Phase 6/7 — mirrored, not imported)"
  - "internal/preflight kernel floor 6.18.4 (Phase 1 — re-expressed, not imported)"
provides:
  - "detect.ROCmReadiness struct (5 typed-Optional Bool fields)"
  - "HostProfile.ROCmReadiness (json:rocm_readiness) appended after the GPU block"
  - "hostProfileSchemaVersion = 2 (v1.1 contract marker)"
  - "computeROCmReadiness() — off-hardware unknowns -> UnknownBool"
  - "re-frozen detect.golden.json (schema 2 + rocm_readiness)"
affects:
  - "Phase-10 recommend + dashboard (consume rocm_readiness, detect v1.1 via schema_version)"
tech-stack:
  added: []
  patterns:
    - "Strictly-additive, schema-bumped JSON contract change (append after the GPU block, SchemaVersion stays last)"
    - "No-false-green typed-Optional (UnknownBool for undetectable off-hardware signals; never KnownBool(false))"
    - "Backend-seam literal isolation (image-tag + kernel-floor literals behind gpu_amd.go; readiness_rocm.go stays literal-free)"
key-files:
  created:
    - "internal/detect/readiness_rocm.go"
    - "internal/detect/readiness_rocm_test.go"
  modified:
    - "internal/detect/profile.go"
    - "internal/detect/profile_test.go"
    - "internal/detect/gpu_amd.go"
    - "internal/detect/detect.go"
    - "cmd/villa/detect_test.go"
    - "cmd/villa/testdata/detect.golden.json"
decisions:
  - "Off-hardware undetectable readiness signals (rocminfo_gfx1151, firmware_date_ok, hsa_override_viable) serialize as UnknownBool/UNSET — never a fabricated false (D-08)"
  - "image_policy_ok is config-driven against the resolved image string, NOT a host probe (Pitfall 5)"
  - "detect cannot import inference (cycle) nor preflight (cycle); the ROCm image-tag + kernel-floor literals are mirrored behind the gpu_amd.go seam, and the kernel comparator is re-expressed (not re-rolled with new semantics)"
requirements-completed: [DET-04]
metrics:
  duration: 14 min
  completed: 2026-06-06
  tasks: 3
  files: 8
---

# Phase 7 Plan 3: ROCm Readiness in `villa detect --json` Summary

Appended a nested `rocm_readiness` object to `villa detect --json` as a strictly-additive, schema-bumped (1→2) contract change — five typed-Optional readiness signals where off-hardware unknowns serialize as UNSET (never a false-green), with the golden re-frozen once as a purely additive diff.

## What Was Built

`villa detect --json` now reports a `rocm_readiness` sub-tree (`hsa_override_viable`, `firmware_date_ok`, `kernel_floor_ok`, `rocminfo_gfx1151`, `image_policy_ok`) appended AFTER the existing GPU block, with `hostProfileSchemaVersion` bumped 1→2 so the dashboard and Phase-10 `recommend` can detect the v1.1 contract. Every field is a typed-Optional `Bool`: an undetectable off-hardware signal is `UnknownBool` (serializes Known=false/UNSET), distinct from a real `false` (D-08 no-false-green). The frozen v1.0 key order is untouched — `SchemaVersion` stays last.

### Task 1 — Append `ROCmReadiness` struct + bump schema (commit `340a1d6`)
- Bumped `hostProfileSchemaVersion` 1→2 with an append-only rationale comment.
- Added the `ROCmReadiness` struct (5 typed-Optional `Bool` fields, keys per D-06).
- Appended `HostProfile.ROCmReadiness` (`json:"rocm_readiness"`) after the GPU block, before `SchemaVersion` (which remains the last field).
- Extended `TestHostProfileJSONRoundTrips`: a mixed Known/Unknown `rocm_readiness` survives marshal/unmarshal, `Raw` never serialized, schema==2.

### Task 2 — Compute `rocm_readiness` fields (commit `17956e6`)
- New `internal/detect/readiness_rocm.go`: `computeROCmReadiness(gfxID, kernel, resolvedImage)` returns `KnownBool` only when the source fact is `Known`, else `UnknownBool`.
- `rocminfo_gfx1151` / `firmware_date_ok` / `hsa_override_viable`: UNSET off-hardware (rocminfo absent → gfx id Unknown; firmware/override not probed).
- `kernel_floor_ok`: Known when `KernelVersion` is Known (real true/false vs the 6.18.4 gfx1151 floor); Unknown otherwise.
- `image_policy_ok`: config-driven against the resolved image string — pinned stable → `KnownBool(true)`, a `rocm7-nightlies` tag → `KnownBool(false)`.
- Wired the compute into `Probe()`.
- **Seam discipline:** the ROCm image-tag literals and the kernel-floor target live behind `gpu_amd.go` (`resolvedROCmImage` / `rocmImagePolicyOK` / `kernelMeetsROCmFloor`); `readiness_rocm.go` carries no backend literal. `TestSeamGrepGate` stays green.
- Added `readiness_rocm_test.go` asserting off-hardware UNSET, a known-below-floor `KnownBool(false)`, an Unknown-kernel UNSET, and the image-policy true/false pair.

### Task 3 — Re-freeze `detect.golden.json` (commit `f47782e`)
- Bumped `fixtureProfile()` to `SchemaVersion: 2`, populated `rocm_readiness` mixing Known (`kernel_floor_ok`, `image_policy_ok`) and Unknown (`rocminfo_gfx1151`, `firmware_date_ok`, `hsa_override_viable`) so the golden locks BOTH serialized shapes.
- Regenerated the golden once via `-update`. Reviewed diff is APPEND-ONLY: `schema_version` 1→2 and a `rocm_readiness` object appears after the GPU/memory blocks; no existing key moved or renamed.

## How It Works

```
Probe()
  ├── probeGPU()                       (seam: gpu_amd.go)
  ├── kernelVersion(...)
  └── computeROCmReadiness(            (readiness_rocm.go — literal-free)
        gpu.gfxID, kernel,
        resolvedROCmImage())            (seam: gpu_amd.go — image literal)
            ├── rocminfo_gfx1151  = Known iff gfxID.Known
            ├── kernel_floor_ok   = Known iff kernel.Known (kernelMeetsROCmFloor — seam)
            ├── firmware_date_ok  = UnknownBool (not probed off-hardware)
            ├── hsa_override_viable = UnknownBool (not probed off-hardware)
            └── image_policy_ok   = rocmImagePolicyOK(image)  (seam)
```

`detect` cannot import `inference` (would cycle: inference imports detect) nor `preflight` (preflight imports detect), so the ROCm image tag and the `6.18.4` kernel floor are mirrored behind the `gpu_amd.go` seam, and the dotted-version comparator is re-expressed there (same suffix-tolerant semantics as `preflight.compareVersions`, not re-rolled with new behavior).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `rocm-7.2.4` literal in a `profile.go` doc comment tripped `TestSeamGrepGate`**
- **Found during:** Task 2 verification.
- **Issue:** The `ImagePolicyOK` field doc comment in `profile.go` (a backend-neutral, non-seam file committed in Task 1) named the `rocm-7.2.4` image tag. `TestSeamGrepGate` matches image-tag literals in comments too, so it flagged the leak.
- **Fix:** Reworded the comment to "the pinned stable image" and noted the policy literals live behind the `gpu_amd.go` seam. Folded into the Task 2 commit since the gate dependency is introduced by Task 2.
- **Files modified:** `internal/detect/profile.go`
- **Commit:** `17956e6`

## Threat Model Coverage

- **T-07-01 (Information Disclosure / false-green):** mitigated — every `rocm_readiness` field is a typed-Optional; off-hardware undetectable signals serialize UNSET (Known=false), proven by the mixed Known/Unknown fixture + round-trip test and the off-hardware unit test.
- **T-07-02 (Tampering / silent reshape):** mitigated — schema bumped 1→2 and the byte-golden re-frozen as a reviewed append-only diff; no existing key reordered/renamed.
- **T-07-SC (package installs):** N/A — no package installs (stdlib `encoding/json` only).

## Verification

| Check | Result |
|-------|--------|
| `go test ./internal/detect/ -run 'RoundTrip\|Schema'` | PASS |
| `go test ./internal/detect/` | PASS (41 tests) |
| `go test ./internal/inference/ -run TestSeamGrepGate` | PASS |
| `go test ./cmd/villa/ -run 'JSONGolden\|Detect'` | PASS (8 tests) |
| `detect.golden.json` diff append-only (schema 1→2 + rocm_readiness) | VERIFIED |
| `go test ./...` (phase gate) | PASS (481 tests, 17 packages) |

## Commits

- `340a1d6` feat(07-03): append ROCmReadiness struct + bump schema 1->2 (additive, typed-Optional)
- `17956e6` feat(07-03): compute rocm_readiness fields (off-hardware unknowns -> UnknownBool)
- `f47782e` test(07-03): re-freeze detect.golden.json (append-only, schema 1->2 + rocm_readiness)

## Known Stubs

`firmware_date_ok` and `hsa_override_viable` are intentionally `UnknownBool` (not probed) in the off-hardware detect path — this is the no-false-green contract, NOT a stub: they serialize as UNSET and are documented in PROJECT.md / 07-RESEARCH as advisory-only off-hardware. A real firmware/override probe would replace the `UnknownBool` with a `KnownBool` comparison on-hardware.

## Self-Check: PASSED
- internal/detect/readiness_rocm.go — FOUND
- internal/detect/readiness_rocm_test.go — FOUND
- cmd/villa/testdata/detect.golden.json (rocm_readiness + schema 2) — FOUND
- commit 340a1d6 — FOUND
- commit 17956e6 — FOUND
- commit f47782e — FOUND
