---
phase: 16-backup-restore
plan: 03
subsystem: backup
tags: [restore, transactional, rollback, clean-recreate, podman-volume, skew, prove, offload-asserting, seam]
dependency_graph:
  requires:
    - "internal/backup.Deps / Result / ProveVerdict / ProveStatusPass (16-01)"
    - "internal/backup.CompareSkew / SkewVerdict / SkewWarning / CurrentInstall (16-01)"
    - "internal/backup verify (checksum) + readArchive (tar-slip guard) (16-01)"
    - "internal/backup.Manifest / EntryChecksum / Entry* consts / marshalManifest (16-01)"
    - "cmd/villa shared podman VOLUME seam: volumeImportArgs/volumeExportArgs/volumeRmArgs/podmanVolume/requirePodman (16-02)"
    - "orchestrate.OpenWebUIImage() / OpenWebUIVolumeName() (16-01/16-02)"
    - "backendswap liveProve composition (Phase 8) — reused for the residency assert"
  provides:
    - "internal/backup.Restore() pure transactional state-machine (clean-recreate-before-import on apply AND rollback)"
    - "internal/backup.RestoreInput (plain-data drive: archive opener, current facts, consent+bypass, volume/dest paths)"
    - "internal/backup parseManifest (inverse of marshalManifest)"
    - "config.Parse([]byte) — in-memory config.toml parse (restore config source-of-truth)"
    - "villa restore <archive> cobra command (BAK-02/BAK-03), registered in root"
    - "cmd/villa liveRestoreDeps + liveRestoreProve (offload-asserting: preflight + status residency)"
  affects:
    - "future phases: restore is the transactional consumer of the backup archive contract"
tech_stack:
  added: []          # stdlib only — archive/tar, crypto/sha256, encoding/json, io, os
  patterns:
    - "pure-core + injectable-seam (Restore over Deps; podman/systemd/fs/prove in cmd)"
    - "transactional capture-before-mutate + verbatim rollback (clone of backendswap.Run)"
    - "clean-recreate-before-import: VolumeRm->ReconcileAndWrite->EnsureVolume->VolumeImport on apply AND rollback"
    - "offload-asserting prove: health-200-but-residency-FAIL maps to NON-pass -> rollback (never false-green)"
    - "fail-closed BLOCK on checksum mismatch / incompatible manifest schema (zero side effects)"
    - "WARN-and-confirm skew gate, --yes/--force bypass; consent opt-in, non-interactive declines"
    - "config restored via config.SaveVilla (never hand-written); data-dir via atomic WriteFileAtomic 0600/0700"
    - "image digests seam-sourced, never re-typed (TestSeamGrepGate green)"
key_files:
  created:
    - internal/backup/restore.go
    - internal/backup/restore_test.go
    - cmd/villa/restore.go
    - cmd/villa/restore_test.go
  modified:
    - internal/backup/manifest.go
    - internal/config/villaconfig.go
    - cmd/villa/root.go
decisions:
  - "Archive read is a TWO-pass design over an OpenArchive opener (verify pass + extract pass) so the input reader need not be seekable; readArchive's tar-slip guard runs on every entry of every pass."
  - "Restored config is parsed from the archive's config.toml via a new config.Parse([]byte) (mirrors LoadVilla's unmarshal+normalize), then persisted via config.SaveVilla — config stays the single source of truth the Quadlet recreate renders from (D-07)."
  - "liveRestoreProve composes the ROCm preflight gate (rocm targets) + the proven liveProve residency assert, mapping ONLY a true offload-honest pass to ProveStatusPass; it re-uses Phase-8's liveProve so no backend marker is re-typed (D-07)."
  - "--force is the inherited global persistent flag (root.go); the subcommand registers only --yes and reads the global force in liveRestore, avoiding a duplicate-flag panic."
  - "VolumeRm tolerates not-found and EnsureVolume tolerates already-exists via stderr inspection (mirrors removeVolumesLive) so the clean-recreate is idempotent."
metrics:
  duration: ~22m
  completed: 2026-06-07
  tasks: 2
  files_created: 4
  files_modified: 3
---

# Phase 16 Plan 03: `villa restore` (transactional clean-recreate restore) Summary

`villa restore <archive>` ships end-to-end (BAK-02/BAK-03): it verifies the archive's
per-entry SHA-256 checksums (a corrupt archive or an incompatible manifest schema is a
fail-closed BLOCK with ZERO side effects), warns-and-confirms on version/digest/store-
schema skew (bypass with `--yes`/`--force`), captures the current state STRICTLY before
mutating, briefly quiesces Open WebUI, restores config via `config.SaveVilla` and the
data-dir artifacts atomically, **clean-recreates** the Open WebUI volume (VolumeRm ->
Quadlet recreate -> EnsureVolume create -> import) so a merge-import never leaks stale
chats/webui.db, restarts, PROVEs the cutover offload-honestly, and rolls back verbatim
on any mutate error or non-pass prove — with honest rollback-complete/incomplete
reporting. The clean-recreate ordering is enforced on BOTH the forward apply AND the
rollback path.

## What was built

**Task 1 — pure transactional Restore() state-machine (commit `386f3dd`):**
- `internal/backup/restore.go` — `Restore(d Deps, in RestoreInput) Result` cloning the
  `backendswap.Run` frame around the RESEARCH §Transactional Restore ordering:
  read+verify -> skew (WARN-and-confirm / fail-closed BLOCK) -> capture-before-mutate
  -> quiesce -> MUTATE (SaveConfig -> data-dir -> clean-recreate owui volume -> import
  -> start) -> offload-asserting prove -> rollback-on-failure. The load-bearing fact
  (`podman volume import` MERGES + does NOT auto-create) is honored by a
  `cleanRecreateThenImport` closure (VolumeRm not-found-tolerant -> ReconcileAndWrite
  Quadlet recreate from the restored config -> EnsureVolume explicit create ->
  VolumeImport) used on apply AND rollback. `readAndVerify` is a pure two-pass read
  (parse manifest FIRST + verify every entry's SHA-256), fail-closed on an
  unreadable/newer manifest schema. Imports no inference/detect/exec — the prove
  sentinel is local (TestSeamGrepGate stays green).
- `internal/backup/manifest.go` — `parseManifest` (inverse of marshalManifest).
- `internal/config/villaconfig.go` — `Parse([]byte)` (in-memory config.toml parse,
  seeds defaults + self-heals loopback exactly as LoadVilla, never widens the bind).
- `restore_test.go` — 9 fake-driven tests: verify-fail/incompatible-schema/BLOCK refuse
  with ZERO mutate calls; WARN+consent-denied refuses; `--yes`/Bypass proceeds; forward
  clean-recreate ordering (VolumeRm<ReconcileAndWrite<EnsureVolume<VolumeImport); a
  mutate error rolls back and re-imports the CAPTURED tar through the SAME ordering (2
  VolumeRm + 2 VolumeImport, rollback uses RollbackVolumeTar); a rollback-step error
  yields RolledBack:true + an honest rollback-incomplete Reason; a non-pass prove rolls back.

**Task 2 — `villa restore` cobra command + live wiring + registration (commit `7189fd4`):**
- `cmd/villa/restore.go` — `newRestore()` (positional archive arg, `--yes` flag;
  `--force` is the inherited global), `runRestore` Result->exit mapping (Restored ->
  exitPass; Refused/RolledBack -> exitBlocked with honest messages), `liveRestore`
  (archive path resolve + stat, seam-/accessor-sourced `CurrentInstall`, temp-dir for
  the extracted/rollback volume tars), and `liveRestoreDeps` wiring every seam:
  SaveConfig=config.SaveVilla, the shared fixed-arg podman volume export/import/rm +
  an explicit `podman volume create` EnsureVolume, the Quadlet recreate closure
  (Render->Reconcile->WriteUnits->DaemonReload from the restored config), Stop/Start/
  Restart via orchestrate.NewSystemd, WriteFileAtomic=usage.WriteFileAtomic, and
  `liveRestoreProve` (ROCm preflight + the proven Phase-8 liveProve residency assert,
  mapping ONLY a true offload-honest pass to ProveStatusPass). `liveSkewConsent` clones
  the uninstall.go stdinIsInteractive + promptConsent gate (non-interactive declines).
- `cmd/villa/root.go` — `newRestore()` registered in `newRoot`.
- `restore_test.go` — 6 tests over a fake `backup.Deps` + an in-memory valid archive
  built from the EXPORTED backup builders: positional-arg required; happy path exitPass;
  consent-denied exitBlocked; `--yes` bypasses consent; offload-FAIL prove rolls back
  exitBlocked; corrupt archive fail-closed BLOCK.

## Verification

- `go test ./internal/backup/ -run TestRestore -count=1` — green (9 tests).
- `go test ./cmd/villa/ -run TestRestore -count=1` — green (6 tests).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — green (no image
  literal leaked into internal/backup or cmd/villa restore wiring).
- `grep -rn 'internal/inference\|internal/detect' internal/backup/restore.go` — empty
  (sentinel local; seam discipline).
- `grep -n 'config.SaveVilla' cmd/villa/restore.go` — matches; `grep -nE
  'EnsureVolume|volume.*create'` — matches (explicit ensure-create before import);
  `grep -nE 'OpenWebUIImage|BackendFor'` — matches (digests from the seam).
- `./villa restore --help` lists `--yes`/`--force` and takes a positional archive.
- `make check` (go vet + `go test ./...`) — green across the whole module.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `internal/inference`/`internal/detect` tokens in a restore.go
doc comment tripped the acceptance grep**
- **Found during:** Task 1 acceptance (`grep -rn 'internal/inference\|internal/detect'
  internal/backup/restore.go` matched a doc-comment line, though the package imports
  neither — same class as the 16-01/16-02 deviations).
- **Issue:** The acceptance criterion is a literal grep; doc-comment prose naming the
  tokens would fail it despite zero import.
- **Fix:** Rephrased the comment ("links NO inference and NO detect package"); the
  package genuinely imports neither.
- **Files modified:** internal/backup/restore.go
- **Commit:** 386f3dd

### Plan-internal decisions (not deviations)
- The plan's Task-1 text referenced `d.OpenArchive`; the 16-01 `Deps` shape carries no
  archive opener (it is host-I/O the cmd layer owns), so the opener is a `RestoreInput`
  field (`OpenArchive func() (io.ReadCloser, error)`) — consistent with the pure-core/
  seam split (the archive bytes are plain input, not a host effect of the core).
- The plan's prove text said "compose preflight + a status residency assert";
  `liveRestoreProve` composes the ROCm preflight gate + the proven Phase-8 `liveProve`
  (which already folds readiness + a real generation probe + the residency proof) rather
  than re-implementing the residency math — the lightest offload-honest path (CONTEXT
  "Claude's Discretion").

## Threat Flags

None — the plan's threat_model items were all implemented as registered: tar-slip guard
on extraction (T-16-03a, reused readArchive); per-entry SHA-256 fail-closed BLOCK
(T-16-03b); incompatible/future manifest schema BLOCK (T-16-03c); transactional
capture-before-mutate + verbatim rollback with honest incomplete reporting (T-16-03d);
clean-recreate-before-import on apply AND rollback (T-16-03e); fixed-arg podman/systemd
exec with config-resolved volume/service names (T-16-03f); offload-asserting prove =
FAIL -> rollback, never health-200-as-success (T-16-03g); no new outbound, status
no_telemetry preserved (T-16-03h); config via SaveVilla 0600/0700 + data-dir atomic
0600/dirs 0700 (T-16-03i). No package installs (T-16-SC). No new security surface beyond
these.

## Known Stubs

None — `villa restore` is fully wired end-to-end (verify + skew + capture + quiesce +
config + data-dir + clean-recreate + import + restart + prove + rollback), driven and
asserted off-hardware. The remaining bar is the deferred on-hardware UAT below.

## On-Hardware UAT (phase gate — deferred, do NOT run live podman here)

- **clean-recreate-no-merge:** backup -> write a stray file / extra chat into the live
  OWUI volume -> `villa restore <archive>` -> assert the clean-recreate path runs
  (VolumeRm -> Quadlet recreate -> EnsureVolume `podman volume create` -> VolumeImport)
  and the stray/old data is GONE (no merge survivor).
- **same-host round-trip:** backup -> mutate -> restore round-trip on gfx1151 — chats
  restored, OWUI starts healthy with residency-proven status.
- **live-SQLite quiesce:** verify the brief OWUI stop during restore yields a clean
  importable DB (no torn copy).
- **cross-version skew:** skew WARN+confirm on a cross-version archive; `--yes` bypass.
- **cross-host best-effort:** document the cross-host limitation honestly (the
  `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` + `:Z` relabel remediation is
  already surfaced in the host-skew WARN).

## Self-Check: PASSED

- internal/backup/restore.go + restore_test.go — FOUND
- cmd/villa/restore.go + restore_test.go — FOUND
- internal/backup/manifest.go (parseManifest), internal/config/villaconfig.go (Parse),
  cmd/villa/root.go (newRestore registered) — FOUND (grep-confirmed)
- Commit 386f3dd — FOUND; Commit 7189fd4 — FOUND
