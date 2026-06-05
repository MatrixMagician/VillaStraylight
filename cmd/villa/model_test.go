package main

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
)

// newTestCmd returns a cobra command whose stdout/stderr are captured buffers so
// runModelPull's output can be asserted without spawning a subprocess.
func newTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "pull"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// TestModelPullUnknownNameRejected: an unknown catalog name exits non-zero and is
// never interpreted as a filesystem path (V5 / T-02-04).
func TestModelPullUnknownNameRejected(t *testing.T) {
	cmd, _, errOut := newTestCmd()
	// A name that would be a path-traversal attempt must still be a clean lookup miss.
	for _, name := range []string{"no-such-model", "../../etc/passwd"} {
		errOut.Reset()
		code := runModelPull(cmd, name)
		if code == exitPass {
			t.Errorf("name %q: expected non-zero exit, got %d", name, code)
		}
		if !strings.Contains(errOut.String(), "unknown model") {
			t.Errorf("name %q: expected an unknown-model error, got %q", name, errOut.String())
		}
	}
}

// TestModelPullSuccess: a known catalog name with a stubbed downloader exits 0 and
// prints a verified success line. The pullFn seam avoids live network.
func TestModelPullSuccess(t *testing.T) {
	origPull := pullFn
	t.Cleanup(func() { pullFn = origPull })

	var gotModel catalog.CatalogModel
	var gotDir string
	pullFn = func(_ context.Context, m catalog.CatalogModel, dir string) error {
		gotModel = m
		gotDir = dir
		return nil
	}
	// Point the models dir under a temp dir so MkdirAll does not touch the real XDG path.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd, out, _ := newTestCmd()
	code := runModelPull(cmd, "qwen2.5-0.5b")
	if code != exitPass {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if gotModel.ID != "qwen2.5-0.5b" {
		t.Errorf("downloader got model %q, want qwen2.5-0.5b", gotModel.ID)
	}
	if !strings.HasSuffix(gotDir, "/villa/models") {
		t.Errorf("models dir = %q, want suffix /villa/models", gotDir)
	}
	if !strings.Contains(out.String(), "verified") || !strings.Contains(out.String(), "qwen2.5-0.5b") {
		t.Errorf("success output missing model id / verified marker: %q", out.String())
	}
}

// TestModelPullDownloadFailure: a downloader error maps to a non-zero exit and is
// surfaced on stderr (no warn tier for pull).
func TestModelPullDownloadFailure(t *testing.T) {
	origPull := pullFn
	t.Cleanup(func() { pullFn = origPull })
	pullFn = func(_ context.Context, _ catalog.CatalogModel, _ string) error {
		return errors.New("checksum mismatch")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd, _, errOut := newTestCmd()
	code := runModelPull(cmd, "qwen2.5-0.5b")
	if code == exitPass {
		t.Fatalf("expected non-zero exit on download failure, got %d", code)
	}
	if !strings.Contains(errOut.String(), "failed") {
		t.Errorf("expected a failure message on stderr, got %q", errOut.String())
	}
}

// TestModelPullRegistered: the `model pull` verb is wired into the command tree
// and does not collide with a Phase-3 lifecycle verb name (D-13).
func TestModelPullRegistered(t *testing.T) {
	root := newRoot()
	model, _, err := root.Find([]string{"model"})
	if err != nil || model.Name() != "model" {
		t.Fatalf("`model` noun not registered: %v", err)
	}
	pull, _, err := root.Find([]string{"model", "pull"})
	if err != nil || pull.Name() != "pull" {
		t.Fatalf("`model pull` subcommand not registered: %v", err)
	}
	for _, reserved := range []string{"up", "down", "restart", "install", "status"} {
		if model.Name() == reserved {
			t.Errorf("`model` noun collides with reserved Phase-3 verb %q", reserved)
		}
	}
}

// TestModelListRegistered / TestModelSwapRegistered: the new subcommands are wired
// under `model` (MODEL-01/03).
func TestModelListSwapRegistered(t *testing.T) {
	root := newRoot()
	for _, sub := range []string{"list", "swap"} {
		c, _, err := root.Find([]string{"model", sub})
		if err != nil || c.Name() != sub {
			t.Fatalf("`model %s` subcommand not registered: %v", sub, err)
		}
	}
}

// swapRecorder records the side-effecting seam calls so the cmd-layer tests can
// assert the cobra/exit-mapping wiring (the verbatim resolve→fit→pull→save→restart
// ORDERING asserts now live in internal/modelswap; here we only check the exit codes
// + human messages the cobra caller maps modelswap.Result to).
type swapRecorder struct {
	saved        config.VillaConfig
	pulled       []string
	restarted    []string
	downloaded   map[string]bool // models considered already-on-disk
	fitOverrides map[string]bool // model id -> Fits result for recommend stub
	// reconcileNoChange, when true, makes the reconcileAndWrite stub report "nothing
	// changed" so the no-op-swap-skips-restart path (WR-06) is exercisable.
	reconcileNoChange bool
}

func newSwapStub(rec *swapRecorder) *modelswap.Deps {
	return &modelswap.Deps{
		InstallServiceName: installServiceName,
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
			rec.pulled = append(rec.pulled, m.ID)
			return nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			rec.saved = c
			return nil
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) {
			return !rec.reconcileNoChange, nil
		},
		Restart: func(service string) error {
			rec.restarted = append(rec.restarted, service)
			return nil
		},
	}
}

// TestModelListLoadedVsAvailable: the config'd model is marked "loaded"; the rest
// are "available" (D-10 / MODEL-01).
func TestModelListLoadedVsAvailable(t *testing.T) {
	d := &listDeps{
		loadCatalog: func() (catalog.Catalog, []string, error) { return catalog.Load("") },
		loadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen2.5-0.5b"}, nil
		},
	}
	cmd, out, _ := newTestCmd()
	code := runModelList(cmd, listOpts{}, d)
	if code != exitPass {
		t.Fatalf("expected exit 0, got %d", code)
	}
	s := out.String()
	// The loaded model must be flagged loaded; at least one other entry available.
	if !strings.Contains(s, "qwen2.5-0.5b") {
		t.Fatalf("list output missing the loaded model id: %q", s)
	}
	loadedLineFound := false
	availFound := false
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "qwen2.5-0.5b") && strings.Contains(line, "loaded") {
			loadedLineFound = true
		}
		if strings.Contains(line, "available") {
			availFound = true
		}
	}
	if !loadedLineFound {
		t.Errorf("expected the config'd model marked loaded, got:\n%s", s)
	}
	if !availFound {
		t.Errorf("expected at least one available (non-loaded) catalog entry, got:\n%s", s)
	}
}

// TestModelSwapExitMapping exercises the cobra caller's mapping of modelswap.Result
// to exit codes + human messages (the verbatim resolve→fit→pull→save→restart ORDERING
// is asserted in internal/modelswap): non-fitting refusal → exit 1 + "won't fit",
// a fitting swap → exit 0, a no-op → exit 0 + "no restart needed", and an unknown
// name → exit 1 + "unknown".
func TestModelSwapExitMapping(t *testing.T) {
	t.Run("non-fitting target → exit 1 + 'won't fit'", func(t *testing.T) {
		rec := &swapRecorder{
			downloaded:   map[string]bool{"toobig-model": true},
			fitOverrides: map[string]bool{}, // nothing fits
		}
		d := newSwapStub(rec)
		cmd, _, errOut := newTestCmd()
		code := runModelSwap(cmd, "toobig-model", d)
		if code == exitPass {
			t.Fatalf("non-fitting swap must be a clear FAIL, got exit 0")
		}
		if len(rec.saved.Model) != 0 || len(rec.restarted) != 0 || len(rec.pulled) != 0 {
			t.Errorf("non-fitting swap must not save/pull/restart, got saved=%q pulled=%v restarted=%v",
				rec.saved.Model, rec.pulled, rec.restarted)
		}
		if !strings.Contains(errOut.String(), "fit") {
			t.Errorf("expected a 'won't fit' message, got %q", errOut.String())
		}
	})

	t.Run("fitting swap → exit 0, inference-only restart", func(t *testing.T) {
		rec := &swapRecorder{
			downloaded:   map[string]bool{"fits-model": true},
			fitOverrides: map[string]bool{"fits-model": true},
		}
		d := newSwapStub(rec)
		cmd, out, _ := newTestCmd()
		code := runModelSwap(cmd, "fits-model", d)
		if code != exitPass {
			t.Fatalf("fitting swap should exit 0, got %d", code)
		}
		if rec.saved.Model != "fits-model" {
			t.Errorf("config not persisted to the new model, got %q", rec.saved.Model)
		}
		if len(rec.restarted) != 1 || rec.restarted[0] != installServiceName {
			t.Errorf("expected only %s restarted, got %v", installServiceName, rec.restarted)
		}
		if len(rec.pulled) != 0 {
			t.Errorf("already-downloaded model must not be re-pulled, got %v", rec.pulled)
		}
		if !strings.Contains(out.String(), "restarted") {
			t.Errorf("expected a 'restarted' success message, got %q", out.String())
		}
	})

	t.Run("fitting but not downloaded → auto-pull, exit 0 + 'pulling'", func(t *testing.T) {
		rec := &swapRecorder{
			downloaded:   map[string]bool{}, // not present on disk
			fitOverrides: map[string]bool{"fits-undl": true},
		}
		d := newSwapStub(rec)
		cmd, out, _ := newTestCmd()
		code := runModelSwap(cmd, "fits-undl", d)
		if code != exitPass {
			t.Fatalf("fitting auto-pull swap should exit 0, got %d", code)
		}
		if len(rec.pulled) != 1 || rec.pulled[0] != "fits-undl" {
			t.Errorf("expected auto-pull of fits-undl, got %v", rec.pulled)
		}
		if !strings.Contains(out.String(), "pulling fits-undl") {
			t.Errorf("expected a 'pulling' progress message, got %q", out.String())
		}
	})

	t.Run("no-op reconcile → exit 0 + 'no restart needed' (WR-06)", func(t *testing.T) {
		rec := &swapRecorder{
			downloaded:        map[string]bool{"fits-model": true},
			fitOverrides:      map[string]bool{"fits-model": true},
			reconcileNoChange: true, // regenerate found the units already up to date
		}
		d := newSwapStub(rec)
		cmd, out, _ := newTestCmd()
		code := runModelSwap(cmd, "fits-model", d)
		if code != exitPass {
			t.Fatalf("no-op swap should still exit 0, got %d", code)
		}
		if rec.saved.Model != "fits-model" {
			t.Errorf("config must still be persisted, got %q", rec.saved.Model)
		}
		if len(rec.restarted) != 0 {
			t.Errorf("a no-op reconcile must NOT restart the service, got %v", rec.restarted)
		}
		if !strings.Contains(out.String(), "no restart needed") {
			t.Errorf("no-op swap should report no restart was needed, got %q", out.String())
		}
	})

	t.Run("unknown catalog name → exit 1 + 'unknown', no side effects", func(t *testing.T) {
		rec := &swapRecorder{downloaded: map[string]bool{}}
		d := newSwapStub(rec)
		cmd, _, errOut := newTestCmd()
		code := runModelSwap(cmd, "no-such-model", d)
		if code == exitPass {
			t.Fatalf("unknown swap target must exit non-zero")
		}
		if len(rec.saved.Model) != 0 || len(rec.restarted) != 0 || len(rec.pulled) != 0 {
			t.Errorf("unknown swap must fire zero side effects, got saved=%q pulled=%v restarted=%v",
				rec.saved.Model, rec.pulled, rec.restarted)
		}
		if !strings.Contains(errOut.String(), "unknown") {
			t.Errorf("expected an unknown-model error, got %q", errOut.String())
		}
	})
}
