package orchestrate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
)

const (
	toolWebSecretFixture    = "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	toolRecallSecretFixture = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
)

// toolServerFixtureInput is the whole stack with the tool server on: the stateful fixture
// (memory, web search and the gate all on) plus both bearers, the host binary path and
// the recall scope directory the unit mounts.
func toolServerFixtureInput() RenderInput {
	in := statefulFixtureInput()
	in.Cfg.ToolWebSecret = toolWebSecretFixture
	in.Cfg.ToolRecallSecret = toolRecallSecretFixture
	in.HostVillaPath = "/home/villa/.local/bin/villa"
	in.RecallScopeDir = "/home/villa/.local/share/villa/recall-scope"
	return in
}

// toolConnections parses the TOOL_SERVER_CONNECTIONS value out of openwebui.env text.
func toolConnections(t *testing.T, envText string) []map[string]any {
	t.Helper()
	body, ok := strings.CutPrefix(envText, "TOOL_SERVER_CONNECTIONS=")
	if !ok {
		t.Fatalf("openwebui.env does not start with TOOL_SERVER_CONNECTIONS=:\n%s", envText)
	}
	line, rest, _ := strings.Cut(body, "\n")
	if rest != "" {
		t.Fatalf("openwebui.env holds more than one line:\n%s", envText)
	}
	var conns []map[string]any
	if err := json.Unmarshal([]byte(line), &conns); err != nil {
		t.Fatalf("the connection value is not JSON: %v\n%s", err, line)
	}
	return conns
}

func toolEnvFile(t *testing.T, files []ToolEnvFile, name string) string {
	t.Helper()
	for _, f := range files {
		if f.Name == name {
			return f.Text
		}
	}
	t.Fatalf("no %s among %v", name, files)
	return ""
}

// TestRenderToolServerOffIsByteIdentical: with the gate off no tool unit, env line or
// persistent-config change is rendered, whatever stale secrets the config still holds,
// so every existing golden and every existing install is unchanged (ADR-0042).
func TestRenderToolServerOffIsByteIdentical(t *testing.T) {
	for name, mk := range map[string]func() RenderInput{
		"minimal": fixtureInput,
		"memory":  memoryFixtureInput,
		"stateful": func() RenderInput {
			in := statefulFixtureInput()
			in.Cfg.ToolServer = false
			return in
		},
		"voice": voiceFixtureInput,
	} {
		base, err := Render(mk())
		if err != nil {
			t.Fatalf("%s: Render: %v", name, err)
		}
		stale := mk()
		stale.Cfg.ToolWebSecret, stale.Cfg.ToolRecallSecret = toolWebSecretFixture, toolRecallSecretFixture
		stale.RecallScopeDir = "/home/villa/.local/share/villa/recall-scope"
		got, err := Render(stale)
		if err != nil {
			t.Fatalf("%s: Render with stale secrets: %v", name, err)
		}
		if !slices.EqualFunc(base, got, func(a, b Unit) bool { return a == b }) {
			t.Errorf("%s: a gate-off render changed with stale tool fields", name)
		}
		for _, u := range got {
			if strings.HasPrefix(u.Name, "villa-tools-") || strings.Contains(u.Text, "villa/tools/") || strings.Contains(u.Text, "TOOL_SERVER") {
				t.Errorf("%s: gate off but %s carries tool-server wiring:\n%s", name, u.Name, u.Text)
			}
		}
	}
}

// TestRenderToolUnitsGolden freezes both units and the chat unit that registers them.
func TestRenderToolUnitsGolden(t *testing.T) {
	units, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	goldenCompare(t, "villa-tools-web.container.golden", unitByName(t, units, "villa-tools-web.container").Text)
	goldenCompare(t, "villa-tools-recall.container.golden", unitByName(t, units, "villa-tools-recall.container").Text)

	// The chat unit is frozen on the memory fixture, so the golden differs from the
	// memory golden by the one EnvironmentFile line the tool server adds.
	mem := memoryFixtureInput()
	mem.Cfg.ToolServer = true
	memUnits, err := Render(mem)
	if err != nil {
		t.Fatalf("Render (memory): %v", err)
	}
	goldenCompare(t, "villa-openwebui.container.tools.golden", unitByName(t, memUnits, "villa-openwebui.container").Text)
}

// TestRenderToolUnitsOrder: the units render after the voice units and before the
// sandbox network, web first, so no existing unit moves (ADR-0042).
func TestRenderToolUnitsOrder(t *testing.T) {
	units, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	names := unitNames(units)
	web, recall := slices.Index(names, "villa-tools-web.container"), slices.Index(names, "villa-tools-recall.container")
	if web < 0 || recall != web+1 {
		t.Fatalf("tool units at %d, %d in %v; want web then recall, adjacent", web, recall, names)
	}
	if tts := slices.Index(names, "villa-tts.container"); tts != web-1 {
		t.Errorf("the web unit does not follow villa-tts (%d vs %d):\n%v", web, tts, names)
	}
	if sbx := slices.Index(names, "villa-sandbox.network"); sbx != recall+1 {
		t.Errorf("villa-sandbox.network at %d, want %d, straight after the tool units", sbx, recall+1)
	}
}

// TestRenderToolUnitsNetworks: the web unit joins the routed network and nothing else,
// the recall unit joins the closed network and nothing else, and neither bridges the
// two (ADR-0042). The recall unit's one extra mount is the scope directory, read-only.
func TestRenderToolUnitsNetworks(t *testing.T) {
	units, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	web := unitByName(t, units, "villa-tools-web.container")
	recall := unitByName(t, units, "villa-tools-recall.container")
	if got := networkLines(web.Text); !slices.Equal(got, []string{"villa.network"}) {
		t.Errorf("villa-tools-web joins %v, want [villa.network] only", got)
	}
	if !strings.Contains(web.Text, "After=villa-network.service") {
		t.Errorf("villa-tools-web is not ordered after the routed network:\n%s", web.Text)
	}
	if got := networkLines(recall.Text); !slices.Equal(got, []string{"villa-closed.network"}) {
		t.Errorf("villa-tools-recall joins %v, want [villa-closed.network] only", got)
	}
	if !strings.Contains(recall.Text, "After=villa-closed-network.service") || strings.Contains(recall.Text, "After=villa-network.service") {
		t.Errorf("villa-tools-recall is not ordered after the closed network:\n%s", recall.Text)
	}

	wantWeb := []string{"Volume=/home/villa/.local/bin/villa:/usr/local/bin/villa:ro,z"}
	if got := lines(web.Text, "Volume="); !slices.Equal(got, wantWeb) {
		t.Errorf("web volumes = %v, want %v", got, wantWeb)
	}
	wantRecall := []string{
		"Volume=/home/villa/.local/bin/villa:/usr/local/bin/villa:ro,z",
		"Volume=/home/villa/.local/share/villa/recall-scope:/villa/recall-scope:ro,z",
	}
	if got := lines(recall.Text, "Volume="); !slices.Equal(got, wantRecall) {
		t.Errorf("recall volumes = %v, want %v", got, wantRecall)
	}
	for _, u := range []Unit{web, recall} {
		if !strings.Contains(u.Text, "Image="+WebsafeImage()) {
			t.Errorf("%s does not run the websafe base image:\n%s", u.Name, u.Text)
		}
	}
	if !strings.Contains(web.Text, "Exec=/usr/local/bin/villa tools-serve --set web --host 0.0.0.0 --port 8092\n") {
		t.Errorf("web Exec drifted:\n%s", web.Text)
	}
	if !strings.Contains(recall.Text, "Exec=/usr/local/bin/villa tools-serve --set recall --host 0.0.0.0 --port 8093 --embedding-model nomic-embed-text-v1.5\n") {
		t.Errorf("recall Exec drifted:\n%s", recall.Text)
	}
}

// TestToolUnitsPublishNoPort: a tool unit is reached by container DNS only.
func TestToolUnitsPublishNoPort(t *testing.T) {
	units, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, name := range []string{"villa-tools-web.container", "villa-tools-recall.container"} {
		if got := lines(unitByName(t, units, name).Text, "PublishPort="); len(got) != 0 {
			t.Errorf("%s publishes %v; a tool unit has no host port", name, got)
		}
	}
}

// TestToolUnitsFollowTheirDerivedGates: web search off drops the web unit, memory off
// drops the recall unit, and the chat unit registers only what renders.
func TestToolUnitsFollowTheirDerivedGates(t *testing.T) {
	for _, tc := range []struct {
		name    string
		mutate  func(*RenderInput)
		want    []string
		connIDs []string
	}{
		{"no web search", func(in *RenderInput) { in.Cfg.WebSearchEnabled = false }, []string{"villa-tools-recall.container"}, []string{"villa-recall"}},
		{"no memory", func(in *RenderInput) {
			in.Cfg.MemoryEnabled, in.Cfg.Reranker, in.Cfg.Extractor = false, false, false
		}, []string{"villa-tools-web.container"}, []string{"villa-web"}},
	} {
		in := toolServerFixtureInput()
		tc.mutate(&in)
		units, err := Render(in)
		if err != nil {
			t.Fatalf("%s: Render: %v", tc.name, err)
		}
		var got []string
		for _, u := range units {
			if strings.HasPrefix(u.Name, "villa-tools-") {
				got = append(got, u.Name)
			}
		}
		if !slices.Equal(got, tc.want) {
			t.Errorf("%s: tool units = %v, want %v", tc.name, got, tc.want)
		}
		files, err := RenderToolEnvFiles(in.Cfg)
		if err != nil {
			t.Fatalf("%s: RenderToolEnvFiles: %v", tc.name, err)
		}
		var ids []string
		for _, c := range toolConnections(t, toolEnvFile(t, files, "openwebui.env")) {
			ids = append(ids, c["info"].(map[string]any)["id"].(string))
		}
		if !slices.Equal(ids, tc.connIDs) {
			t.Errorf("%s: registered connections = %v, want %v", tc.name, ids, tc.connIDs)
		}
	}
}

// TestToolSecretNeverInAUnit: neither bearer appears in any rendered unit, so no 0644
// file holds a key. They are in the 0600 env files and only there.
func TestToolSecretNeverInAUnit(t *testing.T) {
	in := toolServerFixtureInput()
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		for _, s := range []string{toolWebSecretFixture, toolRecallSecretFixture} {
			if strings.Contains(u.Text, s) {
				t.Errorf("%s carries a tool bearer:\n%s", u.Name, u.Text)
			}
		}
	}
	files, err := RenderToolEnvFiles(in.Cfg)
	if err != nil {
		t.Fatalf("RenderToolEnvFiles: %v", err)
	}
	if !strings.Contains(toolEnvFile(t, files, "openwebui.env"), toolWebSecretFixture) {
		t.Error("the bearer is missing from the 0600 file Open WebUI reads")
	}
}

// TestWriteToolEnvRoundTrip: each unit's file holds exactly its own key, Open WebUI's
// holds both inside one line, and the writer leaves 0600 files in a 0700 directory.
func TestWriteToolEnvRoundTrip(t *testing.T) {
	in := toolServerFixtureInput()
	files, err := RenderToolEnvFiles(in.Cfg)
	if err != nil {
		t.Fatalf("RenderToolEnvFiles: %v", err)
	}
	if got, want := toolEnvFile(t, files, "web.env"), "VILLA_TOOLS_KEY="+toolWebSecretFixture+"\n"; got != want {
		t.Errorf("web.env = %q, want %q", got, want)
	}
	if got, want := toolEnvFile(t, files, "recall.env"), "VILLA_TOOLS_KEY="+toolRecallSecretFixture+"\n"; got != want {
		t.Errorf("recall.env = %q, want %q", got, want)
	}

	dir := filepath.Join(t.TempDir(), "tools")
	for _, f := range files {
		if err := WriteToolEnvFileTo(dir, f.Name, f.Text); err != nil {
			t.Fatalf("WriteToolEnvFileTo(%s): %v", f.Name, err)
		}
		got, err := os.ReadFile(filepath.Join(dir, f.Name))
		if err != nil || string(got) != f.Text {
			t.Errorf("%s did not round-trip: %q, %v", f.Name, got, err)
		}
		if fi, _ := os.Stat(filepath.Join(dir, f.Name)); fi.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %v, want 0600", f.Name, fi.Mode().Perm())
		}
	}
	if fi, _ := os.Stat(dir); fi.Mode().Perm() != 0o700 {
		t.Errorf("tools dir mode = %v, want 0700", fi.Mode().Perm())
	}
	if err := WriteToolEnvFileTo(dir, "../escape.env", "x"); err == nil {
		t.Error("a name that escapes the tools dir was written")
	}
}

// TestRenderToolEnvFilesRefusesAMissingBearer: an empty bearer for a unit that renders
// is an error, never an env file that starts a server with no key.
func TestRenderToolEnvFilesRefusesAMissingBearer(t *testing.T) {
	for name, mutate := range map[string]func(*RenderInput){
		"web":    func(in *RenderInput) { in.Cfg.ToolWebSecret = "" },
		"recall": func(in *RenderInput) { in.Cfg.ToolRecallSecret = "" },
	} {
		in := toolServerFixtureInput()
		mutate(&in)
		if _, err := RenderToolEnvFiles(in.Cfg); err == nil {
			t.Errorf("%s: rendered an env file with no bearer", name)
		}
	}
}

// TestToolsOpenWebUIEnvIsValidJSON: Open WebUI reads a malformed value as an empty
// list with no error, so the line must be one valid JSON array, and the identity
// placeholder it fills per user must survive verbatim (built with encoding/json, never
// text/template, which would eat the braces).
func TestToolsOpenWebUIEnvIsValidJSON(t *testing.T) {
	files, err := RenderToolEnvFiles(toolServerFixtureInput().Cfg)
	if err != nil {
		t.Fatalf("RenderToolEnvFiles: %v", err)
	}
	text := toolEnvFile(t, files, "openwebui.env")
	conns := toolConnections(t, text)
	if len(conns) != 2 {
		t.Fatalf("connections = %d, want 2", len(conns))
	}
	if !strings.Contains(text, "{{USER_ID}}") {
		t.Errorf("the {{USER_ID}} placeholder did not survive:\n%s", text)
	}
	wantURL := map[string]string{"villa-web": "http://villa-tools-web:8092", "villa-recall": "http://villa-tools-recall:8093"}
	wantKey := map[string]string{"villa-web": toolWebSecretFixture, "villa-recall": toolRecallSecretFixture}
	for _, c := range conns {
		id := c["info"].(map[string]any)["id"].(string)
		for k, want := range map[string]any{"type": "openapi", "spec_type": "url", "path": "openapi.json", "auth_type": "bearer", "url": wantURL[id], "key": wantKey[id]} {
			if c[k] != want {
				t.Errorf("%s: %s = %v, want %v", id, k, c[k], want)
			}
		}
		conf, _ := c["config"].(map[string]any)
		if conf["enable"] != true {
			t.Errorf("%s is not enabled: %v", id, c["config"])
		}
		if _, has := conf["access_grants"]; has {
			t.Errorf("%s carries access grants; the connections stay private to admins", id)
		}
	}
}

// TestToolRecallConnectionCarriesIdentityHeader: the recall connection asks Open WebUI
// to fill the caller's id into the header the recall unit scopes on.
func TestToolRecallConnectionCarriesIdentityHeader(t *testing.T) {
	files, _ := RenderToolEnvFiles(toolServerFixtureInput().Cfg)
	for _, c := range toolConnections(t, toolEnvFile(t, files, "openwebui.env")) {
		if c["info"].(map[string]any)["id"] != "villa-recall" {
			continue
		}
		hdr, _ := c["headers"].(map[string]any)
		if len(hdr) != 1 || hdr[toolserver.IdentityHeader] != "{{USER_ID}}" {
			t.Errorf("recall headers = %v, want {%s: {{USER_ID}}}", hdr, toolserver.IdentityHeader)
		}
		return
	}
	t.Fatal("no recall connection")
}

// TestToolWebConnectionCarriesNoIdentityHeader: a unit is never sent an identity it
// does not need.
func TestToolWebConnectionCarriesNoIdentityHeader(t *testing.T) {
	files, _ := RenderToolEnvFiles(toolServerFixtureInput().Cfg)
	for _, c := range toolConnections(t, toolEnvFile(t, files, "openwebui.env")) {
		if c["info"].(map[string]any)["id"] != "villa-web" {
			continue
		}
		if _, has := c["headers"]; has {
			t.Errorf("the web connection carries headers: %v", c["headers"])
		}
		return
	}
	t.Fatal("no web connection")
}

// TestToolsOpenWebUIUnitReadsTheConnectionFile: the chat unit gains one EnvironmentFile
// line for the tools file, no Environment= key of its own, and still carries
// ENABLE_PERSISTENT_CONFIG=False exactly once and last, because the connection list is
// authoritative only with it (ADR-0042, question 8).
func TestToolsOpenWebUIUnitReadsTheConnectionFile(t *testing.T) {
	units, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	owui := unitByName(t, units, "villa-openwebui.container")
	if got := lines(owui.Text, "EnvironmentFile="); !slices.Contains(got, "EnvironmentFile=%h/.config/villa/tools/openwebui.env") || len(got) != 3 {
		t.Errorf("EnvironmentFile lines = %v, want the inference, websafe and tools files", got)
	}
	if strings.Contains(owui.Text, "TOOL_SERVER") {
		t.Errorf("the chat unit names the connection key; it belongs in the 0600 file only:\n%s", owui.Text)
	}
	env := lines(owui.Text, "Environment=")
	if got := lines(owui.Text, "Environment=ENABLE_PERSISTENT_CONFIG=False"); len(got) != 1 || env[len(env)-1] != got[0] {
		t.Errorf("ENABLE_PERSISTENT_CONFIG=False must be rendered once and last; got %v", got)
	}
}

// TestToolServerRefusesAnUnsafeEmbeddingModel: the recall unit's Exec carries the
// model id as one argv token, so an id that would split it is refused.
func TestToolServerRefusesAnUnsafeEmbeddingModel(t *testing.T) {
	in := toolServerFixtureInput()
	in.Cfg.EmbeddingModel = "nomic --port 1"
	if _, err := Render(in); err == nil {
		t.Error("an embedding model id with a space rendered into the recall Exec")
	}
}

// TestToolServerGateOffRemovesUnits: the registry is the removal set (ADR-0035), so a
// host with both units on disk and a config that no longer renders them plans both for
// removal, and keeps every other unit.
func TestToolServerGateOffRemovesUnits(t *testing.T) {
	on, err := Render(toolServerFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	dir := t.TempDir()
	if err := WriteUnits(Plan{Changed: on}, dir); err != nil {
		t.Fatalf("WriteUnits: %v", err)
	}
	offIn := toolServerFixtureInput()
	offIn.Cfg.ToolServer = false
	off, err := Render(offIn)
	if err != nil {
		t.Fatalf("Render off: %v", err)
	}
	plan, err := Reconcile(off, dir)
	if err != nil {
		t.Fatalf("Reconcile: %v", err)
	}
	var removed []string
	for _, u := range plan.Removed {
		removed = append(removed, u.Name)
	}
	slices.Sort(removed)
	if want := []string{"villa-tools-recall.container", "villa-tools-web.container"}; !slices.Equal(removed, want) {
		t.Errorf("removed = %v, want %v", removed, want)
	}
	var changed []string
	for _, u := range plan.Changed {
		changed = append(changed, u.Name)
	}
	if want := []string{"villa-openwebui.container"}; !slices.Equal(changed, want) {
		t.Errorf("changed = %v, want only the chat unit, which loses the connection file", changed)
	}
}
