package main

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestCaptureSurvivesAConfigTheRenderRefuses (#299): `vision = true` on an entry
// without a projector is refused by the render, and `villa model swap` is the way
// out of it. The swap's capture renders the PRIOR config to know which units to
// snapshot, so it must not refuse that same config, or the remediation the
// refusal names could never run.
func TestCaptureSurvivesAConfigTheRenderRefuses(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir, err := quadletUnitDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"villa-llama.container": "PRIOR llama\n",
		"villa.network":         "PRIOR network\n",
	}
	for name, text := range want {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(text), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(filepath.Join(dir, "other.container"), []byte("not villa's\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg := config.VillaConfig{Model: "qwen3.5-0.8b", Quant: "Q4_K_M", Ctx: 4096, Vision: true, InferenceSecret: "s"}
	got, err := captureUnits(liveStackDeps())(cfg)
	if err != nil {
		t.Fatalf("capture refused the config the swap exists to leave: %v", err)
	}
	for name, text := range want {
		if got[name] != text {
			t.Errorf("captured %s = %q, want %q", name, got[name], text)
		}
	}
	if _, ok := got["other.container"]; ok {
		t.Error("capture must take villa's units only")
	}
}

// TestCaptureTakesTheRegistryUnitsThePriorConfigDoesNotRender (#330): a swap's apply
// removes a registry unit the target no longer renders, even one the prior config
// did not render either, so the capture holds its bytes for the rollback.
func TestCaptureTakesTheRegistryUnitsThePriorConfigDoesNotRender(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	dir, err := quadletUnitDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "villa-searxng.container"), []byte("STALE searxng\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	cfg := config.VillaConfig{Model: "qwen3.5-0.8b", Quant: "Q4_K_M", Ctx: 4096, InferenceSecret: "s"}
	got, err := captureUnits(liveStackDeps())(cfg)
	if err != nil {
		t.Fatalf("capture: %v", err)
	}
	if got["villa-searxng.container"] != "STALE searxng\n" {
		t.Errorf("captured %q for the stale searxng unit, want its on-disk bytes", got["villa-searxng.container"])
	}
}
