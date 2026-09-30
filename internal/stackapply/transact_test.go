package stackapply

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// transact_test.go is the one ordering suite for the swap transaction frame
// (ADR-0015). The swap cores' own tests cover only their guards and mutations; the
// capture → mutate → prove → rollback order, the restart set and the lock live here.

const provenService = "villa-llama.service"

// txFake records every host call in order. Its zero knobs are a clean cutover of
// villa-llama plus one running resident.
type txFake struct {
	calls   []string
	saved   []config.VillaConfig
	proved  []string
	prior   config.VillaConfig
	units   map[string]string // what Capture returns
	changed []string          // what Apply reports as written
	states  map[string]string // IsActive answers; absent = "active"

	lockErr, loadErr, captureErr, saveErr, applyErr error
	restoreErr, reloadErr                           error
	restartErr                                      map[string]error // first restart of a service fails
	rollbackRestartErr                              error            // a restart after the rollback began fails
	proveStatus                                     string
	rollingBack                                     bool
	restarts                                        map[string]int
}

func newTxFake() *txFake {
	return &txFake{
		prior:   config.VillaConfig{Model: "chat", Backend: "vulkan"},
		units:   map[string]string{"villa-llama.container": "PRIOR-LLAMA", "villa-llama-small.container": "PRIOR-SMALL"},
		changed: []string{"villa-llama.container", "villa-llama-small.container", "villa.network"},
		states:  map[string]string{},
	}
}

func (f *txFake) deps() TxDeps {
	return TxDeps{
		Lock: func() (*stacklock.Lock, error) {
			f.calls = append(f.calls, "lock")
			return nil, f.lockErr
		},
		LoadConfig: func() (config.VillaConfig, error) {
			f.calls = append(f.calls, "load")
			return f.prior, f.loadErr
		},
		SaveConfig: func(c config.VillaConfig) error {
			f.calls = append(f.calls, "save:"+c.Backend)
			f.saved = append(f.saved, c)
			if c.Backend != f.prior.Backend {
				return f.saveErr
			}
			return nil
		},
		Capture: func(c config.VillaConfig) (map[string]string, error) {
			f.calls = append(f.calls, "capture:"+c.Backend)
			return f.units, f.captureErr
		},
		Apply: func(c config.VillaConfig) ([]orchestrate.Unit, error) {
			f.calls = append(f.calls, "apply:"+c.Backend)
			if f.applyErr != nil {
				return nil, f.applyErr
			}
			var out []orchestrate.Unit
			for _, n := range f.changed {
				out = append(out, orchestrate.Unit{Name: n})
			}
			return out, nil
		},
		Restore: func(m map[string]string) error {
			f.rollingBack = true
			f.calls = append(f.calls, "restore")
			if !reflect.DeepEqual(m, f.units) {
				f.calls = append(f.calls, "restore-NOT-VERBATIM")
			}
			return f.restoreErr
		},
		DaemonReload: func() error {
			f.calls = append(f.calls, "reload")
			return f.reloadErr
		},
		IsActive: func(svc string) (string, error) {
			if s, ok := f.states[svc]; ok {
				return s, nil
			}
			return "active", nil
		},
		Restart: func(svc string) error {
			f.calls = append(f.calls, "restart:"+svc)
			if f.restarts == nil {
				f.restarts = map[string]int{}
			}
			f.restarts[svc]++
			if f.rollingBack {
				return f.rollbackRestartErr
			}
			return f.restartErr[svc]
		},
		Prove: func(_ context.Context, backend string) prove.Verdict {
			f.calls = append(f.calls, "prove")
			f.proved = append(f.proved, backend)
			if f.proveStatus != "" {
				return prove.Verdict{Status: f.proveStatus, Detail: "not resident"}
			}
			return prove.Verdict{Status: prove.StatusPass}
		},
		Service: provenService,
	}
}

// toRocm is the Change every test runs unless it is testing a stop.
func toRocm(cfg config.VillaConfig) (config.VillaConfig, *Outcome) {
	cfg.Backend = "rocm"
	return cfg, nil
}

func wantCalls(t *testing.T, f *txFake, want ...string) {
	t.Helper()
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls:\n got %v\nwant %v", f.calls, want)
	}
}

// TestTransactCutsOverInOrder: lock before the config is read, capture of the PRIOR
// config strictly before any mutation, the target persisted before it is applied,
// every changed running service restarted (the resident too, #251, and never the
// network), then the proof of the target backend.
func TestTransactCutsOverInOrder(t *testing.T) {
	f := newTxFake()
	o := Transact(f.deps(), toRocm)
	if !o.Switched || o.Prove.Status != prove.StatusPass {
		t.Fatalf("expected Switched with the passing verdict, got %+v", o)
	}
	wantCalls(t, f, "lock", "load", "capture:vulkan", "save:rocm", "apply:rocm",
		"restart:villa-llama.service", "restart:villa-llama-small.service", "prove")
	if !reflect.DeepEqual(f.proved, []string{"rocm"}) {
		t.Errorf("the proof must drive the target backend, got %v", f.proved)
	}
}

// TestTransactRestartsOnlyRunningChangedServices: a changed unit whose service is
// stopped is written but not started, and a service whose unit did not change is
// not restarted. The proven service is restarted even when stopped: the proof needs
// it serving.
func TestTransactRestartsOnlyRunningChangedServices(t *testing.T) {
	f := newTxFake()
	f.states = map[string]string{"villa-llama-small.service": "inactive", provenService: "inactive"}
	o := Transact(f.deps(), toRocm)
	if !o.Switched {
		t.Fatalf("expected Switched, got %+v", o)
	}
	if !reflect.DeepEqual(f.restarts, map[string]int{provenService: 1}) {
		t.Errorf("restarts = %v, want only the proven service", f.restarts)
	}

	f = newTxFake()
	f.changed = []string{"villa-llama.container"}
	Transact(f.deps(), toRocm)
	if !reflect.DeepEqual(f.restarts, map[string]int{provenService: 1}) {
		t.Errorf("an unchanged resident must not be restarted, restarts = %v", f.restarts)
	}
}

// TestTransactStopsBeforeCapture: a Change's refusal or no-op returns as-is and
// nothing is captured, saved or restarted.
func TestTransactStopsBeforeCapture(t *testing.T) {
	for _, stop := range []Outcome{{NoOp: true}, {Refused: true, Reason: "does not fit"}, {FailedStep: "pull", Err: errors.New("offline")}} {
		f := newTxFake()
		o := Transact(f.deps(), func(cfg config.VillaConfig) (config.VillaConfig, *Outcome) { return cfg, &stop })
		if !reflect.DeepEqual(o, stop) {
			t.Errorf("Outcome = %+v, want the Change's %+v", o, stop)
		}
		wantCalls(t, f, "lock", "load")
	}
}

// TestTransactLockAndLoadFailuresTouchNothing: a lock that cannot be taken (ErrBusy
// for the dashboard) stops before the config is read; a config that cannot be read
// stops before the Change runs.
func TestTransactLockAndLoadFailuresTouchNothing(t *testing.T) {
	f := newTxFake()
	f.lockErr = stacklock.ErrBusy
	o := Transact(f.deps(), toRocm)
	if !errors.Is(o.Err, stacklock.ErrBusy) || o.FailedStep != "lock" || o.RolledBack || o.Switched {
		t.Errorf("Outcome = %+v, want a lock failure carrying ErrBusy", o)
	}
	wantCalls(t, f, "lock")

	f = newTxFake()
	f.loadErr = errors.New("bad toml")
	o = Transact(f.deps(), func(config.VillaConfig) (config.VillaConfig, *Outcome) {
		t.Fatal("the Change must not run on an unreadable config")
		return config.VillaConfig{}, nil
	})
	if o.FailedStep != "load config" || o.Err == nil {
		t.Errorf("Outcome = %+v, want a load-config failure", o)
	}
}

// TestTransactCaptureFailureRefuses: an uncapturable prior unit is never mutated.
func TestTransactCaptureFailureRefuses(t *testing.T) {
	f := newTxFake()
	f.captureErr = errors.New("unreadable unit")
	o := Transact(f.deps(), toRocm)
	if !o.Refused || o.FailedStep != "capture" || o.Err == nil {
		t.Errorf("Outcome = %+v, want a capture refusal", o)
	}
	wantCalls(t, f, "lock", "load", "capture:vulkan")
}

// TestTransactNoUnitChangedIsANoOp: a target that renders the units already on disk
// is persisted, and nothing is restarted or proven.
func TestTransactNoUnitChangedIsANoOp(t *testing.T) {
	f := newTxFake()
	f.changed = nil
	o := Transact(f.deps(), toRocm)
	if !o.NoOp || o.Switched {
		t.Errorf("Outcome = %+v, want NoOp", o)
	}
	wantCalls(t, f, "lock", "load", "capture:vulkan", "save:rocm", "apply:rocm")
}

// TestTransactRollsBackEveryFailure: a save, write, restart or proof failure restores
// the captured units verbatim, re-saves the prior config, reloads, and restarts
// exactly the services the cutover restarted or tried to — none on a save or write
// failure, since nothing running was touched.
func TestTransactRollsBackEveryFailure(t *testing.T) {
	cases := []struct {
		name  string
		setup func(*txFake)
		want  []string
	}{
		{"save", func(f *txFake) { f.saveErr = errors.New("disk full") },
			[]string{"lock", "load", "capture:vulkan", "save:rocm", "restore", "save:vulkan", "reload"}},
		{"write", func(f *txFake) { f.applyErr = errors.New("write units") },
			[]string{"lock", "load", "capture:vulkan", "save:rocm", "apply:rocm", "restore", "save:vulkan", "reload"}},
		{"restart", func(f *txFake) {
			f.restartErr = map[string]error{"villa-llama-small.service": errors.New("start failed")}
		}, []string{"lock", "load", "capture:vulkan", "save:rocm", "apply:rocm",
			"restart:villa-llama.service", "restart:villa-llama-small.service",
			"restore", "save:vulkan", "reload", "restart:villa-llama.service", "restart:villa-llama-small.service"}},
		{"prove", func(f *txFake) { f.proveStatus = prove.StatusFail }, []string{"lock", "load", "capture:vulkan", "save:rocm", "apply:rocm",
			"restart:villa-llama.service", "restart:villa-llama-small.service", "prove",
			"restore", "save:vulkan", "reload", "restart:villa-llama.service", "restart:villa-llama-small.service"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := newTxFake()
			tc.setup(f)
			o := Transact(f.deps(), toRocm)
			if !o.RolledBack || o.Switched || o.FailedStep != tc.name {
				t.Fatalf("Outcome = %+v, want RolledBack at %q", o, tc.name)
			}
			if strings.Contains(o.Reason, "did not fully complete") {
				t.Errorf("a clean rollback must not be flagged incomplete: %q", o.Reason)
			}
			wantCalls(t, f, tc.want...)
			if last := f.saved[len(f.saved)-1]; !reflect.DeepEqual(last, f.prior) {
				t.Errorf("rollback must re-save the prior config verbatim, saved %+v", last)
			}
		})
	}
}

// TestTransactProveFailureCarriesTheVerdict: a failed proof is not an error; its
// detail is the reason and its verdict travels with the Outcome.
func TestTransactProveFailureCarriesTheVerdict(t *testing.T) {
	f := newTxFake()
	f.proveStatus = prove.StatusFail
	o := Transact(f.deps(), toRocm)
	if o.Err != nil || o.Reason != "not resident" || o.Prove.Status != prove.StatusFail {
		t.Errorf("Outcome = %+v, want the failed verdict and its detail", o)
	}
}

// TestTransactRollbackIncompleteIsReported: every failed rollback step is named,
// and the Outcome still says RolledBack rather than presenting a half-restored
// stack as a clean no-op.
func TestTransactRollbackIncompleteIsReported(t *testing.T) {
	f := newTxFake()
	f.proveStatus = prove.StatusFail
	f.restoreErr = errors.New("unit dir gone")
	f.reloadErr = errors.New("bus down")
	f.rollbackRestartErr = errors.New("refused")
	o := Transact(f.deps(), toRocm)
	if !o.RolledBack {
		t.Fatalf("expected RolledBack, got %+v", o)
	}
	for _, want := range []string{"did not fully complete", "RestoreUnits failed", "DaemonReload failed", "Restart(villa-llama-small.service) failed"} {
		if !strings.Contains(o.Reason, want) {
			t.Errorf("Reason %q is missing %q", o.Reason, want)
		}
	}
}

// TestTransactHoldsTheStackLock (#250): while a transaction runs, a second mutation
// on the same lock file gets ErrBusy (the dashboard's non-blocking binding), and the
// lock is free again once the transaction returns.
func TestTransactHoldsTheStackLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), stacklock.FileName)
	f := newTxFake()
	d := f.deps()
	d.Lock = func() (*stacklock.Lock, error) { return stacklock.Acquire(path) }
	var during error
	d.Prove = func(context.Context, string) prove.Verdict {
		_, during = stacklock.TryAcquire(path)
		return prove.Verdict{Status: prove.StatusPass}
	}
	Transact(d, toRocm)
	if !errors.Is(during, stacklock.ErrBusy) {
		t.Errorf("a second mutation during the transaction got %v, want ErrBusy", during)
	}
	after, err := stacklock.TryAcquire(path)
	if err != nil {
		t.Fatalf("the lock must be released when the transaction returns: %v", err)
	}
	_ = after.Release()
}
