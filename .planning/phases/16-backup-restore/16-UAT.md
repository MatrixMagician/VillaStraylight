---
status: passed
phase: 16-backup-restore
source: [16-VERIFICATION.md]
started: 2026-06-07T00:00:00Z
updated: 2026-06-07T23:25:00Z
executed_on: gfx1151 Fedora host (rootless Podman 5.8.2), villa v1.1-138-g8eb2526
---

## Current Test

(all complete)

## Tests

### 1. Same-host backup→restore round-trip
expected: `villa backup` → mutate → `villa restore` → chats restored, `villa status` green + residency-proven
result: PASS — `villa backup` produced a 265 MB single .tar; planted a stray file; `villa restore --yes` restored; `webui.db` came back byte-identical (sha `ff939dec…` == pre-mutation original); `villa status` overall PASS, villa-llama OFFLOAD PASS (residency-proven), OWUI HTTP 200. A same-version restore (no skew) proceeded with NO prompt and cutover proven.

### 2. Clean-volume-before-import (no stale data leak)
expected: Restore over a volume containing a stray file; the stray file is GONE post-restore (clean-recreate must beat merge)
result: PASS — planted `UAT_STRAY_MARKER.txt` in the live volume; after restore the marker was GONE (clean-recreate VolumeRm → Quadlet recreate → `podman volume create` → import won; no merge survivor).

### 3. OWUI live-SQLite quiesce yields importable DB
expected: Stop villa-openwebui.service, export, restart; webui.db opens clean after restore (no WAL corruption)
result: PASS — backup quiesced villa-openwebui before `podman volume export` and auto-restarted it; post-restore the cutover prove passed and OWUI served HTTP 200 on a valid `webui.db` (an unimportable/corrupt DB would have failed the prove → rollback).

### 4. Cross-host / post-`podman system reset` restore (KNOWN best-effort LIMITATION)
expected: Restore on a reset/foreign host; if perms fail, documented `podman unshare chown -R` remediation applies; outcome documented honestly
result: DOCUMENTED LIMITATION (not fully exercised) — a true `podman system reset`/foreign-host test was deliberately NOT run on this host (it would destroy all of the user's podman volumes/images, including the model weights). Mechanism validated indirectly: restore recreates the volume via Quadlet (`ReconcileAndWrite`), which re-applies the `:Z` SELinux relabel + ownership; and the host-fingerprint skew path surfaces the remediation `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` (backup.go:308). Honest best-effort limitation as designed.

### 5. Cross-version skew WARN-and-confirm gate + fail-closed corruption BLOCK
expected: Version/digest skew → named WARN + remediation + y/N confirm (`--yes`/`--force` bypass); corrupt/incompatible-manifest archive → fail-closed BLOCK with zero side effects
result: PASS — restoring a backup made by villa v1.1-137 onto v1.1-138 printed the named version-skew WARN + remediation, prompted `[y/N]`, and safely DECLINED (zero side effects) without confirmation; `--yes` bypassed it. A tampered entry → `checksum mismatch` BLOCK (exit 1); a `schema_version 99` manifest → `unreadable or newer than this villa supports` BLOCK (exit 1). Both BLOCKs left a planted marker intact (volume NOT recreated), services up, no /tmp residue → zero side effects confirmed.

## Summary

total: 5
passed: 4
issues: 0
pending: 0
skipped: 0
blocked: 0
documented_limitation: 1

## Notes

- On-hardware UAT surfaced and fixed a real regression: the WR-05 store-root guard on `usage.WriteFileAtomic` rejected restore's legitimate `/tmp` volume-tar staging write, failing every restore at the volume stage (the transactional rollback fired correctly — stack stayed intact). Fixed by splitting the seam: store writes stay guarded; a new unguarded `WriteTempFile` stages the /tmp tar. Commit `8eb2526`, with a regression test (`TestRestoreTempVolumeStagingFailureRollsBack`).
- WR-01 (temp-dir cleanup) confirmed on hardware: no `/tmp/villa-restore-*` dirs left after any run (success, rollback, or BLOCK).

## Gaps
