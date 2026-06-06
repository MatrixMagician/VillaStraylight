---
phase: 10-backend-tok-s-surfacing
reviewed: 2026-06-06T00:00:00Z
depth: deep
files_reviewed: 10
files_reviewed_list:
  - internal/status/status.go
  - internal/status/status_test.go
  - cmd/villa/status.go
  - cmd/villa/status_test.go
  - internal/recommend/recommend.go
  - internal/recommend/recommend_test.go
  - cmd/villa/recommend.go
  - cmd/villa/recommend_test.go
  - internal/dashboard/assets/dashboard.js
  - internal/dashboard/api_test.go
findings:
  critical: 0
  warning: 2
  info: 5
  total: 7
status: issues_found
---

# Phase 10: Code Review Report

**Reviewed:** 2026-06-06
**Depth:** deep
**Files Reviewed:** 10
**Status:** issues_found

## Summary

Phase 10 is a read/surfacing capstone that makes `villa status`, the dashboard, and
`villa recommend` backend-aware through tail-appended struct fields. I reviewed all ten
files at deep depth, traced the tok/s seam across `cmd/villa/status.go` →
`internal/metrics` → `internal/dashboard/api.go`, traced the two parallel ROCm-readiness
folds (`status.foldROCmReadiness` vs `recommend.deriveROCmAdvice`), and audited the
dashboard render path for XSS and honesty regressions.

The four LOCKED invariants hold:

- **Honesty / no-false-green:** `GenTokensPerSec` is `*float64 + omitempty`, the seam
  returns nil on idle/unavailable (verified through `liveGenTokensPerSec`), and the table
  renderer + JSON both omit it. ROCm readiness folds Unknown-wins-over-not-ready in both
  `foldROCmReadiness` (status.go:161) and `deriveROCmAdvice` (recommend.go:174). The
  recommend ROCm Notes (`rocmAdviceNote`, `rocmVerifyNote`, and the withheld-blocker copy)
  point at `villa bench` and contain none of the forbidden promise words; the test
  `TestPickROCmAdviceNoteHonorsHonesty` asserts this.
- **Seam discipline:** grep for `ROCm0` / `HSA_OVERRIDE_GFX_VERSION` / `Memory access
  fault` in the five non-test surfacing surfaces returned zero hits (the `ROCm0` literal
  appears only in `_test.go`, which is exempt).
- **Append-only / pure Pick:** `Pick` performs no I/O (envelope/readiness arrive on the
  passed `HostProfile`); `finalizeRecommendation` never reassigns `rec.Backend`; the
  recommended backend stays `vulkan` (asserted in every advice case).
- **XSS:** the only `innerHTML` write (dashboard.js:117) assigns a static string literal
  with no server interpolation; every server value (`svc.service`, `svc.active`, backend,
  image, model id/quant, fit_detail) is set via `textContent`.
- **D-01:** `/api/metrics` is pinned to NOT carry backend identity
  (`TestHandleMetricsShapeUnchanged`); the JS sources `lastBackend` from the
  `/api/status` poll and uses it only as a label on the generating tok/s branch.

No BLOCKER-class defects were found. Two WARNING items concern a possible permanent
"Switching…" wedge and a benign duplicate `slotsOK` discard; the INFO items are
maintainability / contract-clarity notes.

## Structural Findings (fallow)

No structural-findings block was supplied with this review.

## Narrative Findings (AI reviewer)

## Warnings

### WR-01: Successful switch that never reports `loaded` wedges the row on "Switching…" indefinitely

**File:** `internal/dashboard/assets/dashboard.js:474-516`
**Issue:** `doSwitch` keeps `switching` set on any HTTP-2xx `switched`/`no_op` result
(line 493-497) and relies SOLELY on `clearSwitchIfLoaded` (line 508) to clear it once a
later `/api/models` poll shows the target as `loaded`. If the swap succeeds at the HTTP
layer but the model never subsequently reports `loaded` (restart loops, the inference
unit comes up with a different/failed model, the catalog id and the loaded-model id never
match, or `/api/models` degrades to its empty/typed-Unknown branch for an extended
period), `switching` is never cleared. Because every other row's Switch button is
disabled while `switching !== null` (line 402), the ENTIRE models panel becomes
permanently unusable until a full page reload — there is no timeout or escape hatch on the
success path. The WR-06 comment only covers terminal *failure* results; the
indefinitely-pending *success* case has no guard.
**Fix:** Add a bounded timeout that clears the in-flight state if the target has not
reported `loaded` within N poll cycles, and re-enable the row:
```javascript
// in doSwitch, after a confirmed success:
if (success) {
  switchDeadline = Date.now() + SWITCH_TIMEOUT_MS; // e.g. 90_000
}
// in clearSwitchIfLoaded (or poll), also clear on deadline:
function clearSwitchIfLoaded(models) {
  if (switching === null) { return; }
  if (switchDeadline && Date.now() > switchDeadline) {
    switching = null; switchDeadline = null; return; // give the row back to the user
  }
  if (!models) { return; }
  for (var i = 0; i < models.length; i++) {
    if (models[i].id === switching && models[i].loaded) { switching = null; switchDeadline = null; return; }
  }
}
```

### WR-02: `liveGenTokensPerSec` discards the `/slots` availability flag, narrowing the generating gate

**File:** `cmd/villa/status.go:217`
**Issue:** `slots, _ := metrics.ScrapeSlots(endpoint)` drops the `slotsOK` boolean that
`internal/dashboard/api.go:83-90` deliberately keeps to compute `ActivityKnown`. The CLI
then calls `metrics.IsGenerating(snap, slots)` with a possibly-nil `slots`. The honesty
direction is safe (when `/slots` is unavailable and `requests_processing==0`,
`IsGenerating` returns false → tok/s omitted, never fabricated). However the two surfaces
now disagree: the dashboard distinguishes "activity unknown" (slots scrape failed,
requests==0) from "idle", whereas the CLI collapses both into "omit". A server that is
generating-between-requests with `/slots` disabled will show a tok/s figure in the
dashboard's generating logic only when `requests_processing>0`, but the CLI will silently
omit in the ambiguous window — an inconsistent user-facing story across two surfaces that
the plan intended to share one collector.
**Fix:** Either document the intentional CLI simplification, or mirror the dashboard's
tri-state by honoring `slotsOK`:
```go
slots, slotsOK := metrics.ScrapeSlots(endpoint)
generating := metrics.IsGenerating(snap, slots)
if !(slotsOK || generating) {
    return nil // activity genuinely unknown → omit (matches dashboard ActivityKnown=false)
}
if !generating {
    return nil // confidently idle → omit
}
```

## Info

### IN-01: `ROCmAdviceReady` enum value is defined but never produced or consumed

**File:** `internal/recommend/recommend.go:41-44`
**Issue:** `ROCmAdviceReady = "ready"` is declared "reserved for contract completeness"
but `deriveROCmAdvice` only ever returns `worth-trying`, `verify-with-bench`, or `""`.
A reader (or a dashboard author keying on `rocm_advice`) may reasonably expect `"ready"`
to appear and write a branch that is permanently dead. This also diverges from
`status.foldROCmReadiness`, which DOES emit `"ready"` for the same all-Known-good input
(see IN-02). Dead-by-design enum values invite false assumptions about the contract.
**Fix:** Either remove the unused const, or add an explicit `// never emitted by Pick in
Phase 10 — see deriveROCmAdvice` note adjacent to every consumer, and ensure any JSON
consumer treats an absent `rocm_advice` and `"ready"` identically.

### IN-02: Two parallel ROCm-readiness folds with different output vocabularies and no shared source

**File:** `internal/status/status.go:161-179`, `internal/recommend/recommend.go:174-206`
**Issue:** `foldROCmReadiness` and `deriveROCmAdvice` reimplement the same five-signal,
Unknown-wins worst-wins fold independently. They intentionally differ in output (`ready`/
`not-ready`/`unknown` vs `worth-trying`/`verify-with-bench`/withheld), but the fold logic
(iterate signals, any `!Known` → Unknown wins, else any `!Value` → not-ready/withheld) is
duplicated. A future change to the readiness signal set or the worst-wins rule must be
applied in two places or the surfaces silently drift. The `deriveROCmAdvice` version also
hand-builds a `signal{name, b}` slice while `foldROCmReadiness` uses a bare `[]detect.Bool`
— two shapes for one concept.
**Fix:** Extract the shared fold (e.g. a `detect.FoldReadiness(r) (allKnown bool,
firstBlocker string, sawUnknown bool)`) and have both callers map the common result into
their own vocabulary, so the worst-wins discipline lives in exactly one place.

### IN-03: Duplicated GiB-formatting helpers across packages

**File:** `cmd/villa/recommend.go:194-196` (`gib`), `internal/recommend/recommend.go:342-344` (`humanGiB`)
**Issue:** `gib` (`%.3f GiB (%d bytes)`) and `humanGiB` (`%.2f GiB`) are near-duplicate
byte-to-GiB formatters with subtly different precision/format, plus the dashboard JS has
its own `fmtBytes` (`(n / 1024^3).toFixed(1) + " GiB"`). Three independent definitions of
the same conversion invite divergence (e.g. one switching to GB vs GiB) and make the
user-facing memory numbers inconsistent across CLI notes, the fit table, and the
dashboard.
**Fix:** Consolidate the Go-side formatters into one helper (the package that owns the
byte type), and document the JS `fmtBytes` as the deliberate browser-side mirror with the
matching 1024^3 base.

### IN-04: `renderPerformance` prints a `prompt` tok/s row even when prompt throughput is 0

**File:** `internal/dashboard/assets/dashboard.js:271`
**Issue:** On the generating branch the code unconditionally renders
`metricRow("prompt", (m.prompt_tokens_per_sec || 0).toFixed(1) + " tok/s")`. When the
server is generating but has no in-window prompt processing (`prompt_tokens_per_sec==0`),
this shows "prompt 0.0 tok/s". The server side guards the derived latency on
`PromptTokensPerSec > 0` (api.go:102) but the prompt-rate row itself has no such guard.
This is a display nit, not a fabricated-value honesty breach (0 is the true gauge value),
but "0.0 tok/s" on a row that is simply not applicable reads as a stalled prompt phase.
**Fix:** Gate the prompt row like the latency row, or label it "n/a" when the gauge is 0
within the current generation window.

### IN-05: No null-guard on cached header DOM elements; a markup rename silently no-ops rendering

**File:** `internal/dashboard/assets/dashboard.js:15-27, 109-113`
**Issue:** `verdictEl`, `perfBody`, `gpuBody`, `modelsBody`, `banner` are looked up once at
load via `getElementById` with no null check before use (`renderVerdict` dereferences
`verdictEl.className` directly; `renderPerformance` dereferences `perfBody.textContent`).
`renderBackend` guards `healthRows` (line 180) but the others do not. If the embedded HTML
template is renamed/refactored, these throw an uncaught `TypeError` inside the poll
`.then`, which the surrounding `.catch` swallows — the panel silently stops updating with
no console signal. This is defensive-coding debt, not a current bug (the markup contract
is satisfied today).
**Fix:** Add the same `if (!el) return;` guard `renderBackend` already uses to the other
render functions, or assert the element set once at `startPolling` and log a clear error
if any required node is missing.

---

_Reviewed: 2026-06-06_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
