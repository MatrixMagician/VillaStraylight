---
phase: 29-searxng-search-service
plan: 02
subsystem: orchestrate
tags: [searxng, web-search, settings, secret, atomic-write, 0600, traversal-guard]
requires:
  - "orchestrate.RenderSearxngSettings + RenderSearxngSecretEnv (Plan 01 pure renders)"
  - "orchestrate.SearXNGSecretEnvFilePath / searxngSecretEnvFileName (Plan 01 env-file path contract)"
  - "orchestrate.assertInsideDir + atomicWrite discipline (reconcile.go, T-03-02/03)"
provides:
  - "orchestrate.WriteSearxngSettings + WriteSearxngSecretEnv impure 0600 writers (siblings of WriteUnits)"
  - "orchestrate.WriteSearxngSettingsTo + WriteSearxngSecretEnvTo explicit-dir testable seams"
  - "orchestrate.searxngSettingsDir resolver ($XDG_CONFIG_HOME/villa/searxng) + searxngSettingsFileMode (0600) const"
  - "orchestrate.atomicWriteMode (mode-parameterized atomic temp->fsync->rename)"
affects:
  - "Plan 03 (install flow calls WriteSearxngSettings + WriteSearxngSecretEnv before service start)"
tech-stack:
  added: []
  patterns:
    - "impure config-file writer co-located in its own file (searxng_settings_write.go), reconcile.go stays focused (WARNING 4 divergence)"
    - "mode-parameterized atomic write (atomicWriteMode) reuses reconcile.go discipline without duplicating it"
    - "explicit-dir *To seam mirrors config.SaveVillaTo for off-$HOME testability"
    - "0600 own mode const distinct from unitFileMode 0644 — secret env file holds the live secret (BLOCKER 1)"
key-files:
  created:
    - "internal/orchestrate/searxng_settings_write.go"
    - "internal/orchestrate/searxng_settings_write_test.go"
  modified: []
decisions:
  - "New file searxng_settings_write.go rather than a reconcile.go modify (29-PATTERNS WARNING 4) — keeps reconcile.go's idempotency core focused; the assertInsideDir + temp->fsync->rename discipline is reused from the same package, only the location differs."
  - "Factored a single mode-parameterized atomicWriteMode rather than cloning atomicWrite at 0600 — one atomic-write code path, two callers (units 0644, searxng 0600)."
  - "Testability via explicit-dir *To seams (mirrors config.SaveVillaTo) PLUS the live resolver exercised through t.Setenv(XDG_CONFIG_HOME) — no dependency on real $HOME."
metrics:
  tasks_completed: 1
  files_created: 2
  files_modified: 0
  completed: 2026-06-18
status: complete
---

# Phase 29 Plan 02: SearXNG settings.yml + Secret-Env Writers Summary

Atomic, traversal-guarded, 0600 impure writers (`WriteSearxngSettings`,
`WriteSearxngSecretEnv`) — siblings of `WriteUnits` — that persist Plan 01's pure
renders into `$XDG_CONFIG_HOME/villa/searxng/` (never the systemd unit dir), so the
container can mount `settings.yml` read-only at `/etc/searxng:ro,Z` and read its live
secret from the 0600 `searxng.env` the unit references via `EnvironmentFile=` (BLOCKER 1
— the secret never lands in the 0644 unit).

## What Was Built

**Task 1 (TDD; RED commit `9a6f53f`, GREEN commit `6daa455`):** New
`internal/orchestrate/searxng_settings_write.go` with:

- `searxngSettingsFileMode = 0o600` (its OWN const, distinct from reconcile.go's
  `unitFileMode` 0o644) and `searxngSettingsDirMode = 0o700`.
- `searxngSettingsDir() (string, error)` — resolves via `os.UserConfigDir()` (honors
  `$XDG_CONFIG_HOME`, V12), joins `villa/searxng`. This is the host side of BOTH the
  `%h/.config/villa/searxng:/etc/searxng:ro,Z` mount AND the `EnvironmentFile=` path the
  `.container` unit references — never the systemd unit dir (Pitfall 1 / T-29-09).
- `WriteSearxngSettings(name, text)` / `WriteSearxngSecretEnv(name, text)` — live wrappers
  resolving the dir, the callers Plan 03 wires.
- `WriteSearxngSettingsTo(dir, name, text)` / `WriteSearxngSecretEnvTo(dir, name, text)` —
  explicit-dir testable seams (mirror `config.SaveVillaTo`).
- Shared `writeSearxngFile(dir, name, text)`: `MkdirAll(dir, 0700)` → `assertInsideDir`
  (reused from reconcile.go, refuses traversal escapes before any write, T-29-06) →
  `atomicWriteMode` at 0600.
- `atomicWriteMode(target, data, mode)` — mirrors reconcile.go `atomicWrite`'s
  temp→Sync→Close→Rename→dir-fsync ordering byte-for-byte, parameterizing only the file
  mode so the secret-safe 0600 path reuses the proven atomic discipline without a second
  copy of the logic.

Test file `searxng_settings_write_test.go` covers: round-trip equality with
`RenderSearxngSettings` / `RenderSearxngSecretEnv` (single source of truth, no second
renderer), 0600 file + 0700 dir modes for BOTH files (with an explicit guard that
`searxngSettingsFileMode != unitFileMode`), traversal refusal for both writers, atomic
no-`.tmp`-remnant + idempotent re-write, MkdirAll of an absent dir, the live resolver
honoring `$XDG_CONFIG_HOME` and never resolving into a `systemd/user` or
`containers/systemd` dir, and the cross-plan contract that the secret-env host path
matches the host suffix of `SearXNGSecretEnvFilePath()`.

## Deviations from Plan

None — plan executed exactly as written. The deliberate file-placement divergence from
29-PATTERNS.md (new `searxng_settings_write.go` instead of a `reconcile.go` modify) was
specified in the plan's `<objective>` (WARNING 4), not a runtime deviation. No Rule 1–4
auto-fixes were needed.

## Verification

- `go test ./internal/orchestrate/...` — green (all 9 new tests pass: round-trip,
  mode, traversal, atomicity, idempotency, dir-creation, live resolver, live secret-env
  target, secret-env body).
- `go vet ./internal/orchestrate/...` — clean.
- `go fmt ./internal/orchestrate/` — no reformatting (already gofmt-clean).
- `go test ./...` — full repo suite green (no regressions; Plan 01 goldens untouched).
- Acceptance greps: `func WriteSearxngSettings` + `func WriteSearxngSecretEnv` present,
  `0o600` const present, NO unit-dir reference in the writer file.

## Success Criteria Met

- **SRCH-01 (settings persistence):** the rendered `settings.yml` is written atomically,
  traversal-guarded, 0600, into `$XDG_CONFIG_HOME/villa/searxng` (not the unit dir),
  byte-equal to `RenderSearxngSettings` — the file the container mounts to enable json
  format + the bounded engine allowlist.
- **SC#2 (secret route):** the 0600 `searxng.env` secret env file is written into the same
  villa config dir at the exact host path the unit's `EnvironmentFile=` references
  (`SearXNGSecretEnvFilePath()` host side) — the secret reaches the container without ever
  landing in the 0644 unit (BLOCKER 1).

## Threat Mitigations Applied

| Threat | Mitigation (in code) |
|--------|----------------------|
| T-29-06 (tamper: write path) | `assertInsideDir(target, dir)` refuses traversal escapes before any write — asserted for BOTH writers. |
| T-29-07 (partial/observable write) | `atomicWriteMode` temp→fsync→rename; no `.tmp` remnant on failure — asserted. |
| T-29-08 (settings.yml mode disclosure) | Written 0600 (own const, not 0644), dir 0700 — asserted. |
| T-29-09 (file lands in unit dir) | `searxngSettingsDir` joins `villa/searxng`; live-resolver test rejects any `systemd/user` / `containers/systemd` path. |
| T-29-14 (live secret disclosure) | `WriteSearxngSecretEnv` writes the secret env file at 0600; error wrapping never logs the body; host path == `SearXNGSecretEnvFilePath()` (path-only unit ref). |

## Notes for Downstream Plans

- **Plan 03** generates+persists the secret via `config.GenerateSearxngSecret`, then calls
  `orchestrate.WriteSearxngSettings(RenderSearxngSettings(cfg))` and
  `orchestrate.WriteSearxngSecretEnv(RenderSearxngSecretEnv(cfg.SearxngSecret))` BEFORE
  starting the service, gating the start on `SearXNGContainerUnitName()` being present in
  the rendered plan. The live writers resolve the dir themselves; the `*To` seams exist for
  tests only.

## TDD Gate Compliance

- RED gate: `test(29-02)` commit `9a6f53f` — failing tests committed before implementation.
- GREEN gate: `feat(29-02)` commit `6daa455` — minimal implementation making them pass.
- REFACTOR: not needed (implementation clean on first pass; shared helper + reused guard).

## Known Stubs

None. No placeholder values, hardcoded empties, or unwired data paths.

## Self-Check: PASSED

- FOUND: internal/orchestrate/searxng_settings_write.go
- FOUND: internal/orchestrate/searxng_settings_write_test.go
- FOUND commit 9a6f53f (RED test)
- FOUND commit 6daa455 (GREEN feat)
