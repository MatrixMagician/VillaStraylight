package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestExtractorRoundTripsWithMemoryOn: the extractor gate survives a save and load
// beside the memory fields it belongs to, so a `villa install` that turned the
// extractor on is still rendering it after the next load (ADR-0029).
func TestExtractorRoundTripsWithMemoryOn(t *testing.T) {
	cfg := DefaultVillaConfig()
	cfg.MemoryEnabled = true
	cfg.Extractor = true

	dir := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dir, cfg); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if !strings.Contains(string(body), "extractor = true") {
		t.Errorf("extractor = true was not written for a memory-on config:\n%s", body)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if !got.Extractor {
		t.Errorf("Extractor = false after a round trip of a config that set it")
	}
}

// TestExtractorIsOmittedWhenMemoryOff: the extractor is part of the memory stack,
// so a memory-off save drops the key the way it drops the reranker's, and an
// install that never opted into memory gains no key on disk.
func TestExtractorIsOmittedWhenMemoryOff(t *testing.T) {
	cfg := DefaultVillaConfig()
	cfg.Extractor = true

	dir := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dir, cfg); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	body, err := os.ReadFile(filepath.Join(dir, "config.toml"))
	if err != nil {
		t.Fatalf("read config.toml: %v", err)
	}
	if strings.Contains(string(body), "extractor") {
		t.Errorf("a memory-off save wrote the extractor key:\n%s", body)
	}
}
