package orchestrate

import (
	"strings"
	"testing"
)

// TestRenderOpenWebUIExternalLoaderWiring (Phase-31 GUARD-01/GROUND-01/02): with web search
// on, the OWUI unit carries the external-loader wiring — WEB_LOADER_ENGINE=external, the
// config-composed EXTERNAL_WEB_LOADER_URL ending in the Plan-01 /load path, BYPASS flipped
// to False (native embed→retrieve back on), and the retrieval-fix key. Each key is bound to
// buildOpenWebUIView's output so an env-name regression fails by construction. The
// search-OFF unit must carry NONE of these keys (byte-identical-off).
func TestRenderOpenWebUIExternalLoaderWiring(t *testing.T) {
	on, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render(on): %v", err)
	}
	c := unitByName(t, on, "villa-openwebui.container")

	wantOn := []string{
		"Environment=WEB_LOADER_ENGINE=external",
		"Environment=EXTERNAL_WEB_LOADER_URL=http://villa-websafe:8090/load",
		"Environment=BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=False",
		"Environment=ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS=True",
	}
	for _, want := range wantOn {
		if !strings.Contains(c.Text, want) {
			t.Errorf("web search on: OWUI unit missing external-loader key %q:\n%s", want, c.Text)
		}
	}
	// The Phase-30 direct-inject value (BYPASS=True) MUST be gone (flipped to False).
	if strings.Contains(c.Text, "Environment=BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL=True") {
		t.Errorf("web search on: OWUI unit still carries the Phase-30 BYPASS=True (must flip to False, GROUND-01/02):\n%s", c.Text)
	}

	// Search-OFF: NONE of the external-loader keys may appear (byte-identical-off).
	off, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render(off): %v", err)
	}
	cOff := unitByName(t, off, "villa-openwebui.container")
	for _, k := range []string{"WEB_LOADER_ENGINE", "EXTERNAL_WEB_LOADER_URL", "ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS", "BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL"} {
		if strings.Contains(cOff.Text, k) {
			t.Errorf("web search off: OWUI unit must NOT carry %q (byte-identical-off):\n%s", k, cOff.Text)
		}
	}
}

// TestRenderOpenWebUIExternalLoaderURLConfigDriven (WR-01): the EXTERNAL_WEB_LOADER_URL
// host:port is composed from the resolved cfg.WebsafeAddr/cfg.WebsafePort, NOT an
// orchestrate-local const. A non-default addr+port must surface, and the path is the
// single-source Plan-01 /load route.
func TestRenderOpenWebUIExternalLoaderURLConfigDriven(t *testing.T) {
	in := searxngFixtureInput()
	in.Cfg.WebsafeAddr = "villa-websafe-custom"
	in.Cfg.WebsafePort = 9099
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	want := "Environment=EXTERNAL_WEB_LOADER_URL=http://villa-websafe-custom:9099/load"
	if !strings.Contains(c.Text, want) {
		t.Errorf("OWUI EXTERNAL_WEB_LOADER_URL not composed from the config-resolved websafe host:port (rendered from a const?):\nwant substring %q\n%s", want, c.Text)
	}
}

// TestRenderOpenWebUIBearerViaEnvironmentFile (T-31-12): with web search on, the OWUI unit
// references the EXTERNAL_WEB_LOADER_API_KEY bearer via a 0600 EnvironmentFile= (the SAME
// websafe.env file the villa-websafe unit references) — NEVER an inline Environment= line,
// and the secret VALUE never lands in the 0644 unit. Search-OFF: no EnvironmentFile= at all
// (byte-identical-off).
func TestRenderOpenWebUIBearerViaEnvironmentFile(t *testing.T) {
	on, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render(on): %v", err)
	}
	c := unitByName(t, on, "villa-openwebui.container")
	want := "EnvironmentFile=" + WebsafeSecretEnvFilePath()
	if !strings.Contains(c.Text, want) {
		t.Errorf("web search on: OWUI unit does not reference the bearer EnvironmentFile= %q:\n%s", want, c.Text)
	}
	if strings.Contains(c.Text, "Environment=EXTERNAL_WEB_LOADER_API_KEY=") {
		t.Errorf("web search on: OWUI unit carries an inline Environment=EXTERNAL_WEB_LOADER_API_KEY= (T-31-12 leak):\n%s", c.Text)
	}
	if strings.Contains(c.Text, "websafe_testsecret_must_not_appear_in_the_0644_unit") {
		t.Errorf("web search on: OWUI unit leaked the bearer VALUE into the 0644 unit (T-31-12):\n%s", c.Text)
	}

	off, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render(off): %v", err)
	}
	cOff := unitByName(t, off, "villa-openwebui.container")
	if strings.Contains(cOff.Text, "EnvironmentFile=") {
		t.Errorf("web search off: OWUI unit must NOT carry an EnvironmentFile= (byte-identical-off):\n%s", cOff.Text)
	}
}

// TestOpenWebUIImageAccessor asserts the exported OpenWebUIImage() accessor
// returns the same digest-pinned value as the unexported managed-service const,
// so the Phase-16 backup manifest can source the OWUI digest through the seam
// without re-typing the literal (D-10).
func TestOpenWebUIImageAccessor(t *testing.T) {
	got := OpenWebUIImage()
	if got != openWebUIImage {
		t.Fatalf("OpenWebUIImage() = %q, want %q", got, openWebUIImage)
	}
	// Sanity: it is a digest-pinned image (the accessor is the manifest's source).
	if !strings.Contains(got, "@sha256:") {
		t.Errorf("OpenWebUIImage() %q is not digest-pinned", got)
	}
}

// --- Phase 33: PRIV-09 / PRIV-07 regression assertions (assert-only) ---------
//
// These guards LOCK the already-shipped invariants Phase 33 depends on; they ASSERT, never
// modify (re-adding the env keys or re-freezing the golden would break the existing
// byte-frozen guards). openwebui.go, villaconfig.go, and the OWUI goldens are untouched by
// this phase — these tests fail loudly if a future change silently drops the outbound-kill
// env or widens the web-search-OFF render.

// TestOWUIKillEnvPresentBothViewsPRIV09 (PRIV-09) asserts the SIX outbound-kill env keys are
// present in BOTH the web-search-OFF and web-search-ON rendered villa-openwebui.container
// units — a dedicated, self-documenting guard that the kill-set is never silently dropped or
// gated behind the web-search toggle. The keys live UNCONDITIONALLY in the base env block of
// openwebui.go (buildOpenWebUIView); this is assertion-only.
func TestOWUIKillEnvPresentBothViewsPRIV09(t *testing.T) {
	killEnv := []string{
		"Environment=HF_HUB_OFFLINE=1",
		"Environment=ANONYMIZED_TELEMETRY=False",
		"Environment=DO_NOT_TRACK=True",
		"Environment=SCARF_NO_ANALYTICS=True",
		"Environment=OFFLINE_MODE=True",
		"Environment=ENABLE_VERSION_UPDATE_CHECK=False",
	}

	views := []struct {
		name string
		in   RenderInput
	}{
		{"web-search-off", fixtureInput()},
		{"web-search-on", searxngFixtureInput()},
	}
	for _, v := range views {
		t.Run(v.name, func(t *testing.T) {
			units, err := Render(v.in)
			if err != nil {
				t.Fatalf("Render(%s): %v", v.name, err)
			}
			c := unitByName(t, units, "villa-openwebui.container")
			for _, key := range killEnv {
				if !strings.Contains(c.Text, key) {
					t.Errorf("PRIV-09 regression: %s OWUI unit is missing outbound-kill env %q (the kill-set must never be dropped or gated):\n%s", v.name, key, c.Text)
				}
			}
		})
	}
}

// TestOWUIWebOffByteIdenticalPRIV07 (PRIV-07) asserts the web-search-OFF OWUI render stays
// byte-identical to the v1.4 villa-openwebui.container.golden — opting into web search must
// be the ONLY thing that changes the unit (no silent scope reduction of the opt-in). It
// reuses the EXISTING golden (goldenCompare); it does NOT re-freeze it.
func TestOWUIWebOffByteIdenticalPRIV07(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render(web-off): %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.golden", c.Text)
}
