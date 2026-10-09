---
status: accepted
---

# The agent proof budget is sized from the measured decode rate

`villa doctor` proves the coding agent with one real `crush run` round trip: read
a planted file, edit it, reply. The round trip ran under a fixed 90 s budget
(`agentProofBudget`, `cmd/villa/doctor.go`), sized when the served model was a
50 tok/s MoE with thinking off. The #315 vet (`docs/research/catalog-families-vet-2026-10-08.md`)
served gemma-4-31b, a dense model that thinks by default and decodes at 10.3 tok/s,
and doctor reported `agent-tool-call BLOCK FAIL crush run: signal: killed` on a
stack that was otherwise healthy and that completed the same round trip given the
time. The control, qwen3.8-27b at 11.55 tok/s with no reasoning block, passed with
the round trip's 512 tokens generated in 44 s. The vet also recorded that the next
completion after the killed proof ran at 1.7 tok/s, which it read as the killed
request still generating on its slot (#318).

The issue left four options open. The decision below takes two and refuses two.

## What the kill does, measured

Before deciding what "cancel the server-side request" could mean, the kill was
reproduced on the dev host (qwen3.6-35b-a3b, ROCm 10.0, llama.cpp b11430, four
slots, Crush v0.93.1 at `127.0.0.1:8080/v1` through rootlessport): the probe's
`crush run` was started in a planted working directory and SIGKILLed 8.0 s in, the
way `exec.CommandContext` kills it, while `/slots` was read every 0.5 s and the
villa-llama journal captured.

- Crush had two requests in flight: a short one (197-token prompt, its session
  title) on slot 1 and the agent round on slot 3, a 17762-token prompt that was
  still in prompt processing at 1150 tok/s when the kill landed.
- llama-server logged `stop: cancel task` for the first 1.9 s after the kill and
  released its slot 3.8 s after; for the second 9.1 s after the kill, at the moment
  its prompt processing finished (`progress = 1.00`), and released it 20 ms later.
- `/slots` read all four slots idle 8.5 s after the kill. No Crush or language
  server process outlived the kill.

llama-server's completion handler polls `should_stop`, which is httplib's
`is_connection_closed`, once a second in both the streaming and the non-streaming
path (`server_response_reader::next`, `HTTP_POLLING_SECONDS = 1`), and the
reader's destructor posts a cancel task for every unfinished task. rootlesskit's
`bicopy` half-closes the container side (`CloseWrite`) when the host side reads
EOF, so the killed client's FIN reaches the server through the proxy. The cancel
is applied by the server loop between batches, so a kill that lands during the
prefill of a long prompt is honoured when that prefill ends. On gemma-4-31b, which
prefills at 265 tok/s, a 17.7k-token Crush prompt takes about 67 s to process, so a
kill in that window leaves the slot busy for up to a minute. That is the
mechanism behind the vet's slow completion. llama-server at this build has no
cancel route: its control route (`/v1/chat/completions/{id}/control`) accepts
`reasoning_end` and nothing else.

## Decision

- **The budget is sized from the served model's decode rate, measured at proof
  time.** `agentBudgetFor(rate, err)` (`cmd/villa/doctor_budget.go`) gives
  `agentProofTokens` (2048) at the measured rate, no less than
  `agentProofBudgetFloor` (90 s, the old constant) and no more than
  `agentProofBudgetCeiling` (300 s). The token count is the 512 content tokens the
  control generated, plus about 350 reasoning tokens per model round over three
  rounds, with the remainder covering the prefill the rate does not measure. At
  10.3 tok/s the budget is 199 s; at 11.55 tok/s, 177 s; at 50 tok/s, the floor.
  A model that cannot decode 2048 tokens in five minutes cannot complete a usable
  round trip, and doctor says so rather than wait.

- **The rate is one bounded completion through `inference.Client`, read from
  llama-server's own timings.** `Client.DecodeRate` (`internal/inference/decode.go`)
  sends a 64-token, non-streaming completion with thinking left on and returns
  `timings.predicted_per_second`. It samples flat (`top_k` 0, `top_p` 1, `min_p`
  0, temperature 2) with a fresh seed, so a speculating stack cannot replay an
  earlier probe (below, "A probe a draft can replay"). Fewer than 16 predicted tokens, a body without
  timings or a non-200 are errors, never a rate. Thinking stays on because the
  agent drives the model as served, and a model that reasons by default must be
  measured reasoning. The budget is measured once per doctor run
  (`sync.OnceValue` in `liveDoctorDeps`) and both agent proofs run under it; the
  tools-mode cutover (`liveToolsProve`) measures its own.

- **An unmeasured rate gets the floor and says so.** `agentBudget.Source` is
  `measured 10.3 tok/s` or `decode rate unmeasured (<error>)`, and every verdict
  the proof returns names it, so a 90 s budget on a dead or keyless server reads
  as a fallback with its reason, never as a measurement.

- **A rate taken under another client's load says so.** The slots are read
  before the probe, and the source becomes `measured 1.5 tok/s while 2 other
  slots were generating`. The first run of this change on the dev host read
  exactly that on qwen3.6-35b-a3b, a 46 tok/s model, while two other `villa
  doctor` runs were prefilling Crush prompts on the same GPU; the budget went to
  the ceiling. Contention can only inflate the budget, never shrink it, so the
  proof cannot fail from it, but the detail must not report the GPU's share as
  the model's rate.

- **A killed round is followed by a bounded wait for the slots to drain, and the
  verdict says what they did.** `awaitSlotsIdle` re-reads the `/metrics`
  `requests_processing` gauge through the client every 500 ms until it is zero,
  for at most the floor, and the detail reads `the server's slots went idle 9s
  after the kill`, `1 slot still generating 1m30s after the kill` or `the slot
  count could not be read after the kill`. The gauge, not `/slots`: on llama.cpp
  b11430 each slot's `next_token` is an array, which `metrics.ParseSlots` does
  not read, so `Client.Slots` is typed-Unknown on the served build (the first
  forced-budget run reported exactly that; the `/slots` body measured 5 KB, so it
  was not the client's cap). The parser defect is tracked on its own.
  The wait cancels nothing, since the server cancels on its own; it keeps the next
  proof and the operator's next completion from running against a slot still
  prefilling, and it makes the property the issue asks for ("a killed proof leaves
  no generating slot behind") a measured fact in every report rather than an
  assumption.

- **The search-residency drive keeps the floor.** Its rounds are 512 tokens, 51 s
  at 10 tok/s, and the proof stops after the first sampled round, inside 90 s.

## A probe a draft can replay

The first Gemma leg (2026-10-09) ran doctor twice. The second probe read 53.4
tok/s on a model that decodes at 10.6, with the journal showing `draft acceptance
= 1.00000 (53 accepted / 53 generated)`, so the second doctor sized its budget at
the floor. llama-server's `ngram-mod` keeps one n-gram map "shared across all
sequences" (`common/speculative.cpp` at the pinned commit 8345f3339). It adds every
prompt and every generated token to that map, and it resets the map only past 25%
occupancy. A greedy probe answers the same way every time, so the next run drafts
the whole answer back.

Measured on the dev host (qwen3.6-35b-a3b, `--spec-type ngram-mod`, 64 tokens,
each pair back to back):

| probe | first | second |
|---|---|---|
| identical greedy (the shipped probe) | 51.4 tok/s, nothing drafted | 196.6 tok/s, 53 of 53 accepted |
| a random nonce in the prompt, greedy | 93.5, 38 of 53 | 259.7, 57 of 57 |
| a fresh seed at temperature 1 | 70.8, 26 of 57 | 72.2, 26 of 54 |
| flat sampler at temperature 2, fresh seed | 44.3, 4 of 54 | 46.3, nothing drafted |

**Decision: the probe samples flat at temperature 2 with a fresh seed.** Every
token is then close to uniform over the vocabulary, so no 24-token window of the
answer (`ngram-mod`'s `n_match`) repeats one the map holds. The request carries
it through `llm.Sampler`, which only `Complete` sends; the bench passes nil and
its wire body is unchanged. The flat read sits about 10% under the cold greedy
read on that model, which is the sampler working over the whole vocabulary (about
2.6 ms a token). That errs toward a longer budget and is 2.6% at 10 tok/s. A fixed
seed also read clean (46.1, 46.0), but only because draft verification and batch
shapes perturb a temperature-2 sample; the fresh seed makes it hold by
construction.

*Instead:*

- **Turn speculation off for the probe request.** This was the preferred fix. The
  pinned build has no per-request switch: `speculative.n_max`, `speculative.type`
  and the n-gram sizes sit under `#if 0` in `tools/server/server-schema.cpp` at
  8345f3339, although the server README still lists them.
- **A nonce in the prompt.** Measured above, it does not work. Greedy output
  converges on the same text, and once the lookup window lies entirely inside the
  answer, it hits again.
- **A seeded sample at an ordinary temperature.** Measured above, it does not
  work either. A reasoning model opens every answer with the same preamble, and
  that preamble is already in the map.

## A kill that was never the budget's

The first Gemma leg also failed `agent-tool-call` under the scaled budget, `crush
run: signal: killed (budget 3m14s, measured 10.6 tok/s ...)`. The journal shows a
cause the budget cannot reach. Crush sent its 205-token title request and the
16988-token agent request. The agent prompt prefilled at 240 to 370 tok/s in
2048-token chunks, and the title request queued behind them, released at 201 of
its 205 tokens. At 60.7 s Crush hung up on both, and the server cancelled them
with the agent prompt at 14336 tokens. Nothing was generated before the kill at
194 s. Crush v0.93.1 gives up on a request that sends nothing for its
`request_timeout`, which defaults to 60 s (`internal/config/config.go`,
`DefaultRequestTimeout`). A stub server that never answers saw Crush's requests at
t=10 s and t=70 s. Gemma prefills Crush's prompt in about 71 s, so no round can
start. A longer budget only waits longer.

**Decision: a killed round reports what the server did, and names prefill when
it generated nothing.** `liveAgentToolCallVerdict` reads llama-server's cumulative
counters (`prompt_tokens_total`, `prompt_seconds_total`, `tokens_predicted_total`)
through `inference.Client.Counters` before the round. When the budget kills the
round, it reads them again after the slot drain. The order matters. The server
flushes a cancelled prefill's prompt tokens when its slots go idle and a slot's
generated tokens at `release()` (`tools/server/server-context.cpp`), so only a read
after the drain is whole. The difference rides in the detail as `the server
prefilled N prompt tokens at R tok/s and generated M`. A round that took prompt
tokens and generated none gets `and generated none, so no agent request reached its
first token`. Its remediation names Crush's 60 s limit and #323, and says a longer
budget cannot help. The verdict stays FAIL, because the agent cannot complete a
round trip on that model. A killed round that generated tokens keeps the budget
remediation. A counter that could not be read, or that went backwards because the
unit restarted, reads as `the server's token counters could not be read around the
round`.

The counters are server-wide, like the drain witness. Another client generating
during the round makes the generated count non-zero, and the remediation then
falls back to the budget's: less specific, never a wrong prefill claim. A model
whose title request decodes while the agent prompt prefills does the same. The
detail still shows both numbers.

*Instead:*

- **Raise Crush's `request_timeout` in the rendered `crush.json`** (`options.request_timeout`,
  in seconds; 0 disables it). This would let a slow-prefill model finish a round
  trip, in doctor and in `villa code` alike. It changes the agent's behaviour for
  the operator and the AGENT-04 reference crush.json is checked against, so it is
  a separate decision, not part of a diagnostic fix.
- **Read Crush's own error.** `crush run` prints `LLM stream received no data for
  1m0s`, but the probe discards Crush's output, and that text is not a contract.
- **Measure the prefill rate in the probe.** A probe prompt the size of Crush's
  costs as long as the failure it diagnoses, about 70 s on Gemma.
- **`villa tools-mode enter` gets the same evidence.** Its kill path has no drain
  wait, so its counter read would miss the cancelled prefill. It keeps the
  budget-only detail.

## Rejected

**A per-entry thinking default rendered into the unit (option 1).** A
`--chat-template-kwargs '{"enable_thinking":false}'` on the served unit changes
what every client gets, Open WebUI included, to make a diagnostic pass.

**A thinking-off kwarg in the proof only (option 3).** The cutover probe does
this (`GenerationProbe`) because it asks one question whose answer is `ok`. The
agent proof exists to show the agent can drive the model the operator will use
from `villa code`, and Crush does not send that kwarg. A proof that measures a
model the agent never talks to is a false PASS.

**Reading the rate the stack already samples.** `status.Report.GenTokensPerSec`
is nil unless a slot is generating (`liveGenTokensPerSec` omits it when
`metrics.IsGenerating` is false), and the stack is idle when the proof starts.
The `/metrics` gauge `llamacpp:predicted_tokens_seconds` is a per-scrape bucket
that doctor's own status read resets; it read 0 on the live host. The lifetime
counters (`tokens_predicted_total` over `tokens_predicted_seconds_total`, 1864
tokens in 40.4 s on the live host) average every request since the unit started,
across speculation hits, reasoning and concurrent slots, and are 0 over 0 on a
fresh unit. None is the served model's rate at the moment the proof needs it.

**A per-entry class in the catalog.** A constant that drifts with the backend,
speculation mode and kernel, and that the first dense entry would have had to
guess. The probe costs a few seconds of doctor time and measures the model as
served today.

**SIGTERM before SIGKILL, or a server-side cancel call.** The server learns of
the kill from the socket close, which SIGTERM produces the same way, and it has no
cancel route to call.

## Consequences

`villa doctor` and `villa tools-mode enter` take a few seconds longer when the
agent is on (the 64-token probe), and up to five minutes for the agent proof on a
slow model where they took 90 s. The `agent-tool-call` detail now carries the
budget and its source, so an operator reading a FAIL can see whether the bound
was measured or the floor. The `--json` goldens are unchanged: the finding's
shape did not move, only the detail's text, which the goldens hold as test data.

The probe reads low on a GPU that has been idle for minutes. Measured on the dev
host with the probe's own request (qwen3.6-35b-a3b, 64 tokens, greedy): 50.3,
50.8 and 51.3 tok/s after 45 s idle; 15.4 then 26.0 tok/s after 160 s idle;
doctor's own probe read 28.4 after about 2.5 min idle. The limiter is the GPU's
power ramp, which one 64-token request does not outlast. A cold read only widens
the budget (the floor and ceiling bound it either way) and the proof cannot fail
from it, but the detail's rate is then the cold GPU's, not the model's. A warm-up
request before the probe, or taking the faster of two reads, would cost a few
seconds more per doctor run and is left to a later change if the number matters
to an operator.

The kill path was also driven through villa itself: a scratch build with the
floor and ceiling at 10 s ran `villa doctor` on qwen3.6-35b-a3b, which killed
both agent rounds and reported `crush run: signal: killed (budget 10s, measured
42.0 tok/s while 1 other slot was generating; 1 slot still generating 10s after
the kill)`; the journal shows the server cancelling each round's two tasks
within 1.3 s of the kill. The slot still generating belonged to another `villa
doctor` running at the same time: the gauge counts every client's slots, so on a
shared server the drain detail is a fact about the server, not an attribution
to the killed round.

On gemma-4-31b (dev host, 2026-10-09, ngram speculation, ctx 131072), two doctors
ran back to back with both fixes in place:

```
doctor 1  agent-tool-call FAIL | ... (budget 3m58s, measured 8.6 tok/s; the server prefilled 14537 prompt tokens at 221 tok/s and generated none, so no agent request reached its first token; the server's slots went idle 0s after the kill)
doctor 2  agent-tool-call FAIL | ... (budget 3m19s, measured 10.3 tok/s; the server prefilled 14337 prompt tokens at 219 tok/s and generated none, so no agent request reached its first token; the server's slots went idle 0s after the kill)
```

The second probe read 10.3 tok/s (97.2 ms a token, one 53-token draft with 2
accepted), against 95.5 ms a token for a 512-token greedy round on the same unit.
Under the old probe, the second doctor read 53.4. The first read 8.6 tok/s
(116 ms a token, nothing drafted) 4 s after the swap loaded the model. That is a
cold read of the kind recorded above, not the sampler, since the second doctor
used the same sampler. The agent stays unusable on Gemma through Crush until its
prefill fits inside Crush's limit, and doctor now says why.

Each probe run plants a fresh working directory, and Crush writes it into the
`<env>` block of its system prompt. Two runs' agent requests differ only in that
line, 28% of the way into the request. A cached prefix, with or without context
checkpoints, can therefore cover at most that first part of a new run's prompt.
