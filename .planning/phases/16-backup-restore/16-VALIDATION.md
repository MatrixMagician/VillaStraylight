---
phase: 16
slug: backup-restore
status: draft
nyquist_compliant: false
wave_0_complete: false
created: 2026-06-07
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
| 16-01-01 | 01 | 1 | BAK-01 | T-16-01 | Archive excludes model weights; manifest records SHA-256 + digests; no image literal leaks (seam gate green) | unit | `go test ./internal/backup/...` | ❌ W0 | ⬜ pending |
| 16-02-01 | 02 | 2 | BAK-02 | T-16-02 | Failed/partial restore rolls back verbatim; running stack intact; rollback-complete/incomplete reported honestly | unit | `go test ./internal/backup/... ./cmd/villa/...` | ❌ W0 | ⬜ pending |
| 16-03-01 | 03 | 2 | BAK-03 | T-16-03 | Version/digest/schema skew → WARN+confirm before apply; checksum/incompatible-manifest → fail-closed BLOCK | unit | `go test ./internal/backup/...` | ❌ W0 | ⬜ pending |

---

## Wave 0 Requirements

- [ ] `internal/backup/backup_test.go` — pure-core unit stubs for BAK-01 (manifest build/verify, archive entry plan, SHA-256), BAK-03 (skew comparison: WARN vs BLOCK boundary)
- [ ] `internal/backup/restore_test.go` — transactional frame stubs for BAK-02 (capture→quiesce→swap→restart→prove→rollback) driven by `fakeDeps`
- [ ] `cmd/villa/backup_test.go` / `cmd/villa/restore_test.go` — cobra command wiring + `fake*Deps`; tar-slip extraction guard; seam-gate assertion (no image literal outside inference/orchestrate)
- [ ] Existing `go test` infrastructure covers the rest — no new framework needed

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

- [ ] All tasks have `<automated>` verify or Wave 0 dependencies
- [ ] Sampling continuity: no 3 consecutive tasks without automated verify
- [ ] Wave 0 covers all MISSING references
- [ ] No watch-mode flags
- [ ] Feedback latency < 60s
- [ ] `nyquist_compliant: true` set in frontmatter

**Approval:** pending
