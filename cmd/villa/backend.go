package main

import (
	"context"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// backend.go is the cmd-tier `villa backend` noun: the live host wiring that drives
// the pure internal/backendswap transactional core (Plan 08-01). It holds the ONE
// genuinely new composition — liveProve — plus the cobra surface (set/show/--dry-run),
// the Result→exit mapping, and liveBackendSwapDeps (Task 08-02-02).
//
// CRITICAL — backend-marker discipline (T-8-11): this file must stay LITERAL-FREE of
// backend marker tokens (the per-backend residency device token, the HSA override env
// var, the GPU-fault abort string, and any image/device literal). Every such marker
// arrives ONLY through inference.BackendFor(target).ResidencyProof(); the
// 08-01-03-extended TestSeamGrepGate now WALKS cmd/villa and fails CI on any such leak.
// Do NOT paste markers here.

// proveTimeout bounds the cutover readiness poll (and, by the shared deadline
// context, the whole prove). It is seeded from inference's defaultReadyTimeout (5m)
// and documented as the load_tensors-hang guard (Pitfall 2 / T-8-07): a server that
// never becomes ready before this deadline is a PROVE FAIL → rollback, NEVER an
// infinite wait. inference does not export defaultReadyTimeout, so the value is
// mirrored here as a named const with that provenance.
const proveTimeout = 5 * time.Minute

// busySampleInterval is how often liveProve re-reads detect.GPUBusyPercent() DURING
// the generation probe (D-07 / Assumption A2): a single post-probe read can miss a
// short decode's busy window, so the prove samples repeatedly while tokens stream and
// keeps the max.
const busySampleInterval = 100 * time.Millisecond

// liveProve is the injected cutover gate (backendswap.Deps.Prove). It probes the
// ALREADY-running villa-llama.service (no --rm container) and returns a backendswap
// verdict the transactional core gates on. It composes THREE required gates inside a
// single bounded deadline:
//
//	(a) bounded readiness via the EXPORTED inference.PollHealth (Pitfall 2 timeout),
//	(b) a REAL generation probe via the EXPORTED inference.GenerationProbe (tokens>0),
//	(c) residency proof via inference.RunningOffloadVerdict fed the target backend's
//	    BackendFor(target).ResidencyProof() markers + the SAME concrete liveWeightBytes
//	    / liveModelFile seams status.go uses, with detect.GPUBusyPercent() sampled
//	    DURING the decode (D-07).
//
// It maps ONLY inference.StatusPass to backendswap.ProveStatusPass; any other verdict
// (including ready+health-200-but-residency-FAIL, SC#3) is a "fail" → the core rolls
// back. All ROCm/HSA/fault markers stay behind ResidencyProof() — this function is
// literal-free of them.
func liveProve(ctx context.Context, target string) backendswap.ProveVerdict {
	// Resolve the backend fail-closed (D-02): an unknown target is a prove fail, never
	// a silent fallback. backend.ResidencyProof() is the ONLY source of backend markers.
	backend, err := inference.BackendFor(target)
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: err.Error()}
	}

	// Load the source of truth for the residency seams (ConfigModel/ConfigContext) and
	// the probe's model id. A config-load failure is a prove fail.
	cfg, err := config.LoadVilla()
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: "load config: " + err.Error()}
	}

	// Resolve the catalog-resolved GGUF filename ONCE for ConfigModel — the SAME
	// concrete seam status.go uses (cmd/villa/lifecycle.go liveModelFile), never a
	// placeholder. Its error is a prove fail.
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return backendswap.ProveVerdict{Status: "fail", Detail: "resolve model file: " + err.Error()}
	}

	// Derive the inference endpoint the SAME way the status path does (mirror
	// status.go:150): the resolved backend's container runner, never a hand-rolled URL.
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

	// Bound the WHOLE prove by proveTimeout (Pitfall 2 / T-8-07). A never-ready server
	// trips this deadline and is a FAIL, not an infinite wait.
	deadlineCtx, cancel := context.WithTimeout(ctx, proveTimeout)
	defer cancel()

	// (a) Bounded readiness. PollHealth is the EXPORTED non-container readiness gate
	// over the already-running server; a 200 means "accepting requests", not "offload
	// happened" — that is gate (c). Never-ready before the deadline → FAIL.
	ready := inference.PollHealth(deadlineCtx, endpoint, proveTimeout)
	if !ready.Known || !ready.Value {
		return backendswap.ProveVerdict{
			Status: "fail",
			Detail: "not ready before timeout (possible load_tensors hang or CPU-fallback stall)",
		}
	}

	// (b) REAL generation probe, with the live gpu_busy_percent sampled DURING the
	// decode window (D-07 / Assumption A2 / Open Question 3). Start the probe in a
	// goroutine and poll detect.GPUBusyPercent() repeatedly while tokens stream, keeping
	// the max — a single post-probe read can miss a short decode's busy window.
	chatCh := make(chan inference.ChatResult, 1)
	go func() {
		chatCh <- inference.GenerationProbe(deadlineCtx, endpoint, cfg.Model)
	}()

	maxBusy := detect.UnknownInt("gpu_busy_percent not sampled during probe", "")
	ticker := time.NewTicker(busySampleInterval)
	defer ticker.Stop()
sampleLoop:
	for {
		// Sample once up front and on every tick so even a very short decode gets at
		// least one in-flight read.
		if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
			maxBusy = b
		}
		select {
		case chat := <-chatCh:
			// One last read at completion, then stop sampling.
			if b := detect.GPUBusyPercent(); b.Known && (!maxBusy.Known || b.Value > maxBusy.Value) {
				maxBusy = b
			}
			if !chat.OK || chat.Tokens == 0 {
				detail := "generation probe returned no tokens"
				if chat.Detail != "" {
					detail = "generation probe failed: " + chat.Detail
				}
				return backendswap.ProveVerdict{Status: "fail", Detail: detail}
			}
			break sampleLoop
		case <-ticker.C:
			// keep sampling
		case <-deadlineCtx.Done():
			return backendswap.ProveVerdict{
				Status: "fail",
				Detail: "generation probe did not complete before timeout (possible load_tensors hang or CPU-fallback stall)",
			}
		}
	}

	// (c) Residency proof. Read the INVOCATION-scoped journal (F-3 — ResidencyJournal,
	// not the whole-unit journal whose oldest bytes are stale prior-start output), then
	// fold the journal + GTT floor + the DURING-probe gpu_busy reading + the target
	// backend's markers into one Verdict. WeightBytes/ConfigModel/ConfigContext mirror
	// internal/status/status.go exactly via the concrete liveWeightBytes(cfg)/
	// liveModelFile(cfg)/cfg.Ctx seams — never placeholders. Markers come ONLY from
	// backend.ResidencyProof() (keeps this file literal-free, T-8-11).
	journal, _ := orchestrate.NewSystemd().ResidencyJournal(installServiceName)
	v := inference.RunningOffloadVerdict(inference.RunningOffloadInput{
		JournalText:    journal,
		GTTUsedBytes:   detect.GTTUsedBytes(),
		GPUBusyPercent: maxBusy,
		WeightBytes:    liveWeightBytes(cfg),
		ConfigModel:    modelFile,
		ConfigContext:  cfg.Ctx,
		Markers:        backend.ResidencyProof(),
	})

	// Map ONLY a true StatusPass to ProveStatusPass; everything else (FAIL/WARN, incl.
	// ready+200-but-residency-FAIL) is a fail → the core rolls back (SC#3).
	if v.Status == inference.StatusPass {
		return backendswap.ProveVerdict{Status: backendswap.ProveStatusPass, Detail: v.Detail}
	}
	return backendswap.ProveVerdict{Status: "fail", Detail: v.Detail}
}
