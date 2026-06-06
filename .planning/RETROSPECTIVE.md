# Project Retrospective

*A living document updated after each milestone. Lessons feed forward into future planning.*

## Milestone: v1.1 — ROCm Opt-In Backend

**Shipped:** 2026-06-06
**Phases:** 6 (6–11) | **Plans:** 16 | **Tasks:** 29

### What Was Built
- A second, strictly opt-in ROCm/HIP inference backend behind the existing v1.0 `Backend` interface, selected through a single fail-closed `BackendFor()` resolver — Vulkan RADV stays the byte-identical default.
- A backend-neutral residency-proof engine (HIP `ROCm0` markers, journal fault scan, partial-offload FAIL, `gpu_busy_percent` corroboration) so a silent/partial CPU fallback is never false-green.
- A transactional `villa backend set` switch (capture→mutate→prove→rollback) that flips the backend on a running install and auto-rolls back verbatim on any failure — 4/4 on-hardware UAT including forced CPU-fallback rollback and a bounded never-ready timeout.
- An honest A/B `villa bench` reporting prompt-processing and token-generation tok/s **separately** (never a blended headline), over residency-checked runs only — live proof-of-value Δpp +4.84 / Δtg −11.15.
- Backend-aware `recommend`/`status`/dashboard surfacing as a single append-only contract change, plus real `rocm_readiness` firmware/HSA detect probes (Phase 11).

### What Worked
- **Spine-first phase ordering.** Building the `BackendFor` resolver + `ResidencyProof()` interface extension first (Phase 6, off-hardware, behavior no-op under the Vulkan default) meant render, switch, bench, and surfacing all slotted onto a proven seam without re-rolling shared logic.
- **Concentrating on-hardware risk into one phase.** Phase 8 (the switch verb) absorbed the live ROCm offload / HSA-override / rollback risk; phases around it stayed off-hardware and deterministic. The on-hardware UAT closed all four items including both failure paths via fault injection.
- **Append-only contract discipline.** Landing all surfacing last (Phase 10) let the byte-frozen `--json`/dashboard goldens re-freeze exactly once, as reviewed pure-addition diffs — no contract churn across the milestone.
- **Composition over duplication.** `bench --ab` delegating the flip to `backendswap.Run` (explicit anti-pattern: never re-implement switching) kept one switch implementation.
- **Externalized policy as data.** ROCm version floors + firmware/image denylists in a `go:embed` `rocm-policy.json` made the preflight gate updatable without code changes and biased it against over-blocking a working host.

### What Was Inefficient
- **SUMMARY frontmatter tagging lag.** 6 SUMMARYs shipped without `requirements-completed` tags despite being verified SATISFIED in their VERIFICATION.md — surfaced by the milestone audit and needed a dedicated cleanup plan (11-02). The automated 3-source cross-check is only as good as the tags.
- **A functional follow-up slipped to a cleanup phase.** The `rocm_readiness` badge read `unknown` on a live ROCm host because the firmware/HSA probes were stubbed — caught by the audit, fixed in Phase 11. Detect-path probes should have been scoped into Phase 7 alongside the readiness fields they back.
- **Nyquist validation-status drift.** Four phases carried full green verification suites but `VALIDATION.md` left in `draft`/`nyquist_compliant:false` — process/status debt, not a coverage hole, but it muddied the audit signal.
- **Milestone work accumulated on a feature branch** (`feat/phase-09-...`, ~141 commits) rather than merging per-milestone to `main` as v1.0 did via PR #1 — a release-hygiene gap to close on ship.

### Patterns Established
- **Polymorphic resolver + grep-gated seam:** one `BackendFor()` resolution point, with a `TestSeamGrepGate` walking both `internal/` and `cmd/villa` to keep backend marker strings from leaking past the seam.
- **Capture→prove→cutover→rollback** as the template for any mutating switch on a running stack: `systemctl is-active` alone never counts as success — gate on a real generation-probe + residency proof within a bounded timeout.
- **Honest measurement:** separate pp/tg figures, warmup-discard, N-rep median+stddev, residency-void-gating, and advice that never promises a speed-up.
- **Append-only + schema-bump + golden re-freeze-once** for evolving frozen `--json`/dashboard contracts.

### Key Lessons
1. Scope detect-path *probes* into the same phase as the detect *fields* they populate — a readiness field whose probe is stubbed is a latent false signal that surfaces only on-hardware.
2. Enforce SUMMARY `requirements-completed` tagging at plan-completion time, not at milestone-audit time — the cross-check depends on it and the lag compounds across phases.
3. Concentrating on-hardware risk into a single well-bounded phase (with fault-injected failure-path UAT) is a repeatable de-risking pattern for fragile hardware backends.
4. Reconcile `VALIDATION.md` status as part of phase verification so the milestone audit reads a clean Nyquist signal.

### Cost Observations
- Model mix: adaptive profile (Opus-led planning/verification, Sonnet execution).
- Notable: most plans were small and fast (many 3–6 min); the spine and on-hardware switch phases (6, 8) were the heaviest. Worktrees were skipped for the hand-orchestrated on-hardware phases to keep git hygiene simple on the live box.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0 MVP | 5 (1–5) | 23 | Vertical-MVP slices; on-hardware UAT per phase; PR-to-`main` merge (PR #1) |
| v1.1 ROCm Opt-In | 6 (6–11) | 16 | Spine-first ordering; on-hardware risk concentrated in one phase; append-only contract discipline; first exercise of the `Backend` seam |

### Cumulative Quality

| Milestone | Tests | Packages | Posture |
|-----------|-------|----------|---------|
| v1.0 MVP | ~430 green | — | Phases 4 & 5 STRIDE-secured (12/12 + 19/19), threats_open=0 |
| v1.1 ROCm Opt-In | ~548 green | 16 | Milestone audit `tech_debt`: 13/13 reqs, 0 critical blockers |

### Top Lessons (Verified Across Milestones)

1. On-hardware UAT per phase (not just at milestone end) catches the failure modes that off-hardware tests structurally cannot (silent CPU fallback, OOM, HSA-override behavior).
2. Freeze `--json`/dashboard contracts with byte-goldens early and only ever extend them append-only — it has protected the dashboard read-model across both milestones.
