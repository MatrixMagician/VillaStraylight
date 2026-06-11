---
phase: 23-surfacing-backup-memory-aware-swap
fixed_at: 2026-06-10T22:20:50Z
review_path: .planning/phases/23-surfacing-backup-memory-aware-swap/23-REVIEW.md
iteration: 1
findings_in_scope: 7
fixed: 7
skipped: 0
status: all_fixed
---

# Phase 23: Code Review Fix Report

**Fixed at:** 2026-06-10T22:20:50Z
**Source review:** `.planning/phases/23-surfacing-backup-memory-aware-swap/23-REVIEW.md`
**Scope:** CR-01 + WR-01..WR-06 (Critical + Warning). Info findings (IN-01..IN-08) deliberately left documented, not fixed.

**Summary:**
- Findings in scope: 7
- Fixed: 7 (WR-06 fixed on the backup side; restore-side streaming is a documented deferred residual)
- Skipped: 0
- Gates: `make check` green; `TestSeamGrepGate` green; goldens untouched (no schema change).

## Fixed Issues

### CR-01: Prove-fail rollback cannot complete on a live host; cmd tier deletes the only rollback copy

**Files modified:** `internal/backup/restore.go`, `internal/backup/deps.go`, `cmd/villa/restore.go`, `internal/backup/restore_test.go`, `cmd/villa/restore_test.go`
**Commit:** `98433fc`
**Applied fix:** `rollback()` now quiesces FIRST (Stop OWUI; Stop Qdrant when the forward path started it) before the clean-recreate re-imports, mirroring the forward quiesce — a running container holds its volume and `podman volume rm` fails in-use. Added `Result.RollbackIncomplete`; `runRestore` returns `preserveTmp` and on an incomplete rollback the cmd tier PRESERVES the restore temp dir (it holds the only copies of `rollback-owui.tar` / `rollback-qdrant.tar`) and prints a `podman volume import` recovery hint instead of `os.RemoveAll`.
**Tests added:** `TestRestoreProveFailRollbackQuiescesBeforeVolumeRm` (live-host-fidelity fake: VolumeRm fails "in use" while the owning service runs — the exact failure mode the no-op fakes made structurally invisible), `TestRestoreRollbackIncompleteSetsFlag`, `TestRestoreRollbackIncompletePreservesTmpDir`.

### WR-01: `recall index` mutated (KB reset/create, state-stamp persist) before the single-operator guard

**Files modified:** `cmd/villa/recall.go`, `cmd/villa/recall_test.go`
**Commit:** `c98dcaa`
**Applied fix:** Hoisted `listUsers` + the multi-human refusal to step (3a), directly after token mint (its only dependency) and BEFORE the state/KB step; the fetched users are reused for the chat listing. A refusal is now side-effect-free — no `--rebuild` wipe, no KB create, no embedding-stamp overwrite.
**Test added:** `TestRecallSingleOperatorGuard/multi-human refusal with --rebuild is side-effect-free (WR-01)` — zero `reset:`/`ensureKB`/`persist` calls and the recorded embedding stamp survives verbatim.

### WR-02: Restore gated destructive Qdrant work on the fail-soft volume-exists check

**Files modified:** `cmd/villa/podman_volume.go`, `cmd/villa/restore.go`, `internal/backup/restore.go`, `cmd/villa/podman_volume_test.go`, `internal/backup/restore_test.go`
**Commit:** `045642e`
**Applied fix:** New tri-state `volumeExistsTri` (exists / confident-absent / UNKNOWN) for the destructive restore path; `RestoreInput.QdrantVolumeUnknown` makes the core REFUSE at capture (zero side effects, with remediation) when the archive carries a qdrant entry but existence is unknown. Backup keeps the fail-soft helper (honest omission is correct there). Memory-free archives restore regardless.
**Tests added:** `TestRestoreQdrantExistsUnknownFailsClosed` (refusal + memory-free pass-through), `TestVolumeExistsTri` (all three cells).

### WR-03: Memory-OFF backup archived a leftover recall-state.json without a manifest schema claim

**Files modified:** `cmd/villa/backup.go`, `internal/backup/backup.go` (doc), `cmd/villa/backup_test.go`
**Commit:** `f9d6a05`
**Applied fix:** `in.RecallStatePath` is now gated on `cfg.MemoryEnabled` (mirroring the qdrant pair), so a memory-off archive stays v1-identical and the entry can no longer bypass the fail-closed `recall_schema_version` gate; honest "not included (memory disabled)" reporting; BackupInput doc updated to match the contract.
**Test added:** `TestBackupMemoryOffOmitsLeftoverRecallState` — real cmd-tier wiring with memory off + an existing recall-state.json (the pure-core tests passed an empty path and never saw this).

### WR-04: `backup -o <existing.tar>` truncated the prior archive up-front and deleted it on failure

**Files modified:** `cmd/villa/backup.go`, `cmd/villa/backup_test.go`
**Commit:** `b7a46a4`
**Applied fix:** The archive is assembled in `os.CreateTemp(parent, ".villa-backup-*.tar")` (0600) and `os.Rename`d onto the destination only after `Backup()` + `Close` succeed; every failure path removes only the temp. Same temp+rename discipline as `config.SaveVilla` / `usage.WriteFileAtomic`.
**Test added:** `TestBackupFailurePreservesPriorArchive` — a mid-backup failure leaves the prior archive byte-for-byte intact with no torn temp behind.

### WR-05: Dashboard renderModels conflated a fetch failure with an empty catalog

**Files modified:** `internal/dashboard/assets/dashboard.js`
**Commit:** `9067868`
**Applied fix:** `null` from `getJSON` (failed fetch) now returns early in both the poll handler and `renderModels` — last-good rows are kept under the global stale dimming (typed-Unknown rendering); `renderModelsEmpty()` is reserved for a genuine `[]`.
**Verification:** No JS test harness exists (no toolchain by design) — verified via `node --check` + manual review. NOTE the dashboard binary trap: after `make build`, `systemctl --user restart villa-dashboard.service` is required for the embedded asset to take effect.

### WR-06: Backup/restore buffered entire volume tars in memory

**Files modified:** `internal/backup/deps.go`, `internal/backup/tarutil.go`, `internal/backup/backup.go`, `cmd/villa/backup.go`, `internal/backup/backup_test.go`, `internal/backup/restore_test.go` (literal-style fixup)
**Commit:** `6a1e190`
**Applied fix (backup side):** New optional `Deps.OpenFile` streaming seam (live: `os.Open`+`Stat`); the two volume-tar entries are checksummed via a streaming `sum()` pass and tar-copied from a fresh reader at assembly, with an exact byte-count guard against mid-backup source drift. Nil seam (existing fakes) and small entries keep the ReadFile path; archive layout/checksums unchanged.
**Test added:** `TestBackupStreamsVolumeTars` — volume tars stream (zero ReadFile on their paths) and the manifest checksums verify against the streamed bodies.

**DEFERRED RESIDUAL (explicit):** the restore side still buffers — `readAndVerify` collects every entry's bytes (bounded by the existing 8 GiB/entry, 16 GiB/archive tarutil caps) and the cmd tier re-stages volume tars via `WriteTempFile([]byte)`. The true two-pass verify-then-stream-extract redesign touches the verify ordering guards (manifest-first, duplicate/extra-entry rejection) and the `WriteTempFile` seam contract — judged too invasive for a fix pass per the fix-scope constraint. Recommend a dedicated follow-up task (pairs naturally with IN-03's stale `OpenArchive` two-pass doc comment, which was intentionally left as-is since the implementation is still single-pass).

## Skipped Issues

None. (Info findings IN-01..IN-08 were out of scope by instruction and remain documented in 23-REVIEW.md.)

## Verification

- `make check` (vet + full test suite): PASS
- `go test ./internal/inference -run TestSeamGrepGate`: PASS (no backend/volume literals leaked; the WR-06 seam keeps identities behind `orchestrate.QdrantVolumeName()` etc.)
- Goldens untouched: no `--json`/dashboard contract change; the only struct additions are non-serialized core types (`Result.RollbackIncomplete`, `RestoreInput.QdrantVolumeUnknown`, `Deps.OpenFile`).
- Locked decisions honored: no auto-reindex, guards never mutate (WR-01 makes this true), typed-Unknown is never a confident negative (WR-02/WR-05), append-only contracts, v3 status schema untouched.

---

_Fixed: 2026-06-10T22:20:50Z_
_Fixer: Claude (gsd-code-fixer)_
_Iteration: 1_
