---
phase: 07-rocm-render-unit-preflight-detect
plan: 02
subsystem: preflight
tags: [preflight, rocm, gfx1151, go-embed, policy, refuse-with-remediation, cli]

# Dependency graph
requires:
  - phase: 07-rocm-render-unit-preflight-detect
    plan: 01
    provides: "ROCm render unit (villa-llama-rocm.container) + multi-value parseContainerArgs"
  - phase: 06-rocm-backend-resolver-spine
    provides: "BackendFor(\"rocm\") resolver, digest-pinned rocm-7.2.4 image (the in-tree stable image the image gate passes)"
provides:
  - "go:embed'd internal/preflight/rocm-policy.json: v1.0 floors (6.18.4/6.18.9/25.0.0/20260110) + firmware denylist + image denylist + required HSA override 11.5.1"
  - "loadROCmPolicy() + ROCmPolicy struct; Floors() re-sourced from the embedded policy (behavior no-op migration)"
  - "RunROCm/RunROCmWithPolicy: refuse-with-remediation ROCm host-fitness gate (PRE-06), one TierBlock check per signal (gfx/kernel/firmware/hsa/image), known-bad→FAIL / unevaluable→WARN"
  - "villa preflight --backend rocm CLI routing (local flag); off-hardware exits 2, never 1"
affects: [08-backend-set-switch]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Externalized version/denylist policy as a build-time go:embed JSON (rocm-policy.json) sourced into Floors() — a floor/denylist edit touches data, not check logic"
    - "Refuse-with-remediation gate reusing the existing CheckResult/Tier/Status vocabulary (no new verdict type); D-02/D-15 bias: only a confident known-bad FAILs, every unevaluable signal degrades to WARN"
    - "Backend image-tag literals kept OUT of non-seam .go files (live as rocm-policy.json data) to satisfy the inference TestSeamGrepGate"

key-files:
  created:
    - internal/preflight/rocm-policy.json
    - internal/preflight/policy_test.go
    - internal/preflight/checks_rocm.go
    - internal/preflight/checks_rocm_test.go
  modified:
    - internal/preflight/floors.go
    - cmd/villa/preflight.go
    - cmd/villa/preflight_test.go

key-decisions:
  - "Floor.FirmwareDeny stays SCALAR (collapsed to FirmwareDeny[0]) to preserve the v1.0 checkFirmwareFloor/golden contract byte-identically; the full denylist is exposed via loadROCmPolicy for the ROCm checks — the migration is a proven no-op."
  - "Firmware date and HSA_OVERRIDE_GFX_VERSION are NOT probed HostProfile fields in v1.0, so RunROCmWithPolicy sources them as typed-Unknown (always WARN off-hardware); the per-check functions take a detect.Str directly so the table test drives the known-bad FAIL branch and a future probe wires a real value without reshaping the check."
  - "Removed the rocm-7.2.4/rocm7-nightlies image-tag string literals from checks_rocm.go and floors.go (Rule 3): TestSeamGrepGate forbids ROCm image literals outside internal/inference. The denied/stable tags live in rocm-policy.json data; remediation text names the denied SET dynamically via pol.ImageDeny."

patterns-established:
  - "go:embed policy + behavior-no-op migration proof: policy_test.go asserts the loaded values equal the documented v1.0 constants, so a silent floor drift fails CI."
  - "Off-hardware over-block guard: a table case feeds a bare HostProfile and asserts ZERO StatusFail across all ROCm checks (T-07-02)."

requirements-completed: [PRE-06]

# Metrics
duration: 4min
completed: 2026-06-06
---

# Phase 7 Plan 02: ROCm Preflight Refuse-with-Remediation Gate Summary

**A reusable `RunROCm` host-fitness gate (PRE-06) — driven by a `go:embed`'d `rocm-policy.json` of version floors + firmware/image denylists + the required HSA override — that refuses ROCm bring-up with actionable remediation ONLY on a confident known-bad host (firmware exactly 20251125, a nightly image request, kernel < 6.18.4, missing/wrong HSA override, non-gfx1151) and degrades every unevaluable signal to WARN, demoable now off-hardware via `villa preflight --backend rocm`. The migrated v1.0 floors are a proven behavior no-op.**

## Performance

- **Duration:** 4 min
- **Started:** 2026-06-06T09:33:18Z
- **Completed:** 2026-06-06T09:37:37Z
- **Tasks:** 3
- **Files modified:** 7 (4 created, 3 modified)

## Accomplishments
- Externalized the v1.0 version floors into a `go:embed`'d `internal/preflight/rocm-policy.json` carrying the migrated floors PLUS the new ROCm policy data (firmware denylist `["20251125"]`, image denylist `["rocm7-nightlies"]`, required `HSA_OVERRIDE_GFX_VERSION` `11.5.1`). `Floors()` is re-sourced from the loaded policy — `policy_test.go` proves the loaded values are byte-identical to the documented v1.0 constants, and the existing `CheckKernelFloor`/`CheckFirmwareFloor`/`CompareVersions` tests + goldens pass UNCHANGED (the no-op proof).
- Added `RunROCm`/`RunROCmWithPolicy` (mirroring `Run`→`RunWithResources`): one `TierBlock` check per ROCm signal using the existing `pass`/`warn`/`fail` constructors — NO new verdict type (D-01). Each FAILs ONLY on a positively-detected known-bad; any `Known==false` underlying fact → WARN (D-02/D-15). A non-colliding `ROCM-PRE-*` id scheme avoids the `PRE-06`/`PRE-07` check-id clash; `compareVersions` is reused for the kernel floor.
- Wired a LOCAL `--backend` flag on `villa preflight`: `--backend rocm` routes to `preflight.RunROCm` through the EXISTING `renderPreflight` seam; the default path calls the unchanged `preflight.Run` (D-03). Off-hardware `--backend rocm` maps to exit 2 (WARN), never exit 1 (Pitfall 4).
- Full suite green (`go test ./...` — 477 tests), including the over-block guard (a bare HostProfile yields ZERO `StatusFail`) and the inference seam grep-gate.

## Task Commits

Each task was committed atomically:

1. **Task 1: Externalize floors into go:embed rocm-policy.json (behavior no-op migration)** — `ed58763` (feat)
2. **Task 2: RunROCm checks — known-bad→FAIL, unevaluable→WARN, per signal** — `caba9fd` (feat)
3. **Task 3: Wire villa preflight --backend rocm (standalone Run unchanged)** — `8e54a50` (feat) — includes the Rule-3 seam fix (see Deviations).

## Files Created/Modified
- `internal/preflight/rocm-policy.json` — NEW go:embed data: `kernelFloor`/`kernelTested`/`mesaFloor`/`firmwareFloor` v1.0 values + `firmwareDeny`/`imageDeny`/`requiredHSAOverride`.
- `internal/preflight/floors.go` — added `//go:embed rocm-policy.json` over `rocmPolicyBytes`, an `ROCmPolicy` struct, `loadROCmPolicy()` (panics on a malformed embed — build-time only, T-07-03), and re-sourced `Floors()` from the loaded policy (scalar `Floor.FirmwareDeny` = `FirmwareDeny[0]` to keep the v1.0 contract). Removed the inlined denied-tag literal from a doc comment (seam-gate).
- `internal/preflight/policy_test.go` — NEW: asserts loaded floors equal the v1.0 constants, `Floors()` is policy-sourced, and the denylists/HSA value are present.
- `internal/preflight/checks_rocm.go` — NEW: `RunROCm`/`RunROCmWithPolicy` + the five `checkROCm*` functions; firmware/HSA take a `detect.Str` (not-probed → typed-Unknown → WARN); denied image-tag literals kept out (policy data only).
- `internal/preflight/checks_rocm_test.go` — NEW: per-signal both-branch table (known-bad→FAIL, unknown→WARN), the off-hardware zero-FAIL over-block guard, and a `RunROCm(...)` embedded-policy smoke test.
- `cmd/villa/preflight.go` — added a LOCAL `--backend` `StringVar`; `RunE` branches to `preflight.RunROCm` when `== "rocm"`, else the unchanged `preflight.Run`.
- `cmd/villa/preflight_test.go` — added the off-hardware `--backend rocm` exit-2 + `ROCM-PRE-*` row assertions and the standalone-path-unchanged assertion.

## Decisions Made
- **Scalar `Floor.FirmwareDeny` preserved:** the v1.0 `Floor` struct exposes a single `FirmwareDeny string`; collapsing the policy's `firmwareDeny[0]` into it keeps `checkFirmwareFloor` and its golden byte-identical while the ROCm checks consume the full slice via `loadROCmPolicy`. The migration is a no-op exactly because of this.
- **Firmware/HSA as injected `detect.Str`, not invented profile fields:** Phase 1 never probes a firmware date or the HSA env, so inventing `HostProfile.FirmwareVersion()`/`HSAOverride()` would have been a fabricated field. Instead `RunROCmWithPolicy` sources both as `detect.UnknownStr(...)` (always WARN off-hardware) and the per-check functions take the typed value, so the table test injects a Known known-bad and a future probe wires a real value with no reshaping.
- **Image gate is request-driven, not a host probe (Pitfall 5):** `checkROCmImage` takes the requested/resolved image string; an empty request (standalone) → WARN, a denied tag → FAIL. The denied tag literals live in `rocm-policy.json`, not the check code.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Inference seam grep-gate failed on ROCm image-tag literals**
- **Found during:** Task 3 full-suite phase gate (`go test ./...`).
- **Issue:** `internal/inference/seam_test.go::TestSeamGrepGate` forbids the `rocm-7.2.4`/`rocm7-nightlies` image-tag literals in any non-seam, non-test `.go` file (backend-neutrality, Phase-2 SC#4). My `checks_rocm.go` remediation text and `floors.go` doc comment inlined those tags, so the gate flagged a "seam leak".
- **Fix:** Removed the inlined tag literals from both `.go` files. The denied/stable tags are POLICY DATA in `rocm-policy.json` (a data file, not scanned), and the remediation text now names the denied SET dynamically via `pol.ImageDeny`. No behavior change — the gate values are identical, just sourced from data.
- **Files modified:** `internal/preflight/checks_rocm.go`, `internal/preflight/floors.go`
- **Commit:** `8e54a50` (folded into the Task 3 commit, since the gate only triggers at full-suite time and the fix is part of making the phase gate green).

## Issues Encountered
The seam grep-gate (above) was the only friction; caught at the `go test ./...` phase gate, fixed within scope, full suite green afterward (477 tests).

## User Setup Required
None — no external service configuration. The gate is testable off-hardware now (`villa preflight --backend rocm` → exit 2 WARN on a non-Strix-Halo host). On-hardware FAIL behavior (a real below-floor kernel / denied firmware) is exercised when Phase 8's `backend set` verb consumes `RunROCm`.

## Next Phase Readiness
- PRE-06 is met and the reusable `RunROCm` verdict is ready for Phase 8's `backend set rocm` gate to consume (it returns the same `[]CheckResult` the renderer already maps to exit codes).
- The policy is `go:embed`-updatable: a floor or denylist correction edits `rocm-policy.json` only, no check reshaping.
- Firmware-date and HSA-override PROBES are the natural Phase-8 follow-up — the checks already accept a `detect.Str`, so wiring a real probe lights up the FAIL branch with zero check changes.
- No blockers.

## Self-Check: PASSED

All 7 created/modified files exist on disk; all 3 task commits (`ed58763`, `caba9fd`, `8e54a50`) are in git history; `go test ./...` is green (477 passed).

---
*Phase: 07-rocm-render-unit-preflight-detect*
*Completed: 2026-06-06*
