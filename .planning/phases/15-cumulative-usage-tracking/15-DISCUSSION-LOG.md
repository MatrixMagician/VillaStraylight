# Phase 15: Cumulative Usage Tracking - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-07
**Phase:** 15-cumulative-usage-tracking
**Mode:** `--auto` (all gray areas auto-selected; each resolved to the recommended option without interactive prompts)
**Areas discussed:** Store location/format, Reset-aware fold, Counter source, Writer ownership, status/dashboard contract shape, Privacy assertion

---

## Store location / format

| Option | Description | Selected |
|--------|-------------|----------|
| New `internal/usage` core + `usage.json` under XDG **data** dir (0600/0700, atomic, traversal-guarded, own schema_version) | Keeps config.toml clean; usage is state/data not config; reuses config's write discipline | ✓ |
| Persist inside `config.toml` | Reuses existing `SaveVilla`; but pollutes user config with accumulated state | |

**Selected:** New pure `internal/usage` core writing `$XDG_DATA_HOME/villa/usage.json` (recommended default).
**Notes:** Research flag — confirm `os.UserCacheDir` vs an XDG_DATA_HOME resolver matching the existing `os.UserConfigDir` XDG honoring.

---

## Reset-aware fold

| Option | Description | Selected |
|--------|-------------|----------|
| Per-model `{cumulative_total, last_seen_raw}`; delta = sample>=last_seen ? sample-last_seen : sample; absent counter → no fold/no write | Reset detected by non-monotonicity; degrades to typed-Unknown | ✓ |
| Naive delta (sample - last_seen) | Goes negative on reset — corrupts totals | |

**Selected:** Non-monotonicity reset detection, per-model (recommended default).
**Notes:** On counter going backwards, whole current sample counts as new generation.

---

## Counter source

| Option | Description | Selected |
|--------|-------------|----------|
| Extend existing bounded scrape to surface `llamacpp:prompt_tokens_total` + `llamacpp:tokens_predicted_total` (monotonic `_total`) | Reuses `parsePromText`/`ScrapeMetrics`, keeps LimitReader bound + typed-Unknown | ✓ |
| Derive from existing rate gauges | Rate gauges are last-window snapshots, not cumulative — wrong source | |

**Selected:** Extend `internal/metrics` scrape for the `_total` counters (recommended default).
**Notes:** ROADMAP research flag — confirm exact names + reset semantics on a live `llama-server` at phase start.

---

## Writer ownership

| Option | Description | Selected |
|--------|-------------|----------|
| Dashboard server scrape path is SOLE mutex-guarded writer; `villa status` reads only | Single accumulation authority; no concurrent-writer races | ✓ |
| Both CLI and dashboard write | Concurrent-writer races on `usage.json` | |

**Selected:** Dashboard-only writer, status read-only (recommended default; per ROADMAP impl note).
**Notes:** Dashboard poll is request-driven (`/api/metrics` handler) — fold+write hooks there with a dedicated mutex. Accepted consequence: usage accumulates only while the dashboard service runs.

---

## status / dashboard contract shape

| Option | Description | Selected |
|--------|-------------|----------|
| ONE append-only `status.Report` field above `SchemaVersion` (pointer/omitempty), bump `reportSchemaVersion` 1→2, ONE golden refreeze; dashboard reads same field | Single byte-frozen contract evolution; mirrors `GenTokensPerSec` precedent | ✓ |
| Separate `/api/usage` endpoint + own contract | Adds a second contract surface; unnecessary divergence | |

**Selected:** Single append-only Report field + one schema bump + one golden refreeze (recommended default).
**Notes:** Live tok/s (`GenTokensPerSec`) remains; dashboard reuses the same field, no new API.

---

## Privacy assertion

| Option | Description | Selected |
|--------|-------------|----------|
| Structural field-set test (mirror `metrics.Slot`) asserting no prompt/response fields; no new network calls; preserve `no_telemetry` assertion | Counts-only + no-outbound enforced by construction | ✓ |
| Rely on convention / code review only | No structural guard against future content leakage | |

**Selected:** Structural counts-only test + reuse existing loopback scrape (recommended default).

---

## Claude's Discretion

- Exact Go type/field names (`UsageTotals`, `ModelUsage`, `Fold` signature), the precise JSON field name on `status.Report`, and whether the two `_total` counters ride on `PerfSnapshot` or a sibling struct.
- Whether the usage-store mutex is a new `Server` field or encapsulated in the store seam.

## Deferred Ideas

- Per-session / per-request usage history, time-series charts, retention/rotation, cost estimation — future phase.
- A server-side background polling goroutine (vs the current request-driven scrape) — out of scope here.
