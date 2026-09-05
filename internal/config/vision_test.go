package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestVisionRoundTrip asserts the persisted vision decision survives a save/load
// cycle, and that false writes no key at all: a v1.8 install must gain no vision
// line on disk until an install or `recommend --save` resolves a projector.
func TestVisionRoundTrip(t *testing.T) {
	for _, want := range []bool{false, true} {
		cfg := DefaultVillaConfig()
		cfg.Model = "qwen3.6-35b-a3b"
		cfg.Vision = want
		dir := filepath.Join(t.TempDir(), "villa")
		if err := SaveVillaTo(dir, cfg); err != nil {
			t.Fatalf("SaveVillaTo: %v", err)
		}
		body, err := os.ReadFile(filepath.Join(dir, "config.toml"))
		if err != nil {
			t.Fatalf("read config.toml: %v", err)
		}
		if got := strings.Contains(string(body), "vision"); got != want {
			t.Errorf("vision=%v wrote a vision key: %v, want %v", want, got, want)
		}
		got, err := LoadVillaFrom(dir)
		if err != nil {
			t.Fatalf("LoadVillaFrom: %v", err)
		}
		if got.Vision != want {
			t.Errorf("vision round-trip: got %v, want %v", got.Vision, want)
		}
	}
}

// TestVisionAbsentKeyLoadsFalse asserts a config.toml written before this field
// existed loads with vision off, which is what keeps its rendered unit unchanged.
func TestVisionAbsentKeyLoadsFalse(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "villa")
	if err := os.MkdirAll(dir, configDirMode); err != nil {
		t.Fatalf("MkdirAll: %v", err)
	}
	body := "model = \"qwen3-35b-a3b-moe-64\"\nbackend = \"rocm\"\n"
	if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), configFileMode); err != nil {
		t.Fatalf("WriteFile: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if got.Vision {
		t.Errorf("absent vision key loaded as true")
	}
}
