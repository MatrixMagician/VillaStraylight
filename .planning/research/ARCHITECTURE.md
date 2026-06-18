# Architecture Research — v1.5 Web Search (Grounded & Guarded)

**Domain:** Local AI server stack — integrating opt-in, guarded web search into an existing Go control plane (Podman Quadlet orchestration + Open WebUI + v1.3 villa-embed/Qdrant RAG)
**Researched:** 2026-06-18
**Confidence:** HIGH (OWUI integration seams + external-loader contract verified against released source; v1.3/v1.4 patterns verified against this repo)

> This is **integration research**, not greenfield. It answers: *how do the v1.5 web-search features slot into the existing architecture without breaking the project's load-bearing disciplines* (orchestrate is the only impure module; config is the single source of truth; backend literals seam-locked; one byte-frozen `status.Report` bump landing last; negative-control-first egress proof). Every recommendation maps to a **real existing module**.

---

## Executive Finding (the crux — guard-layer integration point)

**OWUI owns the entire fetch→load→embed→inject pipeline internally.** When `WEB_SEARCH_ENGINE=searxng`, OWUI: (1) generates a search query, (2) calls `SEARXNG_QUERY_URL` for result URLs, (3) **fetches each result page itself** via its configured `WEB_LOADER_ENGINE`, (4) splits/embeds the page text (or bypasses embedding and attaches raw), (5) injects the result into the model context. Villa does **not** sit in this fetch path by default — so a naive "villa sanitizes the fetch" plan is architecturally dishonest.

**The honest seam exists and is released:** `WEB_LOADER_ENGINE=external` makes OWUI delegate page fetching to an HTTP service via `EXTERNAL_WEB_LOADER_URL`. Verified against `open-webui/open-webui` source — `retrieval/web/utils.py::get_web_loader` and `retrieval/loaders/external_web.py::ExternalWebLoader`:

```
OWUI → POST {EXTERNAL_WEB_LOADER_URL}   # batches of ≤20 URLs
       Authorization: Bearer {EXTERNAL_WEB_LOADER_API_KEY}
       body: { "urls": ["https://…", …] }
     ← 200 [ { "page_content": "<text>", "metadata": {…} }, … ]
```

**Recommendation (guard layer): option (b) — a villa-managed sanitizing fetch-loader container is the architecturally honest integration point.** Set `WEB_LOADER_ENGINE=external` and point `EXTERNAL_WEB_LOADER_URL` at a villa-owned `villa-websafe` service on `villa.network`. That service IS the fetch path: it pulls each URL, strips active markup, classifies for injection, wraps the surviving text in provenance fences, and returns `page_content` to OWUI. OWUI then embeds the *already-sanitized, already-fenced* text through its normal villa-embed/Qdrant RAG path. This gives villa **real control over sanitize+fence+classify without rebuilding OWUI search** — villa controls the only place untrusted bytes become `page_content`. Options (a) "proxy/intercept the SearXNG response" and (c) "guard pass on indexed content" are rejected below (§Guard-Layer Decision).

---

## Standard Architecture (v1.5 integrated)

### System Overview

```
┌──────────────────────────────────────────────────────────────────────┐
│  Command tier  cmd/villa/*.go  (thin cobra; live*Deps wiring)          │
│   install --web-search · verify search · status · doctor · dashboard   │
├──────────────────────────────────────────────────────────────────────┤
│  Pure cores  internal/*  (no host I/O; injected Deps)                   │
│  detect  recommend  preflight  status  verify(search)  websafe  config  │
├──────────────────────────────────────────────────────────────────────┤
│  orchestrate  (the ONLY impure module — renders Quadlet, drives systemd)│
│   render: searxng.container/.volume · websafe.container · OWUI env block │
└──────────────────────────────────────────────────────────────────────┘
                                  │ regenerated from config.toml
                                  ▼
┌──────────────────────────────────────────────────────────────────────┐
│                 villa.network  (rootless Podman, container-DNS only)   │
│                                                                        │
│   ┌──────────────┐   native web search    ┌──────────────────────┐    │
│   │villa-openwebui│ ─ SEARXNG_QUERY_URL ──▶│  villa-searxng        │───┼──▶ upstream
│   │  (chat + RAG) │                        │ (metasearch engines)  │   │   engines
│   │               │ ─ EXTERNAL_WEB_LOADER ▶│  villa-websafe        │───┼──▶ result
│   │  embeds via ─▶│   (POST {urls})        │  GUARD: fetch+strip+   │   │   sites
│   │  villa-embed  │ ◀─ page_content ───────│  classify+fence        │   │
│   └──────┬────────┘                        └──────────────────────┘    │
│          │ /v1/embeddings (768-dim)                                     │
│          ▼                                                              │
│   ┌──────────────┐        ┌──────────────┐        (v1.3 stack, reused)  │
│   │ villa-embed  │───────▶│  villa-qdrant │  ← web pages land in OWUI    │
│   │ (nomic-embed)│        │ (vector store)│    Knowledge collections     │
│   └──────────────┘        └──────────────┘                              │
└──────────────────────────────────────────────────────────────────────┘
        outbound (opt-in, egress-bounded, honestly surfaced):
        villa-searxng → search engines · villa-websafe → result sites
```

### Component Responsibilities

| Component | New / Modified | Responsibility | Existing pattern it follows |
|-----------|----------------|----------------|-----------------------------|
| `villa-searxng` (container) | **NEW** | Metasearch — turns a query into result URLs; JSON format enabled | v1.3 managed-service render (`orchestrate/memory.go` → `qdrant.container.tmpl`/`.volume.tmpl`); digest-pinned, seam-locked image const, on `villa.network`, container-DNS only |
| `villa-websafe` (container) | **NEW** | **The guard layer + fetch path.** Receives `{urls}` from OWUI, fetches, strips active markup, runs the injection classifier, wraps in provenance fences, returns `page_content` | New managed service rendered the same way; image/marker consts seam-locked in orchestrate; **the only first-party component that touches result sites** |
| OWUI env block | **MODIFIED** | Add `ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL`, `WEB_LOADER_ENGINE=external`, `EXTERNAL_WEB_LOADER_URL`, result-count, domain filter — env-only, ordered `envPair` | v1.3 `buildOpenWebUIView` env-only wiring; `ENABLE_PERSISTENT_CONFIG=False` discipline preserved |
| `internal/config` | **MODIFIED** | Append web-search fields (opt-in toggle + addrs/ports/knobs) | `MemoryEnabled`/`AgentEnabled` toggle precedent; container-DNS `*Addr`/`*Port` fields |
| `internal/recommend` | **MODIFIED (light)** | SearXNG + websafe have negligible model footprint; **no new resident model** (reuses villa-embed). Optionally surface "web search enabled"; fit math largely untouched | reuses existing embed reservation; no new envelope claim |
| `internal/preflight` | **MODIFIED** | Add web-search checks (disk for SearXNG volume; outbound-reachability is a WARN, not BLOCK — opt-in feature) | reusable BLOCK/WARN gate; typed-Unknown → WARN |
| `internal/websafe` | **NEW (pure core)** | strip + classify + fence over a page string; network fetch injected as a `Deps` func | pure-core + injectable-seam (like `status`/`backendswap`) |
| `internal/verify` (`villa verify search`) | **NEW verb, reused pattern** | Negative-control-first egress + guard proof: prove the guard strips/fences a planted injection; prove outbound is bounded to searxng+websafe | **clones v1.4 `villa verify agent`** four-layer seam (pure eval core + live Deps + nft egress block + negative control) |
| `internal/status` + dashboard | **MODIFIED (LANDS LAST)** | `status.Report` schema **4→5**, append-only `web_search` block; hidden-until-data dashboard Web Search panel | v1.4 SURF precedent: one golden re-freeze, append-only, schema bump, panel inherits the report verbatim |

---

## Integration Points (against REAL existing modules)

### (1) SearXNG — slots into the v1.3 managed-service rendering path **exactly**

**Answer: YES.** SearXNG is rendered the same way as `villa-qdrant`/`villa-embed`. The repo's pattern (verified in `internal/orchestrate/memory.go` + `quadlet/qdrant.container.tmpl` + `qdrant.volume.tmpl`) is:

1. A seam-locked image constant + accessor: `func SearxngImage() string { return searxngImage }` (digest-pinned `docker.io/searxng/searxng@sha256:…`), plus `SearxngContainerUnitName()`/`SearxngVolumeName()` accessors — mirroring `QdrantImage()`/`QdrantContainerUnitName()`/`QdrantVolumeName()`. **The image literal must live in `internal/orchestrate`** so `TestSeamGrepGate` (walks `internal/` + `cmd/villa`) stays green.
2. A `buildSearxngView(...)` pure builder + `searxng.container.tmpl` / `searxng.volume.tmpl` (`go:embed`). The container joins `villa.network`, **publishes no host port** (PRIV-01: container-DNS only, like Qdrant), durable named volume for `settings.yml`.
3. SearXNG config (`settings.yml`) must enable JSON output (`search.formats: [html, json]`) and a generated `secret_key` (`openssl rand -hex 32` → rendered into config, **never hand-edited**; config is the single source of truth). Rendered into the durable volume at install, regenerated from `config.toml`.
4. Byte-identical-when-off: a `TestRenderByteIdenticalWhenWebSearchOff` golden mirrors v1.3's `TestRenderByteIdenticalWhenMemoryOff` — web-search-off render must equal the v1.4 baseline byte-for-byte.

**New `config.toml` fields** (append-only; use `omitempty` for strings/bools and `omitzero` for ints with a meaningful 0 — per the BurntSushi caveat noted in `villaconfig.go`):

| Field | toml | Default | Purpose |
|-------|------|---------|---------|
| `WebSearchEnabled` | `web_search_enabled,omitempty` | `false` | The opt-in gate (mirrors `MemoryEnabled`/`AgentEnabled`). Off = byte-identical render. |
| `SearxngAddr` | `searxng_addr,omitempty` | `villa-searxng` | container-DNS name on villa.network (no host port) |
| `SearxngPort` | `searxng_port,omitzero` | `8080` | in-network SearXNG port |
| `WebSafeAddr` | `websafe_addr,omitempty` | `villa-websafe` | container-DNS name of the guard/loader service |
| `WebSafePort` | `websafe_port,omitzero` | `8181` | in-network loader port (`EXTERNAL_WEB_LOADER_URL` target) |
| `WebSearchResultCount` | `web_search_result_count,omitzero` | `3` | → OWUI `WEB_SEARCH_RESULT_COUNT` |
| `WebSearchFullPageFetch` | `web_search_full_page_fetch,omitempty` | `false` | snippet-only vs full-page fetch+embed (controls whether the loader path is exercised / `BYPASS_…` env) |
| `WebSearchEgressAllowlist` | `web_search_egress_allowlist,omitempty` | `[]` | optional domain allowlist → OWUI `WEB_SEARCH_DOMAIN_FILTER_LIST` and re-enforced in the guard |

> The image **digest** is NOT a config field — image pins live as seam-locked constants in `internal/orchestrate` (the v1.3/v1.4 norm), so a hand-edited config can never request an unpinned image.

### (2) OWUI native-search wiring — env-only behind the orchestrate seam (like v1.3 Memory)

**Answer: YES, identical discipline.** This extends `buildOpenWebUIView` (`internal/orchestrate/openwebui.go`), which already assembles an **ordered** `[]envPair` and enforces `ENABLE_PERSISTENT_CONFIG=False` (D-03). When `WebSearchEnabled`, append these ordered entries (all verified to exist in released OWUI `config.py`):

```
ENABLE_WEB_SEARCH=True
WEB_SEARCH_ENGINE=searxng
SEARXNG_QUERY_URL=http://{SearxngAddr}:{SearxngPort}/search?q=<query>&format=json
WEB_SEARCH_RESULT_COUNT={WebSearchResultCount}
WEB_SEARCH_DOMAIN_FILTER_LIST={egress allowlist}     # if set
WEB_LOADER_ENGINE=external                            # ← delegates fetch to villa
EXTERNAL_WEB_LOADER_URL=http://{WebSafeAddr}:{WebSafePort}/load
EXTERNAL_WEB_LOADER_API_KEY={rendered shared secret}
BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL={!WebSearchFullPageFetch}  # snippet path may bypass embed
```

Discipline preserved:
- **`ENABLE_PERSISTENT_CONFIG=False` stays mandatory** — the v1.3 rationale (PersistentConfig bakes env into `webui.db` on first boot then ignores env) applies identically; config must remain the single source of truth.
- **Off-render byte-identical to v1.4**: when `WebSearchEnabled=false`, none of these entries render; the env block equals the v1.4 baseline (frozen by extending the existing memory/OWUI golden test).
- **Embedding reuses villa-embed/Qdrant verbatim** — `RAG_EMBEDDING_*` already points at `villa-embed`; web pages flow through the *same* 768-dim path. No new embedding plumbing (the milestone's explicit reuse goal).
- The `<query>` placeholder and `format=json` are required by OWUI's SearXNG provider; `format=json` must also be enabled in SearXNG's `settings.yml`.

### (3) The villa-owned injection guard layer — WHERE it lives (the hard problem, honestly analyzed)

**Problem restated honestly:** OWUI's native web search fetches result pages *inside the OWUI container*. By default villa is **not** in the fetch path, so villa cannot sanitize/fence/classify what reaches the model — unless it changes *which component does the fetch*.

**Three options analyzed:**

| Option | What it means | Verdict |
|--------|---------------|---------|
| **(a)** villa proxies/intercepts the SearXNG response | Sit between OWUI and SearXNG, or rewrite SearXNG output | **Rejected.** SearXNG returns *result metadata (URLs/snippets)*, not page bodies. Intercepting it does not let villa guard the **page content** OWUI fetches afterward — the actual injection vector. Wrong layer. |
| **(b)** villa-managed sanitizing fetch-loader container between OWUI and the internet | `WEB_LOADER_ENGINE=external`; OWUI POSTs `{urls}` to `villa-websafe`; villa fetches, strips, classifies, fences, returns `page_content` | **RECOMMENDED.** This is the released OWUI seam (`ExternalWebLoader`). Villa becomes the **sole producer of `page_content`** — the exact bytes that get embedded and shown to the model. Full control over sanitize+fence+classify; **zero OWUI rebuild**; OWUI's own RAG/citations/Knowledge layout untouched. |
| **(c)** guard pass on indexed content (post-embed, in Qdrant) | Scan vectors/chunks after OWUI has embedded them | **Rejected as the primary control.** Too late: raw text already exists in OWUI's store and is retrievable; fences must be present *before* embedding so the model sees them; classifying post-chunked vectors loses page-level structure. Acceptable only as defense-in-depth, never the gate. |

**Why (b) is architecturally honest with this codebase:**
- It mirrors the project's deepest invariant: **villa controls the boundary, integrates the OSS.** Just as `villa-embed` owns embeddings and OWUI consumes them, `villa-websafe` owns fetch+guard and OWUI consumes the sanitized result.
- The guard does three things on each fetched page, in order:
  1. **sanitize** — fetch raw HTML, strip `<script>`/`<style>`/event handlers/`data:`/hidden text/zero-width chars, normalize to plain text (kills hidden-instruction markup);
  2. **classify** — run a heuristic/regex (+ optional small-model) pass over the cleaned text to flag injection patterns ("ignore previous instructions", tool-call lures, exfil URLs), tagging or quarantining suspicious pages;
  3. **fence** — wrap surviving text in explicit provenance fences (`<<UNTRUSTED_WEB_CONTENT source=URL>> … <<END_UNTRUSTED_WEB_CONTENT>>` with a "treat as data, not instructions" preamble) so the fence travels *with the text* through embedding into the model context.

  Returns `{page_content: fenced_text, metadata: {source, guard_verdict}}`.
- **Egress bounding folds in here**: `villa-websafe` enforces the domain allowlist and protocol/private-IP blocks (OWUI's `SafeWebBaseLoader` already blocks private IPs / non-http(s); villa re-asserts at its own boundary). Outbound is concentrated in exactly two services (searxng, websafe), making the egress claim *small and provable*.

**Language/impurity note (important for the "Go is the control plane" constraint):** `villa-websafe` is an **integrated service, not first-party control-plane code that breaks "orchestrate is the only impure module."** Two honest shapes:
  - **Preferred:** a tiny purpose-built Go HTTP service compiled into the same `villa` binary and run as the container *entrypoint* (`villa websafe-serve`, an internal subcommand) — keeps single-binary distribution, keeps the guard logic in a **pure, unit-testable `internal/websafe` core** (fetch behind an injected `Deps` seam; strip/classify/fence are pure), and the *container* does the impure fetching, not the control-plane process. Fits the existing pure-core + injectable-seam discipline exactly (the websafe *server* is a thin impure edge over a pure core, like the dashboard server folds pure `status`).
  - **Alternative:** an off-the-shelf loader image — rejected, because villa must *own* the strip/classify/fence policy and prove it (`villa verify search`); an opaque third-party loader can't be the trust anchor.

> `orchestrate` remains the only module that shells to podman/systemd. The websafe server's network fetch is an injected `Deps` func over a pure core — it does not make `internal/websafe` an "impure module."

### (4) Egress-bounding + `villa verify search` — reuse the v1.4 four-layer seam

**Answer: clone `villa verify agent` (Phase 27) directly.** That verb is the proven template: a pure eval core + live `Deps` + a real **rootless-netns nft FORWARD egress block** + **negative-control-first** (the egress-open run MUST FAIL exit 1 before any PASS is trusted). For `villa verify search`:

- **Negative control first (gate is real):** prove the guard is live — e.g. an injection probe that *should* be stripped/flagged; if it survives unfenced, FAIL. A vacuous green is forbidden (the v1.3/v1.4 lesson: install-time green ≠ runtime safe).
- **Guard proof:** plant a known indirect-injection page; drive a real search→fetch→guard round-trip; assert the returned `page_content` is **stripped + fenced** and the classifier flagged it. The fence/verdict must be observable in the model-facing content.
- **Egress-bound proof:** under a scoped nft block that permits *only* searxng + websafe egress, prove search still works (PASS) and that nothing else (OWUI, llama, qdrant, embed) reaches the internet. An ineffective block must be **REJECTED, not fabricated-PASS** (the Phase-27 correctness bar — an ineffective host-main-netns block was correctly rejected there).
- Outbound is honestly surfaced in `status`/`doctor` — it is NOT "zero data leaving the box" anymore; it is *bounded, opt-in, surfaced* outbound (per PROJECT.md's reconciled tension).

This reuses the netns/nft scaffolding Phase 27 built; the novelty is the *guard* assertions layered on top.

### (5) Surfacing — single `status.Report` 4→5 bump, dashboard panel LANDS LAST

**Answer: one append-only bump, last phase, one golden re-freeze** — the v1.2/v1.3/v1.4 invariant (each milestone evolves `status.Report` exactly once):
- `status.Report` schema **4→5**: append a `web_search` block (`enabled`, searxng/websafe in-network health rows, guard verdict counters, last-query freshness, outbound-bounded indicator). `recommend` stays put; `doctor` owns its own schema (bump doctor independently if it folds web-search checks, like the v1.4 doctor 1→2).
- **Hidden-until-data dashboard Web Search panel** inherits the `web_search` block **verbatim** (XSS-safe render, the v1.3/v1.4 panel pattern) — surfacing reads the frozen contract, never re-derives.
- Land it **in the final phase**, after fit/orchestrate, guard, and verify are all done — so the schema freezes a finished feature set (the explicit staggered-contract-risk discipline).

---

## Recommended Phase Build Order (dependencies honored)

Ordering respects **fit/orchestrate first → guard layer → verify → surfacing last**, with one `status.Report` bump and seam-locked literals throughout.

```
P-A  Fit + Orchestrate (SearXNG + websafe units, OWUI env wiring, config fields)
        │  config.toml fields; searxng/websafe image consts (seam-locked);
        │  *.container/*.volume tmpls; buildSearxngView/buildWebSafeView;
        │  OWUI env block extension (ENABLE_PERSISTENT_CONFIG=False preserved);
        │  byte-identical-when-off golden. NO surfacing yet.
        ▼
P-B  Guard Layer (internal/websafe pure core + villa-websafe service)
        │  strip + classify + fence pure core (Deps-injected fetch);
        │  ExternalWebLoader contract impl (POST {urls} → [{page_content,metadata}]);
        │  egress allowlist + private-IP/protocol re-assertion at the boundary.
        │  DEPENDS ON P-A (the unit + EXTERNAL_WEB_LOADER_URL wiring must exist).
        ▼
P-C  Verify (villa verify search — clone verify-agent four-layer seam)
        │  negative-control-first; planted-injection guard proof;
        │  nft-bounded egress proof (searxng+websafe only); reject ineffective block.
        │  DEPENDS ON P-A+P-B (proves the rendered+guarded stack end-to-end).
        ▼
P-D  Surfacing (LANDS LAST — status.Report 4→5 + dashboard panel)
        │  one golden re-freeze; hidden-until-data Web Search panel;
        │  doctor web-search checks on doctor's own schema if added.
        │  DEPENDS ON A+B+C (freezes a finished, proven feature set).
```

**Why this order:**
- **P-A before P-B:** the guard service is wired via `EXTERNAL_WEB_LOADER_URL` — that env + the unit must render first. P-A is also the only phase (other than P-D's status golden) that touches the orchestrate render goldens.
- **P-B before P-C:** you can't prove a guard that doesn't exist; verify asserts on the guard's stripped+fenced output.
- **P-D last, always:** the byte-frozen `status.Report` must bump over a *finished* feature set (staggered-contract-risk discipline). Surfacing reads, never derives.
- **Seam-lock throughout:** the searxng/websafe image digests are constants in `internal/orchestrate`; `TestSeamGrepGate` fails the build on any leaked image/marker literal — so P-A must place them correctly from the start.

---

## Anti-Patterns (specific to this integration)

### Anti-Pattern 1: Letting OWUI fetch with its built-in loader and "guarding later"
**What people do:** Leave `WEB_LOADER_ENGINE` default (`safe_web`/`playwright`), then try to sanitize in Qdrant or via a prompt instruction.
**Why it's wrong:** The raw, un-fenced page is already embedded and retrievable; the model sees unfenced untrusted text. The injection has already entered the trust boundary.
**Do this instead:** `WEB_LOADER_ENGINE=external` → `villa-websafe`. Villa is the sole producer of `page_content`; fences exist *before* embedding.

### Anti-Pattern 2: Putting the SearXNG/websafe image digest in `config.toml`
**What people do:** Make the image pin a config field "for flexibility."
**Why it's wrong:** Breaks "config is configuration truth, image pins are seam-locked constants" — a hand-edited config could request an unpinned/malicious image, and `TestSeamGrepGate` would not guard a config string.
**Do this instead:** Digest constant in `internal/orchestrate` (like `QdrantImage()`); config carries only addr/port/toggles.

### Anti-Pattern 3: Trusting an install-time egress check (vacuous green)
**What people do:** Assert "no outbound" once at install and call it proven.
**Why it's wrong:** Web search *necessarily* makes outbound calls at *runtime*; an install-time check is meaningless. The v1.3/v1.4 lesson.
**Do this instead:** `villa verify search` at runtime, negative-control-first, under a real nft block — prove the egress is *bounded* (searxng+websafe only), not *absent*.

### Anti-Pattern 4: Rebuilding OWUI's search/RAG to inject the guard
**What people do:** Fork OWUI to intercept its fetch.
**Why it's wrong:** Violates "integrate-not-rebuild"; OWUI owns chunk/embed/retrieve/citations/Knowledge layout (the v1.3 recall decision). A fork rots.
**Do this instead:** Use the released `external` loader seam. Villa controls fetch; OWUI keeps owning RAG.

### Anti-Pattern 5: Auto-enabling web search / widening egress silently
**What people do:** Turn search on by default or open egress broadly.
**Why it's wrong:** Breaks the opt-in/default-off posture and surfaced-outbound honesty (mirrors "ROCm never auto-switches", "coding mode never auto-flips").
**Do this instead:** Explicit `villa install --web-search` (or a `villa web-search enable` verb), default-off, byte-identical when off, outbound surfaced in status/doctor.

---

## Integration Points (summary tables)

### External Services

| Service | Integration Pattern | Notes / gotchas |
|---------|---------------------|-----------------|
| SearXNG | Managed Quadlet on villa.network; `SEARXNG_QUERY_URL=http://villa-searxng:8080/search?q=<query>&format=json` | MUST enable `search.formats: [html, json]` and a generated `secret_key` in `settings.yml`; container-DNS only, no host port |
| OWUI native web search | env-only behind `buildOpenWebUIView`; `WEB_SEARCH_ENGINE=searxng` + `WEB_LOADER_ENGINE=external` | `ENABLE_PERSISTENT_CONFIG=False` mandatory; off-render byte-identical |
| villa-websafe (guard/loader) | `EXTERNAL_WEB_LOADER_URL=http://villa-websafe:8181/load`; OWUI POSTs `{urls}` (≤20/batch, Bearer), expects `[{page_content,metadata}]` | This IS the fetch path + guard; the only first-party component touching result sites |
| Upstream search engines / result sites | reached *only* via villa-searxng / villa-websafe | the entire (opt-in, bounded) outbound surface — proven by `villa verify search` |

### Internal Boundaries

| Boundary | Communication | Notes |
|----------|---------------|-------|
| `internal/websafe` ↔ network fetch | injected `Deps` func (pure core, impure edge) | strip/classify/fence are pure + unit-testable off-hardware |
| `orchestrate` ↔ searxng/websafe units | render Quadlet from config (pure) + WriteUnits/systemd (impure edge) | orchestrate stays the ONLY impure module |
| `status` ↔ dashboard panel | frozen `status.Report.web_search` (schema 5) | append-only; panel reads, never derives |
| `verify(search)` ↔ host | live `Deps` + nft/netns (cloned from verify-agent) | negative-control-first |

---

## Confidence & Gaps

| Area | Confidence | Basis |
|------|------------|-------|
| OWUI external-loader seam & contract | **HIGH** | Verified against released `open-webui` source (`get_web_loader`, `ExternalWebLoader`, `config.py` vars) |
| SearXNG-as-managed-service fit | **HIGH** | Identical to v1.3 qdrant/embed pattern verified in this repo (`orchestrate/memory.go`, templates) |
| OWUI env-only wiring discipline | **HIGH** | Verified in repo (`buildOpenWebUIView`, `ENABLE_PERSISTENT_CONFIG=False`) + released OWUI env names |
| verify-search seam reuse | **HIGH** | v1.4 `villa verify agent` four-layer seam is the proven template |
| Guard classifier *efficacy* (false-pos/neg of injection detection) | **MEDIUM** | Strip+fence is robust; the *classifier* heuristic quality needs phase-level eval (flag P-B/P-C for a small injection benchmark) |
| `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` exact behavior per OWUI version | **MEDIUM** | Confirmed the var exists; snippet-vs-full-page behavior should be pinned to the exact OWUI `@sha256` villa ships |

**Gaps to flag for phase research:**
- The injection **classifier** (P-B) deserves a small pre-declared injection-detection eval (precision/recall on planted prompts) — mirror the v1.4 "must-WIN eval" discipline rather than shipping on hope.
- Confirm the OWUI digest villa pins exposes `external` loader + `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` (current pin is OWUI v0.9.6-era; verify against the exact `@sha256` before P-A).
- SearXNG bot-detection/limiter tuning (`UWSGI_WORKERS`, `botdetection`) for a single-user local instance — minor, but render sane defaults.

## Sources

- [Open WebUI — SearXNG provider](https://docs.openwebui.com/features/chat-conversations/web-search/providers/searxng/) (HIGH — config: `SEARXNG_QUERY_URL`, JSON format requirement)
- [Open WebUI — Web Search Integration (DeepWiki)](https://deepwiki.com/open-webui/open-webui/6.5-web-search-integration) (HIGH — pipeline: search→load→embed→inject, loader engines incl. `external`)
- [Open WebUI — Agentic Search & URL Fetching](https://docs.openwebui.com/features/chat-conversations/web-search/agentic-search/) (MEDIUM — fetch_url, 50k-char truncation)
- `open-webui/open-webui` source `backend/open_webui/retrieval/web/utils.py::get_web_loader` and `retrieval/loaders/external_web.py::ExternalWebLoader` (HIGH — verified external-loader request/response contract)
- `open-webui/open-webui` source `backend/open_webui/config.py` (HIGH — verified env var names: `ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE`, `SEARXNG_QUERY_URL`, `WEB_LOADER_ENGINE`, `EXTERNAL_WEB_LOADER_URL`, `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL`, `WEB_SEARCH_DOMAIN_FILTER_LIST`)
- [SearXNG — Docker installation](https://docs.searxng.org/admin/installation-docker.html) + [settings.yml](https://github.com/searxng/searxng/blob/master/searx/settings.yml) (HIGH — JSON format, secret_key, rootless container)
- [Thoughtworks — prompt fencing vs prompt injection](https://www.thoughtworks.com/en-us/insights/blog/generative-ai/how-prompt-fencing-can-tackle-prompt-injection-attacks) (MEDIUM — provenance fencing as a defense)
- [Document Injection: the prompt-injection vector inside every RAG pipeline](https://tianpan.co/blog/2026-04-15-document-injection-rag-pipeline) (MEDIUM — sanitize-before-embed, fence-as-data)
- [Indirect Prompt Injection in RAG Systems and AI Agents (AquilaX)](https://aquilax.ai/blog/indirect-prompt-injection-rag-agents) (MEDIUM — defense-in-depth: provenance + isolation + validation)
- This repo: `internal/orchestrate/memory.go`, `openwebui.go`, `quadlet/*.tmpl`, `internal/config/villaconfig.go`, PROJECT.md, CLAUDE.md (HIGH — existing managed-service + env-wiring + config patterns)

---
*Architecture research for: VillaStraylight v1.5 Web Search (Grounded & Guarded) — integration with existing Go control plane*
*Researched: 2026-06-18*
