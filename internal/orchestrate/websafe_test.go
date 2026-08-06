package orchestrate

import (
	"fmt"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// websafe_test.go guards the Phase-31 villa-websafe managed-service render: the gated
// unit golden, the seam-locked distroless image, the config-resolved network identity, the
// read-only host-binary bind-mount, the fixed-token websafe-serve Exec, the no-host-port
// PRIV-01 posture, and the 0600 bearer EnvironmentFile secret discipline (T-31-12). It
// reuses searxngFixtureInput() (WebSearchEnabled, websafe fields + HostVillaPath populated)
// so the websafe unit renders deterministically.

// TestRenderWebsafe: with web search on, the villa-websafe.container unit matches its golden
// byte-for-byte (regen with -update) and carries the config-resolved container-DNS identity,
// the EnvironmentFile= secret reference (path only), the read-only host-binary bind-mount,
// the fixed-token websafe-serve Exec, and the digest-pinned distroless image — with NO host
// port (GUARD-01 service shape).
func TestRenderWebsafe(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-websafe.container")
	goldenCompare(t, "villa-websafe.container.golden", c.Text)

	if !strings.Contains(c.Text, "Image=gcr.io/distroless/static-debian12@sha256:") {
		t.Errorf("websafe unit missing digest-pinned distroless image:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Network=villa.network") {
		t.Errorf("websafe unit missing Network=villa.network:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "ContainerName=villa-websafe") {
		t.Errorf("websafe unit missing config-resolved ContainerName=villa-websafe:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "EnvironmentFile=") {
		t.Errorf("websafe unit missing EnvironmentFile= secret reference:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Volume=/home/villa/.local/bin/villa:/usr/local/bin/villa:ro,z") {
		t.Errorf("websafe unit missing read-only host-binary bind-mount:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Exec=/usr/local/bin/villa websafe-serve --host 0.0.0.0 --port 8090") {
		t.Errorf("websafe unit missing fixed-token websafe-serve Exec:\n%s", c.Text)
	}
}

// TestWebsafeUnitNoSecretLeak (T-31-12): the rendered .container unit carries NO inline
// secret — no `Environment=EXTERNAL_WEB_LOADER_API_KEY=` line and NOT the secret value
// itself; only an `EnvironmentFile=` PATH reference. The secret never lands in the 0644 unit.
func TestWebsafeUnitNoSecretLeak(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-websafe.container")
	if strings.Contains(c.Text, "Environment=EXTERNAL_WEB_LOADER_API_KEY=") {
		t.Errorf("websafe unit carries an inline Environment=EXTERNAL_WEB_LOADER_API_KEY= literal (T-31-12 leak):\n%s", c.Text)
	}
	if strings.Contains(c.Text, "websafe_testsecret_must_not_appear_in_the_0644_unit") {
		t.Errorf("websafe unit leaked the secret VALUE into the 0644 unit (T-31-12):\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "EnvironmentFile=") {
		t.Errorf("websafe unit must reference the secret via EnvironmentFile= (path only):\n%s", c.Text)
	}
}

// TestWebsafeUnitNoPublishPort (PRIV-01): the websafe unit publishes NO host port
// (container-DNS only on villa.network).
func TestWebsafeUnitNoPublishPort(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-websafe.container")
	for _, bad := range []string{"PublishPort=", "Publish=", "-p "} {
		if strings.Contains(c.Text, bad) {
			t.Errorf("websafe unit must not publish a host port (privacy leak, PRIV-01): found %q in:\n%s", bad, c.Text)
		}
	}
}

// TestWebsafeSecretEnvFilePathContract (T-31-12 cross-plan contract): the EnvironmentFile=
// path the websafe unit references is the exported WebsafeSecretEnvFilePath() — the exact
// path Plan 02 writes at 0600. The unit's EnvironmentFile= line must reference THAT path.
func TestWebsafeSecretEnvFilePathContract(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-websafe.container")
	want := "EnvironmentFile=" + WebsafeSecretEnvFilePath()
	if !strings.Contains(c.Text, want) {
		t.Errorf("websafe unit EnvironmentFile= does not reference the contract path %q:\n%s", want, c.Text)
	}
	if !strings.Contains(WebsafeSecretEnvFilePath(), "websafe.env") {
		t.Errorf("WebsafeSecretEnvFilePath() %q must end at the websafe.env file", WebsafeSecretEnvFilePath())
	}
}

// TestRenderWebsafeSecretEnv: the secret env-file render is the single-source format Plan 02
// writes at 0600 — an `EXTERNAL_WEB_LOADER_API_KEY=<value>` line, the bare filename
// websafe.env, and it carries the secret value (this file is the 0600 target, NOT a 0644 file).
func TestRenderWebsafeSecretEnv(t *testing.T) {
	name, text := RenderWebsafeSecretEnv("hunter2hunter2")
	if name != "websafe.env" {
		t.Errorf("RenderWebsafeSecretEnv name = %q, want websafe.env", name)
	}
	if text != "EXTERNAL_WEB_LOADER_API_KEY=hunter2hunter2\n" {
		t.Errorf("RenderWebsafeSecretEnv body = %q, want EXTERNAL_WEB_LOADER_API_KEY=hunter2hunter2\\n", text)
	}
}

// TestRenderWebsafeUsesSharedIdentity (WR-01): the websafe unit derives its
// container-DNS identity and its Exec --port from the SINGLE home of those literals
// in internal/config, not from an orchestrate-local const.
func TestRenderWebsafeUsesSharedIdentity(t *testing.T) {
	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-websafe.container")
	if !strings.Contains(c.Text, "ContainerName="+config.WebsafeAddr) {
		t.Errorf("websafe unit did not render ContainerName=%s from the shared constant:\n%s", config.WebsafeAddr, c.Text)
	}
	if !strings.Contains(c.Text, fmt.Sprintf("--port %d", config.WebsafePort)) {
		t.Errorf("websafe unit did not render Exec --port %d from the shared constant:\n%s", config.WebsafePort, c.Text)
	}
}
