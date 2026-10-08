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
  `timings.predicted_per_second`; fewer than 16 predicted tokens, a body without
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
  verdict says what they did.** `awaitSlotsIdle` re-reads `/slots` through the
  client every 500 ms until no slot is processing, for at most the floor, and the
  detail reads `the server's slots went idle 9s after the kill`, `1 slot still
  generating 1m30s after the kill` or `/slots could not be read after the kill`.
  The wait cancels nothing, since the server cancels on its own; it keeps the next
  proof and the operator's next completion from running against a slot still
  prefilling, and it makes the property the issue asks for ("a killed proof leaves
  no generating slot behind") a measured fact in every report rather than an
  assumption.

- **The search-residency drive keeps the floor.** Its rounds are 512 tokens, 51 s
  at 10 tok/s, and the proof stops after the first sampled round, inside 90 s.

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

The Gemma leg of this change is to be proven on hardware once the entry lands
(#321): doctor under gemma-4-31b with `agent_enabled = true`, the agent check
passing under the scaled budget, and `/slots` idle after a forced kill.
