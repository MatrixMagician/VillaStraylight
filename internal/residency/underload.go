package residency

import (
	"context"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// underload.go is the second residency-proof protocol: proving the served model is
// resident WHILE a real workload runs against it.
//
// It differs from Prove in what drives the load and when the sample is safe to take.
// Prove drives its own generation probe and samples throughout the decode. Under
// load, the workload belongs to the caller — embedding requests, coding-agent
// tool-call round-trips, search-augmented chat rounds — and the sample is only
// honest while one of those is verifiably IN FLIGHT. Sampling an idle stack is the
// exact false-green this protocol exists to prevent: a CPU fallback under load looks
// identical to a healthy stack once the load stops.
//
// Doctor implemented this three times, twice at 50 identical lines out of 65.

// Load describes the workload and the sampling discipline. It is separate from
// Target, which describes what is being proven, because the same target can be
// proven under different loads.
type Load struct {
	// Drive runs ONE round of the caller's workload, synchronously. Its error is
	// counted but is not itself the failure signal: what is being proven is
	// residency, not the round's success.
	Drive func(ctx context.Context) error
	// Rounds bounds how many rounds will be driven.
	Rounds int
	// Warmup is how many rounds must COMPLETE before a sample is attempted, so the
	// sample lands after the stack has demonstrably done real work. Zero samples on
	// the first round.
	Warmup int
	// Settle is how long to wait after launching a round before sampling, and it
	// selects the sampling discipline:
	//
	//   - Settle > 0: sample ONLY IF the round is still in flight at the deadline. A
	//     round that finished sooner was too fast to have loaded the model under
	//     observation, so it is joined and the next round driven. This is the
	//     discipline for heavyweight rounds (a coding-agent round-trip, a
	//     search-augmented chat) that may exit early on error.
	//   - Settle == 0: sample immediately after launching, without confirming the
	//     round is still running. This is the discipline for cheap, uniform rounds
	//     (embedding requests) driven after a warmup: they are too short to be caught
	//     by a settle deadline, so requiring one would mean never sampling at all.
	//     The load evidence here is the WARMUP — the stack has demonstrably completed
	//     real rounds — rather than a confirmed in-flight instant. It is the weaker
	//     of the two disciplines, so prefer Settle > 0 whenever rounds are long
	//     enough to be caught.
	Settle time.Duration
	// RoundTimeout bounds each individual round. Zero means only Budget applies.
	RoundTimeout time.Duration
	// Budget bounds the WHOLE proof: drive, sample and join.
	Budget time.Duration
	// DriveAllRounds keeps driving the remaining rounds after the sample is taken.
	// False stops at the sample, which is what a heavyweight round wants.
	DriveAllRounds bool
}

// LoadResult is the outcome of a proof under load: the verdict plus the facts a
// caller needs to judge whether the verdict was honestly obtained.
//
// The verdict is only meaningful when Sampled is true. The caller supplies the
// wording of a degradation because the remediation differs per workload; the
// protocol owns WHEN to degrade, via Sampled and DriveFaltered.
type LoadResult struct {
	// Verdict is the residency fold taken during the in-flight sample. Zero when
	// Sampled is false.
	Verdict inference.Verdict
	// Sampled reports whether a sample was actually taken under load. False means
	// the "under load" precondition was never met, and the caller MUST degrade to an
	// unevaluable WARN rather than report a verdict.
	Sampled bool
	// Completed and DriveErrs count rounds that finished and rounds that errored.
	Completed int
	DriveErrs int
	// Rounds echoes the bound, so a caller can report "n of m".
	Rounds int
}

// DriveFaltered reports a PASS sampled under a workload that was not fully
// exercising the stack. A PASS obtained while the drive was erroring is not a proven
// PASS, so the caller degrades it to unevaluable. A confident FAIL, or an already
// unevaluable WARN, stands on its own and is not second-guessed here.
func (r LoadResult) DriveFaltered() bool {
	return r.DriveErrs > 0 && r.Verdict.Status == inference.StatusPass
}

// UnderLoad drives the caller's workload and samples the residency fold while a
// round is in flight.
//
// Every launched round is JOINED before the call returns, so no probe process or
// --rm container ever outlives it.
func UnderLoad(ctx context.Context, d Deps, t Target, l Load) LoadResult {
	ctx, cancel := context.WithTimeout(ctx, l.Budget)
	defer cancel()

	res := LoadResult{Rounds: l.Rounds}
	for range l.Rounds {
		if ctx.Err() != nil {
			break // budget exhausted — stop driving
		}
		if res.Sampled && !l.DriveAllRounds {
			break
		}

		roundCtx, roundCancel := ctx, context.CancelFunc(func() {})
		if l.RoundTimeout > 0 {
			roundCtx, roundCancel = context.WithTimeout(ctx, l.RoundTimeout)
		}

		// Not yet eligible to sample: drive this round synchronously as warmup.
		if res.Sampled || res.Completed < l.Warmup {
			if err := l.Drive(roundCtx); err != nil {
				res.DriveErrs++
			}
			roundCancel()
			res.Completed++
			continue
		}

		// Eligible: launch the round so the sample can land while it runs. started
		// closes once the goroutine has actually entered Drive, so "in flight" is a
		// fact rather than an assumption about goroutine scheduling.
		done := make(chan error, 1)
		started := make(chan struct{})
		go func() {
			close(started)
			done <- l.Drive(roundCtx)
		}()
		<-started

		if l.Settle == 0 {
			// Cheap uniform round: it is in flight, so sample now.
			res.Verdict = sampleFold(d, t)
			res.Sampled = true
			if err := <-done; err != nil { // JOIN
				res.DriveErrs++
			}
			roundCancel()
			res.Completed++
			continue
		}

		settle := time.NewTimer(l.Settle)
		select {
		case err := <-done:
			// Finished before the settle deadline: too fast to have loaded the model
			// under observation. Never sample idle — drive the next round instead.
			settle.Stop()
			if err != nil {
				res.DriveErrs++
			}
			roundCancel()
			res.Completed++
		case <-ctx.Done():
			settle.Stop()
			if err := <-done; err != nil { // JOIN: never let a probe outlive the call
				res.DriveErrs++
			}
			roundCancel()
			res.Completed++
		case <-settle.C:
			// Verifiably still in flight — this is the only honest moment to sample.
			res.Verdict = sampleFold(d, t)
			res.Sampled = true
			if err := <-done; err != nil { // JOIN
				res.DriveErrs++
			}
			roundCancel()
			res.Completed++
		}
	}
	return res
}

// Unevaluable builds the typed-Unknown WARN every unmet precondition and every
// unevaluable drive degrades to. It is never a false-green PASS and never a FAIL
// fabricated from a signal that could not be evaluated — the Unknown-versus-negative
// rule, stated once for all three doctor proofs that used to each declare their own
// constructor.
func Unevaluable(detail, remediation string) inference.Verdict {
	return inference.Verdict{
		Status:      inference.StatusWarn,
		Detail:      detail,
		Remediation: remediation,
	}
}

// sampleFold reads the residency signals and folds them into a verdict. It is the
// same fold Prove ends with, minus the GPU-busy sampler: under load the workload is
// the caller's, so the corroborating signals are the journal, the /props overlay and
// the GTT floor read at the in-flight instant.
func sampleFold(d Deps, t Target) inference.Verdict {
	journal, _ := d.Journal(t.Service)
	in := inference.RunningOffloadInput{
		JournalText:   journal,
		GTTUsedBytes:  d.GTTUsed(),
		WeightBytes:   t.WeightBytes,
		ConfigModel:   t.ModelFile,
		ConfigContext: t.ContextLen,
		Markers:       t.Markers,
	}
	if d.Props != nil {
		in.Props = d.Props()
	}
	return d.Fold(in)
}
