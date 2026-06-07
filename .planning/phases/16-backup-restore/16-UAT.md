---
status: testing
phase: 16-backup-restore
source: [16-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T00:00:00Z
---

## Current Test

number: 1
name: Same-host backup→restore round-trip
expected: |
  `villa backup -o /tmp/b.tar` → mutate a chat in Open WebUI → `villa restore /tmp/b.tar`
  → chats restored, `villa status` green and residency-proven, stack healthy.
awaiting: user response

## Tests

### 1. Same-host backup→restore round-trip
expected: `villa backup -o /tmp/b.tar` → mutate a chat → `villa restore /tmp/b.tar` → chats restored, `villa status` green + residency-proven
result: [pending]

### 2. Clean-volume-before-import (no stale data leak)
expected: Restore over a volume containing a stray file; the stray file is GONE post-restore (import MERGES, so the clean-recreate path — VolumeRm → Quadlet recreate → EnsureVolume `podman volume create` → VolumeImport — must win)
result: [pending]

### 3. OWUI live-SQLite quiesce yields importable DB
expected: Stop villa-openwebui.service, export, restart; `webui.db` opens clean after restore (no WAL corruption)
result: [pending]

### 4. Cross-host / post-`podman system reset` restore (KNOWN best-effort LIMITATION)
expected: Restore on a reset/foreign host; if perms fail, the documented `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` remediation applies; outcome documented honestly (this is a documented limitation, not a committed guarantee)
result: [pending]

### 5. Cross-version skew WARN-and-confirm gate
expected: Restore an archive produced by a different villa/image version against a live install → named WARN + remediation prints and the apply waits for y/N confirmation (`--yes`/`--force` bypasses); a SHA-256/incompatible-manifest archive is fail-closed BLOCKED with zero side effects
result: [pending]

## Summary

total: 5
passed: 0
issues: 0
pending: 5
skipped: 0
blocked: 0

## Gaps
