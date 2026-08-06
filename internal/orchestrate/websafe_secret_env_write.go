package orchestrate

// websafe_secret_env_write.go holds the impure villa-websafe secret-env writer — a sibling
// of WriteUnits (reconcile.go) and WriteSearxngSecretEnv (searxng_settings_write.go) — that
// persists the pure RenderWebsafeSecretEnv render (an EXTERNAL_WEB_LOADER_API_KEY=<value>
// line) into a villa-owned config dir ($XDG_CONFIG_HOME/villa/websafe), NOT the systemd unit
// dir (Pitfall 1: systemd would try to parse a non-unit file). It writes atomically
// (temp -> fsync -> rename, the same discipline as reconcile.go's atomicWrite), refuses any
// target resolving outside the websafe config dir (assertInsideDir, reused from reconcile.go),
// and writes at mode 0600 / dir 0700 — reusing the writeSearxngFile / atomicWriteMode helpers
// from searxng_settings_write.go so the atomic + traversal-guarded + mode-fixed discipline
// lives in exactly one place.
//
// The mode is the load-bearing divergence from WriteUnits: units are world-readable 0644
// (systemd --user must read them), but websafe.env stays 0600. The secret env file holds the
// live EXTERNAL_WEB_LOADER_API_KEY bearer — 0600 is the ONLY thing keeping it from every local
// user, and it is the route that keeps the secret out of the 0644 unit (BOTH the villa-websafe
// AND the OWUI unit reference it via EnvironmentFile=<path> only).
//
// The resolved path equals the host side of WebsafeSecretEnvFilePath() — the EnvironmentFile=
// path both the villa-websafe and OWUI units reference — so the live secret reaches the
// containers WITHOUT ever landing in a 0644 unit.

import (
	"fmt"
	"os"
	"path/filepath"
)

// websafeSecretEnvDir resolves the villa-owned websafe config dir,
// $XDG_CONFIG_HOME/villa/websafe (os.UserConfigDir honors $XDG_CONFIG_HOME safely). This is
// the host side of the EnvironmentFile= path BOTH the villa-websafe .container unit AND the
// OWUI unit reference (WebsafeSecretEnvFilePath). It is NEVER the systemd unit dir, and it is
// a DIFFERENT directory from the searxng config dir (websafe vs searxng).
func websafeSecretEnvDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("orchestrate: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "villa", "websafe"), nil
}

// WriteWebsafeSecretEnv writes the rendered websafe.env secret file (name+text from
// RenderWebsafeSecretEnv — an EXTERNAL_WEB_LOADER_API_KEY=<value> line) atomically +
// traversal-guarded + 0600 into the live villa websafe config dir. Its resolved path equals
// the host side of WebsafeSecretEnvFilePath() — the EnvironmentFile= path both the
// villa-websafe and OWUI units reference — so the live secret reaches the containers WITHOUT
// ever landing in the 0644 unit. Mirrors WriteSearxngSecretEnv exactly, only the
// target dir differs (websafe, not searxng).
func WriteWebsafeSecretEnv(name, text string) error {
	dir, err := websafeSecretEnvDir()
	if err != nil {
		return err
	}
	return WriteWebsafeSecretEnvTo(dir, name, text)
}

// WriteWebsafeSecretEnvTo is the explicit-dir testable seam behind WriteWebsafeSecretEnv
// (mirrors WriteSearxngSecretEnvTo). The secret bytes are NEVER logged here (the error wraps
// only the filename, not the body). cmd/villa wires WriteWebsafeSecretEnv as the live caller.
func WriteWebsafeSecretEnvTo(dir, name, text string) error {
	// Reuse the shared mode-fixed (0600) atomic + traversal-guarded writer (writeSearxngFile,
	// searxng_settings_write.go) so the atomic-write discipline lives in exactly one place;
	// only the target dir differs.
	if err := writeSearxngFile(dir, name, text); err != nil {
		return fmt.Errorf("orchestrate: write websafe secret env %q: %w", name, err)
	}
	return nil
}
