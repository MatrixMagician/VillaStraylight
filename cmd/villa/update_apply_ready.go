package main

// update_apply_ready.go is the readiness wait in front of the update proofs that
// drive a service an earlier subsystem's apply restarted (#359).
//
// An update's mutation restarts every running service whose unit the apply rewrote
// (ADR-0038), and the subsystems update in order: inference, chat, memory. So the
// embedder and Open WebUI are often restarted by the inference update and then
// proven, still loading, by the next subsystem's ProveCurrent. llama-server answers
// 503 while its model loads, and Open WebUI refuses connections while it boots. A
// proof that fires into that window reads a healthy stack as broken, and the
// refusal says the subsystem "was not healthy before the update".
//
// The wait is install's (#335), not a second one: pollReadiness polls, and the
// in-network /health probe is the one memoryProofWith builds.

import (
	"context"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inprobe"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

// updateReadyDeps is the host the readiness wait polls: the in-network curl for the
// closed-network llama-servers, Open WebUI's own health call over loopback, and the
// bound on the wait.
type updateReadyDeps struct {
	exec       inprobe.Exec
	image      string
	owuiHealth func(context.Context) error
	timeout    time.Duration
	interval   time.Duration
}

// liveUpdateReady binds the wait to the real host. It is a var so a test can drive
// the whole flow's wiring with a fake, as liveProofFuncs is.
var liveUpdateReady = func(cfg config.VillaConfig) updateReadyDeps {
	return updateReadyDeps{
		exec:  runClosedProbeCurlCode,
		image: orchestrate.EmbedImage(),
		owuiHealth: func(ctx context.Context) error {
			return liveOpenWebUIClient(owuiLoopbackBase(cfg.ChatPort)).Health(ctx)
		},
		timeout:  readinessTimeout,
		interval: readinessInterval,
	}
}

// awaitReady wraps a proof so it runs only once the services it drives answer. A
// wait that expires is a Reject: villa could not conduct the proof, which is not
// evidence the subsystem is broken, and the detail names the unit that never came up.
func awaitReady(d updateReadyDeps, cfg config.VillaConfig, prove func(context.Context, subsystem.Kind) updateflow.Proof) func(context.Context, subsystem.Kind) updateflow.Proof {
	return func(ctx context.Context, k subsystem.Kind) updateflow.Proof {
		if err := d.await(ctx, cfg, k); err != nil {
			return updateflow.Proof{Status: updateflow.ProofReject, Detail: err.Error()}
		}
		return prove(ctx, k)
	}
}

// await waits, in turn, for each service the kind's proof drives. Chat's proof asks
// Open WebUI; memory's drives Open WebUI, the embedder and, with its gate on, the
// reranker. Every other proof reaches no service an update's apply restarts for
// another subsystem and keeps its own handling.
func (d updateReadyDeps) await(ctx context.Context, cfg config.VillaConfig, k subsystem.Kind) error {
	if k != subsystem.Chat && k != subsystem.Memory {
		return nil
	}
	if k == subsystem.Memory {
		if err := d.awaitClosed(ctx, "villa-embed.service", config.EmbedAddr, config.EmbedPort); err != nil {
			return err
		}
		if subsystem.RerankOn(cfg) {
			if err := d.awaitClosed(ctx, "villa-rerank.service", config.RerankAddr, config.RerankPort); err != nil {
				return err
			}
		}
	}
	probe := func() (int, error) {
		if err := d.owuiHealth(ctx); err != nil {
			return 0, err
		}
		return 200, nil
	}
	return d.poll(ctx, "villa-openwebui.service", probe)
}

// awaitClosed polls a closed-network llama-server's /health through install's probe.
func (d updateReadyDeps) awaitClosed(ctx context.Context, unit, addr string, port int) error {
	return d.poll(ctx, unit, closedHealthProbe(ctx, d.exec, d.image, addr, port))
}

func (d updateReadyDeps) poll(ctx context.Context, unit string, probe func() (int, error)) error {
	if ready := pollReadiness(ctx, probe, d.timeout, d.interval); ready.Status != preflight.StatusPass {
		return fmt.Errorf("%s not ready after %s: %s", unit, d.timeout, ready.Detail)
	}
	return nil
}
