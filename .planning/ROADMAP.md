# Roadmap: VillaStraylight

## Overview

VillaStraylight is a control plane + managed workload: a single Go binary that inspects an AMD Strix Halo (gfx1151) Fedora host, recommends a hardware-fitting model/quant/context, generates rootless Podman Quadlet units, and brings up llama.cpp inference + Open WebUI chat + a read-only control dashboard — with zero data leaving the box. The north star throughout is "runs healthy after install" — every phase ends in something runnable and verifiable on the real hardware. v1.2 (Operability) extended that bar to "and stays operable, recoverable, and measurable over time." v1.3 (Memory & Knowledge) extended it to "and remembers the user and their documents across chats — strictly local." v1.4 (Coding Agent) extended it to "and gives the operator a strictly-local terminal coding agent, wired to a fit-guarded coding model." v1.5 (Web Search, Grounded & Guarded) extends it again to "and can ground answers in live web search when the operator opts in — accurate, up-to-date, with malicious-site prompt injection defended-in-depth and outbound provably bounded."

## Milestones

- ✅ **v1.0 MVP** — Phases 1–5 (shipped 2026-06-05, tag `v1.0`)
- ✅ **v1.1 ROCm Opt-In Backend** — Phases 6–11 (shipped 2026-06-06, tag `v1.1`)
- ✅ **v1.2 Operability** — Phases 12–17 (shipped 2026-06-08, tag `v1.2`)
- ✅ **v1.3 Memory & Knowledge (local RAG)** — Phases 18–23 (shipped 2026-06-11, tag `v1.3`)
- ✅ **v1.4 Coding Agent** — Phases 24–28 (shipped 2026-06-15, tag `v1.4`)
- 🔨 **v1.5 Web Search (Grounded & Guarded)** — Phases 29–34 (in progress, started 2026-06-18)

Full per-phase detail for shipped milestones is archived under `.planning/milestones/`:

- `milestones/v1.0-ROADMAP.md` · `milestones/v1.0-REQUIREMENTS.md`
- `milestones/v1.1-ROADMAP.md` · `milestones/v1.1-REQUIREMENTS.md` · `milestones/v1.1-MILESTONE-AUDIT.md`
- `milestones/v1.2-ROADMAP.md` · `milestones/v1.2-REQUIREMENTS.md` · `milestones/v1.2-MILESTONE-AUDIT.md`
- `milestones/v1.3-ROADMAP.md` · `milestones/v1.3-REQUIREMENTS.md` · `milestones/v1.3-MILESTONE-AUDIT.md`
- `milestones/v1.4-ROADMAP.md` · `milestones/v1.4-REQUIREMENTS.md` · `milestones/v1.4-MILESTONE-AUDIT.md`

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

<details>
<summary>✅ v1.4 Coding Agent (Phases 24–28) — SHIPPED 2026-06-15</summary>

**Milestone goal:** A villa-orchestrated, strictly-local terminal coding agent as an optional install addon — **Crush v0.76.0** (research-selected over OpenCode on zero-outbound grounds), delivered as a pinned SHA-256-verified host binary with villa-rendered config, talking over loopback to a fit-guarded recommended coding model (Qwen3-Coder family) via a transactional swap-based coding mode. Codebase memory is agent-native (LSP + ripgrep + context files); the original Qdrant code-collection premise was researched and **rejected on evidence** — villa-qdrant/villa-embed stay untouched. Zero new outbound, proven at runtime, negative-control-first.

**Build order was research-converged (mirrors the proven v1.2/v1.3 shape):** fit math + on-hardware model qualification first (schema bumps land once, early), render/swap before the agent exists so the endpoint contract is golden-frozen first, agent delivery core next, the install addon + egress/cloud-fallback proofs over the full assembly, and ALL surfacing last — the single `status.Report` 3→4 bump with exactly one golden re-freeze.

- [x] Phase 24: Coder Fit Math, Catalog & On-Hardware Model Qualification (4/4 plans) — completed 2026-06-13 (catalog FROZEN, D-13 toolbox keep; CODER-01/02/03)
- [x] Phase 25: Coding-Mode Render & Transactional Swap Verb (2/2 plans) — completed 2026-06-13 (25-VERIFICATION 9/9; CMODE-01/02)
- [x] Phase 26: Agent Delivery Core & Lockdown Launcher (3/3 plans) — completed 2026-06-13 (binary SHA pinned on-hardware, `crush run` round-trip + drift-refusal; AGENT-01/02/03/04)
- [x] Phase 27: Install Addon, Preflight Gates & `villa verify agent` (6/6 plans) — completed 2026-06-14 (on-hardware readiness + PRIV-06 under real rootless-netns egress block; gap-closure 27-05/06; INSTALL-03/04, PRIV-06)
- [x] Phase 28: Agent Surfacing & Contracts (3/3 plans) — completed 2026-06-15 (`status.Report` 3→4 single re-freeze, dashboard Agent panel, doctor/backup/usage; SURF-01/02/03, USAGE-03/04)

Audit `tech_debt` — 17/17 requirements satisfied, integration PASS (7/7 seam groups, 0 blockers), 6/6 E2E flows, Nyquist 4/5 (Phase 28 contract open); no blockers, debt acknowledged at close. See `milestones/v1.4-ROADMAP.md` for full phase detail, success criteria, and plan breakdowns.

</details>

### 🔨 v1.5 Web Search (Grounded & Guarded) — Phases 29–34 (IN PROGRESS)

**Milestone goal:** Give the local model accurate, up-to-date answers by grounding them in live web search — integrated alongside the existing Open WebUI chat — while defending in depth against malicious sites that try to inject destructive/dangerous prompts into the model. **Integrate, not rebuild:** exactly ONE new search container (SearXNG) wired into OWUI's *native* web search, with the fetch → chunk → embed → retrieve → cite pipeline reusing the shipped v1.3 villa-embed/Qdrant RAG verbatim; the differentiator is a villa-owned injection-defense guard layer *in front of* the embed (the new `villa-websafe` loader), plus a provable egress-bounding backstop. **Strictly opt-in, default-OFF** — with web search disabled the install renders byte-identical to v1.4 and the zero-outbound posture is unchanged.

**Two governing claims (frozen into success criteria):** *"outbound is bounded"* is provable and IS proven (negative-control-first, inverse-framed); *"safe from injection"* is **NOT solvable and is never claimed** — the guard layer **reduces and flags, never eliminates**, and the browser-side markdown-image exfil channel is documented as a known residual, not closed.

**Build order is research-converged (fit/orchestrate → guard → verify → surface-last):** the premise SearXNG container and OWUI env wiring first (they own the orchestrate render goldens); the bounded fetch path (SSRF + ephemeral collection + ctx-fit) before the guard policy is layered onto it; the guard before the verify that asserts on its output; egress-bounding `villa verify search` as the honest backstop over the full assembly; and ALL surfacing last — the single `status.Report` 4→5 bump with exactly one golden re-freeze.

- [ ] **Phase 29: SearXNG Search Service** - The premise container: `villa-searxng` rendered like v1.3 qdrant/embed, readiness proven by a real `format=json` query
- [ ] **Phase 30: OWUI Native-Search Wiring** - Env-only opt-in wiring of OWUI's native SearXNG search behind the orchestrate seam, off-render byte-identical to v1.4
- [ ] **Phase 31: Grounded Fetch → Embed Grounding** - The `villa-websafe` fetch path: full-page fetch → embed via v1.3 RAG → cited answer, dedicated ephemeral collection, ctx reservation, SSRF guard
- [ ] **Phase 32: Villa Injection Guard Layer** - Sanitize + Unicode-normalize + nonced provenance-fence + heuristic flag-not-block classifier layered onto the fetch path (reduces/flags, never eliminates)
- [ ] **Phase 33: Egress-Bounding + `villa verify search`** - The honest backstop: inverse-framed negative-control-first egress proof under a real rootless-netns nft block; opt-in/default-off honesty
- [ ] **Phase 34: Web-Search Surfacing (LANDS LAST)** - The single `status.Report` 4→5 bump + dashboard Web Search panel + doctor checks + backup/restore coverage; outbound-bounded indicator derives from the real verify-search result

## Phase Details

### Phase 29: SearXNG Search Service
**Goal**: The operator has a local SearXNG metasearch service running as a managed Quadlet unit on `villa.network`, returning parseable JSON results from a bounded, auditable set of upstream engines — the premise that nothing grounds without.
**Depends on**: Nothing new (builds on the established v1.3 managed-service render path; first v1.5 phase)
**Requirements**: SRCH-01, SRCH-04
**Success Criteria** (what must be TRUE):
  1. `villa-searxng.container`/`.volume` come up on `villa.network` with container-DNS-only reachability and **no host port** (rendered like the v1.3 qdrant/embed managed-service path; image digest-pinned behind a seam-locked const).
  2. `settings.yml` is rendered from config with `search.formats: [html, json]`, a generated `secret_key`, and `limiter: false` — and a real `format=json` query returns parseable JSON results (readiness is the actual query, never a health-200).
  3. The rendered SearXNG runs a **vetted subset** of upstream engines (a bounded, auditable list of outbound upstream hosts), not the full default engine set.
  4. With web search not configured, the rest of the stack renders byte-identical to v1.4 (the new unit is additive and gated).
**Plans**: TBD

### Phase 30: OWUI Native-Search Wiring
**Goal**: The operator's Open WebUI is wired to the local SearXNG via OWUI's native web search, opt-in per-query, with honest no-results behavior — and with web search off the install is byte-identical to v1.4.
**Depends on**: Phase 29 (SearXNG must be reachable before OWUI can call it)
**Requirements**: SRCH-02, SRCH-03
**Success Criteria** (what must be TRUE):
  1. OWUI reaches the local SearXNG through its **native** web search via an env-only block behind the orchestrate seam (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count) with `ENABLE_PERSISTENT_CONFIG=False` mandatory and frozen in the golden.
  2. The operator can opt into web search per-query/per-session via OWUI's native toggle and tune the result count.
  3. On no-results the behavior is honest (never a fabricated answer).
  4. The search-disabled render is **byte-identical to v1.4**, and a drift test binds the env keys to their orchestrate accessors (env-name churn caught by construction).
**Plans**: TBD
**UI hint**: yes

### Phase 31: Grounded Fetch → Embed Grounding
**Goal**: With search on, the operator asks a current-events/research question and gets an answer grounded in fetched pages with inline citations to live URLs — the fetch path established and resource-bounded (SSRF-guarded, ephemeral, ctx-reserved) before any guard policy is layered on.
**Depends on**: Phase 30 (OWUI search wiring must exist; this swaps OWUI's fetch to `villa-websafe`)
**Requirements**: GROUND-01, GROUND-02, GROUND-03, GUARD-01, GUARD-05
**Success Criteria** (what must be TRUE):
  1. The `villa-websafe` loader is registered as OWUI's `WEB_LOADER_ENGINE=external` fetch path and is the **sole producer of `page_content`** — every byte embedded or shown to the model passes through it (the GUARD-01 seam lands here, on the fetch path).
  2. A search-on research question returns an answer **grounded in fetched pages with inline citations to live URLs** — full-page fetch → chunk → embed → retrieve → cite reusing the v1.3 villa-embed/Qdrant RAG and its top-level `sources` field verbatim (no new embedder, vector DB, or citation plumbing).
  3. Fetched content is embedded into a **dedicated ephemeral collection** (clean-replace / bounded lifetime), never the operator's durable memory/document-KB store.
  4. `recommend` reserves a web-search context budget (bounded result-count × page-size) **before** the chat-model fit, and residency is **offload-asserted under search load** (a silent/partial CPU fallback is a FAIL).
  5. The fetcher enforces an **SSRF guard** — resolve-and-validate the target IP (reject loopback / link-local / `169.254.169.254` / internal `villa-*` hosts), re-check after every redirect, and allow only an http(s) scheme list.
**Plans**: TBD

### Phase 32: Villa Injection Guard Layer
**Goal**: Fetched web content reaching the model is sanitized, Unicode-normalized, provenance-fenced as untrusted-data-not-instructions, and screened by a heuristic classifier that flags injection attempts — honestly surfaced as "reduces and flags, does not eliminate."
**Depends on**: Phase 31 (the guard policy layers onto the established `villa-websafe` fetch path)
**Requirements**: GUARD-02, GUARD-03, GUARD-04
**Success Criteria** (what must be TRUE):
  1. Fetched content is **sanitized** (active markup stripped via pure-Go `bluemonday` StrictPolicy) and **normalized** (invisible / bidirectional / zero-width / homoglyph Unicode neutralized) *before* fencing.
  2. Sanitized content is wrapped in a **nonced provenance fence** marking it untrusted-data-not-instructions before it reaches the model.
  3. A pure-Go **heuristic injection classifier** flags injection attempts as a flag-not-block tripwire (never silently passes), and the detection outcome (strip/flag/quarantine) is surfaced honestly.
  4. The package doc and operator-facing copy state **"reduces and flags, does not eliminate"** (no "injection-safe" copy), and the browser-side markdown-image exfiltration channel is **documented as a known residual** — not claimed closed.
**Plans**: TBD
**Research**: `/gsd-plan-phase --research-phase` — needs a phase-specific adversarial injection corpus + a pre-declared injection-detection precision/recall eval (invisible-Unicode + fence-breakout payloads; mirror the v1.4 must-WIN-eval discipline). The rules-vs-model decision is already settled to heuristic-rules-for-v1.5 (the DeBERTa/PromptGuard sidecar is deferred as GUARD-V2-01 behind a must-WIN eval).

### Phase 33: Egress-Bounding + `villa verify search`
**Goal**: The operator can prove — negative-control-first, inverse-framed, under a real rootless-netns nft block — that web search's outbound is bounded; web search is opt-in/default-off and OWUI's lazy/background outbound is killed, so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches.
**Depends on**: Phase 32 (verify asserts on the guard's stripped + fenced output; you cannot prove a guard that does not exist)
**Requirements**: PRIV-07, PRIV-08, PRIV-09
**Success Criteria** (what must be TRUE):
  1. Web search is **opt-in, default-OFF**: with it disabled the install renders byte-identical to v1.4 and the zero-outbound posture is unchanged.
  2. `villa verify search` proves bounded outbound **negative-control-first, inverse-framed** under a real rootless-netns nft block — an off-allowlist canary is reachable *unguarded* and blocked *under* the bound; an ineffective block is **REJECTED**, never a fabricated PASS.
  3. The proof also asserts a planted-injection page comes back stripped + fenced + flagged, exercises SSRF internal-host cases, and includes a secret-in-query-string exfil case.
  4. OWUI's lazy/background outbound (HuggingFace pulls, telemetry) is killed (`HF_HUB_OFFLINE` + telemetry kill switches) and any web-search-required weights are pre-staged, so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches.
**Plans**: TBD
**Research**: `/gsd-plan-phase --research-phase` — needs the exact rootless-netns nft mechanics for the inverse-framed bound. The v1.4 verify-agent four-layer harness is the template, but the canary/allowlist assertions are new and **easy to get backwards** (off-allowlist canary reachable unguarded, blocked under the bound — never invert this).

### Phase 34: Web-Search Surfacing (LANDS LAST)
**Goal**: The finished, proven web-search feature set is surfaced — `villa status`/`--json`, the dashboard, `villa doctor`, and `villa backup`/`restore` all reflect it — over a single append-only `status.Report` 4→5 bump (one golden re-freeze), with the outbound-bounded indicator deriving from the real `villa verify search` result, never a config bool.
**Depends on**: Phase 33 (surfacing reads a proven feature; the schema bump must freeze a finished, verified set — staggered-contract-risk discipline)
**Requirements**: SURF-04, SURF-05, SURF-06, SURF-07
**Success Criteria** (what must be TRUE):
  1. `villa status`/`--json` gains exactly one **append-only** `web_search` block (`status.Report` schema **4→5**, single golden re-freeze): enabled state, `villa-searxng`/`villa-websafe` health rows, guard-verdict counters, last-query freshness, and an **outbound-bounded indicator derived from the real `villa verify search` result** (never a bare config bool).
  2. The control dashboard gains a **hidden-until-data, XSS-safe Web Search panel** surfacing search/guard/outbound state, including outbound visibility (what was searched/fetched) and the bounded-outbound indicator.
  3. `villa doctor` folds web-search checks (on **doctor's own** schema bump) — service readiness, guard health, and egress-proof status — as tri-state (ready / degraded-with-reason / typed-Unknown), with an **offload-asserting residency check under search load** and remediation on every non-PASS.
  4. `villa backup`/`restore` cover the web-search configuration (SearXNG `settings.yml` provenance + the `WebSearchEnabled` gate), consistent with prior backup coverage; fetched ephemeral web content is **excluded by design**.
**Plans**: TBD
**UI hint**: yes

## Cross-Cutting Constraints (v1.5)

These standing project disciplines apply to every v1.5 phase — stated here once, not as separate phases:

- **Opt-in / default-OFF**: web search is an explicit addon; with it disabled the install renders byte-identical to v1.4 and the zero-outbound posture is unchanged (PRIV-07 is the gate that the off-render of every phase honors).
- **Off-render byte-identical**: any phase that touches the orchestrate render must keep the search-disabled output byte-identical to v1.4 (golden-frozen).
- **EXACTLY ONE byte-frozen contract bump**: `status.Report` 4→5, append-only, single golden re-freeze, landing **LAST in Phase 34**. `doctor` owns its own schema bump (independent). `recommend`'s ctx-reservation evolution (Phase 31) is a separate contract landing once where the fit math changes.
- **Seam-locked literals**: SearXNG/villa-websafe image digests + any marker/host literals stay behind their seam (`internal/orchestrate` for managed services, `internal/inference` for backend markers); `TestSeamGrepGate` walks `internal/` + `cmd/villa` and fails on a leak.
- **CGO-free single static binary**: the guard is pure Go (`bluemonday` StrictPolicy is the non-negotiable core); no new runtime/container beyond the one SearXNG image. The PromptGuard/DeBERTa model classifier is deferred (GUARD-V2-01) precisely because it would break this discipline.
- **orchestrate is the only intentionally-impure module**: `villa-websafe` is a tiny Go HTTP service over a pure, unit-testable `internal/websafe` core (network fetch injected as a `Deps` func); host effects stay behind the orchestrate/cmd seams.
- **Never false-green a privacy/security claim**: *"outbound is bounded"* is proven negative-control-first, inverse-framed, and an ineffective block is rejected (never a fabricated PASS); *"safe from injection" is NOT claimed* — injection is not solvable, the guard reduces/flags only, and the browser-side markdown-image exfil channel is documented as a known residual, not closed.

## Research Flags (v1.5)

| Phase | Research needed? | Why |
|-------|------------------|-----|
| 29 SearXNG Service | No (lighter) | Managed-service render is a proven v1.3 qdrant/embed pattern (verified in-repo). |
| 30 OWUI Wiring | No (lighter) | Env-only wiring is a proven v1.3 pattern; re-verify exact OWUI env-var names at the pinned `@sha256`. |
| 31 Grounded Fetch | **Yes** — `--research-phase` | The `villa-websafe` fetch path + OWUI `ExternalWebLoader` external-loader contract + SSRF guard; on-hardware result-count × page-size ctx caps unmeasured. |
| 32 Guard Layer | **Yes** — `--research-phase` | Adversarial injection corpus + a pre-declared precision/recall eval (must-WIN-eval discipline); rules-vs-model already settled to heuristic-rules. |
| 33 Egress Bounding | **Yes** — `--research-phase` | Exact rootless-netns nft mechanics for the inverse-framed bound (the v1.4 harness is the template; the canary/allowlist assertions are new and easy to get backwards). |
| 34 Surfacing | No (lighter) | The surfacing pattern (one append-only bump, hidden-until-data panel) is the v1.2–v1.4 invariant. |

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
| 24. Coder Fit Math, Catalog & On-Hardware Model Qualification | v1.4 | 4/4 | Complete | 2026-06-13 |
| 25. Coding-Mode Render & Transactional Swap Verb | v1.4 | 2/2 | Complete | 2026-06-13 |
| 26. Agent Delivery Core & Lockdown Launcher | v1.4 | 3/3 | Complete | 2026-06-13 |
| 27. Install Addon, Preflight Gates & `villa verify agent` | v1.4 | 6/6 | Complete | 2026-06-14 |
| 28. Agent Surfacing & Contracts | v1.4 | 3/3 | Complete | 2026-06-15 |
| 29. SearXNG Search Service | v1.5 | 0/? | Not started | - |
| 30. OWUI Native-Search Wiring | v1.5 | 0/? | Not started | - |
| 31. Grounded Fetch → Embed Grounding | v1.5 | 0/? | Not started | - |
| 32. Villa Injection Guard Layer | v1.5 | 0/? | Not started | - |
| 33. Egress-Bounding + `villa verify search` | v1.5 | 0/? | Not started | - |
| 34. Web-Search Surfacing | v1.5 | 0/? | Not started | - |
