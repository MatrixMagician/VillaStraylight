package stackapply

import (
	"errors"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// toolHost is a fake host that renders and reconciles for real, so an apply's idempotency
// and removals are observed on the units themselves: the unit dir and the tools config dir
// are temp directories, and the seams that would touch the live host record their calls.
type toolHost struct {
	t        *testing.T
	unitDir  string
	toolsDir string
	scopeDir string

	calls       []string
	saved       []config.VillaConfig
	scopeWrites int
	scopeErr    error
	active      map[string]bool
	stopped     []string
}

func newToolHost(t *testing.T) *toolHost {
	root := t.TempDir()
	if err := os.MkdirAll(filepath.Join(root, "units"), 0o755); err != nil {
		t.Fatal(err)
	}
	return &toolHost{t: t, unitDir: filepath.Join(root, "units"), toolsDir: filepath.Join(root, "tools"), scopeDir: filepath.Join(root, "scope")}
}

func (h *toolHost) deps() Deps {
	return Deps{
		Catalog:        func() (catalog.Catalog, error) { return testCatalog, nil },
		ModelsDir:      func() string { return "/models-dir" },
		HostVillaPath:  func() string { return "/bin/villa" },
		RecallScopeDir: func() string { return h.scopeDir },
		Render:         orchestrate.Render,
		UnitDir:        func() (string, error) { return h.unitDir, nil },
		Reconcile:      orchestrate.Reconcile,
		WriteUnits: func(p orchestrate.Plan, dir string) error {
			h.calls = append(h.calls, "write-units")
			return orchestrate.WriteUnits(p, dir)
		},
		RemoveUnits:  orchestrate.RemoveUnits,
		DaemonReload: func() error { h.calls = append(h.calls, "reload"); return nil },
		IsActive: func(svc string) (string, error) {
			if h.active[svc] {
				return "active", nil
			}
			return "inactive", nil
		},
		Stop: func(svc string) error { h.stopped = append(h.stopped, svc); return nil },
		SaveConfig: func(c config.VillaConfig) error {
			h.calls = append(h.calls, "save")
			h.saved = append(h.saved, c)
			return nil
		},
		WriteInferenceSecretEnv: func(string, string) error { return nil },
		WriteToolEnv: func(name, text string) error {
			h.calls = append(h.calls, "env:"+name)
			return orchestrate.WriteToolEnvFileTo(h.toolsDir, name, text)
		},
		WriteRecallScope: func() error {
			h.calls = append(h.calls, "scope")
			h.scopeWrites++
			return h.scopeErr
		},
	}
}

// toolCfg is a stack with memory and web search on and the tool server on, with no
// bearer yet, the state `villa install --tool-server` or a hand edit leaves behind.
func toolCfg() config.VillaConfig {
	return config.VillaConfig{
		Model: "chat", Quant: "q", Ctx: 8192, Backend: "vulkan",
		MemoryEnabled: true, EmbeddingModel: "nomic-embed-text-v1.5", EmbeddingDim: 768,
		WebSearchEnabled: true,
		ToolServer:       true,
		InferenceSecret:  "inference-secret",
	}
}

func (h *toolHost) read(name string) string {
	h.t.Helper()
	b, err := os.ReadFile(filepath.Join(h.toolsDir, name))
	if err != nil {
		h.t.Fatalf("read %s: %v", name, err)
	}
	return string(b)
}

var hex64 = regexp.MustCompile(`^[0-9a-f]{64}$`)

// TestHealGeneratesToolSecrets: a tool server with no bearers gets two distinct ones,
// persisted in ONE save, before anything renders, and the env files and the scope are
// written from them. An existing bearer is reused, never rotated (ADR-0042).
func TestHealGeneratesToolSecrets(t *testing.T) {
	h := newToolHost(t)
	got, err := heal(h.deps(), toolCfg())
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if !hex64.MatchString(got.ToolWebSecret) || !hex64.MatchString(got.ToolRecallSecret) || got.ToolWebSecret == got.ToolRecallSecret {
		t.Errorf("secrets = %q, %q; want two distinct 64-hex bearers", got.ToolWebSecret, got.ToolRecallSecret)
	}
	if n := countOf(h.calls, "save"); n != 1 {
		t.Errorf("config saved %d times, want once; calls = %v", n, h.calls)
	}
	if h.saved[0].ToolWebSecret != got.ToolWebSecret {
		t.Error("the saved config does not carry the generated bearer")
	}
	if !strings.Contains(h.read("web.env"), got.ToolWebSecret) || !strings.Contains(h.read("recall.env"), got.ToolRecallSecret) {
		t.Error("an env file does not carry its bearer")
	}
	if strings.Contains(h.read("web.env"), got.ToolRecallSecret) || strings.Contains(h.read("recall.env"), got.ToolWebSecret) {
		t.Error("a unit's env file carries the other unit's bearer")
	}
	if !strings.Contains(h.read("openwebui.env"), got.ToolWebSecret) || !strings.Contains(h.read("openwebui.env"), got.ToolRecallSecret) {
		t.Error("the connection file does not carry both bearers")
	}
	if h.scopeWrites != 1 {
		t.Errorf("scope written %d times, want 1", h.scopeWrites)
	}

	// An existing bearer is kept, a missing one is added, with one save.
	h2 := newToolHost(t)
	cfg := toolCfg()
	cfg.ToolWebSecret = "kept-web-secret"
	got2, err := heal(h2.deps(), cfg)
	if err != nil {
		t.Fatalf("heal (partial): %v", err)
	}
	if got2.ToolWebSecret != "kept-web-secret" || !hex64.MatchString(got2.ToolRecallSecret) {
		t.Errorf("partial heal = %q / %q", got2.ToolWebSecret, got2.ToolRecallSecret)
	}
}

// TestHealWritesOnlyTheFilesTheRenderedUnitsRead: web search off leaves no web unit, so
// no web.env and no web connection, and the recall unit still gets its own file and the
// scope; memory off is the mirror, with no scope write at all.
func TestHealWritesOnlyTheFilesTheRenderedUnitsRead(t *testing.T) {
	h := newToolHost(t)
	cfg := toolCfg()
	cfg.WebSearchEnabled = false
	if _, err := heal(h.deps(), cfg); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.toolsDir, "web.env")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("web.env was written with web search off: %v", err)
	}
	if strings.Contains(h.read("openwebui.env"), "villa-tools-web") {
		t.Error("the connection file registers the web unit with web search off")
	}
	if h.scopeWrites != 1 {
		t.Errorf("scope writes = %d, want 1 for the recall unit", h.scopeWrites)
	}

	h = newToolHost(t)
	cfg = toolCfg()
	cfg.MemoryEnabled = false
	if _, err := heal(h.deps(), cfg); err != nil {
		t.Fatalf("heal: %v", err)
	}
	if _, err := os.Stat(filepath.Join(h.toolsDir, "recall.env")); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("recall.env was written with memory off: %v", err)
	}
	if h.scopeWrites != 0 {
		t.Errorf("the scope was written %d times with no recall unit", h.scopeWrites)
	}
}

// TestHealLeavesAGateOffInstallAlone: with the gate off nothing is generated, saved or
// written, so an off install is byte-identical on disk.
func TestHealLeavesAGateOffInstallAlone(t *testing.T) {
	h := newToolHost(t)
	cfg := toolCfg()
	cfg.ToolServer = false
	got, err := heal(h.deps(), cfg)
	if err != nil {
		t.Fatalf("heal: %v", err)
	}
	if got.ToolWebSecret != "" || got.ToolRecallSecret != "" || len(h.saved) != 0 {
		t.Errorf("a gate-off heal generated or saved a tool secret: %+v saved %d", got, len(h.saved))
	}
	if _, err := os.Stat(h.toolsDir); !errors.Is(err, os.ErrNotExist) || h.scopeWrites != 0 {
		t.Errorf("a gate-off heal wrote tool files: %v, scope writes %d", err, h.scopeWrites)
	}
}

// TestHealRefusesWhenAToolFileCannotBeWritten: the chat unit names openwebui.env, so a
// failed write stops the apply before any unit is written, rather than starting a chat
// that cannot read its environment.
func TestHealRefusesWhenAToolFileCannotBeWritten(t *testing.T) {
	h := newToolHost(t)
	d := h.deps()
	d.WriteToolEnv = func(string, string) error { return errors.New("read-only filesystem") }
	if _, err := Apply(d, toolCfg()); err == nil || !strings.Contains(err.Error(), "read-only filesystem") {
		t.Fatalf("Apply = %v, want the write failure", err)
	}
	if slices.Contains(h.calls, "write-units") {
		t.Error("units were written after the tool env write failed")
	}

	h = newToolHost(t)
	h.scopeErr = errors.New("scope: no space left")
	if _, err := Apply(h.deps(), toolCfg()); err == nil || !strings.Contains(err.Error(), "no space left") {
		t.Fatalf("Apply = %v, want the scope failure", err)
	}
}

func countOf(calls []string, want string) int {
	n := 0
	for _, c := range calls {
		if c == want {
			n++
		}
	}
	return n
}

// TestStackApplyToolServerIsIdempotent: the first apply writes both tool units, the chat
// unit's connection file and the env files; the second, from the config the first
// persisted, writes no unit, reloads nothing and leaves every file byte-identical
// (ADR-0042: "a villa up on a fake host registers the connection once").
func TestStackApplyToolServerIsIdempotent(t *testing.T) {
	h := newToolHost(t)
	first, err := Apply(h.deps(), toolCfg())
	if err != nil {
		t.Fatalf("first Apply: %v", err)
	}
	var names []string
	for _, u := range first.Changed {
		names = append(names, u.Name)
	}
	for _, want := range []string{"villa-tools-web.container", "villa-tools-recall.container", "villa-openwebui.container"} {
		if !slices.Contains(names, want) {
			t.Errorf("first apply did not write %s; wrote %v", want, names)
		}
	}
	files := func() map[string]string {
		out := map[string]string{}
		for _, n := range []string{"web.env", "recall.env", "openwebui.env"} {
			out[n] = h.read(n)
		}
		return out
	}
	before := files()
	persisted := h.saved[len(h.saved)-1]
	h.calls, h.saved = nil, nil

	second, err := Apply(h.deps(), persisted)
	if err != nil {
		t.Fatalf("second Apply: %v", err)
	}
	if !second.Empty() {
		t.Errorf("second apply changed %v and removed %v, want nothing", second.Changed, second.Removed)
	}
	if len(h.saved) != 0 {
		t.Errorf("second apply saved the config %d times; the secrets must be reused", len(h.saved))
	}
	if slices.Contains(h.calls, "write-units") || slices.Contains(h.calls, "reload") {
		t.Errorf("second apply wrote units or reloaded: %v", h.calls)
	}
	after := files()
	for n, text := range before {
		if after[n] != text {
			t.Errorf("%s changed between applies", n)
		}
	}
}

// TestApplyGateOffRemovesTheToolUnitsAndKeepsTheEnvFiles: turning the gate off stops and
// removes both units through the registry (ADR-0035) and rewrites the chat unit without
// the connection file. The files under tools/ stay, with keys no unit accepts any more.
func TestApplyGateOffRemovesTheToolUnitsAndKeepsTheEnvFiles(t *testing.T) {
	h := newToolHost(t)
	if _, err := Apply(h.deps(), toolCfg()); err != nil {
		t.Fatalf("Apply on: %v", err)
	}
	h.active = map[string]bool{"villa-tools-web.service": true, "villa-tools-recall.service": true}

	off := h.saved[len(h.saved)-1]
	off.ToolServer = false
	applied, err := Apply(h.deps(), off)
	if err != nil {
		t.Fatalf("Apply off: %v", err)
	}
	var removed, changed []string
	for _, u := range applied.Removed {
		removed = append(removed, u.Name)
	}
	for _, u := range applied.Changed {
		changed = append(changed, u.Name)
	}
	if want := []string{"villa-tools-web.container", "villa-tools-recall.container"}; !slices.Equal(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	if want := []string{"villa-openwebui.container"}; !slices.Equal(changed, want) {
		t.Errorf("changed = %v, want only the chat unit", changed)
	}
	if !slices.Equal(applied.Stopped, []string{"villa-tools-web.service", "villa-tools-recall.service"}) {
		t.Errorf("stopped = %v, want both running tool services", applied.Stopped)
	}
	for _, n := range []string{"web.env", "recall.env", "openwebui.env"} {
		if _, err := os.Stat(filepath.Join(h.toolsDir, n)); err != nil {
			t.Errorf("%s was removed with the gate; it stays: %v", n, err)
		}
	}
	for _, name := range []string{"villa-tools-web.container", "villa-tools-recall.container"} {
		if _, err := os.Stat(filepath.Join(h.unitDir, name)); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("%s is still on disk: %v", name, err)
		}
	}
}

// TestRenderHandsTheRecallScopeDirToTheRenderer: the scope directory the unit mounts is
// derived once, in the one input builder, from Deps.
func TestRenderHandsTheRecallScopeDirToTheRenderer(t *testing.T) {
	h := &host{}
	d := h.deps()
	d.RecallScopeDir = func() string { return "/data/recall-scope" }
	if _, err := Render(d, config.VillaConfig{Model: "chat", Ctx: 8192, Backend: "vulkan"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if h.input.RecallScopeDir != "/data/recall-scope" {
		t.Errorf("RecallScopeDir = %q, want the Deps value", h.input.RecallScopeDir)
	}
}
