package inference

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// This file holds the three READINESS/LIVENESS probes the validate orchestrator
// sequences, all of them backend-neutral (no podman/image/device literals — those
// stay behind the Backend/Runner seam). None of them is the offload verdict: the
// offload assert (offload.go, D-09) is what proves the iGPU engaged. These probes
// answer the adjacent questions:
//
//   - pollHealth          — is the server READY to take requests? (Pitfall 5: this
//     is a readiness gate ONLY, never the offload verdict.)
//   - chatProbe           — does a REAL chat completion return tokens through the
//     reused internal/llm OpenAIClient? (D-04 — reuse, do NOT rebuild the client.)
//   - contextCeilingProbe — does a SECOND run at the envelope-ceiling ctx clear, or
//     does it hit the OOM/long-context cliff? It CLASSIFIES an OOM/hang/timeout as a
//     reported finding and tears the container down — it NEVER propagates the cliff
//     as a crash (D-10, Pitfall 4).

// pollInterval is the default gap between readiness polls.
const pollInterval = 250 * time.Millisecond

// ChatResult is the outcome of a real chat completion probe.
type ChatResult struct {
	// OK is true when the completion streamed at least one token.
	OK bool
	// Tokens is the number of non-empty content deltas received.
	Tokens int
	// Text is the assembled completion (bounded — a probe prompt is tiny).
	Text string
	// Detail explains a failure (empty on success).
	Detail string
}

// pollHealth polls GET <endpoint>/health until it returns 200 OK, treating a 503
// ("Loading model") as still-loading and continuing to poll, bounded by timeout.
// It is the READINESS gate ONLY (Pitfall 5) — a 200 means "accepting requests", NOT
// "offload happened"; the offload verdict is the dual assert in offload.go.
//
//   - 200 before timeout      → Known/true (ready)
//   - 503 repeatedly          → keep polling (still loading)
//   - never-200 before timeout → typed Unknown (could not evaluate readiness), NOT a
//     bare false and NOT a crash — distinct from a confirmed-unhealthy server.
func pollHealth(ctx context.Context, client *http.Client, endpoint string, timeout, interval time.Duration) detect.Bool {
	if interval <= 0 {
		interval = pollInterval
	}
	deadline := time.Now().Add(timeout)
	url := strings.TrimRight(endpoint, "/") + "/health"

	for {
		if ctx.Err() != nil {
			return detect.UnknownBool("readiness poll cancelled before /health returned 200", ctx.Err().Error())
		}
		if ok := probeHealthOnce(ctx, client, url); ok {
			return detect.KnownBool(true, url)
		}
		if time.Now().After(deadline) {
			return detect.UnknownBool("server did not become ready (/health never returned 200 before timeout)", "")
		}
		select {
		case <-ctx.Done():
			return detect.UnknownBool("readiness poll cancelled before /health returned 200", ctx.Err().Error())
		case <-time.After(interval):
		}
	}
}

// probeHealthOnce does a single GET /health and reports whether it was 200. Any
// transport error or non-200 (including 503 still-loading) is reported as not-ready
// so the caller keeps polling.
func probeHealthOnce(ctx context.Context, client *http.Client, url string) bool {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// chatProbe sends a real, small chat completion to <endpoint>/v1/chat/completions
// through the REUSED internal/llm OpenAIClient (D-04 — the gateway is reused, never
// rebuilt) and collects the streamed token deltas. A non-200 / stream error is
// reported as a failure detail, never a panic.
func chatProbe(ctx context.Context, endpoint, modelID string) ChatResult {
	client := llm.NewOpenAIClient(llm.Options{
		BaseURL:      strings.TrimRight(endpoint, "/") + "/v1",
		APIKey:       "local",
		DefaultModel: modelID,
		Timeout:      chatProbeTimeout,
	})

	var (
		tokens int
		sb     strings.Builder
	)
	err := client.StreamChat(ctx, llm.ChatRequest{
		Messages: []llm.Message{{Role: llm.RoleUser, Content: chatProbePrompt}},
	}, func(delta string) error {
		if delta != "" {
			tokens++
			sb.WriteString(delta)
		}
		return nil
	})
	if err != nil {
		return ChatResult{OK: false, Tokens: tokens, Text: sb.String(), Detail: err.Error()}
	}
	if tokens == 0 {
		return ChatResult{OK: false, Detail: "chat completion returned no tokens (server responded but produced nothing)"}
	}
	return ChatResult{OK: true, Tokens: tokens, Text: sb.String()}
}

// chatProbePrompt is the tiny user message the chat probe sends; it only needs to
// elicit a non-empty completion to prove the /v1 path works end-to-end.
const chatProbePrompt = "Reply with the single word: ok"

// chatProbeTimeout bounds the chat probe so a wedged server cannot hang validation.
const chatProbeTimeout = 60 * time.Second

// CeilingResult is the typed outcome of the context-ceiling stress probe. It is a
// CLASSIFIED finding, never a propagated error (D-10): an OOM/hang at the ceiling is
// information the user wants ("your context ceiling is here"), not a validation
// crash.
type CeilingResult struct {
	// Status is PASS (the ceiling cleared) or WARN (a cliff/finding was hit). The
	// ceiling probe never produces FAIL on its own — a cliff is a WARN finding, the
	// offload assert owns FAIL.
	Status Status
	// CliffFound is true when an OOM/hang/timeout cliff was classified.
	CliffFound bool
	// StressCtx is the context length the ceiling was probed at.
	StressCtx int
	// Detail is a one-line human explanation of the classification.
	Detail string
	// Raw captures the offending stderr marker on an OOM classification (bounded).
	Raw string
}

// ceiling stderr markers that indicate the iGPU/host could not allocate at the
// stress context — the OOM cliff. Matched case-insensitively against captured,
// bounded stderr.
var ceilingOOMMarkers = []string{
	"failed to allocate",
	"device memory allocation",
	"out of memory",
	"ggml_vulkan: error",
	"cudamalloc failed", // defensive: other backends' alloc-failure phrasing
}

// contextCeilingProbe starts a SECOND inference run (via the provided Runner — the
// caller wires a fresh --rm container Runner at the stress spec) at a near-ceiling
// context and CLASSIFIES the outcome (D-10, Pitfall 4):
//
//   - became ready before timeout                  → PASS (the ceiling clears)
//   - Start failed                                 → WARN finding (could not probe)
//   - never ready + an OOM marker in stderr        → WARN OOM-cliff finding
//   - never ready + no marker (hung)               → WARN hang finding
//
// In EVERY case the Runner is torn down (deferred Stop) — a hung/OOM ceiling
// container is never left running (T-02-12). It never returns an error: the cliff is
// the finding, not a crash.
func contextCeilingProbe(ctx context.Context, runner Runner, stress RunSpec, readyTimeout, interval time.Duration) CeilingResult {
	res := CeilingResult{StressCtx: stress.ContextLen}

	// Teardown is guaranteed: defer Stop so an OOM/hung ceiling container is always
	// removed even on the early-return paths below.
	defer func() { _ = runner.Stop() }()

	if err := runner.Start(stress); err != nil {
		res.Status = StatusWarn
		res.CliffFound = true
		res.Detail = "context-ceiling probe could not start the stress container: " + err.Error()
		return res
	}

	// Poll readiness at the stress ctx, bounded by readyTimeout, under the caller's
	// context. We reuse the runner's own Health (it knows its endpoint) so this stays
	// backend-neutral.
	if ready := pollRunnerHealth(ctx, runner, readyTimeout, interval); ready.Known && ready.Value {
		res.Status = StatusPass
		res.Detail = "context-ceiling cleared: the stress container became ready at the envelope-ceiling context"
		return res
	}

	// Did not become ready → classify the cliff from the captured stderr.
	stderr, _ := runner.Logs()
	if marker, hit := matchCeilingOOM(stderr); hit {
		res.Status = StatusWarn
		res.CliffFound = true
		res.Detail = "context-ceiling OOM cliff: the model could not allocate at the envelope-ceiling context (validate-time finding, not a runtime crash)"
		res.Raw = marker
		return res
	}

	res.Status = StatusWarn
	res.CliffFound = true
	res.Detail = "context-ceiling hang: the stress container never became ready before timeout (long-context hang surfaced at validation time)"
	return res
}

// pollRunnerHealth polls a Runner's Health until Known+true or the timeout elapses.
// It mirrors pollHealth but goes through the Runner interface (which owns the
// endpoint and probe), so the ceiling probe stays backend-neutral and fake-Runner
// testable.
func pollRunnerHealth(ctx context.Context, runner Runner, timeout, interval time.Duration) detect.Bool {
	if interval <= 0 {
		interval = pollInterval
	}
	deadline := time.Now().Add(timeout)
	for {
		if h := runner.Health(); h.Known && h.Value {
			return h
		}
		if time.Now().After(deadline) || ctx.Err() != nil {
			return detect.UnknownBool("server did not become ready before timeout", "")
		}
		select {
		case <-ctx.Done():
			return detect.UnknownBool("server readiness poll cancelled", ctx.Err().Error())
		case <-time.After(interval):
		}
	}
}

// matchCeilingOOM reports whether captured stderr contains an allocation-failure
// marker and returns the matched line (bounded) for the finding's Raw.
func matchCeilingOOM(stderr string) (marker string, hit bool) {
	low := strings.ToLower(stderr)
	for _, m := range ceilingOOMMarkers {
		if idx := strings.Index(low, m); idx >= 0 {
			// Return the surrounding line for context, bounded to a sane length.
			line := lineAround(stderr, idx)
			return line, true
		}
	}
	return "", false
}

// lineAround returns the full line containing byte offset idx in s, bounded to 200
// chars so an over-long line cannot bloat the finding.
func lineAround(s string, idx int) string {
	start := strings.LastIndexByte(s[:idx], '\n') + 1
	end := strings.IndexByte(s[idx:], '\n')
	if end < 0 {
		end = len(s)
	} else {
		end = idx + end
	}
	line := strings.TrimSpace(s[start:end])
	if len(line) > 200 {
		line = line[:200]
	}
	return line
}

// stressContextFor derives a near-ceiling stress context from the recommend-computed
// fit terms (D-10 reuse of the recommend KV/headroom math WITHOUT importing its
// internals): KV scales LINEARLY with ctx, so kv(stress) = kvAtRecCtx · stress/recCtx.
// It returns the largest ctx whose modelled total weight + kv(stress) + headroom
// still fits the envelope — the context the run would sit right under the ceiling at —
// BOUNDED by the model's trained max context (maxCtx, the catalog default_ctx).
//
// The maxCtx cap is load-bearing: for a small model the per-token KV is tiny, so the
// pure memory-bound ceiling is millions of tokens — far beyond n_ctx_train. Probing
// there is meaningless (llama.cpp clamps/garbles a ctx above the model's rope train
// length), and reporting a multi-million-token "ceiling cleared" is misleading. The
// real ceiling is min(memory-bound ctx, model max). With maxCtx ≤ 0 the cap is off.
//
// It returns a ctx STRICTLY greater than recCtx when there is headroom to grow toward
// the (capped) ceiling, never a ctx whose modelled total exceeds the envelope, and
// never one above the model's trained max.
func stressContextFor(recCtx int, weightBytes, kvAtRecCtx, headroomBytes, envelopeBytes uint64, maxCtx int) int {
	if recCtx <= 0 {
		return recCtx
	}
	// Memory-bound ceiling: the largest ctx whose KV fits the envelope budget.
	memBound := uint64(recCtx)
	base := weightBytes + headroomBytes
	if base < envelopeBytes && kvAtRecCtx > 0 {
		kvBudget := envelopeBytes - base
		// Largest ctx whose KV fits the budget: ctx = recCtx · kvBudget/kvAtRecCtx.
		memBound = uint64(recCtx) * kvBudget / kvAtRecCtx
	}
	// Cap at the model's trained max context — never probe beyond n_ctx_train.
	stress := memBound
	if maxCtx > 0 && stress > uint64(maxCtx) {
		stress = uint64(maxCtx)
	}
	if stress <= uint64(recCtx) {
		// Already at/over the (capped) ceiling at the recommend ctx — probe there.
		return recCtx
	}
	return int(stress)
}
