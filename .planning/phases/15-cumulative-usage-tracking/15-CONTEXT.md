# Phase 15: Cumulative Usage Tracking - Context

**Gathered:** 2026-06-07
**Status:** Ready for planning
**Mode:** `--auto` (decisions auto-resolved to the recommended option for each gray area; review before planning)

<domain>
## Phase Boundary

villa accumulates **cumulative prompt + generated token counts, per model,
locally over time** — **reset-aware** (surviving `llama-server` counter resets on
restart / backend-swap) and **counts-only** (no prompt/response content, no new
outbound) — and surfaces those cumulative totals in `villa status` **and** the
control dashboard alongside the existing live tok/s.

**In scope:**
- New **pure `internal/usage` core** with `Fold(prior, sample) -> Totals` that
  accumulates from **monotonic `_total` counters** (not rate gauges), reset-aware,
  per model. No host I/O in the core; persistence via an injected seam.
- A persisted **usage store** (`usage.json`) under the XDG **data** dir, 0600 file
  / 0700 dir, atomic write, path-traversal guarded, with its own `schema_version`.
- Extend the existing bounded `internal/metrics` `/metrics` scrape to surface the
  two cumulative `_total` counters (typed-Unknown if absent).
- The **dashboard server's scrape path is the SOLE, mutex-guarded writer** of
  `usage.json` (fold + atomic write per scrape). `villa status` is one-shot and
  **reads only**.
- Surface cumulative totals via **exactly ONE append-only field** on
  `status.Report` (above `SchemaVersion`) + **ONE** schema bump
  (`reportSchemaVersion` 1→2) + **ONE** golden re-freeze; the dashboard reads the
  same field (no new API surface). Live tok/s (`GenTokensPerSec`) remains.

**Out of scope (own phases / future / explicitly excluded):**
- **Uploading usage data / telemetry of any kind** — violates zero-telemetry core
  value (REQUIREMENTS.md Out-of-Scope; research-flagged anti-feature).
- **Logging prompt/response content** — usage is counts-only; storing content is a
  privacy breach (REQUIREMENTS.md Out-of-Scope).
- **A second byte-frozen contract evolution** — Phase 14 owned the benchstore
  format; Phase 15 owns *only* the `status.Report` schema bump. Keep exactly one
  byte-frozen contract evolving at a time.
- Per-request / per-session usage history, cost estimation, charts/graphs, or
  retention/rotation policy beyond a single cumulative total per model — not in
  this phase.
</domain>

<decisions>
## Implementation Decisions

### Module boundary & store
- **D-01:** Build a **new pure `internal/usage` core** exporting
  `Fold(prior, sample) -> Totals` (and the `Totals` / per-model types). The core
  has **no host I/O** — folding is pure arithmetic over monotonic counters;
  persistence (read/write `usage.json`) is an injected seam, mirroring the
  established pure-core + injectable-seam pattern (`bench`/`backendswap`/`status`).
- **D-02:** **Persist to the XDG _data_ dir, not config.** Store at
  `$XDG_DATA_HOME/villa/usage.json` (fallback `~/.local/share/villa/usage.json`).
  Usage is accumulated **state/data**, not user configuration — do NOT pollute
  `config.toml`. Reuse `internal/config`'s proven write discipline (0600 file /
  0700 dir, **atomic temp+rename**, path-traversal guard) inside the new `usage`
  store. The store carries its **own** `schema_version` (independent of
  `status.Report`'s).
  > Research flag: confirm `os.UserCacheDir` vs an XDG_DATA_HOME resolver — pick
  > the data-dir resolver that matches the existing `os.UserConfigDir` honoring of
  > XDG and degrades safely if unset.

### Reset-aware fold
- **D-03:** **Per-model** keying. The store holds, per model, a
  `{cumulative_total, last_seen_raw}` pair for each counter (prompt + generated).
- **D-04:** **Reset detection by non-monotonicity.** On each sample:
  `delta = sample >= last_seen_raw ? sample - last_seen_raw : sample` — when the
  raw counter goes *backwards* (server restart / backend swap reset it to a low
  value), treat the **whole current sample** as new generation rather than a
  negative delta. Add `delta` to `cumulative_total`, then store `sample` as the new
  `last_seen_raw`.
- **D-05:** **Degrade to typed-Unknown, never fabricate.** If a `_total` counter is
  absent / unparseable in a scrape, that scrape contributes **no** fold and
  **no** write for that counter (honesty-by-construction, mirroring `metrics`'
  `ok=false` discipline) — never a fabricated zero presented as a real reading.

### Counter source
- **D-06:** Accumulate from the **monotonic `_total` counters**, NOT the existing
  rate gauges. Extend the existing bounded `internal/metrics` `/metrics` scrape
  (`parsePromText` + `ScrapeMetrics`) to surface
  `llamacpp:prompt_tokens_total` and `llamacpp:tokens_predicted_total` (additive
  fields on `PerfSnapshot` or a sibling narrow struct), preserving the
  `io.LimitReader` bound and typed-Unknown behavior.
  > Research flag (from ROADMAP): confirm the **exact** counter names and the
  > counter-reset semantics on restart/backend-swap against a **live**
  > `llama-server` at phase start (MEDIUM-confidence names; HIGH-confidence
  > reset-on-restart pattern).

### Writer ownership & trigger
- **D-07:** The **dashboard server's scrape path is the SOLE, mutex-guarded
  writer** of `usage.json` — on each metrics scrape it folds the sample into the
  store and atomically writes. `villa status` (one-shot CLI) **only reads** the
  persisted totals and **never writes**. This makes the dashboard service the
  single accumulation authority and avoids concurrent-writer races.
  > Note for research/planning: the dashboard's "poll loop" is **request-driven** —
  > the embedded SPA polls `/api/metrics` on an interval and the server handler
  > scrapes per request (no server-side background ticker today, see
  > `internal/dashboard/api.go`). The fold+write hooks into that handler path,
  > guarded by a dedicated mutex (sibling to `swapMu`). Confirm the per-model
  > **model identity** available at scrape time (from config / status) so totals
  > key correctly.
- **D-08:** **Accepted consequence:** usage accumulates **only while the dashboard
  service is running** (the long-lived `villa-dashboard.service`). This is
  acceptable — the dashboard is the persistent component; `villa status` reflects
  whatever has been accumulated so far. Not a gap to design around in this phase.

### status / dashboard contract
- **D-09:** Surface via **exactly ONE append-only field** on `status.Report`,
  inserted **immediately above `SchemaVersion`** (tail-append discipline,
  mirroring how `GenTokensPerSec`/`ROCmReadiness` were added), as a **pointer with
  `omitempty`** so an absent/empty store renders as typed-Unknown rather than a
  fabricated zero. Bump `reportSchemaVersion` **1 → 2** and re-freeze the affected
  golden(s) **once**, intentionally (`go test … -update`).
- **D-10:** The **dashboard reads the same `status.Report` field** — no new
  `/api` endpoint, no separate contract. Display cumulative totals **alongside**
  the existing live tok/s (live tok/s stays).

### Privacy / no-outbound (standing invariants)
- **D-11:** **Counts-only, enforced structurally.** Add a structural field-set test
  on the usage store/`Totals` type asserting it contains **no** prompt/response/
  content fields (mirroring `metrics.Slot`'s narrow-field-set security test). No
  prompt or response text ever enters the store.
- **D-12:** **No new outbound.** Usage derives entirely from the **existing
  loopback `/metrics` scrape** — no new network calls, no new hosts. The
  `status` `no_telemetry` assertion remains; the strictly-local posture is
  preserved and asserted.

### Claude's Discretion
- Exact Go type/field names (`UsageTotals`, `ModelUsage`, `Fold` signature shape),
  the precise JSON field name on `status.Report`, and whether the two `_total`
  counters ride on `PerfSnapshot` or a sibling struct — planner/executor decide
  within the constraints above.
- Whether the usage-store mutex is a new field on `Server` or encapsulated in the
  store seam.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` § "Phase 15: Cumulative Usage Tracking" — goal, success
  criteria, research flag, and the binding implementation note (pure `internal/usage`
  `Fold`; dashboard poll loop is sole mutex-guarded writer of `usage.json`; ONE
  append-only `status.Report` field + ONE schema bump + ONE golden refreeze).
- `.planning/REQUIREMENTS.md` § "Usage Tracking" (USAGE-01, USAGE-02) + the
  Out-of-Scope rows ("Uploading usage data / telemetry", "Logging prompt/response
  content for usage tracking") and the standing strictly-local / zero-telemetry
  invariant.

### Code seams this phase extends (read before planning)
- `internal/metrics/llamacpp.go` — bounded `/metrics` scrape, `parsePromText`,
  `ScrapeMetrics`, `PerfSnapshot`, typed-Unknown (`ok=false`) discipline; the
  `metrics.Slot` narrow-field-set security test pattern (`internal/metrics/llamacpp_test.go`).
- `internal/status/status.go` — `Report`, `reportSchemaVersion` (currently `1` at
  `status.go:135`), tail-append additive-field convention (`GenTokensPerSec` at
  `status.go:108-113`, `:297-298`), and the `--json` golden contract
  (`cmd/villa/testdata/*.golden*`).
- `internal/dashboard/server.go` + `internal/dashboard/api.go` — server struct,
  `swapMu sync.Mutex` precedent (`server.go:120`), the `cfg.Metrics`/`cfg.Slots`
  scrape seams, and the `/api/metrics` handler (`api.go:85-94`) that is the
  request-driven scrape path to hook the fold+write into.
- `internal/config/villaconfig.go` — XDG write discipline to mirror in the new
  store: `SaveVilla`/`SaveVillaTo` (0600/0700, atomic, `assertInsideDir`
  traversal guard at `villaconfig.go:221`).

### Prior-phase precedent
- `.planning/phases/13-villa-doctor-health-diagnosis/13-CONTEXT.md` — D-02 there
  explicitly reserves "the next `status` schema bump" for **Phase 15**; this phase
  is that bump.
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `internal/metrics` bounded-scrape + typed-Unknown core: extend rather than fork —
  add the two `_total` counters to the existing `parsePromText`/`ScrapeMetrics` path.
- `internal/config` XDG persistence discipline (atomic write, 0600/0700,
  traversal guard) — copy the pattern into the new `internal/usage` store seam.
- `metrics.Slot` narrow-field-set **security test** — the template for the
  counts-only structural assertion on the usage store.
- `status.Report` additive-field convention (`GenTokensPerSec`, `ROCmReadiness`) —
  the exact precedent for a tail-appended, `omitempty`, typed-Unknown field above
  `SchemaVersion`.

### Established Patterns
- Pure-core + injectable-seam: `Fold` is pure; persistence and scrape are seams.
- Single byte-frozen contract evolving at a time: only `status.Report` (1→2) here.
- Honesty-by-construction: absent counter → typed-Unknown, never a fabricated zero.

### Integration Points
- New writer hook: `internal/dashboard` `/api/metrics` handler → `usage.Fold` →
  atomic write of `usage.json` (mutex-guarded).
- New reader: `internal/status` reads `usage.json` (via seam) → populates the new
  `Report` field for both CLI `--json`/table and the dashboard.
</code_context>

<specifics>
## Specific Ideas

- Mutex sibling to `swapMu` for the usage writer, so the fold+write path can't race
  the existing swap path or itself across concurrent `/api/metrics` requests.
- The usage-store `schema_version` is **independent** of `status.Report`'s
  `schema_version` — two separate contracts, only one of which (the Report) is
  byte-frozen by golden tests.
</specifics>

<deferred>
## Deferred Ideas

- Per-session / per-request usage history, time-series charts, retention/rotation,
  and cost estimation — future phase; this phase ships a single cumulative total
  per model only.
- A server-side background polling goroutine (vs the current request-driven scrape)
  — out of scope; do not introduce a new accumulation cadence here.

None beyond the above — discussion stayed within phase scope.
</deferred>

---

*Phase: 15-cumulative-usage-tracking*
*Context gathered: 2026-06-07*
