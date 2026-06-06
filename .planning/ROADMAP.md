# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp (Vulkan) inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The roadmap is risk-ordered exactly as all four research streams independently converged: lock the pure, testable core (detection + recommendation + a hard preflight gate) first; then validate the single biggest technical unknown — that `llama-server` actually offloads to the iGPU through the rootless `/dev/dri` + unified-memory gauntlet — as an early vertical slice before any orchestration machinery is built on top of it; then turn that proven manual run into a managed, idempotent, boot-survivable stack with model management and privacy gates; wire Open WebUI so the first chat works immediately; and finally add the dashboard as a read-model over the same internal API `villa status` already consumes. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware.

## Phases

**Phase Numbering:**

- Integer phases (1, 2, 3): Planned milestone work
- Decimal phases (2.1, 2.2): Urgent insertions (marked with INSERTED)

Decimal phases appear between their surrounding integers in numeric order.

</details>

### 🚧 v1.1 ROCm Opt-In Backend (In Progress)

**Milestone Goal:** Add an opt-in ROCm/HIP inference backend for higher throughput on AMD Strix Halo (gfx1151), gated hard enough to preserve the v1.0 "just works" bar, switchable on a running install with transactional rollback, and benchmarked honestly to prove the per-model win — while Vulkan RADV remains the safe default.

**Spine-first ordering (forced by research):** nothing can render, switch, or bench a backend that doesn't exist behind a polymorphic resolver. Phase 6 builds the resolver/residency spine; Phase 7 renders the unit + gates the host (both off-hardware); Phase 8 is the on-hardware switch verb (the risk concentration); Phase 9 composes the switch into an honest A/B bench; Phase 10 surfaces backend + tok/s last so the byte-frozen `--json` goldens re-freeze once.

- [x] **Phase 6: ROCm Backend + Resolver Spine** — `BackendFor()` resolver + `ResidencyProof()` interface extension + `backend_rocm.go` with the HIP residency proof; re-route the 7 hardcoded `VulkanBackend()` sites (off-hardware) (completed 2026-06-05)
- [x] **Phase 7: ROCm Render Unit + Preflight/Detect** — ROCm Quadlet rendering (kfd device + ordered HSA-override env, new byte-golden, Vulkan golden unchanged) + reusable refuse-with-remediation ROCm preflight verdict + detect readiness fields (off-hardware) (completed 2026-06-06)
- [x] **Phase 8: `villa backend set` Switch Verb + Rollback** — transactional capture→prove→cutover→rollback backend switch on a running install (on-hardware risk concentration) (built + off-hardware verified 2026-06-06; on-hardware UAT 4/4 PASS 2026-06-06: happy-path ROCm cutover + dry-run/show + both failure-path residuals closed via fault injection — forced CPU-fallback rollback & bounded 5m01s never-ready timeout; 0 threats open)
- [x] **Phase 9: `villa bench` (Honest A/B)** — read-only A/B over the running `/v1`+`/metrics`, composing the Phase-8 switch; separate pp/tg tok/s with warmup + N-reps + noise band (all 3 plans built + verified; on-hardware UAT 3/3 PASS 2026-06-06: Δpp +4.84 / Δtg −11.15; 11/11 threats secured)
- [x] **Phase 10: Backend + tok/s Surfacing** — backend-aware `recommend` advice + dashboard/`status` active-backend + live tok/s, as append-only `--json`/golden additions (completed 2026-06-06)
- [ ] **Phase 11: Address v1.1 tech debt** — `rocm_readiness` detect probes + doc reconciliation (post-milestone tech-debt cleanup; 2 plans, 1 wave)

## Phase Details

### Phase 6: ROCm Backend + Resolver Spine

**Goal**: A ROCm/HIP backend exists behind the v1.0 `Backend` interface and is selected from config — the single resolver `BackendFor(cfg.Backend)` routes every inference call site, and the offload-assert proves ROCm residency (not just Vulkan). This is the spine every downstream phase depends on; while Vulkan stays the only configured backend it is a behavior no-op, but the precondition for switching, benching, and surfacing.
**Depends on**: Phase 5 (v1.0 `Backend` interface seam, D-11 offload-assert)
**Requirements**: ROCM-01, ROCM-02, ROCM-04
**Success Criteria** (what must be TRUE):

  1. Setting `backend = "rocm"` in config produces a ROCm `Backend` (rocm-7.2.4 image, kfd+dri devices, HSA-override env) via `BackendFor()`, with no backend specifics leaking to callers — `backend = "vulkan"` still produces the unchanged Vulkan path.
  2. The offload-assert distinguishes a real ROCm offload (`ROCm0` device-buffer line + `offloaded N/N layers` with N==M + non-zero sysfs `gpu_busy_percent` during a decode + no `Memory access fault by GPU node`) from a CPU fallback, which is reported as a FAIL.
  3. A grep-gate test fails if a refactor drops the HIP/`ROCm0` marker strings from `backend_rocm.go`.
  4. All 7 previously-hardcoded `VulkanBackend()` call sites resolve through `BackendFor(cfg.Backend)`, and the v1.0 test suite stays green (proving the re-route is a behavior no-op under the Vulkan default).**Plans**: 3 plans (3 waves)

**Wave 1**

- [x] 06-01-PLAN.md — Residency-proof engine: ResidencyProof()/ResidencyMarkers + dual-scrape parameterization (Vulkan byte-identical) ✅ 3/3 tasks

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 06-02-PLAN.md — backend_rocm.go + BackendFor resolver + ROCm fixtures/tests + grep-gates

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 06-03-PLAN.md — Re-route 8 VulkanBackend() sites through BackendFor(cfg.Backend) + full-suite no-op proof

### Phase 7: ROCm Render Unit + Preflight/Detect

**Goal**: The ROCm Quadlet unit renders correctly as a pure delta over the Vulkan unit, and a reusable ROCm preflight verdict + detect-readiness fields can tell (off-hardware) whether a host is fit for ROCm — refusing with remediation rather than silently degrading. These are the two "is this unit/host valid" pieces the switch verb gates on.
**Depends on**: Phase 6 (the `Backend` interface shape + `BackendFor`)
**Requirements**: ROCM-03, PRE-06, DET-04
**Success Criteria** (what must be TRUE):

  1. A ROCm install renders a `villa-llama.container` with the digest-pinned `kyuz0:rocm-7.2.4` image (never nightlies), `/dev/kfd`+`/dev/dri` passthrough, `render` group, `HSA_OVERRIDE_GFX_VERSION=11.5.1` + `ROCBLAS_USE_HIPBLASLT=1` env, and `-ngl 999 -fa 1 --no-mmap` — frozen by a new ROCm byte-golden, with the Vulkan golden byte-for-byte unchanged.
  2. `villa preflight` (and the switch gate) refuses ROCm bring-up with actionable remediation when `rocminfo`/gfx1151 is absent, kernel < 6.18.4, the firmware is exactly `linux-firmware-20251125`, the HSA override is missing, or a `rocm7-nightlies` image is requested — driven by externalized (`go:embed`-updatable) version ranges + denylist, biased not to over-block a genuinely-working host.
  3. `villa detect --json` reports ROCm-readiness fields (e.g. HSA-override viability, firmware date, ROCm kernel-floor OK) appended after the existing GPU block with a bumped schema version — the frozen v1.0 contract is never reordered.

**Plans**: 3 plans (1 wave)

**Wave 1** *(all three plans are independent — zero file overlap, fully parallel)*

- [x] 07-01-PLAN.md — ROCM-03: multi-value render (devices + group-adds + env block) + `{{range}}` template + new ROCm byte-golden, Vulkan golden byte-identical
- [x] 07-02-PLAN.md — PRE-06: `go:embed` `rocm-policy.json` (floors migration no-op) + `RunROCm`/`RunROCmWithPolicy` checks + `villa preflight --backend rocm` wiring
- [x] 07-03-PLAN.md — DET-04: nested `rocm_readiness` typed-Optional object + `hostProfileSchemaVersion` 1→2 + re-frozen `detect.golden.json`

### Phase 8: `villa backend set` Switch Verb + Rollback

**Goal**: A user can flip the inference backend on a *running* install with one command and never end up with a broken stack — the switch captures the prior working unit, gates cutover on a real generation-probe + ROCm residency proof, and auto-rolls back verbatim on any failure. This is the on-hardware risk concentration where the "just works" bar is won or lost (the v1.0 Phase-2 analog).
**Depends on**: Phase 6 (resolver + residency), Phase 7 (render + preflight)
**Requirements**: BSET-01, BSET-02, BSET-03
**Success Criteria** (what must be TRUE):

  1. `villa backend set rocm` on a running install swaps only `villa-llama` (save-before-restart, regenerate, daemon-reload, restart the inference unit only), preserving the configured model/quant/context, and refuses-with-remediation when the model no longer fits or ROCm preflight blocks — rather than guessing or re-picking.
  2. A failed ROCm bring-up (silent CPU fallback, `load_tensors` hang, allocation cap, firmware fault) auto-rolls back to the verbatim captured prior unit/config and re-readies it, leaving the user's stack as it was (a failed switch is a no-op to the running stack).
  3. Cutover only succeeds after a real generation-probe readiness check + residency proof for the *new* backend passes within a bounded timeout — `systemctl is-active` alone never counts as success.
  4. `villa backend show` reports the active backend, and `villa backend set --dry-run` previews the switch (target, fit verdict, preflight verdict) without mutating config or units.

**Plans**: 2 plans (2 waves)

**Wave 1**

- [x] 08-01-PLAN.md — transactional core `internal/backendswap` (Deps/Result/ProveVerdict + Run capture→mutate→prove→rollback; fit-guard + ROCm-preflight refuse; verbatim rollback; state-machine tests) + exported `inference.PollHealth`/`GenerationProbe` + seam gate extended to cmd/villa [BSET-01, BSET-02] ✅

**Wave 2** *(blocked on Wave 1 — consumes the package's Deps/ProveVerdict)*

- [x] 08-02-PLAN.md — `villa backend` cobra noun (`set`/`show`/`--dry-run`), exit mapping, `liveProve` (bounded pollHealth + generation probe + RunningOffloadVerdict, live `detect.GPUBusyPercent()` D-07 read), `liveBackendSwapDeps`, root registration, command tests [BSET-01, BSET-02, BSET-03]

**Research flag**: on-hardware — real ROCm offload, HSA-override behavior, kernel/firmware sensitivity, `load_tensors` hang detection, the transactional rollback state-machine. Flag for `--research-phase` and the most live UAT.

### Phase 9: `villa bench` (Honest A/B)

**Goal**: A user can prove, on their own loaded model, whether ROCm is actually faster than Vulkan — `villa bench` runs an honest A/B over the running endpoint, reporting prompt-processing and token-generation throughput separately, never a single blended number, over residency-checked runs only. The per-metric throughput delta is the milestone's proof-of-value.
**Depends on**: Phase 6 (a backend to measure), Phase 8 (the switch verb bench composes for `--ab` — bench never re-implements switching)
**Requirements**: BENCH-01, BENCH-02
**Success Criteria** (what must be TRUE):

  1. `villa bench` measures the currently-running backend non-disruptively over `/v1`+`/metrics` and reports prompt-processing and token-generation tok/s as two separate figures (never one blended headline number).
  2. The methodology is honest and stated: discarded warmup, N repetitions with median + stddev/noise band, identical model/quant/context/flags on both sides, and only residency-checked runs counted (a CPU-fallback run is void).
  3. Running `bench` on each backend (flipping via the Phase-8 switch) yields a per-metric Vulkan-vs-ROCm delta with its noise band, so the user gets a data-backed "worth it / not worth it" verdict for their model rather than a generic claim.

**Plans**: 3 plans (2 waves)

**Wave 1** *(parallel — zero file overlap: the leaf llm method + the pure bench core)*

- [x] 09-01-PLAN.md — `internal/llm.OpenAIClient.Complete` non-streaming method + `Timings` type (the honest per-request pp/tg measurement source) [BENCH-01]
- [x] 09-02-PLAN.md — pure `internal/bench` core (BenchSpec/RunTimings/Stats/Deps/Result/Run): warmup-discard, residency void-gate, void-exhaustion WARN, separate pp/tg median+stddev, `--ab` always-restore; fake-Deps recorder tests [BENCH-01, BENCH-02]

**Wave 2** *(blocked on Wave 1 — consumes `llm.Complete` + the `bench` Deps/Run)*

- [x] 09-03-PLAN.md — `cmd/villa/bench.go` cobra noun + `liveMeasure` (liveProve clone) + `liveBenchDeps` (`--ab`→`backendswap.Run`, LOCKED) + exit map + frozen separate-pp/tg `--json` golden + root registration [BENCH-01, BENCH-02]

**Research flag**: on-hardware — the per-model pp-vs-tg delta and ROCm-7.x-vs-6.4.4 ordering are workload-dependent and volatile; validate the residency-checked numbers on the live host before trusting the throughput-delta success criterion. Confirm SELinux `/dev/kfd` behavior here or in Phase 7.

### Phase 10: Backend + tok/s Surfacing

**Goal**: The dashboard, `villa status`, and `villa recommend` all become backend-aware as a single, append-only contract change landed last — `status`/dashboard show the active backend (with image tag) + live token-generation tok/s + a ROCm-readiness indicator, and `recommend` gives honest "ROCm ready / worth trying / verify with bench" advice while Vulkan stays the default. Done last so the byte-frozen `--json` goldens re-freeze exactly once.
**Depends on**: Phase 6 (`BackendFor` in `status`), Phase 9 (the tok/s figure)
**Requirements**: REC-05, DASH-06
**Success Criteria** (what must be TRUE):

  1. The control dashboard and `villa status` show the active backend (with image tag), live token-generation tok/s labeled by backend, and a ROCm-readiness indicator — `status`'s previously-hardcoded `VulkanBackend()` now reflects the configured backend (a correctness fix on a ROCm install).
  2. `villa recommend` keeps Vulkan as the recommended default and surfaces honest ROCm advice ("ready / worth trying / verify with bench") for the pick, without ever promising a guaranteed speed-up or auto-switching.
  3. The new `status`/`detect`/`recommend` fields are append-only additions with a bumped schema version — existing `--json`/golden contracts are re-frozen as reviewed pure-addition diffs, never reordered or retagged.

**Plans**: 3 plans (2 waves)

**Wave 1** *(parallel — zero file overlap: the status surface + the recommend surface)*

- [x] 10-01-PLAN.md — DASH-06: status surface — tail-append Backend/Image/GenTokensPerSec/ROCmReadiness/SchemaVersion to `status.Report` + `foldROCmReadiness` + tok/s seam in `liveStatusDeps` (reuse `metrics.ScrapeMetrics`) + new table rows + SC#1 rocm-residency proving test + re-freeze `status.json.golden` once
- [x] 10-02-PLAN.md — REC-05: recommend surface — append `ROCmAdvice` enum + honesty-safe Note + SchemaVersion to `recommend.Recommendation`, derived purely in `Pick` from `rocm_readiness` (Backend stays vulkan, never promise a speed-up) + render advice + re-freeze `recommend.golden.json` once

**Wave 2** *(blocked on Wave 1 — consumes the new `status.Report` fields via `/api/status`)*

- [x] 10-03-PLAN.md — DASH-06: dashboard surface — the three approved UI-SPEC elements in `dashboard.{html,css,js}` (active backend+image from /api/status, tok/s labeled by backend, tri-state ROCm-readiness badge reusing existing classes) + assert /api/status carries new fields and `metricsView` unchanged

**UI hint**: yes

## Progress

**Execution Order:**
Phases execute in numeric order: 1 → 2 → 3 → 4 → 5

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Hardware Foundation & Preflight Gate | v1.0 | 3/3 | Complete | 2026-06-03 |
| 2. GPU-Validated Inference Slice | v1.0 | 3/3 | Complete | 2026-06-04 |
| 3. Orchestrated Install & Lifecycle | v1.0 | 6/6 | Complete | 2026-06-04 |
| 4. Chat Integration | v1.0 | 3/3 | Complete | 2026-06-05 |
| 5. Control Dashboard | v1.0 | 8/8 | Complete | 2026-06-05 |
| 6. ROCm Backend + Resolver Spine | v1.1 | 3/3 | Complete    | 2026-06-05 |
| 7. ROCm Render Unit + Preflight/Detect | v1.1 | 3/3 | Complete    | 2026-06-06 |
| 8. `villa backend set` Switch Verb + Rollback | v1.1 | 2/2 | Complete   | 2026-06-06 |
| 9. `villa bench` (Honest A/B) | v1.1 | 3/3 | Complete | 2026-06-06 |
| 10. Backend + tok/s Surfacing | v1.1 | 3/3 | Complete   | 2026-06-06 |

### Phase 11: Address v1.1 tech debt: rocm_readiness detect probes + doc reconciliation

**Goal:** Make `internal/detect/readiness_rocm.go`'s `firmwareDateOK()` / `hsaOverrideViable()` real probes (KnownBool on-hardware, honest UNSET off-hardware) so the Phase-10 ROCm-readiness badge reads `ready` on a live ROCm host (closes the DASH-06 SC#1 residual + the DET-04 readiness fields), and reconcile the audit-named documentation drift (6 missing SUMMARY `requirements-completed` tags + the stale 06-REVIEW prose Status line + the REQUIREMENTS.md ROCM-02 note). Post-milestone tech-debt cleanup; no new fields, no schema bump, no golden re-freeze (D-04).
**Requirements**: DET-04, DASH-06 (residual sub-clauses; doc plan is cross-cutting tech-debt — no new REQ-IDs)
**Depends on:** Phase 10
**Plans:** 1/2 plans executed
Plans:

- [x] 11-01-PLAN.md — DET-04/DASH-06: real `firmwareDateOK` (rpm firmware-date probe + detect-local floor/deny seam in gpu_amd.go) + `hsaOverrideViable` (pure gfx1151+substrate derivation) + threaded `computeROCmReadiness` + table tests; golden byte-identical (no re-freeze); on-hardware badge=ready as manual UAT
- [ ] 11-02-PLAN.md — tech-debt: add `requirements-completed` frontmatter to 6 SUMMARYs (DET-04/BSET-01..03/BENCH-02/REC-05, evidence-checked) + fix stale 06-REVIEW prose Status line + human-verify checkpoint on the REQUIREMENTS.md ROCM-02 note (no blind edit)
