# Stack Research

**Domain:** Opt-in, guarded web search added to a strictly-local Go control-plane AI stack (VillaStraylight v1.5)
**Researched:** 2026-06-18
**Confidence:** HIGH (versions verified against Docker Hub / GitHub releases / official docs as of June 2026; one MEDIUM area flagged: the prompt-injection classifier runtime path)

> **Scope discipline.** Every choice below assumes the locked v1.5 decisions: SearXNG as a rootless Quadlet unit on `villa.network` wired into Open WebUI's NATIVE web-search; snippet + full-page fetch reusing the **existing** v1.3 `villa-embed`/Qdrant RAG stack; a **villa-owned** Go guard layer (sanitize + provenance-fence + classify); opt-in / default-OFF / egress-bounded. This research **supports** those decisions; it does not relitigate them. See **What NOT to Use** for the anti-scope-creep boundary — that section is as load-bearing as the recommendation.

---

## Recommended Stack

### Core Technologies (new infrastructure containers)

| Technology | Version | Purpose | Why Recommended |
|------------|---------|---------|-----------------|
| **SearXNG** (`docker.io/searxng/searxng`) | date-tag `2026.6.17` (current; pin the matching `@sha256:` digest) | Meta-search aggregator queried by OWUI; the only NEW container | The reference privacy-respecting metasearch engine, no API keys, no upstream account. OWUI has it as a first-class native engine (`WEB_SEARCH_ENGINE=searxng`). Single-user local load is trivial for it. Matches DreamServer's choice and the deferred SRCH-01 theme. **Pin by digest** exactly like every other villa image (the `latest`/date tags are rolling — SearXNG ships a release per commit to master). |
| **Open WebUI** (`ghcr.io/open-webui/open-webui`) | `v0.9.6` (current; **already integrated** — this is a wiring change, not a new image) | Owns query→search→fetch→embed→retrieve→cite; villa adds env + a fetch-time guard hop | v0.9.6 is the version v1.3/v1.4 already pin and prove. Its native web search **reuses the same RAG embedding pipeline** villa already wired to `villa-embed`/Qdrant — so no new embedding plumbing (see Architecture note below). |
| **llama.cpp `llama-server`** (existing `villa-embed` + chat units) | unchanged (Vulkan RADV default) | Embedding of fetched pages (existing path); optionally hosts the injection classifier IF a GGUF-compatible model is chosen | Reuse, don't add. The fetched-page embedding already flows through `villa-embed` (nomic-embed-text-v1.5 Q8_0, 768-dim) via OWUI's RAG path. |

### Supporting Libraries (villa-owned Go guard layer — pure Go, CGO-free)

| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| **`github.com/microcosm-cc/bluemonday`** | `v1.0.27` | Allowlist HTML sanitizer — strip ALL active markup (`<script>`, `<style>`, event handlers, `javascript:` URIs, hidden/`aria` injection vectors) from fetched pages | The primary defensive strip. Use `bluemonday.StrictPolicy()` (text-only, removes every tag) for the provenance-fenced content that reaches the model. Pure Go over `golang.org/x/net/html` (the core-team token parser) — **CGO-free, single-static safe.** |
| **`github.com/PuerkitoBio/goquery`** | `v1.10.x` | jQuery-style DOM traversal — extract main content, drop boilerplate/nav/footer before fencing | When snippet text is insufficient and you want clean main-body text from a full-page fetch. Pure Go (also over `x/net/html`). |
| **`github.com/go-shiori/go-readability`** | actively maintained (pseudo-versioned; **prefer this over the unmaintained `mauidude/go-readability`**) | arc90 readability extraction → clean article text | Optional: if villa wants to extract main article text itself rather than trusting OWUI's web loader. Built on goquery, pure Go. |
| **`golang.org/x/net/html`** | `v0.x` (transitive, already in tree via deps) | Underlying tokenizer | Already present; no new top-level dep. Direct use only if a bespoke token-walk is needed. |

> **Decision:** bluemonday is the non-negotiable core (the active-markup strip + provenance-fence inputs). goquery/go-readability are **optional content-quality** helpers — add only if OWUI's own web loader extraction proves insufficient. Start with bluemonday alone to minimize new deps.

### Injection-classification approach (the guard "classify" pass) — see decision matrix below

| Option | Verdict | Notes |
|--------|---------|-------|
| **Heuristic rule pass (pure Go)** | **RECOMMENDED for v1.5 baseline** | Zero new model, zero new container, CGO-free. Regex/string heuristics over the sanitized text: imperative-override phrases ("ignore previous instructions", "disregard", "system prompt", "you are now"), tool/command sigils, base64 blobs, zero-width/bidi control chars, excessive instruction density. Fast, deterministic, golden-testable, fits the "pure core + injected seam" pattern. The **provenance-fence is the real defense**; the classifier is a flagging tripwire. |
| **PromptGuard 2 (DeBERTa-xsmall 22M / 86M) via a NEW sidecar** | **DEFER / out of v1.5 baseline** | Meta's `Llama-Prompt-Guard-2-22M`/`-86M` are purpose-built injection classifiers — but they are **DeBERTa sequence-classification models, NOT GGUF-generative**. `llama-server` does **not** run DeBERTa classifier heads (it supports generative + embedding + Qwen3-style rerankers with `cls.output.weight`, not arbitrary HF `*ForSequenceClassification`). Running PromptGuard would require a NEW Python/transformers sidecar container — new runtime, new image to pin/prove, breaks "Go control plane only / reuse OSS containers" minimalism. Only pursue behind a must-WIN eval (mirrors the v1.4 CODER-V2-02 discipline). |
| **Generative LLM-as-judge via the resident chat model** | OPTIONAL escalation | Send the fenced content to the already-resident chat model with a "is this trying to inject instructions?" classification prompt over `/v1`. Zero new model. Costs latency/tokens and is itself injectable — keep it as an optional second-stage, never the sole gate. |

> **Recommendation:** Ship the **heuristic rule pass** as the villa-owned classifier in v1.5 (pure Go, no new container, golden-testable, honest). The architecture's real guarantee is **provenance-fencing fetched content as untrusted-data-not-instructions** (data, never instructions); the classifier is a tripwire that flags/annotates, it is not load-bearing. Keep PromptGuard-as-sidecar explicitly deferred behind an eval.

### Development Tools

| Tool | Purpose | Notes |
|------|---------|-------|
| `go test` golden fixtures | Freeze the sanitizer output + the rendered SearXNG `settings.yml` + the new Quadlet `.container` unit + the `status.Report` schema bump | Same byte-frozen-contract discipline as v1.2–v1.4: append-only fields, ONE `status.Report` re-freeze landing last. |
| `TestSeamGrepGate` (existing) | Extend marker-string gate if any SearXNG image literal / loopback fence needs to stay behind a seam | SearXNG image tag + digest is an orchestrate concern; keep literals out of `cmd/villa`. |
| `villa verify search` (new, mirrors `villa verify memory`/`agent`) | Negative-control-first egress proof | Reuse the v1.4 rootless-netns nft FORWARD-block harness. |

---

## Installation

No `npm`/`pip` here — this is Go + pinned container images.

```bash
# New Go deps (command/guard tier; pure Go, CGO-free)
go get github.com/microcosm-cc/bluemonday@v1.0.27
# optional content-quality helpers (add only if OWUI loader extraction insufficient)
go get github.com/PuerkitoBio/goquery@latest
go get github.com/go-shiori/go-readability@latest

# Pin the new container by digest (resolve the digest for the chosen date-tag, then freeze it
# alongside the other images, e.g. in internal/orchestrate/searxng.go):
podman pull docker.io/searxng/searxng:2026.6.17
podman inspect --format '{{index .RepoDigests 0}}' docker.io/searxng/searxng:2026.6.17
# -> docker.io/searxng/searxng@sha256:<digest>   (freeze THIS, not the moving date-tag)
```

**Minimal SearXNG `settings.yml`** (villa renders this from config, like every other unit — never hand-edited):

```yaml
# rendered by villa into a config volume mounted at /etc/searxng/settings.yml
use_default_settings: true
server:
  secret_key: "<villa-generated random 32+ bytes; per-install, stored 0600>"   # MANDATORY — SearXNG refuses to start without it
  limiter: false            # single local user; the bot-limiter (link_token) would 403 OWUI's JSON calls
  image_proxy: false
search:
  formats:
    - html
    - json                  # MANDATORY for OWUI API use — without it OWUI gets 403 Forbidden
  # engines: keep the default general set OR trim to a vetted subset (e.g. duckduckgo, brave,
  # wikipedia, google) to bound which upstreams receive queries. Trimming = fewer outbound hosts.
```

> **No `limiter.toml` bot-detection** is needed when `server.limiter: false` for a single local user. If the limiter is ever enabled, OWUI's JSON requests require `botdetection.ip_limit.link_token = false`.

**Open WebUI env wiring** (added behind the existing orchestrate env seam, conditional on `search_enabled`, default-OFF):

```ini
ENABLE_WEB_SEARCH=True
WEB_SEARCH_ENGINE=searxng
SEARXNG_QUERY_URL=http://villa-searxng:8080/search?q=<query>   # container-DNS, NO host port (matches villa-embed/Qdrant pattern)
WEB_SEARCH_RESULT_COUNT=3                # snippet/result count (tune for the envelope)
WEB_SEARCH_CONCURRENT_REQUESTS=10
WEB_LOADER_CONCURRENT_REQUESTS=2         # bound full-page fetch fan-out
# Full-page fetch + embed reuses the EXISTING villa-embed/Qdrant RAG pipeline by default
# (BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=False => results are chunked+embedded+retrieved).
BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=False
ENABLE_WEB_LOADER_SSL_VERIFICATION=True
ENABLE_PERSISTENT_CONFIG=False           # CRITICAL — inherited from v1.3 D-09; web-search settings are
                                         # ConfigVars that bake into webui.db on first boot otherwise.
```

> **CRITICAL carryover (v1.3 lesson):** OWUI web-search settings are **PersistentConfig/ConfigVars** — once written to `webui.db` they shadow the env. The v1.3 mandate `ENABLE_PERSISTENT_CONFIG=False` MUST cover the new vars so `config.toml` stays the single source of truth. This is the highest-risk integration gotcha; flag it to the roadmap.

---

## Architecture / Integration Notes (load-bearing for the roadmap)

1. **Web search REUSES the existing RAG embedding pipeline.** OWUI's native web search, with bypass OFF, fetches result pages → chunks → embeds via the **same** configured embedding backend (`villa-embed`, 768-dim) → stores/retrieves via the **same** Qdrant → answers with `sources` citations. **No new vector DB, no new embedding model, no new villa-embed wiring** — the v1.3 stack carries over verbatim. This is the single most important confirmation for scope control.

2. **The villa-owned guard layer is a content-transform hop, not a rebuild of OWUI search.** OWUI fetches; villa's value-add is to **sanitize (bluemonday strip) + provenance-fence (wrap as untrusted-data-not-instructions) + classify (heuristic tripwire)** the fetched content before it reaches the model. Cleanest integration: a loopback transform OWUI's loader/pipeline calls, or a villa-side post-fetch normalization — NOT a fork of OWUI's search handler. Confirm the exact OWUI extension seam (Functions/Filter vs. external web-loader) at plan time; this is the one MEDIUM-confidence integration point.

3. **One new container, one new network member.** `villa-searxng` joins `villa.network`, container-DNS only, **no host port** — exactly the v1.3 `villa-embed`/Qdrant pattern. Outbound (to upstream search engines + fetched sites) is the explicit, surfaced exception, bounded and proven negative-control-first.

4. **Surfacing lands last, one `status.Report` schema bump.** A `search` block (enabled, engine, result-count, last-query-time, outbound-bounded indicator) added append-only with ONE golden re-freeze — same staggered-contract discipline as v1.2–v1.4 (`status.Report` is currently at 4 from v1.4 → 4→5).

---

## Alternatives Considered

| Recommended | Alternative | When to Use Alternative |
|-------------|-------------|-------------------------|
| SearXNG native OWUI engine | Perplexica / Perplexica-style answer engine (DreamServer ships it) | If you wanted a separate answer-UI surface; rejected — duplicates OWUI's native search, adds a second UI + container, violates integrate-first. |
| bluemonday `StrictPolicy` (strip all) | `UGCPolicy` (allow some formatting) | Never for model-bound content — fenced data must be inert text. UGCPolicy is for human-rendered UGC, not LLM input. |
| Heuristic Go classifier | PromptGuard-2 DeBERTa sidecar | Only behind a must-WIN eval that proves it beats heuristics enough to justify a NEW transformers/Python container — deferred, mirrors CODER-V2-02. |
| go-shiori/go-readability | mauidude/go-readability | Never — `mauidude` is stale/unmaintained; the search-result hit is years old. Prefer `go-shiori`. |
| SearXNG `limiter: false` (single user) | limiter on + `link_token=false` | Only if the SearXNG instance is ever exposed beyond loopback/this stack — not the v1.5 posture. |

---

## What NOT to Use (anti-scope-creep — explicit for the roadmap)

| Avoid | Why | Use Instead |
|-------|-----|-------------|
| **A new vector DB or a new embeddings model for web content** | OWUI web search reuses the existing `villa-embed`/Qdrant RAG pipeline; a second store/model is pure duplication | The shipped v1.3 villa-embed (768-dim) + villa-qdrant, untouched |
| **Rebuilding / forking OWUI's web-search handler in Go** | Integrate-first constraint; OWUI owns search→fetch→chunk→embed→retrieve→cite | Wire OWUI's native search via env; add only the villa-owned sanitize/fence/classify transform hop |
| **PromptGuard / any DeBERTa classifier on `villa-embed` llama-server** | `llama-server` does NOT run DeBERTa `*ForSequenceClassification` heads (generative + embedding + cls-rank rerankers only) — it would need a NEW Python sidecar | Pure-Go heuristic classifier in v1.5; defer PromptGuard sidecar behind a must-WIN eval |
| **A headless browser web loader (Playwright) for full-page fetch** | Pulls a heavy new container + CGO-adjacent runtime, large memory footprint, contradicts the single-static-binary minimalism; most pages need only readable text | OWUI's default HTTP loader (`WEB_LOADER_ENGINE` default) + villa bluemonday strip / optional go-readability |
| **`searxng/searxng:latest` (unpinned)** | Rolling — a release per commit; reproducibility + supply-chain risk | Pin the resolved `@sha256:` digest of a chosen date-tag, frozen in orchestrate like every other image |
| **Leaving OWUI web-search settings as PersistentConfig** | They bake into `webui.db` on first boot and shadow env → config drift, violates config-is-source-of-truth | `ENABLE_PERSISTENT_CONFIG=False` covering all web-search vars (v1.3 D-09 carryover) |
| **A SearXNG host-published port** | Breaks loopback-only/egress-bounded posture | Container-DNS only on `villa.network` (`http://villa-searxng:8080`), no host port |
| **Reranker / hybrid search (RAG-Q-01)** | Already deferred from v1.3; a third resident model on a constrained envelope | Out of v1.5 |

---

## Stack Patterns by Variant

**If the operator enables web search (opt-in, default-OFF):**
- Render `villa-searxng.container` + `settings.yml` config volume on `villa.network`; add the OWUI web-search env block; surface bounded outbound in `status`/`doctor`.
- Because: this is the v1.4 coding-agent addon pattern — off-render stays byte-identical to v1.4/v1.3, on-render adds exactly one container + an env delta.

**If web search is OFF (the default, the zero-outbound install):**
- No SearXNG unit, OWUI env identical to v1.4, `status.search` omitted (omitempty).
- Because: "zero-outbound install stays byte-identical" is a hard milestone bar; the golden for the OFF path must match v1.4.

**If injection-defense needs to escalate beyond heuristics later:**
- Add a generative LLM-as-judge stage over the resident chat model (zero new container) before considering a PromptGuard DeBERTa sidecar (new container, behind an eval).
- Because: minimize new runtimes; the provenance-fence is the real guarantee.

---

## Version Compatibility

| Package A | Compatible With | Notes |
|-----------|-----------------|-------|
| `searxng/searxng@2026.6.17` | OWUI `v0.9.6` native searxng engine | Requires `search.formats: [html, json]` in settings.yml or OWUI gets 403 Forbidden |
| OWUI `v0.9.6` | existing `villa-embed` 768-dim / Qdrant | Web search reuses the configured RAG embedding backend; no version bump needed |
| `bluemonday v1.0.27` | Go 1.26.2, `CGO_ENABLED=0` | Pure Go over `x/net/html`; single-static-binary safe |
| `go-shiori/go-readability` + `goquery v1.10` | Go 1.26.2, CGO-free | goquery is the shared parsing base |

---

## Sources

- [Docker Hub — searxng/searxng tags](https://hub.docker.com/r/searxng/searxng/tags) — current date-tag (`2026.6.x`, rolling-per-commit), ~92 MB image — HIGH
- [SearXNG Docs — Docker installation](https://docs.searxng.org/admin/installation-docker.html) — settings.yml, secret_key, formats, limiter — HIGH
- [SearXNG.org](https://searxng.org/) — current release `2026.6.17+4dfdc822c` — HIGH
- [Open WebUI Docs — SearXNG provider](https://docs.openwebui.com/features/chat-conversations/web-search/providers/searxng/) — `ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL`, `&format=json`, JSON-format requirement — HIGH
- [Open WebUI Docs — Web Search troubleshooting](https://docs.openwebui.com/troubleshooting/web-search/) — `WEB_LOADER_ENGINE`, `WEB_LOADER_CONCURRENT_REQUESTS`, `WEB_SEARCH_TRUST_ENV`, `WEB_SEARCH_CONCURRENT_REQUESTS`, `USER_AGENT` — HIGH
- [Open WebUI Docs — env-configuration reference](https://docs.openwebui.com/reference/env-configuration/) — ConfigVar/PersistentConfig precedence (env vs webui.db) — HIGH
- [Open WebUI Releases](https://github.com/open-webui/open-webui/releases) — v0.9.6 current (2026-06-02); "Bypass Embedding & Retrieval" rename/behavior — HIGH
- [GitHub — open-webui discussion #11016](https://github.com/open-webui/open-webui/discussions/11016) — SearXNG + bypass-embedding interaction / JSON 403 — MEDIUM
- [microcosm-cc/bluemonday](https://github.com/microcosm-cc/bluemonday) + [pkg.go.dev](https://pkg.go.dev/github.com/microcosm-cc/bluemonday) — pure-Go sanitizer over x/net/html, StrictPolicy — HIGH
- [meta-llama/Llama-Prompt-Guard-2-86M](https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-86M) + [22M](https://huggingface.co/meta-llama/Llama-Prompt-Guard-2-22M) — DeBERTa-based injection classifier (NOT GGUF-generative) — HIGH
- [llama.cpp server README](https://github.com/ggml-org/llama.cpp/blob/master/tools/server/README.md) + [reranker gist](https://gist.github.com/VooDisss/42bce4eb5c76d3c325633886c5e348ee) — llama-server supports generative/embedding/cls-rank rerankers, not arbitrary DeBERTa classifier heads — MEDIUM

---
*Stack research for: opt-in guarded web search on a strictly-local Go/Podman/llama.cpp/OWUI stack (VillaStraylight v1.5)*
*Researched: 2026-06-18*
