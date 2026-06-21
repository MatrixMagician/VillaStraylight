---
phase: 34-web-search-surfacing-lands-last
plan: 02
subsystem: backup
tags: [backup, restore, searxng, web-search, schema-bump, SURF-07]
status: complete
requires:
  - "internal/backup (crush.json EntryCrushConfig optional-entry precedent, Phase 28)"
  - "internal/orchestrate WriteSearxngSettings / searxngSettingsDir (Phase 29)"
  - "config.WebSearchEnabled gate (Phase 31)"
provides:
  - "backup.EntrySearxngSettings optional archive entry + backupSchemaVersion 4"
  - "backup.BackupInput.SearxngSettingsPath (optional, web-search-gated)"
  - "backup.RestoreInput.SearxngSettingsDestPath + WriteSearxngSettings Deps seam"
  - "backup.Result.SearxngSettings{Restored,Skipped}"
  - "orchestrate.SearXNGSettingsFilePath() exported host-path accessor"
affects:
  - "internal/backup/{manifest,backup,restore,deps}.go"
  - "internal/orchestrate/searxng_settings_write.go"
  - "cmd/villa/{backup,restore}.go"
tech-stack:
  added: []
  patterns: ["optional FileMissing-skipped archive entry", "out-of-store-root restore seam", "append-only manifest schema bump", "0600-preserving secret restore"]
key-files:
  created: []
  modified:
    - internal/backup/manifest.go
    - internal/backup/backup.go
    - internal/backup/backup_test.go
    - internal/backup/restore.go
    - internal/backup/restore_test.go
    - internal/backup/deps.go
    - internal/backup/manifest_test.go
    - internal/orchestrate/searxng_settings_write.go
    - cmd/villa/backup.go
    - cmd/villa/restore.go
decisions:
  - "EntrySearxngSettings = \"searxng-settings.yml\"; backupSchemaVersion 3->4 (single SURF-07 append-only bump)"
  - "WebSearchEnabled gate is covered by the EXISTING EntryConfig (config.toml carries web_search_enabled); the NEW entry is the settings.yml provenance only"
  - "Ephemeral fetched web content is EXCLUDED by design (SURF-07/T-34-06) — no query/URL log entry"
  - "settings.yml restored through a DEDICATED WriteSearxngSettings out-of-store-root seam (mirrors WriteCrushConfig), FORCING 0600 (holds the rendered SEARXNG_SECRET, T-34-05)"
metrics:
  tasks: 2
  files: 10
  commits: 2
  duration: ~30 min
  completed: 2026-06-21
---

# Phase 34 Plan 02: Web-Search Backup/Restore Coverage Summary

Extended `villa backup`/`restore` to cover the web-search CONFIGURATION — the rendered SearXNG `settings.yml` provenance — as an OPTIONAL archive entry mirroring the Phase-28 `crush.json` (`EntryCrushConfig`) pattern, on backup's OWN schema bump (`backupSchemaVersion` 3→4). Restore re-writes the entry 0600-preserving (it holds the rendered `SEARXNG_SECRET`) and SHA-256-verifies it through the same path as every member. Fetched ephemeral web content is excluded by design.

## What Was Built

### Task 1 — optional settings.yml backup entry + manifest schema 3→4 (commit `4328f08`)
- `manifest.go`: added `const EntrySearxngSettings = "searxng-settings.yml"` beside `EntryCrushConfig`; bumped `backupSchemaVersion` 3→4 with a v4 history note (SURF-07). The manifest stamps its OWN version in `BuildManifest` (never a caller value).
- `backup.go`: added the OPTIONAL `SearxngSettingsPath string` field to `BackupInput` and one `sources` row `{EntrySearxngSettings, in.SearxngSettingsPath, false}` (required=false → FileMissing-skipped). No entry for ephemeral content.
- `backup_test.go`: `TestBackupSearxngSettings` (present-when-set / skipped-when-empty / FileMissing-skipped-when-absent / schema stamps 4) and `TestBackupExcludesEphemeral` (negative: no archive entry or manifest checksum references a query/URL/page-content/ephemeral key).

### Task 2 — restore branch (0600-preserving) + cmd-tier web-search gating (commit `7d56500`)
- `restore.go`: added `SearxngSettingsDestPath` to `RestoreInput`; `searxngSettings`/`searxngSettingsPresent` to the extracted payload; the `readAndVerify` mapping (SHA-256-verified through the same pass); the forward MUTATE write + verbatim rollback rows (write-prior / remove-forward-created) mirroring crush.json; a `writeSearxngSettings` helper; and `SearxngSettings{Restored,Skipped}` on the success `Result`.
- `deps.go`: added the `WriteSearxngSettings` out-of-store-root seam (distinct from the store-root-guarded `WriteFileAtomic`) + the two new `Result` fields.
- `orchestrate/searxng_settings_write.go`: added `const searxngSettingsFileName = "settings.yml"` and the exported `SearXNGSettingsFilePath()` host-path accessor (so the cmd tier sources the path without re-typing dir/filename literals — mirrors how `crushConfigPath()` is resolved).
- `cmd/villa/backup.go` + `restore.go`: set `SearxngSettingsPath` (backup) / `SearxngSettingsDestPath` (restore) ONLY when `cfg.WebSearchEnabled`; added honest "web search: settings.yml included/restored/skipped" reporting; wired the live `WriteSearxngSettings` seam (MkdirAll 0700 + traversal-guard + WriteFile 0600).
- `restore_test.go`: `TestRestoreSearxngSettings` — present→written with an EXACT 0600 on-disk mode assertion (T-34-05), absent→not-present, present-onto-web-off→skipped (no false-green), tampered→fail-closed Refused with zero side effects (T-34-07).

## Verification Results
- `go test ./internal/backup/ -run 'TestBackupSearxngSettings|TestBackupExcludesEphemeral'` — PASS
- `go test ./internal/backup/ -run 'TestRestoreSearxngSettings'` — PASS (incl. the 0600 mode assertion)
- `go test ./internal/backup/ ./cmd/villa/ -run 'TestBackup|TestRestore'` — PASS (existing tests stay green)
- `make check` (vet + full suite) — PASS
- `TestSeamGrepGate` — PASS (no leaked backend/image literals; settings.yml path resolved via the orchestrate accessor)

## Accepted Scope Limit (per plan)
- The `WebSearchEnabled` GATE itself is ALREADY archived via `config.toml` (`web_search_enabled` → the existing `EntryConfig`). This plan adds the `settings.yml` PROVENANCE only.
- Fetched EPHEMERAL web content is intentionally NOT archived (SURF-07): no query log, no fetched-URL log, no per-page content key — asserted by `TestBackupExcludesEphemeral`.

## Threat Mitigations Applied
- **T-34-05 (Information Disclosure):** restore forces 0600 and never widens the mode — the entry holds the rendered `SEARXNG_SECRET`. Asserted by the on-disk mode test.
- **T-34-06 (Privacy regression):** ephemeral content excluded by design; negative test guards no query/URL log entry is added.
- **T-34-07 (Tampering):** settings.yml is SHA-256-verified through the same `readAndVerify` pass as every member; a tampered entry is a fail-closed Refused with zero side effects.

## Deviations from Plan

### [Rule 3 - Blocking] Restore-side cmd gating lives in `cmd/villa/restore.go`, not `backup.go`
- **Found during:** Task 2
- **Issue:** The plan's `files_modified` listed only `cmd/villa/backup.go` for the cmd tier, and Task 2's action text said to set both `SearxngSettingsPath` (backup) and `SearxngSettingsDestPath` (restore) "in cmd/villa/backup.go". In this codebase the restore-side wiring (the `WebSearchEnabled`-gated dest path + the live `WriteSearxngSettings` seam) lives in the separate `cmd/villa/restore.go` (mirroring how crush.json restore is wired there). Wiring restore there is required for the feature to function.
- **Fix:** Set `SearxngSettingsPath` in `cmd/villa/backup.go` (gated on `cfg.WebSearchEnabled`) and `SearxngSettingsDestPath` + the live seam in `cmd/villa/restore.go` (same gate). Both mirror the existing crush.json gating exactly.
- **Files modified:** cmd/villa/restore.go (added beyond the plan's listed set)
- **Commit:** `7d56500`

### [Rule 1 - Test tracking] Renamed/updated the schema pin test to v4
- **Found during:** Task 2 (`make check`)
- **Issue:** `TestManifestSchemaVersionIsV3` pinned `backupSchemaVersion != 3` — it failed after the deliberate SURF-07 bump to 4.
- **Fix:** Renamed to `TestManifestSchemaVersionIsV4`, asserting `backupSchemaVersion == 4`, `EntryCrushConfig == "crush.json"` (still present), and `EntrySearxngSettings == "searxng-settings.yml"`. This is the intended append-only schema-bump tracking, not a behavior change.
- **Files modified:** internal/backup/manifest_test.go
- **Commit:** `7d56500`

### [Hygiene] Added an exported orchestrate path accessor (anticipated by the plan)
- The plan explicitly said "add/use an exported accessor if `searxngSettingsDir` is unexported." `searxngSettingsDir()` was unexported and there was no `settings.yml` host-path accessor, so `SearXNGSettingsFilePath()` + `searxngSettingsFileName` were added (no re-typed literals; `render.go`'s `RenderSearxngSettings` return literal left untouched to avoid touching the golden path).

## Known Stubs
None. Both backup source and restore destination are wired to real host paths via the orchestrate accessor and gated on the authoritative `cfg.WebSearchEnabled`.

## Self-Check: PASSED
- FOUND: internal/backup/manifest.go (EntrySearxngSettings, backupSchemaVersion = 4)
- FOUND: internal/backup/backup.go (SearxngSettingsPath)
- FOUND: internal/backup/restore.go (SearxngSettingsDestPath, writeSearxngSettings)
- FOUND: internal/orchestrate/searxng_settings_write.go (SearXNGSettingsFilePath)
- FOUND commit 4328f08, FOUND commit 7d56500 (git log)
