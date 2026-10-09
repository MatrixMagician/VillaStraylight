package main

import (
	"context"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
)

// doctor_budget.go sizes the coding-agent tool-call round-trip's budget from the
// served model's decode rate, measured at proof time, and after a round the budget
// killed waits for llama-server to release the round's slots (#318, ADR-0031). The
// invariant: every verdict names the budget it ran under and where it came from,
// so a bound that fell to the floor reads as a fallback, never as a measurement.

const (
	// agentProofBudgetFloor is the smallest budget a round trip gets: the 90 s the
	// proof carried before it scaled, which every model at 23 tok/s or faster still
	// gets. It also bounds the search-residency drive, whose rounds are 512 tokens,
	// and the wait for a killed round's slots to drain.
	agentProofBudgetFloor = 90 * time.Second
	// agentProofBudgetCeiling caps the budget. A model that cannot decode
	// agentProofTokens in five minutes cannot complete a usable round trip, and
	// doctor says so rather than wait on it.
	agentProofBudgetCeiling = 300 * time.Second
	// agentProofTokens is the generation one round trip is given at the measured
	// rate: the 512 content tokens the qwen3.8-27b control generated plus about 350
	// reasoning tokens per round over three rounds, with the remainder covering the
	// prefill of Crush's prompt, which a decode rate does not measure.
	agentProofTokens = 2048
	// slotDrainPoll is how often awaitSlotsIdle re-reads the busy count.
	slotDrainPoll = 500 * time.Millisecond
)

// agentBudget is the bound on one tool-call round trip and the measurement it
// came from, so every verdict can say what it ran under.
type agentBudget struct {
	Budget time.Duration
	// Source names the measurement, or why there is none.
	Source string
}

func (b agentBudget) String() string {
	return fmt.Sprintf("budget %s, %s", b.Budget.Round(time.Second), b.Source)
}

// agentBudgetFor sizes the budget from a measured decode rate: agentProofTokens at
// that rate, no less than the floor and no more than the ceiling. An unmeasured
// rate gets the floor, and the budget says why it is the floor. busy is how many
// other slots were generating when the rate was taken: a rate read beside another
// client's prefill is the GPU's share, not the model's, so the source says so.
func agentBudgetFor(rate float64, err error, busy int) agentBudget {
	if err != nil || rate <= 0 {
		why := "no rate"
		if err != nil {
			why = err.Error()
		}
		return agentBudget{Budget: agentProofBudgetFloor, Source: "decode rate unmeasured (" + why + ")"}
	}
	budget := time.Duration(float64(agentProofTokens) / rate * float64(time.Second)).Round(time.Second)
	budget = min(max(budget, agentProofBudgetFloor), agentProofBudgetCeiling)
	source := fmt.Sprintf("measured %.1f tok/s", rate)
	switch {
	case busy == 1:
		source += " while 1 other slot was generating"
	case busy > 1:
		source += fmt.Sprintf(" while %d other slots were generating", busy)
	}
	return agentBudget{Budget: budget, Source: source}
}

// liveAgentBudget measures the served model's decode rate through the inference
// client and sizes the budget from it. The probe keeps thinking on, so it measures
// the model the agent talks to; the busy count is read first so a rate taken under
// another client's load is reported as such.
func liveAgentBudget(ctx context.Context, cfg config.VillaConfig) agentBudget {
	client := inferenceClient(cfg)
	busy, _ := processingSlots(client.Perf(ctx))
	rate, err := client.DecodeRate(ctx, cfg.Model)
	return agentBudgetFor(rate, err, busy)
}

// processingSlots is the number of slots generating, read from the /metrics
// requests_processing gauge, the one IsGenerating already trusts. /slots is not
// used: llama.cpp b11430 emits each slot's next_token as an array, which
// metrics.ParseSlots does not read, so Client.Slots is typed-Unknown on the
// served build (measured on the dev host 2026-10-08; tracked separately).
func processingSlots(snap metrics.PerfSnapshot, ok bool) (int, bool) {
	if !ok {
		return 0, false
	}
	return int(snap.RequestsProcessing), true
}

// slotDrain is what llama-server's slots did after a killed round: how long until
// none was processing, or how many still were at the bound.
type slotDrain struct {
	Elapsed  time.Duration
	Busy     int
	Readable bool
}

func (d slotDrain) String() string {
	elapsed := d.Elapsed.Round(time.Second)
	switch {
	case !d.Readable:
		return "the slot count could not be read after the kill"
	case d.Busy == 0:
		return fmt.Sprintf("the server's slots went idle %s after the kill", elapsed)
	case d.Busy == 1:
		return fmt.Sprintf("1 slot still generating %s after the kill", elapsed)
	default:
		return fmt.Sprintf("%d slots still generating %s after the kill", d.Busy, elapsed)
	}
}

// awaitSlotsIdle re-reads the busy count until it is zero, the bound passes or
// ctx ends. It cancels nothing: llama-server cancels a round itself once the
// killed client's socket closes and the slot reaches its next batch boundary
// (1.9 s and 9.1 s after the kill on the dev host, ADR-0031). What it adds is the
// witness, so the next proof and the operator's next completion do not run
// against a slot still draining, and the verdict says so when one does.
func awaitSlotsIdle(ctx context.Context, read func() (int, bool), poll, bound time.Duration) slotDrain {
	start := time.Now()
	for {
		busy, ok := read()
		d := slotDrain{Elapsed: time.Since(start), Busy: busy, Readable: ok}
		if !ok || d.Busy == 0 || d.Elapsed >= bound || ctx.Err() != nil {
			return d
		}
		select {
		case <-ctx.Done():
			d.Elapsed = time.Since(start)
			return d
		case <-time.After(poll):
		}
	}
}

// liveSlotDrain waits, bounded by the floor, for the served unit's slots to drain
// after a killed round, counting them through the inference client's /metrics read.
func liveSlotDrain(ctx context.Context, cfg config.VillaConfig) slotDrain {
	client := inferenceClient(cfg)
	return awaitSlotsIdle(ctx, func() (int, bool) { return processingSlots(client.Perf(ctx)) }, slotDrainPoll, agentProofBudgetFloor)
}

// roundWork is what llama-server did, for every client, between a read of its
// cumulative counters before a round and a read after the killed round's slots
// drained. The drain matters: the server flushes a cancelled prefill's prompt
// tokens when its slots go idle and a slot's generated tokens when it is released.
type roundWork struct {
	PromptTokens    uint64
	PromptSeconds   float64
	PredictedTokens uint64
	Known           bool
}

// roundWorkBetween is the difference of two counter reads. An unreadable counter,
// or one that went backwards because the unit restarted in between, makes the
// reading Unknown.
func roundWorkBetween(before, after metrics.CounterSample) roundWork {
	known := before.PromptTokensKnown && after.PromptTokensKnown &&
		before.PromptSecondsKnown && after.PromptSecondsKnown &&
		before.PredictedTokensKnown && after.PredictedTokensKnown &&
		after.PromptTokensTotal >= before.PromptTokensTotal &&
		after.PromptSecondsTotal >= before.PromptSecondsTotal &&
		after.PredictedTokensTotal >= before.PredictedTokensTotal
	if !known {
		return roundWork{}
	}
	return roundWork{
		PromptTokens:    after.PromptTokensTotal - before.PromptTokensTotal,
		PromptSeconds:   after.PromptSecondsTotal - before.PromptSecondsTotal,
		PredictedTokens: after.PredictedTokensTotal - before.PredictedTokensTotal,
		Known:           true,
	}
}

// prefillBound is a round in which the server took prompt tokens and generated
// none: no request reached its first token, so the decode rate never limited it.
func (w roundWork) prefillBound() bool {
	return w.Known && w.PromptTokens > 0 && w.PredictedTokens == 0
}

func (w roundWork) String() string {
	if !w.Known {
		return "the server's token counters could not be read around the round"
	}
	prefill := fmt.Sprintf("the server prefilled %d prompt tokens", w.PromptTokens)
	if w.PromptSeconds > 0 {
		prefill += fmt.Sprintf(" at %.0f tok/s", float64(w.PromptTokens)/w.PromptSeconds)
	}
	if w.prefillBound() {
		return prefill + " and generated none, so no agent request reached its first token"
	}
	return fmt.Sprintf("%s and generated %d", prefill, w.PredictedTokens)
}

// killedRound is the evidence the proof gathers after its budget killed a round.
type killedRound struct {
	Drain slotDrain
	Work  roundWork
}

const (
	agentRunRemediation     = "ensure the agent is installed (`villa install --coding-agent`) and the stack is up (`villa up`), then re-run `villa doctor`; check `villa verify agent` and `villa logs`"
	agentBudgetRemediation  = "the served model generated tokens but could not finish a tool call inside a budget sized from its measured decode rate; check `villa verify agent` and `villa logs`, or serve a faster model (`villa model swap`)"
	agentPrefillRemediation = "the served model is too slow at prefill for the agent: every Crush session opens with its whole system prompt and tool list to prefill, Crush abandons a request that sends nothing for 60 s (its request_timeout), and the server had not finished that prefill in time, so a longer doctor budget cannot help; serve a model that prefills the agent's prompt in under a minute (`villa model swap`) to use the agent"
)

// agentToolCallVerdict maps one round trip's outcome to the doctor verdict. Every
// detail carries the budget and its source; a killed round also carries what the
// server did during the round and what its slots did afterwards, and its
// remediation names the cause that evidence points to.
func agentToolCallVerdict(completed bool, err error, b agentBudget, kill *killedRound) inference.Verdict {
	switch {
	case err != nil:
		detail := fmt.Sprintf("the agent tool-call round-trip failed to run: %v (%s", err, b)
		remediation := agentRunRemediation
		if kill != nil {
			detail += "; " + kill.Work.String() + "; " + kill.Drain.String()
			remediation = agentBudgetRemediation
			if kill.Work.prefillBound() {
				remediation = agentPrefillRemediation
			}
		}
		return inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      detail + ")",
			Remediation: remediation,
		}
	case !completed:
		return inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      fmt.Sprintf("the agent ran but did not complete the read→edit tool-call round-trip (the probe file was not edited as instructed; %s)", b),
			Remediation: "check `villa verify agent` and `villa logs` — the coder model may not be serving tool-calls correctly",
		}
	}
	return inference.Verdict{
		Status: inference.StatusPass,
		Detail: fmt.Sprintf("the agent completed a real read→edit tool-call round-trip over the local endpoint (%s)", b),
	}
}
