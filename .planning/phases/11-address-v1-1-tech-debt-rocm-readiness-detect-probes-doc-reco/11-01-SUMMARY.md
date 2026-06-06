---
phase: 11-address-v1-1-tech-debt-rocm-readiness-detect-probes-doc-reco
plan: 01
subsystem: detect
tags: [rocm-readiness, detect-probes, firmware-date, hsa-override, no-false-green]
requires:
  - internal/detect/value.go (typed Optional spine)
  - internal/detect/gpu_amd.go (backend seam: runTool/capRaw/compareVersionSegments)
  - internal/preflight/rocm-policy.json (authoritative policy VALUES, duplicated not imported)
provides:
  - firmwareDateProbe() Str — fixed-arg rpm host probe behind the gpu_amd.go seam
  - firmwareDatePolicyOK(date) bool — floor/deny compare seam (denylist wins)
  - isYYYYMMDD(s) bool — 8-digit date guard
  - "real firmwareDateOK(date Str) Bool + hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool"
  - "extended computeROCmReadiness(gfxID, kernel, rocmPresent, firmwareDate, resolvedImage)"
affects:
  - internal/detect (probe wiring); downstream status fold + recommend advice flip automatically once leaves go Known
tech-stack:
  added: []
  patterns:
    - "firmware/kernel/image policy literals stay behind the gpu_amd.go seam; backend-neutral files carry none"
    - "no-false-green (D-08): unprobeable source -> UnknownBool (UNSET), never a fabricated false"
    - "pure HSA derivation from host facts; never reads os.Getenv(HSA_OVERRIDE_GFX_VERSION)"
key-files:
  created: []
  modified:
    - internal/detect/gpu_amd.go
    - internal/detect/readiness_rocm.go
    - internal/detect/detect.go
    - internal/detect/readiness_rocm_test.go
decisions:
  - "Firmware floor/deny VALUES duplicated detect-side (floor 20260110, deny 20251125), NOT imported from preflight — preflight imports detect, so the reverse is a forbidden cycle (mirrors the established kernel-floor + image-tag duplication)."
  - "HSA Known-ness gated on gfxID.Known, NOT rocmPresent.Known: rocmPresent is always Known (present/absent is confident), so it could never preserve off-hardware UNSET (Assumptions A1)."
  - "Golden stayed byte-identical with NO -update: detect.golden.json is fixture-driven (fixtureProfile sets the two fields to explicit UnknownBool), so the live probes cannot perturb it (D-04)."
metrics:
  duration_min: 3
  completed: "2026-06-06"
  tasks: 3
  files: 4
requirements: [DET-04, DASH-06]
---

# Phase 11 Plan 01: rocm_readiness detect probes Summary

Made `internal/detect/readiness_rocm.go`'s two stub probes (`firmwareDateOK`, `hsaOverrideViable`) real — a fixed-arg `rpm` firmware-date probe + detect-local floor/deny policy seam in `gpu_amd.go`, threaded through `computeROCmReadiness` and `Probe()` — so the Phase-10 ROCm-readiness badge can read `ready` (non-`unknown`) on a live ROCm host, while preserving no-false-green discipline (off-hardware fields stay honestly UNSET) and a byte-identical detect golden.

## What Was Built

- **Task 1 — `gpu_amd.go` seam (commit cb229cb):** `firmwareDateProbe()` (fixed-arg `runTool("rpm","-q","--qf","%{VERSION}","linux-firmware")`, no-false-green on rpm-absent/unparseable), `isYYYYMMDD()` 8-digit guard, `firmwareDatePolicyOK()` (denylist-wins then floor compare via the reused `compareVersionSegments`), and the `rocmFirmwareFloor`/`rocmFirmwareDeny` consts mirroring `rocm-policy.json`.
- **Task 2 — real probes + threading (commit abc8bea):** rewrote `firmwareDateOK(date Str) Bool` (KnownBool via the policy seam; Unknown date → UNSET) and `hsaOverrideViable(gfxID Str, rocmPresent Bool) Bool` (pure derivation, no `os.Getenv`); extended `computeROCmReadiness` to 5 args; wired `Probe()` to pass `gpu.rocmPresent` + `firmwareDateProbe()`; added `TestFirmwareDateOK` + `TestHSAOverrideViable` table tests; migrated all five legacy 3-arg call sites (four test functions) to 5-arg; extended the off-hardware regression test.
- **Task 3 — regression guard (verification only, no source edits):** confirmed the detect golden is byte-identical WITHOUT `-update`, the status badge fold is unchanged + green, and the full suite is green.

## Verification Results

- `go test ./internal/detect/...` — green (52 tests; both new table tests + extended off-hardware regression).
- `go test ./cmd/villa/ -run TestJSONGolden` — green WITHOUT `-update`; `git diff --stat cmd/villa/testdata/detect.golden.json` empty (D-04 honored).
- `go test ./internal/status/ -run TestReadinessFold` — green (badge fold unchanged).
- `go test ./...` — **559 passed in 16 packages** (phase gate).
- `git diff --name-only` over the plan's commits lists ONLY `internal/detect/*` files (no cmd/villa, status, or recommend source touched).

### Grep gates (all green)
- `firmwareDateProbe` / `firmwareDatePolicyOK` / `isYYYYMMDD` + both policy consts present in `gpu_amd.go`.
- `firmwareDateProbe` uses the existing `runTool("rpm", ...)`; no `sh -c` / `exec.Command(...sh)` introduced.
- No firmware-date literal in `readiness_rocm.go`; no `internal/preflight` import in `internal/detect`; no actual `os.Getenv` call in `internal/detect` (only a doc comment naming the forbidden read).

## Deviations from Plan

None — plan executed exactly as written. Task 3 required no source edits (golden was byte-identical as predicted by the fixture-driven design).

## Deferred / Manual UAT

- **[MANUAL UAT, D-06 — host-dependent]** On a live gfx1151 ROCm host with `backend=rocm`: `villa detect --json` should show `firmware_date_ok:true` + `hsa_override_viable:true`, and `villa status` / the dashboard ROCm-readiness badge should read `ready` (not `unknown`) — closing the DASH-06 SC#1 residual. Not automatable off-hardware (same UAT gating as Phases 8–10).

## Self-Check: PASSED

- Files modified exist: `internal/detect/gpu_amd.go`, `internal/detect/readiness_rocm.go`, `internal/detect/detect.go`, `internal/detect/readiness_rocm_test.go` — all FOUND.
- Commits exist: `cb229cb` (Task 1), `abc8bea` (Task 2) — both FOUND in `git log`.
- No created files claimed (verification-only Task 3 produced no source artifact).
