# Phase 31: Grounded Fetch → Embed Grounding - Context

**Gathered:** 2026-06-19
**Status:** Ready for planning

> Captured via autonomous smart-discuss. All four grey areas resolved with their
> recommended option. Decisions follow the v1.5 roadmap decisions (STATE.md →
> Decisions) and established v1.3 RAG / v1.4 verify / Phase-29/30 patterns.
> Phase 31 is **research-flagged** — the planner MUST run `--research-phase`
> (fetch path / OWUI external-loader contract + SSRF) before planning.

<domain>
## Phase Boundary

Establish the **grounded fetch path** so a search-on research question returns an
answer grounded in fetched pages with **inline citations to live URLs**. Insert a
villa-owned `villa-websafe` loader as OWUI's `WEB_LOADER_ENGINE=external` fetch
path — the **sole producer of `page_content`** (the GUARD-01 seam, on the fetch
path). Reuse the shipped v1.3 villa-embed/Qdrant RAG and OWUI's top-level
`sources` citation field **verbatim**; embed fetched content into a **dedicated
ephemeral collection**; reserve a **web-search ctx budget before the chat fit**;
enforce an **SSRF guard** on the fetcher.

**In scope:** the `internal/websafe` pure core (fetch→[sanitize/normalize/fence
seam stubbed for Phase 32]→produce `page_content`) with network fetch injected as
a `Deps` func; the `villa websafe-serve` containerized HTTP service (Quadlet unit,
gated on `WebSearchEnabled`); OWUI external-loader env wiring; the ephemeral
Qdrant collection lifecycle; `recommend` web-search ctx reservation + offload
assertion seam; the SSRF guard (resolve-and-validate, redirect re-check, scheme
allowlist).

**Out of scope (later phases):** the full injection-guard policy — Unicode
normalization, provenance-fencing, heuristic classifier (**Phase 32**, GUARD-02/03/04);
`villa verify search` egress proof + opt-in/PRIV plumbing (**Phase 33**, PRIV-07/08/09);
`status.Report` 4→5 surfacing, dashboard panel, doctor, backup (**Phase 34**, SURF-04..07).
Phase 31 lands the *fetch path + resource bounds*; the guard *policy* layers on in 32.

</domain>

<decisions>
## Implementation Decisions

### Area 1 — `villa-websafe` service shape & lifecycle
- **Containerize the villa binary itself**: a hidden `villa websafe-serve` subcommand
  run inside a Quadlet container that **bind-mounts the host `villa` binary** into a
  minimal, digest-pinned base image — no new build/publish pipeline; the
  single-static-binary discipline is preserved (the same binary, a new subcommand,
  run in a container context). (Planner/researcher: pin the base image digest; keep
  any image literal behind the `internal/orchestrate` seam, never in a caller.)
- **Render + start the unit ONLY when `WebSearchEnabled`** — byte-identical-off,
  mirroring the Phase-29 SearXNG service gating and the `volumeView`/`searxngView`
  render pattern in `internal/orchestrate/render.go`.
- **Network identity:** container-DNS `villa-websafe` on `villa.network`, **no host
  port** (PRIV-01), fixed internal port (e.g. 8090) composed from config, never a
  re-typed host literal in a caller (keeps `TestSeamGrepGate` green).
- **OWUI wiring:** set `WEB_LOADER_ENGINE=external` + the external-loader URL env
  into the **same `WebSearchEnabled` OWUI env block** added in Phase 30
  (`buildOpenWebUIView`, append-only, golden-frozen). **Researcher MUST verify the
  exact env key names** (`EXTERNAL_WEB_LOADER_URL` and the request/response contract)
  at the pinned OWUI digest before freezing the golden — OWUI churns this family.

### Area 2 — Ephemeral grounding collection & RAG reuse
- **Clean-replace per query**: drop + recreate the ephemeral collection before each
  web search so stale cross-query content never bleeds in; bounded lifetime by
  construction.
- **Fixed dedicated collection** (e.g. `villa_web_ephemeral`), strictly distinct
  from the operator's durable memory / document-KB collections — untrusted ephemeral
  content must never poison long-term recall (GROUND-02).
- **Reuse v1.3 RAG verbatim**: villa-embed + Qdrant + OWUI's top-level `sources`
  citation field — **no new embedder, vector DB, or citation plumbing**
  (`internal/orchestrate/memory.go` `qdrantView`/`buildQdrantView` are the precedent).
- **Reuse OWUI's native chunk/retrieve** (the existing `RAG_*` settings) for fetched
  pages rather than a villa-specific chunker.

### Area 3 — Fetch resource bounds & SSRF guard
- **Comprehensive SSRF rejection**: loopback, link-local (`169.254/16`, `fe80::/10`),
  **all RFC1918 private** (`10/8`, `172.16/12`, `192.168/16`), CGNAT (`100.64/10`),
  the cloud-metadata IP `169.254.169.254`, and internal `villa-*` / `.network` hosts;
  **resolve-then-validate each resolved IP** (not just the hostname); **http(s)
  scheme allowlist only**.
- **Redirects:** follow up to a small cap (**≤5**), **re-running the full SSRF check
  on every hop**; reject when the cap is exceeded.
- **Conservative per-fetch bounds:** max page size ~**2 MB** (truncate beyond),
  fetch timeout ~**10 s**, fetches-per-query bounded by the result-count, bounded
  fetch concurrency. (On-hardware tuning deferred; conservative is the v1.5 default
  per STATE.md unmeasured-ctx blocker.)
- **Fetch-failure behavior:** **skip-and-continue** — a failed URL (timeout,
  SSRF-reject, non-2xx, oversize) is omitted from grounding (honest partial); if
  **all** fetches fail → no injected context (honest no-results, never fabricated),
  consistent with Phase-30 D-06 honesty.

### Area 4 — Context-budget reservation (`recommend`)
- **Reserve the web-RAG injection budget** (retrieval top-K × chunk size + citation
  overhead) **before** the chat-model fit in `recommend.Pick` so a search-on
  envelope cannot silently CPU-fall-back (GROUND-03). Extends the existing
  `memoryReservation(mem)` → `EmbeddingReservationBytes` seam
  (`internal/recommend/recommend.go:160,233`).
- **New append-only field** on `Recommendation` (e.g. `WebSearchReservationBytes`)
  alongside `EmbeddingReservationBytes` — the single sanctioned recommend-side
  contract bump for Phase 31 (roadmap: "recommend's ctx-reservation evolution lands
  once where the fit math changes"). Re-freeze the recommend golden intentionally.
- **Gate on `WebSearchEnabled`** — the off-envelope fit stays identical to v1.4.
- **Offload-assert under search load** — a silent/partial CPU fallback is a FAIL;
  the assertion seam lands here and is fully exercised in Phase 33/34.

### Claude's Discretion
- Exact `internal/websafe` package shape, `Deps` struct fields, and `villa
  websafe-serve` cobra wiring — planner/executor's call, consistent with the
  `live*Deps` seam + pure-core conventions.
- The Phase-32 guard hooks (sanitize/normalize/fence/classify) are **stubbed
  pass-through** in Phase 31 so the seam exists without the policy — exact stub
  shape is implementation detail.
- Precise default numbers (port, page-size/timeout caps, top-K, per-page token
  estimate) within the "conservative" intent — planner picks, researcher informs.

</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- **Managed-service render precedent:** `internal/orchestrate/searxng.go`
  (`SearXNGImage`, `searxngView`, `RenderSearxngSettings`) + `render.go`
  (`Render`, `containerView`, `volumeView`) — the exact template for the
  `villa-websafe` Quadlet unit, gated on `WebSearchEnabled`.
- **Qdrant/RAG reuse:** `internal/orchestrate/memory.go` (`QdrantImage`,
  `buildQdrantView`, `qdrantView`, `QdrantVolumeName`) — the v1.3 vector-DB wiring
  the ephemeral collection rides on; OWUI `sources` citation is already wired.
- **OWUI env block:** `buildOpenWebUIView` (`internal/orchestrate/openwebui.go:137`)
  already carries `webSearchEnabled`/`searxngAddr`/`searxngPort`/`webSearchResultCount`
  from Phase 30 — the external-loader env appends here (append-only, golden-frozen).
- **Reservation seam:** `recommend.Pick` → `memoryReservation(mem)` →
  `finalizeRecommendation(..., reservation uint64, ...)` →
  `rec.EmbeddingReservationBytes` (`internal/recommend/recommend.go:160,233`).
- **Config single-source:** `internal/config/villaconfig.go` web-search fields
  (`WebSearchEnabled`, `SearxngAddr`, `SearxngPort`, `WebSearchResultCount`) +
  omit-when-off marshal discipline — the new websafe/reservation fields slot in.
- **Lifecycle:** `managedServices` (`cmd/villa/lifecycle.go:102`) enumerates managed
  units for up/down/status — `villa-websafe` joins it when enabled.

### Established Patterns
- Append-only env/contract evolution behind a config flag; byte-identical render
  when off (Phase-18/20/30 continuity).
- Config is the single source of truth; host identities composed via `fmt` from
  resolved config, never re-typed (`TestSeamGrepGate`).
- Pure core + injectable `Deps` seam; **orchestrate is the only intentionally-impure
  module** — the websafe network fetch is injected as a `Deps` func so the core is
  unit-testable off-hardware.
- `--json`/golden byte-freeze + schema-bump discipline (recommend golden re-freeze
  here is sanctioned and isolated to the reservation math).

### Integration Points
- `render.go` threads resolved `in.Cfg` into the new `villa-websafe` view + the
  OWUI external-loader env.
- The ephemeral collection is created/replaced against the existing `villa-qdrant`
  service over `villa.network` by container DNS (never a host bind — PRIV-01).
- `villa-websafe` is reached by OWUI over `villa.network` by container DNS only.

</code_context>

<specifics>
## Specific Ideas

- **Two governing claims, never inverted** (carried from the roadmap): *"outbound is
  bounded"* is provable (proven in Phase 33); *"safe from injection" is NEVER
  claimed* — Phase 31 lands the fetch path + SSRF + ctx-bound, NOT injection
  immunity. No "injection-safe" copy.
- **`villa-websafe` is the sole producer of `page_content`** — SC#1 is the hard bar:
  every byte embedded or shown to the model passes through it. The Phase-32 guard
  hooks are stubbed-but-present so the seam is real from Phase 31.
- **No agentic browsing, no link-following beyond depth=1, no cloud search/extraction
  APIs** — read-only fetch + embed + cite only (Out of Scope, REQUIREMENTS.md).
- SSRF + ephemeral-isolation + ctx-bound are the *resource* backstops layered
  *before* any guard policy — defense-in-depth ordering is deliberate.

</specifics>

<deferred>
## Deferred Ideas

- **Injection-guard policy** — Unicode normalization, nonced provenance fence,
  heuristic classifier, "reduces-and-flags" copy, markdown-image residual doc →
  **Phase 32** (GUARD-02/03/04).
- **`villa verify search` egress proof** + opt-in/default-off plumbing + OWUI
  lazy-outbound kill (`HF_HUB_OFFLINE`/telemetry) → **Phase 33** (PRIV-07/08/09).
- **Surfacing** — `status.Report` 4→5 `web_search` block, dashboard panel, doctor
  folds, backup/restore coverage → **Phase 34** (SURF-04..07).
- On-hardware tuning of result-count × page-size ctx caps for guaranteed GPU
  residency under search load → measured in Phase 33/34 (reserve conservatively now).

</deferred>

---

*Phase: 31-grounded-fetch-embed-grounding*
*Context gathered: 2026-06-19 (autonomous smart-discuss)*
