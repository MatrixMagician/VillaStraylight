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
		// Memory fields (D-04/D-08): populate with the inert defaults so the
		// full-literal equality assertion survives the schema extension.
		MemoryEnabled:  false,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		QdrantAddr:     "villa-qdrant",
		QdrantPort:     6333,
		EmbedAddr:      "villa-embed",
		EmbedPort:      8080,
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

// memoryDefaults captures the inert default-OFF memory state defaultConfig()
// must seed (D-04/D-08): MemoryEnabled false, the pinned embedding model/dim, and
// the container-DNS-only Qdrant/embed endpoints (never a routable host bind).
func memoryDefaults() VillaConfig {
	return VillaConfig{
		MemoryEnabled:  false,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		QdrantAddr:     "villa-qdrant",
		QdrantPort:     6333,
		EmbedAddr:      "villa-embed",
		EmbedPort:      8080,
	}
}

// TestDefaultConfigMemoryFields asserts defaultConfig() seeds the memory defaults
// directly (the SINGLE home of those literals, D-05), independent of file I/O.
// MemoryEnabled defaults false (D-04); the rest are inert until opt-in.
func TestDefaultConfigMemoryFields(t *testing.T) {
	d := defaultConfig()
	if d.MemoryEnabled {
		t.Errorf("defaultConfig() MemoryEnabled = true, want false (default-OFF, D-04)")
	}
	if d.EmbeddingModel != "nomic-embed-text-v1.5" {
		t.Errorf("default EmbeddingModel = %q, want nomic-embed-text-v1.5", d.EmbeddingModel)
	}
	if d.EmbeddingDim != 768 {
		t.Errorf("default EmbeddingDim = %d, want 768 (pinned, no Matryoshka truncation)", d.EmbeddingDim)
	}
	if d.QdrantAddr != "villa-qdrant" || d.QdrantPort != 6333 {
		t.Errorf("default Qdrant endpoint = {%q, %d}, want {villa-qdrant, 6333} (container-DNS only)",
			d.QdrantAddr, d.QdrantPort)
	}
	if d.EmbedAddr != "villa-embed" || d.EmbedPort != 8080 {
		t.Errorf("default embed endpoint = {%q, %d}, want {villa-embed, 8080} (container-DNS only)",
			d.EmbedAddr, d.EmbedPort)
	}
}

// TestLoadMemoryDefaultsOff asserts a v1.2-style config.toml carrying NO memory
// keys loads with memory defaulted-OFF and the coherent inert defaults (SC#1).
func TestLoadMemoryDefaultsOff(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A v1.2 config: only the v1.2 keys, NO memory keys.
	tomlBytes := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
dashboard_addr = "127.0.0.1"
dashboard_port = 8888
chat_port = 3000
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBytes), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.MemoryEnabled {
		t.Errorf("v1.2 config loaded MemoryEnabled = true, want false (default-OFF, SC#1)")
	}
	wantMem := memoryDefaults()
	if got.EmbeddingModel != wantMem.EmbeddingModel || got.EmbeddingDim != wantMem.EmbeddingDim ||
		got.QdrantAddr != wantMem.QdrantAddr || got.QdrantPort != wantMem.QdrantPort ||
		got.EmbedAddr != wantMem.EmbedAddr || got.EmbedPort != wantMem.EmbedPort {
		t.Errorf("v1.2 config did not get inert memory defaults:\n got %+v\nwant %+v", got, wantMem)
	}
	// The v1.2 selection must survive untouched.
	if got.Model != "qwen3-35b-a3b-moe-64" || got.Backend != "vulkan" {
		t.Errorf("v1.2 selection mangled: %+v", got)
	}
}

// TestLoadMissingReturnsMemoryDefaults asserts an absent file equals defaultConfig()
// including the memory defaults (mirrors TestLoadMissingReturnsDefaults).
func TestLoadMissingReturnsMemoryDefaults(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom(absent): %v", err)
	}
	if got != defaultConfig() {
		t.Errorf("absent config = %+v, want defaults %+v", got, defaultConfig())
	}
}

// TestNormalizeMemorySelfHeal asserts an on-disk config with zeroed/empty memory
// fields self-heals on load to the defaultConfig() values via normalizeVilla
// (mirrors TestLoadNormalizesZeroPorts). The fill derives from defaultConfig()
// (single source) and NEVER widens a bind (T-18-02 / PRIV-01).
func TestNormalizeMemorySelfHeal(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// memory_enabled=true so the inert fields are load-bearing, but the endpoint
	// and dim fields arrive zeroed/empty (the partial-write / hand-edit case).
	tomlBytes := `model = "m"
quant = "q"
ctx = 4096
backend = "vulkan"
memory_enabled = true
embedding_model = ""
embedding_dim = 0
qdrant_addr = ""
qdrant_port = 0
embed_addr = ""
embed_port = 0
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBytes), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if !got.MemoryEnabled {
		t.Errorf("MemoryEnabled = false, want true (explicit opt-in must survive)")
	}
	wantMem := memoryDefaults()
	if got.EmbeddingModel != wantMem.EmbeddingModel {
		t.Errorf("empty EmbeddingModel self-heal = %q, want %q", got.EmbeddingModel, wantMem.EmbeddingModel)
	}
	if got.EmbeddingDim != wantMem.EmbeddingDim {
		t.Errorf("zeroed EmbeddingDim self-heal = %d, want %d", got.EmbeddingDim, wantMem.EmbeddingDim)
	}
	if got.QdrantAddr != wantMem.QdrantAddr || got.QdrantPort != wantMem.QdrantPort {
		t.Errorf("zeroed Qdrant endpoint self-heal = {%q, %d}, want {%q, %d} (container-DNS only)",
			got.QdrantAddr, got.QdrantPort, wantMem.QdrantAddr, wantMem.QdrantPort)
	}
	if got.EmbedAddr != wantMem.EmbedAddr || got.EmbedPort != wantMem.EmbedPort {
		t.Errorf("zeroed embed endpoint self-heal = {%q, %d}, want {%q, %d} (container-DNS only)",
			got.EmbedAddr, got.EmbedPort, wantMem.EmbedAddr, wantMem.EmbedPort)
	}
}

// TestMemoryNeverWidensBind asserts normalizeVilla fills empty endpoint addrs ONLY
// with the container-DNS default name — it never substitutes a routable/widened
// bind (T-18-02 / PRIV-01), mirroring the dashboard_addr loopback rule.
func TestMemoryNeverWidensBind(t *testing.T) {
	got := normalizeVilla(VillaConfig{MemoryEnabled: true})
	if got.QdrantAddr != "villa-qdrant" {
		t.Errorf("empty QdrantAddr filled with %q, want container-DNS villa-qdrant (never a routable bind)", got.QdrantAddr)
	}
	if got.EmbedAddr != "villa-embed" {
		t.Errorf("empty EmbedAddr filled with %q, want container-DNS villa-embed (never a routable bind)", got.EmbedAddr)
	}
	for _, addr := range []string{got.QdrantAddr, got.EmbedAddr} {
		if strings.Contains(addr, "0.0.0.0") || addr == "" {
			t.Errorf("endpoint addr %q widened/zeroed — PRIV-01 violation", addr)
		}
	}
}

// TestMemoryPreservesExplicitNonDefault asserts normalization only FILLS unset
// memory fields and never overrides an explicit non-default choice (mirrors
// TestLoadPreservesExplicitNonZero).
func TestMemoryPreservesExplicitNonDefault(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	tomlBytes := `model = "m"
quant = "q"
ctx = 4096
backend = "vulkan"
memory_enabled = true
embedding_model = "custom-embed-model"
embedding_dim = 1024
qdrant_addr = "my-qdrant"
qdrant_port = 7777
embed_addr = "my-embed"
embed_port = 9090
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(tomlBytes), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if !got.MemoryEnabled || got.EmbeddingModel != "custom-embed-model" || got.EmbeddingDim != 1024 ||
		got.QdrantAddr != "my-qdrant" || got.QdrantPort != 7777 ||
		got.EmbedAddr != "my-embed" || got.EmbedPort != 9090 {
		t.Errorf("normalization overrode explicit memory values: %+v", got)
	}
}

// TestMemoryByteIdentical proves SC#1's load-path half (D-05): loading a v1.2
// config.toml that carries NO memory keys self-heals the IN-MEMORY struct to the
// memory-off defaults WITHOUT mutating the on-disk file. The guarantee is the
// ABSENCE of a memory save path in Phase 18 — load is read-only. Re-reading the
// file bytes after load must equal the original bytes. The test deliberately does
// NOT call SaveVilla/SaveVillaTo: manufacturing memory keys would be the very
// regression SC#1 forbids (Pitfall 1: BurntSushi/toml emits type-zero keys).
func TestMemoryByteIdentical(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A pristine v1.2 config: only the v1.2 keys, NO memory keys.
	v12 := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
dashboard_addr = "127.0.0.1"
dashboard_port = 8888
chat_port = 3000
`
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(v12), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}

	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read before: %v", err)
	}

	// Load self-heals the in-memory struct to memory-off defaults.
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.MemoryEnabled {
		t.Errorf("in-memory MemoryEnabled = true after load of a memory-key-free config, want false")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read after: %v", err)
	}
	if string(after) != string(before) {
		t.Errorf("load mutated the on-disk config (SC#1 byte-identical break):\nbefore:\n%s\nafter:\n%s",
			before, after)
	}
	// Belt-and-braces: no memory key leaked into the file on load.
	for _, key := range []string{"memory_enabled", "embedding_model", "embedding_dim",
		"qdrant_addr", "qdrant_port", "embed_addr", "embed_port"} {
		if strings.Contains(string(after), key) {
			t.Errorf("memory key %q appeared in a non-opted-in config after load (SC#1 violation)", key)
		}
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
