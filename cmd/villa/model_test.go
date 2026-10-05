package main

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/prove"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/stacklock"
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
// never interpreted as a filesystem path (V5).
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

	var gotModel catalog.Model
	var gotDir string
	pullFn = func(_ context.Context, m catalog.Model, dir string) error {
		gotModel = m
		gotDir = dir
		return nil
	}
	// Point the models dir under a temp dir so MkdirAll does not touch the real XDG path.
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd, out, _ := newTestCmd()
	code := runModelPull(cmd, "qwen3.5-0.8b")
	if code != exitPass {
		t.Fatalf("expected exit 0, got %d", code)
	}
	if gotModel.ID != "qwen3.5-0.8b" {
		t.Errorf("downloader got model %q, want qwen3.5-0.8b", gotModel.ID)
	}
	if !strings.HasSuffix(gotDir, "/villa/models") {
		t.Errorf("models dir = %q, want suffix /villa/models", gotDir)
	}
	if !strings.Contains(out.String(), "verified") || !strings.Contains(out.String(), "qwen3.5-0.8b") {
		t.Errorf("success output missing model id / verified marker: %q", out.String())
	}
}

// TestModelPullDownloadFailure: a downloader error maps to a non-zero exit and is
// surfaced on stderr (no warn tier for pull).
func TestModelPullDownloadFailure(t *testing.T) {
	origPull := pullFn
	t.Cleanup(func() { pullFn = origPull })
	pullFn = func(_ context.Context, _ catalog.Model, _ string) error {
		return errors.New("checksum mismatch")
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd, _, errOut := newTestCmd()
	code := runModelPull(cmd, "qwen3.5-0.8b")
	if code == exitPass {
		t.Fatalf("expected non-zero exit on download failure, got %d", code)
	}
	if !strings.Contains(errOut.String(), "failed") {
		t.Errorf("expected a failure message on stderr, got %q", errOut.String())
	}
}

// TestModelPullRegistered: the `model pull` verb is wired into the command tree
// and does not collide with a Phase-3 lifecycle verb name.
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
	// changed" so the no-op-swap-skips-restart path is exercisable.
	reconcileNoChange bool
	// proveFails makes the cutover proof fail, so the swap rolls back.
	proveFails bool
	// vision is the loaded config's vision decision; visionFits names the targets
	// whose projector fits beside them.
	vision     bool
	visionFits map[string]bool
	// ctx is the loaded config's ctx; overAt names the ctx values a target is over
	// the envelope at.
	ctx    int
	overAt map[int]bool
}

func newSwapStub(rec *swapRecorder) *modelswap.Deps {
	return &modelswap.Deps{
		Tx: stackapply.TxDeps{
			Lock: func() (*stacklock.Lock, error) { return acquireStackLock() },
			LoadConfig: func() (config.VillaConfig, error) {
				return config.VillaConfig{Model: "current-model", Backend: "vulkan", Vision: rec.vision, Ctx: rec.ctx}, nil
			},
			Capture: func(config.VillaConfig) (map[string]string, error) {
				return map[string]string{"villa-llama.container": "prior unit"}, nil
			},
			SaveConfig: func(c config.VillaConfig) error {
				rec.saved = c
				return nil
			},
			Apply: func(config.VillaConfig) ([]orchestrate.Unit, error) {
				if rec.reconcileNoChange {
					return nil, nil
				}
				return []orchestrate.Unit{{Name: "villa-llama.container"}}, nil
			},
			Restore:      func(map[string]string) error { return nil },
			DaemonReload: func() error { return nil },
			IsActive:     func(string) (string, error) { return "active", nil },
			Restart: func(service string) error {
				rec.restarted = append(rec.restarted, service)
				return nil
			},
			Prove: func(context.Context, string) prove.Verdict {
				if rec.proveFails {
					return prove.Verdict{Status: prove.StatusFail, Detail: "not resident (test)"}
				}
				return prove.Verdict{Status: prove.StatusPass}
			},
			Service: installServiceName,
		},
		ResolveCatalog: func(name string) (catalog.Model, bool) {
			known := map[string]catalog.Model{
				"fits-model":    {ID: "fits-model", Quant: "Q4", DefaultCtx: 4096},
				"fits-undl":     {ID: "fits-undl", Quant: "Q4", DefaultCtx: 4096},
				"toobig-model":  {ID: "toobig-model", Quant: "Q4", DefaultCtx: 4096},
				"current-model": {ID: "current-model", Quant: "Q4", DefaultCtx: 4096},
			}
			m, ok := known[name]
			return m, ok
		},
		Fits: func(m catalog.Model, c config.VillaConfig) modelswap.Fit {
			if rec.fitOverrides != nil {
				if ok := rec.fitOverrides[m.ID]; !ok {
					return modelswap.Fit{OverEnvelope: true, Detail: "won't fit envelope (test)"}
				}
			}
			if rec.overAt[c.Ctx] {
				return modelswap.Fit{OverEnvelope: true, Detail: "needs more (test)"}
			}
			return modelswap.Fit{OK: true, Vision: rec.visionFits[m.ID]}
		},
		IsDownloaded: func(m catalog.Model) bool {
			return rec.downloaded[m.ID]
		},
		Pull: func(m catalog.Model) error {
			rec.pulled = append(rec.pulled, m.ID)
			return nil
		},
	}
}

// TestModelListLoadedVsAvailable: the config'd model is marked "loaded"; the rest
// are "available" (MODEL-01).
func TestModelListLoadedVsAvailable(t *testing.T) {
	d := &listDeps{
		loadCatalog: func() (catalog.Catalog, []string, error) { return catalog.Load("") },
		loadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen3.5-0.8b"}, nil
		},
	}
	cmd, out, _ := newTestCmd()
	code := runModelList(cmd, listOpts{}, d)
	if code != exitPass {
		t.Fatalf("expected exit 0, got %d", code)
	}
	s := out.String()
	// The loaded model must be flagged loaded; at least one other entry available.
	if !strings.Contains(s, "qwen3.5-0.8b") {
		t.Fatalf("list output missing the loaded model id: %q", s)
	}
	loadedLineFound := false
	availFound := false
	for _, line := range strings.Split(s, "\n") {
		if strings.Contains(line, "qwen3.5-0.8b") && strings.Contains(line, "loaded") {
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

	t.Run("no-op reconcile → exit 0 + 'no restart needed'", func(t *testing.T) {
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

// TestModelPullUsesTheCommandContext pins that a Ctrl-C can actually interrupt a
// weight download.
//
// `villa model pull` is the longest-running command in the tree — a multi-GB
// transfer — and it passed context.Background() to the downloader, so the
// SIGINT-cancelled context main installs never reached the transfer and a Ctrl-C
// could not stop it. The pull must observe the command's own context.
func TestModelPullUsesTheCommandContext(t *testing.T) {
	origPull := pullFn
	t.Cleanup(func() { pullFn = origPull })

	var gotCtx context.Context
	pullFn = func(ctx context.Context, _ catalog.Model, _ string) error {
		gotCtx = ctx
		return nil
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	ctx, cancel := context.WithCancel(t.Context())
	cmd, _, _ := newTestCmd()
	cmd.SetContext(ctx)

	if code := runModelPull(cmd, "qwen3.5-0.8b"); code != exitPass {
		t.Fatalf("pull exit = %d, want %d", code, exitPass)
	}
	if gotCtx == nil {
		t.Fatal("downloader received a nil context")
	}

	// Cancelling the command's context must be visible to the downloader — that is
	// the whole point of threading it through.
	cancel()
	if gotCtx.Err() == nil {
		t.Fatal("the downloader's context did not observe the command's cancellation — " +
			"a Ctrl-C cannot interrupt a multi-GB model pull")
	}
}

// TestModelPullToleratesNilCommandContext keeps the direct-call path working: a
// cobra Command that was never Execute()d has a nil Context(), and the pull must
// fall back to Background rather than handing nil to the downloader.
func TestModelPullToleratesNilCommandContext(t *testing.T) {
	origPull := pullFn
	t.Cleanup(func() { pullFn = origPull })

	var gotCtx context.Context
	pullFn = func(ctx context.Context, _ catalog.Model, _ string) error {
		gotCtx = ctx
		return nil
	}
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	cmd, _, _ := newTestCmd() // never Execute()d → cmd.Context() is nil
	if code := runModelPull(cmd, "qwen3.5-0.8b"); code != exitPass {
		t.Fatalf("pull exit = %d, want %d", code, exitPass)
	}
	if gotCtx == nil {
		t.Fatal("downloader received a nil context from a never-executed command")
	}
}

// TestModelSwapLockFailureBlocksBeforeAnyMutation is ADR-0010: when the
// cross-process lock cannot be taken, runModelSwap must refuse WITHOUT calling
// modelswap.Run at all — never mutate while unable to exclude a concurrent
// dashboard switch.
// TestModelSwapSaysWhenVisionChanged (#299): a swap that turns vision off or on
// says so, because the operator did not ask for it by name; a swap that leaves it
// alone says nothing about it.
func TestModelSwapSaysWhenVisionChanged(t *testing.T) {
	cases := []struct {
		name       string
		vision     bool
		visionFits map[string]bool
		want       string
	}{
		{"on to off", true, nil, "vision turned off: fits-model has no projector that fits beside it\n"},
		{"off to on", false, map[string]bool{"fits-model": true}, "vision turned on: fits-model ships a projector\n"},
		{"unchanged", true, map[string]bool{"fits-model": true}, ""},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			rec := &swapRecorder{
				downloaded:   map[string]bool{"fits-model": true},
				fitOverrides: map[string]bool{"fits-model": true},
				vision:       tc.vision,
				visionFits:   tc.visionFits,
			}
			cmd, out, _ := newTestCmd()
			if code := runModelSwap(cmd, "fits-model", newSwapStub(rec)); code != exitPass {
				t.Fatalf("swap should exit 0, got %d", code)
			}
			lines := strings.SplitAfter(out.String(), "\n")
			got := strings.Join(lines[1:], "")
			if got != tc.want {
				t.Errorf("after the swapped line got %q, want %q", got, tc.want)
			}
		})
	}
}

// TestModelSwapSaysWhenCtxWasReset (#301): a target that does not fit at the
// configured ctx is served at its default_ctx, and the swap says so, because the
// operator asked for a model, not for a smaller context.
func TestModelSwapSaysWhenCtxWasReset(t *testing.T) {
	rec := &swapRecorder{
		downloaded:   map[string]bool{"fits-model": true},
		fitOverrides: map[string]bool{"fits-model": true},
		ctx:          131072,
		overAt:       map[int]bool{131072: true},
	}
	cmd, out, _ := newTestCmd()
	if code := runModelSwap(cmd, "fits-model", newSwapStub(rec)); code != exitPass {
		t.Fatalf("swap should exit 0, got %d", code)
	}
	if rec.saved.Ctx != 4096 {
		t.Errorf("saved ctx %d, want fits-model's default 4096", rec.saved.Ctx)
	}
	lines := strings.SplitAfter(out.String(), "\n")
	if got, want := strings.Join(lines[1:], ""), "ctx reset to 4096: 131072 does not fit fits-model\n"; got != want {
		t.Errorf("after the swapped line got %q, want %q", got, want)
	}
}

// TestModelSwapRefusalSaysWhy (#301): only a memory shortfall reads as "won't fit";
// a speculation refusal is reported as itself.
func TestModelSwapRefusalSaysWhy(t *testing.T) {
	d := newSwapStub(&swapRecorder{})
	d.Fits = func(catalog.Model, config.VillaConfig) modelswap.Fit {
		return modelswap.Fit{Detail: "speculation: ngram requested but fits-model is not qualified for it; refusing"}
	}
	cmd, _, errOut := newTestCmd()
	if code := runModelSwap(cmd, "fits-model", d); code == exitPass {
		t.Fatal("a refused swap must not exit 0")
	}
	want := "model swap: refusing — speculation: ngram requested but fits-model is not qualified for it; refusing\n"
	if errOut.String() != want {
		t.Errorf("stderr %q, want %q", errOut.String(), want)
	}
}

func TestModelSwapLockFailureBlocksBeforeAnyMutation(t *testing.T) {
	prevLock := acquireStackLock
	acquireStackLock = func() (*stacklock.Lock, error) { return nil, errors.New("lock held") }
	t.Cleanup(func() { acquireStackLock = prevLock })

	rec := &swapRecorder{downloaded: map[string]bool{"fits-model": true}, fitOverrides: map[string]bool{"fits-model": true}}
	d := newSwapStub(rec)
	cmd, _, errOut := newTestCmd()
	code := runModelSwap(cmd, "fits-model", d)
	if code != exitBlocked {
		t.Fatalf("a lock failure must exit 1, got %d", code)
	}
	if rec.saved.Model != "" || len(rec.restarted) != 0 || len(rec.pulled) != 0 {
		t.Errorf("a lock failure must fire ZERO seams, got saved=%q restarted=%v pulled=%v", rec.saved.Model, rec.restarted, rec.pulled)
	}
	if !strings.Contains(errOut.String(), "lock held") {
		t.Errorf("expected the lock error surfaced, got %q", errOut.String())
	}
}

// TestModelSwapRollbackExitsBlocked: a failed cutover proof restored the prior model,
// so the verb says so and exits 1. It used to fall through to "swapped to", exit 0.
func TestModelSwapRollbackExitsBlocked(t *testing.T) {
	rec := &swapRecorder{downloaded: map[string]bool{"fits-model": true}, proveFails: true}
	cmd, out, errOut := newTestCmd()
	code := runModelSwap(cmd, "fits-model", newSwapStub(rec))
	if code != exitBlocked {
		t.Fatalf("a rolled-back swap must exit 1, got %d", code)
	}
	if strings.Contains(out.String(), "swapped to") || !strings.Contains(errOut.String(), "rolled back") {
		t.Errorf("a rolled-back swap must say so, stdout %q stderr %q", out.String(), errOut.String())
	}
}

// TestModelOnDiskCountsSidecars (#299): weights fetched before a projector existed
// are not "on disk" for a swap that turns vision on, so the swap pulls the
// projector instead of rendering --mmproj at a file that is not there.
func TestModelOnDiskCountsSidecars(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	m := catalog.Model{
		ID:        "vision-model",
		Shards:    []catalog.Shard{{Filename: "model.gguf"}},
		Projector: &catalog.Sidecar{Shards: []catalog.Shard{{Filename: "mmproj.gguf"}}},
	}
	if err := os.MkdirAll(modelsDir(), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(modelsDir(), "model.gguf"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if modelOnDisk(m) {
		t.Fatal("weights without their projector must not count as on disk")
	}
	if err := os.WriteFile(filepath.Join(modelsDir(), "mmproj.gguf"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	if !modelOnDisk(m) {
		t.Fatal("weights plus projector must count as on disk")
	}
}
