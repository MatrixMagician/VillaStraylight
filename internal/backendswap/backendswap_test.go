package backendswap

import (
	"context"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// The transaction's ordering, restart set and rollback are asserted once, on the
// frame (internal/stackapply/transact_test.go). These tests cover what the three
// verbs decide inside it: the no-op, the guards, and the config they write.

// swapFake is a no-host frame plus the verbs' guards. Its zero knobs are a clean
// cutover of a running villa-llama.
type swapFake struct {
	cfg          config.VillaConfig
	fitOK        bool
	fitReason    string
	fitSaw       config.VillaConfig
	preflightOK  bool
	preflightWhy string
	preflightSaw string
	captured     bool
	saved        []config.VillaConfig
	proved       []string
	proveStatus  string
	units        map[string]string // the prior units Capture returns
	restored     map[string]string
	restarted    []string
}

func newFake(cfg config.VillaConfig) *swapFake {
	return &swapFake{cfg: cfg, fitOK: true, preflightOK: true,
		units: map[string]string{"villa-llama.container": "PRIOR-LLAMA"}}
}

func (f *swapFake) deps() Deps {
	return Deps{
		Tx: stackapply.TxDeps{
			Lock:       func() (*stacklock.Lock, error) { return nil, nil },
			LoadConfig: func() (config.VillaConfig, error) { return f.cfg, nil },
			SaveConfig: func(c config.VillaConfig) error { f.saved = append(f.saved, c); return nil },
			Capture: func(config.VillaConfig) (map[string]string, error) {
				f.captured = true
				return f.units, nil
			},
			Apply: func(config.VillaConfig) (stackapply.Applied, error) {
				var applied stackapply.Applied
				for name := range f.units {
					applied.Changed = append(applied.Changed, orchestrate.Unit{Name: name})
				}
				return applied, nil
			},
			Restore:      func(m map[string]string) error { f.restored = m; return nil },
			DaemonReload: func() error { return nil },
			IsActive:     func(string) (string, error) { return "active", nil },
			Restart:      func(svc string) error { f.restarted = append(f.restarted, svc); return nil },
			Prove: func(_ context.Context, backend string) prove.Verdict {
				f.proved = append(f.proved, backend)
				if f.proveStatus != "" {
					return prove.Verdict{Status: f.proveStatus, Detail: "not resident"}
				}
				return prove.Verdict{Status: prove.StatusPass}
			},
			Service: "villa-llama.service",
		},
		FitsModel: func(c config.VillaConfig) (bool, string) {
			f.fitSaw = c
			return f.fitOK, f.fitReason
		},
		PreflightROCm: func(c config.VillaConfig) (bool, string) {
			f.preflightSaw = c.Backend
			return f.preflightOK, f.preflightWhy
		},
	}
}

func vulkan() config.VillaConfig {
	return config.VillaConfig{Model: "preserved-model", Backend: "vulkan"}
}

// TestNoOpSameBackend: a same-backend target is a clean no-op, nothing captured.
func TestNoOpSameBackend(t *testing.T) {
	f := newFake(vulkan())
	res := Run(f.deps(), "vulkan")
	if !res.NoOp || res.From != "vulkan" || res.To != "vulkan" {
		t.Fatalf("expected NoOp vulkan→vulkan, got %+v", res)
	}
	if f.captured || len(f.saved) != 0 {
		t.Errorf("a no-op must capture and save nothing")
	}
}

// TestRefuseFitGuard: a PRESERVED model that no longer fits refuses with the
// remediation before anything is captured (BSET-01).
func TestRefuseFitGuard(t *testing.T) {
	f := newFake(vulkan())
	f.fitOK, f.fitReason = false, "needs 9 bytes vs 1 usable"
	res := Run(f.deps(), "rocm")
	if !res.Refused || res.Reason != f.fitReason {
		t.Fatalf("expected a fit refusal carrying the reason, got %+v", res)
	}
	if f.fitSaw.Model != "preserved-model" || f.captured {
		t.Errorf("the fit guard must see the preserved model and refuse before capture (saw %q, captured %v)", f.fitSaw.Model, f.captured)
	}
}

// TestRefusePreflightROCm: a failing ROCm preflight refuses before capture.
func TestRefusePreflightROCm(t *testing.T) {
	f := newFake(vulkan())
	f.preflightOK, f.preflightWhy = false, "kernel below floor"
	res := Run(f.deps(), "rocm")
	if !res.Refused || res.Reason != "kernel below floor" || f.captured {
		t.Fatalf("expected a preflight refusal before capture, got %+v (captured %v)", res, f.captured)
	}
}

// TestPreflightSeesTargetBackend: the gate sees the TARGET backend, or a
// vulkan→rocm switch would skip the kernel/firmware/HSA checks entirely.
func TestPreflightSeesTargetBackend(t *testing.T) {
	f := newFake(vulkan())
	Run(f.deps(), "rocm")
	if f.preflightSaw != "rocm" {
		t.Errorf("PreflightROCm saw %q, want the target rocm", f.preflightSaw)
	}
}

// TestSwitchPersistsAndProvesTheTarget: the change writes only the backend, the
// model is preserved, and the proof drives the target backend.
func TestSwitchPersistsAndProvesTheTarget(t *testing.T) {
	f := newFake(vulkan())
	res := Run(f.deps(), "rocm")
	if !res.Switched || res.From != "vulkan" || res.To != "rocm" {
		t.Fatalf("expected Switched vulkan→rocm, got %+v", res)
	}
	want := vulkan()
	want.Backend = "rocm"
	if len(f.saved) != 1 || f.saved[0].Backend != "rocm" || f.saved[0].Model != want.Model {
		t.Errorf("saved %+v, want only the backend changed", f.saved)
	}
	if len(f.proved) != 1 || f.proved[0] != "rocm" {
		t.Errorf("proved %v, want the target backend", f.proved)
	}
}

// TestRolledBackNamesThePriorBackend: a failed proof rolls back and the Result still
// names what the operator is back on.
func TestRolledBackNamesThePriorBackend(t *testing.T) {
	f := newFake(vulkan())
	f.proveStatus = prove.StatusFail
	res := Run(f.deps(), "rocm")
	if !res.RolledBack || res.From != "vulkan" || res.FailedStep != "prove" {
		t.Fatalf("expected a prove rollback back to vulkan, got %+v", res)
	}
}

// TestSwitchRestartsTheChangedResidentUnit guards #251. Resident units share the
// backend image, so a backend switch rewrites them too. Every rewritten unit whose
// service is running must be restarted on the way in, and restored and restarted on
// the way out, or the resident keeps serving the old image under a unit file that
// already names the new one.
func TestSwitchRestartsTheChangedResidentUnit(t *testing.T) {
	const residentUnit = "villa-llama-small.container"
	const residentService = "villa-llama-small.service"
	count := func(xs []string, want string) (n int) {
		for _, x := range xs {
			if x == want {
				n++
			}
		}
		return n
	}

	t.Run("cutover", func(t *testing.T) {
		f := newFake(vulkan())
		f.units[residentUnit] = "[Container]\nImage=prior\n"
		res := Run(f.deps(), "rocm")
		if !res.Switched {
			t.Fatalf("expected Switched, got %+v", res)
		}
		if n := count(f.restarted, residentService); n != 1 {
			t.Errorf("the running resident whose unit the switch rewrote must be restarted once, restarts %v", f.restarted)
		}
	})

	t.Run("rollback", func(t *testing.T) {
		f := newFake(vulkan())
		f.proveStatus = prove.StatusFail
		f.units[residentUnit] = "[Container]\nImage=prior\n"
		res := Run(f.deps(), "rocm")
		if !res.RolledBack {
			t.Fatalf("expected RolledBack, got %+v", res)
		}
		if f.restored[residentUnit] != "[Container]\nImage=prior\n" {
			t.Errorf("rollback must restore the resident unit verbatim, restored %v", f.restored)
		}
		if n := count(f.restarted, residentService); n != 2 {
			t.Errorf("rollback must restart the restored resident again (cutover + rollback), restarts %v", f.restarted)
		}
	})
}
