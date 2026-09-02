package install

import "testing"

// TestOutcomeExitCodes pins the outcome-to-exit-code contract: 0 for a clean
// completion, a dry run and a true no-op; 2 for passed-with-warnings; 1 for a
// pre-mutation block and a post-mutation refusal. Stated once here — a return
// site never re-derives it.
func TestOutcomeExitCodes(t *testing.T) {
	cases := []struct {
		o    Outcome
		want int
	}{
		{Completed, 0},
		{DryRun, 0},
		{NoChange, 0},
		{Degraded, 2},
		{Blocked, 1},
		{Refused, 1},
	}
	for _, c := range cases {
		if got := c.o.ExitCode(); got != c.want {
			t.Errorf("%s.ExitCode() = %d, want %d", c.o, got, c.want)
		}
	}
}

// TestFinishFoldsDegradation proves the terminal fold: a run that reached the
// end completes cleanly only when neither degradation flag is set, and either
// flag alone degrades it — a bypassed gate is not a satisfied one.
func TestFinishFoldsDegradation(t *testing.T) {
	var clean Result
	if got := clean.Finish(); got != Completed {
		t.Errorf("clean Finish() = %s, want %s", got, Completed)
	}
	gate := Result{GateDegraded: true}
	if got := gate.Finish(); got != Degraded {
		t.Errorf("gate-degraded Finish() = %s, want %s", got, Degraded)
	}
	ready := Result{ReadinessWarn: true}
	if got := ready.Finish(); got != Degraded {
		t.Errorf("readiness-warn Finish() = %s, want %s", got, Degraded)
	}
	noop := Result{Outcome: NoChange}
	if got := noop.Finish(); got != NoChange {
		t.Errorf("clean no-change Finish() = %s, want %s", got, NoChange)
	}
	noopDegraded := Result{Outcome: NoChange, GateDegraded: true}
	if got := noopDegraded.Finish(); got != Degraded {
		t.Errorf("degraded no-change Finish() = %s, want %s (a bypassed gate degrades even a no-op)", got, Degraded)
	}
}

// TestBlockAndRefuseRecordTheirStory proves the two stop paths carry their
// reasons: a Block is pre-mutation (no rollback claim), a Refuse is
// post-mutation and records what the rollback said.
func TestBlockAndRefuseRecordTheirStory(t *testing.T) {
	var b Result
	if code := b.Block("install: cannot read the persisted config"); code != 1 {
		t.Errorf("Block exit = %d, want 1", code)
	}
	if b.Outcome != Blocked || b.Reason == "" || b.RollbackReason != "" {
		t.Errorf("Block result = %+v, want Blocked with a reason and no rollback claim", b)
	}

	var r Result
	if code := r.Refuse("install: start villa-llama.service failed", "restored the captured state"); code != 1 {
		t.Errorf("Refuse exit = %d, want 1", code)
	}
	if r.Outcome != Refused || r.Reason == "" || r.RollbackReason == "" {
		t.Errorf("Refuse result = %+v, want Refused carrying both the reason and the rollback story", r)
	}
}
