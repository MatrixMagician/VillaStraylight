package inference

import (
	"context"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// validate.go is the offload-asserting orchestrator: it sequences a real inference
// run into a single typed Verdict, the frozen Phase-3 status / Phase-5 dashboard
// contract (D-11). Like internal/preflight it is a PURE LIBRARY — it NEVER calls
// os.Exit and NEVER prints; the command layer (cmd/villa/inference.go) maps the
// Verdict to an exit code and a table/JSON. Phase 3 install reuses Validate as-is.
//
// The contract it enforces (D-11): health is OFFLOAD-ASSERTING, not liveness. A
// server that answers /health and even streams tokens but ran on the CPU
// (llvmpipe / offloaded 0/N / ~zero GTT delta) is a FAIL, not a PASS. Uncertainty
// (an unreadable stderr or sysfs read) degrades to WARN — never a false-green.

// defaultReadyTimeout bounds how long Validate waits for the server to become ready
// before treating the run as not-ready (a WARN, since we then cannot assert offload).
const defaultReadyTimeout = 5 * time.Minute

// defaultCeilingTimeout bounds the near-ceiling stress probe's readiness wait.
const defaultCeilingTimeout = 5 * time.Minute

// ValidateInput is the fully-injected description of one validation run. Every
// side-effecting dependency (the Runner, the sysfs GTT reader, the ceiling Runner
// factory) is an injected seam so Validate is pure and fake-Runner testable; no live
// container or live /sys read happens inside this package in CI.
type ValidateInput struct {
	// Model is the catalog-resolved entry being validated (dims feed the math).
	Model catalog.CatalogModel
	// ModelsDir is the host directory holding the GGUF, bind-mounted read-only into
	// the container. It MUST flow through to the RunSpec the Runner is started with;
	// an empty value renders an invalid `-v :/models` bind and the container never
	// becomes ready (the integration gap on-hardware validation caught).
	ModelsDir string
	// ContextLen is the recommend-chosen context the primary run starts at.
	ContextLen int

	// The recommend-computed fit terms (reused for the ceiling stress math, D-10).
	WeightBytes   uint64
	KVCacheBytes  uint64 // KV cache at ContextLen
	HeadroomBytes uint64
	EnvelopeBytes uint64

	// Runner is the primary inference run (the caller wires a container Runner at
	// the recommend ctx; tests inject a fake).
	Runner Runner
	// NewCeilingRunner builds a SECOND Runner at the near-ceiling stress spec. It is
	// a factory (not a Runner) so the ceiling run is independent of the primary one
	// and can be torn down separately (D-10, T-02-12). If nil, the ceiling probe is
	// skipped (run-only / no-ceiling mode).
	NewCeilingRunner func(stress RunSpec) Runner

	// ReadGTTUsed reads the live amdgpu mem_info_gtt_used (the before/after offload
	// delta signal, D-09.2). Injected so tests replay a fixture delta and production
	// wires detect.GTTUsedBytes. Called once before Start and once after readiness.
	ReadGTTUsed func() detect.Bytes

	// ReadyTimeout / PollInterval bound readiness polling (defaults applied if zero).
	ReadyTimeout time.Duration
	PollInterval time.Duration
}

// Validate runs the full offload-asserting sequence and folds every signal into a
// single typed Verdict. It is side-effect-pure beyond the injected Runner/reader
// seams and never errors: an unevaluable step becomes a WARN, a confirmed CPU
// fallback a FAIL (D-11).
//
// Sequence (D-09/D-10/D-11):
//  1. read GTT-used BEFORE start
//  2. Runner.Start at the recommend ctx; defer Stop (always tears down)
//  3. pollHealth to readiness (readiness gate only, Pitfall 5)
//  4. read GTT-used AFTER + capture stderr
//  5. dual offload assert: log-scrape AND sysfs delta, both required (D-09)
//  6. chatProbe a real completion (D-04 reuse)
//  7. contextCeilingProbe at the envelope ceiling (D-10)
//  8. combine → Verdict (PASS needs offload PASS + chat tokens; CPU fallback FAIL
//     even when /health=200; Unknown signal or ceiling cliff → WARN)
func Validate(ctx context.Context, in ValidateInput) Verdict {
	readyTimeout := in.ReadyTimeout
	if readyTimeout <= 0 {
		readyTimeout = defaultReadyTimeout
	}

	// (1) GTT-used BEFORE start (the offload-delta baseline).
	before := in.ReadGTTUsed()

	// (2) Start the primary run; ALWAYS tear it down.
	if err := in.Runner.Start(spec(in)); err != nil {
		_ = in.Runner.Stop()
		return fail(
			"inference run failed to start",
			"check podman is installed and the model file exists under the models dir",
			"Runner.Start", err.Error(),
		)
	}
	defer func() { _ = in.Runner.Stop() }()

	// (3) Poll readiness — this is the gate, NOT the verdict (Pitfall 5).
	ready := pollRunnerHealth(ctx, in.Runner, readyTimeout, in.PollInterval)
	if !ready.Known || !ready.Value {
		// Could not even reach readiness → we cannot assert offload. WARN (uncertain),
		// not FAIL: the run never got far enough to confirm a CPU fallback.
		return warn(
			"server did not become ready — offload could not be asserted",
			"check the container started and llama-server loaded the model (see logs with -v)",
			"pollHealth", firstNonEmptyStr(ready.Source, ready.Raw),
		)
	}

	// (4) GTT-used AFTER readiness + captured stderr.
	after := in.ReadGTTUsed()
	stderr, _ := in.Runner.Logs()

	// (5) The dual offload assert (D-09): both signals required for a PASS.
	logRes := scrapeOffloadLog(stderr)
	sysRes := offloadSysfsDelta(before, after, in.WeightBytes)
	offload := combineOffload(logRes, sysRes)

	// (6) Real chat completion (D-04 reuse). Even if offload PASSed, a run that
	// cannot return tokens is not a clean PASS.
	chat := chatProbe(ctx, in.Runner.Endpoint(), in.Model.ID)

	// (6.5) Stop the primary BEFORE the ceiling probe (CR-01). The ceiling runs a
	// second container that binds the SAME loopback port; if the primary is still up
	// the ceiling container cannot bind it, and its readiness poll would instead hit
	// the live primary on that port and FALSE-CLEAR. All primary signals (offload +
	// chat) are already captured above, so it is safe to tear it down now. The
	// deferred Stop remains as an idempotent safety net for the early-return paths.
	_ = in.Runner.Stop()

	// (7) Near-ceiling stress probe (D-10) — classified finding, never a crash.
	ceiling := runCeiling(ctx, in)

	// (8) Fold all signals into the final verdict.
	return foldVerdict(offload, chat, ceiling)
}

// spec builds the primary RunSpec from the input.
func spec(in ValidateInput) RunSpec {
	return RunSpec{
		ContainerName: "villa-inference-validate",
		ModelFile:     primaryModelFile(in.Model),
		ModelsDir:     in.ModelsDir,
		ContextLen:    in.ContextLen,
	}
}

// runCeiling derives the near-ceiling stress ctx and runs the probe, if a ceiling
// Runner factory was supplied. With no factory the ceiling is skipped (run-only
// mode) and reported as a PASS (nothing to flag).
func runCeiling(ctx context.Context, in ValidateInput) CeilingResult {
	if in.NewCeilingRunner == nil {
		return CeilingResult{Status: StatusPass, Detail: "context-ceiling probe skipped"}
	}
	stressCtx := stressContextFor(in.ContextLen, in.WeightBytes, in.KVCacheBytes, in.HeadroomBytes, in.EnvelopeBytes, in.Model.DefaultCtx)
	stress := RunSpec{
		ContainerName: "villa-inference-ceiling",
		ModelFile:     primaryModelFile(in.Model),
		ModelsDir:     in.ModelsDir,
		ContextLen:    stressCtx,
	}
	timeout := in.ReadyTimeout
	if timeout <= 0 {
		timeout = defaultCeilingTimeout
	}
	return contextCeilingProbe(ctx, in.NewCeilingRunner(stress), stress, timeout, in.PollInterval)
}

// foldVerdict combines the dual-offload verdict, the chat probe, and the ceiling
// finding into the final Verdict, preserving the offload signals + observed GTT
// delta for the --json contract (D-11).
//
// Precedence (FAIL dominates, then WARN):
//   - offload FAIL (confirmed CPU fallback)      → FAIL (even if /health=200, D-11)
//   - offload WARN (unevaluable signal)          → WARN
//   - chat not OK (no tokens)                    → WARN (offload proven but liveness
//     unconfirmed — never a silent PASS)
//   - ceiling cliff (OOM/hang finding)           → WARN (D-10)
//   - else                                       → PASS (offload proven + real tokens
//     + ceiling cleared)
func foldVerdict(offload Verdict, chat ChatResult, ceiling CeilingResult) Verdict {
	// Carry the offload signals + delta into whatever verdict we return.
	base := offload // already has LogOffload/SysfsOffload/GTTDeltaBytes/Provenance

	if offload.Status == StatusFail {
		// Confirmed CPU fallback — the silent-fallback this whole phase exists to catch.
		return base
	}

	if offload.Status == StatusWarn {
		// Offload could not be fully verified → WARN (keep its detail/remediation).
		return base
	}

	// Offload PASSed. Now liveness + ceiling decide PASS vs WARN.
	if !chat.OK {
		base.Status = StatusWarn
		base.Detail = "offload proven but the chat completion returned no tokens: " + chat.Detail
		base.Remediation = "the iGPU offload engaged but the server did not produce a completion — retry and check the model is fully loaded"
		return base
	}

	if ceiling.CliffFound {
		base.Status = StatusWarn
		base.Detail = fmt.Sprintf("offload proven and chat OK (%d tokens), but the context ceiling was hit: %s", chat.Tokens, ceiling.Detail)
		base.Remediation = fmt.Sprintf("safe up to ~ctx %d; the near-max-context probe surfaced a cliff — keep context below the ceiling", ceiling.StressCtx)
		base.Raw = ceiling.Raw
		return base
	}

	base.Status = StatusPass
	base.Detail = fmt.Sprintf("offload proven (log + sysfs), chat returned %d tokens, and the context ceiling cleared at ctx %d", chat.Tokens, ceiling.StressCtx)
	return base
}

// primaryModelFile resolves the on-disk GGUF filename for a model. It prefers the
// first shard's filename (the Plan-01 download manifest) and falls back to the model
// ID when no shard metadata is present (test fixtures).
func primaryModelFile(m catalog.CatalogModel) string {
	if len(m.Shards) > 0 && m.Shards[0].Filename != "" {
		return m.Shards[0].Filename
	}
	return m.ID + ".gguf"
}

// firstNonEmptyStr returns the first non-empty of a, b.
func firstNonEmptyStr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
