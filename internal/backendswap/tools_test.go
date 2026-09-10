package backendswap

// tools_test.go drives RunTools' own guards and the shared transaction beneath
// them: the answered gate decides the no-op, the fit guard sees the target state
// and runs before any capture, and every step failure rolls back verbatim.

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
)

const testService = "villa-llama.service"

// recorder is the fake host every test drives the cutover against: it records the
// order of the seam calls so the tests assert the transaction's ordering, not just
// its outcome.
type recorder struct {
	persisted config.VillaConfig

	fitOK     bool
	fitReason string

	// The forward errors arm the FIRST save/restart only, so a mutate-step failure
	// still gets a working rollback. The rollback* errors arm the restore path, which
	// is the second call of each.
	captureErr         error
	saveErr            error
	writeErr           error
	restartErr         error
	restoreErr         error
	reloadErr          error
	rollbackSaveErr    error
	rollbackRestartErr error

	verdict prove.Verdict

	calls     []string
	saved     []config.VillaConfig
	restarted []string
	restored  []byte
}

func newRecorder(toolsOn bool) *recorder {
	return &recorder{
		persisted: config.VillaConfig{Model: "preserved-model", Backend: "rocm", Ctx: 8192, ToolsMode: toolsOn},
		fitOK:     true,
		verdict:   prove.Verdict{Status: prove.StatusPass, Detail: "residency proven; tool call completed"},
	}
}

func (r *recorder) deps() Deps {
	return Deps{
		InstallServiceName: testService,
		LoadConfig:         func() (config.VillaConfig, error) { return r.persisted, nil },
		FitsModel: func(config.VillaConfig) (bool, string) {
			r.calls = append(r.calls, "fit")
			return r.fitOK, r.fitReason
		},
		CaptureUnit: func() ([]byte, error) {
			r.calls = append(r.calls, "capture")
			if r.captureErr != nil {
				return nil, r.captureErr
			}
			return []byte("prior unit bytes"), nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			r.calls = append(r.calls, "save")
			r.saved = append(r.saved, c)
			if len(r.saved) == 1 {
				return r.saveErr
			}
			return r.rollbackSaveErr
		},
		ReconcileAndWrite: func(config.VillaConfig) (bool, error) {
			r.calls = append(r.calls, "write")
			return r.writeErr == nil, r.writeErr
		},
		RestoreUnit: func(b []byte) error {
			r.calls = append(r.calls, "restore")
			r.restored = b
			return r.restoreErr
		},
		DaemonReload: func() error {
			r.calls = append(r.calls, "reload")
			return r.reloadErr
		},
		Restart: func(svc string) error {
			r.calls = append(r.calls, "restart")
			r.restarted = append(r.restarted, svc)
			if len(r.restarted) == 1 {
				return r.restartErr
			}
			return r.rollbackRestartErr
		},
		Prove: func(context.Context, string) prove.Verdict {
			r.calls = append(r.calls, "prove")
			return r.verdict
		},
	}
}

func (r *recorder) called(name string) bool {
	for _, c := range r.calls {
		if c == name {
			return true
		}
	}
	return false
}

// TestRunNoOp guards the promise that entering a mode already entered (and exiting
// one already exited) exits clean without touching the host: nothing is captured,
// nothing is restarted, and the operator is told it is already there.
func TestRunNoOp(t *testing.T) {
	for _, tc := range []struct {
		name      string
		persisted bool
		target    bool
	}{
		{"enter when already on", true, true},
		{"exit when already off", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder(tc.persisted)
			res := RunTools(r.deps(), tc.target)
			if !res.NoOp {
				t.Fatalf("res = %+v, want NoOp", res)
			}
			if res.From != ToolsLabel(tc.persisted) || res.To != ToolsLabel(tc.target) {
				t.Errorf("From/To = %q/%q, want %q/%q", res.From, res.To, ToolsLabel(tc.persisted), ToolsLabel(tc.target))
			}
			if len(r.calls) != 0 {
				t.Errorf("a no-op touched the host: %v", r.calls)
			}
		})
	}
}

// TestRunAnswersTheGateNotTheFlag guards that the transaction reads the answered
// gate: coding mode implies tool calling, so entering on a coding-mode stack is
// already there, and exiting refuses by naming the flag that actually holds it
// rather than persisting a false that renders nothing.
func TestRunAnswersTheGateNotTheFlag(t *testing.T) {
	t.Run("enter under coding mode is a no-op", func(t *testing.T) {
		r := newRecorder(false)
		r.persisted.CodingMode = true
		res := RunTools(r.deps(), true)
		if !res.NoOp || res.From != toolsStateOn {
			t.Fatalf("res = %+v, want a NoOp from on", res)
		}
		if len(r.calls) != 0 {
			t.Errorf("a no-op touched the host: %v", r.calls)
		}
	})
	t.Run("exit under coding mode refuses", func(t *testing.T) {
		r := newRecorder(false)
		r.persisted.CodingMode = true
		res := RunTools(r.deps(), false)
		if !res.Refused {
			t.Fatalf("res = %+v, want Refused", res)
		}
		if !strings.Contains(res.Reason, "coding-mode exit") {
			t.Errorf("Reason = %q, want it to name `villa coding-mode exit`", res.Reason)
		}
		if len(r.calls) != 0 {
			t.Errorf("a refusal touched the host: %v", r.calls)
		}
	})
}

// TestRunFitRefusalPrecedesCapture guards the ctx-floor promise: a floor that does
// not fit refuses with the fit remediation BEFORE anything is captured, so a refusal
// leaves the running stack untouched rather than rolling one back.
func TestRunFitRefusalPrecedesCapture(t *testing.T) {
	r := newRecorder(false)
	r.fitOK = false
	r.fitReason = "needs 71000000000 bytes vs 64000000000 usable"

	res := RunTools(r.deps(), true)
	if !res.Refused {
		t.Fatalf("res = %+v, want Refused", res)
	}
	if res.Reason != r.fitReason {
		t.Errorf("Reason = %q, want the fit remediation %q", res.Reason, r.fitReason)
	}
	if r.called("capture") {
		t.Errorf("the fit refusal captured the prior unit: %v", r.calls)
	}
	if len(r.calls) != 1 || r.calls[0] != "fit" {
		t.Errorf("calls = %v, want the fit guard alone", r.calls)
	}
}

// TestRunFitGuardSeesTheTargetState guards against the dead-guard bug the sibling
// cores each had: the fit closure must be handed the config it would render, not the
// persisted one, or the ctx floor is never checked on the only path that raises it.
func TestRunFitGuardSeesTheTargetState(t *testing.T) {
	r := newRecorder(false)
	var seen config.VillaConfig
	d := r.deps()
	d.FitsModel = func(c config.VillaConfig) (bool, string) { seen = c; return true, "" }

	RunTools(d, true)
	if !seen.ToolsMode {
		t.Errorf("the fit guard saw ToolsMode=%v, want the TARGET state true", seen.ToolsMode)
	}
}

// TestRunSuccessPersistsTheFlag guards the forward path: a proven cutover leaves the
// flag in config, restarts only the inference service, and captures before it saves.
func TestRunSuccessPersistsTheFlag(t *testing.T) {
	r := newRecorder(false)
	res := RunTools(r.deps(), true)
	if !res.Switched {
		t.Fatalf("res = %+v, want Switched", res)
	}
	if len(r.saved) != 1 || !r.saved[0].ToolsMode {
		t.Fatalf("persisted config = %+v, want tools_mode true", r.saved)
	}
	if r.saved[0].Model != "preserved-model" || r.saved[0].Backend != "rocm" {
		t.Errorf("the cutover mutated more than the flag: %+v", r.saved[0])
	}
	if len(r.restarted) != 1 || r.restarted[0] != testService {
		t.Errorf("restarted %v, want only %s", r.restarted, testService)
	}
	want := []string{"fit", "capture", "save", "write", "restart", "prove"}
	if strings.Join(r.calls, ",") != strings.Join(want, ",") {
		t.Errorf("calls = %v, want %v", r.calls, want)
	}
}

// TestRunExitClearsTheFlag guards the reverse direction under the same discipline:
// exit is a transaction, not a bare config write.
func TestRunExitClearsTheFlag(t *testing.T) {
	r := newRecorder(true)
	res := RunTools(r.deps(), false)
	if !res.Switched || res.From != toolsStateOn || res.To != toolsStateOff {
		t.Fatalf("res = %+v, want a proven on->off switch", res)
	}
	if len(r.saved) != 1 || r.saved[0].ToolsMode {
		t.Errorf("persisted config = %+v, want tools_mode false", r.saved)
	}
}

// TestRunStepFailuresRollBack is the table over every step of the transaction: a
// capture failure refuses without mutating, and a failure at any later step restores
// the verbatim prior unit and the prior config.
func TestRunStepFailuresRollBack(t *testing.T) {
	boom := errors.New("boom")
	for _, tc := range []struct {
		name       string
		arm        func(*recorder)
		wantStep   string
		wantRefuse bool
	}{
		{"capture fails", func(r *recorder) { r.captureErr = boom }, "capture", true},
		{"save fails", func(r *recorder) { r.saveErr = boom }, "save", false},
		{"render fails", func(r *recorder) { r.writeErr = boom }, "write", false},
		{"restart fails", func(r *recorder) { r.restartErr = boom }, "restart", false},
		{"prove fails", func(r *recorder) {
			r.verdict = prove.Verdict{Status: prove.StatusFail, Detail: "the model fell back to CPU"}
		}, "prove", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder(false)
			tc.arm(r)
			res := RunTools(r.deps(), true)

			if tc.wantRefuse {
				if !res.Refused || res.FailedStep != tc.wantStep {
					t.Fatalf("res = %+v, want Refused at %q", res, tc.wantStep)
				}
				if r.called("save") || r.called("restore") {
					t.Errorf("a refusal mutated the host: %v", r.calls)
				}
				return
			}

			if !res.RolledBack || res.FailedStep != tc.wantStep {
				t.Fatalf("res = %+v, want RolledBack at %q", res, tc.wantStep)
			}
			if string(r.restored) != "prior unit bytes" {
				t.Errorf("restored %q, want the verbatim captured bytes", r.restored)
			}
			last := r.saved[len(r.saved)-1]
			if last.ToolsMode {
				t.Errorf("the rollback left tools_mode on: %+v", last)
			}
			if strings.Contains(res.Reason, "did not fully complete") {
				t.Errorf("a clean rollback reported as incomplete: %q", res.Reason)
			}
		})
	}
}

// TestRunSaveFailureRollsBackWithoutARestartLoop guards that a save failure still
// restores, since the prior config is the snapshot taken before the mutation.
func TestRunSaveFailureRollsBackWithoutARestartLoop(t *testing.T) {
	r := newRecorder(false)
	r.saveErr = errors.New("read-only config dir")
	res := RunTools(r.deps(), true)
	if !res.RolledBack || res.Err == nil {
		t.Fatalf("res = %+v, want RolledBack carrying the save error", res)
	}
	if r.called("write") {
		t.Errorf("a failed save still rendered units: %v", r.calls)
	}
}

// TestRunRollbackIncompleteIsReportedHonestly guards the no-false-green promise: when
// a restore step itself fails, the result says so instead of presenting a
// half-restored stack as a clean rollback.
func TestRunRollbackIncompleteIsReportedHonestly(t *testing.T) {
	for _, tc := range []struct {
		name string
		arm  func(*recorder)
		want string
	}{
		{"restore fails", func(r *recorder) { r.restoreErr = errors.New("no such unit dir") }, "RestoreUnit failed"},
		{"reload fails", func(r *recorder) { r.reloadErr = errors.New("dbus down") }, "DaemonReload failed"},
		{"re-ready restart fails", func(r *recorder) { r.rollbackRestartErr = errors.New("unit failed") }, "Restart(prior) failed"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r := newRecorder(false)
			r.verdict = prove.Verdict{Status: prove.StatusFail, Detail: "no residency"}
			tc.arm(r)
			res := RunTools(r.deps(), true)
			if !res.RolledBack {
				t.Fatalf("res = %+v, want RolledBack", res)
			}
			if !strings.Contains(res.Reason, "did not fully complete") || !strings.Contains(res.Reason, tc.want) {
				t.Errorf("Reason = %q, want an honest rollback-incomplete naming %q", res.Reason, tc.want)
			}
		})
	}
}

// TestRunLoadFailureRefuses guards that an unreadable config is a refusal, not a
// cutover against a fabricated default.
func TestRunLoadFailureRefuses(t *testing.T) {
	r := newRecorder(false)
	d := r.deps()
	d.LoadConfig = func() (config.VillaConfig, error) { return config.VillaConfig{}, errors.New("unreadable") }
	res := RunTools(d, true)
	if !res.Refused || res.FailedStep != "load config" {
		t.Fatalf("res = %+v, want Refused at load config", res)
	}
	if len(r.calls) != 0 {
		t.Errorf("an unreadable config touched the host: %v", r.calls)
	}
}
