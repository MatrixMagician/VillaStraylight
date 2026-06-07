# Phase 16: Backup / Restore - Research

**Researched:** 2026-06-07
**Domain:** Local workspace backup/restore — rootless Podman volume export/import, Go stdlib `archive/tar`, transactional state-machine, SQLite quiesce
**Confidence:** HIGH (podman mechanics empirically verified against installed podman 5.8.2; all code seams read in-tree)

<user_constraints>
## User Constraints (from CONTEXT.md)

### Locked Decisions

- **D-01:** New **pure `internal/backup` core** — builds manifest, computes/verifies SHA-256 checksums, plans archive entry layout, performs skew comparison; **no host I/O**. Persistence, `podman volume export/import`, service stop/start, filesystem touch are injected `func`-field `Deps`, wired by `live*Deps` in `cmd/villa/backup.go` / `cmd/villa/restore.go`. Mirrors `backendswap`/`bench`/`status`.
- **D-02:** Volume I/O goes through a **cmd-tier fixed-arg `podman` injectable var**, mirroring `cmd/villa/uninstall.go`'s `podmanVolumeRm`. Commands: `podman volume export <name> --output <f>` and `podman volume import <name> <f>`. **Do NOT add a new impure module** — `orchestrate` stays the only intentionally-impure module, untouched except for volume *recreate* via Quadlet (D-07). Fixed-arg exec only; no shell; volume/model names config/catalog-resolved.
- **D-03:** **Single plain POSIX `.tar`** archive (no gzip in v1). Entries: `manifest.json`, `config.toml`, `openwebui-volume.tar` (the `podman volume export` output), `usage.json`, `bench-reports.jsonl` (the single append-only bench-reports store).
- **D-04:** Default output `villa-backup-<timestamp>.tar` in CWD; `-o/--output <path>` override. Written `0600`, parent dir honored as-is; output path **traversal-guarded** on write. *(Discretion: timestamp format (FS-safe, no `:`); whether to accept a positional path. Gzip revisitable, defaults OFF.)*
- **D-05:** **Stop `villa-openwebui.service` before `podman volume export`, restart after.** Volume-level quiesce-then-export. **Rejected:** reaching into the container for `sqlite3 .backup`/WAL-checkpoint. Accept brief chat downtime; document it.
- **D-06:** **Capture → quiesce → swap → restart → prove → rollback-on-failure.** Before mutating: **capture** current state to a temp rollback set under the XDG dir. Then quiesce, apply, restart, **prove**. On ANY mutate error or non-pass prove: **verbatim restore** + honest rollback-complete / rollback-incomplete (mirroring `backendswap.Run`).
- **D-07:** **Apply path:** restore `config.toml` via `config.SaveVilla` (0600/0700, atomic, guarded — never hand-write); restore data-dir artifacts with same atomic discipline; **recreate the OWUI volume via Quadlet** (regenerate units from restored config) then `podman volume import`; re-run **preflight** + assert **`status`** health (offload/residency-aware) as prove. Silent/partial CPU fallback at prove = FAIL → rollback.
- **D-08:** **WARN-and-confirm before applying; BLOCK only on corruption / incompatible manifest.** Skew → named warning + remediation + explicit `y/N` (bypass `--yes`/`--force`). Skew is often legitimate → do NOT hard-block. **Fail closed (BLOCK)** only on: failed **SHA-256** (corruption) or unreadable/incompatible **`manifest.schema_version`**.
- **D-09:** `manifest.json` carries own `schema_version`; records created-at, villa version, host fingerprint (arch/iGPU/kernel via `internal/detect`), **image digests** (inference backend + OWUI), `config`/`usage`/bench-store `schema_version`s, **per-entry SHA-256** (config, owui volume tar, usage.json, bench reports), and **excluded model identities** (catalog id/quant/ctx/source) for re-pull.
- **D-10:** **Image digests sourced from the seam, never re-typed.** Inference digest from `internal/inference` (`Backend.Image()`); OWUI image from `internal/orchestrate`. Backup MUST NOT hardcode any image literal — `TestSeamGrepGate` stays green.
- **D-11:** **Tar-slip guard on restore extraction.** Every archive entry path validated to stay inside the intended extraction dir before write (reuse `config.assertInsideDir` discipline). Output path traversal-guarded on write. All files `0600`, dirs `0700`.
- **D-12:** **No new outbound.** Only the *existing* image/model pull on re-pull. Preserve `status` `no_telemetry` posture.
- **D-13:** `villa backup`/`villa restore` may expose `--json`. If so, a **new, separate frozen contract** (own `schema_version` + new golden under `cmd/villa/testdata/`), NOT an evolution of `status.Report`. Restore output is offload-/prove-honest (never false-green).

### Claude's Discretion

- Exact Go type/field names (`Manifest`, `Deps` shape, `Fold`-style signatures), precise archive timestamp format, whether `--json` ships this phase, whether bench reports are tarred individually or as a sub-bundle.
- Whether restore's prove step calls the `status` core directly or composes `preflight` + a residency assert — pick the lightest offload-honest path.

### Deferred Ideas (OUT OF SCOPE)

- Automatic model-weight **re-pull** on restore (record + report identities only).
- Scheduling / retention / rotation / incremental / differential backups.
- Encryption-at-rest and remote/off-box backup targets.
- gzip/zstd compression (defaults OFF in v1).
- Hardened cross-host / post-`podman system reset` restore beyond what the research flag validates — same-host round-trip is the committed bar.
</user_constraints>

<phase_requirements>
## Phase Requirements

| ID | Description | Research Support |
|----|-------------|------------------|
| BAK-01 | Back up workspace (config + OWUI data volume) to a single archive excluding model weights, with a manifest of versions/digests/checksums | `archive/tar` outer assembly (stdlib); `podman volume export` (verified §"Podman Volume Mechanics"); `crypto/sha256` checksums; manifest from `detect`+`inference.Backend.Image()`+new orchestrate OWUI accessor+store schema consts (§"Manifest & Digest Sourcing") |
| BAK-02 | Restore transactionally (capture→quiesce→swap→restart→prove→rollback) so a failed/partial restore never corrupts a running stack | `backendswap.Run` skeleton to clone (§"Transactional Restore"); empirically-verified import-into-recreated-volume requirement (§"Podman Volume Mechanics" — merge-not-replace finding); preflight+status prove |
| BAK-03 | `villa restore` warns on version/digest skew between manifest and current install before applying | Pure skew comparison in `internal/backup`; WARN-and-confirm vs fail-closed boundaries (D-08, §"Skew Detection") |
</phase_requirements>

## Summary

Phase 16 adds `villa backup` and `villa restore` to the `villa` control plane. The architecture is fully prescribed by CONTEXT.md and slots cleanly into the established pure-core + injectable-seam pattern: a new **pure `internal/backup`** core (manifest build, SHA-256 compute/verify, archive entry planning, skew comparison) with all host I/O (podman volume export/import, file r/w, service stop/start, Quadlet recreate, preflight, status-prove) injected as `Deps` `func` fields and wired by `live*Deps` closures in `cmd/villa/backup.go` / `cmd/villa/restore.go`. The restore state-machine is a near-clone of `internal/backendswap/backendswap.go`'s capture→mutate→prove→rollback frame with honest rollback-complete/incomplete reporting.

The MEDIUM-confidence external mechanics flagged by the ROADMAP are now **HIGH-confidence, empirically verified** against the installed `podman 5.8.2`. Two findings are load-bearing and change the plan: **(1) `podman volume import` does NOT auto-create the target volume** — it errors `no volume with name "X" found`, so the OWUI volume MUST be recreated (via Quadlet, D-07) before import; **(2) `podman volume import` MERGES into existing contents, it does NOT replace** — a stray file left in a populated volume survives re-import. Therefore restore MUST import into a **freshly-recreated (clean) volume**, never over a live one. The Open WebUI official docs corroborate D-05: cold (stopped-container) backups are recommended for SQLite integrity.

**Primary recommendation:** Build `internal/backup` as a pure core mirroring `backendswap`; add a cmd-tier fixed-arg `podmanVolumeExport`/`podmanVolumeImport` var pair cloned from `uninstall.go:podmanVolumeRm`; add one new **exported orchestrate accessor for the OWUI image digest** (currently unexported with no getter — the only new seam-resident addition needed for D-10); recreate-clean-then-import the OWUI volume on restore; and introduce a build-stamped villa version (none exists today — see Open Questions).

## Architectural Responsibility Map

| Capability | Primary Tier | Secondary Tier | Rationale |
|------------|-------------|----------------|-----------|
| Manifest build / SHA-256 compute & verify / skew compare / entry planning | Pure core (`internal/backup`) | — | No host I/O — testable off-hardware; mirrors `backendswap`/`status` purity (D-01) |
| Outer `.tar` assembly + tar-slip-guarded extraction | Pure core (`internal/backup`) over injected `io.Writer`/`io.Reader` | cmd-tier (opens files) | Tar logic is deterministic/pure; only the file handles are seams (D-03/D-11) |
| `podman volume export`/`import` | cmd-tier fixed-arg `podman` var | — | Mirrors `uninstall.go` precedent; keeps `orchestrate` the only impure module (D-02) |
| OWUI volume *recreate* (clean) | `orchestrate` (Quadlet render + WriteUnits + systemd) | cmd-tier wiring | Config is source of truth — volume regenerated from restored config, never hand-built (D-07) |
| config restore | `internal/config` (`SaveVilla`) | cmd-tier | Atomic 0600/0700 + traversal guard already proven; never hand-write (D-07) |
| data-dir artifact restore (usage.json, bench reports) | cmd-tier file write (atomic, guarded) | `internal/usage`/`internal/benchstore` resolvers | Same XDG write discipline; resolvers locate paths (D-07) |
| service quiesce (stop/restart OWUI) | `orchestrate.Systemd` seam (via Deps) | cmd-tier | Existing systemd seam; quiesce is host I/O (D-05) |
| image digests for manifest | `internal/inference` (`Backend.Image()`) + `internal/orchestrate` (new OWUI accessor) | — | Seam-locked literals — never re-typed (D-10) |
| prove (preflight + status/residency) | `internal/preflight` + `internal/status` cores | cmd-tier wiring | Offload-asserting; silent CPU fallback = FAIL → rollback (D-07) |

## Standard Stack

### Core (Go stdlib — no new third-party dependency)
| Library | Version | Purpose | Why Standard |
|---------|---------|---------|--------------|
| `archive/tar` | stdlib (Go 1.26) | Assemble + read the outer single `.tar` (manifest.json + config.toml + openwebui-volume.tar + usage.json + bench-reports.jsonl) | Single-static-binary constraint forbids new deps; deterministic POSIX tar is exactly D-03 |
| `crypto/sha256` | stdlib | Per-entry checksums in the manifest; verify on restore | Standard, CGO-free, no dep |
| `encoding/json` | stdlib | `manifest.json` (mirrors `usage`/`benchstore`/`status` JSON contracts) | Matches existing schema-versioned JSON contracts |
| `os` / `io` / `path/filepath` | stdlib | File r/w (seams), tar-slip guard, atomic temp+rename | Same primitives `config`/`usage`/`benchstore` already use |
| `os/exec` | stdlib | Fixed-arg `podman volume export/import` (cmd-tier var) | Identical to `uninstall.go:podmanVolumeRm` |

### Supporting (existing in-tree — reuse, do not re-implement)
| Symbol | File | Purpose | Reuse As |
|--------|------|---------|----------|
| `podmanVolumeRm` / `volumeRmArgs` | `cmd/villa/uninstall.go:352,361` | Seam-gate-proven fixed-arg podman var + pure arg-builder | **Clone** into `podmanVolumeExport`/`podmanVolumeImport` + `volumeExportArgs`/`volumeImportArgs` |
| `backendswap.Run` + `Deps` + `Result` | `internal/backendswap/backendswap.go` | Transactional capture→mutate→prove→rollback w/ honest rollback reporting | **Clone the skeleton** for `backup.Restore` |
| `config.SaveVilla` / `SaveVillaTo` / `assertInsideDir` | `internal/config/villaconfig.go:150,184,223` | Atomic 0600/0700 XDG write + traversal guard | Config restore + the template for the tar-slip extraction guard (D-11) |
| `orchestrate.Render` / `WriteUnits` / `Systemd` (`Stop`/`Restart`/`DaemonReload`) | `internal/orchestrate/render.go`, `systemd.go` | Recreate OWUI volume via Quadlet from restored config; quiesce | Volume recreate (D-07) + service quiesce (D-05) |
| `inference.BackendFor(name).Image()` | `internal/inference/backend.go:24`, `inference.go:58` | Seam-clean inference image digest accessor | Manifest digest source (D-10) |
| `usage.UsagePath()` + `usageSchemaVersion` (NEW accessor `usage.SchemaVersion()`) | `internal/usage/usage.go:209,36` | Locate `usage.json`; record store schema in manifest | Data-dir capture + manifest field (accessor added in Plan 02) |
| `benchstore` (`bench-reports.jsonl`, `savedReportSchemaVersion=1`, NEW accessor `benchstore.SavedReportSchemaVersion()`) | `internal/benchstore/benchstore.go:300,32` | Locate bench reports; record store schema | Data-dir capture + manifest field (accessor added in Plan 02) |
| `benchReportsStorePath()` / `benchStoreLocation()` | `cmd/villa/bench.go:285,271` | Cmd-tier resolver for the single `bench-reports.jsonl` store path + root | Capture/restore the bench-reports.jsonl entry |
| `detect` host profile (arch/iGPU/kernel) | `internal/detect/` | Host fingerprint for the manifest + skew compare | Manifest field (D-09) |
| `preflight.Run` / `status` core | `internal/preflight/`, `internal/status/status.go` | Prove step (offload-asserting) | Restore prove (D-07) |

### Alternatives Considered
| Instead of | Could Use | Tradeoff |
|------------|-----------|----------|
| `podman volume export/import` | Host-path tar of the storage `_data` dir | **FORBIDDEN by ROADMAP/D-02** — host-path tar bypasses podman's UID-map handling and is brittle across podman storage layout; export/import is the supported path |
| Plain `.tar` (D-03) | `.tar.gz`/`.tar.zst` | Gzip adds non-determinism to checksum reasoning and weights are excluded (archive is small) — deferred, default OFF |
| New cmd-tier podman var (D-02) | New `orchestrate.volume_io` seam | ROADMAP allows either; CONTEXT D-02 **locks** the cmd-tier var (keeps `orchestrate` untouched bar volume recreate) |
| Container-internal `sqlite3 .backup` | volume-level stop-then-export (D-05) | Container approach couples to OWUI internals + adds a tool dep — **rejected by D-05** |

**Installation:** None. Zero new third-party packages — Go stdlib only. (Single-static-binary / CGO-free constraint preserved.)

## Package Legitimacy Audit

**Not applicable — this phase installs no external packages.** All functionality uses the Go standard library (`archive/tar`, `crypto/sha256`, `encoding/json`, `os`, `io`, `os/exec`, `path/filepath`) and existing in-tree internal packages. No `go get`, no `go.mod` change. The single-static-binary / CGO-free invariant is preserved by construction.

## Podman Volume Mechanics (research flag — now HIGH confidence)

**All claims below were empirically verified against `podman 5.8.2` (installed on this host) on 2026-06-07.** [VERIFIED: local `podman 5.8.2` round-trip]

### Export
```
podman volume export VOLUME -o OUTPUT.tar      # -o/--output; default stdout (must redirect)
```
- The export tar contains the volume's contents **at the tar root** (no leading path prefix). For an OWUI volume, `webui.db`, `uploads/`, `vector_db/`, `cache/`, `audit.log` land directly under the tar root.
- Verified: `tar tvf` of a 2-file volume export showed `a.txt`, `b.txt` at root.

### Import — TWO load-bearing findings that shape the restore plan
```
podman volume import VOLUME SOURCE.tar         # or: cat x.tar | podman volume import VOLUME -
```
Accepted source formats: `.tar`, `.tar.gz`, `.tgz`, `.bzip`, `.tar.xz`, `.txz`, `.tar.zst`.

1. **`import` does NOT auto-create the target volume.** Importing into a non-existent volume fails: `Error: no volume with name "X" found: no such volume`. **→ The OWUI volume MUST be recreated (via Quadlet, D-07) BEFORE `podman volume import`.** [VERIFIED]
2. **`import` MERGES into existing contents — it does NOT replace/wipe first.** A stray file written into a populated volume *survived* a subsequent `import` (the imported `a.txt`/`b.txt` coexisted with the pre-existing `stray.txt`). **→ Restore MUST import into a FRESHLY-RECREATED (clean) volume, never over a live/populated one** — otherwise stale state (old chats, old `webui.db`) leaks through. The clean-recreate is: `podman volume rm` (cloned `volumeRmArgs`) → Quadlet `WriteUnits` + recreate → `import`. [VERIFIED]

### Rootless ownership & cross-host (research flag — honest limitation)
- Rootless volumes live at `~/.local/share/containers/storage/volumes/<name>/_data`; host-side ownership maps to the running user (verified `1000:1000` mountpoint). [VERIFIED: local inspect]
- **Same-host round-trip** (the committed bar, SC2): export then import on the same host with an unchanged UID map needs **no chown** — the UID mapping is identical. `podman volume import` writes through the same user namespace.
- **Cross-host / post-`podman system reset`** (research flag, NOT a committed deliverable): if the subuid/subgid range or the user's UID differs on the target, imported files can land with a mismatched in-namespace owner. The documented repair is `podman unshare chown -R <uid>:<gid> <mountpoint>` (or the `:U` mount option to auto-chown on first run). [CITED: redhat.com rootless user-namespace blog; tutorialworks.com]
- **SELinux:** host is `Enforcing`. The OWUI volume mount already carries `:Z` (private relabel) — `internal/orchestrate/openwebui.go:39` `openWebUIVolumeMount = "...:/app/backend/data:Z"`. Because the volume is **recreated via Quadlet from config** (D-07), the `:Z` relabel is re-applied by podman on the recreated mount on next container start — so same-host restore needs no manual `chcon`. A cross-host import into a volume whose label context differs may need `podman unshare ... ` / relabel; this is the same cross-host case above.

**What the same-host round-trip test MISSES** (must be documented as a known limitation, not silently passed): UID-map divergence and SELinux label-context divergence only manifest on a *different* host or after `podman system reset` rebuilds the storage/idmap. A same-host test cannot exercise these. **Recommendation:** ship same-host round-trip as the verified bar; document cross-host restore as "best-effort — if OWUI fails to read its volume after a cross-host restore, run `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` and ensure the `:Z` relabel" in the restore output/docs. Do NOT claim cross-host parity.

## Open WebUI Quiesce (research flag — D-05 confirmed)

- OWUI stores a **SQLite `webui.db`** plus `uploads/`, `vector_db/`, `cache/`, `audit.log` under `/app/backend/data` (the `villa-openwebui` volume). [CITED: docs.openwebui.com database-and-storage]
- Open WebUI's **own backup docs recommend a cold (stopped-container) backup** "to maintain data integrity … on cold filesystems" — exactly D-05's stop-then-export. Live `rsync` is offered only as a lower-integrity alternative. [CITED: docs.openwebui.com/tutorials/maintenance/backups]
- **WAL sidecars:** SQLite in WAL mode keeps `webui.db-wal` and `webui.db-shm` alongside the DB. If exported live, an un-checkpointed `-wal` could yield a torn copy. **Stopping the container cleanly closes the DB**, which checkpoints and removes/empties `-wal`/`-shm`, so the stopped-then-export copy is consistent. `podman volume export` captures whatever files are present at export time — so the stop MUST complete before export. The chosen quiesce (D-05) is therefore correct and tool-dependency-free.
- **Concrete quiesce sequence for backup:** `systemctl --user stop villa-openwebui.service` → wait for stop to return → `podman volume export villa-openwebui -o <tmp>/openwebui-volume.tar` → assemble outer tar → `systemctl --user start villa-openwebui.service`. The restart belongs in a `defer`-style cleanup so a mid-backup failure still restarts chat (mirror `backendswap`'s best-effort re-ready).

## Manifest & Digest Sourcing (D-09 / D-10)

The `manifest.json` (own `schema_version`) records, per D-09:
- `created_at` (RFC3339), `villa_version` (see Open Question 1 — **no version constant exists today**), host fingerprint (arch / iGPU / kernel via `internal/detect`),
- **image digests:** inference image via `inference.BackendFor(cfg.Backend).Image()` (seam-clean, `inference.go:58`); **OWUI image — needs a NEW exported orchestrate accessor** (see below),
- store schema versions: `config` (no explicit const — use a backup-owned const or the recommend/config contract version), `usage` (`usage.SchemaVersion()` = `usageSchemaVersion=1`), bench (`benchstore.SavedReportSchemaVersion()` = `savedReportSchemaVersion=1`) — both are sourced via NEW exported accessors (the consts are unexported, so neither pure `internal/backup` nor `cmd/villa` could read them otherwise; accessors added in Plan 02),
- **per-entry SHA-256** for config.toml, openwebui-volume.tar, usage.json, and the single bench-reports.jsonl,
- **excluded model identities** (catalog id / quant / ctx / source) read from `config` + catalog for re-pull (model weights excluded — BAK-01).

### OWUI digest accessor — the ONE new seam-resident addition (D-10)
`openWebUIImage` is currently **unexported with no getter** (`internal/orchestrate/openwebui.go:20`); only the *inference* image is reachable (`status.go:323` `backend.Image()`). To satisfy D-10 without re-typing a literal, the plan MUST **add an exported accessor in `internal/orchestrate`** (e.g. `func OpenWebUIImage() string { return openWebUIImage }`). This keeps the literal behind the orchestrate seam (orchestrate is allowed to hold managed-service image literals — `openwebui.go` comment confirms OWUI image is an orchestrate-level constant, NOT an inference-seam token). `internal/backup` and `cmd/villa` then read the digest via this accessor — never as a literal — so `TestSeamGrepGate` stays green (the gate's `container image literal` regex matches `ghcr.io`? **No** — verify: the regex is `kyuz0|docker\.io/|server-vulkan|:rocm-...`; `ghcr.io/open-webui` is NOT matched, but the accessor approach is still correct discipline and future-proof). [VERIFIED: seam_test.go regex read]

### Store schema-version accessors — TWO new exported one-liners (D-09 reachability)
`usageSchemaVersion` (`internal/usage/usage.go:36`) and `savedReportSchemaVersion` (`internal/benchstore/benchstore.go:32`) are **unexported consts** — neither the pure `internal/backup` core nor `cmd/villa` can read them, yet `manifest.UsageSchemaVersion`/`manifest.BenchSchemaVersion` (D-09) must source them. The plan adds two exported one-line accessors — `func SchemaVersion() int { return usageSchemaVersion }` in `internal/usage` and `func SavedReportSchemaVersion() int { return savedReportSchemaVersion }` in `internal/benchstore` (Plan 02 Task 1, `files_modified` extended accordingly). The cmd-tier supplies the real values into the manifest input via these accessors. (Config has no dedicated schema-version const — see A3; that one row stays a backup-owned constant that MIRRORS the config contract, with a guard test, exactly as A3 documents.)

## Transactional Restore (mirror `backendswap.Run`)

Clone the `backendswap` frame (`internal/backendswap/backendswap.go:145-253`). Concrete sequence for `backup.Restore`:

1. **Read + verify archive (pure, BLOCK on failure):** open outer tar, parse `manifest.json`, verify every entry's SHA-256 against the manifest. A checksum mismatch or unreadable/incompatible `manifest.schema_version` → **fail closed, zero side effects** (D-08).
2. **Skew check (pure) + WARN-and-confirm:** compare manifest vs current install (§"Skew Detection"). Print named warnings + remediation; require `y/N` (bypass `--yes`/`--force`). Legitimate skew does NOT block (D-08).
3. **CAPTURE strictly before any mutation** (mirror `backendswap` step 4): export the *current* `villa-openwebui` volume to a temp rollback tar under the XDG dir; copy current `config.toml`; copy current `usage.json` + bench-reports.jsonl. An uncapturable current state → refuse without mutating.
4. **QUIESCE:** `systemctl --user stop villa-openwebui.service` (and any service whose data is being replaced).
5. **MUTATE (any error → rollback):**
   a. `config.SaveVilla(restoredCfg)` (atomic, guarded — D-07).
   b. restore data-dir artifacts (usage.json, bench-reports.jsonl) with atomic temp+rename, 0600/0700, traversal-guarded.
   c. **recreate the OWUI volume CLEAN:** `podman volume rm villa-openwebui` (cloned `volumeRmArgs`, tolerate-not-found) → `orchestrate.Render(restoredCfg)` + `WriteUnits` + `DaemonReload` to re-establish the volume unit → ensure the volume exists (explicit fixed-arg `podman volume create villa-openwebui` — the robust default; see resolved Open Question 2) → `podman volume import villa-openwebui <extracted owui tar>`. **(Clean recreate is mandatory — import MERGES, §"Podman Volume Mechanics".)**
   d. restart services (`systemctl --user start ...`).
6. **PROVE (offload-asserting):** re-run `preflight` AND assert `status` health with residency proof. Switch-to-success ONLY on a true pass; a ready+health-200-but-residency-FAIL or silent CPU fallback = FAIL → rollback (D-07, mirrors `ProveStatusPass`).
7. **ROLLBACK on ANY mutate error or non-pass prove:** verbatim restore from the captured set — re-`SaveVilla(priorCfg)`, restore captured data-dir artifacts, recreate-clean + `import` the captured OWUI volume tar, restart, re-prove best-effort. Accumulate errors across steps; report **rollback-complete vs rollback-incomplete** honestly (clone `backendswap`'s `rollback()`/`rolledBack()` closures — never claim a clean no-op when a restore step errored).

The `Result` type mirrors `backendswap.Result` (Refused / Restored / RolledBack / NoOp / Reason / Err / FailedStep / Prove).

## Skew Detection (BAK-03 / D-08)

Pure comparison in `internal/backup`. Compare manifest vs current install and classify:

| Field | Source (current) | WARN-and-confirm | Fail-closed BLOCK |
|-------|------------------|------------------|-------------------|
| `manifest.schema_version` | backup-owned const | — | **BLOCK** if unreadable / > current (incompatible) |
| per-entry SHA-256 | recomputed on read | — | **BLOCK** (archive corruption) |
| villa version | build-stamp (OQ1) | WARN on mismatch | — |
| inference image digest | `Backend.Image()` | WARN on mismatch (re-pull remediation) | — |
| OWUI image digest | new orchestrate accessor | WARN on mismatch (re-pull remediation) | — |
| config schema version | config/recommend const | WARN on older; — | BLOCK on *newer* (future schema can't be safely applied — mirror `usage.Load` fail-closed-on-future) |
| usage schema version | `usage.SchemaVersion()` | WARN on older | BLOCK on newer (same rationale) |
| bench schema version | `benchstore.SavedReportSchemaVersion()` | WARN on older | BLOCK on newer |
| host fingerprint (arch/iGPU/kernel) | `detect` | WARN on mismatch (cross-host caveat — §"Podman Volume Mechanics") | — |

Each WARN carries named remediation text (e.g. "image digest differs — after restore, re-pull weights with `villa model pull <id>`" / "backed up on a different host — if OWUI cannot read its data, run `podman unshare chown -R ...`").

## Architecture Patterns

### System Architecture Diagram

```
                          villa backup                              villa restore
                               │                                         │
  cmd/villa/backup.go ─────────┤                  cmd/villa/restore.go ──┤
   (liveBackupDeps wiring)     │                   (liveRestoreDeps)      │
                               ▼                                         ▼
                    ┌─────────────────────┐               ┌──────────────────────────┐
                    │  internal/backup    │   pure core   │   internal/backup        │
                    │  (PURE)             │◄──────────────┤   Restore() = clone of   │
                    │  • Manifest build   │               │   backendswap.Run frame  │
                    │  • SHA-256 compute  │               │  capture→quiesce→swap→   │
                    │  • tar entry plan   │               │  restart→prove→rollback  │
                    │  • skew compare     │               └──────────┬───────────────┘
                    └──────────┬──────────┘                          │ injected Deps (func fields)
                               │ injected Deps                       │
        ┌──────────────────────┼─────────────────┐  ┌───────────────┼──────────────────────────┐
        ▼                      ▼                 ▼  ▼               ▼                            ▼
  [QUIESCE]             [VOLUME I/O]        [FILE I/O]      [CONFIG]                    [PROVE]
  orchestrate.Systemd   cmd-tier fixed-arg  os read/write   config.SaveVilla           preflight.Run
  Stop/Restart          podman var:         (atomic,0600,   (atomic,guarded)           + status core
  villa-openwebui       export/import       tar-slip guard)  ───────────┐              (offload-asserting;
        │               (clone of                │                      ▼              silent CPU = FAIL)
        │               podmanVolumeRm)           │            orchestrate.Render +
        │                      │                  │            WriteUnits (recreate
        │                      │                  │            OWUI volume CLEAN from
        ▼                      ▼                  ▼            restored config — Quadlet
   systemd --user      podman volume        outer .tar        is source of truth)
                       export/import        manifest.json +
                       (rootless v5)        config.toml +
                                            openwebui-vol.tar +
                                            usage.json + bench-reports.jsonl

  DIGEST SOURCES (manifest, seam-locked — never re-typed):
    inference image ◄── inference.BackendFor(cfg.Backend).Image()   (internal/inference seam)
    OWUI image      ◄── orchestrate.OpenWebUIImage()  [NEW accessor] (internal/orchestrate seam)
    host/arch/kernel◄── internal/detect
```

### Recommended Project Structure
```
internal/backup/
├── backup.go        # pure: Manifest type, BuildManifest, archive entry planning, Backup() orchestrator over Deps
├── restore.go       # pure: Restore() = capture→quiesce→swap→restart→prove→rollback (clone of backendswap.Run)
├── manifest.go      # pure: Manifest struct (schema_version LAST field, append-only), Skew compare
├── checksum.go      # pure: sha256 of an io.Reader; verify
├── tarutil.go       # pure: write/read outer tar over io.Writer/io.Reader; tar-slip guard (config.assertInsideDir shape)
├── deps.go          # the injectable Deps struct (func fields)
└── *_test.go        # off-hardware: fakeDeps drive the whole flow + golden manifest fixtures

cmd/villa/
├── backup.go        # newBackup() cobra cmd; liveBackupDeps(); podmanVolumeExport var; runBackup returns exit code
├── restore.go       # newRestore() cobra cmd; liveRestoreDeps(); podmanVolumeImport var; y/N consent + --yes/--force
└── testdata/
    └── backup.json.golden / restore.json.golden   # IF --json ships (D-13) — NEW frozen contract, own schema_version
```

### Pattern 1: cmd-tier fixed-arg podman var (clone of `uninstall.go`)
**What:** A package-level `var podmanVolumeExport = func(args []string) (stderr string, err error) {...}` running `exec.Command("podman", args...)` with fixed args, plus a pure `volumeExportArgs(name, out string) []string` builder. Identical pattern for import.
**When to use:** Every podman volume op in this phase (D-02).
**Example:**
```go
// Source: clone of cmd/villa/uninstall.go:352-367 (volumeRmArgs / podmanVolumeRm)
func volumeExportArgs(name, out string) []string {
    return []string{"volume", "export", name, "--output", out} // fixed args, no shell
}
func volumeImportArgs(name, src string) []string {
    return []string{"volume", "import", name, src}
}
var podmanVolume = func(args []string) (stderr string, err error) {
    var buf bytes.Buffer
    cmd := exec.Command("podman", args...) // fixed args (T-03-25: never a shell)
    cmd.Stderr = &buf
    err = cmd.Run()
    return strings.TrimSpace(buf.String()), err
}
```

### Pattern 2: transactional capture→prove→rollback (clone of `backendswap`)
**What:** Capture verbatim prior state BEFORE any mutation; gate cutover on a `Prove` verdict; on any error/non-pass, run a best-effort `rollback()` that accumulates errors and reports complete/incomplete.
**When to use:** `backup.Restore` (D-06).
**Example:** See `internal/backendswap/backendswap.go:145-253` — clone the `rollback`/`rolledBack` closures and the `ProveStatusPass` sentinel discipline verbatim.

### Pattern 3: schema-versioned JSON contract, append-only, version field LAST
**What:** `Manifest` (and any `--json` output) carries `SchemaVersion int` as the LAST struct field; new fields append ABOVE it; bump the const on a breaking change.
**When to use:** `manifest.json` and the optional `--json` contract (D-09/D-13).
**Example:** `internal/benchstore/benchstore.go:67-69` and `internal/usage/usage.go:69` (`SchemaVersion … json:"schema_version"` LAST).

### Anti-Patterns to Avoid
- **Host-path tar of the volume `_data` dir** — FORBIDDEN (D-02); bypasses podman UID-map handling.
- **`podman volume import` over a live/populated volume** — MERGES, leaks stale state (verified). Always recreate clean first.
- **Hand-editing/hand-writing a restored Quadlet unit or config.toml** — config is the single source of truth; recreate units via `orchestrate.Render`, write config via `config.SaveVilla`.
- **Re-typing an image digest literal in `internal/backup` or `cmd/villa`** — read via `Backend.Image()` / new `orchestrate.OpenWebUIImage()` (D-10).
- **Treating health-200 / is-active as restore success** — prove must be offload-asserting; silent CPU fallback = FAIL → rollback.
- **Claiming a clean no-op when rollback errored** — report rollback-incomplete honestly (`backendswap` Pitfall 5).

## Don't Hand-Roll

| Problem | Don't Build | Use Instead | Why |
|---------|-------------|-------------|-----|
| Volume capture/restore | Host-path tar of `_data` | `podman volume export/import` (cmd-tier var) | UID-map/SELinux handling; D-02 mandate |
| Atomic guarded config write | Hand `os.WriteFile` of TOML | `config.SaveVilla` | Atomic, 0600/0700, traversal-guarded, single-source-of-truth |
| Transactional rollback | New state-machine | Clone `backendswap.Run` | Proven capture/prove/rollback + honest incomplete reporting |
| Path-traversal guard (tar-slip + output) | Ad-hoc `strings.HasPrefix` | `config.assertInsideDir` shape (already cloned 3× in-tree) | Correct `filepath.Rel`-based escape check |
| Volume recreate | `podman volume create` as the authority | Quadlet `Render`+`WriteUnits` from restored config | Config is the single source of truth (D-07) |
| Image digest for manifest | Copy the literal string | `Backend.Image()` / new `orchestrate.OpenWebUIImage()` | Seam-locked; `TestSeamGrepGate` |

**Key insight:** Almost everything this phase needs already exists in-tree as a proven, tested seam. The phase is ~80% composition (clone `uninstall` podman var + clone `backendswap` frame + reuse `config`/`orchestrate`/`detect`/`preflight`/`status`) and ~20% new pure code (manifest, sha256, outer-tar assembly, skew compare). The only genuinely new seam-resident additions are the exported OWUI image accessor and the two store schema-version accessors (`usage.SchemaVersion()` / `benchstore.SavedReportSchemaVersion()`).

## Runtime State Inventory

> Rename/refactor inventory is N/A for this greenfield-feature phase. The analogous "what runtime state must restore reconstruct?" inventory:

| Category | Items Found | Action Required |
|----------|-------------|------------------|
| Stored data (in scope) | OWUI `villa-openwebui` volume (webui.db + uploads/vector_db/cache); `config.toml`; `usage.json`; bench reports (single `bench-reports.jsonl`) | Captured by backup; recreated by restore (volume via clean-recreate+import; files via atomic write) |
| Stored data (EXCLUDED) | `villa-models` volume (GGUF weights) | Excluded by BAK-01; identity recorded in manifest for re-pull (`villa model pull`) — NOT restored |
| Live service config | Quadlet units (`*.container`/`*.volume`/`*.network`) | NOT in the archive — regenerated from restored `config.toml` via `orchestrate.Render`+`WriteUnits` (config is source of truth) |
| OS-registered state | `systemctl --user` services; user linger | Quiesce stops/starts OWUI; restore relies on existing units/linger from install — restore does NOT re-run install/enable-linger |
| Secrets/env vars | OWUI env block (telemetry-kill set, `sk-no-key-required` sentinel) | Baked into the regenerated Quadlet unit by `orchestrate` — NOT separately backed up (deterministic from config) |
| Build artifacts | None | — |
| **NEW gap** | **No villa version constant exists** (no `cobra.Version`, no build stamp) — manifest `villa_version` (D-09) has no source | See Open Question 1 — add a build-stamped version var |

## Common Pitfalls

### Pitfall 1: Importing into a live/populated volume (MERGE, not replace)
**What goes wrong:** `podman volume import` merges; old `webui.db`/chats survive the restore, silently corrupting the "restored" state with stale data.
**Why it happens:** Intuition says import = overwrite; it does not (empirically verified).
**How to avoid:** Always `podman volume rm` + Quadlet-recreate CLEAN before `import` (both forward apply AND rollback).
**Warning signs:** Restore "succeeds" but old chats/users remain.

### Pitfall 2: Importing into a non-existent volume
**What goes wrong:** `import` errors `no volume with name "X" found`; restore aborts.
**Why it happens:** `import` does not auto-create (verified).
**How to avoid:** Recreate the volume via Quadlet before import (D-07). If the Quadlet generator does not eagerly create the volume until first container start, an explicit fixed-arg `podman volume create` may be required (Open Question 2 — resolved: explicit create is the robust default).

### Pitfall 3: Exporting a live SQLite DB (torn copy)
**What goes wrong:** Live export captures an un-checkpointed `-wal`/`-shm`; the imported DB is inconsistent/corrupt.
**Why it happens:** SQLite WAL coordination needs a clean close to checkpoint.
**How to avoid:** Stop `villa-openwebui.service` BEFORE export (D-05); ensure stop returns before exporting; restart after (best-effort in a defer).
**Warning signs:** OWUI errors on first start after restore; missing recent chats.

### Pitfall 4: Capturing rollback state AFTER mutation begins
**What goes wrong:** A mid-restore failure cannot be rolled back because the prior state was already overwritten.
**How to avoid:** Capture (export current volume + copy current config/usage/bench) STRICTLY before any mutation, exactly like `backendswap` step 4.

### Pitfall 5: False-green prove / dishonest rollback
**What goes wrong:** Restore reports success on health-200 alone, or claims a clean no-op when rollback steps errored.
**How to avoid:** Prove is offload-asserting (silent CPU fallback = FAIL → rollback); rollback accumulates errors and reports complete/incomplete (clone `backendswap`).

### Pitfall 6: Leaking an image digest literal past the seam
**What goes wrong:** Copying the OWUI/inference digest into `internal/backup` or `cmd/villa` — risks `TestSeamGrepGate` (and the discipline) regressing.
**How to avoid:** Add/use exported seam accessors (`Backend.Image()`, new `orchestrate.OpenWebUIImage()`); never a literal.

## Code Examples

### Outer tar assembly (pure, over an injected io.Writer)
```go
// Source: Go stdlib archive/tar; mirrors deterministic-layout discipline (D-03)
func writeArchive(w io.Writer, entries []entry) error {
    tw := tar.NewWriter(w)
    for _, e := range entries { // deterministic order: manifest.json FIRST
        hdr := &tar.Header{Name: e.name, Mode: 0o600, Size: int64(len(e.data)), Format: tar.FormatPAX}
        if err := tw.WriteHeader(hdr); err != nil { return err }
        if _, err := tw.Write(e.data); err != nil { return err }
    }
    return tw.Close()
}
```

### Tar-slip-guarded extraction (D-11)
```go
// Source: clone of config.assertInsideDir (villaconfig.go:223)
for {
    hdr, err := tr.Next()
    if err == io.EOF { break }
    if err != nil { return err }
    dst := filepath.Join(destDir, hdr.Name)
    if err := assertInsideDir(dst, destDir); err != nil { // refuse traversal escapes
        return fmt.Errorf("backup: refusing tar entry %q outside %q", hdr.Name, destDir)
    }
    // write dst with 0600 / dirs 0700 ...
}
```

### SHA-256 of an entry (manifest + verify)
```go
// Source: Go stdlib crypto/sha256
func sum(r io.Reader) (string, error) {
    h := sha256.New()
    if _, err := io.Copy(h, r); err != nil { return "", err }
    return hex.EncodeToString(h.Sum(nil)), nil
}
```

## State of the Art

| Old Approach | Current Approach | When Changed | Impact |
|--------------|------------------|--------------|--------|
| Host-path tar of volume `_data` | `podman volume export/import` | podman v2+ (stable in v5) | Supported, UID-map-aware; the D-02 mandate |
| Live DB file copy | Stop-then-export (cold) | SQLite WAL guidance | Consistent backups; OWUI docs concur |

**Deprecated/outdated:** None relevant. `podman volume export/import` is stable and present in the installed v5.8.2.

## Assumptions Log

| # | Claim | Section | Risk if Wrong |
|---|-------|---------|---------------|
| A1 | A build-stamped villa version should be added (none exists) for `manifest.villa_version` (D-09) | Runtime State Inventory / OQ1 | Manifest version field is empty/placeholder; skew-on-version (BAK-03) degraded but not blocking |
| A2 | The Quadlet generator does not eagerly create the OWUI volume until first container start, so restore may need an explicit `podman volume create` before `import` | Transactional Restore step 5c / OQ2 | If wrong (generator pre-creates), the extra create is a harmless no-op; if right and omitted, import fails — plan must verify on hardware |
| A3 | `config` has no dedicated schema-version const; a backup-owned const that MIRRORS the config contract is the comparison source for config skew, guarded by a test that catches future config drift | Skew Detection | Config skew row uses a mirrored source; WARN-only so low blast radius; the guard test catches drift |
| A4 | Stopping `villa-openwebui.service` cleanly checkpoints/closes SQLite WAL before export | OWUI Quiesce | If OWUI does not close cleanly on SIGTERM within the stop timeout, export could still be torn — verify on hardware (research-flag UAT) |

## Open Questions (RESOLVED)

1. **No villa version constant exists in the codebase.** `cobra.Version` is unset; there is no build-stamped version var.
   - What we know: D-09 requires `manifest.villa_version`; skew (BAK-03) compares it.
   - What's unclear: where the version should come from.
   - Recommendation: add a `-ldflags "-X main.version=v1.2.0"` build stamp (Makefile) + a `var version = "dev"` in `cmd/villa`; expose it for the manifest. Small, self-contained; planner should add a task. If deferred, manifest records `"dev"`/`"unknown"` and version-skew is WARN-only (non-blocking) — acceptable but weaker.
   - **RESOLVED:** Plan 16-01 (Task 2) adds a build-stamped `var version = "dev"` in `cmd/villa/version.go` plus a Makefile `-ldflags "-X main.version=$(VERSION)"` stamp, exposed via `villaVersion()` as the source for `manifest.villa_version`.

2. **Does the Quadlet `.volume` generator pre-create the OWUI volume, or only on first container start?**
   - What we know: `import` requires the volume to pre-exist (verified).
   - What's unclear: whether `WriteUnits`+`daemon-reload` alone materializes the podman volume, or whether it appears only when the container starts.
   - Recommendation: after recreate, either (a) `systemctl --user start villa-openwebui.service` once to materialize the volume then stop+import+start, or (b) explicit fixed-arg `podman volume create villa-openwebui` before import. Verify on hardware; (b) is the robust default.
   - **RESOLVED:** Plan 16-03 uses `EnsureVolume = podman volume create villa-openwebui` (explicit, idempotent — tolerate already-exists) before `VolumeImport`, the robust option (b). An extra create is a harmless no-op even if the Quadlet generator already pre-creates the volume, so this is safe either way; the Plan 03 on-hardware UAT exercises this explicit-create-then-import (`EnsureVolume → VolumeImport`) path on gfx1151.

3. **Should `--json` ship this phase (D-13)?**
   - Recommendation: Discretionary. If shipped, it is a NEW frozen golden (`backup.json.golden`/`restore.json.golden`) with its own `schema_version` — never a `status.Report` bump. Defaulting to ship `--json` for `restore` (the prove-honest outcome is valuable to script) is reasonable; planner decides.
   - **RESOLVED:** Discretionary per CONTEXT.md D-13; deferred-within-scope this phase (no `--json` in Plans 16-01/02/03). If added in a later phase it is a NEW frozen golden (own `schema_version` under `cmd/villa/testdata/`), never an evolution/bump of `status.Report`.

## Environment Availability

| Dependency | Required By | Available | Version | Fallback |
|------------|------------|-----------|---------|----------|
| podman | volume export/import, quiesce | ✓ | 5.8.2 | none (mandated) |
| `podman volume export`/`import` subcommands | BAK-01/BAK-02 | ✓ | present in 5.8.2 (verified) | none |
| `podman unshare` | cross-host chown repair (research flag only) | ✓ | present | documented manual step |
| SELinux | volume `:Z` relabel | ✓ Enforcing | — | volume recreated via Quadlet re-applies `:Z` |
| Go 1.26 stdlib (`archive/tar`, `crypto/sha256`) | all | ✓ | 1.26 | none |
| running OWUI container + populated volume | on-hardware quiesce/round-trip UAT | host-dependent | — | off-hardware tests use `fakeDeps`; on-hardware UAT validates real round-trip |

**Missing dependencies with no fallback:** None — all required tooling present on this host.
**Missing dependencies with fallback:** Cross-host UID/SELinux repair is best-effort with a documented `podman unshare chown` remediation (not a committed deliverable).

## Validation Architecture

### Test Framework
| Property | Value |
|----------|-------|
| Framework | Go standard `testing` (table-driven, `fakeDeps`, golden fixtures) — no third-party |
| Config file | none (go test) |
| Quick run command | `go test ./internal/backup/... ./cmd/villa/...` |
| Full suite command | `make check` (vet + `go test ./...`) |

### Phase Requirements → Test Map
| Req ID | Behavior | Test Type | Automated Command | File Exists? |
|--------|----------|-----------|-------------------|-------------|
| BAK-01 | backup assembles tar(manifest+config+owui-vol+usage+bench), excludes weights, records digests/checksums | unit | `go test ./internal/backup/ -run TestBuildArchive` | ❌ Wave 0 |
| BAK-01 | manifest digests sourced from seam (no literal) | unit + seam | `go test ./internal/inference/ -run TestSeamGrepGate` (must stay green) | ✅ exists |
| BAK-01 | sha256 per-entry compute/verify round-trips | unit | `go test ./internal/backup/ -run TestChecksum` | ❌ Wave 0 |
| BAK-02 | restore = capture→quiesce→swap→restart→prove→rollback; failure leaves stack intact | unit (fakeDeps drive flow, assert ordering + rollback) | `go test ./internal/backup/ -run TestRestore` | ❌ Wave 0 |
| BAK-02 | rollback honest complete/incomplete | unit | `go test ./internal/backup/ -run TestRestoreRollback` | ❌ Wave 0 |
| BAK-02 | import into clean-recreated volume (no merge of stale state) | on-hardware UAT | manual: backup → modify → restore → assert no stale chats | ❌ manual (research-flag) |
| BAK-02 | same-host volume round-trip integrity (OWUI starts healthy) | on-hardware UAT | manual round-trip on gfx1151 | ❌ manual |
| BAK-03 | skew WARN-and-confirm; corruption/incompatible-schema BLOCK | unit | `go test ./internal/backup/ -run TestSkew` | ❌ Wave 0 |
| BAK-03 | tar-slip guard refuses traversal entries | unit | `go test ./internal/backup/ -run TestTarSlip` | ❌ Wave 0 |
| D-13 | `--json` byte-frozen (if shipped) | golden | `go test ./cmd/villa/ -run TestBackupJSON` | ❌ Wave 0 (if shipped) |

### Sampling Rate
- **Per task commit:** `go test ./internal/backup/... ./cmd/villa/...`
- **Per wave merge:** `make check`
- **Phase gate:** `make check` green + on-hardware UAT (clean-recreate-no-merge + same-host round-trip + live-SQLite quiesce) before `/gsd-verify-work`.

### Wave 0 Gaps
- [ ] `internal/backup/backup_test.go` — covers BAK-01 (archive assembly, exclusion, manifest)
- [ ] `internal/backup/restore_test.go` — covers BAK-02 (transactional flow + rollback via fakeDeps)
- [ ] `internal/backup/manifest_test.go` — covers BAK-03 (skew compare classification)
- [ ] `internal/backup/checksum_test.go`, `tarutil_test.go` — sha256 + tar-slip guard
- [ ] `cmd/villa/backup_test.go`, `restore_test.go` — fakeDeps cmd-flow + exit codes + consent/`--yes`
- [ ] `cmd/villa/testdata/backup.json.golden` / `restore.json.golden` — IF `--json` ships (D-13)
- [ ] Framework install: none (stdlib `testing`)

## Security Domain

> `security_enforcement` not explicitly false — included.

### Applicable ASVS Categories
| ASVS Category | Applies | Standard Control |
|---------------|---------|-----------------|
| V2 Authentication | no | No new auth surface (local CLI; OWUI auth unchanged, persisted in restored volume) |
| V3 Session Management | no | — |
| V4 Access Control | yes | All written files `0600`, dirs `0700` (D-11); archive is local-only, owner-readable |
| V5 Input Validation | yes | Archive is untrusted input on restore: validate `manifest.schema_version`, verify SHA-256 (fail-closed), tar-slip guard every entry path (D-11); fixed-arg podman exec — no shell interpolation (volume/model names config/catalog-resolved) |
| V6 Cryptography | yes | SHA-256 (`crypto/sha256`) for integrity (not secrecy — encryption-at-rest is deferred); never hand-roll |
| V12 File & Resources | yes | Path-traversal guard on BOTH archive output (write) and extraction (read) — reuse `config.assertInsideDir` (D-11) |

### Known Threat Patterns for {Go CLI + tar archive + podman volumes}
| Pattern | STRIDE | Standard Mitigation |
|---------|--------|---------------------|
| Tar-slip (malicious archive entry escapes extraction dir) | Tampering / EoP | `assertInsideDir` per entry before write (D-11) |
| Archive corruption silently restored | Tampering | Per-entry SHA-256 verify, fail-closed BLOCK on mismatch (D-08) |
| Incompatible/future manifest schema mis-applied | Tampering | Fail-closed BLOCK on unreadable/newer `manifest.schema_version` (mirrors `usage.Load` future-fail-closed) |
| Shell injection via volume/model name | Tampering / EoP | Fixed-arg `exec.Command("podman", ...)` — no shell, names config/catalog-resolved (D-02) |
| Info disclosure via loose archive perms | Info Disclosure | `0600` archive, `0700` dirs (D-11) |
| Stale state leak through merge-import | Tampering (integrity) | Clean-recreate volume before import (verified merge behavior) |
| New outbound / telemetry | Info Disclosure | No new outbound; preserve `no_telemetry` posture (D-12) |
| Silent CPU fallback false-green on restore prove | Repudiation (false success) | Offload-asserting prove = FAIL → rollback (D-07) |

## Sources

### Primary (HIGH confidence)
- **Local `podman 5.8.2`** — empirical round-trip: `volume export -o`, `volume import` (no-auto-create error; merge-not-replace; tar-root layout), `volume inspect` mountpoint ownership, `getenforce`=Enforcing, `podman unshare`. [VERIFIED, 2026-06-07]
- In-tree code (read): `cmd/villa/uninstall.go` (`podmanVolumeRm`/`volumeRmArgs`), `internal/backendswap/backendswap.go` (transactional frame), `internal/config/villaconfig.go` (`SaveVilla`/`assertInsideDir`), `internal/orchestrate/openwebui.go` (`openWebUIImage` unexported; `:Z` mount), `internal/inference/backend.go`+`inference.go` (`Backend.Image()`), `internal/inference/seam_test.go` (`TestSeamGrepGate` regex), `internal/usage/usage.go` (`UsagePath`/`usageSchemaVersion`), `internal/benchstore/benchstore.go` (`benchReportsPath`/`savedReportSchemaVersion`), `cmd/villa/bench.go` (`benchStoreLocation`/`benchReportsStorePath`), `internal/status/status.go` (`no_telemetry`/`backend.Image()`), `cmd/villa/testdata/status.json.golden`.

### Secondary (MEDIUM confidence)
- docs.openwebui.com — backups tutorial (cold-backup recommended for integrity) + database-and-storage (webui.db under DATA_DIR / `/app/backend/data`). [CITED]
- redhat.com (rootless user-namespace modes), tutorialworks.com (rootless volumes) — `podman unshare chown` / `:U` for UID-map repair. [CITED]

### Tertiary (LOW confidence)
- None relied upon for load-bearing claims.

## Metadata

**Confidence breakdown:**
- Standard stack: HIGH — stdlib-only, all seams read in-tree.
- Podman mechanics (was MEDIUM/research-flag): HIGH — empirically verified against installed podman 5.8.2.
- SQLite quiesce: HIGH — OWUI docs + WAL reasoning concur with D-05.
- Cross-host repair: MEDIUM — documented mechanism (`podman unshare chown`) confirmed; honest known-limitation, not a committed deliverable (per scope).
- Architecture/patterns: HIGH — direct clone of proven in-tree seams (`uninstall`, `backendswap`, `config`).

**No conflicts found with CONTEXT.md locked decisions.** Two additive gaps surfaced that the plan must handle (not conflicts): (A1/OQ1) no villa version constant exists for `manifest.villa_version`; (A2/OQ2) the OWUI volume likely needs an explicit pre-create before import. Both are within D-07/D-09 scope.

**Research date:** 2026-06-07
**Valid until:** 2026-07-07 (stable — stdlib + podman v5 mechanics; re-verify if podman major version bumps)
