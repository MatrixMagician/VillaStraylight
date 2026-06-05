package main

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// fakeConfigDeps stubs the config show/set seams so the verbs are exercisable
// without touching the user's real XDG config. saved captures the last write.
type fakeConfigDeps struct {
	*configDeps
	loaded    config.VillaConfig
	saved     *config.VillaConfig
	saveCalls int
	loadErr   error
	saveErr   error
}

func newFakeConfigDeps(loaded config.VillaConfig) *fakeConfigDeps {
	f := &fakeConfigDeps{loaded: loaded}
	f.configDeps = &configDeps{
		load: func() (config.VillaConfig, error) { return f.loaded, f.loadErr },
		save: func(c config.VillaConfig) error {
			f.saveCalls++
			if f.saveErr != nil {
				return f.saveErr
			}
			cp := c
			f.saved = &cp
			return nil
		},
		path: func() (string, error) { return "/tmp/villa/config.toml", nil },
	}
	return f
}

func fixtureConfig() config.VillaConfig {
	return config.VillaConfig{Model: "qwen2.5-0.5b", Quant: "UD-Q4_K_M", Ctx: 4096, Backend: "vulkan"}
}

// TestConfigShowPrintsEffectiveConfig: `config show` prints the loaded config in a
// readable table.
func TestConfigShowPrintsEffectiveConfig(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, out, _ := lifecycleTestCmd()
	code := runConfigShow(cmd, false, f.configDeps)
	if code != exitPass {
		t.Fatalf("config show exit = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, "qwen2.5-0.5b") || !strings.Contains(s, "4096") || !strings.Contains(s, "vulkan") {
		t.Errorf("config show must print the effective config values, got %q", s)
	}
}

// TestConfigShowJSON: `config show --json` emits valid JSON containing the model.
func TestConfigShowJSON(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, out, _ := lifecycleTestCmd()
	code := runConfigShow(cmd, true, f.configDeps)
	if code != exitPass {
		t.Fatalf("config show --json exit = %d, want 0", code)
	}
	s := out.String()
	if !strings.Contains(s, `"model"`) || !strings.Contains(s, "qwen2.5-0.5b") {
		t.Errorf("config show --json must emit the model field, got %q", s)
	}
}

// TestConfigSetValidKeyWritesViaSave: `config set model=<id>` updates the field,
// writes via the save seam exactly once, and notes it applies on next up/restart.
func TestConfigSetValidKeyWritesViaSave(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, out, _ := lifecycleTestCmd()
	code := runConfigSet(cmd, "model=llama-3.1-8b", f.configDeps)
	if code != exitPass {
		t.Fatalf("config set exit = %d, want 0", code)
	}
	if f.saveCalls != 1 {
		t.Errorf("config set should save exactly once, saved %d times", f.saveCalls)
	}
	if f.saved == nil || f.saved.Model != "llama-3.1-8b" {
		t.Errorf("config set should persist model=llama-3.1-8b, saved=%+v", f.saved)
	}
	// Other fields must be preserved.
	if f.saved.Ctx != 4096 || f.saved.Backend != "vulkan" {
		t.Errorf("config set must preserve untouched fields, saved=%+v", f.saved)
	}
	if !strings.Contains(strings.ToLower(out.String()), "up") && !strings.Contains(strings.ToLower(out.String()), "restart") {
		t.Errorf("config set should note it applies on next up/restart, got %q", out.String())
	}
}

// TestConfigSetCtxParsesInt: `config set ctx=8192` parses the int field.
func TestConfigSetCtxParsesInt(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, _ := lifecycleTestCmd()
	code := runConfigSet(cmd, "ctx=8192", f.configDeps)
	if code != exitPass {
		t.Fatalf("config set ctx exit = %d, want 0", code)
	}
	if f.saved == nil || f.saved.Ctx != 8192 {
		t.Errorf("config set ctx=8192 should persist Ctx=8192, saved=%+v", f.saved)
	}
}

// TestConfigSetBackendValid: `config set backend=vulkan` is accepted (the only
// supported v1 backend) and persisted (WR-01).
func TestConfigSetBackendValid(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, _ := lifecycleTestCmd()
	code := runConfigSet(cmd, "backend=vulkan", f.configDeps)
	if code != exitPass {
		t.Fatalf("config set backend=vulkan exit = %d, want 0", code)
	}
	if f.saved == nil || f.saved.Backend != "vulkan" {
		t.Errorf("config set backend=vulkan should persist Backend=vulkan, saved=%+v", f.saved)
	}
}

// TestConfigSetBackendUnknownBlocks: `config set backend=rocm` exits 1 and never
// writes — the key must not lie by persisting a backend no render path honors (WR-01).
func TestConfigSetBackendUnknownBlocks(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, errOut := lifecycleTestCmd()
	code := runConfigSet(cmd, "backend=rocm", f.configDeps)
	if code != exitBlocked {
		t.Fatalf("config set backend=rocm exit = %d, want 1 (unsupported backend)", code)
	}
	if f.saveCalls != 0 {
		t.Errorf("unsupported backend must not write, saved %d times", f.saveCalls)
	}
	if !strings.Contains(errOut.String(), "rocm") || !strings.Contains(strings.ToLower(errOut.String()), "backend") {
		t.Errorf("unsupported backend should name the bad value, got %q", errOut.String())
	}
}

// TestConfigSetUnknownKeyBlocks: `config set bogus=1` exits 1 and never writes.
func TestConfigSetUnknownKeyBlocks(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, errOut := lifecycleTestCmd()
	code := runConfigSet(cmd, "bogus=1", f.configDeps)
	if code != exitBlocked {
		t.Fatalf("config set unknown-key exit = %d, want 1", code)
	}
	if f.saveCalls != 0 {
		t.Errorf("unknown key must not write, saved %d times", f.saveCalls)
	}
	if !strings.Contains(errOut.String(), "bogus") {
		t.Errorf("unknown key should name the bad key, got %q", errOut.String())
	}
}

// TestConfigSetBadCtxBlocks: a non-integer ctx value exits 1 and never writes.
func TestConfigSetBadCtxBlocks(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, _ := lifecycleTestCmd()
	code := runConfigSet(cmd, "ctx=lots", f.configDeps)
	if code != exitBlocked {
		t.Fatalf("config set ctx=lots exit = %d, want 1", code)
	}
	if f.saveCalls != 0 {
		t.Errorf("invalid ctx must not write, saved %d times", f.saveCalls)
	}
}

// TestConfigSetMalformedArgBlocks: an arg without `=` exits 1.
func TestConfigSetMalformedArgBlocks(t *testing.T) {
	f := newFakeConfigDeps(fixtureConfig())
	cmd, _, _ := lifecycleTestCmd()
	code := runConfigSet(cmd, "modelfoo", f.configDeps)
	if code != exitBlocked {
		t.Fatalf("malformed set arg exit = %d, want 1", code)
	}
	if f.saveCalls != 0 {
		t.Errorf("malformed arg must not write, saved %d times", f.saveCalls)
	}
}

// TestConfigRegistered: the `config` noun + show/set subcommands are registered.
func TestConfigRegistered(t *testing.T) {
	root := newRoot()
	c, _, err := root.Find([]string{"config"})
	if err != nil || c.Name() != "config" {
		t.Fatalf("`config` verb not registered: %v", err)
	}
	subs := map[string]bool{}
	for _, s := range c.Commands() {
		subs[s.Name()] = true
	}
	if !subs["show"] || !subs["set"] {
		t.Errorf("config noun must have show + set subcommands, got %v", subs)
	}
}
