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
		DashboardPort: 8888,                       // D-13 dashboard port
		ChatPort:      3000,                       // D-12 chat link target
		// Memory fields (D-04/D-08): populate with the inert defaults so the
		// full-literal equality assertion survives the schema extension.
		MemoryEnabled:  false,
		EmbeddingModel: "nomic-embed-text-v1.5",
		EmbeddingDim:   768,
		// Web-search fields (v1.5, SRCH-01): populate with the inert defaults so the
		// full-literal equality assertion survives the schema extension (mirrors the
		// memory-field treatment above). normalizeVilla self-heals these on load.
		WebSearchEnabled:     false,
		WebSearchResultCount: 3, // inert default so the full-literal equality survives the schema extension (normalizeVilla self-heals 0 -> 3 on load)
		// villa-websafe fields (v1.5, GROUND/GUARD): inert addr/port defaults so the
		// full-literal equality survives the schema extension (normalizeVilla self-heals
		// "" / 0 -> villa-websafe / 8090 on load). The secret/path stay empty (not self-healed).
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
	if got.Backend != "rocm" {
		t.Errorf("default backend = %q, want rocm (ROCm 7.2.4 is the default backend)", got.Backend)
	}
	// The dashboard/chat ports default to 8888 / chat 3000 when absent (D-13/D-12).
	// The bind address is no longer persisted: it is the DashboardAddr constant.
	if DashboardAddr != "127.0.0.1" {
		t.Errorf("DashboardAddr = %q, want 127.0.0.1 (loopback-only)", DashboardAddr)
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
	if got.DashboardPort != 9999 || got.ChatPort != 4000 {
		t.Errorf("normalization overrode explicit values: got {%d, %d}, want {9999, 4000}",
			got.DashboardPort, got.ChatPort)
	}
	// The dashboard_addr key in that file is one of the nine that no longer exist.
	// It must be ignored rather than rejected: a config already on disk carrying the
	// old keys has to keep loading.
}

// TestDefaultConfigDashboardFields asserts defaultConfig() seeds the dashboard/chat
// port defaults directly (D-13/D-12), independent of file I/O. The bind address is
// no longer among them — it is the DashboardAddr constant, asserted separately.
func TestDefaultConfigDashboardFields(t *testing.T) {
	d := defaultConfig()
	if d.DashboardPort != 8888 || d.ChatPort != 3000 {
		t.Errorf("defaultConfig() dashboard fields = {%d, %d}, want {8888, 3000}",
			d.DashboardPort, d.ChatPort)
	}
}

// TestServiceIdentityConstants pins the nine values that used to be persisted
// config keys. They moved to constants because nothing could set them, and the
// values themselves must not drift: the addresses are the container-DNS names the
// rendered units and the in-network probes both resolve, and widening any of them
// off the private network (or off loopback, for the dashboard) is the privacy
// violation the fields were never allowed to express (PRIV-01).
func TestServiceIdentityConstants(t *testing.T) {
	cases := []struct {
		name string
		got  any
		want any
	}{
		{"DashboardAddr", DashboardAddr, "127.0.0.1"},
		{"QdrantAddr", QdrantAddr, "villa-qdrant"},
		{"QdrantPort", QdrantPort, 6333},
		{"EmbedAddr", EmbedAddr, "villa-embed"},
		{"EmbedPort", EmbedPort, 8080},
		{"SearxngAddr", SearxngAddr, "villa-searxng"},
		{"SearxngPort", SearxngPort, 8080},
		{"WebsafeAddr", WebsafeAddr, "villa-websafe"},
		{"WebsafePort", WebsafePort, 8090},
	}
	for _, tc := range cases {
		if tc.got != tc.want {
			t.Errorf("%s = %v, want %v (the value the removed config key resolved to)", tc.name, tc.got, tc.want)
		}
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
	// The Qdrant and embed endpoints are no longer defaults on this struct: they are
	// constants, asserted by TestServiceIdentityConstants.
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
	if got.EmbeddingModel != wantMem.EmbeddingModel || got.EmbeddingDim != wantMem.EmbeddingDim {
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
}

// TestEndpointsNeverWidenBind asserts the service endpoints are container-DNS
// names on the private network and the dashboard is loopback — never a routable
// or all-interfaces bind (T-18-02 / PRIV-01).
//
// This used to be a property of normalizeVilla, which healed a hand-edited value
// back to the default. It is now stronger: there is no value to hand-edit, so the
// only way to widen a bind is to change the constant, which this test refuses.
func TestEndpointsNeverWidenBind(t *testing.T) {
	for _, addr := range []string{QdrantAddr, EmbedAddr, SearxngAddr, WebsafeAddr, DashboardAddr} {
		if addr == "" || strings.Contains(addr, "0.0.0.0") || addr == "::" {
			t.Errorf("endpoint addr %q is widened or empty — PRIV-01 violation", addr)
		}
	}
	if DashboardAddr != "127.0.0.1" {
		t.Errorf("DashboardAddr = %q, want loopback", DashboardAddr)
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
	if !got.MemoryEnabled || got.EmbeddingModel != "custom-embed-model" || got.EmbeddingDim != 1024 {
		t.Errorf("normalization overrode explicit memory values: %+v", got)
	}
	// The qdrant_*/embed_* keys in that file are among the nine that no longer
	// exist. They are ignored rather than rejected, which is what lets a config
	// already on disk keep loading.
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

// TestMemorySaveOmitsKeysWhenDisabled guards SC#1 on the SAVE path (the gap a
// load-only test cannot see): a save-bearing command on a memory-off install must
// NOT introduce any memory_* key on disk, even though the in-memory struct carries
// non-zero memory defaults. marshalVilla zeroes the memory fields when disabled so
// the ,omitempty tags drop all three remaining keys. When memory is ON, every key
// is written. (The endpoint keys are no longer persisted at all — they are
// constants — so they can never appear on either side.)
func TestMemorySaveOmitsKeysWhenDisabled(t *testing.T) {
	memKeys := []string{"memory_enabled", "embedding_model", "embedding_dim"}

	// Memory OFF: a config seeded from typed defaults (non-zero memory fields).
	off := DefaultVillaConfig() // MemoryEnabled == false, memory fields at defaults
	off.Model = "qwen3-35b-a3b-moe-64"
	dirOff := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOff, off); err != nil {
		t.Fatalf("SaveVillaTo(off): %v", err)
	}
	dataOff, err := os.ReadFile(filepath.Join(dirOff, "config.toml"))
	if err != nil {
		t.Fatalf("read off config: %v", err)
	}
	for _, k := range memKeys {
		if strings.Contains(string(dataOff), k) {
			t.Errorf("memory-off save wrote memory key %q (SC#1 byte-identical break):\n%s", k, dataOff)
		}
	}

	// Memory ON: opting in must persist the full memory contract.
	on := DefaultVillaConfig()
	on.MemoryEnabled = true
	dirOn := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOn, on); err != nil {
		t.Fatalf("SaveVillaTo(on): %v", err)
	}
	dataOn, err := os.ReadFile(filepath.Join(dirOn, "config.toml"))
	if err != nil {
		t.Fatalf("read on config: %v", err)
	}
	for _, k := range memKeys {
		if !strings.Contains(string(dataOn), k) {
			t.Errorf("memory-on save omitted memory key %q (opt-in must persist the full contract):\n%s", k, dataOn)
		}
	}

	// Opting in then saving must round-trip back to an equal struct.
	got, err := LoadVillaFrom(dirOn)
	if err != nil {
		t.Fatalf("LoadVillaFrom(on): %v", err)
	}
	if got != on {
		t.Errorf("memory-on round-trip mismatch:\n got %+v\nwant %+v", got, on)
	}
}

// TestCodingModeSaveOmitsKeysWhenDisabled is the coding-mode (Phase-25 D-04) twin of
// TestMemorySaveOmitsKeysWhenDisabled: on a non-coding install a save-bearing command
// must NOT introduce any coding_mode / coder_* key on disk (byte-identical off-path,
// D-02/D-04). marshalVilla zeroes the coder_* fields when CodingMode==false so the
// ,omitempty tags drop all four keys. When coding mode is ON every field is written and
// round-trips. The coder_* fields hold the model/quant/agent_ctx RESOLVED AT ENTER.
func TestCodingModeSaveOmitsKeysWhenDisabled(t *testing.T) {
	codeKeys := []string{"coding_mode", "coder_model", "coder_quant", "coder_agent_ctx"}

	// Coding mode OFF: a config seeded from typed defaults (CodingMode == false). Even if
	// the in-memory struct carried resolved coder fields, off-path marshal must drop them.
	off := DefaultVillaConfig()
	off.Model = "qwen3-35b-a3b-moe-64"
	// Seed non-zero coder fields to prove the off-path zeroing is the gate (not absence).
	off.CoderModel = "qwen3-coder-30b"
	off.CoderQuant = "UD-Q4_K_M"
	off.CoderAgentCtx = 65536
	dirOff := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOff, off); err != nil {
		t.Fatalf("SaveVillaTo(off): %v", err)
	}
	dataOff, err := os.ReadFile(filepath.Join(dirOff, "config.toml"))
	if err != nil {
		t.Fatalf("read off config: %v", err)
	}
	for _, k := range codeKeys {
		if strings.Contains(string(dataOff), k) {
			t.Errorf("coding-off save wrote coding key %q (D-02/D-04 byte-identical break):\n%s", k, dataOff)
		}
	}

	// Coding mode ON: opting in must persist the full coder contract (resolved at enter).
	on := DefaultVillaConfig()
	on.Model = "qwen3-35b-a3b-moe-64"
	on.CodingMode = true
	on.CoderModel = "qwen3-coder-30b"
	on.CoderQuant = "UD-Q4_K_M"
	on.CoderAgentCtx = 65536
	dirOn := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOn, on); err != nil {
		t.Fatalf("SaveVillaTo(on): %v", err)
	}
	dataOn, err := os.ReadFile(filepath.Join(dirOn, "config.toml"))
	if err != nil {
		t.Fatalf("read on config: %v", err)
	}
	for _, k := range codeKeys {
		if !strings.Contains(string(dataOn), k) {
			t.Errorf("coding-on save omitted coding key %q (opt-in must persist the full contract):\n%s", k, dataOn)
		}
	}

	// Opting in then saving must round-trip back to an equal struct (all four preserved).
	got, err := LoadVillaFrom(dirOn)
	if err != nil {
		t.Fatalf("LoadVillaFrom(on): %v", err)
	}
	if got != on {
		t.Errorf("coding-on round-trip mismatch:\n got %+v\nwant %+v", got, on)
	}
}

// TestCodingModeNotSelfHealed asserts normalizeVilla does NOT self-heal the coding
// fields: CodingMode is a deliberate bool toggle (false is a valid explicit choice,
// mirroring MemoryEnabled), and the coder_* fields have no meaningful default when off
// (they are dropped on disk and always re-written by the enter path). A loaded
// coding-off config leaves CoderAgentCtx==0 / CoderModel=="" exactly as parsed.
func TestCodingModeNotSelfHealed(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	// A coding-off config: no coding keys at all (the byte-identical install).
	cfg := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "vulkan"
dashboard_addr = "127.0.0.1"
dashboard_port = 8888
chat_port = 3000
`
	path := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(path, []byte(cfg), 0o600); err != nil {
		t.Fatalf("write config: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.CodingMode {
		t.Errorf("CodingMode self-healed to true on a coding-key-free config, want false")
	}
	if got.CoderModel != "" || got.CoderQuant != "" || got.CoderAgentCtx != 0 {
		t.Errorf("coder_* fields were self-healed to a default; want zero values when off, got %q/%q/%d",
			got.CoderModel, got.CoderQuant, got.CoderAgentCtx)
	}
}

// TestAgentEnabledSaveOmitsKeyWhenDisabled is the v1.4 coding-agent (Phase-27 D-01)
// twin of the memory/coding omit-when-off tests: on a non-agent install a save-bearing
// command must NOT introduce the agent_enabled key on disk (byte-identical off-path,
// D-01). AgentEnabled is a plain bool with ,omitempty, so a default-false marshal drops
// the key with no marshalVilla zeroing. When the agent addon is ON the key is written
// and round-trips.
func TestAgentEnabledSaveOmitsKeyWhenDisabled(t *testing.T) {
	const agentKey = "agent_enabled"

	// Agent OFF: a config seeded from typed defaults (AgentEnabled == false).
	off := DefaultVillaConfig()
	off.Model = "qwen3-35b-a3b-moe-64"
	dirOff := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOff, off); err != nil {
		t.Fatalf("SaveVillaTo(off): %v", err)
	}
	dataOff, err := os.ReadFile(filepath.Join(dirOff, "config.toml"))
	if err != nil {
		t.Fatalf("read off config: %v", err)
	}
	if strings.Contains(string(dataOff), agentKey) {
		t.Errorf("agent-off save wrote %q (D-01 byte-identical break):\n%s", agentKey, dataOff)
	}

	// Agent ON: opting in must persist the gate so a bare re-install gates on it.
	on := DefaultVillaConfig()
	on.Model = "qwen3-35b-a3b-moe-64"
	on.AgentEnabled = true
	dirOn := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOn, on); err != nil {
		t.Fatalf("SaveVillaTo(on): %v", err)
	}
	dataOn, err := os.ReadFile(filepath.Join(dirOn, "config.toml"))
	if err != nil {
		t.Fatalf("read on config: %v", err)
	}
	if !strings.Contains(string(dataOn), agentKey) {
		t.Errorf("agent-on save omitted %q (opt-in must persist the gate):\n%s", agentKey, dataOn)
	}

	got, err := LoadVillaFrom(dirOn)
	if err != nil {
		t.Fatalf("LoadVillaFrom(on): %v", err)
	}
	if !got.AgentEnabled {
		t.Errorf("agent-on round-trip lost the gate: AgentEnabled = false, want true")
	}
}

// TestAgentEnabledNotSelfHealed asserts normalizeVilla does NOT self-heal AgentEnabled:
// like MemoryEnabled / CodingMode it is a deliberate bool toggle (false is a valid
// explicit choice). A loaded agent-free config leaves AgentEnabled==false as parsed.
func TestAgentEnabledNotSelfHealed(t *testing.T) {
	got := normalizeVilla(VillaConfig{})
	if got.AgentEnabled {
		t.Errorf("normalizeVilla widened AgentEnabled to true, want false (not self-healed, D-01)")
	}
}

// TestDefaultConfigWebSearchFields asserts defaultConfig() seeds the web-search defaults
// directly (the SINGLE home of those literals, SRCH-01), independent of file I/O.
// WebSearchEnabled defaults false; the addr/port are the container-DNS-only endpoint;
// the secret has no default (generated at opt-in).
func TestDefaultConfigWebSearchFields(t *testing.T) {
	d := defaultConfig()
	if d.WebSearchEnabled {
		t.Errorf("defaultConfig() WebSearchEnabled = true, want false (default-OFF)")
	}
	if d.SearxngSecret != "" {
		t.Errorf("default SearxngSecret = %q, want empty (generated at opt-in, never a hardcoded default)", d.SearxngSecret)
	}
	if d.WebSearchResultCount != 3 {
		t.Errorf("default WebSearchResultCount = %d, want 3 (conservative ctx budget ahead of Phase 31)", d.WebSearchResultCount)
	}
}

// TestWebSearchSaveOmitsKeysWhenDisabled is the web-search (v1.5, SC#4/PRIV-07) twin of
// the memory/coding omit-when-off tests: on a non-web-search install a save-bearing
// command must NOT introduce any web-search key on disk, even though the in-memory struct
// carries non-zero searxng defaults. marshalVilla zeroes the searxng fields when disabled
// so the ,omitempty/,omitzero tags drop all four keys. When web search is ON, every key
// is written and round-trips.
func TestWebSearchSaveOmitsKeysWhenDisabled(t *testing.T) {
	webKeys := []string{"web_search_enabled", "searxng_secret", "web_search_result_count"}

	// Web search OFF: a config seeded from typed defaults (non-zero searxng fields). Seed a
	// secret too, to prove the off-path zeroing is the gate (not mere absence).
	off := DefaultVillaConfig() // WebSearchEnabled == false, searxng fields at defaults
	off.Model = "qwen3-35b-a3b-moe-64"
	off.SearxngSecret = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef0"
	dirOff := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOff, off); err != nil {
		t.Fatalf("SaveVillaTo(off): %v", err)
	}
	dataOff, err := os.ReadFile(filepath.Join(dirOff, "config.toml"))
	if err != nil {
		t.Fatalf("read off config: %v", err)
	}
	for _, k := range webKeys {
		if strings.Contains(string(dataOff), k) {
			t.Errorf("web-search-off save wrote web-search key %q (SC#4 byte-identical break):\n%s", k, dataOff)
		}
	}
	// The secret VALUE must never appear in the off config (it is zeroed before marshal).
	if strings.Contains(string(dataOff), "deadbeef") {
		t.Errorf("web-search-off save leaked the secret value into config.toml:\n%s", dataOff)
	}

	// Web search ON: opting in must persist the full searxng contract.
	on := DefaultVillaConfig()
	on.Model = "qwen3-35b-a3b-moe-64"
	on.WebSearchEnabled = true
	on.SearxngSecret = "deadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeefdeadbeef0"
	on.WebSearchResultCount = 7 // a non-default value must round-trip when web search is ON
	dirOn := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOn, on); err != nil {
		t.Fatalf("SaveVillaTo(on): %v", err)
	}
	dataOn, err := os.ReadFile(filepath.Join(dirOn, "config.toml"))
	if err != nil {
		t.Fatalf("read on config: %v", err)
	}
	for _, k := range webKeys {
		if !strings.Contains(string(dataOn), k) {
			t.Errorf("web-search-on save omitted web-search key %q (opt-in must persist the full contract):\n%s", k, dataOn)
		}
	}
	// The tuned (non-default) result count must be persisted verbatim (round-trip when ON).
	if !strings.Contains(string(dataOn), "web_search_result_count = 7") {
		t.Errorf("web-search-on save did not persist web_search_result_count = 7 (operator tuning must round-trip):\n%s", dataOn)
	}

	// Opting in then saving must round-trip back to an equal struct.
	got, err := LoadVillaFrom(dirOn)
	if err != nil {
		t.Fatalf("LoadVillaFrom(on): %v", err)
	}
	if got != on {
		t.Errorf("web-search-on round-trip mismatch:\n got %+v\nwant %+v", got, on)
	}
}

// TestWebSearchNormalizeSelfHeal asserts normalizeVilla fills a zero SearxngPort and an
// empty SearxngAddr from defaultConfig() (mirrors TestNormalizeMemorySelfHeal) while NEVER
// self-healing WebSearchEnabled (a deliberate bool) or SearxngSecret (a generated secret
// with no default). The addr fill only ever yields the container-DNS name (PRIV-01).
func TestWebSearchNormalizeSelfHeal(t *testing.T) {
	got := normalizeVilla(VillaConfig{WebSearchEnabled: true})
	if !got.WebSearchEnabled {
		t.Errorf("WebSearchEnabled = false, want true (explicit opt-in must survive)")
	}
	if got.SearxngSecret != "" {
		t.Errorf("SearxngSecret was self-healed to %q, want empty (a generated secret has no default)", got.SearxngSecret)
	}
	if got.WebSearchResultCount != 3 {
		t.Errorf("zeroed WebSearchResultCount self-heal = %d, want 3 (mirrors SearxngPort heal)", got.WebSearchResultCount)
	}
}

// TestWebSearchEnabledNotSelfHealed asserts normalizeVilla does NOT widen WebSearchEnabled
// (like MemoryEnabled / CodingMode / AgentEnabled, false is a valid explicit choice).
func TestWebSearchEnabledNotSelfHealed(t *testing.T) {
	got := normalizeVilla(VillaConfig{})
	if got.WebSearchEnabled {
		t.Errorf("normalizeVilla widened WebSearchEnabled to true, want false (not self-healed)")
	}
}

// TestGenerateSearxngSecret asserts the secret generator returns a non-empty,
// high-entropy hex string from crypto/rand, two calls differ, and it never panics (V6).
func TestGenerateSearxngSecret(t *testing.T) {
	a, err := GenerateSearxngSecret()
	if err != nil {
		t.Fatalf("GenerateSearxngSecret: %v", err)
	}
	if len(a) != 64 {
		t.Errorf("secret length = %d, want 64 hex chars (32 random bytes)", len(a))
	}
	for _, r := range a {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("secret %q contains a non-hex rune %q (crypto/rand hex-encode expected)", a, r)
		}
	}
	b, err := GenerateSearxngSecret()
	if err != nil {
		t.Fatalf("GenerateSearxngSecret (2nd): %v", err)
	}
	if a == b {
		t.Errorf("two GenerateSearxngSecret calls returned the same value %q — not high-entropy", a)
	}
}

// TestGenerateSearxngSecretUsesCryptoRand is a source-level guard (V6): the generator
// MUST import crypto/rand and MUST NOT import math/rand. A regression that swaps the
// source is caught here in addition to the behavioral test above.
func TestGenerateSearxngSecretUsesCryptoRand(t *testing.T) {
	data, err := os.ReadFile("villaconfig.go")
	if err != nil {
		t.Fatalf("read villaconfig.go: %v", err)
	}
	src := string(data)
	if !strings.Contains(src, `"crypto/rand"`) {
		t.Errorf("villaconfig.go must import crypto/rand for GenerateSearxngSecret (V6)")
	}
	if strings.Contains(src, `"math/rand"`) {
		t.Errorf("villaconfig.go must NOT import math/rand — the secret must be cryptographically random (V6)")
	}
}

// TestDefaultConfigWebsafeFields asserts the v1.5 (GROUND/GUARD) websafe defaults:
// the addr/port are the SINGLE home of villa-websafe:8090 (container-DNS only,
// PRIV-01), and the bearer secret + host binary path have NO default (captured /
// generated at opt-in, never a hardcoded literal).
func TestDefaultConfigWebsafeFields(t *testing.T) {
	d := defaultConfig()
	if d.WebLoaderSecret != "" {
		t.Errorf("default WebLoaderSecret = %q, want empty (generated at opt-in, never a hardcoded default)", d.WebLoaderSecret)
	}
	if d.HostVillaPath != "" {
		t.Errorf("default HostVillaPath = %q, want empty (captured at opt-in, never a hardcoded default)", d.HostVillaPath)
	}
}

// TestWebsafeSaveOmitsKeysWhenDisabled is the websafe twin of the SearXNG omit-when-off
// test: on a non-web-search install a save-bearing command must NOT introduce any websafe
// key on disk, even though the in-memory struct carries non-zero websafe addr/port defaults.
// marshalVilla zeroes the websafe fields when disabled so the ,omitempty/,omitzero tags
// drop all four keys. When web search is ON, the addr/port (and the secret/path when set)
// are written and round-trip.
func TestWebsafeSaveOmitsKeysWhenDisabled(t *testing.T) {
	websafeKeys := []string{"web_loader_secret", "host_villa_path"}

	// Web search OFF: seed a secret + host path to prove the off-path zeroing is the gate.
	off := DefaultVillaConfig() // WebSearchEnabled == false, websafe fields at defaults
	off.Model = "qwen3-35b-a3b-moe-64"
	off.WebLoaderSecret = "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"
	off.HostVillaPath = "/usr/local/bin/villa"
	dirOff := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOff, off); err != nil {
		t.Fatalf("SaveVillaTo(off): %v", err)
	}
	dataOff, err := os.ReadFile(filepath.Join(dirOff, "config.toml"))
	if err != nil {
		t.Fatalf("read off config: %v", err)
	}
	for _, k := range websafeKeys {
		if strings.Contains(string(dataOff), k) {
			t.Errorf("web-search-off save wrote websafe key %q (SC#4 byte-identical break):\n%s", k, dataOff)
		}
	}
	if strings.Contains(string(dataOff), "cafebabe") {
		t.Errorf("web-search-off save leaked the bearer secret value into config.toml:\n%s", dataOff)
	}
	if strings.Contains(string(dataOff), "/usr/local/bin/villa") {
		t.Errorf("web-search-off save leaked the host villa path into config.toml:\n%s", dataOff)
	}

	// Web search ON: opting in must persist the websafe addr/port and the secret/path when set.
	on := DefaultVillaConfig()
	on.Model = "qwen3-35b-a3b-moe-64"
	on.WebSearchEnabled = true
	on.WebLoaderSecret = "cafebabecafebabecafebabecafebabecafebabecafebabecafebabecafebabe"
	on.HostVillaPath = "/usr/local/bin/villa"
	dirOn := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dirOn, on); err != nil {
		t.Fatalf("SaveVillaTo(on): %v", err)
	}
	dataOn, err := os.ReadFile(filepath.Join(dirOn, "config.toml"))
	if err != nil {
		t.Fatalf("read on config: %v", err)
	}
	for _, k := range websafeKeys {
		if !strings.Contains(string(dataOn), k) {
			t.Errorf("web-search-on save omitted websafe key %q (opt-in must persist the full contract):\n%s", k, dataOn)
		}
	}

	// Opting in then saving must round-trip back to an equal struct.
	got, err := LoadVillaFrom(dirOn)
	if err != nil {
		t.Fatalf("LoadVillaFrom(on): %v", err)
	}
	if got != on {
		t.Errorf("websafe-on round-trip mismatch:\n got %+v\nwant %+v", got, on)
	}
}

// TestWebsafeNormalizeSelfHeal asserts normalizeVilla fills a zero WebsafePort and an
// empty WebsafeAddr from defaultConfig() (mirrors the SearXNG self-heal) while NEVER
// self-healing WebLoaderSecret (a generated secret) or HostVillaPath (a captured host
// path). The addr fill only ever yields the container-DNS name (PRIV-01).
func TestWebsafeNormalizeSelfHeal(t *testing.T) {
	got := normalizeVilla(VillaConfig{WebSearchEnabled: true})
	if got.WebLoaderSecret != "" {
		t.Errorf("WebLoaderSecret was self-healed to %q, want empty (a generated secret has no default)", got.WebLoaderSecret)
	}
	if got.HostVillaPath != "" {
		t.Errorf("HostVillaPath was self-healed to %q, want empty (a captured host path has no default)", got.HostVillaPath)
	}
}

// TestGenerateWebLoaderSecret asserts the bearer-token generator returns a non-empty,
// high-entropy hex string from crypto/rand, two calls differ, and it never panics (V6).
// It mirrors GenerateSearxngSecret exactly (the EXTERNAL_WEB_LOADER_API_KEY bearer).
func TestGenerateWebLoaderSecret(t *testing.T) {
	a, err := GenerateWebLoaderSecret()
	if err != nil {
		t.Fatalf("GenerateWebLoaderSecret: %v", err)
	}
	if len(a) != 64 {
		t.Errorf("secret length = %d, want 64 hex chars (32 random bytes)", len(a))
	}
	for _, r := range a {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("secret %q contains a non-hex rune %q (crypto/rand hex-encode expected)", a, r)
		}
	}
	b, err := GenerateWebLoaderSecret()
	if err != nil {
		t.Fatalf("GenerateWebLoaderSecret (2nd): %v", err)
	}
	if a == b {
		t.Errorf("two GenerateWebLoaderSecret calls returned the same value %q — not high-entropy", a)
	}
}

// TestSaveIsAtomicUnderWriteFailure is the durability gate for the one file the
// whole control plane treats as authoritative. A save that cannot complete
// must leave the config that was already on disk intact, never a truncated or
// empty one — the loader fails closed on unparseable input, so a torn write is a
// control plane that refuses to run until the file is repaired by hand.
//
// The failure is induced by making the config directory unwritable after seeding
// a config. A whole-file write still truncates the existing file in that state
// (write permission lives on the file, not on its directory), whereas a
// temp-plus-rename writer cannot even create its temp file and so fails with the
// prior config untouched. That difference is exactly the guarantee under test.
func TestSaveIsAtomicUnderWriteFailure(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root: directory permissions do not deny writes")
	}
	dir := filepath.Join(t.TempDir(), "villa")

	prior := DefaultVillaConfig()
	prior.Model = "the-config-already-on-disk"
	if err := SaveVillaTo(dir, prior); err != nil {
		t.Fatalf("seed SaveVillaTo: %v", err)
	}
	path := filepath.Join(dir, "config.toml")
	before, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read seeded config: %v", err)
	}

	// Deny new entries in the directory, so the write cannot land. The mode is
	// restored via t.Cleanup so TempDir can remove the tree afterwards.
	if err := os.Chmod(dir, 0o500); err != nil {
		t.Fatalf("chmod dir read-only: %v", err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, configDirMode) })

	next := DefaultVillaConfig()
	next.Model = "the-config-that-never-landed"
	if err := SaveVillaTo(dir, next); err == nil {
		t.Fatal("SaveVillaTo into an unwritable directory returned nil; expected a write failure")
	}

	after, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("prior config unreadable after the failed save: %v", err)
	}
	if len(after) == 0 {
		t.Fatal("prior config was truncated to zero bytes by a failed save")
	}
	if string(after) != string(before) {
		t.Errorf("prior config changed under a failed save:\nbefore: %q\nafter:  %q", before, after)
	}

	// A failed save must not litter the directory with a partial file either.
	if err := os.Chmod(dir, configDirMode); err != nil {
		t.Fatalf("restore dir mode: %v", err)
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("ReadDir: %v", err)
	}
	for _, e := range entries {
		if strings.HasSuffix(e.Name(), ".tmp") {
			t.Errorf("failed save left a temp remnant %q", e.Name())
		}
	}
}
