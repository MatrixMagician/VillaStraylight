# Phase 16: Backup / Restore - Context

**Gathered:** 2026-06-07
**Status:** Ready for planning
**Mode:** `--auto` (decisions auto-resolved to the recommended option for each gray area; review before planning)

<domain>
## Phase Boundary

`villa` can **back up a workspace to a single self-describing local archive** and
**restore from it transactionally**. The archive captures the recreatable *state*
(config + Open WebUI data + the local data-dir artifacts added by phases 14–15)
plus a manifest of versions / image digests / checksums, **excludes** the
re-pullable model weights, and restore applies with `backendswap`-grade
transactional discipline so a failed or partial restore never corrupts a running
stack.

**In scope:**
- `villa backup` → a single archive containing:
  - `config.toml` (XDG config),
  - the **Open WebUI data volume** (`villa-openwebui.volume`) via
    `podman volume export` (NEVER host-path tar),
  - the **XDG data-dir artifacts** added by phases 14–15: the usage store
    (`$XDG_DATA_HOME/villa/usage.json`) and the saved **bench reports**
    (BENCH-03, under `$XDG_DATA_HOME/villa/`),
  - a **`manifest.json`** (own `schema_version`) of villa version, image
    digests, store schema versions, per-entry SHA-256 checksums, and the
    **identities of the excluded model weights** (for re-pull).
- `villa restore` → transactional apply: capture → quiesce → swap → restart →
  prove → rollback-on-failure. A failed/partial restore leaves the running stack
  intact (BAK-02).
- **Skew warning** (BAK-03): compare manifest vs current install
  (villa version / image digests / store schema versions) and warn with
  remediation **before** applying.
- A new **pure `internal/backup` core** (manifest build, checksum compute/verify,
  skew comparison, archive entry planning); all host I/O (podman volume
  export/import, file r/w, service stop/start) injected via a `Deps` seam.

**Out of scope (own phases / future / explicitly excluded):**
- **Backing up model weights.** The `villa-models.volume` is excluded — weights
  are re-pullable; only their *identity* is recorded in the manifest (BAK-01).
- **Auto re-pulling weights on restore.** Restore reports the model identities to
  re-pull (and may point at `villa model pull`); automatic re-pull is deferred.
- **Remote / cloud / off-box backup targets.** Strictly-local archive only — no
  new outbound (zero-telemetry invariant).
- **Cross-host / post-`podman system reset` UID-map + SELinux `:Z` repair** is a
  **research flag to VALIDATE**, not a guaranteed deliverable — the same-host
  round-trip is the committed bar; cross-host robustness is investigated and
  documented honestly (see canonical refs).
- Scheduling / retention / rotation / incremental or differential backups,
  encryption-at-rest — future phases.
- A new `status.Report` schema bump (Phase 15 owned the last one) — backup/restore
  output is a **separate** new contract, not an evolution of `status.Report`.
</domain>

<decisions>
## Implementation Decisions

### Module boundary & seam placement
- **D-01:** New **pure `internal/backup` core**. It builds the manifest, computes
  and verifies SHA-256 checksums, plans the archive entry layout, and performs the
  skew comparison — **no host I/O**. Persistence, `podman volume export/import`,
  service stop/start, and filesystem touch are **injected `func`-field seams**
  (`Deps`), wired by `live*Deps` closures in `cmd/villa/backup.go` /
  `cmd/villa/restore.go`. Mirrors the established pure-core + injectable-seam
  pattern (`backendswap`/`bench`/`status`).
- **D-02:** **Volume I/O goes through a cmd-tier fixed-arg `podman` injectable
  var**, mirroring `cmd/villa/uninstall.go`'s `podmanVolumeRm` (already proven to
  pass `TestSeamGrepGate`). Commands: `podman volume export <name> --output <f>`
  and `podman volume import <name> <f>`. **Do NOT add a new impure module** —
  `internal/orchestrate` stays the only intentionally-impure first-party module,
  untouched except for volume *recreate* via Quadlet (D-07). Fixed-arg exec only;
  no shell, no interpolation (volume/model names are config/catalog-resolved).

### Archive format & layout
- **D-03:** **Single plain POSIX `.tar`** archive (no gzip in v1 — deterministic
  layout, simpler checksum/manifest reasoning; model weights are excluded so the
  archive is small). Entries: `manifest.json`, `config.toml`,
  `openwebui-volume.tar` (the `podman volume export` output), `usage.json`, and
  the bench-report file(s).
- **D-04:** **Default output `villa-backup-<timestamp>.tar` in the current working
  directory**, with `-o/--output <path>` to override. Written `0600`, parent dir
  honored as-is; the output path is **traversal-guarded** on write.
  > Discretion: exact timestamp format (filesystem-safe, no `:`); whether to also
  > accept a positional path. Gzip may be revisited but defaults OFF.

### Quiesce (live-SQLite safety — research flag)
- **D-05:** **Stop `villa-openwebui.service` before `podman volume export`, restart
  after.** Open WebUI keeps a live SQLite DB on its volume; exporting it live risks
  a torn/inconsistent copy. Volume-level quiesce-then-export is the
  integration-honest fix. **Rejected:** reaching into the container to run
  `sqlite3 .backup`/WAL-checkpoint (adds a tool dependency and couples us to OWUI
  internals). Accept a brief chat downtime during backup; document it.
  > Research flag (from ROADMAP): confirm the live-SQLite quiesce approach against
  > a running Open WebUI — verify export-after-stop yields a clean importable DB.

### Restore transactional discipline (mirror `backendswap`)
- **D-06:** **Capture → quiesce → swap → restart → prove → rollback-on-failure.**
  Before mutating: **capture** the current state to a temp rollback set under the
  XDG dir (export current `villa-openwebui.volume` + copy current `config.toml` +
  current `usage.json`/bench reports). Then quiesce services, apply the archive,
  restart, and **prove**. On ANY mutate error or non-pass prove: **verbatim
  restore** from the captured set and report honest rollback-complete /
  rollback-incomplete (mirroring `backendswap.Run`). Never leave a half-applied
  stack silently.
- **D-07:** **Apply path:** restore `config.toml` via `config.SaveVilla` (0600/0700,
  atomic, traversal-guarded — do NOT hand-write); restore the data-dir artifacts
  with the same atomic write discipline; **recreate the Open WebUI volume via
  Quadlet** (regenerate units from the restored config — config stays the single
  source of truth) then `podman volume import` the data into it; re-run
  **preflight** and assert **`status`** health (offload/residency-aware) as the
  prove step. A silent/partial CPU fallback at prove = FAIL → rollback.

### Skew handling (BAK-03)
- **D-08:** **WARN-and-confirm before applying; BLOCK only on corruption /
  incompatible manifest.** On version/digest/store-schema skew between manifest and
  current install, print a **named warning with remediation** and require explicit
  confirmation (interactive `y/N`, bypass with `--yes`/`--force` for
  non-interactive). Skew is often legitimate (newer villa restoring an older
  backup) — do NOT hard-block it. **Fail closed (BLOCK, no apply)** only on: a
  failed **SHA-256 checksum** (archive corruption) or an **unreadable /
  incompatible `manifest.schema_version`**.

### Manifest (self-describing)
- **D-09:** `manifest.json` carries its **own `schema_version`** and records:
  created-at timestamp, villa binary version, host fingerprint (arch / iGPU /
  kernel — reuse `internal/detect`), **image digests** (the resolved inference
  backend image + the Open WebUI image), the `config`/`usage`/bench-store
  `schema_version`s, **per-entry SHA-256 checksums** (config, openwebui volume
  tar, usage.json, bench reports), and the **excluded model identities**
  (catalog id / quant / ctx / source) for re-pull.
- **D-10:** **Image digests are sourced from the seam, never re-typed.** The
  manifest obtains the inference image digest from `internal/inference` (the
  `Backend`) and the Open WebUI image from `internal/orchestrate` — backup MUST NOT
  hardcode any image literal, so `TestSeamGrepGate` stays green.

### Privacy / security (standing invariants)
- **D-11:** **Tar-slip guard on restore extraction.** Every entry path read from an
  archive is validated to stay inside the intended extraction dir before write
  (reuse the `config.assertInsideDir` traversal-guard discipline). Archive output
  path is likewise traversal-guarded on write. All files `0600`, dirs `0700`.
- **D-12:** **No new outbound.** Backup/restore touch only the local box and the
  user's podman volumes; the only network action is the *existing* image/model pull
  on re-pull. The `status` `no_telemetry` posture (`outbound = image/model pulls
  only`) is preserved.

### CLI output contract
- **D-13:** `villa backup` / `villa restore` may expose `--json`. If so, it is a
  **new, separate frozen contract** (its own `schema_version` + new golden under
  `cmd/villa/testdata/`), NOT an evolution of `status.Report`. Keep exactly one
  *existing* byte-frozen contract from evolving per phase; a brand-new contract is
  fine. Restore is offload-/prove-honest in its output (never false-green).

### Claude's Discretion
- Exact Go type/field names (`Manifest`, `Deps` shape, `Fold`-style signatures),
  the precise archive timestamp format, whether `--json` ships this phase, and
  whether bench reports are tarred individually or as a sub-bundle — planner /
  executor decide within the constraints above.
- Whether restore's prove step calls the `status` core directly or composes
  `preflight` + a residency assert — pick the lightest path that is offload-honest.
</decisions>

<canonical_refs>
## Canonical References

**Downstream agents MUST read these before planning or implementing.**

### Phase scope & requirements
- `.planning/ROADMAP.md` § "Phase 16: Backup / Restore" — goal, success criteria
  (SC1–SC3), the **research flag** (cross-host / post-`podman system reset`
  UID-map + SELinux `:Z` repair via `podman unshare chown -R`; live-SQLite quiesce),
  and the binding **implementation note** (`podman volume export/import` NEVER
  host-path tar; cmd-tier fixed-arg podman or orchestrate `volume_io` — do NOT add
  a new impure module; pure `internal/backup` does manifest/verify; restore config
  via `config.SaveVilla` + re-run preflight; recreate volume via Quadlet; mirror
  `backendswap`; 0600/0700 XDG).
- `.planning/REQUIREMENTS.md` § "Backup / Restore" (BAK-01, BAK-02, BAK-03) + the
  standing strictly-local / zero-telemetry Out-of-Scope invariant.

### Code seams this phase extends / mirrors (read before planning)
- `cmd/villa/uninstall.go` — the **`podmanVolumeRm` precedent** (injectable
  fixed-arg `podman` var that passes `TestSeamGrepGate`), `volumeRmArgs` pure
  arg-builder, and its traversal-guarded volume handling. Model the
  export/import vars on this.
- `internal/backendswap/backendswap.go` — the **transactional capture → prove →
  cutover → rollback** discipline to mirror, including honest
  rollback-complete/incomplete reporting.
- `internal/config/villaconfig.go` — `SaveVilla`/`SaveVillaTo` (0600/0700, atomic
  temp+rename, `assertInsideDir` traversal guard at `villaconfig.go:221`) — reuse
  for config restore AND as the template for the tar-slip extraction guard.
- `internal/orchestrate/` — `WriteUnits` (`reconcile.go:52`), the Open WebUI
  volume/container units (`openwebui.go`), and `render.go` — for **recreating the
  volume via Quadlet from restored config** (config is the single source of truth).
- `internal/preflight/preflight.go` — re-run the host-prep gate after restore as
  part of the prove step.
- `internal/status/status.go` — `Report`, offload/residency-honest health, and the
  `no_telemetry` statement (`status.go:27`) to assert post-restore and preserve.
- `internal/inference/backend_vulkan.go` / `backend_rocm.go` +
  `internal/inference/seam_test.go` — the **only** source of inference image
  digests for the manifest; the `TestSeamGrepGate` constraint (no image literal
  outside the inference/orchestrate seam).
- `internal/usage/usage.go` (usage store, `schema_version`, XDG data-dir resolver
  at `usage.go:205-211`) and the Phase 14 bench-store (saved reports under
  `$XDG_DATA_HOME/villa/`) — the data-dir artifacts to capture and their schema
  versions to record in the manifest.

### Prior-phase precedent
- `.planning/phases/15-cumulative-usage-tracking/15-CONTEXT.md` — the XDG data-dir
  write discipline (atomic, 0600/0700, traversal guard) and the "one byte-frozen
  contract per phase" rule this phase honors (new contract, not a `status.Report`
  bump).
</canonical_refs>

<code_context>
## Existing Code Insights

### Reusable Assets
- `cmd/villa/uninstall.go` `podmanVolumeRm` + `volumeRmArgs` — the seam-gate-proven
  pattern for cmd-tier fixed-arg podman volume ops; clone for export/import.
- `internal/backendswap/backendswap.go` — copy the transactional capture/prove/
  rollback skeleton wholesale (don't re-invent transactional discipline).
- `internal/config/villaconfig.go` `SaveVillaTo` + `assertInsideDir` — atomic XDG
  write and the traversal guard reused both for config restore and tar extraction.
- `internal/usage` XDG data-dir resolver — the same resolver locates `usage.json`
  and the bench-report dir to back up.

### Established Patterns
- Pure-core + injectable-seam: `internal/backup` is pure; podman/fs/systemd are seams.
- Honesty-by-construction: prove is offload-asserting (silent CPU fallback = FAIL);
  rollback reports complete vs incomplete truthfully.
- Backend literals stay behind the inference/orchestrate seam — manifest reads
  digests, never re-types them (`TestSeamGrepGate`).
- Config is the single source of truth: volumes recreated via Quadlet from restored
  config, never hand-rebuilt.

### Integration Points
- `villa backup`: quiesce OWUI → `podman volume export` → assemble tar (manifest +
  config + volume tar + usage.json + bench reports) → restart OWUI.
- `villa restore`: read+verify archive → skew warn → capture current → quiesce →
  `config.SaveVilla` + restore data-dir artifacts + Quadlet recreate +
  `podman volume import` → restart → preflight+status prove → rollback on failure.
</code_context>

<specifics>
## Specific Ideas

- Default backup file `villa-backup-<timestamp>.tar` in CWD; `-o/--output` override.
- Skew is WARN-and-confirm (`--yes`/`--force` to bypass); corruption/incompatible
  manifest is the only fail-closed BLOCK.
- Backup deliberately includes the phase-14/15 data-dir artifacts (usage store +
  saved bench reports), not just config + the OWUI volume — per the ROADMAP note
  "backup must capture the usage store and saved reports added by 14–15".
</specifics>

<deferred>
## Deferred Ideas

- Automatic model-weight **re-pull** on restore (this phase records identities and
  reports them; driving `villa model pull` automatically is a later enhancement).
- Scheduling / retention / rotation / incremental / differential backups.
- Encryption-at-rest and remote/off-box backup targets (would breach strictly-local
  unless explicitly designed; future milestone).
- gzip/zstd compression of the archive (defaults OFF in v1; revisit if size warrants).
- Hardened cross-host / post-`podman system reset` restore (UID-remap + SELinux `:Z`
  repair) beyond what the research flag validates — same-host round-trip is the
  committed bar this phase.

None beyond the above — discussion stayed within phase scope.
</deferred>

---

*Phase: 16-backup-restore*
*Context gathered: 2026-06-07*
