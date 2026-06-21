---
phase: 34-web-search-surfacing-lands-last
reviewed: 2026-06-21T00:00:00Z
depth: deep
files_reviewed: 18
files_reviewed_list:
  - internal/verifystate/store.go
  - internal/verifystate/store_test.go
  - cmd/villa/verify_search.go
  - cmd/villa/verify_search_test.go
  - internal/status/status.go
  - internal/status/status_test.go
  - cmd/villa/status.go
  - internal/doctor/doctor.go
  - cmd/villa/doctor.go
  - cmd/villa/doctor_test.go
  - internal/backup/backup.go
  - internal/backup/restore.go
  - internal/backup/manifest.go
  - internal/backup/deps.go
  - cmd/villa/backup.go
  - cmd/villa/restore.go
  - internal/orchestrate/searxng_settings_write.go
  - internal/dashboard/assets/dashboard.js
findings:
  critical: 0
  warning: 1
  info: 2
  total: 3
status: issues_found
resolved:
  - WR-01  # fixed: freshness lower-bound clamp (no-false-green on future timestamp)
accepted:
  - IN-01  # out of scope this pass (non-atomic restore-side settings.yml write)
  - IN-02  # out of scope this pass (unused clock seam in verify-search persist)
---

# Phase 34: Code Review Report

**Reviewed:** 2026-06-21
**Depth:** deep
**Files Reviewed:** 18
**Status:** issues_found

## Summary

Phase 34 is a surfacing phase: one genuinely new store (`internal/verifystate`) plus
read-only/append-only surfacing across status, doctor, backup/restore, and the dashboard.
The implementation is disciplined and the load-bearing security invariant holds: the
outbound-bounded indicator is derived purely from the cached `verifystate.State` via
`webSearchInfo`, gated on a 24h freshness window, and `cfg.WebSearchEnabled` is used ONLY
to decide whether to build the WebSearch section (status.go:613) — never to derive the
indicator. Fail-closed Load (absent/corrupt/future-schema ⇒ empty State), atomic 0600
write, and the traversal guard are faithful clones of the proven recall store. The
settings.yml backup/restore path forces 0600 (carries `SEARXNG_SECRET`), excludes ephemeral
web content, and verifies every archive entry via SHA-256 through the single `readAndVerify`
pass. The dashboard renders all server/web data via `textContent` (no `innerHTML` of server
data), is hidden-until-data, and adds no new fetch/endpoint/probe. Schema bumps are
append-only and independent (status 4→5, doctor 2→3, backup 3→4). All 804 tests in the five
touched packages pass; `go vet` is clean; the seam-grep gate is green.

One real defect was found: the freshness gate treats a FUTURE-dated `CheckedAt` as fresh,
so a clock-skewed or forged future timestamp with `Verdict=="PASS"` surfaces as "bounded"
(and as a doctor StatusPass) — a no-false-green gap. It is classified WARNING (not BLOCKER)
because exploitation requires write access to `$XDG_DATA_HOME/villa/` and the timestamp is
villa-stamped in the normal path, but it directly contradicts the stated intent of the
freshness gate ("a security property must be re-proven, never trusted"). The two INFO items
are minor robustness/consistency notes.

## Warnings

### WR-01: Future-dated verify timestamp bypasses the freshness gate → false "bounded"

**Status:** RESOLVED — `fix(34): clamp verify-result freshness lower bound (no-false-green on future timestamp, WR-01)`. Both call sites now reject a negative age (`age < 0 || age > window`), so a future-dated PASS surfaces as "unknown"/StatusWarn, never "bounded"/StatusPass. Covered by a new `PASS future-dated → unknown` case in `TestWebSearchOutboundBounded` (status) and `TestLiveSearchEgressProofFreshness` (doctor). `make check` + `TestSeamGrepGate` green; no golden change.

**File:** `internal/status/status.go:899` (and mirrored at `cmd/villa/doctor.go:631`)
**Issue:** The freshness gate uses `time.Since(checked) > verifyFreshnessWindow` to reject
stale results. `time.Since` returns a NEGATIVE duration for a `CheckedAt` in the future, and
a negative duration is never `> verifyFreshnessWindow`, so the check passes. A cached
`verifystate.State{Verdict:"PASS", CheckedAt:<future RFC3339>}` therefore falls through to
the verdict mapping and surfaces as `OutboundBounded == "bounded"` in `villa status --json`
/ the dashboard, and as `inference.StatusPass` ("outbound bounded") in `villa doctor`. This
contradicts the documented invariant ("a security property must be re-proven, not trusted
indefinitely from a stale cache", status.go:206-208 / T-34-08) — a future timestamp is
trusted MORE than indefinitely, never re-proven. The store is fail-closed against torn/forged
corruption (T-34-01), but a syntactically-valid future timestamp defeats the only temporal
guard. Both surfaces share the identical bug (consistent, but doubly present), and
`TestWebSearchOutboundBounded` (status_test.go:1125) covers fresh and stale PASS but has no
future-timestamp case, so the gap is untested.
**Fix:** Reject a future `checked` as not-fresh (clamp the lower bound), so only a timestamp
inside `[now-window, now]` is "fresh":
```go
age := time.Since(checked)
if age < 0 || age > verifyFreshnessWindow {
    return wi // future-dated OR stale ⇒ unknown, never bounded
}
```
Apply the same guard in `cmd/villa/doctor.go` `liveSearchEgressProof` (line 631):
```go
age := time.Since(checked)
if perr != nil || age < 0 || age > searchVerifyFreshnessWindow {
    // unparseable, future-dated, or stale → re-prove, never bounded
    ...
}
```
Add a `"PASS future-dated → unknown"` case to `TestWebSearchOutboundBounded` and the doctor
egress-proof test.

## Info

### IN-01: Restore's live settings.yml writer is non-atomic, unlike the backup-side writer

**File:** `cmd/villa/restore.go:474-493` (`WriteSearxngSettings` live seam)
**Issue:** The live restore seam writes settings.yml with a plain `os.WriteFile(path, data,
0o600)` (correct mode, correct traversal guard, correct 0700 dir), but NOT via a
temp→rename atomic write. The companion writer `orchestrate.writeSearxngFile` /
`atomicWriteMode` (searxng_settings_write.go:122-168) uses the temp→fsync→rename discipline,
and the recall/usage/verifystate stores all write atomically. A crash mid-write during the
restore mutate step could leave a torn settings.yml; restore's rollback would then re-write
the captured prior bytes, so the blast radius is bounded, but the inconsistency is worth
closing. Mode (0600) is correct, so this is robustness, not a secret-leak.
**Fix:** Route the restore seam through `orchestrate.WriteSearxngSettingsTo(dir, name, data)`
(or replicate the temp→rename in the closure) so settings.yml is written atomically on
restore as it is on first render.

### IN-02: verify-search persist uses `time.Now()` directly rather than an injectable clock

**File:** `cmd/villa/verify_search.go:749`
**Issue:** `runVerifySearch` stamps `CheckedAt: time.Now().UTC().Format(time.RFC3339)`
directly in the cmd-tier run path. `verifystate.Deps` already carries an optional `Now
func() time.Time` clock seam (store.go:73) that goes unused here. This is acceptable (the
persist is a best-effort side effect outside the pure `evalSearchVerify` core, and
`TestVerifySearchPersists` injects a capturing `persistFn` that asserts the verdict without
needing a deterministic timestamp), but wiring the clock through would make the stamped
timestamp testable and align with the store's own seam contract. No correctness impact.
**Fix:** Optional — thread a `nowFn` through `searchVerifyDeps` (defaulting to
`time.Now`) and use it for `CheckedAt`, so the persisted timestamp is deterministic in tests.

---

_Reviewed: 2026-06-21_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
