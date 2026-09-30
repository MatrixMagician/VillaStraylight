package main

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
)

// inference_secret_seam_test.go is the GHSA-qxg9 (ADR-0011) proof that the live
// stack adapter every unit-writing verb applies through (liveStackDeps, ADR-0013)
// migrates an upgraded host's missing inference secret BEFORE it renders: the
// secret is persisted, its 0600 env file is written, and the units written in the
// same apply already carry it. A resident set is configured because that is where
// the ordering shows — the chat UI's plural OPENAI_API_KEYS bakes the secret into
// the unit, so a render before the heal wrote it with an empty key.

func TestLiveStackApplyHealsTheInferenceSecretBeforeRendering(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("HOME", t.TempDir())
	// No systemctl on PATH: the units land in the temp unit dir and the reload is
	// refused as a missing tool instead of reaching the host's user manager.
	t.Setenv("PATH", t.TempDir())

	cfg := config.VillaConfig{
		Model: "qwen3.5-2b", Quant: "Q4_K_M", Ctx: 8192, Backend: "vulkan",
		Resident: []config.ResidentModel{{Model: "qwen3.5-0.8b", Ctx: 4096, Port: 8081}},
	}
	if err := config.SaveVilla(cfg); err != nil {
		t.Fatalf("SaveVilla: %v", err)
	}

	var missing orchestrate.ErrToolNotFound
	if _, err := stackapply.Apply(liveStackDeps(), cfg); err != nil && !errors.As(err, &missing) {
		t.Fatalf("Apply: %v", err)
	}

	migrated, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla after apply: %v", err)
	}
	if migrated.InferenceSecret == "" {
		t.Fatal("apply must generate and persist a non-empty inference_secret")
	}
	envPath, err := orchestrate.InferenceSecretEnvHostPath()
	if err != nil {
		t.Fatalf("InferenceSecretEnvHostPath: %v", err)
	}
	envBytes, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatalf("env file was not written at %q: %v", envPath, err)
	}
	if want := "LLAMA_API_KEY=" + migrated.InferenceSecret; !strings.Contains(string(envBytes), want) {
		t.Errorf("env file body = %q, want it to contain %q", envBytes, want)
	}

	dir, err := quadletUnitDir()
	if err != nil {
		t.Fatalf("quadletUnitDir: %v", err)
	}
	owui, err := os.ReadFile(filepath.Join(dir, orchestrate.OpenWebUIContainerUnitName()))
	if err != nil {
		t.Fatalf("chat UI unit was not written: %v", err)
	}
	if want := migrated.InferenceSecret + ";" + migrated.InferenceSecret; !strings.Contains(string(owui), want) {
		t.Errorf("chat UI unit does not carry the persisted secret for both endpoints — the render ran before the heal:\n%s", owui)
	}

	// A second apply reuses the SAME secret rather than rotating it.
	if _, err := stackapply.Apply(liveStackDeps(), migrated); err != nil && !errors.As(err, &missing) {
		t.Fatalf("second Apply: %v", err)
	}
	second, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla after second apply: %v", err)
	}
	if second.InferenceSecret != migrated.InferenceSecret {
		t.Errorf("second apply's inference_secret = %q, want the reused %q (never rotated)", second.InferenceSecret, migrated.InferenceSecret)
	}
}
