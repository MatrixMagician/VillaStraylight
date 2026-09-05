package backendswap

import (
	"bytes"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

// speculation_test.go drives RunSpeculation through the same fake Deps as the
// backend switch, because it is the same transaction with a different mutation.

// specStub wires a recorder whose loaded config carries the given persisted mode.
func specStub(rec *swapRecorder, persisted string) Deps {
	d := newSwapStub(rec)
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{Model: "preserved-model", Backend: "rocm", Speculation: persisted}, nil
	}
	return d
}

// TestSpeculationNoOp: an unset config already runs off, so setting off is a clean
// no-op with zero side effects.
func TestSpeculationNoOp(t *testing.T) {
	rec := passStub()
	res := RunSpeculation(specStub(rec, ""), config.SpeculationOff)
	if !res.NoOp {
		t.Fatalf("setting off on an unset config = %+v, want NoOp", res)
	}
	if res.From != config.SpeculationOff || res.To != config.SpeculationOff {
		t.Errorf("From/To = %q/%q, want off/off", res.From, res.To)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a no-op touched the host: %v", rec.callOrder)
	}
}

// TestSpeculationRefusesUnqualifiedTarget: the fit guard is what carries
// ResolveSpeculation's refusal, so an unqualified target refuses with the note as
// the reason and ZERO side effects.
func TestSpeculationRefusesUnqualifiedTarget(t *testing.T) {
	rec := passStub()
	rec.fitOK = false
	rec.fitReason = "speculation: ngram requested but m is not qualified for it; refusing"
	res := RunSpeculation(specStub(rec, config.SpeculationOff), config.SpeculationNgram)
	if !res.Refused {
		t.Fatalf("res = %+v, want Refused", res)
	}
	if !strings.Contains(res.Reason, "not qualified") {
		t.Errorf("Reason = %q, want the resolver's note", res.Reason)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a refusal touched the host: %v", rec.callOrder)
	}
}

// TestSpeculationSuccessPersistsTheMode: a proven cutover leaves the new mode in
// config and restarts only the inference service.
func TestSpeculationSuccessPersistsTheMode(t *testing.T) {
	rec := passStub()
	res := RunSpeculation(specStub(rec, config.SpeculationOff), config.SpeculationNgram)
	if !res.Switched {
		t.Fatalf("res = %+v, want Switched", res)
	}
	if rec.saved.Speculation != config.SpeculationNgram {
		t.Errorf("persisted speculation = %q, want ngram", rec.saved.Speculation)
	}
	if rec.saved.Backend != "rocm" {
		t.Errorf("the backend was mutated to %q; only the mode should change", rec.saved.Backend)
	}
	if len(rec.restarted) != 1 || rec.restarted[0] != installService {
		t.Errorf("restarted %v, want only %s", rec.restarted, installService)
	}
	if i, j := indexOf(rec.callOrder, "capture"), indexOf(rec.callOrder, "save"); i < 0 || j < 0 || i > j {
		t.Errorf("capture must precede save, got %v", rec.callOrder)
	}
}

// TestSpeculationMutateFailureRollsBack: a write error restores the verbatim prior
// unit and the prior config, so the persisted mode never outlives a failed cutover.
func TestSpeculationMutateFailureRollsBack(t *testing.T) {
	rec := passStub()
	rec.writeErr = errors.New("write failed")
	res := RunSpeculation(specStub(rec, config.SpeculationOff), config.SpeculationNgram)
	if !res.RolledBack || res.FailedStep != "write" {
		t.Fatalf("res = %+v, want RolledBack at write", res)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("restored unit is not the captured prior bytes")
	}
	if rec.saved.Speculation != config.SpeculationOff {
		t.Errorf("config left at %q after rollback, want the prior off", rec.saved.Speculation)
	}
}

// TestSpeculationProveFailureRollsBack: a non-pass verdict is a rollback, so a mode
// that cannot be proven on the running server is never left persisted.
func TestSpeculationProveFailureRollsBack(t *testing.T) {
	rec := passStub()
	rec.proveStatus = prove.StatusFail
	rec.proveDetail = "residency FAIL"
	res := RunSpeculation(specStub(rec, config.SpeculationOff), config.SpeculationNgram)
	if !res.RolledBack || res.FailedStep != "prove" {
		t.Fatalf("res = %+v, want RolledBack at prove", res)
	}
	if rec.saved.Speculation != config.SpeculationOff {
		t.Errorf("config left at %q after rollback, want the prior off", rec.saved.Speculation)
	}
}

// TestSpeculationProvesTheBackend: the cutover proof is a residency proof of the
// backend the mutated config runs, never of the speculation target. Proving "ngram"
// as a backend name failed BackendFor and rolled every live swap back (found on the
// dev host).
func TestSpeculationProvesTheBackend(t *testing.T) {
	rec := passStub()
	res := RunSpeculation(specStub(rec, config.SpeculationOff), config.SpeculationNgram)
	if !res.Switched {
		t.Fatalf("res = %+v, want Switched", res)
	}
	if rec.proveSaw != "rocm" {
		t.Errorf("Prove saw target %q, want the config backend \"rocm\"", rec.proveSaw)
	}
}
