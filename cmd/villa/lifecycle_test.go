package main

import (
	"bytes"
	"errors"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// fakeLifecycleDeps wires a lifecycleDeps entirely to stubs so runUp/runDown/
// runRestart (and runLogs) are exercisable without a live podman/systemd/journald
// host. Counters let each test assert exactly which seam fired (idempotency,
// targeting, no-op).
type fakeLifecycleDeps struct {
	*lifecycleDeps
	writeCalls   int
	reloadCalls  int
	startCalls   []string
	stopCalls    []string
	restartCalls []string
	journalCalls []string
}

// twoUnitStack returns the rendered stack as the live Render would: the inference
// .container (→ villa-llama.service) plus the .network and .volume scaffolds (no
// service). The known service set derives from these names.
func twoUnitStack() []orchestrate.Unit {
	return []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\nImage=x\n"},
		{Name: "villa.network", Text: "[Network]\n"},
		{Name: "villa-models.volume", Text: "[Volume]\n"},
	}
}

func newFakeLifecycleDeps(t *testing.T, units []orchestrate.Unit, plan orchestrate.Plan) *fakeLifecycleDeps {
	t.Helper()
	f := &fakeLifecycleDeps{}
	d := &lifecycleDeps{
		loadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen2.5-0.5b", Quant: "Q4", Ctx: 4096, Backend: "vulkan"}, nil
		},
		modelFile: func(config.VillaConfig) (string, error) { return "qwen2.5-0.5b.gguf", nil },
		modelsDir: func() string { return t.TempDir() },
		render:    func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil },
		reconcile: func([]orchestrate.Unit, string) (orchestrate.Plan, error) { return plan, nil },
		unitDir:   func() (string, error) { return t.TempDir(), nil },
	}
	d.writeUnits = func(orchestrate.Plan, string) error { f.writeCalls++; return nil }
	d.daemonReload = func() error { f.reloadCalls++; return nil }
	d.start = func(svc string) error { f.startCalls = append(f.startCalls, svc); return nil }
	d.stop = func(svc string) error { f.stopCalls = append(f.stopCalls, svc); return nil }
	d.restart = func(svc string) error { f.restartCalls = append(f.restartCalls, svc); return nil }
	d.journalText = func(svc string) (string, bool) {
		f.journalCalls = append(f.journalCalls, svc)
		return "load_tensors: Vulkan0 model buffer size = 512 MiB\n", true
	}
	d.followJournal = func(svc string) error { f.journalCalls = append(f.journalCalls, svc); return nil }
	f.lifecycleDeps = d
	return f
}

func lifecycleTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

// TestLifecycleUpReconcilesAndStartsWholeStack: `up` with no service arg and a
// changed plan writes the units, daemon-reloads, and starts the inference service.
func TestLifecycleUpReconcilesAndStartsWholeStack(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}
	f := newFakeLifecycleDeps(t, units, plan)

	cmd, out, _ := lifecycleTestCmd()
	code := runUp(cmd, upOpts{}, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("up exit = %d, want 0", code)
	}
	if f.writeCalls != 1 || f.reloadCalls != 1 {
		t.Errorf("up should write+reload once: write=%d reload=%d", f.writeCalls, f.reloadCalls)
	}
	if !contains(f.startCalls, "villa-llama.service") {
		t.Errorf("up should start villa-llama.service, started %v", f.startCalls)
	}
	if !bytes.Contains(out.Bytes(), []byte("villa-llama.service")) {
		t.Errorf("up should report what it started, got %q", out.String())
	}
}

// TestLifecycleUpModelFileErrorBlocks: when the model file cannot be resolved (bad
// catalog / unknown id), `up` blocks BEFORE writing or starting anything rather than
// rendering a container whose -m points at a fabricated, non-existent GGUF (WR-08).
func TestLifecycleUpModelFileErrorBlocks(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}
	f := newFakeLifecycleDeps(t, units, plan)
	f.lifecycleDeps.modelFile = func(config.VillaConfig) (string, error) {
		return "", errors.New("model \"ghost\" is not in the catalog")
	}

	cmd, _, errOut := lifecycleTestCmd()
	code := runUp(cmd, upOpts{}, nil, f.lifecycleDeps)
	if code != exitBlocked {
		t.Fatalf("up with unresolvable model file exit = %d, want %d (block)", code, exitBlocked)
	}
	if f.writeCalls != 0 || f.reloadCalls != 0 || len(f.startCalls) != 0 {
		t.Errorf("model-file failure must fire zero write/reload/start seams: write=%d reload=%d start=%v", f.writeCalls, f.reloadCalls, f.startCalls)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("model file")) {
		t.Errorf("up should surface the model-file resolution failure, got %q", errOut.String())
	}
}

// TestLifecycleUpUnchangedIsNoOp: `up` with an empty Changed plan writes nothing,
// reloads nothing, starts nothing, and exits 0 (true no-op, CLI-01/D-06).
func TestLifecycleUpUnchangedIsNoOp(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Unchanged: units}
	f := newFakeLifecycleDeps(t, units, plan)

	cmd, out, _ := lifecycleTestCmd()
	code := runUp(cmd, upOpts{}, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("up no-op exit = %d, want 0", code)
	}
	if f.writeCalls != 0 || f.reloadCalls != 0 || len(f.startCalls) != 0 {
		t.Errorf("up no-op must not write/reload/start: write=%d reload=%d start=%v", f.writeCalls, f.reloadCalls, f.startCalls)
	}
	if !bytes.Contains(bytes.ToLower(out.Bytes()), []byte("no changes")) {
		t.Errorf("up no-op should report no changes, got %q", out.String())
	}
}

// TestLifecycleUpDryRunWritesNothing: `up --dry-run` prints the changed units and
// writes/starts nothing.
func TestLifecycleUpDryRunWritesNothing(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}
	f := newFakeLifecycleDeps(t, units, plan)

	cmd, out, _ := lifecycleTestCmd()
	code := runUp(cmd, upOpts{dryRun: true}, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("up --dry-run exit = %d, want 0", code)
	}
	if f.writeCalls != 0 || f.reloadCalls != 0 || len(f.startCalls) != 0 {
		t.Errorf("up --dry-run must not write/reload/start: write=%d reload=%d start=%v", f.writeCalls, f.reloadCalls, f.startCalls)
	}
	if !bytes.Contains(out.Bytes(), []byte("[Container]")) {
		t.Errorf("up --dry-run must print rendered unit text, got %q", out.String())
	}
}

// TestLifecycleUpTargetsNamedService: `up villa-llama` reconciles the whole stack
// but starts only the named service.
func TestLifecycleUpTargetsNamedService(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}
	f := newFakeLifecycleDeps(t, units, plan)

	cmd, _, _ := lifecycleTestCmd()
	code := runUp(cmd, upOpts{}, []string{"villa-llama"}, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("up villa-llama exit = %d, want 0", code)
	}
	if len(f.startCalls) != 1 || !contains(f.startCalls, "villa-llama.service") {
		t.Errorf("up villa-llama should start exactly villa-llama.service, started %v", f.startCalls)
	}
}

// TestLifecycleUnknownServiceBlocks: an unknown service name on up/down/restart
// exits 1 and fires ZERO seam calls.
func TestLifecycleUnknownServiceBlocks(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}

	t.Run("up", func(t *testing.T) {
		f := newFakeLifecycleDeps(t, units, plan)
		cmd, _, errOut := lifecycleTestCmd()
		code := runUp(cmd, upOpts{}, []string{"nope"}, f.lifecycleDeps)
		if code != exitBlocked {
			t.Fatalf("up nope exit = %d, want 1", code)
		}
		if f.writeCalls != 0 || len(f.startCalls) != 0 {
			t.Errorf("unknown-service up must fire zero seams: write=%d start=%v", f.writeCalls, f.startCalls)
		}
		if !bytes.Contains(errOut.Bytes(), []byte("nope")) {
			t.Errorf("unknown-service up should name the bad service, got %q", errOut.String())
		}
	})
	t.Run("down", func(t *testing.T) {
		f := newFakeLifecycleDeps(t, units, plan)
		cmd, _, _ := lifecycleTestCmd()
		code := runDown(cmd, []string{"nope"}, f.lifecycleDeps)
		if code != exitBlocked {
			t.Fatalf("down nope exit = %d, want 1", code)
		}
		if len(f.stopCalls) != 0 {
			t.Errorf("unknown-service down must fire zero seams: stop=%v", f.stopCalls)
		}
	})
	t.Run("restart", func(t *testing.T) {
		f := newFakeLifecycleDeps(t, units, plan)
		cmd, _, _ := lifecycleTestCmd()
		code := runRestart(cmd, []string{"nope"}, f.lifecycleDeps)
		if code != exitBlocked {
			t.Fatalf("restart nope exit = %d, want 1", code)
		}
		if len(f.restartCalls) != 0 || f.writeCalls != 0 {
			t.Errorf("unknown-service restart must fire zero seams: restart=%v write=%d", f.restartCalls, f.writeCalls)
		}
	})
}

// TestLifecycleDownStopsWithoutRemovingUnits: `down` stops the service(s) and never
// writes or removes a unit (removal is uninstall).
func TestLifecycleDownStopsWithoutRemovingUnits(t *testing.T) {
	units := twoUnitStack()
	f := newFakeLifecycleDeps(t, units, orchestrate.Plan{})

	cmd, _, _ := lifecycleTestCmd()
	code := runDown(cmd, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("down exit = %d, want 0", code)
	}
	if !contains(f.stopCalls, "villa-llama.service") {
		t.Errorf("down should stop villa-llama.service, stopped %v", f.stopCalls)
	}
	if f.writeCalls != 0 {
		t.Errorf("down must never write/remove units, write=%d", f.writeCalls)
	}
}

// TestLifecycleRestartReconcilesThenRestarts: `restart` reconciles first (so a
// config edit is applied) then restarts; a changed plan is written before restart.
func TestLifecycleRestartReconcilesThenRestarts(t *testing.T) {
	units := twoUnitStack()
	plan := orchestrate.Plan{Changed: units}
	f := newFakeLifecycleDeps(t, units, plan)

	cmd, _, _ := lifecycleTestCmd()
	code := runRestart(cmd, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("restart exit = %d, want 0", code)
	}
	if f.writeCalls != 1 || f.reloadCalls != 1 {
		t.Errorf("restart should reconcile (write+reload) first: write=%d reload=%d", f.writeCalls, f.reloadCalls)
	}
	if !contains(f.restartCalls, "villa-llama.service") {
		t.Errorf("restart should restart villa-llama.service, restarted %v", f.restartCalls)
	}
}

// TestLifecycleVerbsRegistered: up/down/restart/logs are wired into the root tree.
func TestLifecycleVerbsRegistered(t *testing.T) {
	root := newRoot()
	for _, name := range []string{"up", "down", "restart", "logs"} {
		c, _, err := root.Find([]string{name})
		if err != nil || c.Name() != name {
			t.Errorf("`%s` verb not registered: %v", name, err)
		}
	}
}

// TestLogsPrintsJournalForNamedService: `logs villa-llama` prints the stubbed
// journal text for that service (non-follow path).
func TestLogsPrintsJournalForNamedService(t *testing.T) {
	units := twoUnitStack()
	f := newFakeLifecycleDeps(t, units, orchestrate.Plan{})

	cmd, out, _ := lifecycleTestCmd()
	code := runLogs(cmd, logsOpts{}, []string{"villa-llama"}, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("logs exit = %d, want 0", code)
	}
	if !contains(f.journalCalls, "villa-llama.service") {
		t.Errorf("logs should read villa-llama.service journal, read %v", f.journalCalls)
	}
	if !bytes.Contains(out.Bytes(), []byte("Vulkan0")) {
		t.Errorf("logs should print the journal text, got %q", out.String())
	}
}

// TestLogsDefaultsToInferenceService: `logs` with no arg defaults to the single
// inference service.
func TestLogsDefaultsToInferenceService(t *testing.T) {
	units := twoUnitStack()
	f := newFakeLifecycleDeps(t, units, orchestrate.Plan{})

	cmd, _, _ := lifecycleTestCmd()
	code := runLogs(cmd, logsOpts{}, nil, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("logs (no arg) exit = %d, want 0", code)
	}
	if !contains(f.journalCalls, "villa-llama.service") {
		t.Errorf("logs with no arg should default to villa-llama.service, read %v", f.journalCalls)
	}
}

// TestLogsFollowUsesFollowSeam: `logs villa-llama -f` calls the follow seam.
func TestLogsFollowUsesFollowSeam(t *testing.T) {
	units := twoUnitStack()
	f := newFakeLifecycleDeps(t, units, orchestrate.Plan{})

	cmd, _, _ := lifecycleTestCmd()
	code := runLogs(cmd, logsOpts{follow: true}, []string{"villa-llama"}, f.lifecycleDeps)
	if code != exitPass {
		t.Fatalf("logs -f exit = %d, want 0", code)
	}
	if !contains(f.journalCalls, "villa-llama.service") {
		t.Errorf("logs -f should follow villa-llama.service, calls %v", f.journalCalls)
	}
}

// TestLogsUnknownServiceBlocks: an unknown service exits 1 and reads no journal.
func TestLogsUnknownServiceBlocks(t *testing.T) {
	units := twoUnitStack()
	f := newFakeLifecycleDeps(t, units, orchestrate.Plan{})

	cmd, _, errOut := lifecycleTestCmd()
	code := runLogs(cmd, logsOpts{}, []string{"nope"}, f.lifecycleDeps)
	if code != exitBlocked {
		t.Fatalf("logs nope exit = %d, want 1", code)
	}
	if len(f.journalCalls) != 0 {
		t.Errorf("unknown-service logs must read no journal, read %v", f.journalCalls)
	}
	if !bytes.Contains(errOut.Bytes(), []byte("nope")) {
		t.Errorf("unknown-service logs should name the bad service, got %q", errOut.String())
	}
}
