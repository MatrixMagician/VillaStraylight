# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware. v1.2 (Operability) extended that bar to "and stays operable, recoverable, and measurable over time." v1.3 (Memory & Knowledge) extended it to "and remembers the user and their documents across chats — strictly local." v1.4 (Coding Agent) extends it again to "and gives the operator a strictly-local terminal coding agent, wired to a fit-guarded coding model."

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)
- ✅ **v1.2 Operability** — Phases 12–17 (shipped 2026-06-08, tag `v1.2`)
- ✅ **v1.3 Memory & Knowledge (local RAG)** — Phases 18–23 (shipped 2026-06-11, tag `v1.3`)
- 🚧 **v1.4 Coding Agent** — Phases 24–28 (in progress)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:

- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`
- `milestones/v1.2-ROADMAP.md` · `milestones/v1.2-REQUIREMENTS.md` · `milestones/v1.2-MILESTONE-AUDIT.md`
- `milestones/v1.3-ROADMAP.md` · `milestones/v1.3-REQUIREMENTS.md` · `milestones/v1.3-MILESTONE-AUDIT.md`

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

<details>
<summary>✅ v1.3 Memory & Knowledge (Phases 18–23) — SHIPPED 2026-06-11</summary>

**Milestone goal:** Give VillaStraylight a strictly-local memory system so the assistant remembers the user across chats and can recall past conversations and uploaded documents — **integrate + orchestrate, never rebuild** (Go stays control-plane only). Two new digest-pinned managed-service Quadlet units (Qdrant + a dedicated embeddings llama-server) wired into Open WebUI's native Memory/RAG by ENV only; `villa` recommends, installs, gates, surfaces, and backs up the new stack — with zero new outbound.

- [x] Phase 18: Memory Spine — config core + embeddings/wiring research spike (2/2 plans) — completed 2026-06-09
- [x] Phase 19: Vector Store + Local Embeddings Services (3/3 plans) — completed 2026-06-09
- [x] Phase 20: Open WebUI Memory/RAG Wiring + Offline Lockdown (3/3 plans) — completed 2026-06-10 (on-hardware UAT 6/6 incl. runtime zero-outbound proof)
- [x] Phase 21: Conversational Recall Indexer (3/3 plans) — completed 2026-06-10 (on-hardware SC 3/3; semantic retrieval with citation)
- [x] Phase 22: Control-Plane Fit + Host Gate (4/4 plans) — completed 2026-06-10 (residency proven under real embedding load)
- [x] Phase 23: Surfacing, Backup & Memory-Aware Swap (5/5 plans) — completed 2026-06-10 (status.Report 2→3 single re-freeze; live restore + swap drills)

Audit PASSED — 22/22 requirements, 15/16 integration connections (0 blockers), 5/5 E2E flows. See `milestones/v1.3-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

### 🚧 v1.4 Coding Agent (In Progress)

**Milestone goal:** A villa-orchestrated, strictly-local terminal coding agent as an optional install addon — **Crush v0.76.0** (research-selected over OpenCode on zero-outbound grounds), delivered as a pinned SHA-256-verified host binary with villa-rendered config, talking over loopback to a fit-guarded recommended coding model (Qwen3-Coder family) via a transactional swap-based coding mode. Codebase memory is agent-native (LSP + ripgrep + context files); the original Qdrant code-collection premise was researched and **rejected on evidence** — villa-qdrant/villa-embed stay untouched. Zero new outbound, proven at runtime, negative-control-first.

**Build order is research-converged (mirrors the proven v1.2/v1.3 shape):** fit math + on-hardware model qualification first (schema bumps land once, early; qualification can delete/re-pin catalog entries), render/swap before the agent exists so the endpoint contract is golden-frozen first, agent delivery core next, the install addon + egress/cloud-fallback proofs over the full assembly, and ALL surfacing last — the single `status.Report` 3→4 bump with exactly one golden re-freeze.

- [x] **Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification** - `role:"coder"` catalog entries + recommend coder-fit stage at agent-profile ctx with honest residency-mode output, qualified agent-in-the-loop on the gfx1151 box *(COMPLETE 2026-06-13 — 4/4 plans; catalog FROZEN, D-13 toolbox keep, CODER-01/02/03)*
- [x] **Phase 25: Coding-Mode Render & Transactional Swap Verb** - Tool-calling-ready llama-server unit delta behind the seams + a transactional enter/exit coding-mode verb composing `modelswap` (completed 2026-06-13)
- [ ] **Phase 26: Agent Delivery Core & Lockdown Launcher** - Pinned SHA-256-verified Crush install via villa-owned pin policy, `crush.json` rendered from config.toml with kill switches, `villa code` launcher with env lockdown, drift detection
- [ ] **Phase 27: Install Addon, Preflight Gates & `villa verify agent`** - Optional install addon with sanctioned-window pre-staging + tool-call readiness proof, honest preflight gates, negative-control-first egress + cloud-fallback proofs, uninstall coverage
- [ ] **Phase 28: Agent Surfacing & Contracts** - `status.Report` 3→4 `coding` block (single golden re-freeze), dashboard Agent panel, doctor agent checks, backup coverage, per-model usage + cache-effectiveness signals

## Phase Details

### Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification

**Goal**: `villa recommend` produces an honest, hardware-qualified coding-model recommendation — fit computed at agent-profile context, residency mode (`swap`/`shared`) derived purely from fit math — backed by catalog entries that survived a real agent-in-the-loop tool-call loop on the gfx1151 box.
**Depends on**: Nothing (first phase of v1.4; consumes shipped v1.3 recommend/catalog contracts)
**Requirements**: CODER-01, CODER-02, CODER-03
**Success Criteria** (what must be TRUE):

  1. The catalog ships `role:"coder"` entries (Qwen3-Coder-30B-A3B for all tiers; Qwen3-Coder-Next for 96/128 GB tiers) with revision-pinned GGUF artifacts and template provenance — catalog schema 2→3, append-only; existing chat entries untouched.
  2. `villa recommend` outputs a coder fit computed at agent-profile context, AFTER the embed reservation and chat fit, with an honest residency mode (`swap` or `shared`) that is an output of the fit math — never a preference or tier special-case (recommend schema 2→3, append-only, one golden re-freeze).
  3. Every coder catalog entry that ships has passed a real multi-step agent-in-the-loop tool-call loop through llama-server `--jinja` on the pinned toolbox image, with KV footprint MEASURED at agent ctx on the gfx1151 box — benchmark scores alone never qualify an entry; entries that fail are deleted or re-pinned, never shipped on hope.
  4. The toolbox re-pin decision (Qwen3-Coder-Next arch support + tool-call parser vintage, `--cache-reuse` compatibility per model) is recorded as a decision with evidence before the catalog freezes.

**Plans**: 4 plans
Plans:
**Wave 1**

- [x] 24-01-PLAN.md — Catalog schema 2→3: `role:"coder"` entries with revision-pinned artifacts + template provenance (CODER-01)

**Wave 2** *(blocked on Wave 1 completion)*

- [x] 24-02-PLAN.md — Recommend schema 2→3: coder fit at agent ctx + residency mode, ONE golden re-freeze (CODER-02)
- [x] 24-03-PLAN.md — On-hardware agent-in-the-loop qualification of all three coder entries, measured KV (CODER-03)

**Wave 3** *(blocked on Wave 2 completion)*

- [x] 24-04-PLAN.md — Reconciliation, D-13 toolbox keep-decision record, operator-approved catalog freeze (CODER-01/03)

**Research**: recommended (`/gsd-plan-phase 24 --research-phase` — exact on-hardware qualification protocol, KV measurement, toolbox re-pin decision, `--cache-reuse` vs Qwen3-Coder-Next hybrid attention)

### Phase 25: Coding-Mode Render & Transactional Swap Verb

**Goal**: User can flip the running stack into a tool-calling-ready coding mode and back via an explicit transactional verb — chat model restored on exit, residency proven under load, addon-off renders byte-identical to v1.3.
**Depends on**: Phase 24 (qualified coder catalog entries + residency-mode fit output)
**Requirements**: CMODE-01, CMODE-02
**Success Criteria** (what must be TRUE):

  1. With the addon enabled, coding mode renders a tool-calling-ready llama-server unit delta (`--jinja`, agent ctx, sampling preset, `--cache-reuse` only where catalog-declared model-compatible) behind the inference/orchestrate seams; with the addon off, render output is byte-identical to the v1.3 goldens and the seam grep-gate stays green.
  2. User can enter coding mode via a transactional verb composing `modelswap` (capture → cutover → under-load residency prove → verbatim rollback on any failure) — a silent or partial CPU fallback during the prove step FAILS the swap and rolls back; idle-green is not green.
  3. User can exit coding mode and the chat model is restored under the same transactional discipline; the mode never changes automatically (explicit verb only, ROCm `backend set` precedent).

**Plans**: 2 plans
Plans:

**Wave 1**

- [x] 25-01-PLAN.md — CMODE-01 render delta: optional `RunSpec.CodingMode` descriptor + `ContainerArgs` `--jinja`/agent-ctx/sampling/`--cache-reuse` append behind the seam, append-only config fields, new `villa-llama-coding.container.golden` (off-path byte-identical, seam-gate extended)

**Wave 2** *(blocked on Wave 1 completion)*

- [~] 25-02-PLAN.md — CMODE-02 transactional verb: new pure `internal/codingmode` core cloning `backendswap` + composing `modelswap`, `villa coding-mode enter|exit` + `liveCodingModeDeps`, under-load residency prove (idle-green = rollback), symmetric exit — **Tasks 1-2 done & `make check` green; Task 3 on-hardware acceptance OPEN**

**Research**: not needed (composes shipped modelswap + Phase-7/D-09 render-delta patterns)

### Phase 26: Agent Delivery Core & Lockdown Launcher

**Goal**: villa installs a pinned, checksum-verified Crush binary and renders its config as a derived artifact of `config.toml` — kill switches set, loopback-only provider, launched through a locked-down `villa code` verb, with drift detected and never auto-corrected.
**Depends on**: Phase 24 (villa-unique coder model ids); pure core parallel-capable with Phase 25
**Requirements**: AGENT-01, AGENT-02, AGENT-03, AGENT-04
**Success Criteria** (what must be TRUE):

  1. villa installs the pinned Crush release (version + per-platform asset + SHA-256 from a villa-owned `go:embed` pin policy, rocm-policy pattern); the checksum is verified BEFORE install — a mismatch refuses with remediation — and autoupdate is forced off.
  2. `crush.json` is rendered as a derived artifact of `config.toml`: both kill switches set (`disable_metrics`, `disable_provider_auto_update`), exactly one villa provider block pointing at the loopback endpoint, villa-unique model ids (shadowing workaround), and LSP entries for detected toolchains — a missing server like `gopls` produces a WARN with remediation, never a BLOCK.
  3. User can launch the agent via `villa code`, which applies the belt-and-braces env lockdown (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`) before exec.
  4. Drift of the agent binary or rendered config from the pin policy is detected and surfaced with remediation — never silently auto-corrected.

**Plans**: 3 plans
Plans:

**Wave 1**

- [x] 26-01-PLAN.md — Pure `internal/agent` core: `go:embed` pin policy + checksum gate, deterministic `crush.json` renderer (kill switches, one loopback provider, villa- model id, LSP WARN-on-absence), version comparator, drift detector + golden (AGENT-01/02/04) — COMPLETE (3/3 TDD tasks; 22 tests + golden; TestSeamGrepGate + make check green)

**Wave 2** *(blocked on Wave 1 completion)*

- [ ] 26-02-PLAN.md — `villa code` launcher + `liveAgentDeps` + checksum-before-extract install seam + LSP PATH probe + root.go registration; env lockdown, no-auto-flip honored, drift surfaced at launch (AGENT-01/03/04)

**Wave 3** *(blocked on Wave 2; on-hardware checkpoint)*

- [ ] 26-03-PLAN.md — On-hardware: pin the extracted-binary SHA-256 (Pitfall 6 / Q2), flip binary-drift to a confident signal, and a real `villa code` launch acceptance on the gfx1151 box (AGENT-01/04)

**Research**: COMPLETE (26-RESEARCH.md — crush.json v0.76.0 schema frozen; #2649 villa- prefix resolved; pin artifacts verified). Open questions locked in planning: Q1 → render-only (option ii, llama.cpp single-model leniency); Q2 → policy carries `binarySha256`, pinned on-hardware in 26-03; Q3 → minimal/omitted `permissions` (full STRIDE allowlist is Phase 27).

### Phase 27: Install Addon, Preflight Gates & `villa verify agent`

**Goal**: The coding agent is an optional `villa install` addon that comes up ready (real tool-call round-trip), gated honestly by preflight, removable by uninstall, and PROVEN strictly local at runtime — egress and cloud-fallback negative controls, never flag-trusted.
**Depends on**: Phases 24, 25, 26 (full assembly required for the proofs)
**Requirements**: INSTALL-03, INSTALL-04, PRIV-06
**Success Criteria** (what must be TRUE):

  1. User can enable the coding agent as an optional `villa install` addon (mirroring the memory addon): gate → pre-stage the coder GGUF + agent binary inside the sanctioned outbound window → render → readiness proof that includes a REAL tool-call round-trip — a health-200 alone never passes readiness.
  2. Preflight gates the addon honestly: disk BLOCK, post-coder envelope BLOCK, cloud-credential WARN — refuse-with-remediation on confident known-bad, typed-Unknown degrades to WARN.
  3. `villa verify agent` proves zero outbound at runtime, negative-control-FIRST, covering agent **startup**: the egress-open control run must FAIL the gate (proving it is real), and the egress-blocked run must complete a real agent task.
  4. A llama-down negative control proves no silent cloud-model fallback: with `villa-llama` stopped, the agent must NOT keep working — an agent answering with inference down is the smoking gun and FAILS the verification.
  5. `villa uninstall` removes the agent binary, rendered config, and addon artifacts.

**Plans**: TBD
**Research**: recommended (`/gsd-plan-phase 27 --research-phase` — Crush's complete outbound surface under negative control + FSL-1.1-MIT consent text)

### Phase 28: Agent Surfacing & Contracts

**Goal**: The operator can see, diagnose, back up, and measure the coding agent — `status.Report` 3→4 lands once at the end (the proven v1.2/v1.3 single-bump discipline), dashboard Agent panel, doctor agent checks, and honest usage/cache-effectiveness signals.
**Depends on**: Phase 27 (surfaces only a finished feature set)
**Requirements**: SURF-01, SURF-02, SURF-03, USAGE-03, USAGE-04
**Success Criteria** (what must be TRUE):

  1. `villa status` (human + `--json`) reports an append-only `coding` block (enabled, agent version + pin match, model, mode, residency) — `status.Report` 3→4 in exactly ONE golden re-freeze (coding-on/off variants); the dashboard renders a matching Agent panel, hidden entirely until data exists.
  2. `villa doctor` folds agent checks: binary/version drift, config drift, a real tool-call round-trip probe, and under-load residency — an offload/residency FAIL dominates a healthy-looking HTTP probe, never a false-green.
  3. `villa backup`/`restore` cover the rendered agent config; the agent binary is identity-recorded and excluded from the archive, exactly like model weights.
  4. Agent token usage is attributed per-model in status and the dashboard via the v1.2 usage core (the coder is a distinct served model).
  5. Cache effectiveness (`timings.cache_n` vs `prompt_n`) is surfaced as an honest agent-speed signal.

**Plans**: TBD
**UI hint**: yes
**Research**: not needed (v1.2 P15 / v1.3 P23 surfacing discipline applies verbatim)

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
| 12. `rocm-6.4.4` Alternate Backend | v1.2 | 3/3 | Complete | 2026-06-07 |
| 13. `villa doctor` Health Diagnosis | v1.2 | 3/3 | Complete | 2026-06-07 |
| 14. Saved Bench Reports + `--compare` | v1.2 | 3/3 | Complete | 2026-06-07 |
| 15. Cumulative Usage Tracking | v1.2 | 4/4 | Complete | 2026-06-07 |
| 16. Backup / Restore | v1.2 | 3/3 | Complete | 2026-06-07 |
| 17. Guided TUI Install | v1.2 | 3/3 | Complete | 2026-06-08 |
| 18. Memory Spine — config core + research spike | v1.3 | 2/2 | Complete | 2026-06-09 |
| 19. Vector Store + Local Embeddings Services | v1.3 | 3/3 | Complete | 2026-06-09 |
| 20. Open WebUI Memory/RAG Wiring + Offline Lockdown | v1.3 | 3/3 | Complete | 2026-06-10 |
| 21. Conversational Recall Indexer | v1.3 | 3/3 | Complete | 2026-06-10 |
| 22. Control-Plane Fit + Host Gate | v1.3 | 4/4 | Complete | 2026-06-10 |
| 23. Surfacing, Backup & Memory-Aware Swap | v1.3 | 5/5 | Complete | 2026-06-10 |
| 24. Coder Fit Math, Catalog & On-Hardware Model Qualification | v1.4 | 4/4 | Complete    | 2026-06-13 |
| 25. Coding-Mode Render & Transactional Swap Verb | v1.4 | 2/2 | Complete    | 2026-06-13 |
| 26. Agent Delivery Core & Lockdown Launcher | v1.4 | 0/3 | Planned | - |
| 27. Install Addon, Preflight Gates & `villa verify agent` | v1.4 | 0/TBD | Not started | - |
| 28. Agent Surfacing & Contracts | v1.4 | 0/TBD | Not started | - |
