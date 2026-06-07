---
phase: 16-backup-restore
plan: 02
subsystem: backup
tags: [backup, podman-volume, quiesce, manifest, seam, store-schema-accessor, tar]
dependency_graph:
  requires:
    - "internal/backup.Manifest / BuildManifest / ManifestInput (16-01)"
    - "internal/backup writeArchive + sum (16-01)"
    - "internal/backup.Deps / Result (16-01)"
    - "orchestrate.OpenWebUIImage() (16-01)"
    - "cmd/villa villaVersion() (16-01)"
  provides:
    - "internal/backup.Backup() pure quiesce->export->assemble->restart orchestrator"
    - "internal/backup.BackupInput (plain-data drive) + marshalManifest"
    - "cmd/villa shared podman VOLUME seam: volumeExportArgs/volumeImportArgs/podmanVolume/requirePodman"
    - "usage.SchemaVersion() exported accessor over usageSchemaVersion"
    - "benchstore.SavedReportSchemaVersion() exported accessor over savedReportSchemaVersion"
    - "orchestrate.OpenWebUIVolumeName() accessor (volume name behind the seam)"
    - "villa backup cobra command (BAK-01), registered in root"
  affects:
    - "cmd/villa (Plan 03 villa restore composes the same podman volume seam + Backup-style Deps)"
    - "internal/backup (Plan 03 adds the transactional Restore over the same Deps)"
tech_stack:
  added: []          # stdlib only — archive/tar, crypto/sha256, encoding/json, os, time
  patterns:
    - "pure-core + injectable-seam (Backup over Deps; podman/systemd/fs in cmd)"
    - "cmd-tier fixed-arg podman volume seam (cloned from uninstall.go podmanVolumeRm — D-02)"
    - "image digest seam-sourced, never re-typed (TestSeamGrepGate green — D-10)"
    - "store schema version via exported one-line accessor mirroring an unexported const"
    - "OWUI quiesce: stop before export, defer best-effort restart (D-05)"
    - "0600 traversal-guarded output; corrupt partial archive removed on failed write"
key_files:
  created:
    - cmd/villa/backup.go
    - cmd/villa/backup_test.go
    - cmd/villa/podman_volume.go
    - cmd/villa/podman_volume_test.go
  modified:
    - internal/backup/backup.go
    - internal/backup/manifest.go
    - internal/backup/backup_test.go
    - internal/usage/usage.go
    - internal/usage/usage_test.go
    - internal/benchstore/benchstore.go
    - internal/benchstore/benchstore_test.go
    - internal/orchestrate/openwebui.go
    - cmd/villa/root.go
decisions:
  - "Backup reads d.OpenWebUIServiceName (Deps field), not BackupInput, for the quiesce target — keeps the service identity a seam field (mirrors backendswap)."
  - "VillaConfig has no schema_version field; manifest ConfigSchemaVersion is left 0 (= not recorded), which CompareSkew treats as 'absent' (never blocks). usage/bench versions ARE recorded via the new accessors."
  - "Default backup name villa-backup-<timestamp>.tar uses an FS-safe basic-RFC3339 stamp (20060102T150405Z, no ':') per D-04."
  - "Excluded-model identity is sourced from config (the single source of truth: Model/Quant/Ctx), not a catalog lookup — identity only, recorded for re-pull (BAK-01)."
  - "Temp volume-export file is created in the OUTPUT's parent dir (same filesystem) and removed via defer; the archive is the only durable artifact."
  - "orchestrate.OpenWebUIVolumeName() added so the cmd layer sources the podman volume name from the orchestrate seam, never a re-typed literal."
metrics:
  duration: ~12m
  completed: 2026-06-07
  tasks: 2
  files_created: 4
  files_modified: 9
---

# Phase 16 Plan 02: `villa backup` (quiesce -> volume export -> single .tar) Summary

`villa backup` ships end-to-end (BAK-01): it briefly stops Open WebUI for a clean
SQLite copy, `podman volume export`s the OWUI data volume through a shared cmd-tier
fixed-arg seam, assembles a single self-describing `.tar` (manifest.json +
config.toml + openwebui-volume.tar + usage.json + the single bench-reports.jsonl)
with seam-sourced image digests and accessor-sourced store schema versions, records
the excluded model-weight identities for re-pull, restarts Open WebUI, and writes the
archive 0600 to a traversal-guarded path. Model weights are excluded.

## What was built

**Task 1 — pure orchestrator + shared seam + accessors (commit `e10e5b4`):**
- `internal/backup/backup.go` — `Backup(d Deps, in BackupInput) (Result, error)`: pure
  quiesce->export->assemble->restart ordering. Stops `d.OpenWebUIServiceName`, DEFERS a
  best-effort restart (fires even on mid-backup error — D-05), exports the OWUI volume
  to a temp tar, reads config.toml + usage.json + the single bench-reports.jsonl
  (absent optional data-dir artifact is skipped via `FileMissing`), computes per-entry
  SHA-256, `BuildManifest` with injected seam digests + accessor store schema versions
  + excluded-model identities, and `writeArchive` (manifest.json FIRST) to the 0600
  writer the caller opened. The villa-models volume is never exported. Imports no exec
  package, no image literal.
- `internal/backup/manifest.go` — `marshalManifest` (indented, human-readable JSON;
  not golden-frozen — D-13).
- `cmd/villa/podman_volume.go` — shared fixed-arg podman VOLUME seam (D-02): pure
  `volumeExportArgs`/`volumeImportArgs` builders, injectable `podmanVolume` var
  (`exec.Command("podman", args...)`, no shell), and a `requirePodman` LookPath guard
  returning `orchestrate.ErrToolNotFound`. Cloned from `uninstall.go`'s `podmanVolumeRm`.
- `internal/usage.SchemaVersion()` / `internal/benchstore.SavedReportSchemaVersion()` —
  exported one-line accessors over the unexported consts, each with a mirror-guard
  test so the manifest fields can never silently desync from the stores.
- `internal/orchestrate.OpenWebUIVolumeName()` — accessor so the volume name stays
  behind the orchestrate seam.

**Task 2 — cobra command + live wiring + registration (commit `227ac9f`):**
- `cmd/villa/backup.go` — `newBackup()` cobra command + `runBackup` (RETURNS the exit
  code; the RunE wrapper calls `os.Exit`, mirroring `runUninstall`). Flags
  `-o/--output` (default `villa-backup-<timestamp>.tar`, FS-safe stamp, no ':'). Resolves
  + traversal-guards the output path against its parent (`assertBackupOutputInside`),
  opens it 0600, builds `BackupInput` sourcing the inference digest via
  `inference.BackendFor(cfg.Backend).Image()`, the OWUI digest via
  `orchestrate.OpenWebUIImage()`, the store schema versions via the new accessors, the
  bench path via the existing cmd-tier `benchReportsStorePath()`, the config path via
  `config.Path()`, usage via `usage.UsagePath()`, and the host fingerprint via a
  `.Known`-guarded `detect.Probe()` flatten (Unknown -> "" sentinel, never fabricated).
  On any failure the partial output file is removed so a corrupt archive is never left.
- `liveBackupDeps()` wires Stop/Start via `orchestrate.NewSystemd()`, VolumeExport via
  the shared `podmanVolume(volumeExportArgs(...))` (with `requirePodman` guard), and
  ReadFile via `os.ReadFile`.
- `cmd/villa/root.go` — `newBackup()` registered in `newRoot`.

## Verification

- `go test ./internal/backup/ ./internal/usage/ ./internal/benchstore/ ./cmd/villa/ -count=1` — green (Backup ordering: stop-before-export + deferred-restart-on-error; exact entry-name set incl. single bench-reports.jsonl, models volume excluded; manifest seam digests + accessor store schema versions + excluded identities; absent optional artifact skipped; arg-builder fixed-arg equality + no-shell-metachar; accessor mirror-guards; default-name FS-safe; output traversal refusal; end-to-end runBackup -> 0600 archive with @sha256-pinned digests).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — green (no image literal in cmd/villa or internal/backup).
- `grep -rn 'os/exec' internal/backup/` — empty (pure core).
- `make build` -> `./villa`; `./villa backup --help` lists `-o/--output`.
- `make check` (go vet + `go test ./...`) — green across the whole module.

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 3 - Blocking] `os/exec` token in a Backup doc comment tripped the acceptance grep**
- **Found during:** Task 1 acceptance (`grep -rn 'os/exec' internal/backup/` matched prose "imports no os/exec", though the package imports no exec).
- **Issue:** The acceptance criterion is a literal grep; doc-comment prose containing the token would fail it (same class as the 16-01 deviation).
- **Fix:** Rephrased the comment ("links the exec package NOT at all"); the package genuinely imports no exec.
- **Files modified:** internal/backup/backup.go
- **Commit:** e10e5b4

**2. [Rule 3 - Blocking] `go fmt ./cmd/villa/` reformatted an out-of-scope pre-existing file**
- **Found during:** Task 1 formatting.
- **Issue:** A package-wide `go fmt ./cmd/villa/` reformatted `cmd/villa/bench_compare.go` (a pre-existing file not in this plan's scope, and a borderline comment reflow).
- **Fix:** Reverted `bench_compare.go` (`git checkout --`); formatted only this plan's files via `gofmt -w` on explicit paths thereafter. Out-of-scope per the SCOPE BOUNDARY rule.
- **Files modified:** none (reverted)
- **Commit:** n/a

### Plan-internal decisions (not deviations)
- The plan's Task-1 text referenced `Backup` reading the OWUI service name; the
  service identity lives on `Deps.OpenWebUIServiceName` (the 16-01 seam shape), so
  Backup reads `d.OpenWebUIServiceName` rather than a `BackupInput` field. No behaviour
  change — consistent with backendswap.
- `BackupInput.ConfigSchemaVersion` is 0 because `VillaConfig` carries no
  schema_version field; CompareSkew already treats `<= 0` as "not recorded" (never
  blocks). usage/bench versions ARE recorded via the accessors as specified.

## Threat Flags

None — the plan's threat_model items were all implemented as registered: output
path traversal-guarded + 0600 (T-16-02a/d), podman volume export fixed-arg with a
config-resolved volume name (T-16-02b), digests seam-sourced with TestSeamGrepGate
green (T-16-02c), and OWUI stop-before-export + deferred restart (T-16-02e). No new
security surface beyond these.

## Known Stubs

None — `villa backup` is fully wired end-to-end (config + volume export + usage +
bench + manifest), driven and asserted off-hardware. The remaining bar is the
deferred on-hardware UAT (gfx1151 round-trip + clean webui.db after quiesce), which
the plan's verification block explicitly defers to the phase gate.

## Self-Check: PASSED

- cmd/villa/{backup,podman_volume}.go + their _test.go — FOUND
- internal/backup/{backup,manifest,backup_test}.go — FOUND (modified)
- internal/{usage,benchstore}/{*,*_test}.go accessors — FOUND (grep-confirmed)
- internal/orchestrate/openwebui.go OpenWebUIVolumeName() — FOUND
- cmd/villa/root.go newBackup() registered — FOUND
- Commit e10e5b4 — FOUND; Commit 227ac9f — FOUND
