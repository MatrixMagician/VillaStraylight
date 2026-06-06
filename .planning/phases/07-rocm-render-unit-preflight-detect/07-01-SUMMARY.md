---
phase: 07-rocm-render-unit-preflight-detect
plan: 01
subsystem: infra
tags: [quadlet, podman, rocm, gfx1151, golden-test, text-template, render]

# Dependency graph
requires:
  - phase: 06-rocm-backend-resolver-spine
    provides: "backend_rocm.go (Image/ContainerArgs/ResidencyProof seam), BackendFor(\"rocm\") resolver, digest-pinned rocm-7.2.4 image"
  - phase: 03-orchestrate-render-reconcile
    provides: "internal/orchestrate Render/parseContainerArgs renderer, container.tmpl, byte-golden discipline"
  - phase: 04-open-webui-managed-service
    provides: "ordered []envPair pattern + {{range .Env}}Environment= template idiom (openwebui.container.tmpl)"
provides:
  - "Multi-value parseContainerArgs: collects ALL --device, ALL --group-add, ALL --env tokens (no silent drop, order preserved)"
  - "container.tmpl ranges over AddDevice/GroupAdd/Env; seam-sourced {{.BackendLabel}} Description"
  - "villa-llama-rocm.container.golden: byte-frozen ROCm unit (kfd+dri, keep-groups+render, HSA/hipBLASLt env, rocm-7.2.4 digest)"
  - "TestRenderROCmContainerGolden + TestRenderROCmEnvGroupFrozen + TestParseContainerArgsMultiValue"
affects: [08-backend-set-switch, 09-villa-bench, 10-backend-surfacing]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Range-over-slice Quadlet fields (devices/groups/env) mirroring the Phase-4 ordered-env template idiom"
    - "Seam-keyed Description label: backendLabel(Backend.Name()) selects display string without re-typing backend identity"

key-files:
  created:
    - internal/orchestrate/testdata/villa-llama-rocm.container.golden
    - internal/orchestrate/parseargs_test.go
  modified:
    - internal/orchestrate/render.go
    - internal/orchestrate/quadlet/container.tmpl
    - internal/orchestrate/render_test.go

key-decisions:
  - "D-09's literal 'AddDevice becomes []string' is incomplete: the parser handles THREE deltas (multi --device, second --group-add render, two --env) — implementing only devices yields a non-functional ROCm unit a naive golden would still freeze."
  - "BackendLabel is keyed off Backend.Name() through the seam; the 'Vulkan RADV' / 'ROCm 7.2.4 (HIP)' display strings are this package's unit documentation, not backend imperatives, so the seam grep-gate stays green and the Vulkan golden Description is byte-identical."
  - "Env is intentionally EXCLUDED from the defensive all-fields-non-empty check — Vulkan legitimately emits zero env; requiring it would break the default Vulkan path (RESEARCH Pitfall 1)."

patterns-established:
  - "ROCm unit = pure additive delta over Vulkan, proven by git diff --exit-code on the Vulkan golden (byte-identical) + a separate ROCm golden a reviewer diffs to read the delta."
  - "Count + full-set intent guard (TestRenderROCmEnvGroupFrozen) backstops the golden against a wrongly-regenerated silent drop of the second group-add / env block."

requirements-completed: [ROCM-03]

# Metrics
duration: 3min
completed: 2026-06-06
---

# Phase 7 Plan 01: ROCm Render Unit Summary

**Renders the ROCm `villa-llama.container` Quadlet unit as a byte-frozen additive delta over Vulkan — kfd+dri passthrough, keep-groups+render, HSA_OVERRIDE/hipBLASLt env, rocm-7.2.4 digest — via a multi-value parseContainerArgs and a `{{range}}`-based template, with the Vulkan golden byte-for-byte unchanged.**

## Performance

- **Duration:** 3 min
- **Started:** 2026-06-06T09:26:43Z
- **Completed:** 2026-06-06T09:29:55Z
- **Tasks:** 3
- **Files modified:** 5 (2 created, 3 modified)

## Accomplishments
- `parseContainerArgs` now collects EVERY `--device`, `--group-add`, and `--env` token from the seam (order preserved) — closing the Pitfall-1 silent-drop of the second group-add and both env flags.
- `container.tmpl` converted to `{{range}}` over devices/groups/env, mirroring the Phase-4 ordered-env whitespace; the empty Vulkan `.Env` slice renders zero lines (no spurious blank line).
- New `villa-llama-rocm.container.golden` freezes the rendered ROCm unit; a count+full-set intent guard backstops it against a mis-regeneration.
- Vulkan golden proven byte-for-byte unchanged (`git diff --exit-code` clean) — the ROCM-03 additivity criterion — and the inference seam grep-gate stays green (no backend literal leaked into render.go).

## Task Commits

Each task was committed atomically:

1. **Task 1: Multi-value parseContainerArgs (devices + group-adds + env)** - `d2db46b` (feat) — TDD: failing `TestParseContainerArgsMultiValue` written first (RED), then the slice-retype + flEnv case made it pass (GREEN).
2. **Task 2: Template {{range}} conversion + seam-sourced Description** - `5fca8d9` (feat)
3. **Task 3: ROCm golden + render delta-freeze and env/group intent guard** - `8ab871d` (test)

_Task 1 followed the TDD RED→GREEN cycle within a single combined commit (new test + the implementation that makes it pass)._

## Files Created/Modified
- `internal/orchestrate/render.go` - `containerView.AddDevice`/`.GroupAdd` retyped `string`→`[]string`; new `Env []envPair` + `BackendLabel string`; `parseContainerArgs` appends all device/group tokens and adds a `flEnv` case (`strings.Cut` on first `=`); defensive check made slice-aware (Env excluded); `backendLabel()` map; `BackendLabel` threaded in `Render` from `Backend.Name()`.
- `internal/orchestrate/quadlet/container.tmpl` - scalar `AddDevice`/`GroupAdd` lines → `{{range}}` blocks + a new `{{range .Env}}Environment=` block; hardcoded `(Vulkan RADV)` Description parenthetical → `{{.BackendLabel}}`.
- `internal/orchestrate/render_test.go` - added `rocmFixtureInput`, `TestRenderROCmContainerGolden`, `TestRenderROCmEnvGroupFrozen`.
- `internal/orchestrate/testdata/villa-llama-rocm.container.golden` - NEW byte-golden of the rendered ROCm unit (generated via `-update`, eyeballed against the delta checklist).
- `internal/orchestrate/parseargs_test.go` - NEW; `TestParseContainerArgsMultiValue` (ROCm multi-value collection + Vulkan single-device/zero-env still passes).

## Decisions Made
- **Multi-value over scalar (all three deltas):** Implemented multi `--device`, the second `--group-add render`, AND the two `--env` flags — not just the device change D-09 literally names. A device-only implementation renders a non-functional ROCm unit (no render group, no HSA/hipBLASLt env) that a naive golden would still freeze.
- **`BackendLabel` keyed off `Backend.Name()`:** The label *selection* flows through the seam; the display strings live in render.go's `backendLabel()` map. `Name()` returns `"vulkan"`/`"rocm"`, but the historical golden carries `(Vulkan RADV)`, so a direct `Name()` interpolation would have changed the Vulkan golden — the map reproduces `Vulkan RADV` exactly.
- **Env excluded from the defensive check:** per RESEARCH Pitfall 1, Vulkan emits zero env legitimately; gating on `len(cv.Env) > 0` would break the default path.

## Deviations from Plan

None - plan executed exactly as written. All three tasks landed as specified; no Rule 1–4 deviations were required. The plan pre-anticipated the `BackendLabel` byte-fidelity concern (Open Question A1 / fallback A2); the recommended single-template `{{.BackendLabel}}` path worked and the A2 second-template fallback was not needed.

## Issues Encountered
None. The `git diff --exit-code` Vulkan-golden check passed on the first `-update` regeneration, confirming the `{{range}}` whitespace and the `backendLabel()` Vulkan string were correct.

## User Setup Required
None - no external service configuration required. Phase 7 freezes rendered TEXT only; nothing is run (per the threat model, `/dev/kfd`+`/dev/dri` passthrough and the `render` group are RENDERED here, exercised on-hardware in Phase 8).

## Next Phase Readiness
- The two off-hardware inputs Phase 8's `backend set` verb gates on are ready: the ROCm render output (`villa-llama-rocm.container`) and its byte-freeze.
- Vulkan stays the byte-identical default; the renderer now reshapes for any multi-device/multi-group/env backend without further template work.
- No blockers. The SELinux `--security-opt label=disable` decision remains explicitly DEFERRED to Phase 8 (on-hardware AVC) — not added speculatively here (T-07-01).

---
*Phase: 07-rocm-render-unit-preflight-detect*
*Completed: 2026-06-06*
