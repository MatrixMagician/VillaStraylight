package inference

import (
	"context"
	"errors"
	"fmt"
	"math"
	"math/rand/v2"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// decode.go is the served model's measured decode rate: one bounded completion
// whose per-token timing llama-server reports itself. The coding-agent proof sizes
// its round-trip budget from it (#318, ADR-0031).
//
// It is a measurement at the moment it is asked for, not a reading of a gauge:
// the /metrics rate gauge is a per-scrape bucket that reads 0 once idle, the
// status report omits its tok/s unless a slot is generating, and the lifetime
// counters mix every request the unit has served. Thinking stays on, so a model
// that reasons by default is measured the way the agent will drive it.

const (
	// decodeProbeMaxTokens bounds the probe. It is enough tokens for the rate to be
	// a rate (a reasoning model spends all of them thinking) and short enough that
	// the slowest usable model finishes in a few seconds.
	decodeProbeMaxTokens = 64
	// decodeProbeMinTokens is the fewest predicted tokens a sample may rest on: a
	// reply that stopped after a couple of tokens is dominated by its warm-up.
	decodeProbeMinTokens = 16
	// decodeProbeTimeout bounds the whole probe, prefill included.
	decodeProbeTimeout = 60 * time.Second
	// decodeProbePrompt asks for prose long enough to exceed the bound from any
	// model, thinking or not.
	decodeProbePrompt = "Explain in detail, step by step, why the sky is blue by day and red at sunset."
	// decodeProbeTemperature over the flat decodeProbeSampler makes every sampled
	// token close to uniform, so no window of the probe's output repeats one the
	// server has seen. llama-server's ngram-mod keeps one n-gram map across every
	// request and the pinned build takes no per-request speculation field, so this
	// is how the probe keeps a draft from replaying an earlier answer (ADR-0031).
	decodeProbeTemperature = 2.0
)

var decodeProbeSampler = llm.Sampler{TopK: 0, TopP: 1, MinP: 0}

// errDecodeTooShort is returned when the reply carried fewer than
// decodeProbeMinTokens predicted tokens.
var errDecodeTooShort = errors.New("inference: decode probe produced too few tokens to measure a rate")

// DecodeRate measures the served model's decode rate in tokens per second from
// llama-server's timings of one bounded completion. Any reply that cannot stand as
// a rate is an error naming why: a non-200, a body without timings, or too few
// tokens.
func (c Client) DecodeRate(ctx context.Context, modelID string) (float64, error) {
	ctx, cancel := context.WithTimeout(ctx, decodeProbeTimeout)
	defer cancel()
	timings, err := c.Chat(decodeProbeTimeout).Complete(ctx, llm.ChatRequest{
		Model:    modelID,
		Messages: []llm.Message{{Role: llm.RoleUser, Content: decodeProbePrompt}},
		Sampler:  &decodeProbeSampler,
	}, decodeProbeMaxTokens, rand.IntN(math.MaxInt32), decodeProbeTemperature)
	if err != nil {
		return 0, err
	}
	if timings.PredictedN < decodeProbeMinTokens {
		return 0, fmt.Errorf("%w: %d predicted tokens, want at least %d", errDecodeTooShort, timings.PredictedN, decodeProbeMinTokens)
	}
	if timings.PredictedPerSec <= 0 {
		return 0, fmt.Errorf("inference: decode probe timings carry no rate (%d tokens in %.0f ms)", timings.PredictedN, timings.PredictedMS)
	}
	return timings.PredictedPerSec, nil
}
