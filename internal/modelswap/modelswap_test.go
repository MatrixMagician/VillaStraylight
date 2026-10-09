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
	// vision is the loaded config's vision decision; fitVision is the fit's answer
	// for the target (its projector fits beside it).
	vision    bool
	fitVision bool
	// ctx is the loaded config's ctx; fitsAt, when set, answers the fit per ctx
	// and records each ctx it was asked about.
	ctx    int
	fitsAt map[int]Fit
	asked  []int
}

var known = map[string]catalog.Model{
	"target": {ID: "target", Quant: "Q8_0", DefaultCtx: 8192},
}

func (f *swapFake) deps() Deps {
	return Deps{
		Tx: stackapply.TxDeps{
			Lock: func() (*stacklock.Lock, error) { f.calls = append(f.calls, "lock"); return nil, nil },
			LoadConfig: func() (config.VillaConfig, error) {
				f.calls = append(f.calls, "load")
				return config.VillaConfig{Model: "current", Quant: "Q4_K_M", Backend: "vulkan", Vision: f.vision, Ctx: f.ctx}, nil
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
			Apply: func(config.VillaConfig) (stackapply.Applied, error) {
				if f.unchanged {
					return stackapply.Applied{}, nil
				}
				return stackapply.Applied{Changed: []orchestrate.Unit{{Name: "villa-llama.container"}}}, nil
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
		Fits: func(_ catalog.Model, c config.VillaConfig) Fit {
			f.calls = append(f.calls, "fit")
			f.asked = append(f.asked, c.Ctx)
			if f.fitsAt != nil {
				return f.fitsAt[c.Ctx]
			}
			if !f.fits {
				return Fit{OverEnvelope: true, Detail: "needs 9 bytes vs 1 usable"}
			}
			return Fit{OK: true, Vision: f.fitVision}
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

// TestSwapVisionFollowsTheTarget (#299): the swap writes the target's own vision
// answer, so a vision stack can move to a text-only entry and back without a hand
// edit, and the Result names both sides so the caller can say what changed.
func TestSwapVisionFollowsTheTarget(t *testing.T) {
	cases := []struct {
		name              string
		vision, fitVision bool
	}{
		{"vision model to text-only turns vision off", true, false},
		{"text-only to a model whose projector fits turns vision on", false, true},
		{"vision to vision stays on", true, true},
		{"text-only to text-only stays off", false, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &swapFake{fits: true, downloaded: true, vision: tc.vision, fitVision: tc.fitVision}
			res := Run(f.deps(), "target")
			if !res.Switched {
				t.Fatalf("expected a switch, got %+v", res)
			}
			if got := f.saved[0].Vision; got != tc.fitVision {
				t.Errorf("saved vision = %v, want %v", got, tc.fitVision)
			}
			if res.FromVision != tc.vision || res.ToVision != tc.fitVision {
				t.Errorf("Result vision %v -> %v, want %v -> %v", res.FromVision, res.ToVision, tc.vision, tc.fitVision)
			}
		})
	}
}

// TestSwapRefusalLeavesVisionAlone (#299): a refused swap writes nothing, so the
// vision decision it would have changed is untouched.
func TestSwapRefusalLeavesVisionAlone(t *testing.T) {
	f := &swapFake{fits: false, vision: true}
	res := Run(f.deps(), "target")
	if !res.Refused || len(f.saved) != 0 || !res.FromVision || res.ToVision != res.FromVision {
		t.Fatalf("expected a refusal that keeps vision on, got %+v saved=%v", res, f.saved)
	}
}

// TestSwapSizesAtTheServedCtx (#301): the target is sized at the configured ctx, and
// when it does not fit there the swap falls back to the target's default_ctx and
// writes it; a target that fits at neither is refused naming the default's
// shortfall, and a refusal that is not a memory shortfall is not retried.
func TestSwapSizesAtTheServedCtx(t *testing.T) {
	short := func(d string) Fit { return Fit{OverEnvelope: true, Detail: d} }
	cases := []struct {
		name       string
		ctx        int
		fitsAt     map[int]Fit
		wantAsked  []int
		wantCtx    int // the ctx written; -1 for a refusal
		wantDetail string
		wantVision bool
	}{
		{"unset ctx sizes at the default and stays unset",
			0, map[int]Fit{0: {OK: true}}, []int{0}, 0, "", false},
		{"fits at the configured ctx keeps it",
			131072, map[int]Fit{131072: {OK: true, Vision: true}}, []int{131072}, 131072, "", true},
		{"short at the configured ctx falls back to the default",
			131072, map[int]Fit{131072: short("big"), 8192: {OK: true, Vision: true}}, []int{131072, 8192}, 8192, "", true},
		{"short at both refuses with the default's shortfall",
			131072, map[int]Fit{131072: short("big"), 8192: short("needs 9 GiB at default")}, []int{131072, 8192}, -1, "needs 9 GiB at default", false},
		{"a configured ctx below the default is not raised",
			4096, map[int]Fit{4096: short("small")}, []int{4096}, -1, "small", false},
		{"a speculation refusal is not retried at the default",
			131072, map[int]Fit{131072: {Detail: "speculation: ngram requested but target is not qualified for it; refusing"}}, []int{131072}, -1, "speculation: ngram requested but target is not qualified for it; refusing", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			f := &swapFake{downloaded: true, ctx: tc.ctx, fitsAt: tc.fitsAt}
			res := Run(f.deps(), "target")
			if !reflect.DeepEqual(f.asked, tc.wantAsked) {
				t.Errorf("fit asked at ctx %v, want %v", f.asked, tc.wantAsked)
			}
			if tc.wantCtx < 0 {
				if !res.Refused || res.Reason != tc.wantDetail || len(f.saved) != 0 {
					t.Fatalf("expected a refusal %q with nothing saved, got %+v", tc.wantDetail, res)
				}
				if res.ToCtx != tc.ctx {
					t.Errorf("a refusal reports ToCtx %d, want the prior %d", res.ToCtx, tc.ctx)
				}
				return
			}
			if !res.Switched {
				t.Fatalf("expected a switch, got %+v", res)
			}
			if got := f.saved[0]; got.Ctx != tc.wantCtx || got.Vision != tc.wantVision {
				t.Errorf("saved ctx %d vision %v, want %d %v", got.Ctx, got.Vision, tc.wantCtx, tc.wantVision)
			}
			if res.FromCtx != tc.ctx || res.ToCtx != tc.wantCtx {
				t.Errorf("Result ctx %d -> %d, want %d -> %d", res.FromCtx, res.ToCtx, tc.ctx, tc.wantCtx)
			}
		})
	}
}

// TestSwapOnPullAnnouncesOnlyARealPull (#301): the pull is announced from inside the
// swap, after the fit guard, so a refused target is never announced as pulling.
func TestSwapOnPullAnnouncesOnlyARealPull(t *testing.T) {
	for _, tc := range []struct {
		name             string
		fits, downloaded bool
		want             []string
	}{
		{"absent and fitting", true, false, []string{"target"}},
		{"absent but refused", false, false, nil},
		{"already on disk", true, true, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f := &swapFake{fits: tc.fits, downloaded: tc.downloaded}
			d := f.deps()
			var announced []string
			d.OnPull = func(m catalog.Model) { announced = append(announced, m.ID) }
			Run(d, "target")
			if !reflect.DeepEqual(announced, tc.want) {
				t.Errorf("announced %v, want %v", announced, tc.want)
			}
		})
	}
}
