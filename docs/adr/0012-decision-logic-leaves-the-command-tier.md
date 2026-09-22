---
status: accepted
---

# Decision logic leaves the command tier: backup, recall, uninstall

A structure review at `81eacec` (lizard CCN over all 215 non-test Go files)
found the three next-most-complex functions in the tree after the ones
ADR-0005 already moved: `runBackup` (CCN 37 over 180 NLOC), `runRecallIndex`
(CCN 34 over 131 NLOC), and `runUninstall` (CCN 20 over 92 NLOC). All three
were cobra `RunE` bodies holding real decisions — a stage/rename sequence and
archive entry selection, two pre-mutation refusal guards and a rebuild/stamp
decision, and a whole ordered teardown plus the model keep/remove choice —
which breaks the project's thin-caller rule (parse, call, render). This ADR
completes that move for #239, #240 and #241.

## Decision

- **`internal/backup` gains `RunBackup(Deps, Input) (Result, error)`**,
  wrapping the existing pure `Backup` with the stage→assemble→publish
  sequence: the output-path traversal guard, the three same-directory temp
  files (the archive itself, the OpenWebUI volume export, and the optional
  qdrant volume export), and the atomic rename onto the destination only after
  a fully successful write. `Deps` gains `CreateTemp`/`Rename`/`Remove`, wired
  to `os.CreateTemp`/`os.Rename`/`os.Remove` in `cmd/villa`. `runBackup` keeps
  the Input-gathering (the subsystem-gated decision of WHICH optional entries
  to include — still live-host-derived: config, seam-sourced image digests, a
  podman volume-exists probe — so it cannot move into a pure core) and the
  post-success reporting (thin-caller "render").

- **`internal/recall` gains `PrepareIndexRun(IndexRunInput) IndexRunPlan`**: a
  pure function holding BOTH pre-mutation guards `runRecallIndex` used to
  inline — the single-operator shared-recall disclosure guard and the
  embedding-skew refusal — plus the rebuild/stamp decision (clear `Chats` on
  rebuild, stamp the embedding identity and `LastIndexStartedAt`, decide
  whether the Knowledge collection itself needs an id-preserving reset).
  `runRecallIndex` still drives the Open WebUI HTTP calls in the order their
  results demand (listing users before counting them, ensuring the KB after a
  plan says to), but it decides nothing: it calls `PrepareIndexRun` once,
  refuses on `Refused != ""`, and otherwise executes exactly what the plan
  says.

- **`internal/uninstall` is new**: `Run(Deps, Opts, Input) Result` holds the
  whole ordered teardown (dashboard service, coding-agent addon, service stop
  in reverse-of-start order, unit-file removal, daemon-reload, non-model
  volume removal, the optional model wipe, disable-linger) plus the pure
  `ResolveModelChoiceSource` decision (flag wins; interactive prompts;
  non-interactive defaults to keep) and `NonModelVolumes`. `Deps` is the same
  seam set `cmd/villa`'s `uninstallDeps` already declared, now also the
  core's real Deps type; `uninstallDeps` stays in `cmd/villa` (its lowercase
  fields are load-bearing for `uninstall_test.go`'s existing construction) and
  gains a `toCore()` translation. `runUninstall` in `cmd/villa` now only
  parses flags, rejects the mutually-exclusive pair, derives the rendered
  stack and stop order (via the existing `serviceUnits` helper, which cannot
  move — it is shared by five other verbs and importing `cmd/villa` back from
  an `internal` package is not legal Go), calls `uninstall.Run`, and renders
  `Result.Lines`/`Result.Err`.

## Rejected

**Move backup's reporting (the ten `switch`/`if` blocks narrating which
optional entries made it into the archive) into the core too.** Rejected:
those lines only restate decisions the CALLER already made from subsystem
gates — moving them would duplicate the gate results into `Result` just to
print them back, and the thin-caller rule explicitly keeps "render" in the
command tier.

**Give `internal/recall` an `Emit`-narration seam like `internal/install`
(ADR-0005).** Rejected for this ticket: recall's HTTP calls dominate its
wall-clock, not its decisions, so accumulating lines after the fact (the
`update`/`backup` shape) would not have install's stale-progress problem —
but this ticket only moves two guards and a stamp decision, not the whole
pipeline, so no narration seam is needed yet.

**Give `internal/uninstall.Deps` exported names that alias `cmd/villa`'s
existing lowercase `uninstallDeps` fields via a type alias.** Rejected: Go
field names in a struct literal must match exactly, and `uninstall_test.go`
constructs `uninstallDeps` with unexported field names — an exported-field
core type cannot share an identity with it. `toCore()` is the smaller,
explicit translation instead of forcing the test file to change (out of
scope: "existing cmd tests must pass unchanged").

**Fold `uninstall`'s `Deps` into `install.Deps`.** Rejected: install and
uninstall never run in the same process invocation, and install's `Deps`
already carries fields uninstall never reads (mirrors the backup/restore
`Deps`/`RestoreDeps` split already recorded in `internal/backup/deps.go`).

## Consequences

Lizard CCN (`lizard -l go`, before → after):

| Function | Before | After |
|---|---|---|
| `runBackup` | 37 (180 NLOC) | 29 (132 NLOC) |
| `runRecallIndex` | 34 (131 NLOC) | 29 (126 NLOC) |
| `runUninstall` | 20 (92 NLOC) | 7 (32 NLOC) |

`internal/uninstall` imports `orchestrate` for `Unit`-shaped seams, the same
as `internal/backup` already does; neither package imports `inference` or any
image/backend literal, so `TestSeamGrepGate` stays green. `cmd/villa/*_test.go`
is unchanged except for three additive lines in `backup_test.go`'s
`fakeRunDeps` (the new `CreateTemp`/`Rename`/`Remove` fields `RunBackup`
needs) — `git diff main -- 'cmd/villa/*_test.go'` is additions-only. The three
new core packages/files get their own tests against fake `Deps`: table tests
for `PrepareIndexRun` and `ResolveModelChoiceSource`, and call-order/ordering
tests for `RunBackup` and `uninstall.Run`.
