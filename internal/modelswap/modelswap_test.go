package modelswap

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// modelswap_test.go holds the swap-ordering asserts relocated from cmd/villa
// model_test.go — the security contract (D-09 / STATE.md [03-05]): resolve(catalog) →
// fit-guard → auto-pull → SaveVilla BEFORE reconcileAndWrite → restart ONLY the
// inference service, skipping the restart on a no-op (WR-06). Run is driven through
// stubbed Deps with no live host; the typed Result is asserted directly.

const installService = "villa-llama.service"

// swapRecorder records each side-effecting seam call so the test can assert ordering
// (SaveVilla BEFORE Restart) and that ONLY the inference unit is restarted.
type swapRecorder struct {
	callOrder         []string
	saved             config.VillaConfig
	pulled            []string
	restarted         []string
	downloaded        map[string]bool // models considered already-on-disk
	fitOverrides      map[string]bool // model id -> Fits result
	reconcileNoChange bool            // make reconcileAndWrite report "nothing changed" (WR-06)
}

func newSwapStub(rec *swapRecorder) Deps {
	return Deps{
		InstallServiceName: installService,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "current-model", Backend: "vulkan"}, nil
		},
		ResolveCatalog: func(name string) (catalog.CatalogModel, bool) {
			known := map[string]catalog.CatalogModel{
				"fits-model":    {ID: "fits-model", Quant: "Q4", DefaultCtx: 4096},
				"fits-undl":     {ID: "fits-undl", Quant: "Q4", DefaultCtx: 4096},
				"toobig-model":  {ID: "toobig-model", Quant: "Q4", DefaultCtx: 4096},
				"current-model": {ID: "current-model", Quant: "Q4", DefaultCtx: 4096},
			}
			m, ok := known[name]
			return m, ok
		},
		Fits: func(m catalog.CatalogModel) (bool, string) {
			if rec.fitOverrides != nil {
				if ok := rec.fitOverrides[m.ID]; !ok {
					return false, "won't fit envelope (test)"
				}
			}
			return true, ""
		},
		IsDownloaded: func(m catalog.CatalogModel) bool {
			return rec.downloaded[m.ID]
		},
		Pull: func(m catalog.CatalogModel) error {
			rec.callOrder = append(rec.callOrder, "pull:"+m.ID)
			rec.pulled = append(rec.pulled, m.ID)
			return nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			rec.callOrder = append(rec.callOrder, "save:"+c.Model)
			rec.saved = c
			return nil
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) {
			rec.callOrder = append(rec.callOrder, "write")
			return !rec.reconcileNoChange, nil
		},
		Restart: func(service string) error {
			rec.callOrder = append(rec.callOrder, "restart:"+service)
			rec.restarted = append(rec.restarted, service)
			return nil
		},
	}
}

// TestSwapResolvesThroughCatalog: an unknown id is a typed refuse with zero side
// effects (never interpreted as a path).
func TestSwapResolvesThroughCatalog(t *testing.T) {
	rec := &swapRecorder{downloaded: map[string]bool{}}
	res := Run(newSwapStub(rec), "no-such-model")
	if !res.Refused || !res.Unknown {
		t.Fatalf("unknown id must be a Refused/Unknown result, got %+v", res)
	}
	if len(rec.callOrder) != 0 {
		t.Errorf("unknown swap must fire zero seams, got %v", rec.callOrder)
	}
}

// TestSwapFitGuardFirst: a non-fitting model is refused BEFORE any pull/save/restart
// (fit-guard first, D-09.1).
func TestSwapFitGuardFirst(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"toobig-model": true},
		fitOverrides: map[string]bool{}, // nothing fits
	}
	res := Run(newSwapStub(rec), "toobig-model")
	if !res.Refused {
		t.Fatalf("non-fitting target must be Refused, got %+v", res)
	}
	if res.Unknown {
		t.Errorf("a known-but-too-big model must not be flagged Unknown")
	}
	if len(rec.saved.Model) != 0 || len(rec.callOrder) != 0 {
		t.Errorf("non-fitting swap must not pull/save/write/restart, got calls %v", rec.callOrder)
	}
	if !strings.Contains(res.Reason, "fit") {
		t.Errorf("refusal reason should mention fit, got %q", res.Reason)
	}
}

// TestSwapSaveBeforeReconcileAndInferenceOnlyRestart: a fitting absent model →
// pull → SaveVilla called BEFORE reconcileAndWrite → restart targets ONLY the
// inference service (the ordering contract).
func TestSwapSaveBeforeReconcileAndInferenceOnlyRestart(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{}, // not on disk → auto-pull
		fitOverrides: map[string]bool{"fits-undl": true},
	}
	res := Run(newSwapStub(rec), "fits-undl")
	if !res.Switched {
		t.Fatalf("fitting auto-pull swap must Switch, got %+v", res)
	}
	if !res.Pulled || len(rec.pulled) != 1 || rec.pulled[0] != "fits-undl" {
		t.Errorf("expected auto-pull of fits-undl, got pulled=%v Result.Pulled=%v", rec.pulled, res.Pulled)
	}
	if rec.saved.Model != "fits-undl" {
		t.Errorf("config not persisted to the new model, got %q", rec.saved.Model)
	}

	// pull < save < write < restart; SaveVilla precedes the reconcileAndWrite "write".
	pullIdx, saveIdx, writeIdx, restartIdx := -1, -1, -1, -1
	for i, c := range rec.callOrder {
		switch {
		case strings.HasPrefix(c, "pull:"):
			pullIdx = i
		case strings.HasPrefix(c, "save:"):
			saveIdx = i
		case c == "write":
			writeIdx = i
		case strings.HasPrefix(c, "restart:") && restartIdx == -1:
			restartIdx = i
		}
	}
	if !(pullIdx < saveIdx && saveIdx < writeIdx && writeIdx < restartIdx) {
		t.Errorf("expected pull<save<write<restart, got %v", rec.callOrder)
	}
	// ONLY the inference unit is restarted (network/volume untouched).
	if len(rec.restarted) != 1 || rec.restarted[0] != installService {
		t.Errorf("expected only %s restarted, got %v", installService, rec.restarted)
	}
}

// TestSwapAlreadyDownloadedSkipsPull: a fitting already-present model swaps without a
// re-pull (save before restart, inference-only).
func TestSwapAlreadyDownloadedSkipsPull(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.Switched {
		t.Fatalf("fitting swap should Switch, got %+v", res)
	}
	if res.Pulled || len(rec.pulled) != 0 {
		t.Errorf("already-downloaded model must not be re-pulled, got %v", rec.pulled)
	}
	saveIdx, restartIdx := -1, -1
	for i, c := range rec.callOrder {
		if strings.HasPrefix(c, "save:") {
			saveIdx = i
		}
		if strings.HasPrefix(c, "restart:") && restartIdx == -1 {
			restartIdx = i
		}
	}
	if saveIdx == -1 || restartIdx == -1 || saveIdx > restartIdx {
		t.Errorf("expected save BEFORE restart, call order: %v", rec.callOrder)
	}
}

// TestSwapNoOpSkipsRestart: a no-op (units already up to date) persists config but
// skips the restart (WR-06).
func TestSwapNoOpSkipsRestart(t *testing.T) {
	rec := &swapRecorder{
		downloaded:        map[string]bool{"fits-model": true},
		fitOverrides:      map[string]bool{"fits-model": true},
		reconcileNoChange: true,
	}
	res := Run(newSwapStub(rec), "fits-model")
	if !res.NoOp || res.Switched {
		t.Fatalf("no-op swap must report NoOp (not Switched), got %+v", res)
	}
	if rec.saved.Model != "fits-model" {
		t.Errorf("config must still be persisted on a no-op, got %q", rec.saved.Model)
	}
	if len(rec.restarted) != 0 {
		t.Errorf("a no-op reconcile must NOT restart the service, got %v", rec.restarted)
	}
}

// TestSwapPullFailureIsErrNotRefuse: a pull failure is an Err (FailedStep=pull), not a
// policy refusal — the cmd layer maps both to exit 1 but the distinction matters for
// the dashboard handler.
func TestSwapPullFailureIsErrNotRefuse(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{},
		fitOverrides: map[string]bool{"fits-undl": true},
	}
	d := newSwapStub(rec)
	d.Pull = func(catalog.CatalogModel) error { return errors.New("checksum mismatch") }
	res := Run(d, "fits-undl")
	if res.Refused {
		t.Errorf("a pull failure is an Err, not a Refused")
	}
	if res.Err == nil || res.FailedStep != "pull" {
		t.Errorf("expected Err at step pull, got %+v", res)
	}
	if len(rec.restarted) != 0 || rec.saved.Model != "" {
		t.Errorf("a pull failure must short-circuit before save/restart, got calls %v", rec.callOrder)
	}
}
