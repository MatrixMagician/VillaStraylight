# Phase 6: ROCm Backend + Resolver Spine - Context

**Gathered:** 2026-06-05
**Status:** Ready for planning

> Captured in `--auto` mode: gray areas auto-selected, recommended option taken
> for each (logged in `06-DISCUSSION-LOG.md`). Decisions are HOW-to-implement
> choices that refine the locked ROADMAP success criteria — no scope added.

<domain>
## Phase Boundary

Build the **spine** every downstream v1.1 phase depends on: a ROCm/HIP backend
that exists behind the v1.0 `inference.Backend` interface and is selected from
config through a single polymorphic resolver `BackendFor(cfg.Backend)`, with an
**offload-assert that proves ROCm residency** (not just Vulkan).

While Vulkan stays the only configured backend this is a **behavior no-op** — but
it is the precondition for switching (Phase 8), benching (Phase 9), and surfacing
(Phase 10). Concretely, Phase 6 delivers:

- `BackendFor(cfg.Backend)` resolver in `internal/inference`.
- `backend_rocm.go` — the ROCm `Backend` implementation (image, `/dev/kfd`+`/dev/dri`
  devices, render group, HSA-override env, mandatory flags), the ROCm sibling of
  `backend_vulkan.go`.
- A `ResidencyProof()` extension of the `Backend` interface so the offload-assert
  is backend-specific (Vulkan0 vs ROCm0 markers) without leaking literals to callers.
- Re-route all **7 hardcoded `VulkanBackend()` call sites** through `BackendFor()`.
- Grep-gate test(s) protecting the HIP/`ROCm0` marker strings.

**Out of scope (later phases):** rendering the ROCm Quadlet unit + byte-golden +
preflight/detect (Phase 7); the on-running-install `villa backend set` switch verb
+ rollback (Phase 8); `villa bench` A/B (Phase 9); backend/tok-s surfacing in
`status`/dashboard/`recommend` (Phase 10).

</domain>

<decisions>
## Implementation Decisions

### Resolver (`BackendFor`)
- **D-01:** `BackendFor` lives in `internal/inference` (the seam package), alongside
  the `Backend` interface and `backend_vulkan.go`/`backend_rocm.go` — it is the one
  place that maps a config string to a concrete backend. Signature:
  `func BackendFor(name string) (Backend, error)`.
- **D-02:** **Fail-closed on unknown backend.** An unrecognized `backend` value
  returns an actionable error (refuse-with-remediation), never a silent Vulkan
  fallback — consistent with the project's "no false-green" posture. Empty string
  maps to the `"vulkan"` default (config already defaults to `"vulkan"`, so empty
  only arises from a hand-edited file; treating it as vulkan preserves the v1.0
  no-op). `"vulkan"` and `"rocm"` are the only accepted values this milestone.
- **D-03:** Each of the **7 call sites** (`cmd/villa/{install,lifecycle,status,
  dashboard,model,inference}.go` + `internal/status/status.go`) resolves the backend
  via `BackendFor(cfg.Backend)` and surfaces the error through its existing error
  path — no call site keeps a literal `VulkanBackend()`. The v1.0 suite must stay
  green under the Vulkan default (proves the re-route is a behavior no-op, SC#4).

### Residency proof (interface extension)
- **D-04:** Extend the `Backend` interface with a `ResidencyProof()` method returning
  a backend-owned descriptor (device token e.g. `"Vulkan0"`/`"ROCm0"`, the
  `model buffer size` phrase, the fault-string to watch for, the sysfs busy signal).
  Each backend file owns its own marker literals — they stay **behind the seam**.
- **D-05:** Refactor `running_offload.go` (`scrapeLoadTensorsVulkan` /
  `RunningOffloadVerdict`) to be **parameterized by the descriptor** rather than
  hardcoding `"Vulkan0"`. The combine discipline (any FAIL→FAIL; else any Unknown→
  WARN; else PASS) and typed-Unknown contract (D-09/D-12) are reused verbatim — do
  not re-roll the offload math. Vulkan behavior must be byte-identical after the
  refactor (the existing Vulkan offload tests stay green).
- **D-06:** The ROCm proof asserts, per ROADMAP SC#2: a `ROCm0` device-buffer line
  + `offloaded N/N` (N==M) + non-zero sysfs `gpu_busy_percent` during a real decode
  + **absence of** `Memory access fault by GPU node`. A silent/partial CPU fallback
  is a **FAIL**, an unevaluable signal degrades to **WARN**.

### Off-hardware build boundary (Phase 6 is off-hardware)
- **D-07:** Build the **pure** ROCm parse/verdict logic + table-test fixtures
  (synthetic `ROCm0` journals: real-offload PASS, CPU-fallback FAIL, fault FAIL,
  empty WARN) + the grep-gate **now** — mirroring how `running_offload.go` is pure
  and fixture-tested. The live-only signals (non-zero `gpu_busy_percent` *during a
  decode*, a live `Memory access fault` journal scan) are wired in as **inputs** now
  but exercised on real hardware in Phase 8's switch verb. Phase 6 proves the logic,
  not the hardware.

### ROCm backend image + container args (`backend_rocm.go`)
- **D-08:** `backend_rocm.go` carries the ROCm image as a **digest-pinned constant**
  mirroring `vulkanImage`: `docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4`
  pinned by `@sha256:…`, resolved via `podman pull` on the dev box during execution.
  **Never `rocm7-nightlies`** (64 GB allocation-cap bug). If the digest cannot be
  resolved off-hardware, pin the tag with a clearly-marked TODO-digest constant
  **and** a test that fails until it is a real `sha256:` digest — resolved before
  Phase 7's byte-golden freeze.
- **D-09:** ROCm `ContainerArgs` is a delta over the Vulkan args: add `--device
  /dev/kfd` **and** `--device /dev/dri`, the `render` group via `--group-add`, and
  ordered env `HSA_OVERRIDE_GFX_VERSION=11.5.1` then `ROCBLAS_USE_HIPBLASLT=1`;
  keep the mandatory `-ngl 999 -fa 1 --no-mmap` and loopback-only host publish.
  (Phase 7 freezes the rendered unit golden; Phase 6 just makes `ContainerArgs`
  correct behind the interface.)

### Grep-gate strategy
- **D-10:** Add a **positive-presence** gate test asserting the `ROCm0`/HIP marker
  strings exist in `backend_rocm.go` (ROADMAP SC#3 — a refactor that drops them
  fails). Keep the existing **negative** seam gate (`TestSeamGrepGate`) and extend
  its `container image literal` pattern so the rocm image token is also seam-bound
  (cannot leak outside `internal/inference/` + `internal/detect/gpu_amd.go`).

### Claude's Discretion
- Exact descriptor struct name/shape for `ResidencyProof()` and how the
  parameterized scraper is factored (single function vs per-signal helpers) — left
  to the planner/executor, provided Vulkan stays byte-identical and ROCm markers
  stay behind the seam.
- Exact error type/wording for the fail-closed unknown-backend path, as long as it
  is actionable and routed through each call site's existing error handling.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` §"Phase 6: ROCm Backend + Resolver Spine" — goal, the 4
  locked success criteria (resolver, ROCm residency assert, grep-gate, 7-site
  re-route as a no-op), and the spine-first ordering rationale.
- `.planning/REQUIREMENTS.md` — ROCM-01 (opt-in ROCm backend behind the interface),
  ROCM-02 (HIP residency proof + grep-gate), ROCM-04 (`BackendFor` routes all sites).
- `.planning/PROJECT.md` — milestone goal, key decisions (ROCm opt-in, pin
  `rocm-7.2.4`, ROCm offload-asserting like Phase-2 Vulkan).

### Stack / ROCm specifics
- `CLAUDE.md` §"Technology Stack" / §"What NOT to Use" — ROCm image, `HSA_OVERRIDE_
  GFX_VERSION=11.5.1`, `ROCBLAS_USE_HIPBLASLT=1`, `--no-mmap -fa 1 -ngl 999`,
  nightlies 64 GB cap, kernel ≥6.18.4, avoid `linux-firmware-20251125`.
- `.planning/research/STACK.md`, `.planning/research/PITFALLS.md`,
  `.planning/research/ARCHITECTURE.md`, `.planning/research/SUMMARY.md` — v1.1 ROCm
  research (image legitimacy/digest pinning, gfx1151 sharp edges, residency proof).

### Code to mirror / extend (the seam)
- `internal/inference/inference.go` — `Backend`/`Runner`/`RunSpec`/`Verdict`
  interfaces + typed-Unknown PASS/WARN/FAIL vocabulary (the contract to extend).
- `internal/inference/backend_vulkan.go` — the seam exemplar: digest-pinned image
  constant, `ContainerArgs`, mandatory flags. `backend_rocm.go` is its sibling.
- `internal/inference/running_offload.go` — the offload-assert to parameterize by
  `ResidencyProof()` (D-09/D-12 combine + typed-Unknown discipline to reuse verbatim).
- `internal/inference/seam_test.go` — `TestSeamGrepGate` (negative leak gate) to
  extend; pattern for the new positive-presence gate.
- `internal/config/villaconfig.go` — `VillaConfig.Backend` field (already exists,
  default `"vulkan"`) that `BackendFor` consumes.
- `internal/orchestrate/render.go` / `orchestrate.go` — `RenderInput.Backend
  inference.Backend` (how the resolved backend flows into unit rendering; the actual
  ROCm render is Phase 7).

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- **`backend_vulkan.go`** — direct template for `backend_rocm.go`: stateless struct
  satisfying `Backend`, digest-pinned image constant, single `ContainerArgs` assembly
  point. Copy its structure; swap image + device/env deltas.
- **`running_offload.go`** — the residency-proof engine: `combineOffload`,
  typed-Unknown signals, `scrapeLoadTensorsVulkan`, `gttFloor`. Parameterize, don't
  duplicate. ROCm reuses the exact PASS/WARN/FAIL combine logic.
- **`seam_test.go` / `TestSeamGrepGate`** — existing negative grep-gate to extend;
  reference for the new positive ROCm-marker gate.
- **`VillaConfig.Backend`** (`internal/config/villaconfig.go`) — config field +
  `"vulkan"` default already present; no config schema change needed for the resolver.

### Established Patterns
- **Seam discipline (D-03/SC#4):** all backend literals (image, devices, flags,
  markers) live ONLY in `internal/inference/` (+ `internal/detect/gpu_amd.go`).
  `BackendFor`, `backend_rocm.go`, and the ResidencyProof descriptor all stay inside
  this boundary; callers depend on the interface only.
- **Typed-Unknown / no-false-green (D-09):** an unevaluable signal is WARN, distinct
  from a confidently-false offload (FAIL). The ROCm assert inherits this exactly.
- **Pure-library + cmd-layer I/O split:** `internal/inference` never does I/O or
  `os.Exit`; journald/HTTP/sysfs reads live in `cmd/villa`. ROCm logic stays pure
  and fixture-testable.

### Integration Points
- The **7 `VulkanBackend()` call sites** (`cmd/villa/{install,lifecycle,status,
  dashboard,model,inference}.go`, `internal/status/status.go`) all switch to
  `BackendFor(cfg.Backend)` — the blast radius of this phase.
- `orchestrate.RenderInput.Backend` consumes the resolved `Backend` — the resolver's
  output feeds unit rendering (live in Phase 7) and the runner/endpoint derivation.

</code_context>

<specifics>
## Specific Ideas

- Mirror the Vulkan digest-pin comment style in `backend_rocm.go` (record the
  `podman pull` date + the RESEARCH legitimacy-audit note), so the ROCm image
  provenance is as auditable as the Vulkan one (`backend_vulkan.go:15-19`).
- The ROCm residency line shape to fixture against:
  `load_tensors: ROCm0 model buffer size = N MiB` (ROCm0 analog of the Vulkan0 line),
  plus an `offloaded N/N layers` line and a `Memory access fault by GPU node`
  negative-case fixture.

</specifics>

<deferred>
## Deferred Ideas

- **ROCm Quadlet unit rendering + byte-golden + ROCm preflight/detect readiness** —
  Phase 7 (ROCM-03, PRE-06, DET-04).
- **`villa backend set rocm|vulkan` switch verb + transactional rollback + live
  generation-probe/residency cutover** — Phase 8 (BSET-01/02/03, on-hardware).
- **`villa bench` honest A/B (separate pp/tg tok/s)** — Phase 9 (BENCH-01/02).
- **Backend-aware `recommend` advice + dashboard/`status` active-backend + live
  tok/s** — Phase 10 (REC-05, DASH-06).
- **Live `gpu_busy_percent`-during-decode + live fault-scan exercise** — logic built
  in Phase 6, validated on real gfx1151 hardware in Phase 8.

</deferred>

---

*Phase: 6-rocm-backend-resolver-spine*
*Context gathered: 2026-06-05*
