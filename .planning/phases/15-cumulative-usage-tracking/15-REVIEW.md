---
phase: 15-cumulative-usage-tracking
reviewed: 2026-06-07T00:00:00Z
depth: deep
files_reviewed: 10
files_reviewed_list:
  - internal/usage/usage.go
  - internal/usage/usage_test.go
  - internal/metrics/llamacpp.go
  - internal/status/status.go
  - cmd/villa/status.go
  - internal/dashboard/server.go
  - internal/dashboard/api.go
  - internal/dashboard/api_test.go
  - cmd/villa/dashboard.go
  - internal/dashboard/assets/dashboard.js
findings:
  critical: 0
  warning: 4
  info: 5
  total: 9
status: issues_found
---

# Phase 15: Code Review Report

**Reviewed:** 2026-06-07
**Depth:** deep (cross-file: usage core ↔ metrics feed ↔ dashboard writer ↔ status reader)
**Files Reviewed:** 10
**Status:** issues_found

## Summary

The cumulative-usage slice is well-architected and the phase invariants (D-01..D-12) are
largely honored: the pure reset-aware fold (D-04) is correct, sole-writer concurrency is
serialized under `usageMu` with model identity captured inside the critical section (D-07),
the store uses an XDG-data atomic temp+rename with a traversal guard and 0600/0700 modes
(D-02), the counts-only type contract is structurally tested (D-11), and the scrape reuses
the existing bounded loopback endpoint with no new outbound (D-12). The CLI status path is
correctly read-only.

No BLOCKER-class defects were found — the write path is atomic, fail-closed, and the
concurrency model is sound for a single process. However, there are several WARNING-class
correctness gaps worth fixing:

1. A **contract mismatch** between the dashboard JS (`report.model`) and the actual frozen
   `status.Report` shape (no `model` field is ever serialized), making the model-keyed
   cumulative-usage lookup silently dead — it only works via the single-entry fallback, and
   becomes ambiguous/wrong as soon as two models accumulate.
2. The metrics `counterFromMap` guard does **not** match its own doc comment: NaN and +Inf
   pass the `v < 0` check and get narrowed into a fabricated `uint64` count, violating D-05.
3. A **multi-process write race** on `usage.json`: `usageMu` only serializes within one
   dashboard process; nothing prevents a second `villa dashboard` (or any future writer)
   from interleaving read-modify-write and losing counts. D-07 is enforced by convention,
   not by construction.
4. The reset-aware fold can **drop legitimately-accumulated tokens** on a counter reset
   when a scrape is missed (the well-known "reset between scrapes" undercount), and the
   monotonic-delta path can **double count** the unscraped tail at reset.

Plus several INFO items (dead `Deps.Now` seam, overclaiming comments, JS fallback fragility).

## Warnings

### WR-01: Dashboard JS reads `report.model`, but `status.Report` never serializes a `model` field

**File:** `internal/dashboard/assets/dashboard.js:348` (consumer); `internal/status/status.go:90-139` (producer)
**Issue:**
`renderCumulativeUsage` keys the cumulative-usage lookup on the configured model:

```js
var model = report.model || "";
...
if (model && usage.models[model]) { entry = usage.models[model]; }
else { /* fall back to sole entry when keys.length === 1 */ }
```

But `status.Report` (the frozen `/api/status` contract) has **no** `model` field — its
json-tagged fields are `services, ports, loopback_only, no_telemetry, overall, backend,
image, gen_tokens_per_sec, rocm_readiness, usage, schema_version`. `config.VillaConfig.Model`
has only a `toml:"model"` tag and is never placed on the Report. So `report.model` is always
`undefined`, `model` is always `""`, and the primary lookup branch is dead code.

The UI therefore relies entirely on the `keys.length === 1` single-entry fallback. The moment
the store accumulates **two** models (e.g. after a model swap — and the store is per-model by
design, D-03), `Object.keys(usage.models).length` is 2, the fallback is skipped, `entry`
stays null, and the panel renders "No usage recorded yet" **even though usage exists** — or,
worse, if the fallback were ever loosened, it would show the *wrong* model's totals.

This is a correctness defect in the USAGE-02 surface: the dashboard cannot reliably show the
*current* model's cumulative totals once more than one model has been observed.

**Fix:** Surface the active model id on the Report so the JS lookup is exact. The cleanest
option, consistent with D-01 ("active identity lives on /api/status"), is to tail-append a
`Model` field to `status.Report` (append-only, bump `reportSchemaVersion`):

```go
// status.go Report (append above SchemaVersion):
// Model is the active configured model id (cfg.Model) — the key the dashboard
// uses to select the current model's cumulative usage row (USAGE-02 / D-10).
Model string `json:"model,omitempty"`
```
and in `Run`: `report.Model = cfg.Model` (cfg is already loaded at the top of Run). Then the
existing `report.model` lookup works exactly. Refreeze the `status --json` golden intentionally
(`go test ... -update`) and note the schema bump. If adding to the frozen Report is undesirable,
instead have the JS iterate all models (render one block per model) rather than guessing a
single entry.

### WR-02: `counterFromMap` does not guard NaN / +Inf — comment overclaims, fabricated count possible (D-05 violation)

**File:** `internal/metrics/llamacpp.go:85-91` (comment at :84)
**Issue:**
The doc comment states "negative/NaN guarded to the typed-Unknown branch", but the guard is:

```go
func counterFromMap(m map[string]float64, name string) (uint64, bool) {
	v, ok := m[name]
	if !ok || v < 0 {
		return 0, false
	}
	return uint64(v), true
}
```

`NaN < 0` evaluates to `false`, and `+Inf < 0` evaluates to `false`, so a `NaN` or `+Inf`
value (which `strconv.ParseFloat` will happily produce from a body line like
`llamacpp:prompt_tokens_total NaN` or `... +Inf`) passes the guard. `uint64(NaN)` and
`uint64(+Inf)` are implementation-specific conversions (Go spec: "the behavior is
implementation-specific" / typically 0 or a garbage/saturated value). That value is then
returned with `Known=true` and folded into the durable cumulative total — a fabricated
reading the D-05 invariant explicitly forbids, and one that can permanently corrupt the
persisted total (the fold is additive and reset-aware, so a spurious large value sticks).

llama.cpp's own counters won't emit NaN/Inf, but `/metrics` is parsed defensively elsewhere
(the comment even claims this case is handled), and the store is durable — a single garbage
scrape poisons the cumulative total irrecoverably.

**Fix:** Reject non-finite and non-integral values explicitly:

```go
import "math"

func counterFromMap(m map[string]float64, name string) (uint64, bool) {
	v, ok := m[name]
	if !ok || math.IsNaN(v) || math.IsInf(v, 0) || v < 0 || v > math.MaxInt64 {
		return 0, false // typed-Unknown, never a fabricated count (D-05)
	}
	return uint64(v), true
}
```
The `> math.MaxInt64` (or `2^53`) upper bound also defends the "lossless below 2^53" claim in
the CounterSample doc against a saturating/overflowing conversion. Add a test case feeding
`NaN`/`+Inf`/a huge value and asserting `Known=false`.

### WR-03: Multi-process write race on usage.json — D-07 sole-writer is by convention, not construction

**File:** `internal/dashboard/api.go:134-161` (`foldUsage`); `cmd/villa/dashboard.go:200-231`; `internal/usage/usage.go:236-275`
**Issue:**
`usageMu` serializes the read-modify-write **within a single dashboard process**, and the
comments correctly justify that. But nothing serializes across **processes**. The store path
is a fixed XDG location (`usage.UsagePath()`), and `villa dashboard` can be launched more than
once (a stray manual `villa dashboard` alongside the `villa-dashboard.service`, or two service
instances). Each runs its own `usageMu`. Two processes can both `readUsage()` (read the same
prior from disk), both `Fold`, and both `WriteFileAtomic` (temp+rename) — the rename is atomic
so the file is never *torn*, but the **second writer's prior is stale**, so the first writer's
delta is silently lost (classic lost-update). Counts are undercounted, not corrupted, but the
D-07 "SOLE writer" invariant is only enforced by deployment convention.

The phase invariant says "ONLY the dashboard /api/metrics path writes usage.json, under
usageMu" — that holds for the *code path*, but the cross-process guarantee that makes "sole
writer" meaningful is absent.

**Fix:** Either (a) document explicitly that single-instance operation is a hard precondition
(the systemd unit should be the only writer; a manual second `villa dashboard` is unsupported)
and add a brief note + a same-process guard, or (b) add an OS-level advisory lock around the
read-modify-write so a second process serializes rather than lost-updates:

```go
// In the live WriteAll/foldUsage path, take a flock on a sidecar lock file (e.g.
// usage.json.lock) via syscall.Flock(LOCK_EX) spanning Read→Fold→Write, so a second
// process blocks instead of clobbering. Release after rename.
```
At minimum, capture this as a known limitation in the package doc so "sole writer" is not read
as a stronger guarantee than the code provides.

### WR-04: Reset-aware fold undercounts on a missed scrape across a reset (and the documented model can double-count the unscraped tail)

**File:** `internal/usage/usage.go:88-99` (`foldCounter`)
**Issue:**
The fold is correct for the two cases it tests (monotonic growth; a single backward step).
But the every-scrape model has two edge gaps the polling reality (a 2.5s poll, a restarting
server) will hit:

1. **Undercount across a reset within one inter-scrape window.** Suppose `LastSeenRaw=150`,
   the server generates to raw=200, then restarts and generates to raw=30, all between two
   scrapes. The next sample is `raw=30`. `30 < 150` → "whole sample is new" → `delta=30`. The
   50 tokens generated *after the previous scrape but before the reset* (200−150) are silently
   dropped. The fold cannot recover them because it never saw raw=200. This is inherent to
   sampling a monotonic counter, but it is unstated and untested.

2. **The complement — over/under at restart boundaries** — is also unhandled: the model
   assumes a backward step is always a full reset to a counter that *started at 0*. If a
   future llama.cpp build ever emits a counter that resets to a non-zero base, the
   `delta = sampleRaw` branch over-counts by that base. (Low likelihood, but the assumption is
   silent.)

Neither path corrupts the store or crashes — they bias the cumulative total **downward**
(case 1) which is the safe direction for a "usage" figure, so this is a WARNING not a BLOCKER.
But the cumulative total is presented to the user as authoritative ("prompt tokens (total)")
and the undercount is unbounded under frequent restarts.

**Fix:** Document the sampling limitation in `foldCounter`'s doc comment honestly ("a reset
that occurs *and is fully overwritten* between two scrapes loses the pre-reset tail; the figure
is a lower bound under restart churn"). Optionally tighten the reset heuristic: only treat a
backward step as a full-sample-new event when the drop is large relative to `LastSeenRaw`
(a tiny backward jitter could otherwise be a transient mis-read), and add a test for the
missed-scrape-across-reset case to pin the documented behavior.

## Info

### IN-01: `usage.Deps.Now` is a dead field — declared, never read

**File:** `internal/usage/usage.go:146-147`; `internal/dashboard/api.go:157` (caller uses `time.Now()` directly)
**Issue:** `Deps.Now func() time.Time` is declared "for callers that want a deterministic
timestamp seam," but no code in the package or its callers ever invokes `d.Now`. `Save`
doesn't stamp a timestamp; `foldUsage` calls `time.Now()` directly (not through any seam). The
test sets `Now` (usage_test.go:205) but never asserts on it. Dead seam → misleading API surface
(a reader assumes timestamps are injectable/deterministic; they are not).
**Fix:** Either remove `Deps.Now` and the test's no-op wiring, or actually route the
`CapturedAt` timestamp through it (e.g. have the live `foldUsage` pull `time.Now` from a seam so
`LastSeen` is deterministic in tests). Removing it is simplest given `Save`/`Load` don't need a
clock.

### IN-02: `counterFromMap` "lossless < 2^53" claim is undefended

**File:** `internal/metrics/llamacpp.go:69, 84`
**Issue:** Both comments assert the float64→uint64 narrowing is "lossless below 2^53," which is
true for the *value*, but nothing enforces the bound, and above 2^53 the float silently loses
integer precision before conversion (so two distinct counts could fold identically). For a
realistic token counter this is astronomically far off, but the comment claims a guarantee the
code does not make.
**Fix:** Covered by the WR-02 upper-bound check; once `v > math.MaxInt64` (or `> 1<<53`) returns
typed-Unknown, the comment becomes accurate.

### IN-03: `WriteFileAtomic` does not fsync the temp file or the directory before/after rename

**File:** `internal/usage/usage.go:256-274`
**Issue:** The atomic temp+rename protects against a *torn* file, but without `tmp.Sync()`
before `Close`/`Rename` and a directory `fsync` after, a power-loss between the rename and the
writeback can leave a zero-length or stale `usage.json` (the rename is durable but the temp's
contents may not be). This matches the existing config/benchstore precedent (neither fsyncs), so
it is **not a regression** for this phase — flagged as INFO for completeness given usage.json is
durable accumulated data where a silent revert-to-old is more user-visible than for config.
**Fix:** If durability matters more for usage than config, add `tmp.Sync()` before `tmp.Close()`
and fsync the parent dir after rename. Otherwise note the deliberate parity with config in the
comment.

### IN-04: JS single-entry fallback is silently wrong with ≥2 models (compounds WR-01)

**File:** `internal/dashboard/assets/dashboard.js:350-359`
**Issue:** Given WR-01 (no `report.model`), the only working path is
`if (keys.length === 1) entry = usage.models[keys[0]]`. With two or more accumulated models the
panel shows "No usage recorded yet" despite real data, with no log/hint that data exists for a
non-displayed model. The comment ("fall back to the sole entry when the report does not surface
a model id") rationalizes the limitation but the user-facing result is a false-empty.
**Fix:** Resolved by WR-01 (surface `report.model`); independently, consider rendering all model
entries when no current-model match is found, rather than only the sole-entry special case, so
multi-model stores are never falsely empty.

### IN-05: `Save` ignores the round-trip risk of `Models: nil` vs `omitempty`

**File:** `internal/usage/usage.go:67-70, 107-119`
**Issue:** `UsageTotals.Models` has `json:"models,omitempty"`. `Fold` always allocates a map, so
a folded store serializes `models`. But a `Save` of a zero `UsageTotals{}` (Models nil) writes
`{"schema_version":1}` with no `models` key, and `Load` returns it as `Models == nil`. Callers
that index `totals.Models[...]` are safe (nil-map read is fine), and `liveReadUsage` guards
`len(totals.Models) == 0`, so this is benign today — flagged only because a future caller doing
`totals.Models[k] = ...` on a freshly-Loaded empty store would nil-panic.
**Fix:** Optional hardening: have `Load` normalize `Models` to a non-nil empty map on success, or
document that callers must treat a Loaded `Models` as read-only/possibly-nil.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
