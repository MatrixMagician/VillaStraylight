package install

// result.go is the typed outcome of one whole install flow — the expand half of
// deepening the install flow behind this package (the updateflow precedent,
// where cmd renders a Result rather than deciding as it goes).
//
// The flow body still lives in the cmd tier for now; it accumulates a Result as
// it runs and derives its exit code HERE, so the outcome-to-exit-code mapping
// is a tested property of this module rather than sixty scattered return
// statements. The contract half (moving the flow itself behind Run) builds on
// this.

// Outcome is the terminal state of one install run.
type Outcome string

const (
	// Completed: the stack was brought up and every readiness proof passed.
	Completed Outcome = "completed"
	// Degraded: the stack came up, but a gate was force-overridden or readiness
	// timed out — passed with warnings, not a clean bring-up.
	Degraded Outcome = "degraded"
	// DryRun: the plan was printed and NOTHING was mutated (no write, no pull,
	// no persist).
	DryRun Outcome = "dry_run"
	// NoChange: a true no-op — units already match config; weights, config and
	// the dashboard unit were still ensured.
	NoChange Outcome = "no_change"
	// Blocked: the flow stopped before mutating anything (an unreadable config,
	// a failed gate, an unfit host, a failed download). Nothing to roll back.
	Blocked Outcome = "blocked"
	// Refused: the flow stopped AFTER mutation began, so ADR-0003's rollback was
	// attempted. Whether it fully restored is the RollbackReason's story.
	Refused Outcome = "refused"
)

// ExitCode maps an outcome to the process exit code contract: 0 pass, 2 passed
// with warnings, 1 blocked/refused. Stated once, tested here — never re-derived
// at a return site.
func (o Outcome) ExitCode() int {
	switch o {
	case Completed, DryRun:
		return 0
	case NoChange:
		return 0
	case Degraded:
		return 2
	}
	return 1
}

// Result is the outcome of one install flow: what it reached, why it stopped
// if it stopped, and what the rollback claimed if one ran.
type Result struct {
	Outcome Outcome
	// Reason is the refusal or block wording, already printed by the flow. It is
	// recorded so the outcome carries its own story.
	Reason string
	// RollbackReason is the rollback's honesty line (install.Rollback's Reason)
	// when Outcome is Refused: a full restoration or an explicit partial one —
	// never a wrong "restored" claim.
	RollbackReason string
	// GateDegraded records a force-overridden host-prep gate: the gap was
	// bypassed, not satisfied, so even a clean bring-up degrades to WARN.
	GateDegraded bool
	// ReadinessWarn records a readiness poll that timed out rather than proved.
	ReadinessWarn bool
}

// Finish folds the degradation flags into the terminal outcome for a run that
// reached the end of the flow: any degradation makes Degraded, otherwise
// Completed. The two flags fold identically on the no-change path (NoChange
// stays NoChange only when clean).
func (r *Result) Finish() Outcome {
	if r.GateDegraded || r.ReadinessWarn {
		r.Outcome = Degraded
	} else if r.Outcome == "" {
		r.Outcome = Completed
	}
	return r.Outcome
}

// Block records a pre-mutation stop and returns its exit code.
func (r *Result) Block(reason string) int {
	r.Outcome = Blocked
	r.Reason = reason
	return r.Outcome.ExitCode()
}

// Refuse records a post-mutation stop (rollback attempted) and returns its
// exit code.
func (r *Result) Refuse(reason, rollbackReason string) int {
	r.Outcome = Refused
	r.Reason = reason
	r.RollbackReason = rollbackReason
	return r.Outcome.ExitCode()
}
