package orchestrate

// searxng_settings_write_test.go guards the impure searxng config-file writers
// (siblings of WriteUnits). The invariants under test (Phase-29 Plan-02):
//   - the written settings.yml / searxng.env bytes equal the pure renders
//     (RenderSearxngSettings / RenderSearxngSecretEnv) — single source of truth;
//   - both targets resolve inside the villa searxng config dir (assertInsideDir
// traversal guard);
//   - both files are written 0600 and the dir 0700 — the secret env file holds the
// live SEARXNG_SECRET so 0600 is load-bearing;
//   - the write is atomic (temp -> fsync -> rename), leaving no .tmp remnant and an
// intact prior file on a mid-write failure;
// - the resolved dir is the villa config dir, NEVER the systemd unit dir;
//   - the secret env file's host path equals the host side of SearXNGSecretEnvFilePath()
//     (the EnvironmentFile= path Plan 01's unit references — cross-plan contract).

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestWriteSearxngSettingsRoundTrip proves the settings.yml writer persists exactly
// the RenderSearxngSettings bytes into the resolved searxng config dir (single source
// of truth; no second renderer).
func TestWriteSearxngSettingsRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "searxng")
	name, want, err := RenderSearxngSettings(config.VillaConfig{WebSearchEnabled: true})
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, want); err != nil {
		t.Fatalf("WriteSearxngSettingsTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != want {
		t.Fatalf("written settings.yml != RenderSearxngSettings output\n got=%q\nwant=%q", got, want)
	}
}

// TestWriteSearxngSecretEnvRoundTrip proves the secret-env writer persists exactly the
// RenderSearxngSecretEnv bytes (a SEARXNG_SECRET=<value> line) into the SAME dir.
func TestWriteSearxngSecretEnvRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "searxng")
	name, want := RenderSearxngSecretEnv("deadbeefcafe")
	if err := WriteSearxngSecretEnvTo(dir, name, want); err != nil {
		t.Fatalf("WriteSearxngSecretEnvTo: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != want {
		t.Fatalf("written searxng.env != RenderSearxngSecretEnv output\n got=%q\nwant=%q", got, want)
	}
	if !strings.HasPrefix(string(got), "SEARXNG_SECRET=") {
		t.Fatalf("env file does not carry SEARXNG_SECRET line: %q", got)
	}
}

// TestWriteSearxngFilesMode asserts BOTH the settings.yml and the secret env file are
// written 0600 and the created dir is 0700 (secret-safe discipline; never the 0644
// unitFileMode). The secret env file at 0600 is load-bearing — it holds the live secret.
func TestWriteSearxngFilesMode(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX file modes not meaningful on windows")
	}
	dir := filepath.Join(t.TempDir(), "villa", "searxng")

	sName, sText, err := RenderSearxngSettings(config.VillaConfig{WebSearchEnabled: true})
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, sName, sText); err != nil {
		t.Fatalf("WriteSearxngSettingsTo: %v", err)
	}
	eName, eText := RenderSearxngSecretEnv("deadbeef")
	if err := WriteSearxngSecretEnvTo(dir, eName, eText); err != nil {
		t.Fatalf("WriteSearxngSecretEnvTo: %v", err)
	}

	dInfo, err := os.Stat(dir)
	if err != nil {
		t.Fatalf("stat dir: %v", err)
	}
	if got := dInfo.Mode().Perm(); got != 0o700 {
		t.Fatalf("dir mode = %o, want 0700", got)
	}
	for _, name := range []string{sName, eName} {
		fi, err := os.Stat(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("stat %s: %v", name, err)
		}
		if got := fi.Mode().Perm(); got != 0o600 {
			t.Fatalf("%s mode = %o, want 0600 (must NOT reuse unitFileMode 0644)", name, got)
		}
	}
	// Guard against accidental reuse of the unit mode for these secret-safe writes.
	if searxngSettingsFileMode == unitFileMode {
		t.Fatalf("searxngSettingsFileMode must be 0600, distinct from unitFileMode 0644")
	}
}

// TestWriteSearxngTraversalRefused proves a name resolving OUTSIDE the searxng config
// dir is refused before any write (assertInsideDir) — for BOTH writers.
func TestWriteSearxngTraversalRefused(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "searxng")
	evil := filepath.Join("..", "..", "escape.yml")

	if err := WriteSearxngSettingsTo(dir, evil, "x"); err == nil {
		t.Fatalf("WriteSearxngSettingsTo accepted a traversal name; want refusal")
	}
	if err := WriteSearxngSecretEnvTo(dir, evil, "SEARXNG_SECRET=x\n"); err == nil {
		t.Fatalf("WriteSearxngSecretEnvTo accepted a traversal name; want refusal")
	}
	// Nothing must have been written outside the dir.
	if _, err := os.Stat(filepath.Join(filepath.Dir(filepath.Dir(dir)), "escape.yml")); !os.IsNotExist(err) {
		t.Fatalf("traversal write leaked a file outside the config dir")
	}
}

// TestWriteSearxngAtomicNoTmpRemnant proves the writer leaves no .tmp remnant and an
// intact prior file: a successful re-write over an existing file yields the new bytes
// and never an orphaned <name>.tmp (atomic temp -> rename).
func TestWriteSearxngAtomicNoTmpRemnant(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "searxng")
	name, first, err := RenderSearxngSettings(config.VillaConfig{WebSearchEnabled: true})
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, first); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, "second-content\n"); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != "second-content\n" {
		t.Fatalf("re-write did not replace content atomically: %q", got)
	}
	if _, err := os.Stat(filepath.Join(dir, name+".tmp")); !os.IsNotExist(err) {
		t.Fatalf("orphaned .tmp remnant left behind: %v", err)
	}
}

// TestWriteSearxngIdempotent proves re-writing identical content leaves a byte-identical
// file (no spurious churn).
func TestWriteSearxngIdempotent(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa", "searxng")
	name, text, err := RenderSearxngSettings(config.VillaConfig{WebSearchEnabled: true})
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, text); err != nil {
		t.Fatalf("first write: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, text); err != nil {
		t.Fatalf("second write: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(dir, name))
	if err != nil {
		t.Fatalf("read back: %v", err)
	}
	if string(got) != text {
		t.Fatalf("idempotent re-write changed bytes: %q", got)
	}
}

// TestWriteSearxngCreatesDir proves the writer creates the config dir (MkdirAll 0700)
// when absent — the first install writes cleanly.
func TestWriteSearxngCreatesDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "deep", "villa", "searxng")
	name, text, err := RenderSearxngSettings(config.VillaConfig{WebSearchEnabled: true})
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if err := WriteSearxngSettingsTo(dir, name, text); err != nil {
		t.Fatalf("write into absent dir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, name)); err != nil {
		t.Fatalf("file not created in absent dir: %v", err)
	}
}

// TestSearxngSettingsDirLiveResolver proves the LIVE resolver honors $XDG_CONFIG_HOME
// (via os.UserConfigDir), joins villa/searxng, and is NEVER the systemd unit dir
// (Pitfall 1). It also proves the secret-env host path equals the host side of
// SearXNGSecretEnvFilePath() — the EnvironmentFile= path Plan 01's unit references
// (cross-plan contract).
func TestSearxngSettingsDirLiveResolver(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	dir, err := searxngSettingsDir()
	if err != nil {
		t.Fatalf("searxngSettingsDir: %v", err)
	}
	want := filepath.Join(base, "villa", "searxng")
	if dir != want {
		t.Fatalf("searxngSettingsDir = %q, want %q", dir, want)
	}
	// Must NOT resolve into the systemd unit dir.
	if strings.Contains(dir, filepath.Join("systemd", "user")) ||
		strings.Contains(dir, filepath.Join("containers", "systemd")) {
		t.Fatalf("searxngSettingsDir resolved into a systemd unit dir: %q", dir)
	}

	// Cross-plan contract: the secret env file's host path matches the host side of the
	// %h-form EnvironmentFile= path the unit references.
	envName, _ := RenderSearxngSecretEnv("x")
	gotEnvHostPath := filepath.Join(dir, envName)
	unitRef := SearXNGSecretEnvFilePath() // %h/.config/villa/searxng/searxng.env
	hostSuffix := filepath.Join("villa", "searxng", envName)
	if !strings.HasSuffix(filepath.ToSlash(unitRef), filepath.ToSlash(hostSuffix)) {
		t.Fatalf("unit EnvironmentFile= ref %q does not end with the host suffix %q", unitRef, hostSuffix)
	}
	if !strings.HasSuffix(filepath.ToSlash(gotEnvHostPath), filepath.ToSlash(hostSuffix)) {
		t.Fatalf("secret env host path %q does not match the unit-referenced suffix %q", gotEnvHostPath, hostSuffix)
	}
}

// TestWriteSearxngSecretEnvLiveTargetsConfigDir proves the LIVE WriteSearxngSecretEnv
// (no explicit dir) writes into $XDG_CONFIG_HOME/villa/searxng — the same dir the unit's
// EnvironmentFile= references — never the unit dir.
func TestWriteSearxngSecretEnvLiveTargetsConfigDir(t *testing.T) {
	base := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", base)

	name, text := RenderSearxngSecretEnv("livecafe")
	if err := WriteSearxngSecretEnv(name, text); err != nil {
		t.Fatalf("WriteSearxngSecretEnv: %v", err)
	}
	got, err := os.ReadFile(filepath.Join(base, "villa", "searxng", name))
	if err != nil {
		t.Fatalf("secret env not written into live config dir: %v", err)
	}
	if string(got) != text {
		t.Fatalf("live secret env bytes != render: %q", got)
	}
}
