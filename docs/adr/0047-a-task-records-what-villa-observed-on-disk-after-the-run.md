---
status: accepted
---

# A task records what villa observed on disk after the run

A task record's `files` list is the harness's word. `taskrun` appends one entry
per Crush `file` event, and nothing re-reads the path afterwards. A record that
says `created actions.md` is an acknowledgement from the executor, not a fact
villa saw. The grounding audit already checks a document's claims against the
files the session read. Nothing checked that the files it wrote are there (#437).

## Decision

- **One observation per distinct reported path.** After `run_complete` and
  before the grounding audit, the runner reads each distinct `files` path once,
  through the same `ReadFile` seam the audit used, and appends a
  `taskstore.Observation`: the path, a state, and `observed_at`. The list is in
  first-reported order.

- **Three states, one of them Unknown.** `present` carries `size` and `sha256`
  of the bytes read. `absent` is a read that failed with `fs.ErrNotExist`, and
  nothing else. `unobserved` is any other failure (permission, a symlink or
  non-regular leaf, the read limit, I/O) and carries the error text. An
  unobserved path is never counted as present and never as absent.

- **Absence flags.** A reported path that is `absent` is a confident negative.
  The task ends `flagged`, and the reason names every absent path. An absent path
  has no document, so the audit skips it: it gets no grounding entry, and
  `grounding.checked` stays a statement about the documents that exist.

- **The audit reads the observed bytes.** The audit is handed the bytes the
  observation hashed and does not read the disk again. The audited document and
  the hash in the record are the same bytes. An unobserved path goes to the audit
  as an error and flags as an audit that could not run, the behavior it had
  before.

- **A dropped history record is not a write.** Crush's history service
  publishes a `file` event under the pubsub verb `created` for each version it
  records, and under `deleted` only when it drops a record (`history.Delete`,
  reached from session cleanup). `crushapi.FileEvent.Op` now carries that verb.
  The runner narrates a `deleted` file event and records no file event for it, so
  it can never flag as absent.

- **The record contract grows append-only.** `observations` is appended before
  `schema`, and the record schema goes from 1 to 2. `writeRecord` stamps the
  current schema and writes `observations: []` rather than `null` on every
  write, so a pre-upgrade record that `Recover` rewrites says what shape its
  bytes follow. The list view did not change, so it keeps its own schema, 1.

- **`villa task show` prints the observation on each file line.**
  `created summary.md: present, 412 bytes, sha256 <hex>`, or
  `unobserved (<error>)`, or `absent after the run`. A record with no observation
  for a path prints the file event alone.

## Rejected

**Fold the observation into each `files` entry.** The file list is one entry per
harness event, so a path written twice has two entries and would carry two
copies of one read. It would also change the shape of an existing field rather
than append one.

**Count an unreadable path as absent.** A permission error or a refused symlink
says nothing about whether the file is there. Treating it as a negative is the
false verdict typed-Unknown exists to prevent.

**Treat a path the task later deleted as expected.** Crush has no delete tool, so
a deletion is a bash command, and its approval row is a pattern, not a proof of
which path went. A file the task wrote and then removed is absent after the run,
and the record says so. The operator reads the approvals beside it.

## Consequences

- A task whose reported file is missing already ended `flagged`, because the
  audit could not read the document. It now flags for the absence itself, with
  `grounding.checked` still true, and the reason names the path.
- A task that writes a scratch file and removes it flags. That is the honest
  account of the end state, and the reason and the approvals show why.
- A reader of `--json` that checks `schema` sees 2 on every record written from
  now on, old ones rewritten by recovery included.
- Wiring each approved tool call's claim to its observation is out of scope. It
  is a follow-up if this one proves useful.
