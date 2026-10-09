package voice

// offload.go is where the voice proof asks where whisper ran (#332). An agreeing
// round trip proves the pair works, not that villa-stt transcribed on the GPU: with
// its Vulkan device lost, whisper falls back to the CPU and still hears the
// sentence, slower. ADR-0001's rule is that such a fallback is a FAIL, so the round
// trip is run as the drive of residency.Prove, unchanged, the way the image proof
// runs its txt2img: GPU busy is sampled while the round trip runs, and the fold
// reads villa-stt's journal.

import (
	"context"
	"fmt"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/verify"
)

// ProveOnGPU runs Prove as the residency drive and folds the placement verdict
// into it. Readiness is Prove's own bounded retry, so r.PollHealth and r.Generate
// are set here and the caller's values are ignored.
//
//   - a round trip that did not pass is returned as it was;
//   - a passing round trip the fold never judged (t's deadline came first) is a
//     Reject;
//   - placement PASS is a Pass, FAIL (a CPU fallback) is a Fail with the
//     remediation, and WARN is a Reject: an unreadable placement proves nothing.
func ProveOnGPU(ctx context.Context, d Driver, r residency.Deps, t residency.Target) verify.Proof {
	trips := make(chan verify.Proof, 1)
	r.PollHealth = func(context.Context, time.Duration) detect.Bool {
		return detect.KnownBool(true, "the round trip retries a unit that is still starting")
	}
	r.Generate = func(context.Context, string) inference.ChatResult {
		trip := Prove(d)
		trips <- trip
		return inference.ChatResult{OK: trip.Status == verify.Pass, Tokens: 1, Detail: trip.Detail}
	}
	fold, folded := r.Fold, false
	r.Fold = func(in inference.RunningOffloadInput) inference.Verdict {
		folded = true
		return fold(in)
	}
	v := residency.Prove(ctx, r, t)

	select {
	case trip := <-trips:
		if trip.Status != verify.Pass {
			return trip
		}
		if folded {
			return placement(trip, v)
		}
	default:
	}
	return verify.Proof{Status: verify.Reject, Detail: fmt.Sprintf("the round trip did not finish (%s)", v.Detail) + checkSTT}
}

// placement maps the fold's verdict over a passing round trip.
func placement(trip verify.Proof, v inference.Verdict) verify.Proof {
	switch v.Status {
	case inference.StatusPass:
		return verify.Proof{Status: verify.Pass, Detail: trip.Detail + "; whisper on the GPU: " + v.Detail}
	case inference.StatusFail:
		return verify.Proof{Status: verify.Fail, Detail: fmt.Sprintf("whisper is not running on the GPU (%s): %s", v.Detail, v.Remediation) + checkSTT}
	default:
		return verify.Proof{Status: verify.Reject, Detail: fmt.Sprintf("could not tell where whisper runs (%s)", v.Detail) + checkSTT}
	}
}
