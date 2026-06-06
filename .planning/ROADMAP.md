# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware.

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:
- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`

## Phases

<details>
<summary>✅ v1.0 MVP (Phases 1–5) — SHIPPED 2026-06-05</summary>

- [x] Phase 1: Hardware Foundation & Preflight Gate (3/3 plans) — completed 2026-06-03
- [x] Phase 2: GPU-Validated Inference Slice (3/3 plans) — completed 2026-06-04
- [x] Phase 3: Orchestrated Install & Lifecycle (6/6 plans) — completed 2026-06-04
- [x] Phase 4: Chat Integration (3/3 plans) — completed 2026-06-05
- [x] Phase 5: Control Dashboard (8/8 plans) — completed 2026-06-05

</details>

<details>
<summary>✅ v1.1 ROCm Opt-In Backend (Phases 6–11) — SHIPPED 2026-06-06</summary>

**Milestone goal:** Add an opt-in ROCm/HIP inference backend for higher throughput on AMD Strix Halo (gfx1151), gated hard enough to preserve the v1.0 "just works" bar, switchable on a running install with transactional rollback, and benchmarked honestly to prove the per-model win — while Vulkan RADV remains the safe default.

- [x] Phase 6: ROCm Backend + Resolver Spine (3/3 plans) — completed 2026-06-05
- [x] Phase 7: ROCm Render Unit + Preflight/Detect (3/3 plans) — completed 2026-06-06
- [x] Phase 8: `villa backend set` Switch Verb + Rollback (2/2 plans) — completed 2026-06-06 (4/4 on-hardware UAT)
- [x] Phase 9: `villa bench` (Honest A/B) (3/3 plans) — completed 2026-06-06 (3/3 on-hardware UAT; Δpp +4.84 / Δtg −11.15)
- [x] Phase 10: Backend + tok/s Surfacing (3/3 plans) — completed 2026-06-06
- [x] Phase 11: Address v1.1 tech debt — rocm_readiness detect probes + doc reconciliation (2/2 plans) — completed 2026-06-06

See `milestones/v1.1-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

### 📋 Next Milestone (Planned)

No active milestone. Start the next cycle with `/gsd-new-milestone` (questioning → research → requirements → roadmap).

Candidate themes (from REQUIREMENTS.md v1.1.x / Milestone 2 / Future, deferred): `villa bench --compare` + saved report (BENCH-03), backup/restore (BAK-01), `villa doctor` (DOCTOR-01), cumulative usage tracking (USAGE-01), guided TUI install (INSTALL-01), `rocm-6.4.4` alternate image (ROCM-ALT-01); then Qdrant memory + SearXNG search (Milestone 2); macOS/Apple-Silicon (Metal) backend, voice, agents (Future).

## Progress

| Phase | Milestone | Plans Complete | Status | Completed |
|-------|-----------|----------------|--------|-----------|
| 1. Hardware Foundation & Preflight Gate | v1.0 | 3/3 | Complete | 2026-06-03 |
| 2. GPU-Validated Inference Slice | v1.0 | 3/3 | Complete | 2026-06-04 |
| 3. Orchestrated Install & Lifecycle | v1.0 | 6/6 | Complete | 2026-06-04 |
| 4. Chat Integration | v1.0 | 3/3 | Complete | 2026-06-05 |
| 5. Control Dashboard | v1.0 | 8/8 | Complete | 2026-06-05 |
| 6. ROCm Backend + Resolver Spine | v1.1 | 3/3 | Complete | 2026-06-05 |
| 7. ROCm Render Unit + Preflight/Detect | v1.1 | 3/3 | Complete | 2026-06-06 |
| 8. `villa backend set` Switch Verb + Rollback | v1.1 | 2/2 | Complete | 2026-06-06 |
| 9. `villa bench` (Honest A/B) | v1.1 | 3/3 | Complete | 2026-06-06 |
| 10. Backend + tok/s Surfacing | v1.1 | 3/3 | Complete | 2026-06-06 |
| 11. Address v1.1 tech debt | v1.1 | 2/2 | Complete | 2026-06-06 |
