package stackapply

import (
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// host is a fake host: it records every seam call in order, the render input it was
// handed, and what was persisted and written.
type host struct {
	calls   []string
	input   orchestrate.RenderInput
	saved   []config.VillaConfig
	envText string
	written []orchestrate.Unit

	changed   bool  // whether Reconcile reports the rendered units as changed
	envErr    error // injected WriteInferenceSecretEnv failure
	reloadErr error // injected DaemonReload failure
}

var testCatalog = catalog.Catalog{Models: []catalog.Model{
	{ID: "chat", Shards: []catalog.Shard{{Filename: "chat.gguf"}}},
	{ID: "slot", Shards: []catalog.Shard{{Filename: "slot.gguf"}}},
	{ID: "coder", Shards: []catalog.Shard{{Filename: "coder.gguf"}}, CacheReuseSafe: true,
		AgentSampling: &catalog.AgentSampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05}},
}}

func (h *host) deps() Deps {
	return Deps{
		Catalog:       func() (catalog.Catalog, error) { return testCatalog, nil },
		ModelsDir:     func() string { return "/models-dir" },
		HostVillaPath: func() string { return "/bin/villa" },
		Render: func(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
			h.calls = append(h.calls, "render")
			h.input = in
			return []orchestrate.Unit{{Name: "villa-llama.container", Text: "llama"}, {Name: "villa.network", Text: "net"}}, nil
		},
		UnitDir: func() (string, error) { return "/units", nil },
		Reconcile: func(units []orchestrate.Unit, _ string) (orchestrate.Plan, error) {
			h.calls = append(h.calls, "reconcile")
			if h.changed {
				return orchestrate.Plan{Changed: units}, nil
			}
			return orchestrate.Plan{Unchanged: units}, nil
		},
		WriteUnits: func(p orchestrate.Plan, _ string) error {
			h.calls = append(h.calls, "write")
			h.written = append(h.written, p.Changed...)
			return nil
		},
		DaemonReload: func() error {
			h.calls = append(h.calls, "reload")
			return h.reloadErr
		},
		SaveConfig: func(c config.VillaConfig) error {
			h.calls = append(h.calls, "save")
			h.saved = append(h.saved, c)
			return nil
		},
		WriteInferenceSecretEnv: func(_, text string) error {
			h.calls = append(h.calls, "env")
			h.envText = text
			return h.envErr
		},
	}
}

// TestRenderDerivesTheServedModelFromTheConfig is #249's invariant at its root: the
// served model file, the coding descriptor and the agent ctx come from the config
// alone, so every verb that renders gets coding mode without remembering it.
func TestRenderDerivesTheServedModelFromTheConfig(t *testing.T) {
	coderSpec := &inference.CodingModeSpec{CacheReuseSafe: true,
		Sampling: &inference.Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05}}
	cases := []struct {
		name      string
		cfg       config.VillaConfig
		wantFile  string
		wantSpec  *inference.CodingModeSpec
		wantAgent int
	}{
		{"chat mode serves the chat entry at the chat ctx",
			config.VillaConfig{Model: "chat", Ctx: 8192, Backend: "vulkan", CoderModel: "coder", CoderAgentCtx: 65536},
			"chat.gguf", nil, 0},
		{"swap residency serves the coder with its descriptor at the agent ctx",
			config.VillaConfig{Model: "chat", Ctx: 8192, Backend: "vulkan", CodingMode: true, CoderModel: "coder", CoderAgentCtx: 65536},
			"coder.gguf", coderSpec, 65536},
		{"shared residency serves the chat entry with ITS descriptor at the agent ctx",
			config.VillaConfig{Model: "chat", Ctx: 8192, Backend: "vulkan", CodingMode: true, CoderAgentCtx: 32768},
			"chat.gguf", &inference.CodingModeSpec{}, 32768},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			h := &host{}
			if _, err := Render(h.deps(), tc.cfg); err != nil {
				t.Fatalf("Render: %v", err)
			}
			in := h.input
			if in.ModelFile != tc.wantFile {
				t.Errorf("ModelFile = %q, want %q", in.ModelFile, tc.wantFile)
			}
			if !reflect.DeepEqual(in.CodingMode, tc.wantSpec) {
				t.Errorf("CodingMode = %+v, want %+v", in.CodingMode, tc.wantSpec)
			}
			if in.AgentCtx != tc.wantAgent {
				t.Errorf("AgentCtx = %d, want %d", in.AgentCtx, tc.wantAgent)
			}
			if in.ModelsDir != "/models-dir" || in.HostVillaPath != "/bin/villa" || in.Backend.Name() != "vulkan" {
				t.Errorf("ModelsDir/HostVillaPath/Backend = %q/%q/%q, want the Deps values and the config's backend",
					in.ModelsDir, in.HostVillaPath, in.Backend.Name())
			}
			if !reflect.DeepEqual(in.Cfg, tc.cfg) {
				t.Errorf("Cfg was altered by a read-only render")
			}
		})
	}
}

// TestRenderResolvesTheResidentSet: the resident slots reach the render input, so a
// re-render never drops them from the chat UI's endpoint list.
func TestRenderResolvesTheResidentSet(t *testing.T) {
	h := &host{}
	cfg := config.VillaConfig{Model: "chat", Backend: "vulkan", Resident: []config.ResidentModel{{Model: "slot", Ctx: 4096, Port: 8081}}}
	if _, err := Render(h.deps(), cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := []orchestrate.ResidentUnit{{Model: "slot", ModelFile: "slot.gguf", Ctx: 4096, Port: 8081}}
	if !reflect.DeepEqual(h.input.Resident, want) {
		t.Errorf("Resident = %+v, want %+v", h.input.Resident, want)
	}
}

// TestRenderRefusesWhatItCannotResolve: an unknown backend, served model or resident
// slot is an error before the render, never a unit whose -m names a fabricated file.
func TestRenderRefusesWhatItCannotResolve(t *testing.T) {
	cases := map[string]struct {
		cfg  config.VillaConfig
		want string
	}{
		"unknown backend":  {config.VillaConfig{Model: "chat", Backend: "cuda"}, "resolve backend"},
		"unknown model":    {config.VillaConfig{Model: "ghost", Backend: "vulkan"}, "resolve model file"},
		"unknown coder":    {config.VillaConfig{Model: "chat", Backend: "vulkan", CodingMode: true, CoderModel: "ghost"}, "resolve model file"},
		"unknown resident": {config.VillaConfig{Model: "chat", Backend: "vulkan", Resident: []config.ResidentModel{{Model: "ghost"}}}, "resident model"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			h := &host{}
			_, err := Render(h.deps(), tc.cfg)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("Render error = %v, want one naming %q", err, tc.want)
			}
			if len(h.calls) != 0 {
				t.Errorf("seams fired before the refusal: %v", h.calls)
			}
		})
	}
}

// TestApplyHealsTheSecretBeforeRendering is the one secret-heal route, ordered
// before the render: a resident set bakes the secret into the chat UI unit, so the
// render must already see the persisted secret, and the env file the units point at
// must exist before any unit is written.
func TestApplyHealsTheSecretBeforeRendering(t *testing.T) {
	h := &host{changed: true}
	cfg := config.VillaConfig{Model: "chat", Backend: "vulkan"}
	changed, err := Apply(h.deps(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	wantCalls := []string{"save", "env", "render", "reconcile", "write", "reload"}
	if !reflect.DeepEqual(h.calls, wantCalls) {
		t.Errorf("seam order = %v, want %v", h.calls, wantCalls)
	}
	if len(h.saved) != 1 || h.saved[0].InferenceSecret == "" {
		t.Fatalf("saved = %+v, want one save carrying a generated secret", h.saved)
	}
	secret := h.saved[0].InferenceSecret
	if h.input.Cfg.InferenceSecret != secret {
		t.Errorf("render saw secret %q, want the persisted %q", h.input.Cfg.InferenceSecret, secret)
	}
	if !strings.Contains(h.envText, secret) {
		t.Errorf("env file text %q does not carry the persisted secret", h.envText)
	}
	if len(changed) != 2 {
		t.Errorf("Apply returned %d changed units, want the 2 written", len(changed))
	}
}

// TestApplyReusesTheSecretAndWritesNothingUnchanged: an existing secret is never
// rotated or re-saved, its env file is still rewritten (self-healing a deleted one),
// and an unchanged stack is a true no-op for units and systemd.
func TestApplyReusesTheSecretAndWritesNothingUnchanged(t *testing.T) {
	h := &host{}
	cfg := config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"}
	changed, err := Apply(h.deps(), cfg)
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	if want := []string{"env", "render", "reconcile"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("seam order = %v, want %v (no save, no write, no reload)", h.calls, want)
	}
	if !strings.Contains(h.envText, "kept") || changed != nil {
		t.Errorf("env text %q / changed %v, want the kept secret and no changed units", h.envText, changed)
	}
}

// TestApplyRefusesBeforeRenderingWhenTheHealFails: no unit is written that points at
// an env file villa could not write.
func TestApplyRefusesBeforeRenderingWhenTheHealFails(t *testing.T) {
	h := &host{changed: true, envErr: errors.New("disk full")}
	_, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
	if err == nil || !strings.Contains(err.Error(), "inference secret") {
		t.Fatalf("Apply error = %v, want an inference-secret refusal", err)
	}
	if want := []string{"env"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("seam order = %v, want %v", h.calls, want)
	}
}

// TestApplyReportsWrittenUnitsWhenTheReloadFails: the caller's rollback must know
// what is already on disk.
func TestApplyReportsWrittenUnitsWhenTheReloadFails(t *testing.T) {
	h := &host{changed: true, reloadErr: errors.New("no manager")}
	changed, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
	if err == nil || !strings.Contains(err.Error(), "daemon-reload") {
		t.Fatalf("Apply error = %v, want a daemon-reload failure", err)
	}
	if len(changed) != 2 {
		t.Errorf("Apply returned %d units, want the 2 it wrote before the reload failed", len(changed))
	}
}

// TestPlanWritesNothing: the --dry-run preview and a transaction's capture render and
// reconcile only — no secret, no env file, no unit, no reload.
func TestPlanWritesNothing(t *testing.T) {
	h := &host{changed: true}
	plan, err := Plan(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan"})
	if err != nil {
		t.Fatalf("Plan: %v", err)
	}
	if want := []string{"render", "reconcile"}; !reflect.DeepEqual(h.calls, want) {
		t.Errorf("seam order = %v, want %v", h.calls, want)
	}
	if len(plan.Changed) != 2 {
		t.Errorf("Plan.Changed = %d units, want 2", len(plan.Changed))
	}
}

// TestRestoreWritesCapturedBytesVerbatim: a restore is the captured text, not a
// re-render; nothing captured is nothing written.
func TestRestoreWritesCapturedBytesVerbatim(t *testing.T) {
	h := &host{}
	if err := Restore(h.deps(), nil); err != nil || len(h.calls) != 0 {
		t.Fatalf("Restore(nil) = %v with calls %v, want a silent no-op", err, h.calls)
	}
	if err := Restore(h.deps(), map[string]string{"villa-llama.container": "prior bytes"}); err != nil {
		t.Fatalf("Restore: %v", err)
	}
	want := []orchestrate.Unit{{Name: "villa-llama.container", Text: "prior bytes"}}
	if !reflect.DeepEqual(h.written, want) || !reflect.DeepEqual(h.calls, []string{"write"}) {
		t.Errorf("written %+v with calls %v, want %+v and one write", h.written, h.calls, want)
	}
}
