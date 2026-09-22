package install

// flow_inference_secret_test.go is the install-flow half of GHSA-qxg9 (ADR-0011):
// the inference bearer has no opt-in gate (unlike WebLoaderSecret/SearxngSecret),
// so it must be generated+persisted+written on EVERY install, and it must be the
// upgrade-migration path for an existing install whose config.toml predates the
// field. Mirrors TestInstallWebsafeWiring's shape exactly, minus the opt-in gate.

import (
	"slices"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// TestInstallInferenceSecretWiring drives Run through a config with NO
// InferenceSecret (a fresh install, or an existing config.toml that predates the
// field — the exact upgrade hazard the ticket names) and asserts: the bearer is
// generated exactly once, persisted via SaveConfig, and its 0600 env file is
// written BEFORE the inference unit is started — a unit whose EnvironmentFile is
// missing fails to start under systemd.
func TestInstallInferenceSecretWiring(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	f := newFakeDeps(t, units, plan, passChecks())
	// newFakeDeps' default LoadConfig returns config.DefaultVillaConfig(), whose
	// InferenceSecret is empty — the "never migrated" state.

	code, _, errOut := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("install exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}

	if f.savedCfg.InferenceSecret == "" {
		t.Fatal("install must generate AND persist a non-empty inference_secret when the loaded config has none")
	}
	if f.inferenceSecretEnvCalls != 1 {
		t.Errorf("the 0600 llama.env bearer must be written exactly once, WriteInferenceSecretEnv calls = %d", f.inferenceSecretEnvCalls)
	}

	// The env file must be written BEFORE the inference unit starts: villa-llama's
	// rendered unit now references it via EnvironmentFile=, so `systemctl start`
	// would fail on a missing file (the exact hazard this ticket closes).
	startIdx := slices.Index(f.callOrder, "start:villa-llama.service")
	if startIdx < 0 {
		t.Fatalf("inference unit start not recorded in callOrder = %v", f.callOrder)
	}
	if envIdx := slices.Index(f.callOrder, "writeInferenceSecretEnv"); envIdx < 0 || envIdx > startIdx {
		t.Errorf("the llama.env bearer must be written BEFORE the inference unit starts (env idx=%d, start idx=%d); callOrder = %v", envIdx, startIdx, f.callOrder)
	}
}

// TestInstallInferenceSecretReused simulates the upgrade path end to end: a first
// install with no secret generates and persists one, and a SECOND, independent run
// that loads THAT persisted secret reuses it verbatim rather than rotating it.
func TestInstallInferenceSecretReused(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	first := newFakeDeps(t, units, plan, passChecks())
	if code, _, errOut := first.run(Opts{}); code != exitPass {
		t.Fatalf("first install exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	generated := first.savedCfg.InferenceSecret
	if generated == "" {
		t.Fatal("first install must have generated a non-empty inference_secret")
	}

	// A second, independent run whose LOADED config already carries the secret
	// the first run persisted — the normal shape of a second `villa install`/`up`.
	seeded := config.DefaultVillaConfig()
	seeded.InferenceSecret = generated
	second := newFakeDeps(t, units, plan, passChecks())
	second.persistedConfig = &seeded
	if code, _, errOut := second.run(Opts{}); code != exitPass {
		t.Fatalf("second install exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}

	if second.savedCfg.InferenceSecret != generated {
		t.Errorf("second run's persisted inference_secret = %q, want the reused %q (never rotated)", second.savedCfg.InferenceSecret, generated)
	}
	// The env file is still (re)written every run — self-healing a manually
	// deleted file — but with the SAME value, never a fresh one.
	if second.inferenceSecretEnvText == "" || first.inferenceSecretEnvText != second.inferenceSecretEnvText {
		t.Errorf("second run's written env body = %q, want it byte-identical to the first run's %q", second.inferenceSecretEnvText, first.inferenceSecretEnvText)
	}
}
