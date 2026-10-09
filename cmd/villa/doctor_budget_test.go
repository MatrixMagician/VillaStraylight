package main

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
)

// TestAgentBudgetForScalesWithTheDecodeRate: the agent proof's budget is the time
// agentProofTokens take at the served model's measured decode rate, floored at the
// old constant and capped at the ceiling (#318). The rows are the models the vet
// measured: a 50 tok/s MoE stays at the floor, the two dense models at 10 to 12
// tok/s get about three minutes, a model too slow to be usable hits the ceiling, and
// an unmeasured rate falls to the floor and says so.
func TestAgentBudgetForScalesWithTheDecodeRate(t *testing.T) {
	cases := []struct {
		name   string
		rate   float64
		err    error
		busy   int
		budget time.Duration
		source string
	}{
		{"fast MoE at the floor", 50, nil, 0, 90 * time.Second, "measured 50.0 tok/s"},
		{"qwen3.8-27b, no thinking", 11.55, nil, 0, 177 * time.Second, "measured 11.6 tok/s"},
		{"gemma-4-31b, thinking", 10.3, nil, 0, 199 * time.Second, "measured 10.3 tok/s"},
		{"too slow to be usable", 2, nil, 0, 300 * time.Second, "measured 2.0 tok/s"},
		// Measured on the dev host 2026-10-08: qwen3.6-35b-a3b read 1.5 tok/s while two
		// other doctors were prefilling Crush prompts on the same GPU. The budget can
		// only grow from that, but the source must say the rate was taken under load.
		{"a 46 tok/s model read under two other prefills", 1.5, nil, 2, 300 * time.Second, "measured 1.5 tok/s while 2 other slots were generating"},
		{"one other slot", 20, nil, 1, 102 * time.Second, "measured 20.0 tok/s while 1 other slot was generating"},
		{"unmeasured", 0, errors.New("llm: upstream returned 401"), 0, 90 * time.Second, "decode rate unmeasured (llm: upstream returned 401)"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			b := agentBudgetFor(c.rate, c.err, c.busy)
			if b.Budget != c.budget {
				t.Errorf("Budget = %v, want %v", b.Budget, c.budget)
			}
			if b.Source != c.source {
				t.Errorf("Source = %q, want %q", b.Source, c.source)
			}
		})
	}
}

// TestAgentToolCallVerdictNamesTheBudget: every verdict the proof returns carries the
// budget it ran under and where that budget came from, so an operator reading
// `agent-tool-call ... signal: killed` can see the bound and the rate it was derived
// from. A kill that the server's slots drained after names the drain; one they did
// not drain after names the busy slots.
func TestAgentToolCallVerdictNamesTheBudget(t *testing.T) {
	b := agentBudget{Budget: 199 * time.Second, Source: "measured 10.3 tok/s"}

	v := agentToolCallVerdict(true, nil, b, nil)
	if v.Status != inference.StatusPass {
		t.Fatalf("completed round: Status = %v, want PASS", v.Status)
	}
	if want := "the agent completed a real read→edit tool-call round-trip over the local endpoint (budget 3m19s, measured 10.3 tok/s)"; v.Detail != want {
		t.Errorf("completed round: Detail = %q, want %q", v.Detail, want)
	}

	v = agentToolCallVerdict(false, nil, b, nil)
	if v.Status != inference.StatusFail || !strings.Contains(v.Detail, "budget 3m19s") {
		t.Errorf("unedited round: Status = %v Detail = %q, want FAIL naming the budget", v.Status, v.Detail)
	}

	killed := errors.New("crush run: signal: killed")
	decoding := roundWork{PromptTokens: 17125, PromptSeconds: 15.6, PredictedTokens: 812, Known: true}
	v = agentToolCallVerdict(false, killed, b, &killedRound{Drain: slotDrain{Elapsed: 9 * time.Second, Readable: true}, Work: decoding})
	if v.Status != inference.StatusFail {
		t.Fatalf("killed round: Status = %v, want FAIL", v.Status)
	}
	if want := "the agent tool-call round-trip failed to run: crush run: signal: killed (budget 3m19s, measured 10.3 tok/s; the server prefilled 17125 prompt tokens at 1098 tok/s and generated 812; the server's slots went idle 9s after the kill)"; v.Detail != want {
		t.Errorf("killed round: Detail = %q, want %q", v.Detail, want)
	}
	if !strings.Contains(v.Remediation, "measured decode rate") {
		t.Errorf("killed while decoding: Remediation = %q, want the budget named as the limit", v.Remediation)
	}

	v = agentToolCallVerdict(false, killed, b, &killedRound{Drain: slotDrain{Elapsed: 90 * time.Second, Busy: 1, Readable: true}, Work: decoding})
	if !strings.Contains(v.Detail, "1 slot still generating 1m30s after the kill") {
		t.Errorf("undrained kill: Detail = %q, want the busy slot named", v.Detail)
	}

	v = agentToolCallVerdict(false, killed, b, &killedRound{})
	if !strings.Contains(v.Detail, "the slot count could not be read after the kill") {
		t.Errorf("unreadable slots: Detail = %q, want the unreadable read named", v.Detail)
	}
	if !strings.Contains(v.Detail, "the server's token counters could not be read around the round") {
		t.Errorf("unreadable counters: Detail = %q, want the unreadable counters named", v.Detail)
	}
}

// TestAgentToolCallVerdictNamesThePrefillCause: a round the budget killed while
// the server generated no token at all was never limited by the decode rate. On
// gemma-4-31b (2026-10-09) Crush's 16988-token prompt prefilled at 240 tok/s, Crush
// abandoned it at 60 s for sending nothing (its request_timeout), and the server
// generated nothing until the 3m14s budget killed the run. The verdict stays FAIL,
// since the agent really cannot work on that model, but the detail and the
// remediation name the prefill, not the budget.
func TestAgentToolCallVerdictNamesThePrefillCause(t *testing.T) {
	b := agentBudget{Budget: 194 * time.Second, Source: "measured 10.6 tok/s"}
	prefilling := &killedRound{
		Drain: slotDrain{Readable: true},
		Work:  roundWork{PromptTokens: 14541, PromptSeconds: 60, PredictedTokens: 0, Known: true},
	}
	v := agentToolCallVerdict(false, errors.New("crush run: signal: killed"), b, prefilling)
	if v.Status != inference.StatusFail {
		t.Fatalf("Status = %v, want FAIL: the agent cannot complete a round trip on this model", v.Status)
	}
	if want := "the agent tool-call round-trip failed to run: crush run: signal: killed (budget 3m14s, measured 10.6 tok/s; the server prefilled 14541 prompt tokens at 242 tok/s and generated none, so no agent request reached its first token; the server's slots went idle 0s after the kill)"; v.Detail != want {
		t.Errorf("Detail = %q, want %q", v.Detail, want)
	}
	for _, want := range []string{"prefill", "request_timeout", "60 s", "#323"} {
		if !strings.Contains(v.Remediation, want) {
			t.Errorf("Remediation = %q, want it to name %q", v.Remediation, want)
		}
	}
	if strings.Contains(v.Remediation, "decode rate") {
		t.Errorf("Remediation = %q, want no decode-rate cause on a round that generated nothing", v.Remediation)
	}
}

// TestRoundWorkBetween: what the server did during a killed round is the difference
// of its cumulative /metrics counters, read before the round and after its slots
// drained. A counter that could not be read, or that went backwards because the unit
// restarted in between, makes the whole reading Unknown rather than a fabricated 0.
func TestRoundWorkBetween(t *testing.T) {
	sample := func(prompt uint64, seconds float64, predicted uint64) metrics.CounterSample {
		return metrics.CounterSample{
			PromptTokensTotal: prompt, PromptTokensKnown: true,
			PromptSecondsTotal: seconds, PromptSecondsKnown: true,
			PredictedTokensTotal: predicted, PredictedTokensKnown: true,
		}
	}
	before := sample(1000, 10, 500)

	if got, want := roundWorkBetween(before, sample(15541, 70, 500)), (roundWork{PromptTokens: 14541, PromptSeconds: 60, PredictedTokens: 0, Known: true}); got != want {
		t.Errorf("roundWorkBetween = %+v, want %+v", got, want)
	}
	if got := roundWorkBetween(before, metrics.CounterSample{}); got.Known {
		t.Errorf("unreadable after: %+v, want Known=false", got)
	}
	unknownSeconds := sample(15541, 70, 500)
	unknownSeconds.PromptSecondsKnown = false
	if got := roundWorkBetween(before, unknownSeconds); got.Known {
		t.Errorf("unknown prompt seconds: %+v, want Known=false", got)
	}
	if got := roundWorkBetween(before, sample(200, 2, 10)); got.Known {
		t.Errorf("counters reset by a restart: %+v, want Known=false", got)
	}
}

// TestAwaitSlotsIdle: after a killed round the proof waits, bounded, for the server
// to release the round's slots, counting them through the /metrics
// requests_processing gauge (Client.Slots is typed-Unknown on llama.cpp b11430,
// whose next_token is an array the slots parser does not read, so the witness
// could not use it). It returns when no slot is processing, or
// at the bound with the busy count, and reports an unreadable gauge as such rather
// than as idle.
func TestAwaitSlotsIdle(t *testing.T) {
	t.Run("returns once the slots drain", func(t *testing.T) {
		reads := []int{2, 1, 0}
		read := func() (int, bool) {
			n := reads[0]
			if len(reads) > 1 {
				reads = reads[1:]
			}
			return n, true
		}
		d := awaitSlotsIdle(t.Context(), read, time.Millisecond, time.Second)
		if !d.Readable || d.Busy != 0 {
			t.Errorf("drain = %+v, want readable and 0 busy", d)
		}
		if d.Elapsed >= time.Second {
			t.Errorf("Elapsed = %v, want under the 1s bound (the slots drained on the third read)", d.Elapsed)
		}
	})

	t.Run("stops at the bound with the busy count", func(t *testing.T) {
		read := func() (int, bool) { return 1, true }
		d := awaitSlotsIdle(t.Context(), read, time.Millisecond, 20*time.Millisecond)
		if !d.Readable || d.Busy != 1 {
			t.Errorf("drain = %+v, want readable with 1 busy", d)
		}
		if d.Elapsed < 20*time.Millisecond {
			t.Errorf("Elapsed = %v, want at least the 20ms bound", d.Elapsed)
		}
	})

	t.Run("an unreadable gauge is not idle", func(t *testing.T) {
		read := func() (int, bool) { return 0, false }
		d := awaitSlotsIdle(t.Context(), read, time.Millisecond, 20*time.Millisecond)
		if d.Readable {
			t.Errorf("drain = %+v, want Readable=false", d)
		}
	})

	t.Run("a cancelled context stops the wait", func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		cancel()
		read := func() (int, bool) { return 1, true }
		d := awaitSlotsIdle(ctx, read, time.Millisecond, time.Minute)
		if d.Elapsed > time.Second {
			t.Errorf("Elapsed = %v, want a prompt return on a cancelled context", d.Elapsed)
		}
	})
}

// TestProcessingSlots: the busy count the budget and the drain read is the
// /metrics requests_processing gauge; an unavailable scrape is not zero busy.
func TestProcessingSlots(t *testing.T) {
	if n, ok := processingSlots(metrics.PerfSnapshot{RequestsProcessing: 2}, true); n != 2 || !ok {
		t.Errorf("processingSlots(2, ok) = %d, %v; want 2, true", n, ok)
	}
	if n, ok := processingSlots(metrics.PerfSnapshot{}, false); ok || n != 0 {
		t.Errorf("processingSlots(unavailable) = %d, %v; want 0, false", n, ok)
	}
}
