// Package residency owns the residency-proof drive protocol: the evidence, from
// two independent signals, that offload actually happened.
//
// ADR-0001 records that this proof must never be false-green, and it is the
// invariant the product is built on. It was implemented five separate times in the
// command tier, two of those copies sharing 54 identical lines out of ~71, and had
// no unit test anywhere — every copy began by reading config and probing the host,
// so none of them was reachable from a test.
//
// The protocol is one shape: poll health, drive a real workload, sample GPU-busy on
// a ticker WHILE that workload runs, join, fold the signals into a verdict. What
// actually differs between callers is which model, context and backend they prove,
// and two timeouts. Those are Target fields. Every host effect is a Deps func field,
// so a test drives the whole protocol without a live host.
//
// # The Unknown-versus-negative rule is part of this interface
//
// An unevaluable signal is a WARN and a contradicted signal is a FAIL, and neither
// may be manufactured from the other. That is why Prove returns the tri-state
// inference.Verdict rather than a two-valued pass/fail: collapsing them here would
// let a probe that could not be read masquerade as proof that offload did not
// happen, and vice versa. The transactional callers, which must treat both as a
// non-pass, convert at the boundary via Cutover.
//
// This package holds NO backend marker. The device token, the fault string and the
// image/device literals arrive only through Target.Markers, sourced by the caller
// from inference.BackendFor(name).ResidencyProof().
package residency

import (
	"context"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// DefaultSampleInterval is how often Prove re-reads GPU-busy during the drive. A
// single post-drive read can miss a short decode's busy window, so the protocol
// samples repeatedly while tokens stream and keeps the maximum.
const DefaultSampleInterval = 100 * time.Millisecond

// DefaultReadyTimeout bounds the readiness poll and, through the shared deadline,
// the whole proof. It is the load_tensors-hang guard: a server that never becomes
// ready before this deadline is a FAIL, never an infinite wait. The value is seeded
// from inference's unexported defaultReadyTimeout.
const DefaultReadyTimeout = 5 * time.Minute

// Deps is the injectable seam set. Every host-touching action is a func field so
// the drive protocol is driven from a test with fakes, with no live host.
type Deps struct {
	// PollHealth is the bounded readiness gate over the ALREADY-running server. A
	// 200 means "accepting requests", NOT "offload happened" — that is the fold.
	// Live wiring is inference.PollHealth.
	PollHealth func(ctx context.Context, endpoint string, timeout time.Duration) detect.Bool
	// Generate runs the REAL generation probe against the already-running server and
	// reports the streamed token result. It starts no container. Live wiring is
	// inference.GenerationProbe.
	Generate func(ctx context.Context, endpoint, modelID string) inference.ChatResult
	// GPUBusy reads the point-in-time sysfs gpu_busy_percent. Live wiring is
	// detect.GPUBusyPercent. Sampled repeatedly DURING the drive.
	GPUBusy func() detect.Int
	// GTTUsed reads the point-in-time mem_info_gtt_used floor. Live wiring is
	// detect.GTTUsedBytes.
	GTTUsed func() detect.Bytes
	// Journal reads the INVOCATION-scoped journal of the named service — not the
	// whole-unit journal, whose oldest bytes are stale prior-start output. Live
	// wiring is orchestrate.NewSystemd().ResidencyJournal, and the bool is its
	// found flag (the repo's status seam shape).
	//
	// A journal that could not be read yields empty text, which the fold types as
	// Unknown. That is deliberate: an unreadable journal is an ABSENT signal, never
	// evidence that offload did not happen.
	Journal func(service string) (text string, found bool)
	// Props reads the llama.cpp /props response for the config-identity drift
	// overlay. OPTIONAL: nil means /props was not consulted, which the fold treats
	// as Unknown — it is corroboration only, never the residency proof itself. The
	// under-load proofs wire it from the status seams so every signal flows through
	// the same readers the status fold uses.
	Props func() *inference.PropsInfo
	// Fold turns the gathered signals into the residency verdict. Live wiring is
	// inference.RunningOffloadVerdict. It is a seam so a test can drive the protocol
	// against a chosen verdict without constructing journal text.
	Fold func(inference.RunningOffloadInput) inference.Verdict
}

// Target is what actually differs between callers: which model, context and backend
// is being proven, plus the two timing bounds.
type Target struct {
	// Endpoint is the inference endpoint to poll and probe, derived by the caller
	// from the resolved backend's container runner — never a hand-rolled URL.
	Endpoint string
	// Service is the systemd service whose invocation-scoped journal carries the
	// load_tensors residency line.
	Service string
	// ModelID is the model id sent to the generation probe. For coding mode this is
	// the SERVED coder model, not the chat model.
	ModelID string
	// ModelFile is the catalog-resolved GGUF FILENAME the /props drift overlay
	// compares against. The identity checks compare against the model FILE, not the
	// catalog id: passing the id makes the drift overlay misfire the moment it
	// evaluates.
	ModelFile string
	// ContextLen is the context the rendered unit actually serves. For coding mode
	// this is the resolved agent ctx (the rendered single -c), so the fit math
	// matches the agent-ctx KV rather than the chat ctx.
	ContextLen int
	// WeightBytes is the served model's expected on-disk weight footprint, the
	// reference the GTT floor compares against.
	WeightBytes uint64
	// Markers is the backend-owned residency descriptor, the ONLY source of backend
	// marker tokens in the whole protocol.
	Markers inference.ResidencyMarkers
	// ReadyTimeout bounds the readiness poll and the whole proof. Zero means
	// DefaultReadyTimeout.
	ReadyTimeout time.Duration
	// SampleInterval is how often GPU-busy is re-read during the drive. Zero means
	// DefaultSampleInterval.
	SampleInterval time.Duration
}

// Prove drives the residency-proof protocol and returns the tri-state verdict.
//
// The three gates run inside ONE bounded deadline:
//
//	(a) bounded readiness — never-ready before the deadline is a FAIL, not a wait;
//	(b) a REAL generation probe (tokens > 0), with GPU-busy sampled DURING the
//	    decode and the maximum kept;
//	(c) the residency fold over the invocation-scoped journal, the GTT floor, the
//	    during-probe GPU-busy reading and the backend's markers.
//
// Gates (a) and (b) fail with a typed FAIL: a server that will not become ready, or
// a probe that streams no tokens, is a contradicted signal about this stack, not an
// unevaluable one. Gate (c)'s tri-state is returned verbatim — Prove never rewrites
// a WARN into a FAIL or the reverse.
func Prove(ctx context.Context, d Deps, t Target) inference.Verdict {
	readyTimeout := t.ReadyTimeout
	if readyTimeout == 0 {
		readyTimeout = DefaultReadyTimeout
	}
	sampleInterval := t.SampleInterval
	if sampleInterval == 0 {
		sampleInterval = DefaultSampleInterval
	}

	deadlineCtx, cancel := context.WithTimeout(ctx, readyTimeout)
	defer cancel()

	// (a) Bounded readiness.
	ready := d.PollHealth(deadlineCtx, t.Endpoint, readyTimeout)
	if !ready.Known || !ready.Value {
		return fail("not ready before timeout (possible load_tensors hang or CPU-fallback stall)")
	}

	// (b) The real generation probe, with GPU-busy sampled DURING the decode window.
	// The probe runs in a goroutine and the sampler polls on a ticker while tokens
	// stream, keeping the max — a single post-probe read can miss a short decode.
	chatCh := make(chan inference.ChatResult, 1)
	go func() {
		chatCh <- d.Generate(deadlineCtx, t.Endpoint, t.ModelID)
	}()

	maxBusy := detect.UnknownInt("gpu_busy_percent not sampled during probe", "")
	sample := func() {
		if b := d.GPUBusy(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
			maxBusy = b
		}
	}
	ticker := time.NewTicker(sampleInterval)
	defer ticker.Stop()
sampleLoop:
	for {
		// Sample once up front and on every tick, so even a very short decode gets at
		// least one in-flight read.
		sample()
		select {
		case chat := <-chatCh:
			sample() // one last read at completion
			if !chat.OK || chat.Tokens == 0 {
				if chat.Detail != "" {
					return fail("generation probe failed: " + chat.Detail)
				}
				return fail("generation probe returned no tokens")
			}
			break sampleLoop
		case <-ticker.C:
		case <-deadlineCtx.Done():
			return fail("generation probe did not complete before timeout (possible load_tensors hang or CPU-fallback stall)")
		}
	}

	// (c) The residency fold. An unreadable journal yields empty text, which the fold
	// reads as a typed-Unknown — absence of the signal is not evidence of CPU
	// fallback.
	journal, _ := d.Journal(t.Service)
	return d.Fold(inference.RunningOffloadInput{
		JournalText:    journal,
		GTTUsedBytes:   d.GTTUsed(),
		GPUBusyPercent: maxBusy,
		WeightBytes:    t.WeightBytes,
		ConfigModel:    t.ModelFile,
		ConfigContext:  t.ContextLen,
		Markers:        t.Markers,
	})
}

// ProveCutover is the entry point for the three transactional callers (backend
// swap, coding mode, restore): drive the protocol, then map the tri-state onto the
// shared cutover verdict. It exists so the mapping is named once rather than
// re-composed at each call site.
func ProveCutover(ctx context.Context, d Deps, t Target) prove.Verdict {
	return Cutover(Prove(ctx, d, t))
}

// Cutover maps a residency verdict onto the transactional cutover decision.
//
// ONLY a true StatusPass is a pass. Everything else — a contradicted FAIL and an
// unevaluable WARN alike — is a non-pass that rolls the cutover back, because a
// transaction cannot commit on evidence it does not have. The tri-state distinction
// survives in the detail text rather than being erased before the caller sees it.
func Cutover(v inference.Verdict) prove.Verdict {
	if v.Status == inference.StatusPass {
		return prove.Verdict{Status: prove.StatusPass, Detail: v.Detail}
	}
	return prove.Verdict{Status: prove.StatusFail, Detail: v.Detail}
}

// fail builds a contradicted-signal verdict for the two drive gates.
func fail(detail string) inference.Verdict {
	return inference.Verdict{Status: inference.StatusFail, Detail: detail}
}
