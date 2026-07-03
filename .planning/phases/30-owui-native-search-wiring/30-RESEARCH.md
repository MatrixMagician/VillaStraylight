# Phase 30: OWUI Native-Search Wiring - Research

**Researched:** 2026-06-18
**Domain:** Open WebUI native web-search env wiring (SearXNG) behind the orchestrate render seam; byte-identical-off discipline; golden + drift tests
**Confidence:** HIGH (env names verified against OWUI `config.py` AND the SearXNG provider source at the EXACT pinned digest's git revision)

## Summary

This phase is a tightly-scoped extension of a proven pattern: append a config-gated, ordered env block to `buildOpenWebUIView` (exactly the Phase-20 `memoryEnabled` template), thread the inputs from resolved config through `render.go`, add one new `omit-when-off` config field, and freeze the result with a search-on golden + a key-binding drift test. There is **no new dependency, no new container, no new template** — OWUI is already the digest-pinned managed service and SearXNG already renders (Phase 29). The work is entirely inside `internal/orchestrate/openwebui.go`, `internal/orchestrate/render.go`, `internal/config/villaconfig.go`, and the orchestrate test/golden set.

The single highest-risk item — verifying OWUI's churned web-search env family at the pinned digest — is now **resolved with HIGH confidence**. The pinned image `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…ea9184e` was confirmed on the live dev box to be OWUI `:main` at git revision `02dc3e689ceac915a870b373318b99c029ddf603`. Against `backend/open_webui/config.py` at that exact revision, the current env family is authoritative: `ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE`, `SEARXNG_QUERY_URL`, `WEB_SEARCH_RESULT_COUNT` (default 3), `WEB_SEARCH_CONCURRENT_REQUESTS` (default 0). The older `ENABLE_RAG_WEB_SEARCH` / `RAG_WEB_SEARCH_*` names are **gone** at this revision — there is no `os.environ` fallback to them. All are `ConfigVar` (DB-backed PersistentConfig), so `ENABLE_PERSISTENT_CONFIG=False` is **mandatory and load-bearing**, identical in rationale to the Phase-20 memory block.

One **non-obvious, digest-pinned finding refines CONTEXT.md D-03**: at this exact revision OWUI's SearXNG provider (`backend/open_webui/retrieval/web/searxng.py`) **strips any query string from `SEARXNG_QUERY_URL` when the `<query>` token is present** (`query_url.split('?')[0]`) and **adds `format=json` itself** as a request param. So the `&format=json` suffix in `SEARXNG_QUERY_URL` is a harmless **no-op at this digest** (it is discarded along with `q=<query>`). It is still correct to freeze it in the golden (matches SC#1's literal frozen-env requirement and is robust if OWUI later stops stripping), but the plan/verification must NOT assume OWUI forwards `&format=json` from the URL — the JSON contract is carried by SearXNG's own `settings.yml` (`search.formats: [html, json]`, already rendered in Phase 29) plus OWUI's own `format=json` param.

**Primary recommendation:** Mirror the Phase-20 memory block exactly. Append a `WebSearchEnabled`-gated ordered env group in `buildOpenWebUIView`; refactor `ENABLE_PERSISTENT_CONFIG=False` to a single trailing emit when memory OR web-search is on; add `WebSearchResultCount int` (`toml:"web_search_result_count,omitzero"`, default 3) with the existing omit-when-off discipline; freeze a new search-on golden and extend the existing `TestRenderOpenWebUITelemetryFrozen` bidirectional binding to a web-search case (that IS the SC#4 drift test). Use the env names verified below verbatim.

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Compose OWUI web-search env from config | orchestrate (`openwebui.go`) | config (`villaconfig.go`) | URLs composed via `fmt` from resolved config; no re-typed host literals (WR-01 / `TestSeamGrepGate`) |
| Thread web-search inputs into the OWUI view | orchestrate (`render.go`) | config | `render.go` reads `in.Cfg` (single source of truth) and passes to `buildOpenWebUIView` |
| Persist + default the result-count knob | config (`villaconfig.go`) | — | Config is the single source of truth; authoritative because `ENABLE_PERSISTENT_CONFIG=False` |
| Per-query/per-session opt-in toggle | OWUI runtime (container) | — | Native OWUI UI feature; villa only supplies env, never overrides the toggle (SRCH-03) |
| Honest no-results behavior | OWUI runtime (native search→embed→retrieve→inject) | — | No villa system prompt / fabrication-guard; honesty is a runtime property asserted by UAT (D-06) |
| Byte-identical-off render | orchestrate + config | tests/goldens | Env group only appends when `WebSearchEnabled`; off-render == v1.4, proven by negative tests |

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** Append the web-search env group inside `buildOpenWebUIView` (`internal/orchestrate/openwebui.go`) as ONE ordered block, gated on `WebSearchEnabled`, exactly mirroring the existing `memoryEnabled` append (Phase-20 D-02 append-only discipline). `buildOpenWebUIView`'s signature gains the web-search inputs (enabled flag + `SearxngAddr`/`SearxngPort` + result count); the `render.go:146` caller threads them from resolved `in.Cfg` (config is the single source of truth — no re-typed host literals, keeps `TestSeamGrepGate` green, WR-01).
- **D-02:** The env keys are `ENABLE_WEB_SEARCH=True`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL=<composed>`, the result-count key, and `ENABLE_PERSISTENT_CONFIG=False`. **Researcher MUST re-verify the exact key names at the pinned OWUI digest** — *RESOLVED: see "OWUI Env Contract — Verified Against Source at the Pinned Digest" below.*
- **D-03:** Compose the query URL from resolved config, never re-typed: `http://{cfg.SearxngAddr}:{cfg.SearxngPort}/search?q=<query>&format=json`. `<query>` is OWUI's literal substitution placeholder (kept verbatim). `&format=json` is mandatory (matches the Phase-29 SearXNG JSON-format proof). Defaults resolve to `villa-searxng:8080`. *NOTE: a digest-pinned behavior refines this — see Pitfall 1; the `&format=json` suffix is a no-op at this digest but harmless to freeze.*
- **D-04:** `ENABLE_PERSISTENT_CONFIG=False` is **load-bearing and MUST appear exactly once**, as the **last** Environment entry, whenever memory OR web-search is enabled. Refactor so it is emitted once at the tail when either group is on — never duplicated, never dropped when web-search is on but memory is off.
- **D-05:** Add a new config field (e.g. `WebSearchResultCount int`, `toml:"web_search_result_count,omitzero"`) to `VillaConfig`, default **3**, with the same **omit-when-off** discipline as the other web-search fields (`villaconfig.go` zeroes web-search fields on the by-value marshal copy when `!WebSearchEnabled`, lines ~333-343). Operator tunes count via `config.toml`; toggles search on/off per-query/per-session via OWUI's native UI toggle (SRCH-03). Default 3 keeps the context budget conservative ahead of Phase 31.
- **D-06:** Rely on OWUI's **native** web-search behavior for honesty: results injected as retrieval context; no results → no injected context → model answers from base knowledge or states it cannot find current info. Phase 30 adds **no** villa system-prompt override or fabrication-guard env. Honesty asserted by **UAT**. Stronger grounding + citations land in Phase 31; injection screening in Phase 32. Do not pre-build either here.
- **D-07:** Search-off render stays **byte-identical to v1.4**: the web-search env group only appends when `WebSearchEnabled`, so the existing OWUI goldens (memory-off + memory-on) are unchanged when search is off — prove via the existing negative-render test.
- **D-08:** Add a new search-on OWUI container golden (e.g. `villa-openwebui.container.websearch.golden`, mirroring `…memory.golden`) that freezes the appended web-search env group, AND a **drift test** that binds each web-search env KEY string to its orchestrate accessor/constant so env-name churn fails the build by construction (SC#4). Refreeze goldens intentionally with `go test … -update`.

### Claude's Discretion

- Exact `buildOpenWebUIView` signature shape (extra params vs a small web-search input struct) — keep consistent with the existing `memory.MemoryRenderInput` handoff style.
- Whether the single-emit `ENABLE_PERSISTENT_CONFIG=False` refactor uses a trailing-append helper or an explicit ordered assembly — implementation detail, as long as it emits exactly once and last.
- `QDRANT_API_KEY`-style omissions / placeholder-key conventions follow the existing telemetry/no-auth-sentinel patterns already in `openwebui.go`.

### Deferred Ideas (OUT OF SCOPE)

- **Grounded fetch → embed grounding + inline citations** — the `villa-websafe` loader replacing OWUI's native fetch, ephemeral RAG collection, ctx-budget reservation, offload-assert-under-load, SSRF guard → **Phase 31** (GROUND-01/02/03, GUARD-01/05).
- **Injection-guard sanitization layer** — Unicode normalization, provenance-fencing, heuristic injection classifier → **Phase 32**.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| SRCH-02 | OWUI wired to local SearXNG via OWUI's **native** web search, env-only behind the orchestrate seam (`ENABLE_WEB_SEARCH`, `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count), `ENABLE_PERSISTENT_CONFIG=False` mandatory; search-disabled render byte-identical to v1.4. | Env names verified at the pinned digest (all `ConfigVar`/DB-backed → `ENABLE_PERSISTENT_CONFIG=False` mandatory). URL composed via `fmt` from `cfg.SearxngAddr`/`SearxngPort` (no re-typed literals). Byte-identical-off proven by the existing negative-render tests, which the plan must extend. |
| SRCH-03 | Operator opts into web search per-query/per-session via OWUI's native toggle, tunes result count, honest no-results behavior. | Native UI toggle is an OWUI runtime feature (confirmed in docs) — villa supplies env only, never overrides the toggle. Result count = new `WebSearchResultCount` config field → `WEB_SEARCH_RESULT_COUNT`. Honest no-results is a native OWUI property (search→embed→retrieve→inject; no results → no context); asserted by UAT, no villa guard. |
</phase_requirements>

## OWUI Env Contract — Verified Against Source at the Pinned Digest

**Pin chain (verified):**
- Pinned image const (`internal/orchestrate/openwebui.go:41`): `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a50cfbac23da3b16f96bc968fd757b26dc9e54e93813d61768ea9184e`.
- On the live dev box: `podman image inspect … --format '{{index .Config.Labels "org.opencontainers.image.revision"}}'` → git revision **`02dc3e689ceac915a870b373318b99c029ddf603`**, `…image.version` = `main`. `[VERIFIED: podman image inspect on dev box]`
- Source verified at exactly that revision: `backend/open_webui/config.py` (4047 lines) and `backend/open_webui/retrieval/web/searxng.py` fetched from `raw.githubusercontent.com/open-webui/open-webui/02dc3e68…/`. `[VERIFIED: OWUI source @ pinned revision]`

**Env keys to emit (the SC#1 frozen set), verified verbatim from `config.py`:**

| OWUI env var | `config.py` line | DB ConfigVar key | Default | villa value | Provenance |
|--------------|------------------|------------------|---------|-------------|------------|
| `ENABLE_WEB_SEARCH` | 1529–1533 | `rag.web.search.enable` | `False` | `True` | `[VERIFIED]` |
| `WEB_SEARCH_ENGINE` | 1535–1539 | `rag.web.search.engine` | `''` | `searxng` | `[VERIFIED]` |
| `SEARXNG_QUERY_URL` | 1630–1634 | `rag.web.search.searxng_query_url` | `''` | `http://{SearxngAddr}:{SearxngPort}/search?q=<query>&format=json` (composed) | `[VERIFIED]` |
| `WEB_SEARCH_RESULT_COUNT` | 1554–1558 | `rag.web.search.result_count` | `3` | `{WebSearchResultCount}` (default 3) | `[VERIFIED]` |
| `ENABLE_PERSISTENT_CONFIG` | 142 | (gate, not a ConfigVar) | `True` | `False` (mandatory, last) | `[VERIFIED]` |

**Resolved verification answers (to the CONTEXT.md re-verify mandate):**

1. **Enable key:** `ENABLE_WEB_SEARCH` (current). `ENABLE_RAG_WEB_SEARCH` is **NOT present** at this revision — no `os.environ` fallback exists, so the old name would be silently ignored. Use `ENABLE_WEB_SEARCH`. `[VERIFIED]`
2. **Engine key:** `WEB_SEARCH_ENGINE`, value `searxng`. `RAG_WEB_SEARCH_ENGINE` is gone. `[VERIFIED]`
3. **SearXNG URL key:** `SEARXNG_QUERY_URL`. The `<query>` token is OWUI's literal placeholder. **Digest-pinned behavior (Pitfall 1):** at this revision the provider strips the query string when `<query>` is present and supplies `format=json` itself — so `&format=json` in the URL is a no-op here, but harmless to keep frozen.  `[VERIFIED]`
4. **Result-count key:** `WEB_SEARCH_RESULT_COUNT`, default `3`, plain `int(os.getenv(...))`. `RAG_WEB_SEARCH_RESULT_COUNT` is gone. `WEB_SEARCH_CONCURRENT_REQUESTS` exists (line 1579, default `0`) but is **not in scope** — do not emit it (concurrency tuning belongs to Phase 31's fetch path; default 0 is fine). `[VERIFIED]`
5. **ConfigVar vs plain env:** ALL of `ENABLE_WEB_SEARCH` / `WEB_SEARCH_ENGINE` / `SEARXNG_QUERY_URL` / `WEB_SEARCH_RESULT_COUNT` are `ConfigVar` (DB-backed PersistentConfig). `ENABLE_PERSISTENT_CONFIG` defaults to `True` (line 142) and seeds these into the DB on first boot. **Therefore `ENABLE_PERSISTENT_CONFIG=False` is mandatory** to keep env authoritative — identical rationale to the Phase-20 memory block (T-20-01). `[VERIFIED]`
6. **Native toggle + honesty:** Web search is exposed as a per-session UI toggle (the "Integrations"/web-search control next to the chat `+`), resetting on reload/conversation switch — an OWUI runtime feature; villa supplies env only and never overrides it. `[CITED: docs.openwebui.com/features/.../searxng]` Honesty is governed by the native search→embed→retrieve→inject path: `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` and `BYPASS_WEB_SEARCH_WEB_LOADER` both default `False` (config.py 1541–1552), so by default results are embedded+retrieved into context; no results → no injected context → no fabricated citation. **Do NOT set the BYPASS_* flags** in Phase 30 — they alter the native path and are Phase-31 territory. `[VERIFIED]`

## Standard Stack

No new packages. Env-only wiring against the already-pinned OWUI image and the already-rendered Phase-29 SearXNG service.

### Core
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…ea9184e` (rev `02dc3e68`) | pinned | Chat UI + native web search consumer | Already the project's pinned managed service (`openwebui.go:41`); web-search keys live in its `config.py` |
| `ghcr.io/searxng/searxng@sha256:ed29454e…56d0` | pinned | Local metasearch backend OWUI calls | Rendered in Phase 29; `search.formats: [html, json]` already proven |
| Go stdlib `fmt`, `text/template`, `testing` | 1.26.2 | URL composition, render, golden tests | Existing orchestrate machinery; no new deps |

**Installation:** None — no `go get`, no new image pull. The pinned digests are present.

## Package Legitimacy Audit

**Not applicable.** This phase installs no external packages and adds no new container image. It composes env strings against the already-pinned, already-audited OWUI and SearXNG images. No registry verification required.

## Architecture Patterns

### System Architecture Diagram

```
operator (browser, loopback :3000)
  │  per-session "Web Search" toggle (OWUI native UI — SRCH-03)
  ▼
villa-openwebui (container)            env (from villa config, ENABLE_PERSISTENT_CONFIG=False → authoritative)
  ├─ ENABLE_WEB_SEARCH=True            ┐
  ├─ WEB_SEARCH_ENGINE=searxng         ├─ appended ONLY when cfg.WebSearchEnabled  ── byte-identical-off when not
  ├─ SEARXNG_QUERY_URL=http://villa-searxng:8080/search?q=<query>&format=json
  ├─ WEB_SEARCH_RESULT_COUNT={cfg.WebSearchResultCount, default 3}
  └─ ENABLE_PERSISTENT_CONFIG=False    ┘ (single trailing emit when memory OR web-search on)
  │
  │  native search→embed→retrieve→inject (BYPASS_* both False = default; Phase 30 leaves native path as-is)
  ▼  HTTP GET over villa.network (container DNS, no host port — PRIV-01)
villa-searxng (container)  /search?q=…&format=json  → JSON results
  │
  ▼  bounded engine allowlist (Phase-29 SRCH-04)
upstream engines (duckduckgo / brave / wikipedia / wikidata)
```
*Render-time data flow (who composes what):* `render.go` reads `in.Cfg` → builds `memory.RenderView(in.Cfg)` (existing) AND threads `WebSearchEnabled`/`SearxngAddr`/`SearxngPort`/`WebSearchResultCount` → `buildOpenWebUIView(...)` → ordered `[]envPair` → `openwebui.container.tmpl` → frozen golden.

### Recommended Project Structure (files touched — NO new files except the golden)
```
internal/orchestrate/
├── openwebui.go        # EXTEND buildOpenWebUIView: WebSearch input + gated env block + single-emit ENABLE_PERSISTENT_CONFIG
├── render.go           # EXTEND the ~line 146 buildOpenWebUIView(...) call to thread web-search inputs from in.Cfg
├── searxng.go          # READ-ONLY here for identity; the URL is composed from cfg, NOT from a re-typed searxng const
├── render_test.go      # EXTEND TestRenderOpenWebUITelemetryFrozen with a "websearch-on" case (= the SC#4 drift test)
│                       #   + add TestRenderOpenWebUIWebSearchContainerGolden
├── searxng_test.go     # UPDATE TestRenderByteIdenticalWhenWebSearchOff on-render expectation (OWUI unit now differs when search on)
└── testdata/
    └── villa-openwebui.container.websearch.golden   # NEW (mirror …memory.golden); only deliberate re-freeze
internal/config/
└── villaconfig.go      # ADD WebSearchResultCount int `toml:"web_search_result_count,omitzero"` (default 3); extend
                        #   defaultConfig(), normalizeVilla() self-heal, marshalVilla() omit-when-off
```

### Pattern 1: Config-gated ordered env append (mirror Phase-20 memory block)
**What:** Append a single ordered group of `envPair`s inside `buildOpenWebUIView`, gated on `webSearchEnabled`, after the base block (and independent of the memory block).
**When to use:** Always for OWUI env evolution — preserves byte-identical-off and golden determinism.
**Example:**
```go
// Source: pattern lifted verbatim from internal/orchestrate/openwebui.go:145-198 (memoryEnabled block)
if webSearchEnabled {
    // SRCH-02 native web-search group. URL composed from resolved config (WR-01);
    // NO re-typed villa-searxng/port literal — searxngAddr/searxngPort flow from cfg,
    // so TestSeamGrepGate stays green (config-sourced values, not GPU/image tokens).
    env = append(env,
        envPair{Key: "ENABLE_WEB_SEARCH", Value: "True"},
        envPair{Key: "WEB_SEARCH_ENGINE", Value: "searxng"},
        envPair{Key: "SEARXNG_QUERY_URL",
            Value: fmt.Sprintf("http://%s:%d/search?q=<query>&format=json", searxngAddr, searxngPort)},
        envPair{Key: "WEB_SEARCH_RESULT_COUNT", Value: strconv.Itoa(webSearchResultCount)},
    )
}
// ENABLE_PERSISTENT_CONFIG=False emitted ONCE, LAST, when memory OR web-search on (D-04) — see Pattern 2.
```

### Pattern 2: Single trailing `ENABLE_PERSISTENT_CONFIG=False` emit (the D-04 refactor)
**What:** Today `ENABLE_PERSISTENT_CONFIG=False` lives **inside** the memory block as the last entry (`openwebui.go:194`). It must move to a single tail emit gated on `memoryEnabled || webSearchEnabled`, so it is emitted exactly once and last regardless of which group(s) are on.
**When to use:** This phase only (the refactor). It is load-bearing: without it OWUI seeds the ConfigVars to its DB on first boot and ignores env afterward — config would no longer be the single source of truth (T-20-01 rationale, now extended to web search).
**Example:**
```go
// After both optional groups are appended:
if memoryEnabled || webSearchEnabled {
    env = append(env, envPair{Key: "ENABLE_PERSISTENT_CONFIG", Value: "False"})
}
```
**Golden impact:** the **memory-on golden changes** — `ENABLE_PERSISTENT_CONFIG=False` moves from inside the memory block to the single tail emit. If the memory block already emits it last and nothing else follows, the byte output for memory-on-only is unchanged (it is still last). **Verify**: render memory-on (web off) and diff against `villa-openwebui.container.memory.golden`; if the line position is identical, the memory golden needs NO re-freeze. The plan must confirm this, not assume it.

### Pattern 3: New config field with omit-when-off discipline (mirror SearxngPort)
**What:** `WebSearchResultCount int` follows the established v1.5 web-search field discipline: `omitzero` tag, defaulted in `defaultConfig()`, self-healed in `normalizeVilla()` (zero → default 3), and zeroed in `marshalVilla()` when `!WebSearchEnabled` (byte-identical-off on disk).
**Example:**
```go
// villaconfig.go — struct field (mirror SearxngPort, lines 143-146):
// WebSearchResultCount is the OWUI native web-search result count (WEB_SEARCH_RESULT_COUNT).
// Default 3 (conservative ctx budget ahead of Phase 31). ,omitzero (NOT ,omitempty): BurntSushi/toml
// only drops a zero int with omitzero — required for byte-identical-off.
WebSearchResultCount int `toml:"web_search_result_count,omitzero"`

// defaultConfig(): add WebSearchResultCount: 3  (next to WebSearchEnabled/SearxngAddr/SearxngPort)
// normalizeVilla(): if cfg.WebSearchResultCount == 0 { cfg.WebSearchResultCount = d.WebSearchResultCount }
// marshalVilla() !WebSearchEnabled block (lines 339-343): add c.WebSearchResultCount = 0
```
**Caution:** default 3 collides with OWUI's own `WEB_SEARCH_RESULT_COUNT` default of 3 — intentional and fine; villa makes it explicit and authoritative.

### Anti-Patterns to Avoid
- **Re-typing `villa-searxng`/`8080` in the URL.** Compose with `fmt` from `searxngAddr`/`searxngPort` threaded from `cfg` — a re-typed host literal in `openwebui.go` is fine for the seam gate (it's a managed-service file, not an inference caller) but DRIFTS from config; use the config-sourced values so the OWUI target can never diverge from the rendered SearXNG identity (the same WR-01 discipline `buildSearxngView` already follows).
- **Emitting `ENABLE_PERSISTENT_CONFIG=False` twice** (once per group) — duplicates the line; OWUI honors only env-vs-DB, but the golden + the count-check in `TestRenderOpenWebUITelemetryFrozen` will (correctly) fail. One tail emit only.
- **Setting `BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL` / `BYPASS_WEB_SEARCH_WEB_LOADER` / `WEB_LOADER_ENGINE=external`** — those alter the native fetch/grounding path and belong to Phase 31. Phase 30 leaves the native path as-is (D-06).
- **Adding a villa system prompt / "answer honestly" env** — there is none; honesty is the native path's property, asserted by UAT (D-06).
- **Re-freezing the memory-off golden** — it MUST stay byte-identical to v1.4 (D-07). Only the NEW web-search golden (and possibly the memory-on golden if the line moves) is a deliberate re-freeze.

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Honest no-results behavior | A villa fabrication-guard / grounding prompt | OWUI's native search→retrieve→inject path | Native path already returns no context on no results; building a guard here pre-empts Phase 31/32 and adds untested surface (D-06) |
| Per-query web-search opt-in | A villa CLI flag / dashboard control | OWUI's native per-session UI toggle | SRCH-03 is satisfied by the env wiring; the toggle is an OWUI runtime feature |
| `format=json` enforcement from the URL | Trusting `&format=json` in `SEARXNG_QUERY_URL` | SearXNG's `settings.yml` `formats: [html, json]` (Phase 29) + OWUI's own `format=json` param | OWUI strips the URL query string at this digest (Pitfall 1) — the JSON contract is NOT carried by the URL suffix |
| Env-name churn detection | Manual review on image bump | The existing `TestRenderOpenWebUITelemetryFrozen` bidirectional binding, extended | It already counts + full-set-matches env lines against `buildOpenWebUIView`; extend it (SC#4 "caught by construction") |

**Key insight:** Everything load-bearing here already exists as a proven pattern (Phase-20 memory block, Phase-29 SearXNG, the byte-frozen golden discipline). The risk is NOT in inventing — it is in (a) using the exact verified env names, (b) the single-emit `ENABLE_PERSISTENT_CONFIG` refactor not duplicating/dropping the key, and (c) not silently breaking the byte-identical-off contract.

## Runtime State Inventory

> Phase 30 is a render/config change (not a rename/migration), but OWUI's DB-backed ConfigVars create a genuine runtime-state concern worth stating explicitly.

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data | OWUI persists web-search ConfigVars (`rag.web.search.*`) into its SQLite DB on the durable `villa-openwebui` volume **on first boot** when `ENABLE_PERSISTENT_CONFIG=True` (the default). | `ENABLE_PERSISTENT_CONFIG=False` (mandatory) makes env authoritative every boot. For an **existing install** that previously booted memory-on (already had `ENABLE_PERSISTENT_CONFIG=False`), env stays authoritative — no migration. For a hypothetical install that booted with persistent-config true, the DB value could shadow env; villa has always emitted `=False` when any ConfigVar group is on, so the documented path is safe. **No data migration task** — the gate handles it. |
| Live service config | None outside git/config. The OWUI env is fully regenerated from `config.toml` via the Quadlet unit; no UI-only state is the source of truth. | None — config is the single source of truth. |
| OS-registered state | The `villa-openwebui.container` Quadlet unit is regenerated by `WriteUnits`; a `systemctl --user restart villa-openwebui.service` (or full re-up) applies the new env after re-render. | Plan/UAT: restart the OWUI service so the new env takes effect (units are regenerated, not hand-edited). |
| Secrets/env vars | No new secret. The SearXNG secret (`searxng.env`, 0600) is unchanged from Phase 29; OWUI needs no SearXNG API key for a private instance. | None. |
| Build artifacts | None — pure Go re-render + new golden. | None. |

**Nothing found in category:** "Live service config" and "Secrets/env vars" — verified: config is the single source of truth; no new secret is introduced.

## Common Pitfalls

### Pitfall 1: Assuming OWUI forwards `&format=json` from `SEARXNG_QUERY_URL` (DIGEST-PINNED)
**What goes wrong:** A plan or verification step assumes the URL's `&format=json` reaches SearXNG. At this exact digest it does NOT.
**Why it happens:** `backend/open_webui/retrieval/web/searxng.py` at rev `02dc3e68` does: `if '<query>' in query_url: query_url = query_url.split('?')[0]` (strips the whole query string), then builds `params = {'q': query, 'format': 'json', 'pageno': 1, ...}` itself. So the `q=<query>&format=json` suffix is discarded; OWUI supplies `q` and `format=json` from params.
**How to avoid:** Freeze `&format=json` in the golden anyway (SC#1 literal requirement + future-robust), but DO NOT rely on it in any verification claim. The JSON contract is guaranteed by SearXNG's `settings.yml formats: [html, json]` (Phase-29 proof) + OWUI's own `format=json` param. UAT must confirm real results come back, not that the URL carries the suffix.
**Warning signs:** A verification step that greps the SearXNG access log for `format=json` and ties PASS to it — the param IS present (OWUI adds it), but for the wrong reason; don't couple the proof to the URL suffix.

### Pitfall 2: Duplicating or dropping `ENABLE_PERSISTENT_CONFIG=False` in the refactor
**What goes wrong:** The single-emit refactor (D-04) accidentally emits the key inside both the memory block and the web-search block (duplicate) or drops it when web-search is on but memory is off.
**Why it happens:** It currently lives inside the `memoryEnabled` block; moving it to a `memoryEnabled || webSearchEnabled` tail emit while ALSO leaving it in the memory block.
**How to avoid:** Remove it from the memory block entirely; emit it once after both optional groups under `if memoryEnabled || webSearchEnabled`. The count assertion in `TestRenderOpenWebUITelemetryFrozen` (`strings.Count(c.Text, "Environment=") == len(env)`) catches a duplicate; a dedicated "web-search on, memory off" test case catches a drop.
**Warning signs:** Two `ENABLE_PERSISTENT_CONFIG` lines in the web-search golden; or the line absent in a memory-off+search-on render.

### Pitfall 3: Breaking byte-identical-off (the hard SC#4 bar)
**What goes wrong:** The memory-off OWUI golden or the on-disk config changes when web search is off, violating "byte-identical to v1.4."
**Why it happens:** Defaulting `WebSearchResultCount` without the `omit-when-off` zeroing in `marshalVilla`, or appending the env group unconditionally.
**How to avoid:** Gate the env group strictly on `webSearchEnabled`. Add `c.WebSearchResultCount = 0` to the `!WebSearchEnabled` block in `marshalVilla` (lines 339-343). Keep the `omitzero` tag (NOT `omitempty` — toml drops a zero int only with `omitzero`).
**Warning signs:** `TestRenderByteIdenticalWhenWebSearchOff` fails; a config round-trip test shows `web_search_result_count = 0` written when search is off.

### Pitfall 4: `TestSeamGrepGate` false positive from a re-typed SearXNG host literal
**What goes wrong:** Composing the URL with a literal `"villa-searxng"` or `"8080"` in `openwebui.go` instead of the config-threaded values.
**Why it happens:** Convenience. `openwebui.go` is on the seam allowlist, so it wouldn't actually trip the gate — but it would DRIFT from config (the real bug).
**How to avoid:** Thread `searxngAddr`/`searxngPort` from `in.Cfg` through the `buildOpenWebUIView` signature and `fmt.Sprintf` the URL (WR-01), exactly as `buildSearxngView(in.Cfg.SearxngAddr)` already does.
**Warning signs:** `TestRenderSearxngIsConfigDriven`-style test for OWUI (custom `SearxngAddr` must surface in the OWUI URL) fails.

### Pitfall 5: Stale on-render expectations in existing searxng tests
**What goes wrong:** `searxngFixtureInput()` has `WebSearchEnabled: true` (memory off). Today the OWUI unit it renders has NO web-search env. After this phase it WILL — so `TestRenderByteIdenticalWhenWebSearchOff` (the on-render branch) and any test asserting the OWUI unit text under that fixture must be updated.
**How to avoid:** Audit every test that renders `searxngFixtureInput()` and inspects the OWUI unit; update expectations. The `len(units)`/order assertions are unaffected (still 6 units, searxng last) — only the OWUI unit's env content changes.
**Warning signs:** Pre-existing tests fail on the OWUI unit content (not the searxng unit) after wiring.

## Code Examples

### Threading web-search inputs from render.go (extend the ~line 146 call)
```go
// Source: internal/orchestrate/render.go:145-146 (current memory-aware call)
mv := memory.RenderView(in.Cfg)
owuiContainerText, err := execTemplate(tmpl, "openwebui.container.tmpl",
    buildOpenWebUIView(mv, in.Cfg.MemoryEnabled,
        in.Cfg.WebSearchEnabled, in.Cfg.SearxngAddr, in.Cfg.SearxngPort, in.Cfg.WebSearchResultCount))
// (Discretion D: a small webSearchRenderInput struct is an acceptable alternative to extra params.)
```

### The drift test = extend the existing bidirectional binding (SC#4)
```go
// Source: internal/orchestrate/render_test.go:425-440 — add a third case binding each
// web-search env KEY to buildOpenWebUIView's output. Because the test asserts EVERY
// Key=Value is rendered AND counts Environment= lines == len(env), an env-name churn
// (e.g. someone "fixes" ENABLE_WEB_SEARCH back to ENABLE_RAG_WEB_SEARCH) fails by construction.
{
    name: "websearch-on",
    in:   searxngFixtureInput(), // WebSearchEnabled:true, memory off
    env:  buildOpenWebUIView(memory.RenderView(searxngFixtureInput().Cfg), false,
              true, "villa-searxng", 8080, 3).Env,
},
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| `ENABLE_RAG_WEB_SEARCH` / `RAG_WEB_SEARCH_ENGINE` / `RAG_WEB_SEARCH_RESULT_COUNT` | `ENABLE_WEB_SEARCH` / `WEB_SEARCH_ENGINE` / `WEB_SEARCH_RESULT_COUNT` | OWUI ≤ this pinned rev; old names removed (no fallback) | Use the new names verbatim; old names are silently ignored |
| `SEARXNG_QUERY_URL` query string forwarded verbatim | URL query string **stripped** when `<query>` present; OWUI sets `format=json`/`q`/`pageno` itself | At rev `02dc3e68` (`searxng.py`) | `&format=json` in the URL is a no-op at this digest (Pitfall 1) |
| Web-search keys as plain env | All are DB-backed `ConfigVar` | standing | `ENABLE_PERSISTENT_CONFIG=False` mandatory to keep env authoritative |

**Deprecated/outdated:**
- `ENABLE_RAG_WEB_SEARCH` and the `RAG_WEB_SEARCH_*` family — gone at the pinned revision; do not emit.
- Relying on `&format=json` in the OWUI SearXNG URL to enable JSON — not how it works at this digest.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | The memory-on golden does NOT need re-freezing after the single-emit `ENABLE_PERSISTENT_CONFIG` refactor (line stays last in the same position). | Pattern 2 | LOW — the plan must render-and-diff to confirm; if it does move, re-freeze the memory-on golden deliberately with `-update`. Caught by the existing memory golden test, never silent. |
| A2 | `WEB_SEARCH_CONCURRENT_REQUESTS` is correctly left unset (default 0) for Phase 30. | OWUI Env Contract §4 | LOW — concurrency tuning is a Phase-31 fetch-path concern; default 0 is OWUI's own default. If results feel slow in UAT, Phase 31 owns it. |
| A3 | OWUI's native per-session toggle (no env required to expose it) is present at this digest and satisfies SRCH-03's opt-in. | OWUI Env Contract §6 | LOW — confirmed in OWUI docs; UAT verifies the toggle appears and gates search per-query. |

## Open Questions

1. **Does the memory-on golden change byte-for-byte after the D-04 refactor?**
   - What we know: `ENABLE_PERSISTENT_CONFIG=False` is currently the last line of the memory block; moving it to a tail emit (memory on, web off) should leave it last in the same position.
   - What's unclear: whether any intermediate refactor reorders lines.
   - Recommendation: render memory-on/web-off and diff against `villa-openwebui.container.memory.golden` as the FIRST verification step; if unchanged, do not `-update` it. (A1.)

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| Pinned OWUI image (rev `02dc3e68`) | env-key verification + on-hardware UAT | ✓ | `:main@sha256:7f1b0a1a…` | — (already pulled on dev box) |
| Pinned SearXNG image (Phase 29) | the search backend OWUI calls | ✓ | `@sha256:ed29454e…` | — |
| Go 1.26.2 toolchain | build + golden tests | ✓ | 1.26.2 | — |
| Rootless Podman + `systemctl --user` (dev box) | on-hardware UAT (real query) | ✓ | gfx1151 dev host | Off-hardware: golden/drift tests fully cover the render contract |

**Missing dependencies with no fallback:** None.
**Missing dependencies with fallback:** Off-hardware execution — the render/golden/drift/config tests are fully exercisable without a live host; only the honest-no-results + per-session-toggle UAT need the dev box.

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven + byte-for-byte goldens; no third-party assert/mocking lib) |
| Config file | none — `go test` driven; goldens in `internal/orchestrate/testdata/` |
| Quick run command | `go test ./internal/orchestrate/ ./internal/config/` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| SRCH-02 | Search-ON OWUI unit freezes the exact env group (`ENABLE_WEB_SEARCH`/`WEB_SEARCH_ENGINE=searxng`/`SEARXNG_QUERY_URL…&format=json`/`WEB_SEARCH_RESULT_COUNT`/`ENABLE_PERSISTENT_CONFIG=False` last) | golden | `go test ./internal/orchestrate/ -run TestRenderOpenWebUIWebSearchContainerGolden` | ❌ Wave 0 (new golden + test) |
| SRCH-02 | Env-name churn caught by construction (drift test binds each KEY to its accessor) | unit | `go test ./internal/orchestrate/ -run TestRenderOpenWebUITelemetryFrozen` | ✅ extend (add "websearch-on" case) |
| SRCH-02 | URL is config-driven (custom `SearxngAddr`/`SearxngPort` surfaces in the OWUI URL — WR-01) | unit | `go test ./internal/orchestrate/ -run TestRenderOpenWebUIWebSearchConfigDriven` | ❌ Wave 0 |
| SRCH-02 | `ENABLE_PERSISTENT_CONFIG=False` emitted exactly once + last when memory OR web-search on (incl. web-on/memory-off) | unit | `go test ./internal/orchestrate/ -run TestRenderOpenWebUIPersistentConfigSingleEmit` | ❌ Wave 0 |
| SRCH-02 / PRIV-07 | Search-OFF render byte-identical to v1.4 (memory-off + memory-on goldens unchanged) | golden | `go test ./internal/orchestrate/ -run 'TestRenderOpenWebUIContainerGolden|TestRenderOpenWebUIMemoryContainerGolden|TestRenderByteIdenticalWhenWebSearchOff'` | ✅ extend (update on-render expectation, Pitfall 5) |
| SRCH-02 / PRIV-07 | Config on-disk byte-identical-off (`web_search_result_count` omitted when search off) | unit | `go test ./internal/config/ -run TestMarshalOmitsWebSearchWhenOff` | ⚠️ likely exists for other web-search fields — extend to cover the new field |
| SRCH-03 | Result-count knob defaults to 3, self-heals zero→3, round-trips when on | unit | `go test ./internal/config/ -run TestWebSearchResultCountDefaultAndHeal` | ❌ Wave 0 |
| SRCH-03 | Per-session native toggle gates search per-query | manual (UAT) | on-hardware: toggle off → no search; toggle on → search runs | manual-only (OWUI runtime UI; no automatable seam) |
| SRCH-03 | Honest no-results (a real no-results query yields NO fabricated cited answer) | manual (UAT) | on-hardware: ask an obscure no-result query with search on; assert no invented citations | manual-only (model behavior; D-06) |

### Sampling Rate
- **Per task commit:** `go test ./internal/orchestrate/ ./internal/config/`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + on-hardware UAT (toggle + honest-no-results) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/orchestrate/testdata/villa-openwebui.container.websearch.golden` — covers SRCH-02 (frozen search-on env group); generate with `-update` after the render is correct.
- [ ] `internal/orchestrate/render_test.go` — `TestRenderOpenWebUIWebSearchContainerGolden`, `TestRenderOpenWebUIWebSearchConfigDriven`, `TestRenderOpenWebUIPersistentConfigSingleEmit`; extend `TestRenderOpenWebUITelemetryFrozen` with a "websearch-on" case (the SC#4 drift test).
- [ ] `internal/orchestrate/searxng_test.go` — update `TestRenderByteIdenticalWhenWebSearchOff` on-render expectation (OWUI unit now carries web-search env when search on — Pitfall 5).
- [ ] `internal/config/villaconfig_test.go` — `TestWebSearchResultCountDefaultAndHeal` + extend the existing omit-when-off marshal test to assert `web_search_result_count` is dropped when `!WebSearchEnabled`.
- [ ] Framework install: none — Go `testing` already in use.

## Security Domain

> `security_enforcement` is enabled (no `false` in config). This phase is env-wiring; the relevant controls are continuity, not new crypto.

### Applicable ASVS Categories

| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | OWUI `WEBUI_AUTH=True` unchanged; no new auth surface |
| V3 Session Management | no | Per-session toggle is OWUI-native, client-side; villa adds nothing |
| V4 Access Control | no | Loopback-only OWUI publish (`127.0.0.1:3000`) unchanged (PRIV-01) |
| V5 Input Validation | yes | URL composed via `fmt` from catalog/config values only; no shell interpolation; the `<query>` token is OWUI's literal placeholder, not user-interpolated by villa |
| V6 Cryptography | no | No new secret; SearXNG `searxng.env` (0600) from Phase 29 unchanged; OWUI needs no SearXNG API key for a private instance |

### Known Threat Patterns for {Go render + container env}

| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Secret leaking into a 0644 unit | Information Disclosure | No secret is rendered into the OWUI env block; `SEARXNG_QUERY_URL` carries no credential (private instance). The only web-search secret (SearXNG `secret_key`) stays in the 0600 `searxng.env` (Phase-29 T-29-02), never touched here. |
| Routable bind / data egress when "off" | Information Disclosure / Spoofing | Web-search env appends ONLY when `WebSearchEnabled`; off-render byte-identical to v1.4 → zero-outbound posture unchanged (PRIV-07). OWUI→SearXNG is container-DNS over `villa.network`, no host port. |
| Env-name regression silently disabling the privacy/telemetry posture | Tampering | The bidirectional `TestRenderOpenWebUITelemetryFrozen` count+full-set check re-confirms the telemetry-kill set survives in the new web-search-on view too. |
| Prompt injection via fetched pages | Tampering / Elevation | **Explicitly NOT mitigated in Phase 30** — native fetch path left as-is; sanitization/fencing/classifier is Phase 32 (GUARD-*). The product never claims injection immunity. Do not add a partial guard here that implies otherwise. |

## Sources

### Primary (HIGH confidence)
- OWUI `backend/open_webui/config.py` @ git rev `02dc3e689ceac915a870b373318b99c029ddf603` (the pinned digest's revision) — exact env-key names, ConfigVar/DB-backed status, defaults (lines 142, 1529-1634). Fetched + grepped locally.
- OWUI `backend/open_webui/retrieval/web/searxng.py` @ same rev — query-string stripping + self-supplied `format=json` (lines 32-46). Fetched + read locally.
- `podman image inspect ghcr.io/open-webui/open-webui:main` on the dev box — confirmed digest `7f1b0a1a…ea9184e` maps to `:main` rev `02dc3e68`.
- Codebase: `internal/orchestrate/openwebui.go`, `render.go`, `searxng.go`, `internal/config/villaconfig.go`, `internal/orchestrate/render_test.go`, `searxng_test.go`, `testdata/villa-openwebui.container.memory.golden`.

### Secondary (MEDIUM confidence)
- docs.openwebui.com — SearXNG provider env vars + per-session UI toggle behavior (cross-checked against source for the env names).
- Phase 29 artifacts (`29-RESEARCH.md` env-family-evolution row; `searxng.go` JSON-format proof).

### Tertiary (LOW confidence)
- General web tutorials (NVIDIA/Medium/Cloudron) corroborating the current env-var set — used only as cross-checks; superseded by the source verification above.

## Metadata

**Confidence breakdown:**
- OWUI env contract: HIGH — verified against `config.py` AND the SearXNG provider at the exact pinned digest's revision (not training data, not docs alone).
- Architecture/patterns: HIGH — a 1:1 mirror of the in-repo Phase-20 memory block + Phase-29 web-search field discipline; templates read directly.
- Pitfalls: HIGH — Pitfall 1 (URL stripping) and Pitfall 5 (stale fixture) derived from reading the actual source + existing tests.

**Research date:** 2026-06-18
**Valid until:** Tied to the pinned OWUI digest, not a date — re-verify the env contract ONLY if `openWebUIImage` is bumped (the golden + telemetry-frozen test force this re-audit by construction). Stable indefinitely while the digest is pinned.
