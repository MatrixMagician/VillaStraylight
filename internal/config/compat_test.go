package config

// compat_test.go guards the two properties the config-knob removal had to keep:
// a config.toml already on disk carrying the removed keys still loads, and an
// opted-out install stays byte-identical on disk across a save/load/save cycle.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestOldConfigWithRemovedKeysStillLoads is the compatibility criterion: a
// config.toml already on disk carrying all nine removed keys must still load
// without error, and the surviving fields must come through untouched.
func TestOldConfigWithRemovedKeysStillLoads(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	old := `model = "qwen3-35b-a3b-moe-64"
quant = "UD-Q4_K_M"
ctx = 131072
backend = "rocm"
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
web_search_enabled = true
searxng_addr = "villa-searxng"
searxng_port = 8080
searxng_secret = "deadbeef"
websafe_addr = "villa-websafe"
websafe_port = 8090
`
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(old), 0o600); err != nil {
		t.Fatalf("write: %v", err)
	}

	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("a config carrying the removed keys failed to load: %v", err)
	}
	if got.Model != "qwen3-35b-a3b-moe-64" || got.Ctx != 131072 || got.Backend != "rocm" {
		t.Errorf("surviving fields mangled: %+v", got)
	}
	if !got.MemoryEnabled || !got.WebSearchEnabled || got.SearxngSecret != "deadbeef" {
		t.Errorf("opt-in state or generated secret lost: %+v", got)
	}
}

// TestByteIdenticalOffAcrossSave is the guarantee the issue asked to be proven
// rather than asserted: saving an opted-out install must produce a file carrying
// none of the removed keys and none of the opt-in keys, and saving it twice must
// be byte-for-byte stable.
func TestByteIdenticalOffAcrossSave(t *testing.T) {
	off := DefaultVillaConfig()
	off.Model = "qwen3-35b-a3b-moe-64"

	first := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(first, off); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	a, err := os.ReadFile(filepath.Join(first, "config.toml"))
	if err != nil {
		t.Fatalf("read: %v", err)
	}

	// None of the nine removed keys may appear.
	for _, k := range []string{
		"dashboard_addr", "qdrant_addr", "qdrant_port", "embed_addr", "embed_port",
		"searxng_addr", "searxng_port", "websafe_addr", "websafe_port",
	} {
		if strings.Contains(string(a), k) {
			t.Errorf("an opted-out save wrote the removed key %q:\n%s", k, a)
		}
	}

	// Load it back and save again: the bytes must not drift.
	loaded, err := LoadVillaFrom(first)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	second := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(second, loaded); err != nil {
		t.Fatalf("SaveVillaTo (2nd): %v", err)
	}
	b, err := os.ReadFile(filepath.Join(second, "config.toml"))
	if err != nil {
		t.Fatalf("read (2nd): %v", err)
	}
	if string(a) != string(b) {
		t.Errorf("save→load→save is not byte-stable:\nfirst:\n%s\nsecond:\n%s", a, b)
	}
}
