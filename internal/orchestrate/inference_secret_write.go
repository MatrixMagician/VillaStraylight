package orchestrate

// inference_secret_write.go holds the impure llama.env writer — a sibling of
// WriteUnits (reconcile.go) and WriteWebsafeSecretEnv (websafe_secret_env_write.go)
// that persists the pure RenderInferenceSecretEnv render into a villa-owned config
// dir ($XDG_CONFIG_HOME/villa/inference), NOT the systemd unit dir. It reuses
// writeSearxngFile (searxng_settings_write.go) for the same atomic +
// traversal-guarded + 0600 discipline every other secret-env writer shares.
//
// Unlike WriteWebsafeSecretEnv (gated behind WebSearchEnabled), this one is called
// UNCONDITIONALLY by every unit-writing verb (GHSA-qxg9, ADR-0011): inference has no
// opt-in gate, so the file must exist before villa-llama, any resident slot,
// villa-openwebui or villa-inferproxy is started.

import (
	"fmt"
	"os"
	"path/filepath"
)

// inferenceSecretEnvDir resolves the villa-owned inference config dir,
// $XDG_CONFIG_HOME/villa/inference — the host side of InferenceSecretEnvFilePath(),
// the EnvironmentFile= path every consumer unit references.
func inferenceSecretEnvDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("orchestrate: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "villa", "inference"), nil
}

// WriteInferenceSecretEnv writes the rendered llama.env secret file (name+text from
// RenderInferenceSecretEnv) atomically + traversal-guarded + 0600 into the live villa
// inference config dir. Idempotent: writing the same secret again is a no-op content
// swap, never a rotation — the caller decides whether the value changes.
func WriteInferenceSecretEnv(name, text string) error {
	dir, err := inferenceSecretEnvDir()
	if err != nil {
		return err
	}
	return WriteInferenceSecretEnvTo(dir, name, text)
}

// WriteInferenceSecretEnvTo is the explicit-dir testable seam behind
// WriteInferenceSecretEnv. The secret bytes are NEVER logged here (the error wraps
// only the filename, not the body).
func WriteInferenceSecretEnvTo(dir, name, text string) error {
	if err := writeSearxngFile(dir, name, text); err != nil {
		return fmt.Errorf("orchestrate: write inference secret env %q: %w", name, err)
	}
	return nil
}

// InferenceSecretEnvHostPath returns the REAL host filesystem path of the 0600
// env file WriteInferenceSecretEnv writes. InferenceSecretEnvFilePath() carries
// Quadlet's %h specifier, resolved only at unit-load time by systemd, so a plain
// `podman run --env-file` invocation OUTSIDE Quadlet (the transient
// villa-inference-validate/-ceiling container, GHSA-qxg9, ADR-0011) needs this
// concrete path instead.
func InferenceSecretEnvHostPath() (string, error) {
	dir, err := inferenceSecretEnvDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, inferenceSecretEnvFileName), nil
}
