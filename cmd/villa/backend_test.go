package main

import (
	"context"
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/backendswap"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// backendRecorder records the side-effecting backendswap.Deps seam calls so the
// cmd-layer tests assert the cobra/exit-mapping wiring + the dry-run-mutates-nothing
// property. The verbatim capture→mutate→prove→rollback ORDERING is asserted in
// internal/backendswap; here we only check the exit codes + messages the cobra caller
// maps backendswap.Result to, and that --dry-run fires ZERO mutate seams.
type backendRecorder struct {
	saved        []config.VillaConfig // SaveConfig calls
	written      int                  // ReconcileAndWrite calls
	restarted    []string             // Restart calls
	captured     int                  // CaptureUnit calls
	restored     int                  // RestoreUnit calls
	reloaded     int                  // DaemonReload calls
	proved       []string             // Prove targets
	curBackend   string               // LoadConfig's current backend
	fits         bool                 // FitsModel result
	fitReason    string               // FitsModel reason on !fits
	preflightOK  bool                 // PreflightROCm result
	preflightWhy string               // PreflightROCm reason on !ok
	captureErr   error                // CaptureUnit error (refuse path)
	writeErr     error                // ReconcileAndWrite error (rollback path)
	proveStatus  string               // Prove verdict status
	proveDetail  string               // Prove verdict detail
}

// newBackendStub builds a fake *backendswap.Deps over a recorder so runBackendSet is
// driven without a live host. Sensible pass-through defaults (fits, preflight-ok,
// prove-pass) are overridden per-subtest via the recorder fields.
func newBackendStub(rec *backendRecorder) *backendswap.Deps {
	return &backendswap.Deps{
		InstallServiceName: installServiceName,
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "current-model", Backend: rec.curBackend}, nil
		},
		FitsModel: func(_ config.VillaConfig) (bool, string) {
			return rec.fits, rec.fitReason
		},
		PreflightROCm: func(_ config.VillaConfig) (bool, string) {
			return rec.preflightOK, rec.preflightWhy
		},
		CaptureUnit: func() ([]byte, error) {
			if rec.captureErr != nil {
				return nil, rec.captureErr
			}
			rec.captured++
			return []byte("PRIOR-UNIT"), nil
		},
		SaveConfig: func(c config.VillaConfig) error {
			rec.saved = append(rec.saved, c)
			return nil
		},
		ReconcileAndWrite: func(_ config.VillaConfig) (bool, error) {
			rec.written++
			if rec.writeErr != nil {
				return false, rec.writeErr
			}
			return true, nil
		},
		RestoreUnit: func(_ []byte) error {
			rec.restored++
			return nil
		},
		DaemonReload: func() error {
			rec.reloaded++
			return nil
		},
		Restart: func(service string) error {
			rec.restarted = append(rec.restarted, service)
			return nil
		},
		Prove: func(_ context.Context, target string) backendswap.ProveVerdict {
			rec.proved = append(rec.proved, target)
			return backendswap.ProveVerdict{Status: rec.proveStatus, Detail: rec.proveDetail}
		},
	}
}

// TestLivePreflightROCmFamily asserts the LIVE PreflightROCm closure (built in
// liveBackendSwapDeps) is family-aware (D-08): it short-circuits ok=true for a
// non-ROCm backend and runs the gate against the RESOLVED target image for every
// ROCm-family name (SC#2).
//
// It does NOT assert a fixed ok=true: the kernel and firmware floors are knowable
// off-hardware (unlike the GPU/HSA signals, which degrade to typed-Unknown WARNs), so
// on a host whose kernel is below the gfx1151 stability floor — e.g. a CI runner on an
// older kernel — the ROCm gate legitimately returns a confident StatusFail. Instead it
// asserts the live closure reproduces a DIRECT run of the same resolved-image gate, which
// is hermetic (both sides see the same host) and still catches a routing/reduction break.
func TestLivePreflightROCmFamily(t *testing.T) {
	d := liveBackendSwapDeps()

	t.Run("non-ROCm backend short-circuits ok=true", func(t *testing.T) {
		ok, why := d.PreflightROCm(config.VillaConfig{Backend: "vulkan"})
		if !ok || why != "" {
			t.Errorf("vulkan must short-circuit (true,\"\"), got (%v,%q)", ok, why)
		}
	})

	// Every ROCm-family name must route through the resolved-image gate (not be treated as
	// non-ROCm). Assert the closure's verdict equals a direct run of that same gate: on a
	// healthy host both are (true,""); on a sub-floor host both are the SAME (false, floor)
	// — a routing miss would wrongly short-circuit to (true,"") and diverge.
	for _, name := range []string{"rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma"} {
		if !inference.IsROCmFamily(name) {
			t.Fatalf("test premise broken: %q is not a ROCm-family name", name)
		}
		gotOK, gotWhy := d.PreflightROCm(config.VillaConfig{Backend: name})

		b, err := inference.BackendFor(name)
		if err != nil {
			t.Fatalf("BackendFor(%q): %v", name, err)
		}
		wantOK, wantWhy := true, ""
		for _, c := range preflight.RunROCmForImage(detect.Probe(), b.Image()) {
			if c.Status == preflight.StatusFail {
				wantOK, wantWhy = false, c.Detail
				break
			}
		}
		if gotOK != wantOK || gotWhy != wantWhy {
			t.Errorf("%s: live closure verdict (%v,%q) != direct resolved-image gate (%v,%q) — family routing/reduction broken",
				name, gotOK, gotWhy, wantOK, wantWhy)
		}
	}
}

// TestBackendRegistered: the `backend` noun and its show/set subcommands are wired
// into the command tree.
func TestBackendRegistered(t *testing.T) {
	root := newRoot()
	backend, _, err := root.Find([]string{"backend"})
	if err != nil || backend.Name() != "backend" {
		t.Fatalf("`backend` noun not registered: %v", err)
	}
	for _, sub := range []string{"show", "set"} {
		c, _, err := root.Find([]string{"backend", sub})
		if err != nil || c.Name() != sub {
			t.Fatalf("`backend %s` subcommand not registered: %v", sub, err)
		}
	}
}

// TestBackendShow: `backend show` reports the active backend (cfg.Backend) plus its
// resolved image tag, and --json emits the {backend,image} shape. The empty-config
// default resolves to vulkan with its pinned image.
func TestBackendShow(t *testing.T) {
	// Point config at an empty temp dir so LoadVilla returns the defaults (backend
	// "vulkan"); no real XDG config is touched.
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	t.Run("human table shows backend + image", func(t *testing.T) {
		cmd, out, _ := newTestCmd()
		code := runBackendShow(cmd, false)
		if code != exitPass {
			t.Fatalf("expected exit 0, got %d", code)
		}
		s := out.String()
		if !strings.Contains(s, "vulkan") {
			t.Errorf("show output missing the active backend, got %q", s)
		}
		if !strings.Contains(s, "image") || !strings.Contains(s, "@sha256:") {
			t.Errorf("show output missing the resolved image tag, got %q", s)
		}
	})

	t.Run("--json emits {backend,image}", func(t *testing.T) {
		cmd, out, _ := newTestCmd()
		code := runBackendShow(cmd, true)
		if code != exitPass {
			t.Fatalf("expected exit 0, got %d", code)
		}
		s := out.String()
		if !strings.Contains(s, `"backend"`) || !strings.Contains(s, `"image"`) {
			t.Errorf("json output missing backend/image keys, got %q", s)
		}
		if !strings.Contains(s, "vulkan") {
			t.Errorf("json output missing the active backend value, got %q", s)
		}
	})
}

// TestBackendSetDryRun: --dry-run previews target/fit/preflight and mutates NOTHING —
// no SaveConfig / ReconcileAndWrite / Restart / CaptureUnit / Prove (BSET-03).
func TestBackendSetDryRun(t *testing.T) {
	rec := &backendRecorder{curBackend: "vulkan", fits: true, preflightOK: true, proveStatus: backendswap.ProveStatusPass}
	d := newBackendStub(rec)
	cmd, out, _ := newTestCmd()

	code := runBackendSet(cmd, "rocm", true, d)
	if code != exitPass {
		t.Fatalf("dry-run should exit 0, got %d", code)
	}
	// Side-effect-free: zero mutate/capture/prove seams.
	if len(rec.saved) != 0 || rec.written != 0 || len(rec.restarted) != 0 || rec.captured != 0 || len(rec.proved) != 0 {
		t.Errorf("dry-run must fire ZERO mutate seams, got saved=%v written=%d restarted=%v captured=%d proved=%v",
			rec.saved, rec.written, rec.restarted, rec.captured, rec.proved)
	}
	s := out.String()
	if !strings.Contains(s, "dry-run") || !strings.Contains(s, "rocm") {
		t.Errorf("dry-run output should preview the target, got %q", s)
	}
	if !strings.Contains(s, "fit") || !strings.Contains(s, "preflight") {
		t.Errorf("dry-run output should preview fit + preflight verdicts, got %q", s)
	}
}

// TestBackendSetExitMapping exercises the cobra caller's mapping of backendswap.Result
// to exit codes + messages (the transactional ORDERING + rollback are asserted in
// internal/backendswap): refused→1, switched→0, rolled-back→1, no-op→0.
func TestBackendSetExitMapping(t *testing.T) {
	t.Run("refused (non-fit) → exit 1, zero mutate seams", func(t *testing.T) {
		rec := &backendRecorder{curBackend: "vulkan", fits: false, fitReason: "needs 999 bytes vs 100 usable", preflightOK: true}
		d := newBackendStub(rec)
		cmd, _, errOut := newTestCmd()
		code := runBackendSet(cmd, "rocm", false, d)
		if code != exitBlocked {
			t.Fatalf("a refused switch must exit 1, got %d", code)
		}
		if len(rec.saved) != 0 || len(rec.restarted) != 0 {
			t.Errorf("a refusal must fire zero mutate seams, got saved=%v restarted=%v", rec.saved, rec.restarted)
		}
		if !strings.Contains(errOut.String(), "refusing") {
			t.Errorf("expected a refusal message, got %q", errOut.String())
		}
	})

	t.Run("switched (prove pass) → exit 0", func(t *testing.T) {
		rec := &backendRecorder{curBackend: "vulkan", fits: true, preflightOK: true, proveStatus: backendswap.ProveStatusPass}
		d := newBackendStub(rec)
		cmd, out, _ := newTestCmd()
		code := runBackendSet(cmd, "rocm", false, d)
		if code != exitPass {
			t.Fatalf("a proven switch must exit 0, got %d", code)
		}
		if len(rec.saved) == 0 || len(rec.restarted) == 0 || len(rec.proved) == 0 {
			t.Errorf("a switch must save+restart+prove, got saved=%v restarted=%v proved=%v",
				rec.saved, rec.restarted, rec.proved)
		}
		if !strings.Contains(out.String(), "switched") {
			t.Errorf("expected a 'switched' success message, got %q", out.String())
		}
	})

	t.Run("rolled back (prove fail) → exit 1 + prior restored", func(t *testing.T) {
		rec := &backendRecorder{
			curBackend:  "vulkan",
			fits:        true,
			preflightOK: true,
			proveStatus: "fail",
			proveDetail: "gpu_busy_percent 0% during a claimed-healthy decode — silent CPU fallback",
		}
		d := newBackendStub(rec)
		cmd, _, errOut := newTestCmd()
		code := runBackendSet(cmd, "rocm", false, d)
		if code != exitBlocked {
			t.Fatalf("a prove-fail rollback must exit 1, got %d", code)
		}
		if rec.restored == 0 {
			t.Errorf("a rollback must restore the prior unit, got restored=%d", rec.restored)
		}
		s := errOut.String()
		if !strings.Contains(s, "rolled back") || !strings.Contains(s, "restored") {
			t.Errorf("expected a 'rolled back; prior backend restored' message, got %q", s)
		}
	})

	t.Run("no-op (same backend) → exit 0", func(t *testing.T) {
		rec := &backendRecorder{curBackend: "vulkan", fits: true, preflightOK: true}
		d := newBackendStub(rec)
		cmd, out, _ := newTestCmd()
		code := runBackendSet(cmd, "vulkan", false, d)
		if code != exitPass {
			t.Fatalf("a same-backend no-op must exit 0, got %d", code)
		}
		if len(rec.saved) != 0 || len(rec.restarted) != 0 {
			t.Errorf("a no-op must fire zero mutate seams, got saved=%v restarted=%v", rec.saved, rec.restarted)
		}
		if !strings.Contains(out.String(), "already on vulkan") {
			t.Errorf("expected an 'already on vulkan' message, got %q", out.String())
		}
	})

	t.Run("rolled back (write error) → exit 1", func(t *testing.T) {
		rec := &backendRecorder{
			curBackend:  "vulkan",
			fits:        true,
			preflightOK: true,
			writeErr:    errors.New("render failed"),
		}
		d := newBackendStub(rec)
		cmd, _, errOut := newTestCmd()
		code := runBackendSet(cmd, "rocm", false, d)
		if code != exitBlocked {
			t.Fatalf("a write-error rollback must exit 1, got %d", code)
		}
		if rec.restored == 0 {
			t.Errorf("a mutate-error rollback must restore the prior unit, got restored=%d", rec.restored)
		}
		if !strings.Contains(errOut.String(), "rolled back") {
			t.Errorf("expected a 'rolled back' message, got %q", errOut.String())
		}
	})
}
