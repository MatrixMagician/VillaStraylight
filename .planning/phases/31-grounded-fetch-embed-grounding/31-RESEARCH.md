# Phase 31: Grounded Fetch → Embed Grounding - Research

**Researched:** 2026-06-19
**Domain:** OWUI external web-loader contract · containerized Go HTTP service · Go SSRF guard · ephemeral Qdrant collection lifecycle · `recommend` ctx-reservation math
**Confidence:** HIGH for the OWUI external-loader contract (verified against source at the pinned commit), the Go SSRF pattern (stdlib + canonical references), and all reuse patterns (read from the live codebase). MEDIUM for the ephemeral-collection lifecycle (the embed→retrieve breakage is empirically proven in Phase 30 but the clean-fix mechanics need on-hardware confirmation). LOW for exact ctx-budget byte math (unmeasured — reserve conservatively, on-hardware tune deferred).

## Summary

Phase 31 inserts a villa-owned HTTP loader as Open WebUI's **`WEB_LOADER_ENGINE=external`** fetch path so every byte of `page_content` embedded or shown to the model passes through villa code (GUARD-01). The OWUI external-loader contract is now **verified at the pinned digest** (`ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…`, git commit `02dc3e6`): OWUI POSTs `{"urls": [<result urls>]}` with `Authorization: Bearer <EXTERNAL_WEB_LOADER_API_KEY>` and expects back a JSON array `[{"page_content": str, "metadata": {…}}]`. That is the contract villa's `villa websafe-serve` HTTP service must implement.

The single hardest design tension the planner must resolve up-front: **Phase 30's D-06 set `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True`** to work around a real OWUI v0.9.6 retrieval bug (web results are embedded into the `web-search-<hash>` collection but never queried back at chat time — confirmed by OWUI issue #25585 and by on-hardware UAT). GROUND-01/02 require the **full fetch → chunk → embed → retrieve → cite** path into a *dedicated ephemeral collection*. Those two are mutually exclusive: with BYPASS=True there is no embed/retrieve and no ephemeral Qdrant collection at all (OWUI sets `collection_name=None` and direct-injects). **Phase 31 must turn BYPASS OFF** and make OWUI's native embed→retrieve path actually ground — which means the external loader provides good `page_content` AND the retrieval bug must be neutralized (the documented lever at this commit is `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True`; this is the highest-risk unknown and MUST be confirmed on-hardware before the golden is frozen).

**Primary recommendation:** Build `internal/websafe` as a pure fetch core (network injected as a `Deps` func, Phase-32 sanitize/normalize/fence/classify hooks stubbed pass-through) + a hidden `villa websafe-serve` cobra subcommand that serves the OWUI external-loader contract over a `villa-websafe` Quadlet container (host `villa` binary bind-mounted into a digest-pinned minimal base image, gated on `WebSearchEnabled`, container-DNS only, no host port). Implement the SSRF guard with `net.Dialer.Control` validating the *connected* IP (defeats DNS rebinding TOCTOU) + `http.Client.CheckRedirect` re-validating every hop + an http(s) scheme allowlist + a `net/netip` private-range prefix set. Add an append-only `WebSearchReservationBytes` field to `Recommendation`, reserved before the chat fit, gated on `WebSearchEnabled`; bump the recommend schema 3→4.

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

**Area 1 — `villa-websafe` service shape & lifecycle**
- Containerize the villa binary itself: a hidden `villa websafe-serve` subcommand run inside a Quadlet container that **bind-mounts the host `villa` binary** into a minimal, digest-pinned base image — no new build/publish pipeline; single-static-binary discipline preserved. Pin the base image digest; keep any image literal behind the `internal/orchestrate` seam, never in a caller.
- Render + start the unit **ONLY when `WebSearchEnabled`** — byte-identical-off, mirroring the Phase-29 SearXNG service gating and the `volumeView`/`searxngView` render pattern.
- Network identity: container-DNS `villa-websafe` on `villa.network`, **no host port** (PRIV-01), fixed internal port (e.g. 8090) composed from config, never a re-typed host literal in a caller (keeps `TestSeamGrepGate` green).
- OWUI wiring: set `WEB_LOADER_ENGINE=external` + the external-loader URL env into the **same `WebSearchEnabled` OWUI env block** added in Phase 30 (`buildOpenWebUIView`, append-only, golden-frozen). Researcher MUST verify exact env key names + request/response contract at the pinned digest. **[VERIFIED — see Standard Stack / OWUI External-Loader Contract]**

**Area 2 — Ephemeral grounding collection & RAG reuse**
- **Clean-replace per query**: drop + recreate the ephemeral collection before each web search so stale cross-query content never bleeds in; bounded lifetime by construction.
- **Fixed dedicated collection** (e.g. `villa_web_ephemeral`), strictly distinct from durable memory / document-KB collections (GROUND-02).
- **Reuse v1.3 RAG verbatim**: villa-embed + Qdrant + OWUI's top-level `sources` citation field — no new embedder, vector DB, or citation plumbing.
- **Reuse OWUI's native chunk/retrieve** (existing `RAG_*` settings) for fetched pages rather than a villa-specific chunker.

**Area 3 — Fetch resource bounds & SSRF guard**
- Comprehensive SSRF rejection: loopback, link-local (`169.254/16`, `fe80::/10`), all RFC1918 private (`10/8`, `172.16/12`, `192.168/16`), CGNAT (`100.64/10`), cloud-metadata `169.254.169.254`, internal `villa-*` / `.network` hosts; **resolve-then-validate each resolved IP** (not just hostname); **http(s) scheme allowlist only**.
- Redirects: follow up to a small cap (**≤5**), **re-running the full SSRF check on every hop**; reject when the cap is exceeded.
- Conservative per-fetch bounds: max page size ~**2 MB** (truncate beyond), fetch timeout ~**10 s**, fetches-per-query bounded by result-count, bounded fetch concurrency.
- Fetch-failure behavior: **skip-and-continue** — a failed URL is omitted (honest partial); if **all** fail → no injected context (honest no-results), consistent with Phase-30 D-06 honesty.

**Area 4 — Context-budget reservation (`recommend`)**
- Reserve the web-RAG injection budget (retrieval top-K × chunk size + citation overhead) **before** the chat-model fit in `recommend.Pick` (GROUND-03). Extends `memoryReservation(mem)` → `EmbeddingReservationBytes` seam.
- New append-only field on `Recommendation` (e.g. `WebSearchReservationBytes`) — the single sanctioned recommend-side contract bump. Re-freeze the recommend golden intentionally.
- Gate on `WebSearchEnabled` — off-envelope fit stays identical to v1.4.
- Offload-assert under search load — silent/partial CPU fallback is a FAIL; the assertion seam lands here, fully exercised in Phase 33/34.

### Claude's Discretion
- Exact `internal/websafe` package shape, `Deps` struct fields, and `villa websafe-serve` cobra wiring — consistent with `live*Deps` seam + pure-core conventions.
- The Phase-32 guard hooks (sanitize/normalize/fence/classify) are **stubbed pass-through** in Phase 31 so the seam exists without the policy.
- Precise default numbers (port, page-size/timeout caps, top-K, per-page token estimate) within the "conservative" intent.

### Deferred Ideas (OUT OF SCOPE)
- **Injection-guard policy** (Unicode normalization, nonced provenance fence, heuristic classifier, "reduces-and-flags" copy, markdown-image residual) → **Phase 32** (GUARD-02/03/04).
- **`villa verify search` egress proof** + opt-in/PRIV plumbing + OWUI lazy-outbound kill → **Phase 33** (PRIV-07/08/09).
- **Surfacing** — `status.Report` 4→5, dashboard panel, doctor, backup → **Phase 34** (SURF-04..07).
- On-hardware tuning of result-count × page-size ctx caps → measured in Phase 33/34 (reserve conservatively now).
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| GROUND-01 | Grounded answer with inline citations to live URLs — full-page fetch → chunk → embed → retrieve → cite, reusing v1.3 villa-embed/Qdrant RAG + OWUI `sources` verbatim. | External-loader contract verified; OWUI native chunk/embed/retrieve reuse path mapped; the **BYPASS=False + retrieval-fix** reconciliation is the load-bearing finding (Pitfall 1). |
| GROUND-02 | Fetched content embedded into a **dedicated ephemeral collection** (clean-replace / bounded lifetime), never durable memory/doc-KB. | OWUI's web-search collection naming (`web-search-<sha>` / prefixed) documented; isolation-by-construction vs the durable `open-webui_*` collections mapped; clean-replace approach (Pitfall 2). |
| GROUND-03 | `recommend` reserves web-search ctx budget before the chat fit; residency offload-asserted under search load. | `memoryReservation` seam + append-only `Recommendation` field + schema 3→4 bump pattern read from source; conservative reservation formula proposed; offload-assert seam location identified. |
| GUARD-01 | `villa-websafe` loader (`WEB_LOADER_ENGINE=external`) is the **sole producer of `page_content`**. | Exact external-loader request/response contract verified at the pinned commit; the stubbed-but-present sanitize/normalize/fence/classify seam keeps it real from Phase 31. |
| GUARD-05 | SSRF guard — resolve-and-validate target IP, re-check after every redirect, http(s) scheme allowlist. | Canonical Go `net.Dialer.Control` connect-time IP-validation pattern + `CheckRedirect` + `net/netip` prefix set verified; OWUI's own loader SSRF is BYPASSED for external loaders, so villa-side SSRF is mandatory (Pitfall 3). |
</phase_requirements>

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Page fetch + SSRF guard + resource bounds | `internal/websafe` (pure core, network injected) | `villa websafe-serve` (cmd tier wires live HTTP) | Decision logic + SSRF math must be unit-testable off-hardware; the HTTP server is a thin impure edge (the established pure-core + `live*Deps` seam). |
| `page_content` production (GUARD-01 sole producer) | `villa websafe-serve` HTTP handler | `internal/websafe` core | Every byte passes through villa; the handler implements the OWUI external-loader contract, the core does the work. |
| Quadlet `villa-websafe` unit render | `internal/orchestrate` (the only impure module) | — | Image/mount/identity literals stay behind the orchestrate seam; gated on `WebSearchEnabled`. |
| OWUI external-loader env wiring | `internal/orchestrate` (`buildOpenWebUIView`) | — | Append-only into the Phase-30 web-search env block; golden-frozen. |
| Chunk / embed / retrieve / cite | OWUI runtime + villa-embed + Qdrant (reused) | `internal/orchestrate` (env) | GROUND-01 explicitly reuses v1.3 RAG verbatim; villa adds NO new embedder/chunker. |
| Ephemeral collection lifecycle | OWUI runtime (owns the `web-search-*` collection) | villa (config / verify) | OWUI creates+overwrites its own per-query web-search collection; villa's job is isolation + clean-replace config, not re-implementing vector storage. |
| Ctx-budget reservation | `internal/recommend` (pure `Pick`) | cmd tier (threads `WebSearchInputs`) | Reservation-before-fit is a pure-core invariant (mirrors `MemoryInputs`). |
| Offload assert under search load | `internal/inference` residency seam | `internal/status` (Phase 33/34) | Silent CPU fallback = FAIL; the marker seam already exists; Phase 31 lands the seam, Phase 33/34 exercise it. |

## Standard Stack

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| Go stdlib `net/http` | go1.26.2 | The `villa websafe-serve` HTTP server + the outbound fetch client | [VERIFIED: codebase] Single-static-binary discipline; chi is already a dep but the websafe server is one POST route — stdlib `http.ServeMux` is sufficient and avoids new surface. Match the existing dashboard chi pattern only if middleware is wanted. |
| Go stdlib `net` (`net.Dialer.Control`) | go1.26.2 | Connect-time IP validation (SSRF, defeats DNS-rebinding TOCTOU) | [VERIFIED: agwa.name, doyensec safeurl] The canonical Go SSRF mechanism — `Control` runs after DNS resolution, before connect, on the *actual* IP. |
| Go stdlib `net/netip` | go1.26.2 | Private/reserved-range prefix matching (`netip.Prefix.Contains`) | [VERIFIED: codebase uses go1.26.2] Modern allocation-free IP type; `Prefix.Contains(addr)` is the clean check for the rejection set. |
| Go stdlib `crypto/rand` | go1.26.2 | Generate the `EXTERNAL_WEB_LOADER_API_KEY` bearer token (shared secret villa↔OWUI) | [VERIFIED: codebase] Mirrors `config.GenerateSearxngSecret` (Phase-29) — same 0600 EnvironmentFile discipline. |

### Supporting
| Library | Version | Purpose | When to Use |
|---------|---------|---------|-------------|
| Go stdlib `context` | go1.26.2 | Per-fetch timeout (~10s) via `http.NewRequestWithContext` | Always — the fetch timeout bound. |
| Go stdlib `io.LimitReader` | go1.26.2 | Cap response body at ~2 MB (truncate beyond) | The max-page-size bound; never read an unbounded body from an untrusted site. |
| Go stdlib `golang.org/x/sync/errgroup` OR a bounded worker pool | (stdlib goroutines + semaphore preferred) | Bounded fetch concurrency across the result URLs | Prefer a `chan struct{}` semaphore (zero new deps) over adding x/sync. |

**No new external Go packages in Phase 31.** `bluemonday` (the StrictPolicy sanitizer) is a **Phase-32** dependency (GUARD-02) — the sanitize hook is a pass-through stub here. The minimal base image is the only new digest-pinned artifact (see below).

### Base Image for `villa-websafe`
| Choice | Recommendation | Tradeoff |
|--------|----------------|----------|
| **`gcr.io/distroless/static-debian12`** (or `:nonroot`) | **Recommended default** — [CITED: distroless docs] purpose-built to run a single static binary, no shell, no package manager, smallest attack surface. CA certs included (needed for HTTPS fetches to upstreams). Digest-pinnable. | Cannot exec a shell for debugging; villa binary must be **fully static (CGO_OFF)** — verify (`make build` already produces a static binary; confirm with `file ./villa` → "statically linked"). |
| `alpine` | Acceptable fallback if a shell is wanted for debugging | musl libc; only matters if the binary is dynamically linked against glibc — it must NOT be (would crash on musl). Larger attack surface. |
| `busybox` | Avoid | No CA certs by default → HTTPS fetches fail; would need a certs layer. |

**Gotcha (static-binary + minimal base):** villa is pure Go; ensure `CGO_ENABLED=0` so the bind-mounted binary runs on distroless/scratch without glibc. The `ghw` hardware-detect dep uses cgo on some platforms but the **`websafe-serve` path does not call detect** — confirm the binary links static regardless. If detect forces cgo, the planner must either build with `CGO_ENABLED=0` (ghw degrades gracefully — it already returns typed-Unknown) or use an alpine/glibc base. **Verify on-hardware: `file ./villa`.**

**Installation:** No `go get`. One new digest-pinned base image, resolved on the dev box exactly like SearXNG/Qdrant were:
```bash
podman pull gcr.io/distroless/static-debian12:nonroot
podman image inspect gcr.io/distroless/static-debian12:nonroot --format '{{index .RepoDigests 0}}'
```
Pin the resulting `@sha256:…` as a managed-service const in `internal/orchestrate/websafe.go`, extend `seam_test.go isSeam` allowlist in the SAME commit (mirrors searxng.go / memory.go).

## Package Legitimacy Audit

> Phase 31 adds **no new Go packages** (stdlib only). The single new external artifact is a container base image (not a registry package).

| Artifact | Registry | Age | Source Repo | Verdict | Disposition |
|----------|----------|-----|-------------|---------|-------------|
| `gcr.io/distroless/static-debian12` | Google Container Registry (official `GoogleContainerTools/distroless`) | mature (>6 yrs) | github.com/GoogleContainerTools/distroless | OK | Approved — pin RepoDigest on dev box before freezing |

**Packages removed due to [SLOP] verdict:** none.
**Packages flagged as suspicious [SUS]:** none.
**[ASSUMED] packages requiring human-verify before install:** none — all stdlib + one official Google image.

## OWUI External-Loader Contract (VERIFIED at pinned commit `02dc3e6`)

> Source: `open-webui/open-webui@02dc3e6` (== the pinned digest `sha256:7f1b0a1a…ea9184e`), files `backend/open_webui/config.py`, `backend/open_webui/retrieval/loaders/external_web.py`, `backend/open_webui/retrieval/web/utils.py`, `backend/open_webui/routers/retrieval.py`.

### Env vars (exact names + defaults) [VERIFIED: config.py @02dc3e6]
| Env var | OWUI ConfigVar path | Default | Phase 31 value |
|---------|---------------------|---------|----------------|
| `WEB_LOADER_ENGINE` | `rag.web.loader.engine` | `""` | `external` |
| `EXTERNAL_WEB_LOADER_URL` | `rag.web.loader.external_web_loader_url` | `""` | `http://{villa-websafe addr}:{port}/{path}` composed from config |
| `EXTERNAL_WEB_LOADER_API_KEY` | `rag.web.loader.external_web_loader_api_key` | `""` | crypto/rand bearer secret (0600 EnvironmentFile, mirrors SearXNG secret) |
| `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` | `rag.web.search.bypass_embedding_and_retrieval` | `False` | **MUST become `False`** in Phase 31 (Phase-30 set it `True`) — see Pitfall 1 |
| `BYPASS_WEB_SEARCH_WEB_LOADER` | `rag.web.search.bypass_web_loader` | `False` | leave `False` (must NOT bypass the loader — villa IS the loader) |
| `WEB_SEARCH_CONCURRENT_REQUESTS` | `rag.web.search.concurrent_requests` | `0` | leave default unless UAT needs tuning |
| `RAG_TOP_K` | `rag.top_k` | `3` | the retrieval top-K used for the reservation math |
| `CHUNK_SIZE` | `rag.chunk_size` | `1000` | per-chunk char size used for the reservation math |
| `CHUNK_OVERLAP` | `rag.chunk_overlap` | `100` | reservation math |
| `WEB_SEARCH_RESULT_COUNT` | `rag.web.search.result_count` | `3` | already wired (Phase-30, `cfg.WebSearchResultCount`) |

All of these are **DB-backed PersistentConfig ConfigVars** — they require the existing `ENABLE_PERSISTENT_CONFIG=False` trailing gate (Phase-30 D-04) to stay env-authoritative. The new loader keys append to the **same `webSearchEnabled` block** in `buildOpenWebUIView`.

### Request villa-websafe receives [VERIFIED: external_web.py @02dc3e6]
```
POST {EXTERNAL_WEB_LOADER_URL}
Headers:
  Content-Type: application/json
  Authorization: Bearer {EXTERNAL_WEB_LOADER_API_KEY}
  User-Agent: Open WebUI External Web Loader   (approx label)
Body:
  {"urls": ["https://result1…", "https://result2…", …]}   # field name is exactly "urls"
```
`urls` is the list of SearXNG result URLs (count = `WEB_SEARCH_RESULT_COUNT`). OWUI runs `safe_validate_urls(urls)` (its own SSRF check) **before** engine selection — but the result is passed to `ExternalWebLoader` and the external loader receives URLs **without OWUI re-validating per-IP at fetch time** (the external infra is trusted to validate). **→ villa MUST do its own SSRF check; do not rely on OWUI's pre-validation (Pitfall 3, GUARD-05).**

### Response OWUI expects back [VERIFIED: external_web.py @02dc3e6]
```json
[
  {"page_content": "…extracted text…", "metadata": {"source": "https://result1…", "title": "…"}},
  {"page_content": "…", "metadata": {…}}
]
```
Per result object: `page_content` (string, → the LangChain `Document.page_content`; defaults to `""` if missing) and `metadata` (object, → `Document.metadata`; defaults to `{}`). **`metadata.source` is what flows into OWUI's top-level `sources` citation field** — villa MUST populate `metadata` with the fetched URL (and ideally title) so citations point at live URLs (GROUND-01). OWUI calls `response.raise_for_status()` — a non-2xx from villa-websafe aborts the whole loader unless `continue_on_failure` is set; villa should return **200 with a (possibly partial) array** and represent per-URL failures by *omitting* that URL from the array (skip-and-continue, matching CONTEXT Area 3).

### Downstream flow once villa returns `page_content`
1. OWUI wraps each object as a `Document`.
2. With **`BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=False`**: `save_docs_to_vector_db(docs, collection_name=web-search-<hash>)` → chunk (`CHUNK_SIZE`/`CHUNK_OVERLAP`) → embed via `RAG_OPENAI_API_BASE_URL` (= villa-embed, already wired by memory block) → store in Qdrant (= villa-qdrant) → at chat time `query_collection(top_k=RAG_TOP_K)` → inject retrieved chunks → cite via `sources`.
3. With **`BYPASS=True`** (Phase-30 state): `collection_name=None`, NO embed, NO Qdrant, content direct-injected. **This is the path Phase 31 replaces.**

## Architecture Patterns

### System Architecture Diagram
```
operator (browser :3000) — toggles "Web Search" ON (OWUI native, per-session)
   │
   ▼
villa-openwebui  ── ENABLE_WEB_SEARCH / WEB_SEARCH_ENGINE=searxng ──►  villa-searxng ──► upstream engines
   │                                                                        │ (vetted allowlist)
   │   result URLs (count = WEB_SEARCH_RESULT_COUNT)                        ▼
   │                                                                   JSON results (urls)
   │  WEB_LOADER_ENGINE=external
   │  POST {urls:[…]}  Bearer EXTERNAL_WEB_LOADER_API_KEY
   ▼   (over villa.network, container DNS, NO host port — PRIV-01)
villa-websafe  (host `villa` binary bind-mounted in distroless; `villa websafe-serve`)
   │  internal/websafe pure core:
   │   for each url (bounded concurrency):
   │     ├─ SSRF guard:  scheme allowlist (http/https only)
   │     │               + net.Dialer.Control validates CONNECTED ip (netip prefix reject-set)
   │     │               + CheckRedirect re-validates EVERY hop (≤5)
   │     │               + hostname reject (villa-*, *.network, localhost)
   │     ├─ fetch: ctx timeout ~10s, io.LimitReader ~2MB, http(s) only
   │     ├─ [Phase-32 stubs: sanitize→normalize→fence→classify]  ← pass-through in P31
   │     └─ produce page_content + metadata{source:url, title}
   │  fetch failure (timeout / SSRF-reject / non-2xx / oversize) ⇒ OMIT url (skip-and-continue)
   │  all fail ⇒ return []  (honest no-results — no fabricated context)
   ▼  200  [{page_content, metadata{source,title}}, …]
villa-openwebui  (BYPASS=False)
   ├─ save_docs_to_vector_db → collection "web-search-<hash>"  (EPHEMERAL, per-query, clean-replaced)
   ├─ chunk (CHUNK_SIZE/OVERLAP) → embed via RAG_OPENAI_API_BASE_URL=villa-embed → store in villa-qdrant
   │     (DEDICATED ephemeral collection — NEVER the durable open-webui_* memory/doc-KB collections — GROUND-02)
   ├─ query_collection(top_k=RAG_TOP_K) at chat time → inject retrieved chunks
   └─ cite via top-level `sources` (metadata.source → live URL) — GROUND-01

recommend.Pick (BEFORE this runs at install/recommend time):
   envelope -= EmbeddingReservationBytes -= WebSearchReservationBytes (gated on WebSearchEnabled)  — GROUND-03
   offload-assert under search load: silent CPU fallback = FAIL
```

### Recommended Project Structure (files created/touched)
```
internal/websafe/                       # NEW pure core (TestSeamGrepGate-clean: no image/host literals)
├── websafe.go        # Fetch core: Deps{Get func(ctx,url)→(body,status,err)} ; Loader(urls)→[]Page
├── ssrf.go           # SSRF guard: scheme allowlist, netip reject-set, Control hook, CheckRedirect, host reject
├── guard_stubs.go    # Phase-32 pass-through hooks: sanitize/normalize/fence/classify (identity funcs + doc)
└── *_test.go         # table tests: SSRF reject-set, redirect re-check, size/timeout bounds, skip-and-continue
cmd/villa/
├── websafe.go        # NEW hidden `villa websafe-serve` cobra cmd; liveWebsafeDeps wires real net.Dialer+http
└── lifecycle.go      # managedServices() gains villa-websafe.service when WebSearchEnabled
internal/orchestrate/
├── websafe.go        # NEW managed-service consts: base image digest, unit name, mount, port; buildWebsafeView
├── openwebui.go      # EXTEND buildOpenWebUIView web-search block: WEB_LOADER_ENGINE=external,
│                     #   EXTERNAL_WEB_LOADER_URL (composed), EXTERNAL_WEB_LOADER_API_KEY (env-file),
│                     #   flip BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL True→False, add retrieval-fix key
├── render.go         # EXTEND: render villa-websafe unit gated on WebSearchEnabled (after searxng branch)
├── seam_test.go      # isSeam allowlist += orchestrate/websafe.go (same commit as the image literal)
└── quadlet/
    └── websafe.container.tmpl   # NEW (mirror searxng.container.tmpl + the binary bind-mount + Exec)
internal/recommend/
└── recommend.go      # ADD WebSearchInputs + WebSearchReservationBytes (above SchemaVersion); schema 3→4
internal/config/
└── villaconfig.go    # ADD WebsafeAddr/WebsafePort + EXTERNAL_WEB_LOADER secret; omit-when-off discipline
internal/inference/
└── (residency seam)  # offload-assert-under-search-load seam stub (exercised Phase 33/34)
```

### Pattern 1: Containerize the host binary via bind-mount + hidden subcommand
**What:** The `villa-websafe` Quadlet container runs a digest-pinned distroless base whose Exec is the bind-mounted host `villa` binary with the hidden `websafe-serve` subcommand.
**When to use:** This phase (Area 1 locked decision) — preserves single-static-binary, no new build/publish pipeline.
**Example (`websafe.container.tmpl`, mirroring searxng/embed templates):**
```
# ~/.config/containers/systemd/villa-websafe.container  (GENERATED — do not edit; source: config.toml)
[Unit]
Description=VillaStraylight web-safe loader (OWUI external web loader)
After=villa-network.service

[Container]
ContainerName={{.ContainerName}}        # cfg.WebsafeAddr, e.g. villa-websafe (config-sourced, WR-01)
Image={{.Image}}                        # pinned distroless digest (managed-service const)
Network={{.Network}}                    # villa.network ; NO PublishPort (PRIV-01)
EnvironmentFile={{.SecretEnvFile}}      # 0600 file carrying EXTERNAL_WEB_LOADER_API_KEY (mirror searxng.env)
Volume={{.BinaryMount}}                 # {host villa path}:/usr/local/bin/villa:ro,z   (read-only, shared :z)
Exec={{.Exec}}                          # /usr/local/bin/villa websafe-serve --addr 0.0.0.0 --port {port}
[Service]
Restart=on-failure
[Install]
WantedBy=default.target
```
**Notes:**
- **Mount label `:z` (lowercase, shared) read-only** for the binary — it is shared with the host and other containers may read it; the read-only `ro` prevents writes. (Contrast: durable data volumes use `:Z` private. The models store uses `:ro,z` — same pattern, `internal/orchestrate/memory.go:95`.) **Verify the SELinux relabel works on the binary's host dir on-hardware.**
- **Host binary path:** resolved at render time (the path the user ran `make build` into / the installed path). This is a host path threaded through `RenderInput`/config — never shell-interpolated. The planner must decide the canonical install path (e.g. the same dir as the running binary via `os.Executable()`, captured at install time into config).
- `--host 0.0.0.0` is container-internal only (no host bind), exactly like `buildEmbedExec` (`memory.go:201`).

### Pattern 2: Append loader env into the existing web-search block (mirror Phase-30)
**What:** Extend the `if webSearchEnabled { … }` block in `buildOpenWebUIView` with the external-loader keys; flip BYPASS; the trailing `ENABLE_PERSISTENT_CONFIG=False` gate is unchanged (already covers `webSearchEnabled`).
**Example:**
```go
// Source: pattern from internal/orchestrate/openwebui.go:214-249 (Phase-30 web-search block)
if webSearchEnabled {
    env = append(env,
        envPair{Key: "ENABLE_WEB_SEARCH", Value: "True"},
        envPair{Key: "WEB_SEARCH_ENGINE", Value: "searxng"},
        envPair{Key: "SEARXNG_QUERY_URL",
            Value: fmt.Sprintf("http://%s:%d/search?q=<query>&format=json", searxngAddr, searxngPort)},
        envPair{Key: "WEB_SEARCH_RESULT_COUNT", Value: strconv.Itoa(webSearchResultCount)},
        // Phase-31 GUARD-01: villa-websafe is the sole producer of page_content.
        envPair{Key: "WEB_LOADER_ENGINE", Value: "external"},
        envPair{Key: "EXTERNAL_WEB_LOADER_URL",
            Value: fmt.Sprintf("http://%s:%d/load", websafeAddr, websafePort)}, // path is villa's choice
        // Bearer secret reaches the container via EnvironmentFile (NOT this 0644 unit) — see config note.
        // Phase-31 GROUND-01/02: turn the native embed→retrieve path back ON (Phase-30 set this True).
        envPair{Key: "BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL", Value: "False"},
        // RETRIEVAL FIX (see Pitfall 1) — confirm the exact key on-hardware before freezing:
        envPair{Key: "ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS", Value: "True"},
    )
}
```
**Caution:** `EXTERNAL_WEB_LOADER_API_KEY` is a SECRET — it must NOT be rendered into the 0644 OWUI unit. Carry it via a 0600 `EnvironmentFile=` on the OWUI unit (the OWUI unit currently has none; adding one is new). Alternatively, since both villa-websafe and OWUI are on the private `villa.network` with no host port, the planner MAY treat the bearer as a non-secret shared sentinel (like the existing `sk-no-key-required` placeholders) IF the threat model accepts that any container on villa.network could call villa-websafe. **Recommendation:** generate a real crypto/rand secret and require it (defense-in-depth; villa-websafe rejects requests without the right bearer), carried via EnvironmentFile on BOTH units. This is the cleaner GUARD-01 posture.

### Pattern 3: Reservation-before-fit (mirror MemoryInputs exactly)
**What:** Add a `WebSearchInputs` struct + a `webSearchReservation()` helper + a `WebSearchReservationBytes` field, applied in `Pick` after the embedding reservation and before `pickBest`/`pickOverride`.
**Example:**
```go
// Source: pattern from internal/recommend/recommend.go:142-203
type WebSearchInputs struct {
    Enabled        bool
    ResultCount    int  // cfg.WebSearchResultCount
    TopK           int  // RAG_TOP_K (3) — what actually gets injected
    ChunkSizeChars int  // CHUNK_SIZE (1000)
}
// In Pick, after `reservation, memNotes := memoryReservation(mem)`:
webRes, webNotes := webSearchReservation(web)
// shrink envelope by BOTH reservations before fit (never wrap uint64):
total := addSaturating(reservation, webRes)
if total >= envelope { envelope = 0 } else { envelope -= total }
```
**Schema bump:** add `WebSearchReservationBytes uint64` ABOVE `SchemaVersion` (append-only), bump `recommendSchemaVersion` 3→4, re-freeze the recommend golden intentionally (`go test … -update`). This is the single sanctioned recommend contract bump for Phase 31 (STATE.md decision).

### Anti-Patterns to Avoid
- **Leaving BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True.** That path skips embed+retrieve+ephemeral-collection entirely — GROUND-01/02 cannot be satisfied with it on. (Pitfall 1.)
- **Relying on OWUI's `safe_validate_urls` for SSRF.** The external loader receives URLs without per-IP re-validation at fetch time; villa MUST validate. (Pitfall 3.)
- **Validating only the hostname (not the connected IP).** A DNS-rebinding site passes a hostname check then resolves to 169.254.169.254 — use `net.Dialer.Control` on the resolved IP. (GUARD-05.)
- **Re-typing the websafe/searxng host:port or the base image literal in a caller.** Compose URLs from config (WR-01); keep the image literal behind `internal/orchestrate` + extend `isSeam` (TestSeamGrepGate).
- **Returning non-2xx for a single bad URL.** OWUI `raise_for_status()` would abort the whole batch — return 200 with the bad URL omitted (skip-and-continue).
- **Embedding fetched web content into a durable `open-webui_*` collection.** Web content is untrusted+ephemeral; it lives only in the per-query `web-search-<hash>` collection (GROUND-02).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Chunk / embed / retrieve / cite | A villa chunker, vector store, or citation formatter | OWUI native RAG (`CHUNK_SIZE`/`RAG_TOP_K`) + villa-embed + villa-qdrant + OWUI `sources` | GROUND-01 explicitly reuses v1.3 RAG verbatim; OWUI already does chunk/embed/retrieve/cite. |
| SSRF guard | A regex on the URL string / a hostname blocklist | `net.Dialer.Control` validating the CONNECTED IP + `netip.Prefix` reject-set + `CheckRedirect` | Regex/hostname checks miss DNS rebinding, IPv6, decimal-IP encodings; the stdlib connect-time hook is the only TOCTOU-safe approach. |
| HTML→text extraction | A custom DOM parser | (Phase-31 minimal) return reasonably-extracted text; full sanitize is Phase-32 `bluemonday` | GUARD-02 (sanitize/normalize) is explicitly deferred; P31 stubs the hook. Keep extraction simple. |
| Ephemeral collection drop/recreate | villa-side Qdrant REST drop+recreate calls | OWUI's per-query `web-search-<hash>` collection (it overwrites per query) | OWUI manages its own web-search collection lifecycle; villa adding Qdrant REST calls duplicates+races it (Pitfall 2). |
| Bearer-secret generation | A hardcoded token | `crypto/rand` + 0600 EnvironmentFile (mirror `config.GenerateSearxngSecret`) | Established Phase-29 secret discipline; never a 0644 literal. |

**Key insight:** Phase 31's only genuinely-new code is (a) the SSRF-guarded fetcher and (b) the external-loader HTTP contract glue. Everything downstream of `page_content` (chunk/embed/retrieve/cite/ephemeral-collection) is OWUI-native and already wired by the v1.3 memory block. The risk is the **BYPASS reconciliation** (Pitfall 1) and the **SSRF connect-time check** (GUARD-05), not invention.

## Runtime State Inventory

> Phase 31 is additive (new core + new unit + env changes), not a rename/migration. The runtime-state concern is OWUI's DB-backed ConfigVars and the per-query Qdrant collection.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | OWUI's `web-search-<hash>` ephemeral collection in villa-qdrant (created per query when BYPASS=False). Durable `open-webui_*` memory/doc-KB collections are SEPARATE. | None to migrate — OWUI overwrites the web-search collection per query. **Verify isolation on-hardware**: list Qdrant collections after a web query, confirm `web-search-*` is distinct from durable collections (GROUND-02 proof). |
| Stored data (config) | The Phase-30 `BYPASS_…=True` and any web-search ConfigVars OWUI seeded into its SQLite DB. | `ENABLE_PERSISTENT_CONFIG=False` (already emitted) makes env authoritative every boot, so flipping BYPASS True→False in env takes effect on restart. **No DB migration** — but UAT MUST restart `villa-openwebui.service` so the new env wins. |
| Live service config | None outside git/config — OWUI env fully regenerated from `config.toml` via the Quadlet unit. | None. |
| OS-registered state | New `villa-websafe.container` Quadlet unit → `villa-websafe.service`; OWUI unit regenerated. | `daemon-reload` + restart villa-openwebui + start villa-websafe on opt-in (the lifecycle reconcile path already handles changed units). |
| Secrets/env vars | New `EXTERNAL_WEB_LOADER_API_KEY` bearer (if used) — crypto/rand, 0600 EnvironmentFile on both villa-websafe and villa-openwebui units. | New secret field in config (omit-when-off); a `websafe.env` writer mirroring `WriteSearxngSecretEnv` (Phase-29). |
| Build artifacts | The host `villa` binary is bind-mounted into the container. After `make build`, the new binary is live in the container on next restart (no rebuild of the image). | UAT: rebuild → restart villa-websafe so the container execs the new binary. (Same "dashboard binary trap" class of gotcha — CLAUDE.md.) |

## Common Pitfalls

### Pitfall 1: BYPASS conflict — the load-bearing reconciliation (DIGEST-PINNED)
**What goes wrong:** Phase 30 set `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True` because, at this OWUI digest (v0.9.6-era, commit `02dc3e6`), the native embed→retrieve path embeds web results into the `web-search-<hash>` collection but the retrieval step does NOT query it back at chat time (confirmed by on-hardware UAT AND OWUI issue #25585 "Web search results not passed to model after upgrading to v0.9.6"). So with BYPASS=False but no fix, grounding silently fails (the model answers from stale base knowledge). With BYPASS=True there is no ephemeral collection at all — GROUND-01/02 impossible.
**Why it happens:** OWUI v0.9.6 changed the tool-calling/retrieval scoping; the maintainer (issue #25585) calls it a ~2-line fix and names the runtime lever **`ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True`** as the workaround that re-connects the web-search collection to retrieval.
**How to avoid:** Phase 31 MUST (1) flip BYPASS → `False`, AND (2) set the retrieval-fix lever so the `web-search-<hash>` collection is actually queried. **VERIFY ON-HARDWARE before freezing the golden:** confirm the exact env key (`ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS`) exists at THIS digest in `config.py` and that BYPASS=False + that key produces a grounded, cited answer. If the key is absent/renamed at this digest, the fallback is to keep direct-injection (BYPASS=True) and descope GROUND-02's "embed into ephemeral collection" to "ephemeral by construction (never persisted)" — but that is a CONTEXT-decision change the planner must escalate, not decide silently.
**Warning signs:** UAT shows web search runs (SearXNG hit, villa-websafe fetched) but the answer has no citations / cites nothing live; Qdrant shows a populated `web-search-*` collection but the model ignores it.

### Pitfall 2: Double-managing the ephemeral collection (race / duplication)
**What goes wrong:** villa adds its own Qdrant REST drop+recreate of a `villa_web_ephemeral` collection while OWUI independently creates+overwrites its `web-search-<hash>` collection — two managers, races, and OWUI ignores villa's collection (it queries its own).
**Why it happens:** CONTEXT Area 2 says "clean-replace per query, fixed dedicated collection `villa_web_ephemeral`" — but the verified reality is OWUI OWNS the web-search collection naming (`web-search-<hash>`, per query) and overwrites it. villa cannot rename OWUI's collection via config.
**How to avoid:** Achieve GROUND-02's intent (dedicated + clean-replace + isolated) **through OWUI's own behavior**: OWUI's web-search collection is already (a) dedicated (its own `web-search-*` namespace, distinct from durable `open-webui_*`), (b) clean-replaced per query (OWUI overwrites), and (c) never the durable store. villa's job is to (1) NOT point web search at a durable collection, and (2) PROVE isolation in UAT (list collections). **Do NOT add villa-side Qdrant REST calls.** If the planner wants a fixed villa-named collection, that requires OWUI config villa does not control at this digest — escalate rather than hand-roll. (The `QDRANT_COLLECTION_PREFIX=open-webui` already set in the memory block governs durable collections; web-search uses a separate `web-search-` prefix.)
**Warning signs:** villa code calling Qdrant `/collections` REST endpoints; a `villa_web_ephemeral` literal that OWUI never references.

### Pitfall 3: Trusting OWUI's SSRF (external loaders bypass it at fetch time)
**What goes wrong:** Assuming OWUI's `safe_validate_urls` / `SafeWebBaseLoader` IP checks protect villa-websafe. They don't — when `WEB_LOADER_ENGINE=external`, OWUI validates URL *format* up front but hands the URLs to the external loader, which fetches them itself with no OWUI per-IP/connect-time check.
**Why it happens:** OWUI delegates fetch to the external loader by design; its `_SSRFSafeAdapter`/`_SSRFSafeResolver` only protect OWUI's *own* `SafeWebBaseLoader`, not the external path.
**How to avoid:** villa-websafe implements the full SSRF guard itself (GUARD-05): scheme allowlist + `net.Dialer.Control` connect-time IP validation + `CheckRedirect` per-hop re-validation + hostname reject-set. Treat every URL OWUI sends as untrusted.
**Warning signs:** villa-websafe with no `Control` hook; an SSRF test that passes a `http://169.254.169.254/...` URL and gets a fetch instead of a reject.

### Pitfall 4: SSRF check on hostname only (DNS rebinding TOCTOU)
**What goes wrong:** Resolving + validating the hostname's IP, then a separate `http.Get` that re-resolves to a different (internal) IP between check and use.
**How to avoid:** Validate the IP inside `net.Dialer.Control`, which Go calls *after* DNS resolution and *before* connect, on the exact IP the socket will use. [VERIFIED: agwa.name] This is the only TOCTOU-safe placement. Also set `CheckRedirect` to re-run the check on each redirect target (a 2xx→302→internal redirect chain).
**Warning signs:** SSRF validation in the handler/loader body rather than the dialer; redirects followed without re-validation.

### Pitfall 5: Non-static binary on distroless
**What goes wrong:** The bind-mounted `villa` binary is dynamically linked (cgo via `ghw`) and crashes on distroless/scratch (no glibc).
**How to avoid:** Build with `CGO_ENABLED=0` (ghw degrades to typed-Unknown, which `websafe-serve` never needs anyway), OR use an alpine/glibc base. Confirm with `file ./villa` → "statically linked". The Makefile `make build` target should be checked/adjusted.
**Warning signs:** `villa-websafe` container exits immediately; `podman logs villa-websafe` shows `no such file or directory` (the classic dynamic-loader-missing error) or a glibc version error.

### Pitfall 6: Byte-identical-off regression
**What goes wrong:** The new villa-websafe unit, the loader env keys, or the new config fields leak into the search-off render/config, breaking the v1.4 byte-identical-off bar.
**How to avoid:** Gate the unit render strictly on `WebSearchEnabled` (after the searxng branch, never mutating shared `units`); gate the env keys inside the existing `webSearchEnabled` block; apply omit-when-off zeroing in `marshalVilla` for the new config fields (mirror the SearXNG fields). The existing `TestRenderByteIdenticalWhenWebSearchOff` guards unit count — extend it to expect the new unit ON and unchanged OFF.
**Warning signs:** `TestRenderByteIdenticalWhenWebSearchOff` fails; the search-off OWUI golden changes.

## Code Examples

### SSRF guard: connect-time IP validation + per-hop redirect re-check + scheme allowlist
```go
// Source: stdlib pattern per https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golang
//         + https://blog.doyensec.com/2022/12/13/safeurl.html ; netip prefixes per RFC allocations.
package websafe

import (
    "context"; "fmt"; "net"; "net/http"; "net/netip"; "strings"; "syscall"; "time"
)

// rejectPrefixes is the SSRF reject-set (CONTEXT Area 3, GUARD-05). netip.MustParsePrefix.
var rejectPrefixes = []netip.Prefix{
    netip.MustParsePrefix("127.0.0.0/8"),    // loopback v4
    netip.MustParsePrefix("::1/128"),        // loopback v6
    netip.MustParsePrefix("10.0.0.0/8"),     // RFC1918
    netip.MustParsePrefix("172.16.0.0/12"),  // RFC1918
    netip.MustParsePrefix("192.168.0.0/16"), // RFC1918
    netip.MustParsePrefix("169.254.0.0/16"), // link-local v4 (incl. 169.254.169.254 metadata)
    netip.MustParsePrefix("fe80::/10"),      // link-local v6
    netip.MustParsePrefix("100.64.0.0/10"),  // CGNAT
    netip.MustParsePrefix("fc00::/7"),       // ULA v6
    netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
    netip.MustParsePrefix("::ffff:0:0/96"),  // v4-mapped v6 (catch mapped-internal)
}

func ipRejected(ip netip.Addr) bool {
    if ip.Is4In6() { ip = ip.Unmap() }
    if !ip.IsValid() || ip.IsLoopback() || ip.IsLinkLocalUnicast() ||
        ip.IsLinkLocalMulticast() || ip.IsPrivate() || ip.IsUnspecified() {
        return true
    }
    for _, p := range rejectPrefixes {
        if p.Contains(ip) { return true }
    }
    return false
}

// hostRejected blocks internal service names regardless of resolution (defense-in-depth).
func hostRejected(host string) bool {
    h := strings.ToLower(host)
    return h == "localhost" || strings.HasPrefix(h, "villa-") ||
        strings.HasSuffix(h, ".network") || strings.HasSuffix(h, ".localhost")
}

// control runs AFTER DNS resolution, BEFORE connect, on the ACTUAL IP (defeats DNS-rebinding TOCTOU).
func control(network, address string, _ syscall.RawConn) error {
    host, _, err := net.SplitHostPort(address)
    if err != nil { return err }
    ip, err := netip.ParseAddr(host)
    if err != nil { return fmt.Errorf("unparseable connect addr %q", host) }
    if ipRejected(ip) { return fmt.Errorf("SSRF: refusing connection to %s", ip) }
    return nil
}

func safeClient() *http.Client {
    d := &net.Dialer{Timeout: 10 * time.Second, Control: control}
    tr := &http.Transport{DialContext: d.DialContext}
    return &http.Client{
        Timeout:   10 * time.Second,
        Transport: tr,
        CheckRedirect: func(req *http.Request, via []*http.Request) error {
            if len(via) >= 5 { return fmt.Errorf("SSRF: too many redirects") }
            if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
                return fmt.Errorf("SSRF: non-http(s) redirect scheme %q", req.URL.Scheme)
            }
            if hostRejected(req.URL.Hostname()) {
                return fmt.Errorf("SSRF: refusing internal redirect host %q", req.URL.Hostname())
            }
            return nil // the Control hook re-validates the resolved IP on the new dial
        },
    }
}
```

### Bounded, size-capped fetch with scheme allowlist (the loader core)
```go
func (l *Loader) fetchOne(ctx context.Context, rawURL string) (Page, error) {
    u, err := url.Parse(rawURL)
    if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
        return Page{}, fmt.Errorf("scheme not allowed: %q", rawURL)
    }
    if hostRejected(u.Hostname()) { return Page{}, fmt.Errorf("host rejected: %q", u.Hostname()) }
    ctx, cancel := context.WithTimeout(ctx, 10*time.Second); defer cancel()
    req, _ := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
    resp, err := l.client.Do(req) // Control hook validates the connected IP here
    if err != nil { return Page{}, err }
    defer resp.Body.Close()
    if resp.StatusCode/100 != 2 { return Page{}, fmt.Errorf("non-2xx: %d", resp.StatusCode) }
    body, err := io.ReadAll(io.LimitReader(resp.Body, 2<<20)) // 2 MiB cap, truncate beyond
    if err != nil { return Page{}, err }
    text := l.deps.Extract(body)               // Phase-31: simple extraction
    text = sanitize(normalize(text)); text = fence(text); _ = classify(text) // Phase-32 stubs (identity)
    return Page{Content: text, Source: rawURL, Title: l.deps.Title(body)}, nil
}
// Loader.Load: bounded concurrency (semaphore sized to min(resultCount, cap)); skip-and-continue on error;
// returns []Page; an empty slice ⇒ OWUI gets [] ⇒ honest no-results (no fabricated context).
```

### OWUI external-loader HTTP handler (the contract glue)
```go
// villa websafe-serve handler — implements the VERIFIED OWUI external-loader contract.
type loadReq struct { URLs []string `json:"urls"` }
type loadResp struct { PageContent string `json:"page_content"`; Metadata map[string]any `json:"metadata"` }

func (s *server) handleLoad(w http.ResponseWriter, r *http.Request) {
    if !s.authOK(r) { w.WriteHeader(http.StatusUnauthorized); return } // Bearer check
    var in loadReq
    if err := json.NewDecoder(io.LimitReader(r.Body, 1<<20)).Decode(&in); err != nil {
        w.WriteHeader(http.StatusBadRequest); return
    }
    pages := s.loader.Load(r.Context(), in.URLs) // SSRF-guarded, bounded, skip-and-continue
    out := make([]loadResp, 0, len(pages))
    for _, p := range pages {
        out = append(out, loadResp{PageContent: p.Content,
            Metadata: map[string]any{"source": p.Source, "title": p.Title}}) // source → OWUI `sources` citation
    }
    w.Header().Set("Content-Type", "application/json")
    _ = json.NewEncoder(w).Encode(out) // ALWAYS 200 with (partial) array — never abort the batch
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| OWUI native fetch (`SafeWebBaseLoader`) does the page fetch | `WEB_LOADER_ENGINE=external` delegates fetch to a user service | OWUI ≥ the PR #12822 era (present at pinned `02dc3e6`) | villa owns `page_content` (GUARD-01); villa MUST do its own SSRF. |
| Phase-30 `BYPASS_…=True` direct-inject | Phase-31 `BYPASS_…=False` + retrieval-fix → embed→retrieve→cite | This phase | Restores the ephemeral-collection RAG path (GROUND-01/02); requires the v0.9.6 retrieval-fix lever (Pitfall 1). |
| Web results silently dropped at retrieval (v0.9.6 bug) | `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True` reconnects them | OWUI issue #25585 | The exact fix lever — CONFIRM at this digest before freezing. |

**Deprecated/outdated:**
- External web **SEARCH** engine (`WEB_SEARCH_ENGINE=external`, POST `{query,count}` → `[{link,title,snippet}]`) — a DIFFERENT feature; villa uses SearXNG for search and `external` only for the LOADER. Do not conflate the two contracts.
- Older `RAG_WEB_*` env family — gone at this digest (Phase-30 finding); use `WEB_*` / `WEB_LOADER_*` names.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True` (or equivalent) re-connects the web-search collection to retrieval at THIS digest, so BYPASS=False grounds. | Pitfall 1 | **HIGH** — if the key is absent/renamed at `02dc3e6`, GROUND-01/02's embed→retrieve path won't ground without a different lever. Mitigation: on-hardware verify in `config.py` + a real cited-answer UAT BEFORE freezing the golden; escalate to a CONTEXT change if no lever works. |
| A2 | OWUI's web-search collection (`web-search-<hash>`) is per-query overwritten and distinct from durable `open-webui_*` collections, satisfying GROUND-02 by construction without villa-side Qdrant calls. | Pitfall 2 | MEDIUM — if OWUI reuses one persistent web-search collection across queries, "clean-replace per query" needs verification; prove by listing collections across two queries on-hardware. |
| A3 | The host `villa` binary links statically (CGO_ENABLED=0 viable) so it runs on distroless. | Pitfall 5 | MEDIUM — if `ghw` forces cgo, use alpine/glibc base or rebuild CGO_OFF. Verify `file ./villa`. |
| A4 | The external loader receives URLs without OWUI per-IP re-validation at fetch time (villa-side SSRF mandatory). | Pitfall 3 | LOW — verified in source (`get_web_loader` external branch); even if OWUI added a check, villa-side SSRF is still correct (defense-in-depth). |
| A5 | `EXTERNAL_WEB_LOADER_API_KEY` as a crypto/rand bearer is honored (OWUI sends `Authorization: Bearer …`). | OWUI Contract | LOW — verified in `external_web.py`. If empty, OWUI sends no/empty bearer; villa can then accept any villa.network caller. |
| A6 | Conservative ctx reservation = `RAG_TOP_K(3) × CHUNK_SIZE(1000 chars) ÷ ~3.5 chars/token + citation overhead`, scaled by a safety factor. | Validation / GROUND-03 | LOW (intentionally conservative) — over-reserving is safe (refuses risky picks); on-hardware tuning is deferred to Phase 33/34. |

## Open Questions

1. **Exact retrieval-fix env key at digest `02dc3e6`.**
   - What we know: OWUI issue #25585 names `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True` as the v0.9.6 workaround; BYPASS must be False for embed→retrieve.
   - What's unclear: whether THIS digest has that exact key (vs a rename / a ~2-line code fix not exposed as env).
   - Recommendation: First Phase-31 task = on-hardware probe `config.py` at the running container for the key, then a BYPASS=False cited-answer UAT. Freeze the OWUI golden ONLY after this passes. If no env lever works, escalate (do not silently keep BYPASS=True — that defeats GROUND-02).

2. **Bearer secret vs villa.network trust.**
   - What we know: both services are on private villa.network with no host port; the bearer adds defense-in-depth.
   - What's unclear: whether the threat model (Phase 33 owns egress proof) requires the bearer in P31.
   - Recommendation: implement the bearer (crypto/rand + 0600 EnvironmentFile on both units) — it's cheap and is the cleaner GUARD-01 posture; villa-websafe rejects unauthenticated callers.

3. **Canonical host binary path for the bind-mount.**
   - What we know: the container execs the host binary; the path must be captured at install time, never shell-interpolated.
   - Recommendation: capture `os.Executable()` (or the install target dir) at opt-in into a config field; render the `:ro,z` Volume from it. Discretion D leaves the exact field shape to the planner.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Pinned OWUI image (`02dc3e6`) | external-loader contract + retrieval-fix UAT | ✓ | `sha256:7f1b0a1a…` | — (on dev box) |
| villa-embed + villa-qdrant (v1.3 memory stack) | the reused embed/retrieve/cite path | ✓ (when memory enabled) | digest-pinned | **Dependency note:** GROUND-01 needs the memory stack's villa-embed + Qdrant + the OWUI RAG env block. Confirm web-search grounding requires `MemoryEnabled` (the RAG_OPENAI_* env is in the memory block) — the planner must decide whether web search implies/requires memory-on, or whether the RAG env must be emitted for web-search-on too. **This is a real wiring dependency to resolve.** |
| `gcr.io/distroless/static-debian12` base image | villa-websafe container | ✗ (not yet pulled) | pin on dev box | alpine (if binary not static) |
| Go 1.26.2 + CGO_ENABLED=0 build | static binary for distroless | ✓ | 1.26.2 | alpine/glibc base |
| Rootless Podman + `systemctl --user` (dev box) | on-hardware UAT | ✓ | gfx1151 | off-hardware: golden/drift/unit tests cover render + SSRF + reservation |

**Missing dependencies with no fallback:** none (the base image is pull-on-demand).
**Critical wiring dependency:** the RAG/embed env (`RAG_OPENAI_API_BASE_URL` → villa-embed, `VECTOR_DB=qdrant`) currently appends ONLY in the `memoryEnabled` block (`openwebui.go:162-212`). For web-search grounding (BYPASS=False) OWUI needs those RAG keys. **The planner MUST decide:** (a) web search requires memory-on (simplest — document it), or (b) emit the RAG/embed/Qdrant env when `memoryEnabled || webSearchEnabled` (a refactor of the memory block, larger golden impact). This is the single biggest planning decision after Pitfall 1.

## Validation Architecture

> nyquist_validation is enabled (config.json). This section drives VALIDATION.md.

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go stdlib `testing` (table-driven + `httptest` + byte-frozen goldens) — the ONLY framework (CLAUDE.md) |
| Config file | none (`go test`) |
| Quick run command | `go test ./internal/websafe/ ./internal/recommend/ ./internal/orchestrate/` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| GUARD-05 | SSRF reject-set blocks loopback/RFC1918/link-local/CGNAT/metadata/ULA + internal hosts | unit (table) | `go test ./internal/websafe/ -run TestSSRFRejectSet` | ❌ Wave 0 |
| GUARD-05 | Redirect re-check rejects a 2xx→302→internal chain and >5 hops | unit (`httptest` redirect server) | `go test ./internal/websafe/ -run TestRedirectRevalidation` | ❌ Wave 0 |
| GUARD-05 | Connect-time `Control` rejects DNS-rebinding (hostname public, IP internal) | unit (`Control` invoked with internal addr) | `go test ./internal/websafe/ -run TestControlConnectTime` | ❌ Wave 0 |
| GUARD-01 | Handler implements OWUI contract: `{urls}` → `[{page_content,metadata.source}]`, 200 always | unit (`httptest`) | `go test ./internal/websafe/ -run TestExternalLoaderContract` | ❌ Wave 0 |
| GROUND-01 (partial) | Skip-and-continue: bad URL omitted; all-fail ⇒ `[]` | unit | `go test ./internal/websafe/ -run TestSkipAndContinue` | ❌ Wave 0 |
| Bounds | 2 MB truncation + 10s timeout + scheme allowlist | unit (`httptest` oversize/slow server) | `go test ./internal/websafe/ -run TestFetchBounds` | ❌ Wave 0 |
| GROUND-03 | `WebSearchReservationBytes` reserved before fit; gated on Enabled; off-path math unchanged | unit (table) | `go test ./internal/recommend/ -run TestWebSearchReservation` | ❌ Wave 0 |
| GROUND-03 | recommend schema 3→4 golden re-freeze (intentional) | golden | `go test ./internal/recommend/ -run TestRecommendationGolden` | ✅ (re-freeze) |
| GUARD-01/render | villa-websafe unit rendered ON / absent OFF; byte-identical-off | golden + drift | `go test ./internal/orchestrate/ -run 'TestRenderWebsafe|TestRenderByteIdenticalWhenWebSearchOff'` | ❌ Wave 0 (new golden) |
| OWUI env | external-loader keys + BYPASS=False + retrieval-fix frozen; drift test binds each key | golden + drift | `go test ./internal/orchestrate/ -run TestRenderOpenWebUITelemetryFrozen` | ✅ (extend) |
| Seam | no image/host literal leaks (websafe.go on isSeam allowlist) | drift | `go test ./internal/inference/ -run TestSeamGrepGate` | ✅ |
| GROUND-01/02, retrieval-fix | **Live grounded, cited answer; ephemeral collection isolated** | **manual UAT (on-hardware)** | gfx1151: real query → cited live URLs; `curl villa-qdrant /collections` shows `web-search-*` distinct from durable | manual-only |
| GROUND-03 | **Offload-assert under search load (no silent CPU fallback)** | **manual UAT (on-hardware)** | search-on query under load; residency proof PASS | manual-only |

### Sampling Rate
- **Per task commit:** `go test ./internal/websafe/ ./internal/recommend/ ./internal/orchestrate/`
- **Per wave merge:** `make check`
- **Phase gate:** full suite green + the two on-hardware UATs (grounded-cited-answer; offload-assert) PASS before `/gsd-verify-work`.

### What genuinely needs on-hardware UAT (cannot be automated off-hardware)
1. **Pitfall-1 reconciliation:** BYPASS=False + retrieval-fix actually yields a grounded answer with inline citations to live URLs (the whole point of the phase). This is the blocking checkpoint.
2. **GROUND-02 isolation proof:** the ephemeral `web-search-*` collection is distinct from durable `open-webui_*` collections (list collections).
3. **GROUND-03 offload-assert under search load:** the chat model stays GPU-resident with web RAG injected (silent CPU fallback = FAIL).
4. **Pitfall-5 static-binary:** the bind-mounted binary execs on distroless (`podman logs villa-websafe`).

### Wave 0 Gaps
- [ ] `internal/websafe/ssrf_test.go` — GUARD-05 reject-set + redirect + Control tests
- [ ] `internal/websafe/websafe_test.go` — contract + skip-and-continue + bounds tests
- [ ] `internal/recommend/recommend_test.go` — extend with `TestWebSearchReservation` + re-freeze golden
- [ ] `internal/orchestrate/testdata/villa-websafe.container.golden` — new render golden
- [ ] `internal/orchestrate/testdata/villa-openwebui.container.websearch.golden` — re-freeze (loader keys + BYPASS=False + retrieval-fix)
- [ ] No framework install needed — stdlib `testing`.

## Security Domain

> security_enforcement enabled (ASVS L1). Phase 31 adds an outbound fetcher + a new inbound HTTP service — both security-relevant.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | yes | `EXTERNAL_WEB_LOADER_API_KEY` Bearer on villa-websafe (crypto/rand, 0600 EnvironmentFile); reject unauthenticated |
| V5 Input Validation | yes | SSRF guard (GUARD-05): scheme allowlist + connect-time IP validation + redirect re-check; `io.LimitReader` on the request body; URL parse-and-reject |
| V6 Cryptography | yes | `crypto/rand` for the bearer (never hand-roll); no other crypto |
| V7 Error Handling | yes | Skip-and-continue (no leaking of internal fetch errors to the model); honest no-results, never fabricate |
| V12 Files/Resources | yes | Read-only `:ro,z` binary bind-mount; loopback/container-DNS only, no host port (PRIV-01) |
| V13 API/Web Service | yes | villa-websafe is loopback-on-villa.network only; bounded body + bounded concurrency (DoS bound) |

### Known Threat Patterns for {Go fetcher + OWUI external loader}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| SSRF to cloud metadata (169.254.169.254) | Information Disclosure | `netip` reject-set incl. link-local; connect-time `Control` |
| DNS rebinding (hostname public, IP internal) | Spoofing/Info Disclosure | validate the CONNECTED IP in `Control`, not the hostname |
| Redirect-based SSRF (302 → internal) | Info Disclosure | `CheckRedirect` re-validates every hop + ≤5 cap |
| Unbounded body / slowloris from malicious site | DoS | `io.LimitReader` 2 MB + `context` 10s timeout + bounded concurrency |
| Unauthenticated caller on villa.network hitting villa-websafe | Spoofing/Tampering | Bearer auth; reject without the shared secret |
| Prompt injection in fetched `page_content` | Tampering | **OUT OF SCOPE for P31** (Phase 32); the sanitize/normalize/fence/classify seam is stubbed-but-present; NEVER claim injection-safety |
| Web content poisoning durable memory | Tampering | Dedicated ephemeral collection only (GROUND-02); never the durable store |

**Posture note (carry verbatim):** Phase 31 lands the fetch path + SSRF + ctx-bound. It does NOT claim injection immunity. "Outbound is bounded" is proven in Phase 33; "safe from injection" is NEVER claimed. Grep-ban "injection-safe" copy.

## Sources

### Primary (HIGH confidence)
- `open-webui/open-webui@02dc3e6` `backend/open_webui/retrieval/loaders/external_web.py` — `ExternalWebLoader` request/response contract (POST `{urls}` → `[{page_content,metadata}]`, Bearer auth, `raise_for_status`).
- `open-webui/open-webui@02dc3e6` `backend/open_webui/config.py` — exact env names + defaults (`WEB_LOADER_ENGINE`, `EXTERNAL_WEB_LOADER_URL/_API_KEY`, `BYPASS_*`, `RAG_TOP_K`, `CHUNK_SIZE/OVERLAP`, `WEB_SEARCH_RESULT_COUNT`).
- `open-webui/open-webui@02dc3e6` `backend/open_webui/retrieval/web/utils.py` — `get_web_loader` external branch; OWUI's own SSRF (`_SSRFSafeAdapter`/`_SSRFSafeResolver`) protects only the native loader.
- `open-webui/open-webui@02dc3e6` `backend/open_webui/routers/retrieval.py` — `process_web`/web-search collection naming + the BYPASS branch (`collection_name=None` vs `save_docs_to_vector_db`).
- https://www.agwa.name/blog/post/preventing_server_side_request_forgery_in_golang — `net.Dialer.Control` connect-time IP validation (the canonical Go SSRF pattern).
- VillaStraylight codebase (read this session): `internal/orchestrate/{openwebui,searxng,memory,render}.go`, `internal/recommend/recommend.go`, `internal/memory/memory.go`, `internal/config/villaconfig.go`, `cmd/villa/lifecycle.go`, the Phase-30 RESEARCH/SUMMARY (D-06 BYPASS finding).

### Secondary (MEDIUM confidence)
- OWUI issue #25585 "Web search results not passed to model after upgrading to v0.9.6" — confirms embed-without-retrieve bug + `ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True` workaround.
- https://blog.doyensec.com/2022/12/13/safeurl.html ; https://pkg.go.dev/code.dny.dev/ssrf — Go SSRF allow/deny via `Control`.
- OWUI docs `features/chat-conversations/web-search/providers/external/` — external SEARCH (distinct from loader) contract, useful to NOT conflate.
- distroless docs (`GoogleContainerTools/distroless`) — static-binary base-image guidance.

### Tertiary (LOW confidence)
- WebSearch summaries of the external-loader request body (`{urls}` → `[{page_content,metadata}]`) — corroborated by the source read, so promoted to HIGH for that claim.

## Metadata

**Confidence breakdown:**
- OWUI external-loader contract (env names, request/response): HIGH — read from source at the exact pinned commit.
- SSRF guard pattern: HIGH — stdlib mechanism + canonical references; code example is directly adaptable.
- Reuse patterns (orchestrate/recommend/config): HIGH — read from the live codebase this session.
- BYPASS=False retrieval reconciliation: MEDIUM — the bug is empirically proven (Phase-30 UAT + issue #25585); the exact fix lever at this digest needs on-hardware confirmation (A1, the top risk).
- Ephemeral collection lifecycle: MEDIUM — OWUI owns it; the planner must NOT double-manage (Pitfall 2).
- Ctx-budget byte math: LOW — intentionally conservative; on-hardware tuning deferred to Phase 33/34.

**Research date:** 2026-06-19
**Valid until:** 2026-07-19 (the OWUI digest is pinned, so the contract is stable; re-verify only on an OWUI digest bump — which by CLAUDE.md forces a deliberate re-audit anyway).

## RESEARCH COMPLETE
