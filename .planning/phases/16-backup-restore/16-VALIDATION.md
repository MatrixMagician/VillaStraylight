---
phase: 16
slug: backup-restore
status: validated
nyquist_compliant: true
wave_0_complete: true
created: 2026-06-07
validated: 2026-06-08
---

# Phase 16 — Validation Strategy

> Per-phase validation contract for feedback sampling during execution.

---

## Test Infrastructure

| Property | Value |
|----------|-------|
| **Framework** | go test (stdlib `testing`; table-driven + golden fixtures; no third-party assert/mock) |
| **Config file** | none — `go.mod` / `Makefile` drive it |
| **Quick run command** | `go test ./internal/backup/... ./cmd/villa/...` |
| **Full suite command** | `make check` (go vet + `go test ./...`) |
| **Estimated runtime** | ~30–60 seconds |

---

## Sampling Rate

- **After every task commit:** Run `go test ./internal/backup/... ./cmd/villa/...`
- **After every plan wave:** Run `make check`
- **Before `/gsd-verify-work`:** Full suite must be green (`make check`)
- **Max feedback latency:** ~60 seconds

---

## Per-Task Verification Map

> Filled by the planner against the final task IDs. Backup/restore logic is
> testable **off-hardware** via injected `fake*Deps` seams (the proven pattern
> from `backendswap`/`uninstall`); only the cross-host / podman round-trip and
> SELinux/UID-remap behaviors are manual-on-hardware.

| Task ID | Plan | Wave | Requirement | Threat Ref | Secure Behavior | Test Type | Automated Command | File Exists | Status |
|---------|------|------|-------------|------------|-----------------|-----------|-------------------|-------------|--------|
| 16-01-01 | 01 | 1 | BAK-01 | T-16-01 | Archive excludes model weights; manifest records SHA-256 + digests; no image literal leaks (seam gate green) | unit | `go test ./internal/backup/... ./cmd/villa/...` | ✅ | ✅ green |
| 16-02-01 | 02 | 2 | BAK-02 | T-16-02 | Failed/partial restore rolls back verbatim; running stack intact; rollback-complete/incomplete reported honestly | unit | `go test ./internal/backup/... ./cmd/villa/...` | ✅ | ✅ green |
| 16-03-01 | 03 | 2 | BAK-03 | T-16-03 | Version/digest/schema skew → WARN+confirm before apply; checksum/incompatible-manifest → fail-closed BLOCK | unit | `go test ./internal/backup/... ./cmd/villa/...` | ✅ | ✅ green |

**Covering tests (reconciled 2026-06-08, all green — 35 backup/restore tests):**

- **16-01-01 (BAK-01)** — `internal/backup`: `TestExcludedModelHasNoContentFields`, `TestManifestJSONRoundTrip`, `TestManifestSchemaVersionIsLastField`, `TestManifestBenchEntryIsSingle`, `TestChecksumSumDeterministic/VerifyMatch/VerifyMismatch`, `TestBackupAssemblesArchive`, `TestBackupSkipsAbsentDataDirArtifacts`, `TestBackupDeferredRestartFiresOnExportError`, `TestTarRoundTrip`, `TestTarSlipRefusesTraversal/Absolute`, `TestTarSlipAllowsInDir` · `cmd/villa`: `TestRunBackupWritesArchive`, `TestBackupDefaultNameIsFSSafe`, `TestBackupOutputTraversalRejected`, `TestBenchEntryResolvesViaCmdResolver`
- **16-02-01 (BAK-02)** — `internal/backup`: `TestRestoreHappyPathCleanRecreateBeforeImport`, `TestRestoreMutateErrorRollsBackAndReImportsCaptured`, `TestRestoreTempVolumeStagingFailureRollsBack`, `TestRestoreRollbackRemovesForwardCreatedDataArtifacts`, `TestRestoreRollbackRemoveFailureReportsIncomplete`, `TestRestoreRollbackStepErrorReportsIncomplete`, `TestRestoreNonPassProveRollsBack` · `cmd/villa`: `TestRestoreHappyPathExitsPass`, `TestRestoreOffloadFailRollsBack`
- **16-03-01 (BAK-03)** — `internal/backup`: `TestSkewClassification`, `TestSkewMatchingNoFindings`, `TestRestoreVerifyMismatchRefusesZeroSideEffects`, `TestRestoreIncompatibleSchemaRefuses`, `TestRestoreBlockSkewRefuses`, `TestRestoreWarnSkewConsentDeniedRefuses`, `TestRestoreWarnSkewBypassProceeds`, `TestReadArchiveEntryCountCapRefuses`, `TestRestoreDuplicateEntryRefuses`, `TestRestoreExtraEntryRefuses`, `TestRestoreManifestNotFirstRefuses` · `cmd/villa`: `TestRestoreConsentDeniedExitsBlocked`, `TestRestoreYesBypassesConsent`, `TestRestoreCorruptArchiveBlocks`, `TestRestoreRequiresPositionalArchive`

---

## Wave 0 Requirements

- [x] `internal/backup/backup_test.go` + `manifest_test.go` + `checksum_test.go` — pure-core units for BAK-01 (manifest build/verify, archive entry plan, SHA-256), BAK-03 (skew comparison: WARN vs BLOCK boundary)
- [x] `internal/backup/restore_test.go` — transactional frame for BAK-02 (capture→quiesce→clean-recreate→prove→rollback) driven by `fakeDeps`
- [x] `internal/backup/tarutil_test.go` — tar-slip / absolute-path / entry-count-cap extraction guards (T-16-01)
- [x] `cmd/villa/backup_test.go` / `cmd/villa/restore_test.go` — cobra wiring + `fake*Deps`; output-traversal guard; FS-safe default name
- [x] Existing `go test` infrastructure covers the rest — no new framework needed

---

## Manual-Only Verifications

| Behavior | Requirement | Why Manual | Test Instructions |
|----------|-------------|------------|-------------------|
| Same-host backup→restore round-trip (committed bar) | BAK-01/02 | Needs live rootless Podman + Open WebUI volume on gfx1151 host | `villa backup -o /tmp/b.tar` → mutate a chat → `villa restore /tmp/b.tar` → confirm chats restored, stack healthy (`villa status` green, residency-proven) |
| Clean-volume-before-import (no stale data leak) | BAK-02 | `podman volume import` MERGES; only observable on a live volume | Restore over a volume with a stray file; confirm the stray file is gone post-restore |
| OWUI live-SQLite quiesce yields importable DB | BAK-01 | Requires a running Open WebUI with an open WAL | Stop `villa-openwebui.service`, export, restart; confirm `webui.db` opens clean after restore |
| Cross-host / post-`podman system reset` restore (KNOWN LIMITATION, best-effort) | BAK-02 | UID-remap + SELinux `:Z` repair only reproducible on a reset/foreign host | Restore on a `podman system reset` host; if perms fail, apply documented `podman unshare chown -R` remediation; document outcome honestly |
| Skew warning + confirm gate on real version/digest mismatch | BAK-03 | Needs an archive from a different villa/image version | Restore an older-manifest archive; confirm WARN + remediation prints and apply waits for confirm (`--yes` bypass) |

---

## Validation Sign-Off

- [x] All tasks have `<automated>` verify or Wave 0 dependencies
- [x] Sampling continuity: no 3 consecutive tasks without automated verify
- [x] Wave 0 covers all MISSING references
- [x] No watch-mode flags
- [x] Feedback latency < 60s
- [x] `nyquist_compliant: true` set in frontmatter

**Approval:** ✅ validated 2026-06-08

---

## Validation Audit 2026-06-08

State A reconciliation: the planning-time VALIDATION.md was never updated after Phase 16
executed. All three per-task verifications are covered by an extensive off-hardware suite
(35 backup/restore tests across `internal/backup` + `cmd/villa`, all green) using the proven
`fake*Deps` seam — including the transactional rollback frame, the clean-recreate-before-import
fix, tar-slip/entry-cap hardening, and fail-closed BLOCK on checksum/incompatible-schema.

On-hardware UAT (16-UAT.md, gfx1151): **4/5 PASS** — same-host round-trip, clean-recreate/no-merge,
live-SQLite quiesce, and skew WARN-and-confirm + fail-closed BLOCK all passed. The 5th item
(cross-host / post-`podman system reset` restore) is a **documented best-effort limitation**,
not run (a `podman system reset` is too destructive); its mechanism + `podman unshare chown -R`
remediation are validated indirectly. UAT also found+fixed a real regression (WR-05 store-guard
broke `/tmp` volume staging, fix 8eb2526 + `TestRestoreTempVolumeStagingFailureRollsBack`).

No auditor spawn needed — zero automated gaps.

| Metric | Count |
|--------|-------|
| Gaps found | 0 |
| Resolved | 0 |
| Escalated | 0 |
| Tasks COVERED (automated, green) | 3/3 (35 tests) |
| Manual-only (UAT, passed) | 4 |
| Manual-only (documented best-effort limitation) | 1 (cross-host) |
