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

## Milestone: v1.2 — Operability

**Shipped:** 2026-06-08
**Phases:** 6 (12–17) | **Plans:** 19 | **Tasks:** 24

### What Was Built
- `villa doctor` — a one-shot, read-only health diagnosis composing the shipped preflight + status + residency-proof cores plus config-vs-disk drift into a doctor-owned PASS/WARN/FAIL report (0/1/2 exit), offload-FAIL dominating a health-200, ROCm residency-supersession so a proven opt-in ROCm install reaches exit 0.
- Saved bench reports (versioned JSONL under XDG, pp/tg separate, residency-void recorded) + read-only `villa bench --compare`/`--list` behind a comparability guard that refuses cross-fingerprint deltas.
- Cumulative, reset-aware, counts-only token usage folded from monotonic `_total` counters, surfaced via ONE append-only `status.Report.usage` field (schema 1→2) with the dashboard as sole mutex-guarded writer — zero new outbound.
- `villa backup`/`restore` — a self-describing local archive (config + Open WebUI volume + usage + bench; weights excluded) with SHA-256 manifest and a transactional, skew-warning, clean-recreate-before-import restore mirroring `backendswap`.
- A `charmbracelet/huh` guided TUI install composing the finished pipeline as a pure collector — byte-identical to the flag path, `--no-tui`/non-TTY bypass, NO_COLOR-degradable, single static CGO-free binary (the milestone's only new dependency).
- An alternate digest-pinned `rocm-6.4.4` backend behind the seam (ROCM-ALT-01) — capability shipped, perf premise honestly disproven on-hardware.

### What Worked
- **Research-converged build order held up.** Seam-locked/composition features first, only ONE byte-frozen contract (`status.Report`) evolving at a time, destructive backup before the TUI capstone — no contract churn, no compounded risk. The capstone composed an already-finished, proven surface.
- **One new pure core per feature, host effects behind a seam.** `doctor`/`benchstore`/`usage`/`backup` are exec-free pure cores (SeamGrepGate green); podman volume I/O reused a cmd-tier fixed-arg seam cloned from `uninstall.go` — `orchestrate` stayed the only intentionally-impure module, and every feature was unit-testable off-hardware.
- **Honest A/B did its job.** ROCM-ALT-01's perf hypothesis was *disproven* on-hardware (Vulkan still leads tg) — the milestone shipped the capability + the truthful measurement rather than a promised speed-up. "Prove, don't assume" caught a false premise.
- **On-hardware UAT found real regressions.** Phase 16 UAT surfaced+fixed WR-05 (store-guard broke `/tmp` volume staging) with a regression test — the live path doing what off-hardware structurally can't.
- **Doctor owns its own schema.** Composing the status read-model without extending its byte-frozen contract (doctor `schema_version=1` distinct from status) avoided evolving a second frozen contract.

### What Was Inefficient
- **VALIDATION.md status drift recurred (same as v1.1).** Four phases (14–17) shipped green test suites but left `VALIDATION.md` in `draft`/`nyquist_compliant:false` — needed a post-hoc `/gsd-validate-phase --auto` reconciliation pass for all four at close (zero actual gaps; pure status lag). The v1.1 lesson was logged but not yet enforced in-flow.
- **Close-gate frontmatter lag.** Three completed quick-task SUMMARYs lacked a top-level `status:` field and three UAT files used `status: passed` (not the audit's terminal `complete`/`resolved` vocabulary) — all flagged as false-positive "open items" at close and needed reconciliation.
- **Milestone work again accumulated on phase branches** not yet merged to `main` (same release-hygiene gap flagged in v1.1) — plus ~77 stale v1.1 phase-dir deletions lingered uncommitted in the working tree until close.
- **Auto-extracted MILESTONES accomplishments were noisy** (code-review artifacts + per-plan granularity) and needed a hand-rewrite to phase-level summaries.

### Patterns Established
- **One pure core per decision-feature + a cmd/orchestrate seam for host effects** — keeps `orchestrate` the sole impure module while adding four new capabilities.
- **Flat JSONL/JSON persistence under `$XDG_DATA_HOME/villa/`** (never `config.toml`, never cgo-SQLite) for append-mostly operability data, schema-versioned from day one and golden-frozen before any writer.
- **Transactional restore = `backendswap` discipline + clean-recreate-before-import** (because `podman volume import` merges and doesn't auto-create) — capture→quiesce→recreate→prove→verbatim rollback.
- **Validated-but-outcome-negative is a legitimate close.** A requirement can be met (capability delivered as specced) while its hypothesis is disproven — recorded as such, not as a gap.

### Key Lessons
1. **Reconcile `VALIDATION.md` (and UAT/quick-task `status:`) at phase-verification time, not at milestone close.** This is the second milestone the same status-lag cost a cleanup pass — it should be a verification-step gate, not an audit-time discovery.
2. Align UAT/quick-task terminal-status vocabulary (`complete`/`resolved`) with what the close audit recognizes, so "passed" work doesn't read as an open item.
3. Merge per-milestone to `main` (PR, as v1.0 did) at ship time — don't let phase branches and stale deletions accumulate across two milestones.
4. The research-converged "one frozen contract in flight at a time" ordering is a repeatable way to ship multiple persistence features without contract churn — keep using it.

### Cost Observations
- Model mix: adaptive profile (Opus-led planning/verification/close, Sonnet execution).
- Sessions: milestone executed 2026-06-07 → 2026-06-08; most plans small/fast (2–22 min); the capstone (P17) and backup/restore (P16) were the heaviest. Worktrees skipped for hand-orchestrated on-hardware phases.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0 MVP | 5 (1–5) | 23 | Vertical-MVP slices; on-hardware UAT per phase; PR-to-`main` merge (PR #1) |
| v1.1 ROCm Opt-In | 6 (6–11) | 16 | Spine-first ordering; on-hardware risk concentrated in one phase; append-only contract discipline; first exercise of the `Backend` seam |
| v1.2 Operability | 6 (12–17) | 19 | Research-converged ordering (one frozen contract in flight at a time); one pure core per feature + cmd/orchestrate seam; honest A/B disproved a perf premise; capstone over a finished surface |

### Cumulative Quality

| Milestone | Tests | Packages | Posture |
|-----------|-------|----------|---------|
| v1.0 MVP | ~430 green | — | Phases 4 & 5 STRIDE-secured (12/12 + 19/19), threats_open=0 |
| v1.1 ROCm Opt-In | ~548 green | 16 | Milestone audit `tech_debt`: 13/13 reqs, 0 critical blockers |
| v1.2 Operability | ~563 green | 16+ | Milestone audit **PASSED**: 13/13 reqs, 5/5 integration flows, 6/6 phases Nyquist-compliant |

### Top Lessons (Verified Across Milestones)

1. On-hardware UAT per phase (not just at milestone end) catches the failure modes that off-hardware tests structurally cannot (silent CPU fallback, OOM, HSA-override behavior).
2. Freeze `--json`/dashboard contracts with byte-goldens early and only ever extend them append-only — it has protected the dashboard read-model across all three milestones.
3. **Status-frontmatter lag recurs every milestone** (SUMMARY `requirements-completed`, `VALIDATION.md` nyquist status, UAT/quick-task terminal vocabulary) and each time costs a close-time reconciliation pass — verified across v1.1 and v1.2. The durable fix is to reconcile these at phase-verification time, ideally enforced by the verifier, rather than rediscovering them at the milestone-close audit.
