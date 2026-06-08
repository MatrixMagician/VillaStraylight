---
phase: 16-backup-restore
reviewed: 2026-06-07T00:00:00Z
depth: deep
files_reviewed: 27
files_reviewed_list:
  - cmd/villa/backup.go
  - cmd/villa/backup_test.go
  - cmd/villa/podman_volume.go
  - cmd/villa/podman_volume_test.go
  - cmd/villa/restore.go
  - cmd/villa/restore_test.go
  - cmd/villa/root.go
  - cmd/villa/version.go
  - internal/backup/backup.go
  - internal/backup/backup_test.go
  - internal/backup/checksum.go
  - internal/backup/checksum_test.go
  - internal/backup/deps.go
  - internal/backup/manifest.go
  - internal/backup/manifest_test.go
  - internal/backup/restore.go
  - internal/backup/restore_test.go
  - internal/backup/tarutil.go
  - internal/backup/tarutil_test.go
  - internal/benchstore/benchstore.go
  - internal/benchstore/benchstore_test.go
  - internal/config/villaconfig.go
  - internal/orchestrate/openwebui.go
  - internal/orchestrate/openwebui_test.go
  - internal/usage/usage.go
  - internal/usage/usage_test.go
  - Makefile
findings:
  critical: 1
  warning: 6
  info: 4
  total: 11
status: issues_found
---

# Phase 16: Code Review Report

**Reviewed:** 2026-06-07
**Depth:** deep
**Files Reviewed:** 27
**Status:** issues_found

## Summary

Reviewed the Phase 16 backup/restore implementation: the pure `internal/backup` core
(manifest, checksum, tar assembly/extraction with tar-slip guard, skew compare, and the
transactional `Restore` state-machine), the cmd-tier wiring (`backup.go`, `restore.go`,
the shared fixed-arg `podman_volume.go` seam, `version.go`), and the supporting
accessor additions in `usage`, `benchstore`, `orchestrate`, `config`, and the `Makefile`.

The architecture is strong and the project invariants are mostly honored:
- **Tar-slip guard is correct** — verified `..`, `../x`, embedded `a/../../x`, and absolute
  paths are all rejected (the leading-separator `filepath.Join` strip is pre-empted by an
  explicit `filepath.IsAbs` check). No bypass found.
- **Fail-closed corruption handling is correct** — checksum mismatch and
  unreadable/future `schema_version` both `Refused` with zero side effects, asserted by
  `hasMutate`-guarded tests.
- **Purity holds** — `internal/backup` imports no `os/exec`/podman/inference/detect; all
  host I/O is `Deps`-injected.
- **No shell interpolation** — `podmanVolume` uses fixed-arg `exec.Command`.
- **Seam discipline holds** — image literals reach the manifest only through
  `orchestrate.OpenWebUIImage()` / `inference.BackendFor(...).Image()`.

However, the **transactional rollback is not fully verbatim**: a restore that writes a
NEW data-dir artifact (one absent before the restore) and then fails at a later step
leaves that artifact on disk after "rollback" — a silent data leak that contradicts the
verbatim-restore contract (BAK-02). Plus a temp-dir/temp-file resource leak and several
robustness gaps detailed below.

## Critical Issues

### CR-01: Rollback does not delete data-dir artifacts the forward path newly created — non-verbatim rollback / data leak

**File:** `internal/backup/restore.go:204-215` (rollback closure) and `:245-254` (forward data writes)

**Issue:** The forward MUTATE writes `usage.json` / `bench-reports.jsonl` whenever the
archive carries them (`ex.usagePresent` / `ex.benchPresent`). The rollback only restores
the *captured prior* artifacts, and only when they were captured:

```go
if priorUsageOK {
    add(d.WriteFileAtomic(in.UsageDestPath, priorUsage), "restore usage.json")
}
if priorBenchOK {
    add(d.WriteFileAtomic(in.BenchDestPath, priorBench), "restore bench-reports.jsonl")
}
```

When the current install has **no** prior `usage.json` (`priorUsageOK == false`) but the
archive **does** carry one (`ex.usagePresent == true`), the forward path writes the new
`usage.json` at `:246`, and then a *later* step fails (e.g. `VolumeImport` at `:260`, or a
non-pass `Prove` at `:271`). Rollback runs, but because `priorUsageOK` is false it does
**nothing** for that path — the restored-from-archive `usage.json` remains on disk. The
result is reported as `RolledBack` ("prior stack restored"), but the prior data-dir state
was NOT restored. Same bug for `bench-reports.jsonl`.

This violates the BAK-02 verbatim-rollback contract and leaks restored data (chat/usage
counters from the backup) into a "rolled back" install. It is fully reachable and not
covered by `restore_test.go` (the existing rollback tests use archives with no usage/bench
entries).

**Fix:** Capture the *prior-existence* of each data-dir path and, on rollback, delete a
file the forward path created where none existed before. Add a `Deps.RemoveFile func(path
string) error` seam (or reuse an existing remove seam) and track existence:

```go
priorUsage, priorUsageOK := captureFile(d, in.UsageDestPath)
priorBench, priorBenchOK := captureFile(d, in.BenchDestPath)
// ... in rollback():
switch {
case priorUsageOK:
    add(d.WriteFileAtomic(in.UsageDestPath, priorUsage), "restore usage.json")
case ex.usagePresent: // forward created it; prior had none → remove to restore verbatim
    add(d.RemoveFile(in.UsageDestPath), "remove restored usage.json")
}
// (symmetric for bench)
```

Add a regression test: prior has no usage.json, archive carries one, force a post-write
failure (e.g. `volumeImportErr`), and assert the usage path is removed on rollback.

## Warnings

### WR-01: Restore temp dir is never cleaned up — resource + data leak in /tmp

**File:** `cmd/villa/restore.go:152-157`

**Issue:** `liveRestore` creates a temp dir via `os.MkdirTemp("", "villa-restore-*")` for
the extracted (`restore-owui.tar`) and rollback (`rollback-owui.tar`) volume tars, but
there is no `defer os.RemoveAll(tmpDir)` anywhere. Every `villa restore` invocation leaves
a `villa-restore-*` dir in `/tmp` containing the captured Open WebUI volume tar — which
holds the user's chat database (`webui.db`). Over time this accumulates and, worse, leaks
private chat data into a world-readable `/tmp` parent (the tars are written via
`podman volume export`, whose perms are not controlled by this code path). `backup.go`
correctly defers cleanup of its temp file (`:137`); restore does not.

**Fix:** Defer cleanup. Because `liveRestore` returns before `runRestore` runs the flow,
the cleanup must be tied to the command lifecycle — e.g. return the temp dir and defer its
removal in the `RunE` wrapper, or move the `MkdirTemp`+`defer os.RemoveAll` into a single
function that wraps the whole `Restore` call:

```go
tmpDir, err := os.MkdirTemp("", "villa-restore-*")
// ...
defer os.RemoveAll(tmpDir) // must survive until after backup.Restore returns
```

### WR-02: Duplicate archive entry names silently last-write-win — verify can be bypassed

**File:** `internal/backup/restore.go:314-326` (`collect[name] = data`)

**Issue:** `readAndVerify` collects entries into `collect map[string][]byte` keyed by name.
A malicious or malformed archive containing two entries with the same name (e.g. two
`config.toml` members) silently keeps only the **last** one — `collect[name] = data`
overwrites. The SHA-256 verify then runs against whichever copy survived. While the
manifest checksum still gates the *surviving* bytes, an attacker who controls archive
layout can place a benign first `config.toml` (matching the manifest checksum is not
required for the discarded copy) and a second one; the behavior is order-dependent and not
what the manifest describes. More concretely, there is no check that the archive contains
*exactly* the manifest-listed entries — extra/unexpected entries are silently accepted and
ignored (only manifest-listed names are verified at `:347-355`).

**Fix:** Reject duplicate entry names explicitly, and reject entries not listed in the
manifest (or at least assert the collected key set equals the manifest set):

```go
if _, dup := collect[name]; dup {
    return fmt.Errorf("archive contains duplicate entry %q", name)
}
collect[name] = data
// ... after collection: every collected non-manifest name must appear in want{}
```

### WR-03: `manifest.json` is not pinned as the FIRST entry on read — out-of-order manifest tolerated

**File:** `internal/backup/restore.go:314-332`

**Issue:** The doc comments and `writeArchive` invariant require `manifest.json` to be the
FIRST tar member so a reader can parse the manifest before trusting the rest. But
`readAndVerify` accepts the manifest at *any* position (it just sets `manifestSeen` whenever
it encounters `EntryManifest`). An archive that places data entries before the manifest is
fully buffered into `collect` (each entry's bytes read via `io.ReadAll`) before the manifest
is even seen. This weakens the "parse manifest first" defense and means a crafted archive
can force the reader to buffer arbitrary entries before any schema/version gate applies.
(The bytes are bounded only by the absence of a size cap — see WR-04.)

**Fix:** Enforce manifest-first on read: if the first `tr.Next()` entry name is not
`EntryManifest`, refuse. Parse and schema-gate the manifest before reading any further
entry body.

### WR-04: No archive size / entry-count bound — unbounded memory on a hostile or huge archive

**File:** `internal/backup/restore.go:297-376`, `internal/backup/tarutil.go:69-94`

**Issue:** `readArchive` does `io.ReadAll(tr)` per entry into memory and `readAndVerify`
holds every entry in `collect`, then `Restore` holds the whole `extracted` payload. The
package comment ("the whole archive is small — model weights are excluded") is an
assumption, not an enforced bound. A corrupt/hostile `.tar` (or simply an Open WebUI volume
that has grown large) is read entirely into RAM with no per-entry size cap and no
entry-count cap. `benchstore.Load` correctly bounds its scanner at 1 MiB/line (T-14-03);
the tar reader has no equivalent. Path traversal is guarded; size is not. This is a
robustness/DoS gap, not pure performance (an OOM is a correctness/availability failure).

**Fix:** Add a per-entry and/or total size cap using `io.LimitReader` around `tr`, and an
entry-count cap, refusing an archive that exceeds them with an actionable error.

### WR-05: `WriteFileAtomic` traversal guard is a no-op for restore's data-dir writes (false sense of protection)

**File:** `internal/usage/usage.go:255-259`, used at `cmd/villa/restore.go:293`

**Issue:** `usage.WriteFileAtomic(path, data)` computes `dir := filepath.Dir(path)` and then
`assertInsideDir(path, dir)` — a path is *always* inside its own parent dir, so the guard
can never fire for any input. For the restore data-dir writes the destination paths
(`usage.UsagePath()`, `benchReportsStorePath()`) are internally resolved, so this is not
currently exploitable, but the guard provides no actual protection and the comment claims it
"guards against traversal." If a future caller ever passes an attacker-influenced path, the
guard will silently pass it. The real traversal guard for these paths lives elsewhere
(`UsagePath` is fixed); the in-function guard is misleading dead protection.

**Fix:** Either remove the misleading self-guard and document that the caller owns path
trust, or guard `path` against a *fixed store-root* (e.g. the resolved `$XDG_DATA_HOME/villa`
root) rather than `filepath.Dir(path)`, so a `..`-bearing path is actually rejected.

### WR-06: ROCm preflight in prove ignores WARN/BLOCK distinction and only fails on `StatusFail`

**File:** `cmd/villa/restore.go:311-321`

**Issue:** `liveRestoreProve` iterates `preflight.RunROCmForImage(...)` and only treats
`preflight.StatusFail` as a prove failure. The project's preflight model is a BLOCK/WARN
tier gate; the code checks `StatusFail` but the prove gate should fail-closed on the
**BLOCK** tier specifically. If the preflight enum distinguishes BLOCK from FAIL (the
CLAUDE.md describes "BLOCK/WARN tiers"), a BLOCK result that is not also `StatusFail` would
slip through the cutover prove and a restore could be reported as proven on a host that
preflight would block. Confirm `StatusFail` is exactly the BLOCK tier; if BLOCK is a
distinct status, this is a false-green hole in the offload-honest prove (D-07).

**Fix:** Match on the BLOCK tier explicitly (the same predicate `villa preflight` uses to
gate an install), not a hand-picked `StatusFail`. If `StatusFail` *is* the canonical block
tier, add a comment citing the enum so the intent is auditable.

## Info

### IN-01: Backup leaves Open WebUI service stopped if the deferred restart fails

**File:** `internal/backup/backup.go:108`

**Issue:** `defer func() { _ = d.Start(d.OpenWebUIServiceName) }()` swallows the restart
error. If the post-backup restart fails, the user is left with Open WebUI down and `villa
backup` exits 0 with no warning. Best-effort restart is reasonable, but a silent failure to
bring the service back is worth surfacing.

**Fix:** Capture the deferred `Start` error into the `Result`/a stderr warning so a failed
restart is reported (e.g. "backup written, but failed to restart villa-openwebui — run
`villa up`").

### IN-02: `excludedModelIdentities` hardcodes `Source: "catalog"` regardless of actual source

**File:** `cmd/villa/backup.go:240`

**Issue:** The excluded-model record always sets `Source: "catalog"`, but a model could have
been a local/external weight. The manifest then misreports the re-pull source. Minor, since
the field is informational, but it can mislead a re-pull.

**Fix:** Resolve the real source (catalog vs local override) or document that only
catalog-sourced models are recorded.

### IN-03: `version` package var is mutated only via ldflags but is exported-shaped for test races

**File:** `cmd/villa/version.go:13`

**Issue:** `version` is a package-level mutable `var` (required for `-X` linker stamping).
That is the correct idiom, but tests that set `version` (if any are added later) would race
with parallel tests reading `villaVersion()`. Not a current bug; flag for awareness if test
mutation is introduced.

**Fix:** Keep ldflags-only mutation; if tests ever need to override, route through a
helper that is not used concurrently.

### IN-04: Skew compare treats config schema as constant 0 — config skew never detected

**File:** `cmd/villa/backup.go:145`, `cmd/villa/restore.go:147`, `internal/backup/backup.go:258`

**Issue:** Both backup and restore hardcode `ConfigSchemaVersion: 0` ("VillaConfig carries
no schema_version field"). `blockOnNewerStore`/`warnOnOlderStore` treat `<= 0` as "not
recorded" and skip, so config-schema skew is structurally undetectable. This is consistent
and documented, but means a future incompatible `config.toml` shape would restore silently
with no skew warning. Acceptable for now given VillaConfig has no version field; worth a
tracking note so a future config schema bump remembers to wire this.

**Fix:** When/if `VillaConfig` gains a `schema_version`, wire it through both sites so the
existing block/warn machinery activates.

---

_Reviewed: 2026-06-07_
_Reviewer: Claude (gsd-code-reviewer)_
_Depth: deep_
