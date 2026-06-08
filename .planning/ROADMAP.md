# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware. v1.2 (Operability) extends that bar to "and stays operable, recoverable, and measurable over time."

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)
- ✅ **v1.2 Operability** — Phases 12–17 (completed 2026-06-08; PR-to-`main` + tag `v1.2` pending via `/gsd-ship`)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:

- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`
- `milestones/v1.2-ROADMAP.md` · `milestones/v1.2-REQUIREMENTS.md` · `milestones/v1.2-MILESTONE-AUDIT.md`

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

<details>
<summary>✅ v1.2 Operability (Phases 12–17) — SHIPPED 2026-06-08</summary>

**Milestone goal:** Harden VillaStraylight into an operable, recoverable daily-driver — self-diagnosis, backup/restore, comparative benchmarking, usage history, a guided install, and a TG-tuned ROCm option — without weakening the v1.0 "just works" bar or the strictly-local / zero-telemetry posture.

**Build order was research-converged:** seam-locked + composition features first, then the two persistence features with their byte-frozen evolutions staggered so only one byte-frozen contract evolved at a time, then the destructive backup, then the TUI capstone over the finished surface.

- [x] Phase 12: `rocm-6.4.4` Alternate Backend (3/3 plans) — completed 2026-06-07 (capability shipped; honest A/B disproved the perf premise — Vulkan stays tg default)
- [x] Phase 13: `villa doctor` Health Diagnosis (3/3 plans) — completed 2026-06-07 (on-hardware UAT 3/3)
- [x] Phase 14: Saved Bench Reports + `--compare` (3/3 plans) — completed 2026-06-07 (UAT PASS; Δtg +10.39 vulkan>rocm)
- [x] Phase 15: Cumulative Usage Tracking (4/4 plans) — completed 2026-06-07
- [x] Phase 16: Backup / Restore (3/3 plans) — completed 2026-06-07 (UAT 4/5 + 1 documented cross-host limitation)
- [x] Phase 17: Guided TUI Install (Capstone) (3/3 plans) — completed 2026-06-08 (on-hardware UAT 3/3)

Audit PASSED — 13/13 requirements, 5/5 integration flows, 6/6 phases Nyquist-compliant. See `milestones/v1.2-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

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
| 12. `rocm-6.4.4` Alternate Backend | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 13. `villa doctor` Health Diagnosis | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 14. Saved Bench Reports + `--compare` | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 15. Cumulative Usage Tracking | v1.2 | 4/4 | Complete    | 2026-06-07 |
| 16. Backup / Restore | v1.2 | 3/3 | Complete    | 2026-06-07 |
| 17. Guided TUI Install | v1.2 | 3/3 | Complete    | 2026-06-08 |
