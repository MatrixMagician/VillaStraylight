---
phase: 06-rocm-backend-resolver-spine
plan: 03
subsystem: inference
tags: [go, rocm, backend-resolver, re-route, fail-closed, call-sites, no-op-proof]

# Dependency graph
requires:
  - phase: 06-rocm-backend-resolver-spine
    plan: 01
    provides: "ResidencyMarkers descriptor + ResidencyProof() on the Backend interface; ValidateInput.Markers field threaded into the start-time offload-assert (scrapeOffloadLog)"
  - phase: 06-rocm-backend-resolver-spine
    plan: 02
    provides: "BackendFor(name) (Backend, error) — the single fail-closed polymorphism point ('' /vulkan → Vulkan, rocm → ROCm, unknown → actionable error)"
provides:
  - "All 8 physical non-test inference.VulkanBackend() call sites (across 7 files) re-routed to inference.BackendFor(cfg.Backend) — config.Backend now drives backend selection everywhere (D-03 / ROCM-04)"
  - "Each site surfaces the BackendFor fail-closed error through its existing error path (exitBlocked / fmt.Errorf %w / return false,err / Report.err / Verdict{StatusFail}) — no silent fallback, no new panic (T-6-07)"
  - "runValidation (villa inference) loads config.LoadVilla() then resolves via BackendFor(cfg.Backend) (A1 resolved deterministically) and threads backend.ResidencyProof() into ValidateInput.Markers — start-time offload-assert is backend-aware, not Vulkan-only (T-6-08)"
  - "liveStatusDeps is the single backend-resolution point for the status/dashboard endpoint; liveDashboardDeps reuses its Endpoint (backend never resolved twice)"
affects: [phase-07 (ROCm quadlet render byte-golden — render path already backend-driven), phase-08 (live backend switch via config.toml backend=rocm now flips the whole inference path with no further caller change)]

# Tech tracking
tech-stack:
  added: []
  patterns:
    - "Config-driven backend selection: every render/endpoint/validate site resolves the Backend from cfg.Backend via BackendFor — the hardcoded Vulkan literal is gone from all callers; only backend_vulkan.go's definition + 6 test helpers construct it directly"
    - "Fail-closed re-route: a BackendFor error is surfaced through each site's pre-existing error path; deps-wiring helpers (func() *T) became func() (*T, error) so the cobra RunE prints + exits exitBlocked rather than swallowing"
    - "Single-resolution reuse: liveDashboardDeps consumes liveStatusDeps's resolved Endpoint instead of resolving the backend a second time"
    - "Behavior no-op proof: because the 6 test helpers keep injecting VulkanBackend() directly and the config default is 'vulkan', the full v1.0 suite stays byte-identical-green — the no-op IS SC#4"

key-files:
  created: []
  modified:
    - "cmd/villa/install.go — render site (234) + liveInstallDeps endpoint site (632) resolve via BackendFor(cfg.Backend); liveInstallDeps returns (*installDeps, error); RunE prints+exitBlocked on error"
    - "cmd/villa/lifecycle.go — renderStack resolves via BackendFor(cfg.Backend), returns nil,\"\",fmt.Errorf(\"resolve backend: %w\")"
    - "cmd/villa/model.go — ReconcileAndWrite closure resolves via BackendFor(c.Backend), returns false,err"
    - "cmd/villa/status.go — liveStatusDeps loads config.LoadVilla + resolves via BackendFor; returns (*status.Deps, error); RunE prints+exitBlocked on error"
    - "cmd/villa/dashboard.go — liveDashboardDeps reuses liveStatusDeps's Endpoint (single resolution); returns (*dashboardDeps, error); RunE prints+exitBlocked on error; dropped now-unused inference import"
    - "cmd/villa/inference.go — runValidation loads config.LoadVilla() then BackendFor(cfg.Backend) (A1 resolved); config-load + unknown-backend each return inference.Verdict{StatusFail}; Markers thread backend.ResidencyProof(); added internal/config import"
    - "internal/status/status.go — Run resolves via BackendFor(cfg.Backend), folds error into Report{Overall: StatusFail, err: err}"

# Decisions
decisions:
  - "A1 RESOLVED: runValidation's cfg source is config.LoadVilla() — the same loader liveStatusDeps/liveDashboardDeps/liveLifecycleDeps/recommend.go use. runValidation takes no cfg parameter today, so loading in-function (rather than a caller-threading signature change) is the minimal, established pattern."
  - "Deps-wiring helpers (liveInstallDeps/liveStatusDeps/liveDashboardDeps) changed signature from func() *T to func() (*T, error) so the BackendFor fail-closed error is surfaced (no swallow), per the threat model. Each cobra RunE prints to stderr and os.Exit(exitBlocked) on error."
  - "liveDashboardDeps reuses liveStatusDeps's resolved Endpoint instead of building its own (was a second independent inference.NewContainerRunner(VulkanBackend(),...).Endpoint()) — honours the plan's 'single resolution point, do not resolve twice'."
  - "A missing config.toml is NOT an install blocker: LoadVilla returns typed defaults (Backend:\"vulkan\") when the file is absent, so a fresh-host install resolves the default Vulkan backend cleanly."

# Metrics
metrics:
  duration: "~25 min"
  completed: 2026-06-06
  tasks: 3
  files-modified: 7
  files-created: 0
  tests: "460 passed across 17 packages (go test ./...); go vet ./... clean"
---

# Phase 06 Plan 03: Backend Resolver Spine Re-route Summary

Re-routed all 8 physical non-test `inference.VulkanBackend()` call sites (across 7 files) to `inference.BackendFor(cfg.Backend)`, each surfacing the resolver's fail-closed error through its existing error path, and threaded the resolved backend's `ResidencyProof()` markers into the start-time offload-assert — proven a behavior no-op under the Vulkan default by a fully-green v1.0 suite (SC#4).

## What was built

- **Task 1 — RenderInput.Backend sites (4):** `install.go:234`, `lifecycle.go:66`, `model.go:361`, `internal/status/status.go:180` now construct the `RenderInput.Backend` from `inference.BackendFor(cfg.Backend)`, with the resolver error returned/printed through each site's pre-existing path (`exitBlocked`, `fmt.Errorf("resolve backend: %w")`, `return false, err`, `Report{Overall: StatusFail, err: err}`).
- **Task 2 — Endpoint()/deps + validate (4):** the three deps-wiring helpers (`liveInstallDeps`, `liveStatusDeps`, `liveDashboardDeps`) load `config.LoadVilla()` and resolve the inference endpoint's backend via `BackendFor`, now returning an error surfaced through their cobra `RunE`. `liveDashboardDeps` reuses `liveStatusDeps`'s single resolution. `runValidation` (A1 resolved) loads `config.LoadVilla()` then `BackendFor(cfg.Backend)`; a config-load or unknown-backend error returns the existing `inference.Verdict{StatusFail}` refusal shape; `Markers` thread `backend.ResidencyProof()`.
- **Task 3 — no-op proof (SC#4):** zero source changes. `go vet ./...` clean; `go test ./...` fully green (460 tests, 17 packages). The 6 `VulkanBackend()`-injecting test helpers are untouched and pass — the byte-identical Vulkan-default behavior IS the proof.

## Verification

- `go build ./...` — success.
- `go vet ./...` — no issues.
- `go test ./...` — 460 passed across 17 packages (the Vulkan-default no-op proof, SC#4).
- `grep -rn 'inference\.VulkanBackend()' cmd/villa/ internal/status/ --include=*.go | grep -v '_test.go'` — zero matches. Only `backend_vulkan.go:63` (the definition) + a comment in `backend.go` + the 6 test helpers retain `VulkanBackend()`.
- Seam/resolver gates (`TestSeamGrepGate`, `TestROCmMarkerPresence`, `TestBackendFor`) — pass.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] Removed now-unused `internal/inference` import from dashboard.go**
- **Found during:** Task 2.
- **Issue:** After `liveDashboardDeps` switched from `inference.NewContainerRunner(inference.VulkanBackend(),...)` to reusing `liveStatusDeps`'s resolved Endpoint, `dashboard.go` no longer referenced the `inference` package → `imported and not used` compile error.
- **Fix:** Dropped the `internal/inference` import line from dashboard.go.
- **Files modified:** cmd/villa/dashboard.go
- **Commit:** 5299e88

### Signature change (planned mechanic)

The three deps-wiring helpers became `func() (*T, error)` (from `func() *T`). The plan's per-site recipe explicitly required surfacing the `BackendFor` error "through the deps constructor's return" — these helpers had no error return and loaded no cfg, so the signature change is the planned mechanic, with each cobra `RunE` handling the error (print + `exitBlocked`). Not a deviation; recorded for traceability.

## Threat coverage

- **T-6-07 (silent backend default):** mitigated — every re-routed site routes through `BackendFor` (fail-closed) and surfaces the error; `runValidation` refuses (`StatusFail`) on config-load/unknown-backend rather than defaulting.
- **T-6-08 (Vulkan-only start-time assert):** mitigated — `runValidation` threads `backend.ResidencyProof()` into `ValidateInput.Markers`, so the assert is backend-correct.
- **T-6-SC (package installs):** no package-manager installs this plan (re-route only, zero new deps).

## Known Stubs

None. No placeholder/empty-value patterns introduced; the change is a behaviour-preserving re-route.

## Self-Check: PASSED

- FOUND: .planning/phases/06-rocm-backend-resolver-spine/06-03-SUMMARY.md
- FOUND: commit 11ced6b (Task 1 — RenderInput sites)
- FOUND: commit 5299e88 (Task 2 — Endpoint/deps + runValidation)
- Task 3 produced zero source changes (no-op proof) — no commit required.
