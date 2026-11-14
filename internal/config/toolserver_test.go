package config

import (
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

// TestToolServerOffSaveWritesNoToolKeys: tool_server is the gate, and the two bearers
// are zeroed by marshalVilla while it is off, so an off install is byte-identical on
// disk even if a stale secret is still in memory (ADR-0042).
func TestToolServerOffSaveWritesNoToolKeys(t *testing.T) {
	off := DefaultVillaConfig()
	off.Model = "qwen3-35b-a3b-moe-64"
	off.ToolWebSecret = "stale-web"
	off.ToolRecallSecret = "stale-recall"
	data, err := marshalVilla(off)
	if err != nil {
		t.Fatalf("marshalVilla: %v", err)
	}
	for _, k := range []string{"tool_server", "tool_web_secret", "tool_recall_secret", "stale-"} {
		if strings.Contains(string(data), k) {
			t.Errorf("tool-server-off save wrote %q:\n%s", k, data)
		}
	}
}

// TestToolServerOnRoundTrips: the gate and both bearers survive a save and load, and
// the loader does not self-heal either secret (a generated secret has no default).
func TestToolServerOnRoundTrips(t *testing.T) {
	on := DefaultVillaConfig()
	on.Model = "qwen3-35b-a3b-moe-64"
	on.ToolServer = true
	on.ToolWebSecret = "web-secret"
	on.ToolRecallSecret = "recall-secret"
	dir := filepath.Join(t.TempDir(), "villa")
	if err := SaveVillaTo(dir, on); err != nil {
		t.Fatalf("SaveVillaTo: %v", err)
	}
	got, err := LoadVillaFrom(dir)
	if err != nil {
		t.Fatalf("LoadVillaFrom: %v", err)
	}
	if !reflect.DeepEqual(got, on) {
		t.Errorf("tool-server round-trip mismatch:\n got %+v\nwant %+v", got, on)
	}
	healed := normalizeVilla(VillaConfig{ToolServer: true})
	if healed.ToolWebSecret != "" || healed.ToolRecallSecret != "" {
		t.Errorf("normalizeVilla self-healed a tool secret: %+v", healed)
	}
	if normalizeVilla(VillaConfig{}).ToolServer {
		t.Error("normalizeVilla widened ToolServer to true")
	}
}

// TestGenerateToolSecret: the same crypto/rand, 64 hex char generator as the other
// bearers, and two calls differ so the two units never share a key by accident.
func TestGenerateToolSecret(t *testing.T) {
	a, err := GenerateToolSecret()
	if err != nil {
		t.Fatalf("GenerateToolSecret: %v", err)
	}
	if len(a) != 64 {
		t.Errorf("secret length = %d, want 64 hex chars", len(a))
	}
	for _, r := range a {
		if !strings.ContainsRune("0123456789abcdef", r) {
			t.Errorf("secret %q has a non-hex rune %q", a, r)
		}
	}
	b, err := GenerateToolSecret()
	if err != nil {
		t.Fatalf("GenerateToolSecret (2nd): %v", err)
	}
	if a == b {
		t.Error("two GenerateToolSecret calls returned the same value")
	}
}

// TestToolServerAddresses pins the container-DNS names and ports the units, the Open
// WebUI connection and the probes all read from one place.
func TestToolServerAddresses(t *testing.T) {
	if ToolsWebAddr != "villa-tools-web" || ToolsWebPort != 8092 {
		t.Errorf("web unit = %s:%d, want villa-tools-web:8092", ToolsWebAddr, ToolsWebPort)
	}
	if ToolsRecallAddr != "villa-tools-recall" || ToolsRecallPort != 8093 {
		t.Errorf("recall unit = %s:%d, want villa-tools-recall:8093", ToolsRecallAddr, ToolsRecallPort)
	}
}
