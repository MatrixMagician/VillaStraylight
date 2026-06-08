---
phase: 16-backup-restore
fixed_at: 2026-06-07
review_path: .planning/phases/16-backup-restore/16-REVIEW.md
iteration: 1
findings_in_scope: 8
fixed: 7
investigated_no_change: 1
status: all_fixed
---

# Phase 16: Code Review Fix Report

**Source review:** `.planning/phases/16-backup-restore/16-REVIEW.md`
**Iteration:** 1

**Summary:**
- In scope: CR-01 (BLOCKER) + WR-01..WR-06 + IN-01 (8 findings)
- Fixed (code change): 7
- Investigated, mirrored canonical predicate (no behavior change): 1 (WR-06)
- Accepted/tracking-only, no change requested: IN-02, IN-03, IN-04

## Fixed Issues

### CR-01 (BLOCKER): Non-verbatim rollback / data leak
**Files:** `internal/backup/deps.go`, `internal/backup/restore.go`, `internal/backup/restore_test.go`, `cmd/villa/restore.go`
**Commit:** 9249718
**Fix:** Added a `RemoveFile func(path string) error` seam to `backup.Deps` (wired to
`os.Remove` tolerating `os.IsNotExist` in `liveRestoreDeps`). The rollback now, per
data-dir artifact: restores the captured prior bytes if a prior existed, ELSE — if the
forward path created the file where none existed before (`ex.usagePresent` /
`ex.benchPresent`) — `RemoveFile`s it to restore the prior (absent) state verbatim
(BAK-02). A failed `RemoveFile` counts as rollback-incomplete (honest reporting).
Symmetric for `usage.json` and `bench-reports.jsonl`. Two regression tests added:
forward-created artifacts are removed on rollback (clean), and a failed remove reports
rollback-incomplete.

### WR-01: Restore temp dir never cleaned up
**File:** `cmd/villa/restore.go`
**Commit:** a485311
**Fix:** `liveRestore` now returns the `tmpDir`; the `RunE` wrapper removes it explicitly
(via `os.RemoveAll`) before every `os.Exit` — which skips defers — on both success and
error paths, after `backup.Restore` returns. Stops accumulating the exported Open WebUI
volume tar (with `webui.db`) in `/tmp`.

### WR-02: Duplicate / extra archive entries
**Files:** `internal/backup/restore.go`, `internal/backup/restore_test.go`
**Commit:** 1d59c45
**Fix:** `readAndVerify` now rejects duplicate entry names (`archive contains duplicate
entry %q`) and rejects any collected entry not listed in the manifest (exact-set), so the
archive must contain exactly the manifest-described members. Fail-closed BLOCK, zero side
effects (pre-mutate). Duplicate + extra-entry regression tests added.

### WR-03: Manifest-first not enforced on read
**Files:** `internal/backup/restore.go`, `internal/backup/restore_test.go`
**Commit:** 1d59c45
**Fix:** The reader now requires `manifest.json` as the FIRST tar member and schema-gates
it BEFORE reading any subsequent entry body — a data entry preceding the manifest is
refused. Manifest-not-first regression test added.

### WR-04: No archive size / entry-count bound
**Files:** `internal/backup/tarutil.go`, `internal/backup/tarutil_test.go`
**Commit:** 1d52f9d
**Fix:** `readArchive` now wraps each per-entry read in `io.LimitReader` (8 GiB/entry,
never trusting `hdr.Size`), caps the total across entries at 16 GiB and the member count
at 64, refusing an over-bound archive with an actionable error before handing bytes to the
callback. Tar-slip guard intact. Entry-count-cap regression test added.

### WR-05: WriteFileAtomic self-guard was a no-op
**File:** `internal/usage/usage.go`
**Commit:** d7304e9
**Fix:** Replaced the dead `assertInsideDir(path, filepath.Dir(path))` (a path is always
inside its own parent) with a guard against a fixed `storeRootDir()` (the resolved
`$XDG_DATA_HOME/villa` store root). Both `usage.json` and `bench-reports.jsonl` live
directly under that root, so legitimate restore/dashboard writes are unaffected, while a
`..`-bearing path is now actually rejected.

### IN-01: Silent backup restart failure
**Files:** `internal/backup/backup.go`, `internal/backup/deps.go`, `cmd/villa/backup.go`
**Commit:** 52e587d
**Fix:** Captured the deferred best-effort Open WebUI restart error into a new
`Result.RestartWarning` (via a named return) and surfaced it as a stderr warning
("backup written, but failed to restart villa-openwebui — run `villa up`"). Restart stays
best-effort (never fails the backup).

## Investigated — Mirrored Canonical Predicate (no behavior change)

### WR-06: Restore-prove preflight gate
**File:** `cmd/villa/restore.go`
**Commit:** 5536dc2
**Finding:** In `internal/preflight/preflight.go`, BLOCK is a TIER, not a distinct
`Status`. `StatusFail` is documented as "only meaningful on `TierBlock` checks", and a
BLOCK-tier check that cannot be evaluated downgrades to `StatusWarn` (D-15) — never
`StatusFail`. The constructor `fail()` always sets `TierBlock`. So `StatusFail` already
implies `TierBlock`: there is NO false-green hole today.
**Action:** To mirror the EXACT canonical install gate predicate (`renderPreflight`:
`r.Status == StatusFail && r.Tier == TierBlock`) and make intent auditable, paired both
conjuncts in `liveRestoreProve` and added a comment citing the enum. No behavior change.

## Accepted / Tracking-Only (no change, per scope)

- **IN-02** (`cmd/villa/backup.go`): hardcoded `Source: "catalog"` for excluded models —
  informational field; left as documented in REVIEW.md.
- **IN-03** (`cmd/villa/version.go`): ldflags-mutable `version` var test-race note — not a
  current bug; ldflags-only mutation retained.
- **IN-04** (`cmd/villa/backup.go`, `cmd/villa/restore.go`, `internal/backup/backup.go`):
  `ConfigSchemaVersion: 0` — `VillaConfig` has no `schema_version` field. The existing
  inline comments ("VillaConfig carries no schema_version field (not recorded)") already
  serve as the tracking note; no config schema field invented.

---

_Fixed: 2026-06-07_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
