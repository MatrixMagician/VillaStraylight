package codingmode

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// codingmode_test.go drives the transactional core through a fake Deps with no live
// host. It asserts the ordering contract (capture STRICTLY before any mutation,
// Pitfall 5), the verbatim rollback (RestoreUnit with the captured priorUnit), the
// prove-gate (switch ONLY on ProveStatusPass — is-active/200 alone is never success,
// D-09), the same-state NoOp (zero side effects, D-06), exit symmetry (D-08), and
// swap-vs-shared residency surfacing (never silently degrade, D-10). It mirrors the
// backendswap swapRecorder + callOrder discipline.

const installService = "villa-llama.service"

// priorUnitBytes is the verbatim prior unit the fake CaptureUnit returns, so the
// rollback tests can assert a byte-equal RestoreUnit.
var priorUnitBytes = []byte("[Container]\nImage=prior\nExec=llama-server --prior\n")

// modeRecorder records each side-effecting seam call so the tests can assert ordering
// (capture < save/write; RestoreUnit precedes the rollback Restart) and that ONLY the
// inference service is restarted.
type modeRecorder struct {
	callOrder []string
	saved     config.VillaConfig
	restarted []string
	captured  []byte
	restored  []byte
	pulled    []string

	// knobs:
	currentCoding bool        // cfg.CodingMode in the loaded config
	chatModel     string      // cfg.Model in the loaded config
	coder         CoderTarget // ResolveCoder result
	resolveOK     bool        // ResolveCoder ok
	resolveReason string      // ResolveCoder remediation reason
	resolveCalled bool        // whether ResolveCoder was invoked
	downloaded    bool        // coder weights already on disk
	pullErr       error       // Pull error
	captureErr    error       // CaptureUnit error (uncapturable prior unit)
	saveErr       error       // SaveConfig error (mutate failure)
	writeErr      error       // ReconcileAndWrite error (mutate failure)
	restartErr    error       // first Restart error (mutate failure)
	proveStatus   string      // Prove verdict Status (ProveStatusPass = pass)
	proveDetail   string      // Prove verdict Detail
	restoreErr    error       // RestoreUnit error during rollback (rollback-incomplete)
	rbRestartErr  error       // Restart error during rollback (rollback-incomplete)

	restartCalls int // counts Restart invocations to distinguish forward vs rollback
}

// newModeStub builds a Deps wired to rec. Defaults: chat model "chat-model", a swap
// coder target that fits + is downloaded, prove=pass — a clean forward enter unless a
// knob flips.
func newModeStub(rec *modeRecorder) Deps {
	if rec.chatModel == "" {
		rec.chatModel = "chat-model"
	}
	return Deps{
		InstallServiceName: installService,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: rec.chatModel, CodingMode: rec.currentCoding}, nil
		},
		ResolveCoder: func(_ config.VillaConfig) (CoderTarget, bool, string) {
			rec.resolveCalled = true
			t := rec.coder
			t.Downloaded = rec.downloaded
			return t, rec.resolveOK, rec.resolveReason
		},
		Pull: func(t CoderTarget) error {
			rec.callOrder = append(rec.callOrder, "pull")
			rec.pulled = append(rec.pulled, t.Model)
			return rec.pullErr
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
			rec.callOrder = append(rec.callOrder, "save")
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
			if rec.restartCalls == 1 {
				return rec.restartErr
			}
			return rec.rbRestartErr
		},
		Prove: func(_ context.Context, _ Direction) ProveVerdict {
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

// enterStub is the baseline happy-path recorder for ENTER: chat→coder, swap residency,
// fits, downloaded, prove pass.
func enterStub() *modeRecorder {
	return &modeRecorder{
		currentCoding: false,
		chatModel:     "chat-model",
		resolveOK:     true,
		downloaded:    true,
		coder:         CoderTarget{Model: "coder-model", Quant: "Q4", AgentCtx: 65536, Residency: ResidencySwap},
		proveStatus:   ProveStatusPass,
	}
}

// TestEnter: a clean enter from chat → swap coder → Prove pass → Switched. SaveConfig
// carried CodingMode=true + the resolved coder model/quant/agent_ctx; capture preceded
// save+write; restart targeted ONLY the inference service.
func TestEnter(t *testing.T) {
	rec := enterStub()
	res := Run(newModeStub(rec), Enter)
	if !res.Switched {
		t.Fatalf("clean enter expected Switched, got %+v", res)
	}
	if res.Residency != ResidencySwap {
		t.Errorf("enter must surface swap residency, got %q", res.Residency)
	}
	if res.FromModel != "chat-model" || res.ToModel != "coder-model" {
		t.Errorf("From/To models wrong: from=%q to=%q", res.FromModel, res.ToModel)
	}
	// SaveConfig persisted the coding fields resolved at enter (D-04).
	if !rec.saved.CodingMode || rec.saved.CoderModel != "coder-model" ||
		rec.saved.CoderQuant != "Q4" || rec.saved.CoderAgentCtx != 65536 {
		t.Errorf("enter must persist CodingMode=true + resolved coder fields, got %+v", rec.saved)
	}
	// D-08: cfg.Model (the durable chat model) is NEVER overwritten — the served coder
	// model lives in CoderModel so exit reverts by clearing the coder fields. Surfaced
	// ToModel still names the coder (the served model) for the user.
	if rec.saved.Model != "chat-model" {
		t.Errorf("swap enter must NOT overwrite the durable chat model; got cfg.Model=%q", rec.saved.Model)
	}
	// Capture STRICTLY before mutate (Pitfall 5).
	capIdx := indexOf(rec.callOrder, "capture")
	saveIdx := indexOf(rec.callOrder, "save")
	writeIdx := indexOf(rec.callOrder, "write")
	if capIdx < 0 || saveIdx < 0 || writeIdx < 0 || !(capIdx < saveIdx && capIdx < writeIdx) {
		t.Errorf("capture must precede save AND write, got order %v", rec.callOrder)
	}
	// Inference-only restart.
	if len(rec.restarted) != 1 || rec.restarted[0] != installService {
		t.Errorf("enter must restart only %s, got %v", installService, rec.restarted)
	}
}

// TestEnterAutoPullsAbsentCoder: on a swap enter with absent weights the core auto-pulls
// (modelswap step 3) BEFORE the capture/mutate, then proceeds.
func TestEnterAutoPullsAbsentCoder(t *testing.T) {
	rec := enterStub()
	rec.downloaded = false
	res := Run(newModeStub(rec), Enter)
	if !res.Switched {
		t.Fatalf("enter with absent weights must pull then switch, got %+v", res)
	}
	if len(rec.pulled) != 1 || rec.pulled[0] != "coder-model" {
		t.Errorf("absent swap coder must be pulled, got %v", rec.pulled)
	}
	pullIdx := indexOf(rec.callOrder, "pull")
	capIdx := indexOf(rec.callOrder, "capture")
	if pullIdx < 0 || capIdx < 0 || !(pullIdx < capIdx) {
		t.Errorf("pull must precede capture, got %v", rec.callOrder)
	}
}

// TestProveFailRollback: a non-pass Prove verdict (CPU fallback / residency FAIL) drives
// RestoreUnit (byte-equal to captured priorUnit) → SaveConfig(priorCfg) → DaemonReload →
// Restart; Result.RolledBack true, Switched false; prior bytes+config are VERBATIM.
func TestProveFailRollback(t *testing.T) {
	rec := enterStub()
	rec.proveStatus = "fail"
	rec.proveDetail = "residency FAIL (CPU fallback)"
	res := Run(newModeStub(rec), Enter)
	if !res.RolledBack || res.Switched {
		t.Fatalf("non-pass prove must roll back (not switch), got %+v", res)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("rollback must RestoreUnit byte-equal to captured prior unit; got %q", rec.restored)
	}
	// The prior config was re-saved (CodingMode back to false).
	if rec.saved.CodingMode {
		t.Errorf("rollback must SaveConfig(priorCfg) restoring CodingMode=false, got %+v", rec.saved)
	}
	restoreIdx := indexOf(rec.callOrder, "restore")
	reloadIdx := indexOf(rec.callOrder, "daemon-reload")
	if restoreIdx < 0 || reloadIdx < 0 || !(restoreIdx < reloadIdx) {
		t.Errorf("expected restore before daemon-reload in rollback, got %v", rec.callOrder)
	}
	if rec.restarted[len(rec.restarted)-1] != installService {
		t.Errorf("rollback restart must target %s, got %v", installService, rec.restarted)
	}
}

// TestIdleGreenNotSuccess: any non-ProveStatusPass verdict (incl. ready+health-200-but-
// residency-FAIL) triggers rollback. Idle-green is never green (D-09).
func TestIdleGreenNotSuccess(t *testing.T) {
	rec := enterStub()
	rec.proveStatus = "ready+200 but residency FAIL" // any non-pass sentinel
	res := Run(newModeStub(rec), Enter)
	if res.Switched || !res.RolledBack {
		t.Fatalf("ready+200-but-not-pass must roll back, never switch, got %+v", res)
	}
}

// TestMutateRollback: a SaveConfig error during mutate rolls back verbatim with the
// FailedStep set and the original error carried.
func TestMutateRollback(t *testing.T) {
	rec := enterStub()
	rec.saveErr = errors.New("disk full")
	res := Run(newModeStub(rec), Enter)
	if !res.RolledBack || res.Switched {
		t.Fatalf("a mutate error must roll back, got %+v", res)
	}
	if res.FailedStep != "save" || res.Err == nil {
		t.Errorf("FailedStep must name the mutate step + carry the error, got step=%q err=%v", res.FailedStep, res.Err)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("mutate-error rollback must restore the captured prior unit")
	}
}

// TestMutateRollback_Restart: a FORWARD restart error also rolls back verbatim.
func TestMutateRollback_Restart(t *testing.T) {
	rec := enterStub()
	rec.restartErr = errors.New("systemd start failed")
	res := Run(newModeStub(rec), Enter)
	if !res.RolledBack || res.FailedStep != "restart" {
		t.Fatalf("a forward-restart error must roll back at step restart, got %+v", res)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("restart-error rollback must restore the captured prior unit")
	}
}

// TestRollbackIncompleteReported: when a rollback step itself errors (here the rollback
// Restart), RolledBack stays true but Reason honestly flags rollback-incomplete
// (Pitfall 5 — never claim a clean no-op when rollback errored).
func TestRollbackIncompleteReported(t *testing.T) {
	rec := enterStub()
	rec.proveStatus = "fail" // trigger rollback
	rec.rbRestartErr = errors.New("systemd refused restart")
	res := Run(newModeStub(rec), Enter)
	if !res.RolledBack {
		t.Fatalf("expected RolledBack=true even on incomplete rollback, got %+v", res)
	}
	if !strings.Contains(res.Reason, "did not fully complete") {
		t.Errorf("an incomplete rollback must be flagged honestly in Reason, got %q", res.Reason)
	}
}

// TestExitRestoresChat: exit from coding mode runs the SAME (capture→mutate→prove→
// rollback) frame (D-08) and persists CodingMode=false, clearing the coder fields. It is
// symmetric to enter, NOT a bare flip — capture fired before save.
func TestExitRestoresChat(t *testing.T) {
	rec := enterStub()
	rec.currentCoding = true // currently in coding mode (cfg.Model is the durable chat model)
	res := Run(newModeStub(rec), Exit)
	if !res.Switched {
		t.Fatalf("clean exit expected Switched, got %+v", res)
	}
	if res.Direction != Exit {
		t.Errorf("exit Result must carry Direction=Exit, got %v", res.Direction)
	}
	if rec.saved.CodingMode || rec.saved.CoderModel != "" || rec.saved.CoderAgentCtx != 0 {
		t.Errorf("exit must persist CodingMode=false + cleared coder fields, got %+v", rec.saved)
	}
	// The durable chat model is restored as the served model (config-derived, D-08).
	if rec.saved.Model != "chat-model" {
		t.Errorf("exit must keep the durable chat model served, got %q", rec.saved.Model)
	}
	// Symmetric, not a bare flip: capture STRICTLY before save (the transactional frame).
	capIdx := indexOf(rec.callOrder, "capture")
	saveIdx := indexOf(rec.callOrder, "save")
	if capIdx < 0 || saveIdx < 0 || !(capIdx < saveIdx) {
		t.Errorf("exit must capture before mutating (symmetric frame, D-08), got %v", rec.callOrder)
	}
	// Exit must not resolve a coder target (no model selection on exit).
	if rec.resolveCalled {
		t.Errorf("exit must NOT call ResolveCoder")
	}
}

// TestExitProveFailRollback: an exit whose prove fails rolls back verbatim to the coding
// unit+config — exit is held to the SAME prove gate as enter (D-08/D-09).
func TestExitProveFailRollback(t *testing.T) {
	rec := enterStub()
	rec.currentCoding = true
	rec.proveStatus = "fail"
	res := Run(newModeStub(rec), Exit)
	if !res.RolledBack || res.Switched {
		t.Fatalf("exit prove-fail must roll back, got %+v", res)
	}
	if !bytes.Equal(rec.restored, priorUnitBytes) {
		t.Errorf("exit rollback must restore the captured prior (coding) unit")
	}
}

// TestNoOpEnterAlreadyCoding: enter while already coding is a clean NoOp with ZERO seams.
func TestNoOpEnterAlreadyCoding(t *testing.T) {
	rec := enterStub()
	rec.currentCoding = true
	res := Run(newModeStub(rec), Enter)
	if !res.NoOp || res.Switched || res.RolledBack || res.Refused {
		t.Fatalf("enter-while-already-coding must be a clean NoOp, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a NoOp must fire zero seams, got %v", rec.callOrder)
	}
	if rec.resolveCalled {
		t.Errorf("a NoOp must not resolve a coder target")
	}
}

// TestNoOpExitAlreadyChat: exit while already chat is a clean NoOp with ZERO seams.
func TestNoOpExitAlreadyChat(t *testing.T) {
	rec := enterStub()
	rec.currentCoding = false
	res := Run(newModeStub(rec), Exit)
	if !res.NoOp || res.Switched || res.RolledBack || res.Refused {
		t.Fatalf("exit-while-already-chat must be a clean NoOp, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a NoOp must fire zero seams, got %v", rec.callOrder)
	}
}

// TestRefuseFitGuard: ResolveCoder→false (the coder does not fit at AgentCtx) refuses
// with ZERO capture/save/write/restart/pull seams (refuse-with-remediation, Pitfall 4).
func TestRefuseFitGuard(t *testing.T) {
	rec := enterStub()
	rec.resolveOK = false
	rec.resolveReason = "coder needs 70 GiB vs 64 GiB usable at agent_ctx"
	res := Run(newModeStub(rec), Enter)
	if !res.Refused || res.Switched || res.RolledBack {
		t.Fatalf("a non-fit must Refuse with zero side effects, got %+v", res)
	}
	if res.Reason != rec.resolveReason {
		t.Errorf("refusal must carry the fit remediation reason, got %q", res.Reason)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("a fit refusal must fire zero seams, got %v", rec.callOrder)
	}
}

// TestCaptureFailureRefuses: a CaptureUnit error refuses at step capture with NO
// save/write/restart (an uncapturable prior unit must not mutate).
func TestCaptureFailureRefuses(t *testing.T) {
	rec := enterStub()
	rec.captureErr = errors.New("unit file unreadable")
	res := Run(newModeStub(rec), Enter)
	if !res.Refused || res.FailedStep != "capture" {
		t.Fatalf("capture failure must Refuse at step capture, got %+v", res)
	}
	if indexOf(rec.callOrder, "save") != -1 || indexOf(rec.callOrder, "write") != -1 || len(rec.restarted) != 0 {
		t.Errorf("capture failure must fire no save/write/restart, got %v", rec.callOrder)
	}
}

// TestSharedResidencyRenderDeltaOnly: in shared residency the enter path applies the
// render delta to the EXISTING chat model WITHOUT a model change (no swap, no pull),
// still transactional + proved, and surfaces the shared mode distinctly — it NEVER
// silently degrades swap→shared (D-10).
func TestSharedResidencyRenderDeltaOnly(t *testing.T) {
	rec := enterStub()
	rec.downloaded = false // would force a pull IF it were a swap
	rec.coder = CoderTarget{AgentCtx: 32768, Residency: ResidencyShared}
	res := Run(newModeStub(rec), Enter)
	if !res.Switched {
		t.Fatalf("shared enter must still switch (render-delta-only), got %+v", res)
	}
	if res.Residency != ResidencyShared {
		t.Errorf("shared residency must be surfaced distinctly, got %q", res.Residency)
	}
	// No model change on shared (D-10): the served model + To stay the chat model.
	if res.ToModel != "chat-model" || rec.saved.Model != "chat-model" {
		t.Errorf("shared residency must NOT change the served model, got To=%q saved.Model=%q", res.ToModel, rec.saved.Model)
	}
	// No pull on shared residency even though weights are "absent".
	if len(rec.pulled) != 0 {
		t.Errorf("shared residency must perform NO pull, got %v", rec.pulled)
	}
	// But the agent-ctx render delta IS persisted + coding mode on.
	if !rec.saved.CodingMode || rec.saved.CoderAgentCtx != 32768 {
		t.Errorf("shared enter must still persist CodingMode + agent ctx, got %+v", rec.saved)
	}
}
