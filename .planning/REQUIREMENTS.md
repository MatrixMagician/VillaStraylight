# Requirements: VillaStraylight — v1.5 Web Search (Grounded & Guarded)

**Defined:** 2026-06-18
**Core Value:** Run a capable local AI workspace that "just works" after install — hardware-aware setup that brings inference, chat, and the dashboard up healthy, with zero data leaving the box. v1.5 extends the bar to: *and can ground answers in live web search when the operator opts in — accurate, up-to-date, with malicious-site prompt injection defended-in-depth and outbound provably bounded.*

> **Posture note (read first):** Web search **deliberately punctures** the "zero data leaving the box" default — SearXNG queries reach upstream engines and page fetches visit result sites. v1.5 reconciles this by making web search **strictly opt-in, default-OFF** (zero-outbound install stays byte-identical to v1.4), **bounding** the outbound surface, and **proving** it negative-control-first. Two claims govern the milestone: *"outbound is bounded"* is provable and must be proven; *"safe from injection"* is **NOT solvable and must never be claimed** — the guard layer **reduces and flags**, never eliminates.

## v1 Requirements

Requirements for the v1.5 milestone. Each maps to exactly one roadmap phase (Traceability below).

### Search Service & Wiring (SRCH)

- [ ] **SRCH-01**: Operator gets a SearXNG metasearch service running as a rootless Podman Quadlet unit on `villa.network` (container-DNS only, no host port, digest-pinned), with `settings.yml` rendered from config (`search.formats: [html, json]`, generated `secret_key`, `limiter: false`); readiness is proven by a real `format=json` query returning parseable results, never a health-200.
- [ ] **SRCH-02**: Operator's Open WebUI is wired to the local SearXNG via OWUI's **native** web search, env-only behind the orchestrate seam (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count), with `ENABLE_PERSISTENT_CONFIG=False` mandatory; the search-disabled render stays byte-identical to v1.4.
- [ ] **SRCH-03**: Operator can opt into web search per-query/per-session via OWUI's native toggle, tune result count, and get honest behavior on no-results (never a fabricated answer).
- [x] **SRCH-04**: Operator's SearXNG is rendered with a vetted subset of upstream engines (bounded, auditable set of outbound upstream hosts) rather than the full default engine set.

### Grounded Answers (GROUND)

- [ ] **GROUND-01**: Operator asks a current-events/research question with search on and gets an answer **grounded in fetched pages with inline citations to live URLs** — full-page fetch → chunk → embed → retrieve → cite, reusing the shipped v1.3 villa-embed/Qdrant RAG and its top-level `sources` field verbatim (no new embedder, vector DB, or citation plumbing).
- [ ] **GROUND-02**: Fetched web content is embedded into a **dedicated ephemeral collection** (clean-replace / bounded lifetime), never the operator's durable memory/document-KB store.
- [ ] **GROUND-03**: `recommend` reserves a web-search context budget (bounded result-count × page-size) **before** the chat-model fit so a search-enabled envelope cannot silently CPU-fall-back; residency is offload-asserted under search load (a silent/partial fallback is a FAIL).

### Injection-Defense Guard Layer (GUARD)

- [ ] **GUARD-01**: A villa-owned `villa-websafe` loader (registered as OWUI's `WEB_LOADER_ENGINE=external` fetch path) is the **sole producer of `page_content`** — every byte embedded or shown to the model passes through it.
- [ ] **GUARD-02**: Fetched content is **sanitized** (active markup stripped via pure-Go `bluemonday` StrictPolicy) and **normalized** (invisible / bidirectional / zero-width / homoglyph Unicode neutralized) *before* fencing.
- [ ] **GUARD-03**: Sanitized content is wrapped in a **nonced provenance fence** marking it untrusted-data-not-instructions before it reaches the model.
- [ ] **GUARD-04**: A pure-Go **heuristic injection classifier** flags injection attempts (flag-not-block tripwire — never silently passes); detection outcome (strip/flag/quarantine) is surfaced honestly, the package doc + operator-facing copy state **"reduces and flags, does not eliminate,"** and the browser-side markdown-image exfiltration channel is **documented as a known residual** (not claimed closed).
- [ ] **GUARD-05**: The fetcher enforces an **SSRF guard** — resolve-and-validate the target IP (reject loopback / link-local / `169.254.169.254` / internal `villa-*` hosts), re-check after every redirect, and allow only an http(s) scheme list.

### Privacy & Egress Honesty (PRIV)

- [ ] **PRIV-07**: Web search is **opt-in, default-OFF**; with it disabled the install renders byte-identical to v1.4 and the zero-outbound posture is unchanged.
- [ ] **PRIV-08**: `villa verify search` **proves bounded outbound negative-control-first** under a real rootless-netns nft block — **inverse-framed**: an off-allowlist canary must be reachable *unguarded* and blocked *under* the bound (an ineffective block is REJECTED, never a fabricated PASS); the proof also asserts a planted-injection page comes back stripped + fenced + flagged, exercises SSRF internal-host cases, and includes a secret-in-query-string exfil case.
- [ ] **PRIV-09**: OWUI's lazy/background outbound (HuggingFace model pulls, telemetry) is killed (`HF_HUB_OFFLINE` + telemetry kill switches) and any web-search-required weights are pre-staged, so the only sanctioned runtime outbound is SearXNG upstreams + result-page fetches.

### Surfacing & Operability (SURF — lands last)

- [ ] **SURF-04**: `villa status` / `--json` gains exactly one append-only `web_search` block (`status.Report` schema **4→5**, single golden re-freeze): enabled state, `villa-searxng`/`villa-websafe` health rows, guard-verdict counters, last-query freshness, and an **outbound-bounded indicator derived from the real `villa verify search` result** (never a bare config bool).
- [ ] **SURF-05**: The control dashboard gains a **hidden-until-data, XSS-safe Web Search panel** surfacing search/guard/outbound state, including outbound visibility (what was searched/fetched) and the bounded-outbound indicator.
- [ ] **SURF-06**: `villa doctor` folds web-search checks (on doctor's own schema bump) — service readiness, guard health, and egress-proof status — as tri-state (ready / degraded-with-reason / typed-Unknown), with an offload-asserting residency check under search load and remediation on every non-PASS.
- [ ] **SURF-07**: `villa backup` / `restore` cover the web-search configuration (SearXNG `settings.yml` provenance + the `WebSearchEnabled` gate), consistent with prior backup coverage; fetched ephemeral web content is excluded by design.

## v2 Requirements

Deferred to a future milestone. Tracked, not in the v1.5 roadmap.

### Advanced Guard (GUARD-V2)

- **GUARD-V2-01**: Model-based injection classifier (e.g. a PromptGuard/DeBERTa sidecar) — deferred behind a **pre-declared must-WIN precision/recall eval** vs. the v1.5 heuristic baseline; adds a new Python runtime/container, so it must prove its worth before breaking the single-static-binary discipline.

### Advanced Search (SRCH-V2)

- **SRCH-V2-01**: Focus modes (Academic / News / Reddit / etc.) and time-range filtering beyond basic result-count tuning.
- **SRCH-V2-02**: Embedding-based reranking of fetched results (ties to the deferred RAG-Q-01 reranker — third resident model / latency cost on a constrained envelope).
- **SRCH-V2-03**: Multi-round "deep research / quality mode" (iterative search→read→refine).

## Out of Scope

Explicitly excluded for v1.5, with reasoning. Anti-features from research are listed here as warnings.

| Feature | Reason |
|---------|--------|
| **Agentic browsing** (the agent navigates/clicks/acts on pages) | The exact vector that hijacked Perplexity Comet; read-only fetch+embed+cite only. Massively expands the injection blast radius. |
| **Link-following beyond depth=1** | Each followed link is a fresh untrusted fetch + egress surface; bounded to the direct result pages. |
| **Cloud search / extraction APIs** (Tavily, Firecrawl, Brave API, etc.) | Violates strictly-local posture; SearXNG is self-hosted metasearch. Outbound must stay to upstreams + result pages only. |
| **Persisting fetched web content into durable memory / document-KB** | Web content is untrusted + ephemeral; mixing it into the durable v1.3 memory/KB store would poison long-term recall. Dedicated ephemeral collection only (GROUND-02). |
| **Claiming injection immunity / "safe from injection"** | Indirect prompt injection is **not solvable**; combined defenses only reduce attack success, never to zero. The product claims *reduces + bounds + surfaces*. Grep-ban "injection-safe" copy. |
| **Always-on / non-opt-in web search** | Breaks the default zero-outbound posture; web search must be an explicit opt-in the operator turns on (PRIV-07). |
| **Always-on runtime egress firewall enforcement** | v1.5 *proves* bounded outbound via `villa verify search` (test-time nft) + app-level SSRF guard, mirroring the v1.4 verify-agent posture; a shipped always-on host firewall is a larger, separate undertaking. |
| **Closing the browser-side markdown-image exfil channel** | It bypasses container egress entirely (operator's browser renders the image); documented as a known residual + mitigated where feasible (CSP/same-origin), not claimed closed (GUARD-04). |

## Traceability

Which phase covers which requirement. Populated during roadmap creation.

| Requirement | Phase | Status |
|-------------|-------|--------|
| SRCH-01 | Phase 29 | Pending |
| SRCH-02 | Phase 30 | Pending |
| SRCH-03 | Phase 30 | Pending |
| SRCH-04 | Phase 29 | Complete |
| GROUND-01 | Phase 31 | Pending |
| GROUND-02 | Phase 31 | Pending |
| GROUND-03 | Phase 31 | Pending |
| GUARD-01 | Phase 31 | Pending |
| GUARD-02 | Phase 32 | Pending |
| GUARD-03 | Phase 32 | Pending |
| GUARD-04 | Phase 32 | Pending |
| GUARD-05 | Phase 31 | Pending |
| PRIV-07 | Phase 33 | Pending |
| PRIV-08 | Phase 33 | Pending |
| PRIV-09 | Phase 33 | Pending |
| SURF-04 | Phase 34 | Pending |
| SURF-05 | Phase 34 | Pending |
| SURF-06 | Phase 34 | Pending |
| SURF-07 | Phase 34 | Pending |

**Coverage:**

- v1 requirements: 19 total
- Mapped to phases: 19 ✓ (Phase 29: 2 · Phase 30: 2 · Phase 31: 5 · Phase 32: 3 · Phase 33: 3 · Phase 34: 4)
- Unmapped: 0 ✓ (no orphans, no duplicates)

---
*Requirements defined: 2026-06-18*
*Last updated: 2026-06-18 after roadmap creation — all 19 v1 requirements mapped to Phases 29–34 (research-converged six-phase build order: fit/orchestrate → guard → verify → surface-last; numbering continues from v1.4's Phase 28).*
