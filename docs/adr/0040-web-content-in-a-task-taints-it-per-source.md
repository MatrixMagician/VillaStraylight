---
status: proposed
---

# Web content in a task taints it, per source

A task reads workspace files and the approval table answers from the action's class
alone, so what the model has read never changes an answer. Under `--auto`, a write is
allowed and `python3 script.py` on a workspace path is allowed (spec §3.3). A file or
a page that carries a prompt injection can steer the model into a write or a script
the operator never sees, and the table cannot tell. Today no task has a web route, so
this is the file-borne case only (`docs/THREAT_MODEL.md` known gap 7, #404). The first
web route is the research task (#408), and it should not ship into a table that has no
answer for it.

## Options

- **No taint.** Keep the table as it is and rely on the sandbox bounding the damage to
  one workspace. Cheapest, and a web-reading task would run `--auto` writes and allowlisted
  scripts exactly as an offline one does. It leaves the one source villa knows to be
  attacker-reachable (a page anyone can publish) with no extra gate.
- **Per-source taint.** A source that villa knows to be outside the operator's control
  sets a taint on the task. A tainted task moves its write and execute answers from
  `allow` to `ask`. Workspace files do not set it.
- **Taint on any external read.** Workspace files taint too. A task reads files in
  every run, so every task would be tainted from its first read, `--auto` would ask on
  every write and the mode would mean nothing. The distinction between a file the
  operator put there and a page a stranger wrote is the only thing that makes the taint
  informative.

## Decision

Per-source taint.

- **A taint source is a closed, enumerated type.** `approval.Taint`, a string type whose
  zero value `Untainted` is no taint and whose only source is `TaintWeb` (`"web"`). A
  new source is a new constant, an ADR amendment and a case in the tests, never a free
  string. `Valid` rejects anything else, and the store's setter refuses it.
- **The decision takes the taint as a third input.** `approval.Decide(mode, taint, req)`.
  Untainted, every cell answers exactly as before. Tainted, an `Allow` for a `write` or
  `execute` action becomes `Ask`, under both modes and including an allowlisted or
  safe-listed command. `Deny` stays `Deny`, deletion already asks in every mode, and
  `read` and `list` are unchanged. No cell becomes more permissive. A stored taint this
  build does not recognise is treated as tainted: the table fails closed.
- **The session grant does not cover a tainted ask.** `allow_session` (`a`, "for the rest
  of this task") is an answer given before the content that tainted the task was read, so
  once tainted, a write or execute asks again. Deletion already behaves this way.
- **It is one-way for the life of the task.** The task record carries `taint` (omitted
  when untainted), and `taskstore.Store.SetTaint` sets it once. A second call, from the
  same or another source, is a no-op: the first source stays on the record. Nothing
  clears it. The first time it is set, one narration line naming the source is appended
  to the task's log, the only transcript that survives the sandbox.
- **Nothing sets it yet.** The only call site that will, `taskrun.Runner.Taint`, has no
  caller until #408. The tool server (#360 to #363) stays read-only.
- **Store skew.** A task record is not a `villa backup` entry (ADR-0020), so no skew rule
  applies to it. The field is `omitempty` and added above `schema`, so a record written
  before it loads as untainted and an untainted record serialises to the same bytes as
  before. The schema version stays 1: nothing a v1 reader depends on changed.

## Consequences

- A tainted `--auto` task asks for every write and every script. That is the cost, and the
  operator pays it only for tasks that read a source villa could name.
- **It is a pattern, not a proof**, in the same sense as the deletion rule. The taint is
  set where villa knows content came from the web; content that reaches the model another
  way is not covered, and a model already steered can still ask for a harmful action that
  the operator then approves.
- **Residual: a workspace file can still carry an injection.** Files do not taint, by the
  decision above, so a task that reads a poisoned file keeps its `--auto` writes and
  allowlisted scripts. The sandbox and the workspace grant bound what that reaches.
- The first source arrives with #408, which must set the taint before the first web
  content reaches the model, not after the response that used it.
