package backendswap

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// backendswap_test.go drives the transactional core through a fake Deps with no
// live host. It asserts the ordering contract (capture STRICTLY before any
// mutation, Pitfall 4), the verbatim rollback (RestoreUnit with the captured
// priorUnit), the prove-gate (switch ONLY on ProveStatusPass — is-active/200 alone
// is never success, SC#3), the refuse-with-remediation paths (fit/preflight leave
// ZERO side effects), and restart-inference-only. It mirrors the modelswap
// swapRecorder + callOrder discipline ([03-05]).

const installService = "villa-llama.service"

// priorUnitBytes is the verbatim prior unit the fake CaptureUnit returns, so the
// rollback tests can assert a byte-equal RestoreUnit.
var priorUnitBytes = []byte("[Container]\nImage=prior\nExec=llama-server --prior\n")

// swapRecorder records each side-effecting seam call so the tests can assert
// ordering (capture < save/write; RestoreUnit precedes the rollback Restart) and
// that ONLY the inference service is restarted.
type swapRecorder struct {
	callOrder []string
	saved     config.VillaConfig
	restarted []string
	captured  []byte
	restored  []byte

	// knobs (task 08-01-02 uses prove/refuse/failure knobs):
	fitOK        bool      // FitsModel result (true = fits)
	fitReason    string    // remediation reason on a non-fit
	preflightOK  bool      // PreflightROCm result
	preflight    string    // remediation reason on a preflight block
	captureErr   error     // CaptureUnit error (uncapturable prior unit)
	saveErr      error     // SaveConfig error (mutate failure)
	writeErr     error     // ReconcileAndWrite error (mutate failure)
	restartErr   error     // first Restart error (mutate failure)
	proveStatus  string    // Prove verdict Status (ProveStatusPass = pass)
	proveDetail  string    // Prove verdict Detail
	restoreErr   error     // RestoreUnit error during rollback (rollback-incomplete)
	rbRestartErr error     // Restart error during rollback (rollback-incomplete)
	currentBE    string    // current backend in the loaded config

	restartCalls int // counts Restart invocations to distinguish forward vs rollback
}

// newSwapStub builds a Deps wired to rec. Defaults: current backend "vulkan",
// fits=true, preflight=ok, prove=pass — a clean forward switch unless a knob flips.
func newSwapStub(rec *swapRecorder) Deps {
	if rec.currentBE == "" {
		rec.currentBE = "vulkan"
	}
	return Deps{
		InstallServiceName: installService,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "preserved-model", Backend: rec.currentBE}, nil
		},
		FitsModel: func(_ config.VillaConfig) (bool, string) {
			return rec.fitOK, rec.fitReason
		},
		PreflightROCm: func(_ config.VillaConfig) (bool, string) {
			return rec.preflightOK, rec.preflight
		},
		CaptureUnit: func() ([]byte, error) {
			if rec.captureErr != nil {
				return nil, rec.captureErr
			}
			rec.callOrder = append(rec.callOrder, "capture")
			rec.captured = append([]byte(nil), priorUnitBytes...)
			return rec.captured, nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			rec.callOrder = append(rec.callOrder, "save:"+c.Backend)
			rec.saved = c
			return rec.saveErr
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) {
			rec.callOrder = append(rec.callOrder, "write")
			if rec.writeErr != nil {
				return false, rec.writeErr
			}
			return true, nil
		},
		RestoreUnit: func(b []byte) error {
			rec.callOrder = append(rec.callOrder, "restore")
			rec.restored = append([]byte(nil), b...)
			return rec.restoreErr
		},
		DaemonReload: func() error {
			rec.callOrder = append(rec.callOrder, "daemon-reload")
			return nil
		},
		Restart: func(service string) error {
			rec.callOrder = append(rec.callOrder, "restart:"+service)
			rec.restarted = append(rec.restarted, service)
			rec.restartCalls++
			// First restart is the forward cutover; a later one is the rollback re-ready.
			if rec.restartCalls == 1 {
				return rec.restartErr
			}
			return rec.rbRestartErr
		},
		Prove: func(_ context.Context, _ string) ProveVerdict {
			status := rec.proveStatus
			if status == "" {
				status = ProveStatusPass
			}
			return ProveVerdict{Status: status, Detail: rec.proveDetail}
		},
	}
}

// indexOf returns the index of the first call whose label equals or is prefixed by
// want, or -1.
func indexOf(order []string, want string) int {
	for i, c := range order {
		if c == want || (len(want) > 0 && len(c) >= len(want) && c[:len(want)] == want) {
			return i
		}
	}
	return -1
}

// passStub is the baseline happy-path recorder: fits, preflight ok, prove pass.
func passStub() *swapRecorder {
	return &swapRecorder{fitOK: true, preflightOK: true, proveStatus: ProveStatusPass}
}

// TestCaptureBeforeMutate: capture index is STRICTLY less than the save and write
// indices (Pitfall 4 — capturing after mutation restores the wrong unit).
func TestCaptureBeforeMutate(t *testing.T) {
	rec := passStub()
	res := Run(newSwapStub(rec), "rocm")
	if !res.Switched {
		t.Fatalf("clean switch expected, got %+v", res)
	}
	capIdx := indexOf(rec.callOrder, "capture")
	saveIdx := indexOf(rec.callOrder, "save:")
	writeIdx := indexOf(rec.callOrder, "write")
	if capIdx < 0 || saveIdx < 0 || writeIdx < 0 {
		t.Fatalf("expected capture, save, write all recorded, got %v", rec.callOrder)
	}
	if !(capIdx < saveIdx && capIdx < writeIdx) {
		t.Errorf("capture must precede save AND write, got order %v", rec.callOrder)
	}
}

// TestSwapInferenceOnly: a successful switch restarts EXACTLY ["villa-llama.service"]
// (no Open WebUI / dashboard restart).
func TestSwapInferenceOnly(t *testing.T) {
	rec := passStub()
	res := Run(newSwapStub(rec), "rocm")
	if !res.Switched {
		t.Fatalf("clean switch expected, got %+v", res)
	}
	if len(rec.restarted) != 1 || rec.restarted[0] != installService {
		t.Errorf("expected only %s restarted, got %v", installService, rec.restarted)
	}
}

// TestNoOpSameBackend: a target equal to the current backend is a clean NoOp with
// ZERO save/write/restart/capture seams (modelswap no-op style).
func TestNoOpSameBackend(t *testing.T) {
	rec := passStub()
	rec.currentBE = "vulkan"
	res := Run(newSwapStub(rec), "vulkan")
	if !res.NoOp || res.Switched || res.RolledBack || res.Refused {
		t.Fatalf("same-backend target must be a clean NoOp, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a NoOp must fire zero seams, got %v", rec.callOrder)
	}
}

// TestCaptureFailureRefuses: a CaptureUnit error refuses with FailedStep="capture"
// and records NO save/write/restart (an uncapturable prior unit must not mutate).
func TestCaptureFailureRefuses(t *testing.T) {
	rec := passStub()
	rec.captureErr = errors.New("unit file unreadable")
	res := Run(newSwapStub(rec), "rocm")
	if !res.Refused || res.FailedStep != "capture" {
		t.Fatalf("capture failure must Refuse at step capture, got %+v", res)
	}
	if indexOf(rec.callOrder, "save:") != -1 || indexOf(rec.callOrder, "write") != -1 || len(rec.restarted) != 0 {
		t.Errorf("capture failure must fire no save/write/restart, got %v", rec.callOrder)
	}
}

// assertVerbatimRestore is a shared helper used by the rollback tests (08-01-02).
func assertVerbatimRestore(t *testing.T, rec *swapRecorder) {
	t.Helper()
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("rollback must RestoreUnit byte-equal to the captured prior unit; got %q want %q", rec.restored, priorUnitBytes)
	}
}

// TestRollbackVerbatim: a non-pass Prove verdict drives RestoreUnit (byte-equal to
// the captured priorUnit) → SaveConfig(priorCfg) → DaemonReload → Restart, in that
// order; the result is RolledBack with From/To set.
func TestRollbackVerbatim(t *testing.T) {
	rec := passStub()
	rec.proveStatus = "fail"
	rec.proveDetail = "residency FAIL"
	res := Run(newSwapStub(rec), "rocm")
	if !res.RolledBack || res.Switched {
		t.Fatalf("non-pass prove must roll back (not switch), got %+v", res)
	}
	if res.FromBackend != "vulkan" || res.ToBackend != "rocm" {
		t.Errorf("From/To must be set on rollback, got from=%q to=%q", res.FromBackend, res.ToBackend)
	}
	assertVerbatimRestore(t, rec)
	// Restore precedes the config-restore, reload, and the rollback restart.
	restoreIdx := indexOf(rec.callOrder, "restore")
	reloadIdx := indexOf(rec.callOrder, "daemon-reload")
	if restoreIdx < 0 || reloadIdx < 0 || !(restoreIdx < reloadIdx) {
		t.Errorf("expected restore before daemon-reload in rollback, got %v", rec.callOrder)
	}
	// The prior config was re-saved (Backend back to vulkan).
	if rec.saved.Backend != "vulkan" {
		t.Errorf("rollback must SaveConfig(priorCfg) restoring backend=vulkan, got %q", rec.saved.Backend)
	}
	// The rollback restart targets ONLY the inference service.
	if rec.restarted[len(rec.restarted)-1] != installService {
		t.Errorf("rollback restart must target %s, got %v", installService, rec.restarted)
	}
}

// TestProveGate: a non-pass Prove verdict yields Switched=false, RolledBack=true.
func TestProveGate(t *testing.T) {
	rec := passStub()
	rec.proveStatus = "warn"
	res := Run(newSwapStub(rec), "rocm")
	if res.Switched {
		t.Errorf("a non-pass prove must NOT switch, got %+v", res)
	}
	if !res.RolledBack {
		t.Errorf("a non-pass prove must roll back, got %+v", res)
	}
}

// TestActiveNotSuccess: a verdict that is "ready+200 but residency FAIL" — any
// non-ProveStatusPass value — triggers rollback. is-active/health-200 alone is
// never success (SC#3).
func TestActiveNotSuccess(t *testing.T) {
	rec := passStub()
	rec.proveStatus = "ready+200 but residency FAIL" // any non-pass sentinel
	res := Run(newSwapStub(rec), "rocm")
	if res.Switched || !res.RolledBack {
		t.Fatalf("ready+200-but-not-pass must roll back, never switch, got %+v", res)
	}
}

// TestRefuseFitGuard: a FitsModel→false refuses with ZERO save/write/restart/capture
// seams (refuse-with-remediation, BSET-01).
func TestRefuseFitGuard(t *testing.T) {
	rec := passStub()
	rec.fitOK = false
	rec.fitReason = "preserved model no longer fits the rocm envelope"
	res := Run(newSwapStub(rec), "rocm")
	if !res.Refused || res.Switched || res.RolledBack {
		t.Fatalf("non-fit must Refuse with zero side effects, got %+v", res)
	}
	if res.Reason != rec.fitReason {
		t.Errorf("refusal must carry the fit remediation reason, got %q", res.Reason)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a fit refusal must fire zero seams (no capture/save/write/restart), got %v", rec.callOrder)
	}
}

// TestRefuseProveFlightROCm: a PreflightROCm→false (rocm target) refuses with ZERO
// side effects (refuse-with-remediation, BSET-01).
func TestRefuseProveFlightROCm(t *testing.T) {
	rec := passStub()
	rec.preflightOK = false
	rec.preflight = "rocm preflight: kernel below 6.18.4 floor"
	res := Run(newSwapStub(rec), "rocm")
	if !res.Refused || res.Switched || res.RolledBack {
		t.Fatalf("preflight block must Refuse with zero side effects, got %+v", res)
	}
	if res.Reason != rec.preflight {
		t.Errorf("refusal must carry the preflight remediation reason, got %q", res.Reason)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a preflight refusal must fire zero seams, got %v", rec.callOrder)
	}
}

// TestMutateErrorRollsBack: an error during the mutate step (here SaveConfig) rolls
// back verbatim (RestoreUnit with priorUnit) and reports RolledBack with FailedStep
// set and the original error carried.
func TestMutateErrorRollsBack(t *testing.T) {
	rec := passStub()
	rec.saveErr = errors.New("disk full")
	res := Run(newSwapStub(rec), "rocm")
	if !res.RolledBack || res.Switched {
		t.Fatalf("a mutate error must roll back, got %+v", res)
	}
	if res.FailedStep != "save" {
		t.Errorf("FailedStep must name the mutate step, got %q", res.FailedStep)
	}
	if res.Err == nil {
		t.Errorf("the original mutate error must be carried, got nil")
	}
	assertVerbatimRestore(t, rec)
}

// TestMutateErrorRollsBack_Restart: an error on the FORWARD restart also rolls back
// verbatim (the mutate step covers save/write/restart).
func TestMutateErrorRollsBack_Restart(t *testing.T) {
	rec := passStub()
	rec.restartErr = errors.New("systemd start failed")
	res := Run(newSwapStub(rec), "rocm")
	if !res.RolledBack || res.FailedStep != "restart" {
		t.Fatalf("a forward-restart error must roll back at step restart, got %+v", res)
	}
	assertVerbatimRestore(t, rec)
}

// TestRollbackIncompleteReported: when a rollback step itself errors (here the
// rollback Restart), RolledBack stays true but Reason honestly flags
// rollback-incomplete (Pitfall 5 — never claim a clean no-op when rollback errored).
func TestRollbackIncompleteReported(t *testing.T) {
	rec := passStub()
	rec.proveStatus = "fail" // trigger rollback
	rec.rbRestartErr = errors.New("systemd refused restart")
	res := Run(newSwapStub(rec), "rocm")
	if !res.RolledBack {
		t.Fatalf("expected RolledBack=true even on incomplete rollback, got %+v", res)
	}
	if !strings.Contains(res.Reason, "did not fully complete") {
		t.Errorf("an incomplete rollback must be flagged honestly in Reason, got %q", res.Reason)
	}
}
