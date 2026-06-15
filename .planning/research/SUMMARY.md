# Project Research Summary

**Project:** VillaStraylight — v1.4 Coding Agent milestone
**Domain:** Strictly-local coding-agent integration (agent + coder model + codebase memory) on the shipped llama.cpp/Vulkan + Open WebUI + Qdrant control plane (AMD Strix Halo / Fedora)
**Researched:** 2026-06-12
**Confidence:** HIGH on agent landscape, integration seams, and pitfall inventory; MEDIUM on KV-cache math and third-party throughput numbers (computed/community — verify on hardware in phase)

## Executive Summary

v1.4 adds a terminal coding agent to the stack as a **villa-managed host binary** (pinned release, SHA-256-verified, config rendered from `config.toml` — never a container, never a service), talking over loopback to the existing OpenAI-compatible llama-server endpoint. The agent of record is **Crush (charmbracelet) v0.76.0**, not OpenCode: research verified that OpenCode cannot be config-locked to villa's zero-outbound posture (unconditional `models.dev` fetch at startup, runtime bun/npm package downloads, autoupdate default-ON, air-gapped support closed upstream as "not planned"), while Crush's only two outbound channels both have documented config kill switches villa can render. The coding model is the **Qwen3-Coder family** (30B-A3B everywhere; Qwen3-Coder-Next on big envelopes), served via a **transactional swap-based "coding mode"** that reuses `internal/modelswap`, with the zero-cost install default being the agent simply riding the existing chat endpoint.

Two of the milestone's original premises are **overturned by evidence**. First, the agent choice flips from OpenCode to Crush on outbound-surface grounds (reconciled below — three of four files weighed in). Second, and decisive for scope: **the "use Qdrant + villa-embed to track the codebase" premise fails the effectiveness test the milestone itself demanded.** All four research files independently converge on the same verdict — 2025–2026's leading agents (Claude Code, OpenCode, Crush, Codex CLI) deliberately ship *without* vector indexes because agentic search (grep/glob + LSP) beats embedding-RAG over code (chunking breaks code semantics, indexes go stale the moment the agent edits a file, and nomic-embed-text is a text model, not a code model). The user asked for an alternative if Qdrant proved ineffective; the alternative is **agent-native retrieval (Crush's built-in LSP + ripgrep tools + `CRUSH.md`/`AGENTS.md` context files)** — which costs villa zero new services and is what the recommended agent does natively. villa-qdrant and villa-embed stay untouched by v1.4.

The key risks are honesty risks, exactly the class this codebase is built to forbid: agent phone-home at *startup* (extend the v1.3 negative-control-first nft egress proof to the agent, including startup), silent cloud-model fallback (llama-down negative control), tool-call/jinja template landmines (model selection must be agent-in-the-loop, GGUF artifacts pinned at revision level), and agent-scale KV cache (~6–12 GiB at 64–128k ctx) blowing fit math anchored on chat contexts. All are preventable with established v1.2/v1.3 patterns; the roadmap below maps each to a phase.

## Reconciled Disagreements (read this before the roadmap)

The four files were researched in parallel and disagree on three load-bearing decisions. Resolution, with the evidence weighed:

### 1. Agent selection: Crush, not OpenCode

- **STACK** (which ran the head-to-head selection): recommends **Crush v0.76.0**. OpenCode unconditionally fetches `models.dev/api.json` at startup, downloads provider/plugin npm packages at runtime via embedded bun, auto-downloads LSP servers, autoupdates by default, and upstream **closed air-gapped support as "not planned"** (anomalyco/opencode #2224; #16117 closed as duplicate). Crush is a single static Go binary whose only two outbound channels (PostHog metrics, Catwalk provider-DB refresh) have documented kill switches (`disable_metrics`, `disable_provider_auto_update`) plus an embedded provider DB for offline.
- **FEATURES** and **ARCHITECTURE**: written assuming OpenCode as the leading candidate. ARCHITECTURE itself flagged the air-gap issues as "the single biggest honesty risk of the milestone" but treated them as mitigable-with-proof; STACK's later finding (closure as not-planned, runtime npm fetches proven) shows the mitigation surface is structurally incomplete — `villa verify agent` would fail OpenCode out of the box.
- **PITFALLS**: documents phone-home defaults for all three candidates (OpenCode worst: unconditional registry fetch + cloud-default model `gpt-5-nano` via Zen + allow-all permissions; Crush: a single metrics channel with three documented opt-outs; Aider: random-subset analytics — worse for a *provable* posture).

**Verdict: Crush.** The combined evidence is one-directional: villa's posture requires config-killable outbound, and only Crush has it. Crucially, **most of FEATURES'/ARCHITECTURE's integration design transfers unchanged** — host-binary delivery, villa-owned pin policy (`go:embed` policy JSON, rocm-policy pattern), rendered agent config as a derived artifact of `config.toml`, one villa-owned provider block, `--jinja` server preset, LSP-as-code-graph, fit math, surfacing discipline — all of it is agent-shape-generic. What does NOT transfer: the OpenCode-specific `opencode-codebase-index` plugin (moot anyway, see #3) and OpenCode config key names. Crush caveats to carry into phase research: FSL-1.1-MIT license (fine for end-user download; document in install consent text), the model-id shadowing issue (#2649 — use villa-unique model ids), and Crush's permission-config surface (PITFALLS' workspace-escape analysis was OpenCode-specific; re-verify Crush's defaults). The cloud-fallback and egress negative controls apply to Crush identically — villa proves, never trusts flags.

### 2. Coding-model residency: swap-based coding mode wins; co-residency is a fit-gated 128 GB stretch

- **STACK + PITFALLS**: swap-based coding mode (reusing `internal/modelswap`) is the universal mechanism across all tiers; co-residency is honest only on 96/128 GB and should be a stretch. PITFALLS: "on a 64 GiB-class envelope, swap-based coding mode is almost certainly the honest recommendation; co-residency is a ≥96–128 GiB outcome" — and co-residency "because it loaded once" is its worst-UX failure mode (mid-session OOM days later, or silent CPU spill).
- **ARCHITECTURE**: argues a co-resident `villa-coder` Quadlet unit with honest degradation to shared mode (agent → existing chat endpoint), explicitly rejecting swap because it "breaks chat while coding, adds a third transactional state machine, and reintroduces the D-09/D-10 hazard class."

**Verdict: swap-based coding mode is the v1.4 core mechanism; co-resident `villa-coder` is deferred to a fit-gated stretch (realistically 128 GB-tier-only).** The weighing:

- **Agent-scale KV math kills co-residency on most tiers.** Agent contexts are 64–128k, not chat-scale: ~6 GiB KV at 64k f16 (~3 GiB q8_0), ~12 GiB at 128k for the 30B-A3B. Against the **62.5 GiB measured GTT envelope** with the chat claimant (~27 GiB incl. embed + headroom) resident, co-residency of the 30B coder fits only on the 128 GB tier; the 96 GB tier is marginal-at-reduced-ctx; 64 GB is a hard no. And the **quality pick — Qwen3-Coder-Next at 49.6 GB — can never co-reside on any tier**: co-residency permanently locks users out of the best model; swap serves it everywhere it fits.
- **ARCHITECTURE's hazard argument is stale.** v1.3's D-09 made chat-swap isolation from memory units *structural and reflect-pin-tested* — swap does not reintroduce that hazard class, it rides the guard that closed it. Swap composes a proven transactional core (capture → prove residency → cutover → rollback); a co-resident unit adds a second inference unit, second under-load residency proof, and second `/metrics` surface for marginal benefit.
- **What survives from ARCHITECTURE** (these are real contributions, keep them): (a) **shared mode as the zero-cost install default** — the agent points at the existing chat endpoint; Qwen3.6-35B-A3B is a competent tool-caller; STACK independently reached the same baseline; (b) **residency mode must be an output of `recommend` fit math computed at the agent-profile ctx** (PITFALLS demands the same), never a preference or tier special-case; (c) chat stays the primary claimant; (d) the `villa-coder` unit design (Backend-seam reuse, loopback publish, role-parameterized spec) is shelf-ready if the stretch ships.
- Mitigation for swap's one real cost (chat serves the coding model while coding mode is on): explicit verb, never automatic (ROCm precedent), surfaced in status; Qwen3-Coder chats acceptably.

### 3. Codebase memory: Qdrant code collection is dead; agent-native retrieval is the default

All four files converge **against** a Qdrant code collection — this is the strongest cross-file agreement in the research, and it **overturns the original milestone premise**:

- **STACK**: zero new services — Crush LSP + agentic grep/glob + `CRUSH.md`/`AGENTS.md`; the "dedicated code collection" default in PROJECT.md "would be write-only plumbing no recommended agent reads."
- **FEATURES**: leading agents explicitly rejected code-RAG (Anthropic tested it — agentic search won "not narrowly"; a Feb 2026 Amazon Science result shows agentic keyword search reaches >90% of RAG performance with no vector DB); demote to optional MCP differentiator at most.
- **PITFALLS**: three compounding failures verified — nomic-embed-text-v1.5 is a *text* model (Nomic ships separate code embedders for a reason), naive text chunking destroys code structure, and a vector index is stale the moment the agent writes a file (retrieval then actively lies). Demands any code-RAG layer **win a pre-declared, numerically-scored eval against the grep/LSP baseline before it ships** — it must win to ship, not lose to be removed.
- **ARCHITECTURE**: also rejects Qdrant-as-code-memory, but proposes an optional `villa-embed`-backed index *plugin* as a differentiator. That plugin (`opencode-codebase-index`) is **OpenCode-specific and moot under Crush**. The surviving shape of the idea is a future Qdrant/embed-backed **MCP server** plugged into Crush's MCP support — explicitly deferred (v1.5+, behind PITFALLS' numeric eval gate).

**Verdict: default = agent-native (Crush LSP + ripgrep/glob + context files); villa-qdrant and villa-embed are untouched by v1.4** (no conditional embed publish, no T-19-01 relaxation — ARCHITECTURE's posture change is no longer needed). villa's only jobs: render `lsp` entries for detected toolchains (WARN, never BLOCK, when servers like `gopls` are missing) and document the `AGENTS.md`/`CRUSH.md` convention. Requirements must state plainly that the Qdrant premise was researched and rejected, per the user's explicit ask for an alternative.

## Key Findings

### Recommended Stack

Full detail: `.planning/research/STACK.md`. No new Go module dependencies; villa stays a single static binary; the agent is also a single static Go binary.

**Core technologies:**
- **Crush v0.76.0** (charmbracelet): the terminal coding agent — pinned release artifact + checksum, installed under `$XDG_DATA_HOME/villa/bin/`; villa renders `crush.json` (global config) from `config.toml` with both kill switches set, plus belt-and-braces env (`CRUSH_DISABLE_METRICS=1`, `DO_NOT_TRACK=1`) in a `villa code` launcher.
- **Qwen3-Coder-30B-A3B-Instruct** (unsloth GGUF, UD-Q4_K_XL, 17.7 GB): fit-everywhere default coder — MoE A3B speed on the bandwidth-bound iGPU (~70–98 tok/s tg), 256K native ctx, agentic-RL-trained, fixed tool-call templates.
- **Qwen3-Coder-Next** (80B-A3B hybrid MoE; UD-Q4_K_XL 49.6 GB / UD-Q3_K_XL 36.3 GB): quality pick for 128/96 GB tiers — near-flagship agentic coding at A3B speed, small KV (hybrid attention), swap-only.
- **llama-server `--jinja` render delta** in coding mode (+ agent ctx ≥64k, `--cache-reuse`, agent sampling preset) — behind the inference/orchestrate seams, Phase-7-style unit-delta pattern.
- **Delivery: host binary, not container** — a containerized agent breaks LSP/toolchains/git and there's no official image; locality is enforced by rendered config + runtime egress proof, not network-namespace theater.

### Expected Features

Full detail: `.planning/research/FEATURES.md` (OpenCode-flavored; table stakes transfer to Crush).

**Must have (table stakes):**
- Agent addon in `villa install` (pinned binary + villa-owned provider config + uninstall coverage) — the headline deliverable
- Tool-calling-ready inference preset (`--jinja`, template-verified GGUF, agent ctx ≥64k, `--cache-reuse` where compatible)
- Fit-guarded recommended coding model, pre-staged in the sanctioned outbound window, honest residency-mode outcome
- Transactional coding-mode swap verb (the residency decision landed on swap — promotes this from FEATURES' conditional P2 to core)
- Status/doctor/dashboard coverage including a **tool-call round-trip probe** (health-200 is not tool-calling success)
- Strictly-local verification: `villa verify agent` negative-control-first egress proof covering agent **startup**

**Should have (differentiators):**
- Hardware-aware coder recommend (fit math decides swap vs co-resident vs shared, per catalog entry) — villa's signature move applied to agents
- Cache-effectiveness surfacing (`timings.cache_n` vs `prompt_n`) — measurable agent speed honesty signal
- Per-model agent usage attribution (near-free via the v1.2 usage core once the coder is a distinct served model)

**Defer (v1.5+):**
- Co-resident `villa-coder` unit (128 GB fit-gated stretch)
- MCP semantic code search backed by Qdrant/villa-embed — only behind a pre-declared numeric eval it must *win*
- Sandboxed/containerized agent profile for untrusted repos

**Anti-features (explicitly rejected):** default Qdrant code collection; villa wrapping the agent's UI or running it as a service; auto-switching models when the agent connects; villa writing `AGENTS.md` into user repos; owning the user's whole agent config (one provider block only); a gateway/proxy layer between agent and llama-server.

### Architecture Approach

Full detail: `.planning/research/ARCHITECTURE.md` (co-residency section superseded by reconciliation #2; integration-seam mapping remains the authoritative modified-vs-new inventory).

The addon bolts onto shipped seams with zero new impure modules: a new pure core (`internal/agent` or `internal/coder`) owns the pin policy (`go:embed` policy JSON — rocm-policy pattern: version, per-platform asset, sha256), the `crush.json` renderer (config-as-source-of-truth, regenerated never hand-edited, doctor flags drift), and version comparison; host effects (download via `internal/download`, unzip, exec) are injected Deps. Coding mode composes `internal/modelswap`; the render delta (`--jinja`/ctx/cache flags) lives in `Backend.ContainerArgs` behind the seam grep-gate.

**Major components:**
1. `internal/agent` (NEW pure core) — pin policy, crush.json render, lockdown env, version drift
2. `cmd/villa/code.go` + `install_agent.go` (NEW) — launcher verb + addon install mirroring `install_memory.go`
3. catalog/recommend (MODIFIED, schema 2→3 each, append-only) — `role:"coder"` entries; coder fit stage at agent-profile ctx after embed reservation + chat fit
4. inference/orchestrate (MODIFIED) — coding-mode flag rendering behind existing seams; addon-off renders byte-identical to v1.3 goldens
5. status/doctor/dashboard/backup/verify/uninstall (MODIFIED, final phase) — `status.Report` 3→4 append-only, single golden re-freeze; doctor tool-call + under-load residency probes; `villa verify agent`

### Critical Pitfalls

Full detail: `.planning/research/PITFALLS.md`. The milestone-shaping five:

1. **Agent phone-home at startup** — extend the v1.3 negative-control-first nft egress proof to the agent **including startup** (registry/update fetches fire at startup, not first prompt); egress-open run must FAIL the gate, blocked run must complete a real edit-loop task. Never flag-trust.
2. **Cloud-model fallback** — exactly one provider in rendered config; preflight WARN on cloud credentials in the agent auth store; **llama-down negative control** (agent working while `villa-llama` is down is the smoking gun).
3. **Tool-call/jinja template landmines** — model selection must be **agent-in-the-loop** (real multi-step tool loop against the actual quant on the actual pinned image; benchmark scores don't qualify a model); **pin GGUF artifacts at repo+revision level** (the embedded chat template is part of the artifact — two quants of "the same model" differ in agentic usability); tool-call smoke proof as an install readiness gate.
4. **Agent-scale KV OOM / silent CPU fallback** — fit math at the *agent's rendered ctx* (not chat ctx); agent config ctx and server `--ctx-size` rendered from the same config value; under-load residency proof (MEM-DOC pattern) — idle-green is not green; KV-quantization only as a catalog-declared, benched choice (aggressive K-cache quant corrupts tool-call JSON).
5. **Version churn + contract freeze** — Crush/OpenCode release weekly-to-daily; villa-owned pin policy with explicit upgrade verb, autoupdate forced off, doctor drift check; ALL surfacing lands in the final phase as the single `status.Report` 3→4 bump, one golden re-freeze, addon-off renders byte-identical.

**Cross-cutting flags for phase research:** `--cache-reuse` is **incompatible with recurrent/hybrid-state models — including the current chat model (Qwen3.6-35B-A3B)**, which strengthens the dedicated-coding-model case; verify whether it also affects Qwen3-Coder-Next (also hybrid-attention). And the pinned `vulkan-radv` toolbox digest may need a **re-pin** if its llama.cpp predates Qwen3-Next arch support (PR #16095) + the Feb-2026 tool-call parser fixes.

## Implications for Roadmap

Suggested structure (continues the proven v1.2/v1.3 shape: pure fit/control-plane gates first, integration middle, surfacing last):

### Phase 1: Coding fit math + catalog (+ on-hardware model qualification)
**Rationale:** Everything downstream consumes the fit verdict and the qualified model artifacts; schema bumps land once, early. The agent-in-the-loop tool-call qualification and the toolbox-digest arch check can delete or re-pin catalog entries — must precede render work.
**Delivers:** catalog schema 2→3 (`role:"coder"`, Qwen3-Coder entries with revision-pinned GGUF SHAs, template provenance, agent-ctx/sampling metadata); `recommend` coder fit stage at agent-profile ctx (schema 2→3, append-only, one golden re-freeze); residency-mode output (`swap`/`shared`, co-resident reserved); on-hardware verification: tool-call round-trip through llama-server `--jinja` on the pinned image, Qwen3-Next arch support check (re-pin decision), measured KV footprints.
**Addresses:** fit-guarded coding model (FEATURES P1).
**Avoids:** Pitfalls 3 (template landmines), 4 (KV OOM), 6 (the eval verdict is recorded as a decision: Qdrant code-RAG rejected).

### Phase 2: Coding-mode render + transactional swap verb
**Rationale:** Pure render + a composition of the proven modelswap core; testable off-hardware; gives the agent something correct to talk to before the agent exists.
**Delivers:** coding-mode unit delta (`--jinja`, agent ctx, `--cache-reuse` where model-compatible, sampling) behind inference/orchestrate seams; `villa code on|off`-style transactional swap (capture → prove residency under load → cutover → rollback; D-09 reflect-pin guard extended); seam grep-gate + goldens extended; addon-off renders byte-identical.
**Uses:** `internal/modelswap`, `Backend.ContainerArgs`, Quadlet render goldens.
**Avoids:** Pitfall 5 (silent CPU fallback — under-load residency in the swap's prove step), Pitfall 9 (no contract changes yet).

### Phase 3: Agent delivery core + lockdown launcher
**Rationale:** The pure agent core (pin policy, crush.json render, lockdown) is fully testable off-hardware and independent of phases 1–2; it gates the install addon.
**Delivers:** `internal/agent` pure core (`go:embed` crush-policy.json: v0.76.0 + asset names + SHA-256s; crush.json renderer with `disable_metrics`/`disable_provider_auto_update`, single villa provider, villa-unique model ids, lsp block; version comparator); `villa code` launcher (env lockdown + exec); download via `internal/download`.
**Uses:** rocm-policy and download patterns verbatim.
**Avoids:** Pitfall 8 (version churn — villa owns the pin), Pitfall 1 (kill-switch config frozen here).

### Phase 4: Install addon + preflight + `villa verify agent`
**Rationale:** Wires 1–3 into the user-facing flow; the egress and cloud-fallback proofs need the full assembly.
**Delivers:** `install_agent.go` mirroring `install_memory.go` (gate → pre-stage GGUF + agent tarball in the sanctioned outbound window → render → readiness proof with a real tool-call probe); preflight gates (disk BLOCK, post-coder envelope BLOCK, gopls/LSP WARN, cloud-credential WARN); `villa verify agent` negative-control-first nft egress proof **covering agent startup** + llama-down negative control; uninstall coverage.
**Avoids:** Pitfalls 1, 2, 7 (Crush permission config rendered restrictively; STRIDE pass on the injection→tool-call path).

### Phase 5: Surfacing + contracts (LAST)
**Rationale:** The single byte-frozen contract evolution lands once, at the end — the discipline v1.2 (P15) and v1.3 (P23) proved.
**Delivers:** `status.Report` 3→4 append-only (`coding` block: enabled, agent version + pin-match, model, mode, residency), one golden re-freeze with coding-on/off variants; doctor agent checks (binary/version drift, config drift, tool-call probe, under-load residency); dashboard Agent panel (hidden-until-data); backup manifest (rendered crush.json included; agent binary identity-recorded/excluded like weights); per-model usage attribution + optional `cache_n` surfacing.
**Avoids:** Pitfall 9 (single bump, service-name drift test debt closed rather than extended).

### Phase Ordering Rationale

- Fit math first because residency mode, catalog entries, and model qualification gate every later phase (PITFALLS' explicit "control-plane phase before the install addon", mirroring v1.3's fit-before-surfacing ordering).
- Render/swap before agent delivery so the endpoint contract (`--jinja`, ctx, alias) exists and is golden-frozen before anything consumes it.
- Verification phases sit with the assemblies they prove (swap proves residency in 2; egress/cloud proofs need the full install in 4).
- Surfacing last by construction — the only way the single-schema-bump discipline holds.

### Research Flags

Phases likely needing deeper research during planning (`/gsd-plan-phase --research-phase N`):
- **Phase 1:** exact on-hardware qualification protocol (agent-in-the-loop tool loop, KV measurement, toolbox re-pin decision, `--cache-reuse` compatibility with Qwen3-Coder-Next's hybrid attention).
- **Phase 3:** freeze the exact `crush.json` schema at the pinned version (options/models/lsp keys, model-id shadowing workaround for #2649, permission-config surface) — STACK's sketch is MEDIUM-confidence.
- **Phase 4:** Crush's complete outbound surface under negative control (kill switches are documented; villa proves, never trusts) + FSL license consent text.

Phases with standard patterns (skip research-phase):
- **Phase 2:** composes shipped modelswap + render-delta patterns (Phase-7/D-09 precedents).
- **Phase 5:** the v1.2/v1.3 surfacing discipline applies verbatim.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Agent versions/licenses/outbound behavior verified against releases, issues, official docs on 2026-06-12; GGUF sizes from HF listings. MEDIUM on throughput numbers and computed KV estimates. |
| Features | MEDIUM-HIGH | Official agent docs + multi-source agentic-search consensus; written OpenCode-flavored — table stakes transfer to Crush, key names don't. |
| Architecture | HIGH on seams (every claim cites a verified file); MEDIUM on its residency recommendation, which this synthesis overrides on KV math + transferred-hazard grounds. |
| Pitfalls | HIGH | Telemetry/template/churn behaviors verified upstream; KV math computed (MEDIUM); code-RAG verdict is industry consensus (MEDIUM) but four-way convergent here. |

**Overall confidence:** HIGH for roadmap structure; MEDIUM on the on-hardware specifics deliberately deferred to Phase 1.

### Gaps to Address

- **Exact `crush.json` schema + Crush permission model** at v0.76.0 — freeze in Phase 3 research; STACK's sketch and PITFALLS' permission analysis (OpenCode-based) both need Crush-specific verification.
- **Crush's complete outbound surface** — only provable under the Phase 4 negative-control nft proof; kill switches are documented but unproven on this host.
- **Qwen3-Coder-Next on the pinned toolbox digest** — hybrid (DeltaNet) arch support + tool-call parser vintage; re-pin decision in Phase 1. Also whether `--cache-reuse` works with its hybrid attention.
- **Measured KV/footprint at agent ctx** — all KV numbers are computed estimates; Phase 1 measures on the gfx1151 box before catalog entries freeze.
- **Crush model-id shadowing (#2649)** — single report; verify and pick villa-unique ids in Phase 3.

## Sources

### Primary (HIGH confidence)
- charmbracelet/crush README + releases (v0.76.0, 2026-06-05) — kill switches, openai-compat provider, LSP config, FSL-1.1-MIT
- anomalyco/opencode #2224 / #16117 — air-gapped support closed as not-planned; runtime npm fetch proven; releases page (v1.17.4, cadence)
- opencode.ai docs (config/providers/LSP/tools) — config surface, autoupdate/share defaults
- llama.cpp function-calling docs + server README — `--jinja` requirement, `--parallel` ctx split, cache types; llama.cpp #20198 (arguments-as-object bug)
- unsloth Qwen3-Coder-30B-A3B / Qwen3-Coder-Next GGUF repos + run guides — sizes, ctx, template fixes
- Codebase (verified 2026-06-12): `internal/recommend/recommend.go` (reservation pattern), `internal/inference/backend_vulkan.go` (loopback seam), `internal/orchestrate/memory_test.go` (T-19-01), `internal/catalog/seed.json`, `cmd/villa/install_memory.go` (addon pattern)

### Secondary (MEDIUM confidence)
- Cline "Why we don't index your codebase", Claude Code no-indexing analyses, Augment SWE-bench grep-vs-embeddings post-mortem, Amazon Science agentic-search result — code-RAG rejection consensus (with Milvus counterpoint weighed)
- kyuz0 + community Strix Halo benchmarks — Qwen3-Coder throughput figures
- llama.cpp discussions #13606/#22354/#20574 — `--cache-reuse` semantics, hybrid-model incompatibility
- ROCm Strix Halo system-optimization docs + llama.cpp #18159/#22372 — GTT/TTM envelope behavior

### Tertiary (LOW confidence, validate in phase)
- charmbracelet/crush #2649 — model-id shadowing (single report)
- LM Studio post on llama.cpp Qwen3-Next support timeline (PR #16095)

---
*Research completed: 2026-06-12*
*Ready for roadmap: yes*
