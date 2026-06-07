# Phase 16: Backup / Restore - Discussion Log

> **Audit trail only.** Do not use as input to planning, research, or execution agents.
> Decisions are captured in CONTEXT.md — this log preserves the alternatives considered.

**Date:** 2026-06-07
**Phase:** 16-backup-restore
**Mode:** `--auto` (all gray areas auto-selected; each resolved to the recommended option without interactive prompts)
**Areas discussed:** Archive format & layout, Volume I/O seam placement, Live-SQLite quiesce, Restore transactional rollback, Skew handling policy, Manifest schema & checksums, Model exclusion & re-pull identity

---

## Archive format & layout

| Option | Description | Selected |
|--------|-------------|----------|
| Single plain POSIX `.tar` (manifest + config + volume tar + usage.json + bench reports) | Deterministic layout; small archive (weights excluded); simplest checksum reasoning | ✓ |
| gzip/zstd-compressed archive | Smaller on disk, adds compression dependency/variability | |
| Directory tree instead of single file | Easier to inspect, but not "a single archive" per BAK-01 | |

**Auto-selected:** Single plain `.tar`, default `villa-backup-<timestamp>.tar` in CWD, `-o/--output` override, `0600`, traversal-guarded write.
**Notes:** gzip deferred (defaults OFF). BAK-01 requires "a single archive" → tar wins.

---

## Volume I/O seam placement

| Option | Description | Selected |
|--------|-------------|----------|
| cmd-tier fixed-arg `podman` injectable var (mirror `podmanVolumeRm`) | Proven to pass `TestSeamGrepGate`; no new impure module | ✓ |
| New `orchestrate`-resident `volume_io` seam | Keeps podman in orchestrate, but expands the impure surface | |

**Auto-selected:** cmd-tier fixed-arg `podman volume export/import` injectable var.
**Notes:** ROADMAP note explicitly forbids a new impure module; `orchestrate` stays the only intentionally-impure module.

---

## Live-SQLite quiesce

| Option | Description | Selected |
|--------|-------------|----------|
| Stop `villa-openwebui.service` before `podman volume export`, restart after | Clean DB copy; brief chat downtime; no container-internal tooling | ✓ |
| Export the volume live | No downtime, but risks a torn/inconsistent SQLite DB | |
| Run `sqlite3 .backup`/WAL-checkpoint inside the container | Online-consistent, but couples to OWUI internals + adds a tool dependency | |

**Auto-selected:** Quiesce (stop → export → restart).
**Notes:** This is the ROADMAP research flag — validate export-after-stop yields a clean importable DB against a running OWUI.

---

## Restore transactional rollback

| Option | Description | Selected |
|--------|-------------|----------|
| Mirror `backendswap`: capture → quiesce → swap → restart → prove → rollback-on-failure | Proven discipline; honest rollback-complete/incomplete reporting | ✓ |
| Apply-in-place, no capture | Simpler, but a partial restore corrupts the running stack (violates BAK-02) | |

**Auto-selected:** Mirror `backendswap`; capture current state to temp rollback set first; prove via preflight + offload-honest status.
**Notes:** config restored via `config.SaveVilla`; volume recreated via Quadlet; silent CPU fallback at prove = FAIL → rollback.

---

## Skew handling policy (BAK-03)

| Option | Description | Selected |
|--------|-------------|----------|
| WARN-and-confirm before apply (`--yes`/`--force` bypass); BLOCK only on corruption/incompatible manifest | Skew often legitimate (newer villa, older backup); fail closed only on real danger | ✓ |
| Hard-block any version/digest skew | Safe but rejects legitimate cross-version restores | |
| Warn silently and proceed | Loses the explicit consent gate BAK-03 implies | |

**Auto-selected:** WARN-and-confirm; fail-closed BLOCK only on SHA-256 checksum failure or unreadable/incompatible `manifest.schema_version`.

---

## Manifest schema & checksums

| Option | Description | Selected |
|--------|-------------|----------|
| Self-describing `manifest.json` (own schema_version, SHA-256 per entry, image digests from seam, store schema versions, excluded model identities) | Full provenance; enables verify + skew + re-pull | ✓ |
| Minimal manifest (versions only, no checksums) | Lighter, but no corruption detection | |

**Auto-selected:** Comprehensive self-describing manifest; image digests sourced from the inference/orchestrate seam (never re-typed — keeps `TestSeamGrepGate` green).

---

## Model exclusion & re-pull identity

| Option | Description | Selected |
|--------|-------------|----------|
| Exclude weights; record identities; restore reports re-pull (auto-pull deferred) | Small archive; honest about what restore needs | ✓ |
| Include model weights in the archive | Self-contained, but huge and re-pullable (violates BAK-01) | |
| Auto re-pull weights during restore | Convenient, but expands scope/outbound this phase | |

**Auto-selected:** Exclude weights; manifest records catalog id/quant/ctx/source; restore reports re-pull instructions.

---

## Claude's Discretion

- Exact Go type/field names, archive timestamp format, whether `--json` ships this phase, bench-report bundling shape.
- Whether restore's prove step calls `status` directly or composes `preflight` + a residency assert.

## Deferred Ideas

- Automatic model-weight re-pull on restore.
- Scheduling / retention / rotation / incremental / differential backups.
- Encryption-at-rest; remote/off-box targets.
- gzip/zstd compression (defaults OFF in v1).
- Hardened cross-host / post-`podman system reset` restore beyond the research-flag validation.
