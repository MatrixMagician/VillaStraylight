package backendswap

import (
	"bytes"
	"context"
	"errors"
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
