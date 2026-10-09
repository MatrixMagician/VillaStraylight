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

	changed   bool               // whether Reconcile reports the rendered units as changed
	removed   []orchestrate.Unit // what Reconcile reports as Removed
	active    map[string]bool    // services IsActive reports running
	envErr    error              // injected WriteInferenceSecretEnv failure
	reloadErr error              // injected DaemonReload failure
	removeErr error              // injected RemoveUnits failure
	stopErr   error              // injected Stop failure
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
				return orchestrate.Plan{Changed: units, Removed: h.removed}, nil
			}
			return orchestrate.Plan{Unchanged: units, Removed: h.removed}, nil
		},
		RemoveUnits: func(p orchestrate.Plan, _ string) error {
			h.calls = append(h.calls, "remove")
			return h.removeErr
		},
		Stop: func(svc string) error {
			h.calls = append(h.calls, "stop:"+svc)
			return h.stopErr
		},
		IsActive: func(svc string) (string, error) {
			if h.active[svc] {
				return "active", nil
			}
			return "inactive", nil
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

// TestRenderResolvesTheImageModel: with image generation on, the render input
// carries the image table's entry translated into the renderer's ImageServe, so
// every verb that renders carries villa-image without a per-site field; with it
// off the field stays nil and the render is what it always was.
func TestRenderResolvesTheImageModel(t *testing.T) {
	h := &host{}
	cfg := config.VillaConfig{Model: "chat", Backend: "vulkan", ImageEnabled: true, ImageModel: "z-image-turbo-q4"}
	if _, err := Render(h.deps(), cfg); err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := &orchestrate.ImageServe{
		DiffusionFile: "z_image_turbo-Q4_K.gguf", TextEncoderFile: "Qwen3-4B-Instruct-2507-Q4_K_M.gguf", VAEFile: "z-image-turbo-ae.safetensors",
		Steps: 8, CfgScale: 1, Width: 1024, Height: 1024,
	}
	if !reflect.DeepEqual(h.input.Image, want) {
		t.Errorf("Image = %+v, want %+v", h.input.Image, want)
	}

	off := &host{}
	if _, err := Render(off.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", ImageModel: "z-image-turbo"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if off.input.Image != nil {
		t.Errorf("image off: Image = %+v, want nil", off.input.Image)
	}
}

// TestRenderRefusesWhatItCannotResolve: an unknown backend, served model, resident
// slot or image model is an error before the render, never a unit whose -m names a
// fabricated file.
func TestRenderRefusesWhatItCannotResolve(t *testing.T) {
	cases := map[string]struct {
		cfg  config.VillaConfig
		want string
	}{
		"unknown backend":  {config.VillaConfig{Model: "chat", Backend: "cuda"}, "resolve backend"},
		"unknown model":    {config.VillaConfig{Model: "ghost", Backend: "vulkan"}, "resolve model file"},
		"unknown coder":    {config.VillaConfig{Model: "chat", Backend: "vulkan", CodingMode: true, CoderModel: "ghost"}, "resolve model file"},
		"unknown resident": {config.VillaConfig{Model: "chat", Backend: "vulkan", Resident: []config.ResidentModel{{Model: "ghost"}}}, "resident model"},
		"unknown image":    {config.VillaConfig{Model: "chat", Backend: "vulkan", ImageEnabled: true, ImageModel: "ghost"}, "image model"},
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
	if len(changed.Changed) != 2 {
		t.Errorf("Apply returned %d changed units, want the 2 written", len(changed.Changed))
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
	if !strings.Contains(h.envText, "kept") || !changed.Empty() {
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

// TestApplyHealsCrushJSONAfterTheSecret guards ADR-0019's stack-apply heal: the
// HealAgentConfig seam runs after the inference-secret heal, so it sees the
// persisted secret it must write into crush.json, and before the render; a nil
// seam is skipped; a failed heal stops the apply before any unit is written.
func TestApplyHealsCrushJSONAfterTheSecret(t *testing.T) {
	t.Run("called with the healed secret before the render", func(t *testing.T) {
		h := &host{changed: true}
		d := h.deps()
		var healed []config.VillaConfig
		d.HealAgentConfig = func(c config.VillaConfig) error {
			h.calls = append(h.calls, "agent")
			healed = append(healed, c)
			return nil
		}
		if _, err := Apply(d, config.VillaConfig{Model: "chat", Backend: "vulkan"}); err != nil {
			t.Fatalf("Apply: %v", err)
		}
		want := []string{"save", "env", "agent", "render", "reconcile", "write", "reload"}
		if !reflect.DeepEqual(h.calls, want) {
			t.Errorf("seam order = %v, want %v", h.calls, want)
		}
		if len(healed) != 1 || healed[0].InferenceSecret == "" || healed[0].InferenceSecret != h.saved[0].InferenceSecret {
			t.Errorf("HealAgentConfig did not see the persisted secret (healed %d configs)", len(healed))
		}
	})

	t.Run("nil seam is skipped", func(t *testing.T) {
		h := &host{changed: true}
		d := h.deps()
		d.HealAgentConfig = nil
		if _, err := Apply(d, config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"}); err != nil {
			t.Fatalf("Apply with no agent heal: %v", err)
		}
		if want := []string{"env", "render", "reconcile", "write", "reload"}; !reflect.DeepEqual(h.calls, want) {
			t.Errorf("seam order = %v, want %v", h.calls, want)
		}
	})

	t.Run("a failed heal writes no unit", func(t *testing.T) {
		h := &host{changed: true}
		d := h.deps()
		d.HealAgentConfig = func(config.VillaConfig) error {
			h.calls = append(h.calls, "agent")
			return errors.New("read-only file system")
		}
		_, err := Apply(d, config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
		if err == nil || !strings.Contains(err.Error(), "heal crush.json") || !strings.Contains(err.Error(), "read-only file system") {
			t.Fatalf("Apply error = %v, want the wrapped crush.json heal failure", err)
		}
		if want := []string{"env", "agent"}; !reflect.DeepEqual(h.calls, want) {
			t.Errorf("seam order = %v, want %v (nothing rendered or written)", h.calls, want)
		}
	})
}

// TestApplyReportsWrittenUnitsWhenTheReloadFails: the caller's rollback must know
// what is already on disk.
func TestApplyReportsWrittenUnitsWhenTheReloadFails(t *testing.T) {
	h := &host{changed: true, reloadErr: errors.New("no manager")}
	changed, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
	if err == nil || !strings.Contains(err.Error(), "daemon-reload") {
		t.Fatalf("Apply error = %v, want a daemon-reload failure", err)
	}
	if len(changed.Changed) != 2 {
		t.Errorf("Apply returned %d units, want the 2 it wrote before the reload failed", len(changed.Changed))
	}
}

// orphans is a web-search pair the target config no longer renders.
var orphans = []orchestrate.Unit{
	{Name: "villa-searxng.container", Text: "SEARXNG"},
	{Name: "villa-websafe.container", Text: "WEBSAFE"},
}

// TestApplyStopsRemovesAndReloadsTheUnitsItNoLongerRenders (#330): a registry unit
// on disk that the target config no longer renders has its running service stopped,
// its file removed and systemd reloaded, inside the one apply. Only the services that
// were running are reported as stopped, so a rollback restarts exactly those.
func TestApplyStopsRemovesAndReloadsTheUnitsItNoLongerRenders(t *testing.T) {
	h := &host{removed: orphans, active: map[string]bool{"villa-searxng.service": true}}
	applied, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
	if err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"env", "render", "reconcile", "stop:villa-searxng.service", "remove", "reload"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("seam order = %v, want %v", h.calls, want)
	}
	if !reflect.DeepEqual(applied.Removed, orphans) || !reflect.DeepEqual(applied.Stopped, []string{"villa-searxng.service"}) {
		t.Errorf("applied = %+v, want both orphans removed and villa-searxng.service stopped", applied)
	}
	if applied.Empty() {
		t.Error("an apply that removed units reports Empty")
	}
}

// TestApplyStopsBeforeItWritesAndReloadsOnce: a change and a removal in one apply
// stop first, write, remove, then reload once.
func TestApplyStopsBeforeItWritesAndReloadsOnce(t *testing.T) {
	h := &host{changed: true, removed: orphans, active: map[string]bool{"villa-searxng.service": true, "villa-websafe.service": true}}
	if _, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"}); err != nil {
		t.Fatalf("Apply: %v", err)
	}
	want := []string{"env", "render", "reconcile", "stop:villa-searxng.service", "stop:villa-websafe.service", "write", "remove", "reload"}
	if !reflect.DeepEqual(h.calls, want) {
		t.Errorf("seam order = %v, want %v", h.calls, want)
	}
}

// TestApplyReportsWhatItStoppedWhenItFails: a failed stop or removal still names the
// services it stopped and the units it set out to remove, so the rollback brings them
// back.
func TestApplyReportsWhatItStoppedWhenItFails(t *testing.T) {
	for name, h := range map[string]*host{
		"removal fails": {removed: orphans, active: map[string]bool{"villa-searxng.service": true}, removeErr: errors.New("busy")},
		"stop fails":    {removed: orphans, active: map[string]bool{"villa-searxng.service": true}, stopErr: errors.New("timeout")},
	} {
		t.Run(name, func(t *testing.T) {
			applied, err := Apply(h.deps(), config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
			if err == nil {
				t.Fatal("Apply swallowed the failure")
			}
			if !reflect.DeepEqual(applied.Stopped, []string{"villa-searxng.service"}) {
				t.Errorf("Stopped = %v, want the service it stopped or tried to", applied.Stopped)
			}
		})
	}
}

// TestApplyReportsTheChangedUnitsWhenTheWriteFails: a write that failed partway may
// have written some units, so the caller's rollback must be told every one it tried.
func TestApplyReportsTheChangedUnitsWhenTheWriteFails(t *testing.T) {
	h := &host{changed: true}
	d := h.deps()
	d.WriteUnits = func(orchestrate.Plan, string) error { return errors.New("no space left") }
	applied, err := Apply(d, config.VillaConfig{Model: "chat", Backend: "vulkan", InferenceSecret: "kept"})
	if err == nil || !strings.Contains(err.Error(), "write units") {
		t.Fatalf("Apply error = %v, want a write failure", err)
	}
	if len(applied.Changed) != 2 {
		t.Errorf("Apply reported %d changed units, want the 2 it tried to write", len(applied.Changed))
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
