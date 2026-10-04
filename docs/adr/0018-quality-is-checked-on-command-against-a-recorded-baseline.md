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
  version that changes whenever a case is added, removed or edited. Three grader
  kinds ship. `exact` matches after trimming and case-folding. `regex` matches
  anywhere in the reply. `json` parses the reply as one JSON object and checks
  required key/value pairs. A new kind is a table row, never a branch at a call
  site.

- **Every completion is greedy and goes through the inference client.** Each case
  is sent with temperature 0, thinking disabled through `chat_template_kwargs` (the
  grounding audit's setting), and a bounded `max_tokens`. It is sent through
  `inferenceClient(cfg).Chat(…)` and `StreamChat`, accumulating the content
  (ADR-0014). `Complete` is not used because it discards the content. Cases run
  one at a time, so a parallel slot cannot change a reply's batching.

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
  nothing is written. A recorded baseline holds every case's pass or fail.
  Recording over an existing baseline is allowed and prints the score it replaces.

- **Baselines persist in their own store.** `internal/evalstore` sits on
  `internal/jsonstore` at `eval-baselines.json` under villa's XDG data root,
  schema-versioned, one document holding every model's baseline. It is a backup
  entry, because a baseline records a known-good state that cannot be re-recorded
  after the regression it exists to catch.

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

**Tool-call cases in the first suite.** `llm.ChatRequest` has no `tools` field, and
the tools-mode proof already drives one real tool call. A `tool` grader kind can be
added later, together with an append-only `Tools` field on the request.

## Consequences

- A pin re-vet gains a quality step: `villa eval` on the old pin, `villa eval` on
  the new one, and the per-case diff in the PR.
- Small catalog models fail many cases. That is expected: the baseline is per
  model, so only a change against the model's own record counts.
- Editing a case orphans every baseline recorded under the old suite version. The
  report says so (Reject: no baseline for this suite version) rather than comparing
  across suites.
