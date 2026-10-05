---
status: accepted
---

# Quality is checked on command, against a recorded baseline

Villa proves that a model is loaded, offloaded and answering. It does not prove
that the model still answers well. The cutover proof's generation probe asserts
only that the reply contains "ok" (`internal/inference/probe.go`), the tools-mode
proof asserts one `crush run` round-trip, and `bench` records throughput and never
response content. A pin move, a backend swap or a speculation change can therefore
pass every gate while the same model answers worse: a broken chat template, a
quantisation regression in a rebuilt image, a draft head that changes greedy output.
The idea comes from antirez/ds4's `ds4-eval`, which runs embedded capability cases
against a real GGUF before each release. This ADR records how villa adopts it.

## Decision

- **`villa eval` is its own verb, run on command only.** It runs an embedded suite
  of capability cases against the served model and compares the per-case result
  with a recorded baseline. `villa eval --record` accepts the current results as
  the baseline. `--json` is a new frozen contract at schema 1. Nothing runs it on a
  timer, from `status`, from `doctor`, or from a swap, which keeps "checks are
  strictly on-command" unqualified. Its natural moments are the hardware re-vet in
  `docs/RELEASING.md` and before and after a swap or update.

- **The core is pure; cases and graders are data.** `internal/eval` owns `Case`
  (id, prompt, grader, token bound), a grader table keyed by kind, and `Compare`.
  The suite is villa-authored, embedded with `go:embed`, and carries a suite
  version that changes whenever a case is added, removed or edited; the sha256 of
  the embedded file is pinned beside the version, so an edit fails the build until
  the version moves with it. Five grader kinds ship. `exact` matches after trimming
  and case-folding. `regex` matches anywhere in the reply. `json` parses the reply,
  optionally inside one ```json fence, as one JSON object and checks required
  key/value pairs. `tool` wants exactly one tool call, of the named function, whose
  arguments parse as a JSON object holding the required key/value pairs. `no_tool`
  wants no tool call and an answer matching a pattern. Each row also names what a
  case of its kind must carry, so the loader refuses an unknown kind, an unknown
  field or an incomplete case. A new kind is a table row, never a branch at a call
  site.

- **Tool-call cases ship in the first suite, gated on tools mode.** A case may
  carry tool definitions in the OpenAI wire shape. llama-server honours them only
  when it runs with `--jinja`, which villa renders only when `subsystem.ToolsOn`
  (tools mode or coding mode). With tools mode off a tool-call case is **skipped**,
  a fourth result status beside passed, failed and unconducted: it is neither a
  failure nor a hole. `Compare` compares only cases conducted on both sides; a case
  skipped in the run or in the baseline is listed, never a regression, and the
  tools-mode change is named as a provenance difference.

- **Every completion is greedy and goes through the inference client.** Each case
  is sent with temperature 0, thinking disabled through `chat_template_kwargs` (the
  grounding audit's setting), and a bounded `max_tokens`. It is sent through
  `inferenceClient(cfg).Chat(…)` as one non-streamed completion (ADR-0014):
  `llm.ChatRequest` gained append-only `Tools` and `ToolChoice` fields, and
  `OpenAIClient.Chat` returns the message content and every tool call. `Complete`
  is not used because it discards the content, and `StreamChat` is not used because
  its parser reads content deltas only and would lose a tool call. Cases run one at
  a time, so a parallel slot cannot change a reply's batching.

- **A baseline belongs to a model, not to a stack.** It is keyed by model, quant
  and suite version. Backend, image digest, speculation mode, context and tools
  mode are recorded beside it as provenance. Keying on them would mean a pin move
  never finds a baseline, and a pin move is exactly the change being checked. The
  report names each provenance field that differs from the baseline's.

- **The verdict reuses the verify family's statuses and exit contract.**
  `verify.Status` and `verify.ExitCode` decide the outcome, so `eval` cannot drift
  onto its own codes. A case that passed in the baseline and fails now is a
  regression, and any regression is **Fail**; the report lists each one with an
  excerpt of the reply so the operator can judge it. No baseline, or a case that
  could not be conducted (transport error, timeout, inference down), is **Reject**,
  never Pass: a comparison that did not happen is not evidence. Otherwise the
  verdict is **Pass**, with the cases that newly pass reported as improvements. A
  confirmed regression outranks a Reject elsewhere in the same run.

- **`--record` refuses a baseline with holes.** If any case could not be conducted,
  nothing is written. A recorded baseline holds every case's pass or fail; a
  skipped tool-call case is not a hole. Recording over an existing baseline is
  allowed and prints the score it replaces.

- **Baselines persist in their own store.** `internal/evalstore` sits on
  `internal/jsonstore` at `eval-baselines.json` under villa's XDG data root,
  schema-versioned, one document holding every model's baseline. It is a backup
  entry, because a baseline records a known-good state that cannot be re-recorded
  after the regression it exists to catch. #275 made the backup and restore entries
  table-driven (ADR-0020) and added `eval-baselines.json` as a row, so `villa
  backup` archives it, the manifest records its schema (backup schema 5), and
  `villa restore` replaces it verbatim and warns about every baseline the archive
  lacks. Recording refuses to overwrite a store it cannot read, so one
  model's baseline never silently replaces every other model's.

- **`eval` takes no stack lock.** It mutates no unit and no config, like `status`. A
  swap that runs during an eval shows up as cases that could not be conducted, or
  as provenance that changed mid-run, never as a silent pass.

## Rejected

**Extend `villa bench` with a quality score.** It would reuse bench's fingerprint
and store, and `bench --ab` would compare quality across backends for free. But it
bumps `SavedReport` from v2 to v3 on a frozen contract, puts response-derived
results into a store that is numbers-only by design, and makes every quality check
pay for a throughput run.

**Gate swaps and updates on the suite.** Running it after the cutover proof and
warning on a drop puts the suite's runtime on every swap. ROCm and Vulkan numerics
differ even at temperature 0, so a borderline case flips across a backend swap and
the warning becomes noise the operator learns to ignore. It would also put a
second verdict inside the swap transaction (ADR-0015). If wanted later, it can be
an opt-in flag that reads the baseline this ADR introduces.

**Port ds4-eval's cases.** Its suites target DeepSeek and GLM models far larger
than villa's catalog, and its data carries third-party licences. Villa's cases are
written for the catalog's sizes, under the repo's licence.

**Grade with the model itself.** A model judging its own answers drifts with the
model under test, which is the thing being measured. Graders are deterministic.

**Defer tool-call cases.** An earlier draft of this ADR rejected them because
`llm.ChatRequest` had no `tools` field and the tools-mode proof already drives one
real tool call. That proof shows one round-trip works; it says nothing about whether
the model still picks the right tool with the right arguments after a pin move,
which is the regression this verb exists to catch. The request fields and the `tool`
grader were added instead (see the decision above).

## Consequences

- A pin re-vet gains a quality step: `villa eval` on the old pin, `villa eval` on
  the new one, and the per-case diff in the PR.
- Small catalog models fail many cases. That is expected: the baseline is per
  model, so only a change against the model's own record counts.
- Editing a case orphans every baseline recorded under the old suite version. The
  report says so (Reject: no baseline for this suite version) rather than comparing
  across suites.
- A baseline recorded with tools mode off holds no tool-call results, so those
  cases are never compared until a baseline is recorded with tools mode on.
- A restore replaces `eval-baselines.json` with the archive's copy, so a baseline
  recorded after the backup is lost. Restore names each one it drops (ADR-0020);
  it does not merge, because the rollback of a failed restore must put the prior
  file back byte for byte.
