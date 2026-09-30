package modelswap

import (
	"context"
	"errors"
	"reflect"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
)

// The transaction's ordering, restart set and rollback are asserted once, on the
// frame (internal/stackapply/transact_test.go). These tests cover the swap's own
// ordering inside it — the security contract: resolve through the catalog, fit
// guard, pull — and what it writes.

type swapFake struct {
	calls       []string
	saved       []config.VillaConfig
	fits        bool
	downloaded  bool
	pullErr     error
	unchanged   bool
	proveStatus string
}

var known = map[string]catalog.Model{
	"target": {ID: "target", Quant: "Q8_0"},
}

func (f *swapFake) deps() Deps {
	return Deps{
		Tx: stackapply.TxDeps{
			Lock: func() (*stacklock.Lock, error) { f.calls = append(f.calls, "lock"); return nil, nil },
			LoadConfig: func() (config.VillaConfig, error) {
				f.calls = append(f.calls, "load")
				return config.VillaConfig{Model: "current", Quant: "Q4_K_M", Backend: "vulkan"}, nil
			},
			SaveConfig: func(c config.VillaConfig) error {
				f.calls = append(f.calls, "save")
				f.saved = append(f.saved, c)
				return nil
			},
			Capture: func(config.VillaConfig) (map[string]string, error) {
				f.calls = append(f.calls, "capture")
				return map[string]string{"villa-llama.container": "PRIOR"}, nil
			},
			Apply: func(config.VillaConfig) ([]orchestrate.Unit, error) {
				if f.unchanged {
					return nil, nil
				}
				return []orchestrate.Unit{{Name: "villa-llama.container"}}, nil
			},
			Restore:      func(map[string]string) error { return nil },
			DaemonReload: func() error { return nil },
			IsActive:     func(string) (string, error) { return "active", nil },
			Restart:      func(string) error { f.calls = append(f.calls, "restart"); return nil },
			Prove: func(context.Context, string) prove.Verdict {
				if f.proveStatus != "" {
					return prove.Verdict{Status: f.proveStatus}
				}
				return prove.Verdict{Status: prove.StatusPass}
			},
			Service: "villa-llama.service",
		},
		ResolveCatalog: func(name string) (catalog.Model, bool) {
			f.calls = append(f.calls, "resolve")
			m, ok := known[name]
			return m, ok
		},
		Fits: func(catalog.Model) (bool, string) {
			f.calls = append(f.calls, "fit")
			return f.fits, "needs 9 bytes vs 1 usable"
		},
		IsDownloaded: func(catalog.Model) bool { return f.downloaded },
		Pull: func(catalog.Model) error {
			f.calls = append(f.calls, "pull")
			return f.pullErr
		},
	}
}

// TestSwapOrderIsTheSecurityContract: under the lock, the name resolves through the
// catalog, the fit guard runs, absent weights are pulled, and only then is anything
// captured or persisted; the model and its quant are written.
func TestSwapOrderIsTheSecurityContract(t *testing.T) {
	f := &swapFake{fits: true}
	res := Run(f.deps(), "target")
	if !res.Switched || !res.Pulled || res.FromModel != "current" || res.ToModel != "target" {
		t.Fatalf("expected a pulled, proven swap current→target, got %+v", res)
	}
	want := []string{"lock", "load", "resolve", "fit", "pull", "capture", "save", "restart"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls %v, want %v", f.calls, want)
	}
	if s := f.saved[0]; s.Model != "target" || s.Quant != "Q8_0" || s.Backend != "vulkan" {
		t.Errorf("saved %+v, want the model and its quant, backend untouched", s)
	}
}

// TestSwapResolvesThroughCatalog: an unknown id is refused, never treated as a path.
func TestSwapResolvesThroughCatalog(t *testing.T) {
	f := &swapFake{fits: true}
	res := Run(f.deps(), "../../etc/passwd")
	if !res.Refused || !res.Unknown || len(f.saved) != 0 {
		t.Fatalf("expected an unknown-model refusal with no side effect, got %+v", res)
	}
}

// TestSwapFitGuardFirst: a non-fitting target refuses before any pull or capture.
func TestSwapFitGuardFirst(t *testing.T) {
	f := &swapFake{fits: false}
	res := Run(f.deps(), "target")
	if !res.Refused || res.Reason == "" || res.Unknown {
		t.Fatalf("expected a fit refusal, got %+v", res)
	}
	want := []string{"lock", "load", "resolve", "fit"}
	if !reflect.DeepEqual(f.calls, want) {
		t.Errorf("calls %v, want the refusal right after the fit guard", f.calls)
	}
}

// TestSwapAlreadyDownloadedSkipsPull and a failed pull is an error, not a refusal,
// with nothing captured.
func TestSwapPull(t *testing.T) {
	f := &swapFake{fits: true, downloaded: true}
	if res := Run(f.deps(), "target"); res.Pulled || !res.Switched {
		t.Fatalf("downloaded weights must not be pulled, got %+v", res)
	}

	f = &swapFake{fits: true, pullErr: errors.New("offline")}
	res := Run(f.deps(), "target")
	if res.Refused || res.FailedStep != "pull" || res.Err == nil || len(f.saved) != 0 {
		t.Fatalf("expected a pull error before capture, got %+v", res)
	}
}

// TestSwapNoOpSkipsRestartAndProve: persisted, but the units already match, so
// nothing is restarted or proven (WR-06).
func TestSwapNoOpSkipsRestartAndProve(t *testing.T) {
	f := &swapFake{fits: true, downloaded: true, unchanged: true}
	res := Run(f.deps(), "target")
	if !res.NoOp || res.Switched || len(f.saved) != 1 {
		t.Fatalf("expected a persisted NoOp, got %+v", res)
	}
	for _, c := range f.calls {
		if c == "restart" {
			t.Errorf("a no-op swap must not restart anything: %v", f.calls)
		}
	}
}

// TestSwapProveFailureRollsBack: the Result still names both models.
func TestSwapProveFailureRollsBack(t *testing.T) {
	f := &swapFake{fits: true, downloaded: true, proveStatus: prove.StatusFail}
	res := Run(f.deps(), "target")
	if !res.RolledBack || res.FromModel != "current" || res.ToModel != "target" {
		t.Fatalf("expected a rollback naming both models, got %+v", res)
	}
}
