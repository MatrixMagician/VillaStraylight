# Phase 34: Web-Search Surfacing (LANDS LAST) - Context

**Gathered:** 2026-06-20
**Status:** Ready for planning
**Mode:** Smart discuss (autonomous) — all 4 grey areas accepted as recommended

<domain>
## Phase Boundary

Surface the finished, proven web-search feature set across the existing read-models — `villa status`/`--json`, the control dashboard, `villa doctor`, and `villa backup`/`restore` — over a SINGLE append-only `status.Report` 4→5 schema bump (one golden re-freeze). The outbound-bounded indicator derives from the REAL `villa verify search` result (cached), never a config bool. This is the LANDS-LAST surfacing phase: it reads a proven, verified feature (Phases 29–33) and freezes the contract once.

Requirements: SURF-04, SURF-05, SURF-06, SURF-07. Depends on Phase 33 (the proven egress-bound verify result; staggered-contract-risk discipline — freeze a finished set).

**In scope:** the status `web_search` block (4→5); the dashboard Web Search panel (design contract via the UI-SPEC step); doctor web-search checks on doctor's own schema bump; backup/restore of web-search config.
**Out of scope (deferred):** any new web-search feature behavior (this phase only SURFACES); v2 items (focus modes, reranking, deep-research).
</domain>

<decisions>
## Implementation Decisions

### Area 1 — status/--json web_search block (schema 4→5)
- ONE append-only `web_search` block; `status.Report` `schema_version` bumps 4→5 with a SINGLE golden re-freeze (mirror the memory v3 / agent v4 v-bump precedents — the "off" output differs from v4 only in `schema_version`).
- Block fields: enabled state; `villa-searxng` + `villa-websafe` health rows; guard-verdict counters (strip/flag/quarantine from Phase 32); last-query freshness; outbound-bounded indicator (Area 2).
- Web-search-OFF: output differs from the v4 contract ONLY in `schema_version` (preserves byte-identical-when-off, mirroring the agent-off precedent).

### Area 2 — outbound-bounded indicator derivation (load-bearing)
- `villa verify search` PERSISTS its last result (verdict + timestamp) to a small state artifact; `status`/dashboard/`doctor` READ that cached result with a freshness stamp.
- NEVER a bare config bool; NEVER run the netns egress proof on a status poll / dashboard refresh (too heavy + disruptive).
- Honesty: stale or absent verify result ⇒ typed-Unknown (NOT "bounded"/PASS). Only a real, recent `villa verify search` PASS surfaces as "outbound bounded."

### Area 3 — villa doctor web-search checks (doctor's OWN schema bump)
- Fold web-search checks into `villa doctor` on doctor's own schema bump (separate from the status 4→5 bump): searxng/websafe service readiness, guard health, egress-proof status (read the cached verify result).
- Tri-state: ready / degraded-with-reason / typed-Unknown; remediation on EVERY non-PASS (refuse-with-remediation).
- Include an offload-asserting chat-model-GPU-resident check UNDER SEARCH LOAD (per the project offload-asserting invariant — a silent/partial CPU fallback under search load is a FAIL, never false-green).

### Area 4 — backup/restore web-search coverage
- `villa backup`/`restore` cover the web-search configuration: SearXNG `settings.yml` provenance + the `WebSearchEnabled` gate, consistent with prior backup coverage.
- Fetched ephemeral web content is EXCLUDED by design.

### Claude's Discretion
- Exact Go field names/JSON keys in the `web_search` block; the cached-verify-result artifact path/format; doctor's exact schema-version value + check IDs; backup entry naming — all at the planner/executor's discretion within the decisions above.
- The dashboard panel's visual/interaction design is captured separately in the UI-SPEC (the `UI hint: yes` design-contract step), not here.
</decisions>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/status/status.go` — `status.Report` + `SchemaVersion` (currently v4; memory→v3, agent→v4 precedents; "off" differs only in schema_version). The web_search block 4→5 bump goes here.
- `cmd/villa/testdata/status.json.golden` (+ status-coding, status-memory) — the goldens to re-freeze once (`-update`).
- `internal/dashboard/assets/dashboard.js` + `dashboard.html`/`.css` — `memory-panel` / `agent-panel` are the hidden-until-data, /api/status-fed precedents for the Web Search panel (no new fetch/endpoint/probe — CTRL-02/D-03 pattern). XSS-safe rendering (textContent, not innerHTML).
- `cmd/villa/doctor.go` — the doctor check framework (its own schema; tri-state CheckResult with remediation) to fold web-search checks into.
- `internal/backup/backup.go` — backup covers config.toml + volume tar + usage.json; add SearXNG settings.yml provenance + WebSearchEnabled gate, exclude ephemeral content.
- `cmd/villa/verify_search.go` (Phase 33) — the verify result to persist + read (cached indicator source).
- `internal/websafe/` guard verdict (Phase 32) — the strip/flag/quarantine counters source.
- `internal/config/villaconfig.go` — `WebSearchEnabled` gate.

### Established Patterns
- Byte-frozen `--json`/dashboard contracts evolve append-only + schema-bump; golden tests guard them; re-freeze intentionally with `-update`.
- Hidden-until-data dashboard panels fed from the SAME /api/status poll (no new endpoint).
- Honesty-by-construction: typed-Unknown → WARN; offload-asserting (CPU fallback = FAIL); refuse-with-remediation in doctor.
- Loopback-only dashboard bind; XSS-safe (no innerHTML of untrusted data).

### Integration Points
- status.Report (status core) → CLI `--json` + dashboard /api/status + doctor.
- dashboard.js new Web Search panel reads report.web_search.
- doctor.go new web-search check group.
- backup.go new web-search config entries.
</code_context>

<specifics>
## Specific Ideas

- Single 4→5 status bump + single golden re-freeze (staggered-contract-risk discipline — freeze a finished, verified set once).
- The outbound-bounded indicator MUST derive from the real cached `villa verify search` result, never a config bool — this is the load-bearing honesty property of the surfacing.
- Dashboard panel must be hidden-until-data and XSS-safe (mirror memory/agent panels).
</specifics>

<deferred>
## Deferred Ideas

- Any new web-search feature behavior (this phase only surfaces existing, proven behavior).
- v2 scope: focus modes, embedding-rerank, multi-round deep-research (per v1.5 roadmap deferrals).
</deferred>
