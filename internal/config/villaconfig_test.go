package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestSaveLoadRoundTrip asserts SaveVillaTo then LoadVillaFrom round-trips the
// persisted fields, and that the file is written with 0600 perms.
func TestSaveLoadRoundTrip(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")

	want := VillaConfig{
		Model:         "qwen3-35b-a3b-moe-64",
		Quant:         "UD-Q4_K_M",
		Ctx:           131072,
		Backend:       "vulkan",
		CatalogPath:   "/srv/catalogs/newer.json", // persisted external-catalog choice (IN-03)
		DashboardAddr: "127.0.0.1",                // D-13 loopback dashboard bind
		DashboardPort: 8888,                       // D-13 dashboard port
		ChatPort:      3000,                       // D-12 chat link target
	}
	if err := SaveVillaTo(dir, want); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}

	info, err := os.Stat(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("stat config: %v", err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Errorf("config file perms = %o, want 600", perm)
	}

	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got != want {
		t.Errorf("round-trip mismatch:\n got %+v\nwant %+v", got, want)
	}
}

// TestLoadMissingReturnsDefaults asserts Load on an absent file returns typed
// defaults (backend vulkan) with no error — read-only by default (D-20).
func TestLoadMissingReturnsDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom(absent): %v", err)
	}
	if got != defaultConfig() {
		t.Errorf("absent config = %+v, want defaults %+v", got, defaultConfig())
	}
	if got.Backend != "vulkan" {
		t.Errorf("default backend = %q, want vulkan", got.Backend)
	}
	// The dashboard/chat ports default to loopback:8888 / chat 3000 when absent (D-13/D-12).
	if got.DashboardAddr != "127.0.0.1" {
		t.Errorf("default DashboardAddr = %q, want 127.0.0.1 (loopback-only)", got.DashboardAddr)
	}
	if got.DashboardPort != 8888 {
		t.Errorf("default DashboardPort = %d, want 8888", got.DashboardPort)
	}
	if got.ChatPort != 3000 {
		t.Errorf("default ChatPort = %d, want 3000", got.ChatPort)
	}
}

// TestLoadNormalizesZeroPorts asserts that an on-disk config.toml carrying the
// dashboard-breaking zeros (dashboard_port=0 / chat_port=0 / dashboard_addr="")
// self-heals on load to the loopback defaults 8888 / 3000 / 127.0.0.1 — the exact
// case that bit the user (gap test:1b). The real model/quant/ctx are preserved.
func TestLoadNormalizesZeroPorts(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlBytes := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
dashboard_addr = ""
dashboard_port = 0
chat_port = 0
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBytes), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.DashboardPort != 8888 {
		t.Errorf("zeroed DashboardPort self-heal = %d, want 8888", got.DashboardPort)
	}
	if got.ChatPort != 3000 {
		t.Errorf("zeroed ChatPort self-heal = %d, want 3000", got.ChatPort)
	}
	if got.DashboardAddr != "127.0.0.1" {
		t.Errorf("empty DashboardAddr self-heal = %q, want 127.0.0.1 (loopback-only)", got.DashboardAddr)
	}
	// The real selection must survive normalization untouched.
	if got.Model != "qwen3-35b-a3b-moe-64" || got.Quant != "UD-Q4_K_M" || got.Ctx != 131072 || got.Backend != "vulkan" {
		t.Errorf("normalization mangled the real selection: %+v", got)
	}
}

// TestLoadPreservesExplicitNonZero asserts normalization only FILLS unset fields
// and never overrides a real choice: an explicit 9999/4000/::1 round-trips
// unchanged through load.
func TestLoadPreservesExplicitNonZero(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlBytes := `model = "m"
quant = "q"
ctx = 4096
backend = "vulkan"
dashboard_addr = "::1"
dashboard_port = 9999
chat_port = 4000
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBytes), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.DashboardPort != 9999 || got.ChatPort != 4000 || got.DashboardAddr != "::1" {
		t.Errorf("normalization overrode explicit values: got {%q, %d, %d}, want {::1, 9999, 4000}",
			got.DashboardAddr, got.DashboardPort, got.ChatPort)
	}
}

// TestDefaultConfigDashboardFields asserts defaultConfig() seeds the dashboard/chat
// loopback defaults directly (D-13/D-12), independent of file I/O.
func TestDefaultConfigDashboardFields(t *testing.T) {
	d := defaultConfig()
	if d.DashboardAddr != "127.0.0.1" || d.DashboardPort != 8888 || d.ChatPort != 3000 {
		t.Errorf("defaultConfig() dashboard fields = {%q, %d, %d}, want {127.0.0.1, 8888, 3000}",
			d.DashboardAddr, d.DashboardPort, d.ChatPort)
	}
}

// TestPathUnderUserConfigDir asserts Path resolves under os.UserConfigDir()/villa.
func TestPathUnderUserConfigDir(t *testing.T) {
	base, err := os.UserConfigDir()
	if err != nil {
		t.Skipf("UserConfigDir unavailable: %v", err)
	}
	got, err := Path()
	if err != nil {
		t.Fatalf("Path: %v", err)
	}
	want := filepath.Join(base, "villa", "config.toml")
	if got != want {
		t.Errorf("Path() = %q, want %q", got, want)
	}
}

// TestSaveRefusesTraversal asserts that a path escaping the config dir is
// refused (V12 path-traversal guard).
func TestSaveRefusesTraversal(t *testing.T) {
	// assertInsideDir is the guard SaveVilla/SaveVillaTo rely on; exercise it
	// directly with an escaping path to prove traversal is rejected.
	dir := t.TempDir()
	escaping := filepath.Join(dir, "..", "evil.toml")
	if err := assertInsideDir(escaping, dir); err == nil {
		t.Errorf("assertInsideDir allowed an escaping path %q under %q", escaping, dir)
	} else if !strings.Contains(err.Error(), "outside config dir") {
		t.Errorf("unexpected error for traversal: %v", err)
	}

	// And the in-dir path is accepted.
	ok := filepath.Join(dir, "config.toml")
	if err := assertInsideDir(ok, dir); err != nil {
		t.Errorf("assertInsideDir rejected an in-dir path %q: %v", ok, err)
	}
}
