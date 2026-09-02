package residency

import (
	"context"
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// proofspec.go is the third piece of the under-load protocol: the shape of one
// WHOLE proof, gate and honesty mapping included.
//
// Doctor's three under-load proofs (embed load, search load, tool-call load)
// were acknowledged clones — one carried the comment "a verbatim-in-shape clone
// with ONLY the drive swapped". Each re-stated the read-only precondition gate
// and the no-false-green honesty mapping above UnderLoad. Both are doctrine
// (ADR-0001: offload is proven, never assumed; an unmet precondition or an
// unexercised stack degrades to a typed-Unknown WARN, never a fabricated
// verdict), so they are stated here ONCE. A proof for a future subsystem is a
// ProofSpec — subject, required services, target resolution, drive — not a
// 70-line clone.

// ProofSpec describes one whole residency-under-load proof: the precondition
// gate, what is being proven, the workload, and the caller's wording for the
// two ways the proof can degrade.
type ProofSpec struct {
	// Subject names the proof in every degradation, e.g. "residency under
	// embedding load".
	Subject string

	// IsActive answers the read-only precondition gate for one service. The
	// proof NEVER starts a service: an inactive unit degrades to a typed-Unknown
	// WARN naming it, never a FAIL fabricated from a stack that simply is not
	// running.
	IsActive func(service string) (state string, err error)
	// Services are the units that must be active before the drive is honest.
	Services []string

	// ResolveTarget resolves WHAT is being proven, after the gate passes. A
	// resolution failure returns the caller's typed-Unknown WARN, never a FAIL.
	ResolveTarget func() (Target, *inference.Verdict)

	// Load is the workload and sampling discipline handed to UnderLoad.
	Load Load

	// Unsampled words the degradation when no round could be sampled under
	// load — the "under load" precondition was never met, so the verdict MUST
	// NOT be reported.
	Unsampled func(r LoadResult) inference.Verdict
	// Faltered, when non-nil, words the degradation for a PASS sampled under a
	// faltering drive (LoadResult.DriveFaltered). Nil means the discipline does
	// not second-guess drive errors — the settle-based proofs sample only a
	// round that is verifiably in flight, so the sampled round itself is the
	// load evidence.
	Faltered func(r LoadResult) inference.Verdict
}

// ProveUnderLoad runs one whole proof: gate, target resolution, drive, honesty
// mapping. It owns the ORDER (gate before target before drive) and the
// doctrine; the spec owns the workload and the wording.
func ProveUnderLoad(ctx context.Context, d Deps, s ProofSpec) inference.Verdict {
	for _, svc := range s.Services {
		if state, err := s.IsActive(svc); err != nil || state != "active" {
			return Unevaluable(
				fmt.Sprintf("could not evaluate %s — %s is not active", s.Subject, svc),
				fmt.Sprintf("check `systemctl --user status %s`; run `villa up` if the stack is stopped, then re-run `villa doctor`", svc))
		}
	}
	target, warn := s.ResolveTarget()
	if warn != nil {
		return *warn
	}
	res := UnderLoad(ctx, d, target, s.Load)
	if !res.Sampled {
		return s.Unsampled(res)
	}
	if s.Faltered != nil && res.DriveFaltered() {
		return s.Faltered(res)
	}
	return res.Verdict
}
