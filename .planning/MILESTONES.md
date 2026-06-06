# Milestones

## v1.1 ROCm Opt-In Backend (Shipped: 2026-06-06)

**Phases completed:** 6 phases (6–11), 16 plans, 29 tasks
**Git range:** `v1.0` → `c62eb52` (141 commits, 160 files changed, +23,360 / −328)
**Timeline:** 2026-06-05 → 2026-06-06
**Codebase:** 129 Go files, ~26.3k LOC; full suite green (16 packages)

**Proof-of-value (measured on-hardware 2026-06-06):** `villa bench --ab` on the live gfx1151 host → **Δpp +4.84 / Δtg −11.15** — ROCm wins prompt-processing, regresses token-generation. The milestone honours its honesty constraint: `recommend` never promises a speed-up; the user proves it per-model with `bench`.

**Key accomplishments:**

- **Phase 6 — ROCm backend + resolver spine:** a backend-neutral residency-proof engine (`ResidencyMarkers` + `ResidencyProof()` on the `Backend` interface, dual offload-assert scrapes, journal fault scan, 0<N<M partial-offload FAIL, D-06 `gpu_busy_percent` fold) and `backend_rocm.go` (digest-pinned `rocm-7.2.4`, kfd+dri, render group, ordered HSA→hipBLASLt env, ROCm0 markers) selected through a single `BackendFor()` resolver that fails closed — Vulkan stays the default, byte-identical.
- **Phase 7 — render + preflight + detect:** renders the ROCm `villa-llama.container` as a byte-frozen additive delta over Vulkan; a reusable `RunROCm` host-fitness gate driven by a `go:embed` `rocm-policy.json` that refuses-with-remediation only on confident known-bad (firmware 20251125, nightly image, kernel < 6.18.4, missing HSA override, non-gfx1151) and degrades unevaluable signals to WARN; `villa detect --json` gains an append-only `rocm_readiness` block (schema 1→2).
- **Phase 8 — `villa backend set` switch + rollback:** the transactional `internal/backendswap` capture→mutate→prove→rollback state machine flips the backend on a running install, gating cutover on a real generation-probe + residency proof and auto-rolling back verbatim on any failure — **4/4 on-hardware UAT** (happy-path cutover, dry-run/show, forced CPU-fallback rollback, bounded 5m timeout).
- **Phase 9 — honest A/B `villa bench`:** non-streaming `llm.Complete` + a pure `internal/bench` core report prompt-processing and token-generation tok/s separately (never blended), warmup-discarded, N-rep median+stddev, residency-void-gated, with `--ab` delegating the flip to `backendswap.Run` (never re-implementing switching) — **3/3 on-hardware UAT**.
- **Phase 10 — backend + tok/s surfacing:** `villa status`, `recommend`, and the control dashboard became backend-aware as a single append-only contract change — active backend + image tag, live tok/s labeled by backend, tri-state ROCm-readiness badge, honest ROCm advice — with the byte-frozen `--json`/goldens re-frozen exactly once.
- **Phase 11 — tech-debt cleanup:** made the `rocm_readiness` firmware/HSA detect probes real (live-verified badge reads `ready`, backend `rocm` on the gfx1151 host) and reconciled the milestone-audit documentation drift (6 SUMMARY `requirements-completed` tags, the stale 06-REVIEW status line, the REQUIREMENTS.md ROCM-02 note).

**Milestone audit:** `tech_debt` verdict — 13/13 requirements satisfied, 5/5 phases verified, 5/5 integration seams wired, 3/3 E2E flows complete; 0 critical blockers. Highest-value debt (rocm_readiness probes + doc reconciliation) closed inline by Phase 11. Remaining items are advisory hardening notes and Nyquist validation-status drafts.

**Known deferred items at close:** 1 — quick task `260606-p3a` is complete (commit `8aa9c90`, in Quick Tasks Completed) but its task-status frontmatter reads `unknown` (tag lag only). See STATE.md → Deferred Items.

---
