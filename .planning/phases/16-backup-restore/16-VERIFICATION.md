---
phase: 16-backup-restore
verified: 2026-06-07T23:25:00Z
status: passed
score: 17/17 must-haves verified (automated) + on-hardware UAT PASS (gfx1151) — 4/5 PASS, 1 documented best-effort limitation (cross-host)
re_verification:
  previous_status: human_needed
  note: |
    On-hardware UAT executed 2026-06-07 on the gfx1151 host (rootless Podman
    5.8.2, villa v1.1-138). UAT-1 round-trip PASS, UAT-2 clean-recreate/no-merge
    PASS, UAT-3 live-SQLite quiesce PASS, UAT-5 skew WARN-and-confirm + fail-closed
    BLOCK (checksum + incompatible schema) with zero side effects PASS, UAT-4
    cross-host = documented best-effort limitation (not run — would require a
    destructive podman system reset; mechanism + remediation validated indirectly).
    UAT surfaced and fixed a real regression (WR-05 store guard broke restore /tmp
    staging) — commit 8eb2526 with a regression test. See 16-UAT.md.
human_verification:
  - test: "Same-host backup→restore round-trip"
    expected: "`villa backup -o /tmp/b.tar` → mutate a chat → `villa restore /tmp/b.tar` → chats restored, `villa status` green + residency-proven"
    why_human: "Needs live rootless Podman + Open WebUI volume on a gfx1151 host; cannot run off-hardware"
  - test: "Clean-volume-before-import (no stale data leak)"
    expected: "Restore over a volume containing a stray file; the stray file is gone post-restore (import MERGES, so clean-recreate must win)"
    why_human: "`podman volume import` merge behavior only observable on a live named volume"
  - test: "OWUI live-SQLite quiesce yields importable DB"
    expected: "Stop villa-openwebui.service, export, restart; webui.db opens clean after restore (no WAL corruption)"
    why_human: "Requires a running Open WebUI with an open WAL on hardware"
  - test: "Cross-host / post-`podman system reset` restore (KNOWN best-effort LIMITATION)"
    expected: "Restore on a reset/foreign host; if perms fail, documented `podman unshare chown -R` remediation applies; outcome documented honestly"
    why_human: "UID-remap + SELinux :Z repair only reproducible on a reset/foreign host"
  - test: "Skew WARN + confirm gate on a real version/digest mismatch"
    expected: "Restoring an older-manifest archive prints a named WARN + remediation and waits for y/N confirm; --yes/--force bypasses"
    why_human: "Needs an archive produced by a different villa/image version against a live install"
---

# Phase 16: Backup / Restore Verification Report

**Phase Goal:** Users can back up their workspace (config .toml + the Open WebUI data volume + the phase-14/15 data-dir artifacts: usage.json + saved bench-reports.jsonl) to a single self-describing local archive that EXCLUDES re-pullable model weights and carries a manifest of versions / image digests / checksums — and restore from it transactionally (capture → quiesce → swap → restart → prove → rollback-on-failure), so a failed or partial restore never corrupts a running stack; restore warns on version/digest skew before applying.
**Verified:** 2026-06-07
**Status:** human_needed
**Re-verification:** No — initial verification

## Goal Achievement

### Observable Truths

Roadmap Success Criteria (SC1–SC3) merged with PLAN frontmatter must-haves.

| #   | Truth                                                                                                                                                                | Status     | Evidence |
| --- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ---------- | -------- |
| SC1 | `villa backup` produces a single archive: config + OWUI volume + version/digest/checksum manifest; model weights excluded, identity recorded for re-pull | ✓ VERIFIED | `villa backup --help` registered; `cmd/villa/backup.go` assembles manifest.json+config.toml+openwebui-volume.tar+usage.json+bench-reports.jsonl; `ExcludedModels: excludedModelIdentities(cfg)` (backup.go:155, :233); villa-models volume NEVER named (backup.go:57,94); `TestRunBackupWritesArchive` PASS |
| SC2 | `villa restore` applies transactionally (capture → quiesce → swap → restart → prove → rollback-on-failure); failed/partial restore leaves stack intact | ✓ VERIFIED | `internal/backup/restore.go` `Restore()` clones backendswap frame: capture strictly before mutate (L146-156), quiesce (L250), mutate (L255-278), prove (L283), `rolledBack()` on any error/non-pass; `TestRestoreOffloadFailRollsBack`, `TestRestoreMutateErrorRollsBackAndReImportsCaptured` PASS |
| SC3 | `villa restore` warns on version/digest skew before applying, with remediation | ✓ VERIFIED | `CompareSkew` → WARN-and-confirm (restore.go:131-140); `skewPrompt` emits Field/Detail/Remediation + y/N; `TestRestoreConsentDeniedExitsBlocked`, `TestRestoreYesBypassesConsent`, `TestSkewClassification` PASS |
| 1   | `internal/backup` is a pure core (no os/exec, no podman); host I/O via Deps func fields (D-01) | ✓ VERIFIED | `grep os/exec internal/backup/` → none; only fake `sha256:aaa` test fixtures, no real image literals; `Deps` is all func fields (deps.go) |
| 2   | Manifest records villa version, host fingerprint, both image digests (seam-sourced), store schema versions, per-entry SHA-256, excluded model identities | ✓ VERIFIED | `BuildManifest`/`Manifest` (manifest.go); digests `be.Image()`+`OpenWebUIImage()` (backup.go:143-144); schema versions via accessors (backup.go:146-147); `TestManifestJSONRoundTrip` PASS |
| 3   | SHA-256 over io.Reader computes + verifies deterministically | ✓ VERIFIED | `checksum.go` `sum`/`verify`; `TestChecksumSumDeterministic`, `TestChecksumVerifyMatch/Mismatch` PASS |
| 4   | Single plain POSIX .tar (manifest.json first) with tar-slip guard refusing traversal | ✓ VERIFIED | `tarutil.go` writeArchive (PAX, manifest first), `assertInsideDir`; `TestTarRoundTrip`, `TestTarSlipRefusesTraversal/Absolute`, `TestTarSlipAllowsInDir` PASS |
| 5   | Skew: version/digest/host → WARN-and-confirm; SHA-256/incompatible-schema → fail-closed BLOCK | ✓ VERIFIED | `CompareSkew` SkewVerdict.Block vs Warnings; readAndVerify schema-gate BLOCK (restore.go:361); `TestRestoreIncompatibleSchemaRefuses`, `TestRestoreBlockSkewRefuses`, `TestRestoreVerifyMismatchRefusesZeroSideEffects` PASS |
| 6   | `orchestrate.OpenWebUIImage()` accessor; no re-typed literal; seam gate green | ✓ VERIFIED | `func OpenWebUIImage` in openwebui.go; `TestSeamGrepGate` PASS |
| 7   | Build-stamped villa version constant | ✓ VERIFIED | `cmd/villa/version.go` `var version="dev"`; Makefile `LDFLAGS := -X main.version=$(VERSION)`; `villa version` → `dev` |
| 8   | Archive output path traversal-guarded + 0600; clean-recreate-before-import on apply AND rollback | ✓ VERIFIED | `TestBackupOutputTraversalRejected` PASS; `cleanRecreateThenImport` (restore.go:171) used forward (L273) AND rollback (L225) |
| 9   | CR-01: rollback removes forward-created data-dir artifacts (RemoveFile) | ✓ VERIFIED | `RemoveFile` seam (deps.go:87); rollback removes when `ex.*Present` and no prior (restore.go:212-223, `rollbackRemove` L294); `TestRestoreRollbackRemovesForwardCreatedDataArtifacts`, `TestRestoreRollbackRemoveFailureReportsIncomplete` PASS |
| 10  | WR-01: restore temp-dir cleaned before every exit | ✓ VERIFIED | `liveRestore` returns tmpDir; RunE `os.RemoveAll(tmpDir)` pre-os.Exit (restore.go:60-64,169) |
| 11  | WR-04: bounded archive reads (per-entry + total + entry-count caps) | ✓ VERIFIED | `io.LimitReader(maxEntryBytes+1)`, total + member-count caps (tarutil.go:104-130); `TestReadArchiveEntryCountCapRefuses` PASS |

**Score:** 17/17 truths verified (automated). 5 on-hardware UAT items remain (human verification).

### Required Artifacts

| Artifact | Expected | Status | Details |
| -------- | ----------- | ------ | ------- |
| `internal/backup/manifest.go` | Manifest (schema_version LAST), BuildManifest, EntryChecksum, ExcludedModel | ✓ VERIFIED | `type Manifest`, `TestManifestSchemaVersionIsLastField` PASS |
| `internal/backup/checksum.go` | sha256 compute+verify over io.Reader | ✓ VERIFIED | `sum`/`verify`, typed ErrChecksumMismatch |
| `internal/backup/tarutil.go` | outer tar write/read + tar-slip + bounded reads | ✓ VERIFIED | writeArchive/readArchive/assertInsideDir + WR-04 caps |
| `internal/backup/deps.go` | injectable Deps, Result, ProveVerdict, RemoveFile/EnsureVolume seams | ✓ VERIFIED | all func fields incl. EnsureVolume, RemoveFile, VolumeExport/Import/Rm |
| `internal/backup/backup.go` | pure Backup() + CompareSkew/SkewVerdict | ✓ VERIFIED | `func Backup`, `func CompareSkew`; `TestBackupAssemblesArchive` PASS |
| `internal/backup/restore.go` | pure Restore() transactional core | ✓ VERIFIED | `func Restore` clones backendswap.Run; 14 restore_test cases PASS |
| `cmd/villa/backup.go` | newBackup, runBackup, liveBackupDeps | ✓ VERIFIED | `villa backup --help`; `func newBackup`; seam-sourced digests |
| `cmd/villa/restore.go` | newRestore, runRestore, liveRestoreDeps, consent + --yes | ✓ VERIFIED | `villa restore --help`; EnsureVolume/VolumeRm/Import wired (L225-267) |
| `cmd/villa/podman_volume.go` | fixed-arg podman volume var + arg builders | ✓ VERIFIED | `TestPodmanVolumeFakeSwappable` PASS |
| `internal/orchestrate/openwebui.go` | OpenWebUIImage() accessor | ✓ VERIFIED | `func OpenWebUIImage` present |
| `cmd/villa/version.go` | build-stamped version var | ✓ VERIFIED | `var version`; ldflags wired in Makefile |
| `internal/usage/usage.go` | SchemaVersion() accessor | ✓ VERIFIED | used at backup.go:146 |
| `internal/benchstore/benchstore.go` | SavedReportSchemaVersion() accessor | ✓ VERIFIED | used at backup.go:147 |

### Key Link Verification

| From | To | Via | Status | Details |
| ---- | --- | --- | ------ | ------- |
| cmd/villa/backup.go | internal/backup.Backup | liveBackupDeps | ✓ WIRED | runBackup composes backup.Backup over live Deps |
| cmd/villa/backup.go | orchestrate.OpenWebUIImage / .Image() | manifest digest source | ✓ WIRED | backup.go:143-144 (no literal) |
| cmd/villa/restore.go | VolumeRm+ReconcileAndWrite+EnsureVolume+VolumeImport | clean-recreate-before-import | ✓ WIRED | restore.go:171 forward + rollback; cmd wiring L225-267 |
| internal/backup/restore.go | Deps.Prove / ProveStatusPass | offload-asserting cutover | ✓ WIRED | restore.go:283-285 |
| cmd/villa/restore.go | config.SaveVilla + orchestrate WriteUnits + preflight + status | liveRestoreDeps | ✓ WIRED | ReconcileAndWrite + liveRestoreProve compose preflight+status residency |

### Behavioral Spot-Checks

| Behavior | Command | Result | Status |
| -------- | ------- | ------ | ------ |
| Build static binary | `CGO_ENABLED=0 go build ./cmd/villa` | exit 0 | ✓ PASS |
| Full suite | `go test ./...` | 728 passed in 20 packages | ✓ PASS |
| go vet | `go vet ./...` | no issues | ✓ PASS |
| backup pkg | `go test ./internal/backup -v` | 43 PASS, 0 FAIL | ✓ PASS |
| cmd backup/restore | `go test ./cmd/villa -run 'Backup\|Restore\|PodmanVolume\|Version'` | 13 PASS, 0 FAIL | ✓ PASS |
| seam gate | `go test ./internal/inference -run TestSeamGrepGate` | exit 0 | ✓ PASS |
| backup help | `villa backup --help` | registered | ✓ PASS |
| restore help | `villa restore --help` | registered | ✓ PASS |
| purity | `grep os/exec internal/backup/` | none | ✓ PASS |
| version stamp | `villa version` | `dev` (ldflags-overridable) | ✓ PASS |

### Requirements Coverage

| Requirement | Source Plan | Description | Status | Evidence |
| ----------- | ---------- | ----------- | ------ | -------- |
| BAK-01 | 16-01, 16-02 | Back up config + OWUI volume to single archive; excludes model weights; manifest of versions/digests/checksums | ✓ SATISFIED | SC1 + truths 2,8; archive assembly, model exclusion, ExcludedModel identity recorded |
| BAK-02 | 16-03 | Restore transactionally; failed/partial restore never corrupts running stack | ✓ SATISFIED | SC2 + truths 8,9; rollback-verbatim + clean-recreate-before-import on both paths; rollback tests PASS |
| BAK-03 | 16-01, 16-03 | restore warns on version/digest skew before applying | ✓ SATISFIED | SC3 + truth 5; CompareSkew WARN-and-confirm, fail-closed BLOCK on checksum/incompatible-schema |

All 3 requirement IDs declared in PLAN frontmatter and mapped in REQUIREMENTS.md (lines 22-24, 90-92, 108). No orphaned requirements.

### Anti-Patterns Found

| File | Line | Pattern | Severity | Impact |
| ---- | ---- | ------- | -------- | ------ |
| — | — | No TODO/FIXME/XXX/TBD/HACK/PLACEHOLDER in any phase-modified non-test file | — | None |

`--json` for `villa restore` is intentionally NOT implemented (D-13, documented in restore.go header + 16-CONTEXT). This is a deliberate scope decision, not debt.

### Human Verification Required

The following are MANUAL UAT items requiring a live rootless-Podman gfx1151 Fedora host. They cannot be exercised off-hardware (no live Open WebUI volume / running SQLite WAL / foreign-host UID-remap available in this environment). They are the ONLY items blocking `passed`; per Phase 15 precedent and the phase notes, these route to human verification, NOT gaps.

1. **Same-host backup→restore round-trip** — `villa backup -o /tmp/b.tar` → mutate a chat → `villa restore /tmp/b.tar` → chats restored, `villa status` green + residency-proven.
2. **Clean-volume-before-import (no stale data leak)** — restore over a volume containing a stray file; confirm it is gone post-restore.
3. **OWUI live-SQLite quiesce yields importable DB** — stop service, export, restart; `webui.db` opens clean after restore.
4. **Cross-host / post-`podman system reset` restore (KNOWN best-effort LIMITATION)** — restore on a reset/foreign host; apply documented `podman unshare chown -R` remediation if perms fail; document outcome honestly.
5. **Skew WARN + confirm gate on a real version/digest mismatch** — restore an older-manifest archive; confirm WARN + remediation prints and apply waits for confirm; `--yes`/`--force` bypasses.

### Gaps Summary

No gaps. All 17 automated must-haves are VERIFIED in the codebase: `internal/backup` is a pure, fully-tested transactional core; `villa backup` and `villa restore` are registered and wired through live Deps; image digests are seam-sourced (TestSeamGrepGate green); clean-recreate-before-import is enforced on BOTH the forward apply and the rollback path; skew is WARN-and-confirm with fail-closed BLOCK only on checksum/incompatible-manifest. The 1 BLOCKER + 6 WARNINGS + IN-01 from the code-review gate are all confirmed landed (CR-01 RemoveFile-on-rollback, WR-01 temp-dir cleanup, WR-04 bounded reads verified in code). `make check`, the seam gate, and purity all pass at HEAD (728 tests, 20 packages).

Status is **human_needed** solely because 5 on-hardware UAT behaviors require a live gfx1151 host. Automated coverage (pure-core + cmd-tier `go test` + build + seam/purity gates) is complete and green.

---

_Verified: 2026-06-07_
_Verifier: Claude (gsd-verifier)_
