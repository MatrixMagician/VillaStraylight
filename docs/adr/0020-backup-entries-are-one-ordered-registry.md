---
status: accepted
---

# Backup entries are one ordered registry, and eval baselines are its next row

`villa backup` and `villa restore` archive nine members. Each one was spelled out
at every site that handles it: an `Input` field, a `RestoreInput` field, a
`Result` field, a branch in `Backup`, a branch in `readAndVerify`, a capture, a
forward write and a rollback case in `Restore`, and a gate plus a narration
switch in the cmd tier. Adding one entry touched about six places in two
packages, and the functions that held them (`Backup`, `Restore`, `readAndVerify`,
`runBackup`, `runRestore`, `liveRestore`) scored CRAP 20 to 200. The eval
baselines store (ADR-0018) is meant to be a backup entry (#275), and a baseline
cannot be re-recorded after the regression it exists to catch, so the entry has to
land without a seventh copy of the same branch.

## Decision

- **One ordered registry in `internal/backup`.** `registry` is a slice of
  `Row{Name, Label, Kind, Required}` in the order members are written to the
  tar. `Kind` is `KindVolume`, `KindConfig` or `KindFile`. The file rows
  (usage.json, bench-reports.jsonl, recall-state.json, crush.json,
  searxng-settings.yml, and now eval-baselines.json) are loops in `Backup`,
  `readAndVerify` and the capture, forward-write and rollback steps of `Restore`.
  `Label` is the name an error message uses ("restore settings.yml"), which is not
  always the entry name.

- **The cmd tier supplies paths by name.** `Input.Sources` and
  `RestoreInput.Dests` are `map[string]string` keyed by entry name. A missing or
  empty path means the entry is not offered (backup) or not applied (restore);
  `liveBackupSources` and `liveRestoreDests` are the only places the subsystem
  gates (`MemoryOn`, `AgentOn`, `WebSearchOn`) are read for the archive. Narration
  stays cmd-side, keyed by entry name (ADR-0012). `Result.Files` carries one
  `FileOutcome{Restored, Skipped}` per file entry the archive carried, so a new
  row needs no new result field.

- **The two volumes and config.toml stay explicit code.** Quiesce, the tri-state
  Unknown refusal on the qdrant volume, and clean-recreate-before-import are
  different in kind from writing a file, and an abstraction over both would hide
  the ordering that makes restore safe. The registry orders them; it does not
  hide them.

- **Eval baselines are a file row, with a named schema field.** The row's source
  and destination are both `evalstore.Path()`, it has no subsystem gate, and an
  absent file is skipped like any optional entry. `Manifest`, `ManifestInput`,
  `Input` and `CurrentInstall` gain `EvalSchemaVersion`, and `CompareSkew` blocks
  on a newer archive schema and warns on an older one, as for the sibling stores.

- **The manifest schema goes 4 to 5.** A v4 villa verifies every member the
  manifest lists and then silently drops one it does not know, so a v5 archive
  restored by a v4 villa would report success without applying the baselines. The
  bump makes it fail closed instead. v1 to v4 archives stay restorable.

- **Restore replaces eval baselines verbatim, and warns.** Restore is a
  replacement of the whole document, so any baseline recorded after the backup is
  lost, and ADR-0018 says a baseline cannot be re-recorded. The cmd tier hands
  the core `EvalKeysOf`, a function that names the baselines a document holds
  (read with `evalstore`, so `internal/backup` still imports nothing from `eval`
  or `evalstore`). The core applies it to the archive's document and to the copy
  of the current file it captured before mutating, so the comparison is against
  the state the restore actually replaced. On success `Result.EvalDropped` names
  each current baseline the archive lacks and the cmd tier prints a WARN for it.
  There is no new prompt: the replacement is what restore is for, and a failed
  proof rewrites the captured file byte for byte.

## Rejected

**An `Entry` interface with `Capture`, `Apply` and `Rollback`.** It shrinks
`Restore` the most, but it is an interface with three bespoke implementations, and
reproducing the exact interleaving of stop, save, write, import and start that the
ordered seam-call log in `restore_test.go` pins is where it would go wrong.

**A generic `StoreSchemas map[string]int` in the manifest.** It would change the
manifest contract during a refactor that must be byte-identical, and the legacy
named fields would have to stay beside it, leaving two schema mechanisms. Eval
takes a named field like the others.

## Consequences

- A new file entry is one `registry` row, one `Sources` and `Dests` key in the two
  cmd helpers, and a manifest schema field if it has its own schema.
- A usage, bench or recall entry restored with no destination is now reported as
  skipped instead of failing a write to the empty path. The live wiring never
  produced an empty destination for them, so no existing behavior moves.
- A baseline recorded after a backup is lost on restore, with a WARN naming it.
  An operator who wants it kept merges by hand before restoring.
- The refactor commit is byte-identical on the wire: the archive layout, the
  manifest and the seam-call order are pinned by `TestBackupAgentOffIsLayoutIdentical`,
  `TestManifestJSONRoundTrip`, `TestRestoreV1ManifestStillRestores` and the
  recorder's call log, none of which changed.
