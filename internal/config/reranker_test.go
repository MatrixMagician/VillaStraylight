package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestRerankerRoundTripsWithMemoryOn: the reranker gate survives a save and load
// beside the memory fields it belongs to, so a `villa install` that staged the
// reranker is still rendering it after the next load.
func TestRerankerRoundTripsWithMemoryOn(t *testing.T) {
	cfg := DefaultVillaConfig()
	cfg.MemoryEnabled = true
	cfg.Reranker = true

	dir := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dir, cfg); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(body), "reranker = true") {
		t.Errorf("reranker = true was not written for a memory-on config:\n%s", body)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if !got.Reranker {
		t.Errorf("Reranker = false after a round trip of a config that set it")
	}
}

// TestRerankerIsOmittedWhenMemoryOff: the reranker is part of the memory stack,
// so a memory-off save drops the key the way it drops embedding_model, and an
// install that never opted into memory gains no key on disk.
func TestRerankerIsOmittedWhenMemoryOff(t *testing.T) {
	cfg := DefaultVillaConfig()
	cfg.Reranker = true

	dir := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dir, cfg); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if strings.Contains(string(body), "reranker") {
		t.Errorf("a memory-off save wrote the reranker key:\n%s", body)
	}
}
