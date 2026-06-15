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

## Milestone: v1.3 — Memory & Knowledge

**Shipped:** 2026-06-11
**Phases:** 6 (18–23) | **Plans:** 20 | **Tasks:** 41

### What Was Built
- A config-driven local memory stack: digest-pinned Qdrant + a dedicated `villa-embed` llama-server (nomic-embed-text-v1.5, 768-dim) as rootless Quadlet managed services on `villa.network`, container-DNS only, embedding GGUF pre-staged at install.
- Open WebUI Memory/RAG wired env-only behind the orchestrate seam (`ENABLE_PERSISTENT_CONFIG=False` mandatory) with the full offline/telemetry kill-set and `villa verify memory` — a negative-control-first runtime zero-outbound proof.
- `villa recall index|status` — a pure `internal/recall` diff core that semantically indexes past chats into an OWUI Knowledge collection through OWUI's own embed path, with clean-replace incrementality and typed-Unknown staleness honesty.
- Control-plane fit + gates: embedding-footprint reservation before the chat fit (recommend schema 2), MEM-PRE disk/headroom preflight gates, and doctor residency proven under a real embedding workload.
- Surfacing + durability: `status.Report` 2→3 (single golden re-freeze) with per-service in-network memory health + a dashboard Memory panel; Qdrant-volume backup/restore (manifest v2 with embedding dim, clean-recreate-before-import, skew WARN+confirm); structural swap isolation (D-09) + fail-closed skew refusal at `recall index`.

### What Worked
- **The Phase-18 research spike paid for itself.** Pinning the version-sensitive OWUI env contract, embeddings runtime, and model footprint against the exact shipped digest BEFORE any golden froze meant zero env-contract rework across Phases 19–23.
- **Integrate-not-rebuild held under pressure.** Zero new first-party Go libraries; recall reuses OWUI's own chunk/embed/retrieve path (citations worked for free), rather than writing Qdrant directly — the riskiest design choice in the milestone and it validated cleanly on-hardware.
- **Negative-control-first proofs caught real things.** `verify memory` proves the egress gate is real before trusting the pass; the 22-04 residency drive exposed the actual binding limit (512-token physical embed batch, not the 8192 ctx); the 23-05 stopped-embed drill closed the carried per-row health false-green live.
- **Single-contract discipline, third milestone running.** Exactly one byte-frozen evolution (`status.Report` 2→3), landed last, frozen once; recommend evolved separately (1→2) in its own phase. The integration audit confirmed no schema churn anywhere else.
- **Deferred human gates were honestly tracked and closed.** Phase 22's auto-approved sign-off was carried as an explicit verification item and closed by the operator in the 23-05 live drill — not silently dropped.

### What Was Inefficient
- **VALIDATION.md status drift for the THIRD consecutive milestone.** Phases 18–19 shipped verified `passed` but left `nyquist_compliant: false` drafts. The v1.1 and v1.2 lessons were logged but still not enforced in-flow.
- **Phase 20 closed without a formal VERIFICATION.md** — the 6/6 UAT + approved VALIDATION carried the evidence, but the audit's 3-source cross-reference had to fall back to UAT parsing; one phase's verification artifact shape diverged from the other five.
- **SUMMARY frontmatter inconsistency** (`requirements:` vs `requirements_completed:`, several empty) degraded the automated requirements cross-check; coverage had to be re-derived from VERIFICATION tables.
- **Auto-extracted MILESTONES accomplishments were noisy again** (a process note surfaced as the first "accomplishment") and needed the same hand-rewrite as v1.2.
- **Phase branches accumulated unmerged again** (18–23 stacked on each other, `main` 171 commits behind at close) — the same release-hygiene gap flagged at v1.1 and v1.2 close.

### Patterns Established
- **Managed-service seam category** — non-inference container images (`QdrantImage()`/`EmbedImage()`, like `openWebUIImage`) live behind orchestrate accessors, NOT the inference `BackendFor`/grep-gate scope; backup/doctor/status read the accessors, never re-type literals.
- **Negative-control-first runtime proof** — a privacy/health gate must first prove it CAN fail (egress-open → FAIL) before its pass is trusted; flag-trust and install-time green are insufficient.
- **Structural invariant tests** — a reflect-pinned Deps-surface test proves a verb *cannot* touch a subsystem (swap vs memory units), stronger than behavioral tests for isolation guarantees.
- **Conservative typed-Unknown reservation + on-hardware measurement** — reserve a pinned constant (512 MiB) on unknown footprint, never silent 0, then measure live (D-05: ~110–217 MiB peak) and record the "constant stands" decision.

### Key Lessons
1. **Make artifact-status reconciliation a phase-close gate, not an audit discovery.** Three milestones in a row (VALIDATION drift; now also a missing VERIFICATION.md and frontmatter key drift) — the verifier should refuse to close a phase with draft VALIDATION or absent VERIFICATION, instead of the milestone audit absorbing it.
2. **Spike version-sensitive third-party contracts against the exact pinned digest before freezing anything** — the cheapest de-risk in the milestone (Phase 18) prevented the most expensive class of rework (golden re-freezes).
3. **Guard where the hazard actually lives.** CTRL-05's literal reading ("make swap warn") would have been theater; the dimension hazard lives at config-change consumption points (recall index / install / restore). Reinterpreting a requirement against its intent — documented — beat implementing its letter.
4. **Real workloads find real limits.** The embed drive had to respect the llama-server physical batch (-ub 512), not the configured ctx (8192) — proofs that drive synthetic load discover the binding constraint that static analysis misses.

### Cost Observations
- Model mix: adaptive profile (Opus-led planning/verification/close, Sonnet execution).
- Sessions: milestone executed 2026-06-09 → 2026-06-11; 20 plans at 2–25 min each; heaviest were the on-hardware drill plans (19-03, 21-03, 22-04, 23-05) where live verification dominated.

---

## Milestone: v1.4 — Coding Agent

**Shipped:** 2026-06-15
**Phases:** 5 (24–28) | **Plans:** 18 | **Tasks:** 39

### What Was Built
- A strictly-local terminal coding agent as an optional `villa install --coding-agent` addon: **Crush v0.76.0** delivered as a pinned, SHA-256-verified host binary with `crush.json` rendered from `config.toml` (both kill switches, one loopback provider, `villa-`prefixed model ids), launched through a locked-down `villa code` verb, drift detected-never-auto-corrected.
- Coder catalog (schema 2→3): three revision-pinned `role:"coder"` entries (Qwen3-Coder-30B-A3B; Qwen3-Coder-Next Q4/Q3), each qualified agent-in-the-loop on gfx1151 (49/49 offload, exact-match KV, real Crush read→edit→verify loops) before freezing.
- `recommend` coder fit at agent-profile ctx with honest `swap`/`shared` residency as a pure fit-math output (schema 2→3); coding mode as an explicit transactional `villa coding-mode enter|exit` verb cloning `backendswap` + composing `modelswap` (under-load residency prove, never auto-flip).
- `villa verify agent`: negative-control-first runtime zero-outbound proof + a llama-down cloud-fallback control, accepted on-hardware under a real rootless-netns nft FORWARD block (closed the Phase-20 egress-block gap).
- Surfacing landed last: `status.Report` 3→4 (single golden re-freeze) coding block + hidden-until-data dashboard Agent panel; doctor agent checks (schema 1→2); backup coverage (crush.json in, binary excluded); per-model usage + cache-effectiveness signals.

### What Worked
- **Research killed the wrong agent before a line was written.** Selecting Crush over OpenCode on zero-outbound *structural* grounds (not popularity) and rejecting the Qdrant code-collection premise on evidence saved an entire mis-built subsystem — the riskiest scoping calls were made in research and held.
- **Negative-control-first, fourth milestone running.** `villa verify agent` proved the egress gate is real before trusting a pass, and an *ineffective* host-main-netns block was correctly REJECTED on-hardware — no fabricated PASS — which is what surfaced that rootless egress traverses FORWARD inside the netns, not the host chain.
- **On-hardware qualification gated the catalog, not bench scores.** Every coder entry had to drive a real tool-call loop with KV measured at agent ctx before freezing; D-13 recorded the toolbox-keep decision with evidence at an explicit operator checkpoint.
- **Single-contract discipline, fourth milestone running.** Exactly one byte-frozen evolution (`status.Report` 3→4), landed last, frozen once; doctor (1→2) and backup (2→3) evolved as their own distinct contracts. The integration audit confirmed exactly one status bump.
- **Composition over re-implementation held.** `codingmode` cloned `backendswap` + composed `modelswap`; the install addon composed the P26 agent core; `villa verify agent` and `villa doctor` share the single `liveAgentToolCallProbe` driver — no forks.

### What Was Inefficient
- **VALIDATION.md status drift for the FOURTH consecutive milestone.** Phase 28 shipped verified `passed` (5/5) but never got a `28-VALIDATION.md` — the same artifact-reconciliation gap logged at v1.1/v1.2/v1.3 close, still not enforced in-flow.
- **SUMMARY frontmatter inconsistency again** — Phase-28 and gap-closure 27-05/06 SUMMARYs encode requirements in `tags`/`provides` rather than `requirements-completed`, so the audit's requirements cross-check had to fall back to VERIFICATION SC tables.
- **Auto-extracted MILESTONES accomplishments were noisy again** — the CLI dumped 14 raw items including a stray on-hardware-context line and a mid-sentence netns fragment; needed the same hand-rewrite to 5 phase-level accomplishments as v1.2/v1.3.
- **Phase branches accumulated unmerged again** (24–28 stacked; `main` 151 commits behind at close) — the identical release-hygiene gap flagged at every prior milestone close.
- **A real config-is-source-of-truth bug shipped as tracked debt** (27-04 backend-revert) — found during on-hardware acceptance, correctly scoped out rather than rushed, but it's a regression in a core invariant that the install path should have caught earlier.

### Patterns Established
- **Host-binary delivery seam** — a pinned, checksum-before-extract `go:embed` policy + traversal-guarded stdlib-tar install for a NON-container first-party tool (Crush), distinct from the inference image seam; the renderer derives config from `config.toml`, never hand-edited.
- **Explicit-verb-never-auto transactional mode** — coding mode mirrors the ROCm `backend set` precedent: clone `backendswap`, prove residency under load (idle-green = rollback), and a structural guard asserting the mode never auto-flips on connect.
- **Negative-control-first now covers the network namespace, not just the host** — the on-hardware egress proof had to discover and block the rootless-netns FORWARD path; an ineffective block must be REJECTED, never absorbed as a pass.
- **Acknowledge-with-evidence milestone close** — a `tech_debt` audit with zero blockers can close by recording each item (bug / process / advisory / by-design) in STATE.md Deferred Items with its reproduction and remediation, rather than gating the release on non-critical debt.

### Key Lessons
1. **Enforce artifact-status reconciliation as a phase-close gate — fourth time logging this.** Draft/absent VALIDATION and frontmatter key drift have now cost audit rework in four consecutive milestones; the verifier should refuse to close a phase without a compliant VALIDATION + a `requirements-completed` key, instead of the milestone audit absorbing it every time.
2. **Reject the wrong architecture in research, loudly and with structural reasons.** "OpenCode can't be locked to zero-outbound" and "a text embedder over chunked code is stale on first edit" were cheap conclusions that prevented the most expensive class of rework — building and then ripping out a subsystem.
3. **Prove the proof on the real topology.** A privacy gate validated against the wrong network layer (host chain vs rootless netns) would have false-greened; driving the actual egress path and rejecting an ineffective block is what made PRIV-06 real.
4. **Scope a found-in-acceptance bug out cleanly rather than rushing it.** The 27-04 backend-revert was real but out of v1.4's coder scope; tracking it with exact repro + remediation (reversible via `villa backend set rocm`) beat a hurried fix in a frozen milestone.

### Cost Observations
- Model mix: adaptive profile (Opus-led planning/verification/close, Sonnet execution); Opus 4.8 (1M context) for the milestone close.
- Sessions: milestone executed 2026-06-12 → 2026-06-15; 18 plans; heaviest were the on-hardware acceptance plans (24-03/04 qualification, 26-03 launch, 27-04 egress proof) where live verification + a real netns egress block dominated.

---

## Cross-Milestone Trends

### Process Evolution

| Milestone | Phases | Plans | Key Change |
|-----------|--------|-------|------------|
| v1.0 MVP | 5 (1–5) | 23 | Vertical-MVP slices; on-hardware UAT per phase; PR-to-`main` merge (PR #1) |
| v1.1 ROCm Opt-In | 6 (6–11) | 16 | Spine-first ordering; on-hardware risk concentrated in one phase; append-only contract discipline; first exercise of the `Backend` seam |
| v1.2 Operability | 6 (12–17) | 19 | Research-converged ordering (one frozen contract in flight at a time); one pure core per feature + cmd/orchestrate seam; honest A/B disproved a perf premise; capstone over a finished surface |
| v1.3 Memory & Knowledge | 6 (18–23) | 20 | Integration milestone (zero new Go libraries); digest-pinned spike before any golden froze; managed-service seam category; negative-control-first runtime proofs; structural invariant tests |
| v1.4 Coding Agent | 5 (24–28) | 18 | Research rejected the wrong agent (Crush over OpenCode) + the wrong memory model (Qdrant code-collection) on structural evidence; host-binary delivery seam; explicit-verb transactional coding mode; egress proof extended to the rootless netns; acknowledge-with-evidence `tech_debt` close |

### Cumulative Quality

| Milestone | Tests | Packages | Posture |
|-----------|-------|----------|---------|
| v1.0 MVP | ~430 green | — | Phases 4 & 5 STRIDE-secured (12/12 + 19/19), threats_open=0 |
| v1.1 ROCm Opt-In | ~548 green | 16 | Milestone audit `tech_debt`: 13/13 reqs, 0 critical blockers |
| v1.2 Operability | ~563 green | 16+ | Milestone audit **PASSED**: 13/13 reqs, 5/5 integration flows, 6/6 phases Nyquist-compliant |
| v1.3 Memory & Knowledge | 885 green | 21+ | Milestone audit **PASSED**: 22/22 reqs, 15/16 integration connections (0 blockers), 5/5 E2E flows; Nyquist 4 compliant / 2 partial |
| v1.4 Coding Agent | 1321 green | 24 | Milestone audit `tech_debt`: 17/17 reqs, integration PASS (7/7 seam groups, 0 blockers), 6/6 E2E flows; Nyquist 4/5 (P28 open); debt acknowledged at close |

### Top Lessons (Verified Across Milestones)

1. On-hardware UAT per phase (not just at milestone end) catches the failure modes that off-hardware tests structurally cannot (silent CPU fallback, OOM, HSA-override behavior).
2. Freeze `--json`/dashboard contracts with byte-goldens early and only ever extend them append-only — it has protected the dashboard read-model across all three milestones.
3. **Status-frontmatter lag recurs every milestone** (SUMMARY `requirements-completed`, `VALIDATION.md` nyquist status, UAT/quick-task terminal vocabulary; in v1.3 also a missing per-phase VERIFICATION.md; in v1.4 a missing `28-VALIDATION.md`) and each time costs a close-time reconciliation pass — now verified across v1.1, v1.2, v1.3, AND v1.4 (four consecutive). The durable fix is to reconcile these at phase-verification time, enforced by the verifier as a phase-close gate, rather than rediscovering them at the milestone-close audit.
4. Negative-control-first proofs (the gate must demonstrably be able to FAIL before its PASS is trusted) generalize beyond offload-assertion — v1.3 applied the pattern to egress (verify memory), service health (stopped-embed drill), and skew refusal (provoked WARN); v1.4 extended it to the rootless network namespace (`verify agent` rejected an *ineffective* host-chain block on-hardware before trusting the netns-FORWARD block), each catching or closing a real false-green.
5. Research that rejects the wrong architecture on **structural** grounds (v1.4: OpenCode unlockable to zero-outbound; a text embedder over chunked code stale on first edit) is the cheapest de-risk available — it prevents the most expensive rework class (build-then-rip-out) before any code is written.
