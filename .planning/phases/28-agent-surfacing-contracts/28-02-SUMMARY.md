---
phase: 28-agent-surfacing-contracts
plan: 02
subsystem: backup-restore + metrics
tags: [backup, restore, agent, crush, metrics, cache-effectiveness, typed-unknown, SURF-03, USAGE-04]
requires:
  - "internal/backup (BAK-01 ExcludedModel weights-exclusion pattern)"
  - "internal/agent (CrushPolicy version + CrushAsset.BinarySHA256 pin)"
  - "cmd/villa code.go accessors (crushConfigPath / agentBinPath / hashFileSHA256)"
  - "internal/metrics (ScrapeCounters + counterFromMap typed-Unknown guard)"
provides:
  - "backup.ExcludedAgent identity record (sha256+version+pin) + EntryCrushConfig archive entry"
  - "backupSchemaVersion 3 (append-only) + backup.Deps.WriteCrushConfig restore seam"
  - "backup.Result.ExcludedAgent + CrushConfigRestored re-stage report"
  - "metrics.CacheSample + ScrapeCacheCounters (cache_n/prompt_n typed-Unknown counter primitive)"
affects:
  - "Plan 28-03 surfacing (consumes metrics.CacheSample to compute the cache-hit ratio)"
tech-stack:
  added: []
  patterns:
    - "Identity-record + bytes-excluded (ExcludedModel weights clone) for the agent binary"
    - "Optional skip-when-absent archive entry (FileMissing) for crush.json"
    - "Out-of-store-root restore via a dedicated WriteCrushConfig seam (not store-guarded WriteFileAtomic)"
    - "Typed-Unknown counter read via counterFromMap (no fabricated 0)"
    - "Bounded-scrape reuse (no second HTTP request / endpoint literal)"
key-files:
  created: []
  modified:
    - internal/backup/manifest.go
    - internal/backup/manifest_test.go
    - internal/backup/backup.go
    - internal/backup/backup_test.go
    - internal/backup/restore.go
    - internal/backup/restore_test.go
    - internal/backup/deps.go
    - cmd/villa/backup.go
    - cmd/villa/restore.go
    - internal/metrics/llamacpp.go
    - internal/metrics/llamacpp_test.go
decisions:
  - "backup owns its own schema; bumped backupSchemaVersion 2→3 append-only (NOT golden-frozen, status/doctor-independent)"
  - "crush.json restore routes through a NEW out-of-store-root WriteCrushConfig seam because usage.WriteFileAtomic's store-root guard rejects ~/.config/crush/"
  - "agent binary identity is recorded even when the on-disk binary is absent (empty sha) — the pinned policy version/pin still anchor the re-stage"
  - "cache_n/prompt_n metric NAME constants encode the cache-reuse pair; absence degrades to typed-Unknown via counterFromMap (no fabricated count)"
metrics:
  duration: "~30 min"
  completed: "2026-06-15"
---

# Phase 28 Plan 02: Agent Backup Coverage + Cache-Effectiveness Counter Summary

Covered the coding agent in backup/restore (SURF-03/D-08) by mirroring the BAK-01 model-weights pattern exactly — the rendered crush.json goes INTO the archive, the agent binary is identity-recorded in the manifest and EXCLUDED from the bytes, and restore re-stages it with fail-closed identity verify — and added the `cache_n`/`prompt_n` typed-Unknown counter primitive to the existing bounded `/metrics` scrape (USAGE-04 core half, D-10) for the Plan-03 surfacing layer to consume.

## What was built

### Task 1 — Agent backup coverage (commit `5bb8b72`)
- **`internal/backup/manifest.go`:** `backupSchemaVersion` 2→3 (append-only, version-history comment added); new `EntryCrushConfig = "crush.json"`; new `ExcludedAgent` identity-only type (`SHA256`/`Version`/`PinSHA256`) cloned from `ExcludedModel`; `Manifest.ExcludedAgent *ExcludedAgent` tail-appended ABOVE `SchemaVersion` (omitempty); threaded through `ManifestInput` + `BuildManifest`.
- **`internal/backup/backup.go`:** `BackupInput.CrushConfigPath` + the three agent-identity fields; crush.json added as an optional `EntryChecksum` member (gated agent-on, skipped-when-absent via `FileMissing`, mirroring the qdrant/recall optional entries); `ExcludedAgent` recorded agent-on with the binary BYTES never archived (no `EntryCrushBinary`).
- **`internal/backup/restore.go` + `deps.go`:** crush.json extracted + SHA-256-verified through the SAME `readAndVerify` pass; restored via a NEW out-of-store-root `Deps.WriteCrushConfig` seam (forward + verbatim rollback rows); `Result.ExcludedAgent` + `Result.CrushConfigRestored` surface the re-stage report; identity drift fails closed (verify mismatch → Refused, zero side effects).
- **`cmd/villa/backup.go` / `restore.go`:** new agent fields gated on `cfg.AgentEnabled`, sourced from `crushConfigPath()` + `agentBinPath()`+`hashFileSHA256` + the pinned `agent.LoadCrushPolicy()` version/pin; `WriteCrushConfig` live wiring mirrors code.go's WriteConfig (traversal-guard + MkdirAll 0700 + WriteFile 0600); honest agent reporting on both verbs.

### Task 2 — Cache-effectiveness counter primitive (commit `a5b21ec`)
- **`internal/metrics/llamacpp.go`:** `mCacheTokensTotal`/`mPromptCacheTokensTotal` metric NAME constants (confined to the package per the single-home grep discipline); `CacheSample` (`CacheN`/`CacheKnown` + `PromptN`/`PromptKnown`) cloned from `CounterSample`; `ScrapeCacheCounters` reuses the IDENTICAL bounded request shape (`scrapeTimeout` client + `maxScrapeBody+1` truncation-refusal + `parsePromText`) — no second HTTP request, no new endpoint literal. The ratio is NOT computed here (Plan 03 owns it).

## Deviations from Plan

None — plan executed exactly as written. Two implementation choices worth recording (both within the plan's `<action>` latitude):
- The crush.json restore required a dedicated `WriteCrushConfig` Deps seam (not the existing store-root-guarded `WriteFileAtomic`) because `~/.config/crush/` lives OUTSIDE `$XDG_DATA_HOME/villa` and the store guard rejects it. This mirrors the documented `WriteTempFile` precedent (also deliberately not store-guarded).
- The cache_n/prompt_n metric NAME constants encode the cache-reuse pair as Prometheus line names; a llama.cpp build that does not emit them degrades to typed-Unknown via `counterFromMap` (Known=false) — exactly the honesty-by-construction contract, so an absent pair is never a fabricated 0.

## Verification

- `go test ./internal/backup/... -count=1` — 83 passed.
- `go test ./internal/metrics/... -run 'TestScrapeCache|TestCounter|TestCacheSample' -count=1` — 17 passed.
- `go test ./cmd/villa/... -run 'TestBackup|TestRestore' -count=1` — 14 passed.
- `go test ./internal/inference/... -run TestSeamGrepGate` — passed (no leaked backend markers).
- `make check` (vet + full `go test ./...`) — all packages green.
- Agent-off backup archive layout confirmed identical to today (no crush.json entry, ExcludedAgent nil) by `TestBackupAgentOffIsLayoutIdentical` / `TestRestoreAgentOffNoCrushNoExcludedAgent`.
- No `status.Report` schema bump in this plan (isolated to Plan 03). `backupSchemaVersion` 2→3 is doctor/status-independent and NOT golden-frozen.

## Threat-model coverage

- **T-28-02-01 (Tampering / drifted agent binary):** restore verifies the crush.json entry's SHA-256 via the existing fail-closed gate (`TestRestoreAgentIdentityDriftFailsClosed`); the EXCLUDED agent identity is surfaced for re-stage with the fail-closed verify contract.
- **T-28-02-02 (Information disclosure):** `ExcludedAgent` is identity-only — `TestExcludedAgentHasNoContentFields` enforces the field allow-set + JSON-key denylist; binary bytes excluded.
- **T-28-02-03 (Fabricated counts):** `counterFromMap` rejects absent/NaN/Inf/negative/over-2^53 → Known=false (`TestCacheSampleRejectsNonFinite`); truncated body refuses the whole sample (`TestScrapeCacheCountersOversizedBodyUnavailable`).
- **T-28-02-04 (DoS):** bounded `scrapeTimeout` client + `maxScrapeBody+1` LimitReader reused, no second request.

## Known Stubs

None — both halves are wired end to end. The `CacheSample` ratio is deliberately NOT computed here (Plan 28-03 surfacing owns the pct, shown only when both Known and prompt_n>0); this plan only adds the counter primitive, as specified.

## Self-Check: PASSED

- All modified files present on disk; both per-task commits (`5bb8b72`, `a5b21ec`) exist in git history.
- All four plan-frontmatter `contains` assertions verified: `ExcludedAgent` in manifest.go, `CrushConfigPath` in backup.go, `cache_n` in llamacpp.go, `counterFromMap(m, mCache` in llamacpp.go.
