# Phase 30: OWUI Native-Search Wiring - Context

**Gathered:** 2026-06-18
**Status:** Ready for planning

> Captured autonomously via `/gsd-discuss-phase 30 --auto`. Every gray area was
> resolved with its recommended option (logged in DISCUSSION-LOG.md). Decisions
> follow established v1.4/v1.5 patterns; the researcher should still re-verify
> the OWUI env names at the pinned digest (see Canonical References).

<domain>
## Phase Boundary

Wire the operator's Open WebUI to the **already-rendered** local SearXNG service
(Phase 29) via OWUI's **native** web-search, gated env-only behind the
orchestrate seam. The SearXNG service unit already renders when
`cfg.WebSearchEnabled` is true (`internal/orchestrate/render.go:222`); this phase
adds the corresponding **OWUI-side env block** so OWUI actually calls SearXNG.

**In scope:** the OWUI web-search env group (`ENABLE_WEB_SEARCH`,
`WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count,
`ENABLE_PERSISTENT_CONFIG=False`), per-query/per-session opt-in via OWUI's native
toggle, a result-count tuning knob, honest no-results behavior, and a
byte-identical search-off render proven by golden + drift tests.

**Out of scope (later phases):** the `villa-websafe` grounded-fetch loader and
RAG/citation grounding (Phase 31), web-search ctx-budget reservation +
offload-assert-under-load (Phase 31), the injection-guard sanitization layer
(Phase 32). Phase 30 is the **search wiring only** — OWUI's native fetch/grounding
path is left as-is and replaced in Phase 31.

</domain>

<decisions>
## Implementation Decisions

### Env block construction (mirror the Phase-20 memory pattern)
- **D-01:** Append the web-search env group inside `buildOpenWebUIView`
  (`internal/orchestrate/openwebui.go`) as ONE ordered block, gated on
  `WebSearchEnabled`, exactly mirroring the existing `memoryEnabled` append
  (Phase-20 D-02 append-only discipline). `buildOpenWebUIView`'s signature gains
  the web-search inputs (enabled flag + `SearxngAddr`/`SearxngPort` + result
  count); the `render.go:146` caller threads them from resolved `in.Cfg` (config
  is the single source of truth — no re-typed host literals, keeps
  `TestSeamGrepGate` green, WR-01).
- **D-02:** The env keys are `ENABLE_WEB_SEARCH=True`,
  `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL=<composed>`, the result-count
  key, and `ENABLE_PERSISTENT_CONFIG=False`. **Researcher MUST re-verify the
  exact key names at the pinned OWUI digest** — OWUI churned this family
  (`ENABLE_RAG_WEB_SEARCH`/`RAG_WEB_SEARCH_ENGINE` → `ENABLE_WEB_SEARCH`/
  `WEB_SEARCH_ENGINE`); the result-count key is likely `WEB_SEARCH_RESULT_COUNT`
  but confirm against `config.py` at the pinned digest before freezing the golden.

### SEARXNG_QUERY_URL composition
- **D-03:** Compose the query URL from resolved config, never re-typed:
  `http://{cfg.SearxngAddr}:{cfg.SearxngPort}/search?q=<query>&format=json`.
  `<query>` is OWUI's literal substitution placeholder (kept verbatim).
  `&format=json` is mandatory (matches the Phase-29 SearXNG JSON-format proof).
  Defaults resolve to `villa-searxng:8080` from `villaconfig.go`.

### ENABLE_PERSISTENT_CONFIG=False (single authoritative emit)
- **D-04:** `ENABLE_PERSISTENT_CONFIG=False` is **load-bearing and MUST appear
  exactly once**, as the **last** Environment entry, whenever memory OR
  web-search is enabled. Today it lives inside the `memoryEnabled` block
  (`openwebui.go`, "KEEP LAST"). Refactor so it is emitted once at the tail when
  either group is on — never duplicated, never dropped when web-search is on but
  memory is off. Without it OWUI seeds the ConfigVars to its DB once and ignores
  env after first boot (config would NOT be the single source of truth — a phase
  failure, same rationale as Phase-20 D-03 / T-20-01).

### Result-count exposure & default
- **D-05:** Add a new config field (e.g. `WebSearchResultCount int`,
  `toml:"web_search_result_count,omitzero"`) to `VillaConfig`, default **3**,
  with the same **omit-when-off** discipline as the other web-search fields
  (`villaconfig.go` zeroes web-search fields on the by-value marshal copy when
  `!WebSearchEnabled`, lines ~333-340) so the search-off on-disk config stays
  byte-identical to v1.4. The operator tunes the count via `config.toml` (single
  source of truth; authoritative because `ENABLE_PERSISTENT_CONFIG=False`), and
  toggles search **on/off per-query/per-session** via OWUI's native UI toggle
  (SRCH-03). Default 3 keeps the context budget conservative ahead of Phase 31's
  ctx-budget reservation.

### Honest no-results behavior (SC#3 / SRCH-03)
- **D-06:** Rely on OWUI's **native** web-search behavior for honesty: search
  results are injected as retrieval context; no results → no injected context →
  the model answers from base knowledge or states it cannot find current info.
  Phase 30 adds **no** villa system-prompt override or fabrication-guard env.
  Honesty is asserted by **UAT** (a real no-results query must not yield a
  fabricated cited answer). Stronger grounding + inline citations to live URLs
  land in Phase 31 (the `villa-websafe` fetch path); injection screening in
  Phase 32. Do not pre-build either here.

### Byte-identical-off + drift test (SC#4)
- **D-07:** Search-off render stays **byte-identical to v1.4**: the web-search
  env group only appends when `WebSearchEnabled`, so the existing OWUI goldens
  (memory-off + memory-on) are unchanged when search is off — prove via the
  existing negative-render test.
- **D-08:** Add a new search-on OWUI container golden (e.g.
  `villa-openwebui.container.websearch.golden`, mirroring
  `villa-openwebui.container.memory.golden`) that freezes the appended
  web-search env group, AND a **drift test** that binds each web-search env KEY
  string to its orchestrate accessor/constant so env-name churn fails the build
  by construction (SC#4 "env-name churn caught by construction"). Refreeze
  goldens intentionally with `go test … -update`.

### Claude's Discretion
- Exact `buildOpenWebUIView` signature shape (extra params vs a small
  web-search input struct) — planner/executor's call; keep it consistent with
  the existing `memory.MemoryRenderInput` handoff style.
- Whether the single-emit `ENABLE_PERSISTENT_CONFIG=False` refactor uses a
  trailing-append helper or an explicit ordered assembly — implementation detail,
  as long as it emits exactly once and last.
- `QDRANT_API_KEY`-style omissions / placeholder-key conventions follow the
  existing telemetry/no-auth-sentinel patterns already in `openwebui.go`.

</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` § "Phase 30: OWUI Native-Search Wiring" — goal + 4 success criteria.
- `.planning/REQUIREMENTS.md` — SRCH-02 (native env-only wiring, byte-identical-off), SRCH-03 (per-query opt-in, tunable count, honest no-results).

### Code to extend (the implementation surface)
- `internal/orchestrate/openwebui.go` — `buildOpenWebUIView` + `envPair` ordered-slice pattern; the `memoryEnabled` append block is the exact template to mirror; `ENABLE_PERSISTENT_CONFIG=False` "KEEP LAST" rationale.
- `internal/orchestrate/render.go` §146 (OWUI render call to extend) and §222 (existing `WebSearchEnabled`-gated SearXNG service render — already done in Phase 29).
- `internal/orchestrate/searxng.go` — SearXNG managed-service constants/identity (container DNS, port) for the query-URL composition contract.
- `internal/config/villaconfig.go` §126-178 (web-search field defaults: `WebSearchEnabled`, `SearxngAddr=villa-searxng`, `SearxngPort=8080`) and §333-340 (omit-when-off marshal discipline to extend for the new result-count field).

### Patterns & constraints (project conventions)
- `CLAUDE.md` — `--json`/golden byte-freeze + schema-bump discipline; `TestSeamGrepGate` (no backend/host literals leaking into callers); orchestrate is the only impure module.
- `.planning/phases/29-owui-native-search-wiring/../29-RESEARCH.md` §"OWUI env-family evolution" row — flags the `ENABLE_WEB_SEARCH`/`WEB_SEARCH_ENGINE` vs older `RAG_WEB_SEARCH` naming and instructs **re-verify env names at the pinned OWUI digest** (the OWUI digest is pinned in `openwebui.go`).
- `.planning/phases/29-coder.../` Phase-29 artifacts (`29-PATTERNS.md`, `29-VERIFICATION.md`, `SECURITY.md`) — the SearXNG service this phase calls; reuse its byte-identical-off + omit-when-off proof patterns.

### External (researcher to fetch/verify)
- Open WebUI environment-configuration docs / `backend/open_webui/config.py` **at the pinned digest** `ghcr.io/open-webui/open-webui:main@sha256:7f1b0a1a…ea9184e` — authoritative source for the exact web-search env key names, result-count key, and native-toggle behavior.

</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `buildOpenWebUIView` + `envPair` ordered-slice (`openwebui.go`): the
  `memoryEnabled` append block is a 1:1 template for the web-search block
  (ordered, append-only, golden-frozen, single trailing `ENABLE_PERSISTENT_CONFIG`).
- `villaconfig.go` web-search fields + self-heal + omit-when-off marshal copy:
  the new result-count field slots into the established pattern (default, omitzero,
  zeroed when `!WebSearchEnabled`).
- Existing OWUI goldens (`villa-openwebui.container.golden`,
  `…memory.golden`) + the memory-aware frozen telemetry test: the model for the
  new search-on golden + drift test.

### Established Patterns
- Append-only env evolution behind a config flag; byte-identical render when the
  flag is off (Phase-18/20 continuity discipline).
- Config is the single source of truth; host identities composed via `fmt` from
  resolved config, never re-typed (keeps `TestSeamGrepGate` green).
- `ENABLE_PERSISTENT_CONFIG=False` is mandatory + load-bearing for any OWUI
  ConfigVar to be authoritative.

### Integration Points
- `render.go` threads resolved `in.Cfg` (`WebSearchEnabled`, `SearxngAddr`,
  `SearxngPort`, new result-count) into `buildOpenWebUIView`.
- The OWUI env block points at the Phase-29 `villa-searxng` service over
  `villa.network` by container DNS (never a host bind — PRIV-01).

</code_context>

<specifics>
## Specific Ideas

- SC#1's frozen env set is explicit: `ENABLE_WEB_SEARCH`,
  `WEB_SEARCH_ENGINE=searxng`, `SEARXNG_QUERY_URL…&format=json`, result-count, and
  **mandatory** `ENABLE_PERSISTENT_CONFIG=False` — all frozen in the golden.
- The byte-identical-off bar is hard (SC#4): off-render must equal v1.4 exactly,
  and env-name churn must fail by construction (drift test).

</specifics>

<deferred>
## Deferred Ideas

- **Grounded fetch → embed grounding + inline citations** — the `villa-websafe`
  loader replacing OWUI's native fetch, ephemeral RAG collection, ctx-budget
  reservation, offload-assert-under-load, SSRF guard → **Phase 31** (GROUND-01/02/03,
  GUARD-01/05).
- **Injection-guard sanitization layer** — Unicode normalization,
  provenance-fencing, heuristic injection classifier → **Phase 32**.
- These were noted only to scope Phase 30 down to native search wiring; not in
  this phase.

</deferred>

---

*Phase: 30-owui-native-search-wiring*
*Context gathered: 2026-06-18*
