---
phase: 16-backup-restore
plan: 01
subsystem: backup
tags: [backup, restore, manifest, checksum, tar, skew, seam, version-stamp]
dependency_graph:
  requires: []
  provides:
    - "internal/backup.Manifest / BuildManifest / ManifestInput (schema-versioned doc, schema_version LAST)"
    - "internal/backup.EntryChecksum / ExcludedModel / HostFingerprint"
    - "internal/backup sum/verify SHA-256 (ErrChecksumMismatch)"
    - "internal/backup writeArchive/readArchive + tar-slip guard (assertInsideDir)"
    - "internal/backup.Deps (func-field seam), Result, ProveVerdict, ProveStatusPass"
    - "internal/backup.CompareSkew / SkewVerdict / SkewWarning / CurrentInstall"
    - "orchestrate.OpenWebUIImage() exported accessor"
    - "cmd/villa villaVersion() + build-stamped main.version"
  affects:
    - "cmd/villa (later plans wire liveBackupDeps/liveRestoreDeps against backup.Deps)"
    - "Makefile build target (now -ldflags version stamp)"
tech_stack:
  added: []          # stdlib only — archive/tar, crypto/sha256, encoding/json
  patterns:
    - "pure-core + injectable-seam (mirrors backendswap/usage/status)"
    - "schema-versioned JSON doc, append-only, schema_version LAST"
    - "image digest sourced from seam accessor, never re-typed (D-10)"
    - "tar-slip traversal guard via filepath.Rel (config.assertInsideDir shape)"
    - "build-time version stamp via -ldflags -X main.version"
key_files:
  created:
    - internal/backup/manifest.go
    - internal/backup/checksum.go
    - internal/backup/tarutil.go
    - internal/backup/deps.go
    - internal/backup/backup.go
    - internal/backup/manifest_test.go
    - internal/backup/checksum_test.go
    - internal/backup/tarutil_test.go
    - internal/backup/backup_test.go
    - internal/orchestrate/openwebui_test.go
    - cmd/villa/version.go
  modified:
    - internal/orchestrate/openwebui.go
    - cmd/villa/root.go
    - Makefile
decisions:
  - "Manifest carries store schema versions as plain ints (UsageSchemaVersion/BenchSchemaVersion); real values supplied by cmd-tier via Plan-02 accessors — this pure core only defines the fields (per plan)."
  - "Absolute-path tar entries rejected explicitly in assertEntryInside before filepath.Join (Join strips a leading separator, which would otherwise silently downgrade an absolute entry to relative)."
  - "VERSION derived from `git describe --tags --always --dirty` with a `dev` fallback; CGO unchanged (no cgo added)."
  - "OpenWebUIImage() routes manifest digest reads through the orchestrate seam even though ghcr.io is outside TestSeamGrepGate's regex — uniform no-re-typed-literal discipline."
metrics:
  duration: ~6m
  completed: 2026-06-07
  tasks: 2
  files_created: 11
  files_modified: 3
---

# Phase 16 Plan 01: Pure backup core + OWUI accessor + version stamp Summary

Pure, host-I/O-free `internal/backup` core (manifest, SHA-256 compute/verify, single-POSIX-tar assembly with tar-slip guard, injectable Deps/Result, and the WARN-vs-fail-closed-BLOCK skew comparison) plus the two additive seam gaps — `orchestrate.OpenWebUIImage()` and a build-stamped `villa` version — that the `villa backup`/`villa restore` commands compose in later waves.

## What was built

**Task 1 — pure core types (commit `2df984e`):**
- `manifest.go` — `Manifest` (schema-versioned, `schema_version` LAST, append-only), `BuildManifest`, `ManifestInput`, `EntryChecksum`, `ExcludedModel` (identity-only), `HostFingerprint`, the deterministic archive-entry name consts (single `bench-reports.jsonl`), `backupSchemaVersion = 1`.
- `checksum.go` — `sum(io.Reader)` (lowercase-hex SHA-256), `verify(r, want)` wrapping `ErrChecksumMismatch`.
- `tarutil.go` — `writeArchive`/`readArchive` over injected `io.Writer`/`io.Reader` (manifest.json FIRST, `tar.FormatPAX`, Mode 0o600), per-entry tar-slip guard (`assertEntryInside`/`assertInsideDir`, `filepath.Rel`-based), 0o600/0o700 mode consts.
- `deps.go` — injectable `Deps` (func-field seams: config, volume export/import/rm/ensure, file r/w, stop/start/restart, reconcile, daemon-reload, prove + service-name fields), `Result`, `ProveVerdict`, `const ProveStatusPass`.

**Task 2 — skew + seam gaps (commit `120de25`):**
- `backup.go` — `CompareSkew(Manifest, CurrentInstall) SkewVerdict`: WARN-and-confirm on villa-version / inference-digest / OWUI-digest / host-fingerprint mismatch and older store schemas (each WARN carries named remediation); fail-closed BLOCK on checksum failure, unreadable/newer `manifest.schema_version`, or any newer store schema. `SkewVerdict`/`SkewWarning`/`CurrentInstall`.
- `internal/orchestrate/openwebui.go` — exported `func OpenWebUIImage() string` (literal stays behind the orchestrate seam; D-10).
- `cmd/villa/version.go` — `var version = "dev"` + `villaVersion()`; wired into the cobra root `Version`.
- `Makefile` — `build` now passes `-ldflags "-X main.version=$(VERSION)"`, `VERSION` from `git describe` with a `dev` fallback; CGO behavior unchanged.

## Verification

- `go test ./internal/backup/ -count=1` — green (manifest round-trip + schema_version-LAST + no-content ExcludedModel; checksum determinism/mismatch; tar round-trip + tar-slip refusal for `../` and absolute; full skew classification table + matching-no-findings).
- `go test ./internal/orchestrate/ -count=1` — green (OpenWebUIImage accessor equals the const, digest-pinned).
- `go test ./internal/inference/ -run TestSeamGrepGate -count=1` — green (no image literal leaked into internal/backup).
- `make build` → `./villa`; `./villa --version` reports the stamped `git describe` version (defaults to `dev` without a stamp).
- `make check` (go vet + `go test ./...`) — green across the whole module.
- `grep -rn 'os/exec' internal/backup/` — empty (pure core; no exec/podman/inference/detect imports).

## Deviations from Plan

### Auto-fixed Issues

**1. [Rule 1 - Bug] Absolute-path tar entry silently downgraded to relative**
- **Found during:** Task 1 (TestTarSlipRefusesAbsolute failed on first run).
- **Issue:** `filepath.Join(".", "/etc/passwd")` cleans the leading separator to `etc/passwd`, so the absolute-path entry passed the `assertInsideDir` check — the tar-slip guard would have accepted an absolute-path entry.
- **Fix:** Added an explicit `filepath.IsAbs(name)` rejection in `assertEntryInside` before the join.
- **Files modified:** internal/backup/tarutil.go
- **Commit:** 2df984e

**2. [Rule 3 - Blocking] `os/exec` token in doc comments tripped the acceptance grep**
- **Found during:** Task 1 acceptance (`grep -rn 'os/exec' internal/backup/` matched prose in two doc comments, though there is no import).
- **Issue:** The acceptance criterion is a literal grep; doc-comment prose containing the token `os/exec` would fail it despite the package importing no exec.
- **Fix:** Rephrased both doc comments ("the exec package", "runs NO subprocess") so the grep is unambiguously clean; the package genuinely imports no exec.
- **Files modified:** internal/backup/deps.go
- **Commit:** 2df984e

## Threat Flags

None — no new security surface beyond the threat_model's registered items (tar-slip guard, fail-closed schema/checksum BLOCK, identity-only excluded-model record, seam-sourced digests) were all implemented as planned.

## Self-Check: PASSED

- internal/backup/{manifest,checksum,tarutil,deps,backup}.go — FOUND
- internal/backup/{manifest,checksum,tarutil,backup}_test.go — FOUND
- internal/orchestrate/openwebui_test.go, cmd/villa/version.go — FOUND
- orchestrate.OpenWebUIImage() / cmd/villa villaVersion() — present (grep-confirmed)
- Commit 2df984e — FOUND; Commit 120de25 — FOUND
