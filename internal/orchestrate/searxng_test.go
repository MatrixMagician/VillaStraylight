package orchestrate

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// searxngFixtureInput is the deterministic RenderInput the searxng-unit golden is frozen
// against: identical to fixtureInput() but with the v1.5 web-search config populated and
// WebSearchEnabled=true so Render() appends the villa-searxng unit. The image digest is
// sourced THROUGH the orchestrate managed-service const (SRCH-01), never hand-typed here.
// A fixed test secret is set so the unit golden is stable — but the secret VALUE must NOT
// appear in the rendered 0644 unit (T-29-02), only the EnvironmentFile= path does.
func searxngFixtureInput() RenderInput {
	return RenderInput{
		Backend: inference.VulkanBackend(),
		Cfg: config.VillaConfig{
			Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
			WebSearchEnabled:     true,
			SearxngAddr:          "villa-searxng",
			SearxngPort:          8080,
			SearxngSecret:        "testsecret_must_not_appear_in_the_0644_unit",
			WebSearchResultCount: 3,
			// Phase-31 villa-websafe loader identity (WR-01, config-resolved). The bearer
			// secret VALUE must NOT appear in the rendered 0644 unit (T-31-12) — only the
			// EnvironmentFile= path does.
			WebsafeAddr:     "villa-websafe",
			WebsafePort:     8090,
			WebLoaderSecret: "websafe_testsecret_must_not_appear_in_the_0644_unit",
		},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
		// Phase-31: the host villa binary path bind-mounted into the villa-websafe container
		// (deterministic for the golden; never shell-interpolated).
		HostVillaPath: "/home/villa/.local/bin/villa",
	}
}

// TestRenderSearxng: with web search on, the villa-searxng.container unit matches its
// golden byte-for-byte (regen with -update) and carries the config-resolved
// container-DNS identity, the EnvironmentFile= secret reference (path only), the
// /etc/searxng:ro,Z settings mount, and the digest-pinned image — with NO host port.
// SRCH-01.
func TestRenderSearxng(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-searxng.container")
	goldenCompare(t, "villa-searxng.container.golden", c.Text)

	if !strings.Contains(c.Text, "Image=ghcr.io/searxng/searxng@sha256:") {
		t.Errorf("searxng unit missing digest-pinned image:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Network=villa.network") {
		t.Errorf("searxng unit missing Network=villa.network:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "ContainerName=villa-searxng") {
		t.Errorf("searxng unit missing config-resolved ContainerName=villa-searxng:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "EnvironmentFile=") {
		t.Errorf("searxng unit missing EnvironmentFile= secret reference:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, ":/etc/searxng:ro,Z") {
		t.Errorf("searxng unit missing /etc/searxng:ro,Z settings mount:\n%s", c.Text)
	}
}

// TestSearxngUnitNoSecretLeak (T-29-02): the rendered .container unit carries NO inline
// secret — no `Environment=SEARXNG_SECRET=` line and NOT the secret value itself; only an
// `EnvironmentFile=` PATH reference. The secret never lands in the 0644 unit.
func TestSearxngUnitNoSecretLeak(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-searxng.container")
	if strings.Contains(c.Text, "Environment=SEARXNG_SECRET=") {
		t.Errorf("searxng unit carries an inline Environment=SEARXNG_SECRET= literal (T-29-02 leak):\n%s", c.Text)
	}
	if strings.Contains(c.Text, "testsecret_must_not_appear_in_the_0644_unit") {
		t.Errorf("searxng unit leaked the secret VALUE into the 0644 unit (T-29-02):\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "EnvironmentFile=") {
		t.Errorf("searxng unit must reference the secret via EnvironmentFile= (path only):\n%s", c.Text)
	}
}

// TestSearxngUnitNoPublishPort (T-29-04): the searxng unit publishes NO host port
// (container-DNS only on villa.network, SC#1).
func TestSearxngUnitNoPublishPort(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-searxng.container")
	for _, bad := range []string{"PublishPort=", "Publish=", "-p "} {
		if strings.Contains(c.Text, bad) {
			t.Errorf("searxng unit must not publish a host port (privacy leak, T-29-04): found %q in:\n%s", bad, c.Text)
		}
	}
}

// TestRenderSearxngSettings: the settings.yml render matches its golden byte-for-byte and
// carries the SRCH-01 server/search shape (limiter:false, image_proxy:false,
// formats:[html,json]) with an EMPTY secret_key (the secret arrives via env, T-29-02).
func TestRenderSearxngSettings(t *testing.T) {
	_, text, err := RenderSearxngSettings(searxngFixtureInput().Cfg)
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	goldenCompare(t, "searxng-settings.yml.golden", text)

	for _, want := range []string{"limiter: false", "image_proxy: false", "- html", "- json"} {
		if !strings.Contains(text, want) {
			t.Errorf("settings.yml missing %q:\n%s", want, text)
		}
	}
	if !strings.Contains(text, `secret_key: ""`) {
		t.Errorf("settings.yml must render secret_key EMPTY (secret arrives via $SEARXNG_SECRET, T-29-02):\n%s", text)
	}
	if strings.Contains(text, "testsecret_must_not_appear_in_the_0644_unit") {
		t.Errorf("settings.yml leaked a secret value (must be empty, T-29-02):\n%s", text)
	}
}

// TestRenderSearxngSettingsIsNotAUnit (Pitfall 1): the settings.yml is produced by a
// SEPARATE helper and is NOT appended to the Render() []Unit slice (it must not land in
// the systemd unit dir). The filename is settings.yml, not a .container/.volume unit.
func TestRenderSearxngSettingsIsNotAUnit(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if strings.Contains(u.Name, "settings") || strings.HasSuffix(u.Name, ".yml") {
			t.Errorf("settings.yml leaked into the Render() unit slice as %q (Pitfall 1):\n%v", u.Name, unitNames(units))
		}
	}
	name, _, err := RenderSearxngSettings(searxngFixtureInput().Cfg)
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if name != "settings.yml" {
		t.Errorf("RenderSearxngSettings name = %q, want settings.yml", name)
	}
}

// TestSearxngEngineAllowlist (SRCH-04): the rendered settings.yml restricts engines via
// keep_only to EXACTLY the vetted subset (duckduckgo/brave/wikipedia/wikidata) and does
// NOT inline a full-engine block. The keep_only allowlist is the auditable outbound surface.
func TestSearxngEngineAllowlist(t *testing.T) {
	_, text, err := RenderSearxngSettings(searxngFixtureInput().Cfg)
	if err != nil {
		t.Fatalf("RenderSearxngSettings: %v", err)
	}
	if !strings.Contains(text, "keep_only:") {
		t.Errorf("settings.yml missing use_default_settings.engines.keep_only (SRCH-04):\n%s", text)
	}
	want := []string{"duckduckgo", "brave", "wikipedia", "wikidata"}
	for _, eng := range want {
		if !strings.Contains(text, "- "+eng) {
			t.Errorf("settings.yml keep_only missing vetted engine %q (SRCH-04):\n%s", eng, text)
		}
	}
	// SearxngEngines() is the single-source allowlist — assert it equals the vetted set so a
	// future drift between the slice and the render is caught.
	got := SearxngEngines()
	if len(got) != len(want) {
		t.Fatalf("SearxngEngines() = %v, want %v (SRCH-04 bounded set)", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("SearxngEngines()[%d] = %q, want %q", i, got[i], want[i])
		}
	}
	// Excluded engines must NOT appear (a too-broad outbound surface is the SRCH-04 risk).
	for _, bad := range []string{"- google", "- bing", "- reddit", "- youtube"} {
		if strings.Contains(text, bad) {
			t.Errorf("settings.yml keep_only includes excluded engine line %q (SRCH-04 over-broad):\n%s", bad, text)
		}
	}
}

// TestRenderSearxngSecretEnv: the secret env-file render is the single-source format Plan
// 02 writes at 0600 — a `SEARXNG_SECRET=<value>` line, the bare filename searxng.env, and
// it carries the secret value (this file is the 0600 target, NOT a 0644 file).
func TestRenderSearxngSecretEnv(t *testing.T) {
	name, text := RenderSearxngSecretEnv("hunter2hunter2")
	if name != "searxng.env" {
		t.Errorf("RenderSearxngSecretEnv name = %q, want searxng.env", name)
	}
	if text != "SEARXNG_SECRET=hunter2hunter2\n" {
		t.Errorf("RenderSearxngSecretEnv body = %q, want SEARXNG_SECRET=hunter2hunter2\\n", text)
	}
}

// TestSearxngSecretEnvFilePathContract (T-29-02 cross-plan contract): the EnvironmentFile=
// path the unit references is the exported SearXNGSecretEnvFilePath() — the exact path Plan
// 02 writes at 0600. The unit's EnvironmentFile= line must reference THAT path.
func TestSearxngSecretEnvFilePathContract(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-searxng.container")
	want := "EnvironmentFile=" + SearXNGSecretEnvFilePath()
	if !strings.Contains(c.Text, want) {
		t.Errorf("searxng unit EnvironmentFile= does not reference the contract path %q:\n%s", want, c.Text)
	}
	if !strings.Contains(SearXNGSecretEnvFilePath(), "searxng.env") {
		t.Errorf("SearXNGSecretEnvFilePath() %q must end at the searxng.env file", SearXNGSecretEnvFilePath())
	}
}

// TestRenderByteIdenticalWhenWebSearchOff (SC#4 / PRIV-07): with web search off, Render
// returns EXACTLY the v1.4 units and the searxng unit name does NOT appear. The existing 5
// (or 8, memory-on) goldens stay unchanged — proven by the existing render/memory tests
// staying green plus this name regression.
func TestRenderByteIdenticalWhenWebSearchOff(t *testing.T) {
	// Web-search-off (the default fixture). The villa-searxng unit must be absent.
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if u.Name == "villa-searxng.container" {
			t.Errorf("web search off: Render must NOT emit villa-searxng.container (got %v)", unitNames(units))
		}
		if u.Name == "villa-websafe.container" {
			t.Errorf("web search off: Render must NOT emit villa-websafe.container (got %v)", unitNames(units))
		}
	}
	if len(units) != 5 {
		t.Fatalf("web search off: Render returned %d units, want exactly 5 (v1.4 baseline): %v", len(units), unitNames(units))
	}

	// Sanity: turning web search ON adds exactly TWO units (the searxng container then the
	// villa-websafe container, in that order), strictly appended — the off-render is the
	// on-render minus those two units (Phase-31 byte-identical-off, SC#4).
	on, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render(on): %v", err)
	}
	if len(on) != 7 {
		t.Fatalf("web search on: Render returned %d units, want 7 (5 baseline + searxng + websafe): %v", len(on), unitNames(on))
	}
	if on[len(on)-2].Name != "villa-searxng.container" {
		t.Errorf("searxng unit must be the second-to-last (strictly appended before websafe) unit, got order %v", unitNames(on))
	}
	if on[len(on)-1].Name != "villa-websafe.container" {
		t.Errorf("websafe unit must be the LAST (strictly appended) unit, got order %v", unitNames(on))
	}
}

// TestRenderSearxngIsConfigDriven (WR-01): the searxng unit derives its container-DNS
// identity from cfg.SearxngAddr via the resolved config — NOT an orchestrate-local const.
// A non-default addr must surface in the unit text.
func TestRenderSearxngIsConfigDriven(t *testing.T) {
	in := searxngFixtureInput()
	in.Cfg.SearxngAddr = "villa-searxng-custom"
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-searxng.container")
	if !strings.Contains(c.Text, "ContainerName=villa-searxng-custom") {
		t.Errorf("searxng unit did not render the config-resolved ContainerName=villa-searxng-custom (rendered from a const?):\n%s", c.Text)
	}
}
