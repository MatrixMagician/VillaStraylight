package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRealWorldConfigSurvivesSaveRoundTrip is the regression for the nine removed
// config keys, taken from a REAL opted-in install rather than a constructed
// fixture: memory, coding agent and web search all on, and the two generated
// secrets present.
//
// Those secrets are why this matters more than a normal compatibility test. The
// SearXNG secret_key and the web-loader bearer are generated ONCE by crypto/rand
// and persisted; nothing can re-derive them. If a save-bearing command (config set,
// model swap, backend set, coding-mode enter/exit, install, restore) dropped one
// while rewriting the file, the failure would be silent at write time and only
// surface later as broken sessions or a bearer mismatch between Open WebUI and
// villa-websafe.
//
// The literal below is a real config.toml carrying all nine removed keys.
func TestRealWorldConfigSurvivesSaveRoundTrip(t *testing.T) {
	const onDisk = `model = "qwen3.6-35b-a3b"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "rocm"
catalog_path = ""
dashboard_addr = "127.0.0.1"
dashboard_port = 8888
chat_port = 3000
memory_enabled = true
embedding_model = "nomic-embed-text-v1.5"
embedding_dim = 768
qdrant_addr = "villa-qdrant"
qdrant_port = 6333
embed_addr = "villa-embed"
embed_port = 8080
agent_enabled = true
web_search_enabled = true
searxng_addr = "villa-searxng"
searxng_port = 8080
searxng_secret = "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"
web_search_result_count = 3
websafe_addr = "villa-websafe"
websafe_port = 8090
web_loader_secret = "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210"
`
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(onDisk), 0o600); err != nil {
		t.Fatal(err)
	}

	cfg, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("the real on-disk config failed to load: %v", err)
	}

	// The opt-in state and BOTH generated secrets must survive the load.
	if !cfg.MemoryEnabled || !cfg.AgentEnabled || !cfg.WebSearchEnabled {
		t.Errorf("opt-in state lost: memory=%v agent=%v websearch=%v", cfg.MemoryEnabled, cfg.AgentEnabled, cfg.WebSearchEnabled)
	}
	if cfg.SearxngSecret != "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef" {
		t.Errorf("SearXNG secret lost or altered: %q", cfg.SearxngSecret)
	}
	if cfg.WebLoaderSecret != "fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210" {
		t.Errorf("web-loader bearer lost or altered: %q", cfg.WebLoaderSecret)
	}

	// Now the dangerous half: a save-bearing command (config set / model swap /
	// backend set) rewrites the file. Both secrets must still be there afterwards.
	out := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(out, cfg); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	saved, err := os.ReadFile(filepath.Join(out, "config.toml"))
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
		"fedcba9876543210fedcba9876543210fedcba9876543210fedcba9876543210",
	} {
		if !strings.Contains(string(saved), secret) {
			t.Errorf("a save DROPPED an unrecoverable generated secret:\n%s", saved)
		}
	}

	// And the reload must be equal, so a second save is stable.
	back, err := LoadVillaFrom(out)
	if err != nil {
		t.Fatalf("reload after save: %v", err)
	}
	if back != cfg {
		t.Errorf("save→load is not identity:\n got %+v\nwant %+v", back, cfg)
	}
}
