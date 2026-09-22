package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// inference_secret_seam_test.go is the GHSA-qxg9 (ADR-0011) proof that
// liveWriteUnits — the ONE live seam backend set, tools-mode, coding-mode,
// speculation set, model swap, a resident-model verb, `villa update`, `villa
// restore`, and the dashboard's model switch all funnel their
// orchestrate.WriteUnits call through — migrates an upgraded host's missing
// inference secret BEFORE writing any unit, not just up/restart/install.
//
// It drives liveWriteUnits directly rather than a full liveBackendSwapDeps()
// (or liveSwapDeps/liveCodingModeDeps/...) wiring: those live*Deps functions
// also wire real systemd/podman seams that only run on the target host, so this
// targets the ONE function actually shared by every one of their
// ReconcileAndWrite/RestoreUnit(s) closures.

func TestLiveWriteUnitsMigratesMissingInferenceSecret(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	unitDir := t.TempDir()

	plan := orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\nImage=x\n"}}}

	// No secret in the config (the "never migrated" state — config.LoadVilla
	// returns typed defaults from the empty XDG dir above).
	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla: %v", err)
	}
	if cfg.InferenceSecret != "" {
		t.Fatalf("fixture bug: fresh config already has an inference_secret %q", cfg.InferenceSecret)
	}

	if err := liveWriteUnits(plan, unitDir); err != nil {
		t.Fatalf("liveWriteUnits: %v", err)
	}

	// The secret was generated AND persisted.
	migrated, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla after migrate: %v", err)
	}
	if migrated.InferenceSecret == "" {
		t.Fatal("liveWriteUnits must generate and persist a non-empty inference_secret")
	}

	// The env file was written BEFORE the unit — both must be on disk now, and
	// the env file must carry the SAME secret just persisted.
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
	unitPath := filepath.Join(unitDir, "villa-llama.container")
	if _, err := os.Stat(unitPath); err != nil {
		t.Errorf("unit file was not written at %q: %v", unitPath, err)
	}

	// A second run reuses the SAME secret rather than rotating it.
	if err := liveWriteUnits(plan, unitDir); err != nil {
		t.Fatalf("second liveWriteUnits: %v", err)
	}
	second, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla after second migrate: %v", err)
	}
	if second.InferenceSecret != migrated.InferenceSecret {
		t.Errorf("second run's inference_secret = %q, want the reused %q (never rotated)", second.InferenceSecret, migrated.InferenceSecret)
	}
}

// TestLiveWriteUnitsSkipsMigrationOnNoOpPlan: an empty Changed plan (nothing to
// write) must not touch the config or the env file at all — mirrors --dry-run's
// side-effect-free contract for every caller that never reaches liveWriteUnits
// with a non-empty plan in the first place.
func TestLiveWriteUnitsSkipsMigrationOnNoOpPlan(t *testing.T) {
	xdg := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", xdg)
	unitDir := t.TempDir()

	if err := liveWriteUnits(orchestrate.Plan{}, unitDir); err != nil {
		t.Fatalf("liveWriteUnits: %v", err)
	}

	cfg, err := config.LoadVilla()
	if err != nil {
		t.Fatalf("LoadVilla: %v", err)
	}
	if cfg.InferenceSecret != "" {
		t.Errorf("a no-op plan must not generate/persist a secret, got %q", cfg.InferenceSecret)
	}
	if envPath, perr := orchestrate.InferenceSecretEnvHostPath(); perr == nil {
		if _, statErr := os.Stat(envPath); statErr == nil {
			t.Errorf("a no-op plan must not write the env file at %q", envPath)
		}
	}
}
