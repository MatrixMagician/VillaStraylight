package main

// update_proofs.go binds each subsystem to the proof it ALREADY has.
//
// Nothing here is a new proof. The residency proof plus a real generation probe for
// inference, the Open WebUI protocol probes for chat, and `verify memory` /
// `verify search` / `verify agent` for the addons — each reused verbatim. A second
// implementation of any of them would be a second opinion about whether the stack
// works, and the two would eventually disagree at the worst moment.
//
// # Fail and Reject
//
// The mapping is the load-bearing part of this file. The existing verify family
// already distinguishes a proof that RAN and failed from one that could not be
// CONDUCTED, and that distinction is exactly what `update` needs: only the first is
// evidence that something is broken. The second becomes a Reject, and the user is
// told villa cannot show the new state works — not that it does not.

import (
	"context"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// init binds the live proofs. It is a var-assignment in init rather than a literal
// map so the entries can name functions declared further down this file without an
// initialisation cycle.
func init() {
	liveProofFuncs = map[subsystem.Kind]func(context.Context) updateflow.Proof{
		subsystem.Inference: proveInference,
		subsystem.Chat:      proveChat,
		subsystem.Memory:    proveMemory,
		subsystem.WebSearch: proveSearch,
		subsystem.Agent:     proveAgent,
	}
}

// proveInference runs the residency proof plus a real generation probe — the same
// liveProve the backend swap gates its cutover on.
//
// A non-pass here is a REJECT rather than a Fail, and that is deliberate. The
// residency proof's own rule is that an unevaluable signal is not a confident
// negative, and marker drift — the exact failure ADR-0001 predicted for a rebuilt
// image — produces precisely that: markers absent from a log villa no longer
// recognises. Calling it a Fail would tell the user upstream shipped something
// broken, when what happened is that villa could not tell.
func proveInference(ctx context.Context) updateflow.Proof {
	cfg, err := config.LoadVilla()
	if err != nil {
		return updateflow.Proof{Status: updateflow.ProofReject, Detail: "could not read the config: " + err.Error()}
	}
	v := liveProve(ctx, cfg.Backend)
	if v.Status == prove.StatusPass {
		return updateflow.Proof{Status: updateflow.ProofPass, Detail: v.Detail}
	}
	return updateflow.Proof{
		Status: updateflow.ProofReject,
		Detail: v.Detail + "\n\n    Residency markers are pinned to the log format villa was tested against,\n" +
			"    and that format changes as the upstream image is rebuilt.",
	}
}

// proveChat runs the Open WebUI protocol probe over the loopback chat port.
//
// A health failure is a REJECT, not a Fail: an unreachable service tells villa
// nothing about whether the new image is good, only that it could not ask.
func proveChat(ctx context.Context) updateflow.Proof {
	cfg, err := config.LoadVilla()
	if err != nil {
		return updateflow.Proof{Status: updateflow.ProofReject, Detail: "could not read the config: " + err.Error()}
	}
	client := liveOpenWebUIClient(owuiLoopbackBase(cfg.ChatPort))
	if err := client.Health(ctx); err != nil {
		return updateflow.Proof{
			Status: updateflow.ProofReject,
			Detail: fmt.Sprintf("Open WebUI did not answer its health probe on the loopback chat port: %v", err),
		}
	}
	return updateflow.Proof{Status: updateflow.ProofPass, Detail: "Open WebUI answered its protocol probes"}
}

// proveMemory runs the same RAG smoke proof `villa verify memory` runs.
func proveMemory(ctx context.Context) updateflow.Proof {
	cfg, err := config.LoadVilla()
	if err != nil {
		return updateflow.Proof{Status: updateflow.ProofReject, Detail: "could not read the config: " + err.Error()}
	}
	const (
		question = "What is the VillaStraylight runtime RAG smoke verification token?"
		wantFact = "VILLA-RAG-SMOKE-TOKEN-7741"
	)
	p := liveRagSmoke(ctx, ragSmokeInput{
		owuiAddr: verifyMemoryLoopbackAddr,
		owuiPort: cfg.ChatPort,
		question: question,
		wantFact: wantFact,
	})
	return fromVerifyProof(memoryProofOutcome(p))
}

// proveSearch runs the bounded-outbound proof `villa verify search` runs, through
// the same seam and the same verdict mapper the verb uses.
func proveSearch(ctx context.Context) updateflow.Proof {
	deps := liveVerifySearchDeps()
	return fromVerifyProof(searchProofOutcome(deps.verifyFn(ctx, deps)))
}

// proveAgent runs the agent proof `villa verify agent` runs.
//
// The verb maps a StatusFail to Fail and everything else to Pass, which is right
// for a verb whose job is a verdict. Here the middle case matters more: an agent
// probe that could not be conducted must not commit a pin, so it maps to Reject.
func proveAgent(ctx context.Context) updateflow.Proof {
	deps := liveVerifyAgentDeps()
	p := deps.verifyFn(ctx, deps)
	if p.status == preflight.StatusFail {
		return updateflow.Proof{Status: updateflow.ProofFail, Detail: p.detail}
	}
	return updateflow.Proof{Status: updateflow.ProofPass, Detail: p.detail}
}

// fromVerifyProof maps the verify family's vocabulary onto the update flow's.
//
// The two vocabularies already agree on the distinction that matters, which is why
// this is a translation and not a decision: verify.Reject means "the proof could not
// be CONDUCTED, so nothing was proven either way", and that is exactly what an
// update Reject means. A Skip becomes a Pass, because a subsystem with nothing to
// verify has nothing that could be broken by an update to it.
func fromVerifyProof(p verify.Proof) updateflow.Proof {
	switch p.Status {
	case verify.Pass, verify.Skip:
		return updateflow.Proof{Status: updateflow.ProofPass, Detail: p.Detail}
	case verify.Fail:
		return updateflow.Proof{Status: updateflow.ProofFail, Detail: p.Detail}
	default:
		return updateflow.Proof{Status: updateflow.ProofReject, Detail: p.Detail}
	}
}
