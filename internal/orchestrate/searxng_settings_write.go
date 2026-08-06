package orchestrate

// searxng_settings_write.go holds the impure SearXNG config-file writers — siblings of
// WriteUnits (reconcile.go) — that persist the pure renders from Plan 01
// (RenderSearxngSettings / RenderSearxngSecretEnv) into a villa-owned config dir
// ($XDG_CONFIG_HOME/villa/searxng), NOT the systemd unit dir (Pitfall 1: systemd would
// try to parse a non-unit file). They write atomically (temp -> fsync -> rename, the same
// discipline as reconcile.go's atomicWrite), refuse any target resolving outside the
// searxng config dir (assertInsideDir, reused from reconcile.go), and write at
// mode 0600 / dir 0700.
//
// The mode is the load-bearing divergence from WriteUnits: units are world-readable 0644
// (systemd --user must read them), but settings.yml AND searxng.env stay 0600. The secret
// env file holds the live SEARXNG_SECRET — 0600 is the ONLY thing keeping it from every
// local user, and it is the BLOCKER-1 route that keeps the secret out of the 0644 unit
// (the unit references it via EnvironmentFile=<path> only — / Pitfall 2).
//
// Deliberate file-placement divergence from 29-PATTERNS.md (WARNING 4): PATTERNS framed
// this as a reconcile.go MODIFY; this plan keeps the searxng-specific impure writers in
// their own file so reconcile.go (the content-hash idempotency core + generic unit writer)
// stays focused. The discipline (assertInsideDir + temp->fsync->rename) is reused from
// reconcile.go in the same package — only the file location differs.

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
)

// searxngSettingsFileMode is the mode for the written settings.yml AND searxng.env. It is
// 0600 — its OWN const, deliberately NOT reconcile.go's unitFileMode (0644). The secret env
// file written with this mode holds the live SEARXNG_SECRET, so 0600 is mandatory; settings.yml
// stays 0600 for consistency (Pitfall 2). The containing dir is created 0700.
const searxngSettingsFileMode os.FileMode = 0o600

// searxngSettingsDirMode is the mode for the villa searxng config dir (0700), mirroring the
// internal/config secret-dir discipline.
const searxngSettingsDirMode os.FileMode = 0o700

// searxngSettingsDir resolves the villa-owned searxng config dir,
// $XDG_CONFIG_HOME/villa/searxng (os.UserConfigDir honors $XDG_CONFIG_HOME safely, V12).
// This is the host side of BOTH the %h/.config/villa/searxng:/etc/searxng:ro,Z settings
// mount AND the EnvironmentFile= path the .container unit references
// (SearXNGSecretEnvFilePath). It is NEVER the systemd unit dir (Pitfall 1).
func searxngSettingsDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", fmt.Errorf("orchestrate: cannot resolve user config dir: %w", err)
	}
	return filepath.Join(base, "villa", "searxng"), nil
}

// searxngSettingsFileName is the BARE filename WriteSearxngSettings writes (and
// RenderSearxngSettings returns). Single-sourced here so the Phase-34 backup/restore
// path resolves the SAME file without re-typing the literal.
const searxngSettingsFileName = "settings.yml"

// SearXNGSettingsFilePath returns the RESOLVED host path of the rendered SearXNG
// settings.yml, $XDG_CONFIG_HOME/villa/searxng/settings.yml — the exact file
// WriteSearxngSettings writes at 0600. EXPORTED as the cross-plan provenance contract
// so the Phase-34 backup/restore path sources the settings.yml path WITHOUT
// re-typing the dir/filename literals (mirrors how crushConfigPath() is resolved at the
// cmd tier). The file holds the rendered SEARXNG_SECRET, so restore re-writes it
// 0600-preserving (never widens the mode).
func SearXNGSettingsFilePath() (string, error) {
	dir, err := searxngSettingsDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, searxngSettingsFileName), nil
}

// WriteSearxngSettings writes the rendered settings.yml (name+text from
// RenderSearxngSettings) atomically + traversal-guarded + 0600 into the live villa searxng
// config dir. It is a SIBLING of WriteUnits targeting the config dir, never the unit dir.
func WriteSearxngSettings(name, text string) error {
	dir, err := searxngSettingsDir()
	if err != nil {
		return err
	}
	return WriteSearxngSettingsTo(dir, name, text)
}

// WriteSearxngSecretEnv writes the rendered searxng.env secret file (name+text from
// RenderSearxngSecretEnv — a SEARXNG_SECRET=<value> line) atomically + traversal-guarded +
// 0600 into the live villa searxng config dir. Its resolved path equals the host side of
// SearXNGSecretEnvFilePath() — the EnvironmentFile= path Plan 01's unit references — so the
// live secret reaches the container WITHOUT ever landing in the 0644 unit (BLOCKER 1).
func WriteSearxngSecretEnv(name, text string) error {
	dir, err := searxngSettingsDir()
	if err != nil {
		return err
	}
	return WriteSearxngSecretEnvTo(dir, name, text)
}

// WriteSearxngSettingsTo is the explicit-dir testable seam behind WriteSearxngSettings
// (mirrors config.SaveVillaTo). cmd/villa wires WriteSearxngSettings as the live caller.
func WriteSearxngSettingsTo(dir, name, text string) error {
	if err := writeSearxngFile(dir, name, text); err != nil {
		return fmt.Errorf("orchestrate: write searxng settings %q: %w", name, err)
	}
	return nil
}

// WriteSearxngSecretEnvTo is the explicit-dir testable seam behind WriteSearxngSecretEnv.
// The secret bytes are NEVER logged here (the error wraps only the filename, not the body).
func WriteSearxngSecretEnvTo(dir, name, text string) error {
	if err := writeSearxngFile(dir, name, text); err != nil {
		return fmt.Errorf("orchestrate: write searxng secret env %q: %w", name, err)
	}
	return nil
}

// writeSearxngFile is the shared mode-fixed (0600) atomic writer both searxng writers call,
// so the atomic-write logic lives in exactly one place. It: (1) MkdirAll(dir, 0700);
// (2) computes target := dir/name and refuses traversal escapes via assertInsideDir (reused
// from reconcile.go); (3) writes a sibling <name>.tmp at 0600, fsyncs it, closes it, then
// os.Rename over target (atomic on the same filesystem) — removing the temp on any failure
// so no .tmp is ever left behind. Same temp->Sync->Close->Rename->dir-fsync ordering as
// reconcile.go's atomicWrite, differing only in the file mode (0600, not 0644).
func writeSearxngFile(dir, name, text string) error {
	// dir is the trusted root (config-dir derived) and name is the untrusted part,
	// so creating dir up front is safe — the guard inside the write is what refuses
	// a name that escapes it.
	if err := os.MkdirAll(dir, searxngSettingsDirMode); err != nil {
		return err
	}
	target := filepath.Join(dir, name)
	return pathsafe.WriteFileAtomic(dir, target, []byte(text), searxngSettingsFileMode)
}
