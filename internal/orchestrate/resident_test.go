package orchestrate

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// residentModelID is a real catalog-shaped id carrying a dot, so every resident
// assertion exercises the character class the slug rule actually has to fold.
const residentModelID = "qwen3.6-35b-a3b"

// residentUnitName is the .container the fixture below renders.
const residentUnitName = "villa-llama-qwen3-6-35b-a3b.container"

// residentFixtureInput is the deterministic RenderInput the resident goldens are
// frozen against: fixtureInput() plus ONE secondary slot. Cfg.Resident carries the
// persisted source of truth and RenderInput.Resident the caller-resolved descriptor,
// mirroring how codingFixtureInput states both halves.
func residentFixtureInput() RenderInput {
	in := fixtureInput()
	in.Cfg.Resident = []config.ResidentModel{
		{Model: residentModelID, Quant: "UD-Q4_K_M", Ctx: 32768, Port: 8081},
	}
	in.Resident = []ResidentUnit{
		{Model: residentModelID, ModelFile: residentModelID + ".gguf", Ctx: 32768, Port: 8081},
	}
	return in
}

// lineWithPrefix returns the single unit line starting with prefix (or fails).
func lineWithPrefix(t *testing.T, unit Unit, prefix string) string {
	t.Helper()
	var found []string
	for _, line := range strings.Split(unit.Text, "\n") {
		if strings.HasPrefix(line, prefix) {
			found = append(found, line)
		}
	}
	if len(found) != 1 {
		t.Fatalf("unit %s has %d lines starting %q, want exactly 1:\n%s", unit.Name, len(found), prefix, unit.Text)
	}
	return found[0]
}

// TestResidentSlug: a catalog model id folds to a unit-name fragment of [a-z0-9-]
// only, with runs collapsed and the ends trimmed — so a real id with dots
// ("qwen3.6-35b-a3b") can never render a unit name systemd reads as type-suffixed.
func TestResidentSlug(t *testing.T) {
	cases := []struct{ in, want string }{
		{"qwen3.6-35b-a3b", "qwen3-6-35b-a3b"},
		{"qwen3-35b-a3b-moe-64", "qwen3-35b-a3b-moe-64"},
		{"Llama.3.1_8B", "llama-3-1-8b"},
		{"a..b", "a-b"},
		{"--trim--me--", "trim-me"},
		{"MiXeD/Case:Id", "mixed-case-id"},
		{"...", ""},
		{"", ""},
	}
	for _, tc := range cases {
		if got := residentSlug(tc.in); got != tc.want {
			t.Errorf("residentSlug(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

// TestRenderEmptyResidentIsByteIdentical is the whole additivity claim: with an empty
// Resident the rendered unit SET and every unit's BYTES are exactly what they were
// before resident slots existed. It re-uses the pre-existing goldens rather than
// freezing new ones, so a regression here means the resident work leaked into a stack
// that configured no resident model.
func TestRenderEmptyResidentIsByteIdentical(t *testing.T) {
	in := fixtureInput() // Resident nil
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}

	want := []string{
		"villa-llama.container",
		"villa.network",
		"villa-models.volume",
		"villa-openwebui.container",
		"villa-openwebui.volume",
	}
	got := unitNames(units)
	if len(got) != len(want) {
		t.Fatalf("Render returned %d units, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("unit[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}

	goldens := map[string]string{
		"villa-llama.container":     "villa-llama.container.golden",
		"villa.network":             "villa.network.golden",
		"villa-models.volume":       "villa-models.volume.golden",
		"villa-openwebui.container": "villa-openwebui.container.golden",
		"villa-openwebui.volume":    "villa-openwebui.volume.golden",
	}
	for _, u := range units {
		goldenCompare(t, goldens[u.Name], u.Text)
	}

	// The memory-ON and web-search-ON stacks must also be untouched: an empty
	// Resident may not shift where their optional units land.
	for _, tc := range []struct {
		name string
		in   RenderInput
		want []string
	}{
		{"memory-on", memoryFixtureInput(), append(append([]string{}, want...),
			"villa-qdrant.container", "villa-qdrant.volume", "villa-embed.container")},
		{"websearch-on", searxngFixtureInput(), append(append([]string{}, want...),
			"villa-searxng.container", "villa-websafe.container")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			units, err := Render(tc.in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			got := unitNames(units)
			if len(got) != len(tc.want) {
				t.Fatalf("Render returned %v, want %v", got, tc.want)
			}
			for i := range tc.want {
				if got[i] != tc.want[i] {
					t.Errorf("unit[%d] = %q, want %q (full order: %v)", i, got[i], tc.want[i], got)
				}
			}
		})
	}
}

// TestRenderResidentContainerGolden: one resident slot renders one extra
// .container whose bytes are frozen — the unit file name, the container DNS name,
// its own host publish port, and its own model/-c Exec.
func TestRenderResidentContainerGolden(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, residentUnitName)
	goldenCompare(t, residentUnitName+".golden", c.Text)
}

// TestRenderResidentUnitOrder: a resident unit is appended STRICTLY after the fixed
// five, in configured order — the position the goldens and the reconcile plan depend on.
func TestRenderResidentUnitOrder(t *testing.T) {
	in := residentFixtureInput()
	in.Cfg.Resident = append(in.Cfg.Resident, config.ResidentModel{Model: "gemma3-12b", Ctx: 8192, Port: 8082})
	in.Resident = append(in.Resident, ResidentUnit{Model: "gemma3-12b", ModelFile: "gemma3-12b.gguf", Ctx: 8192, Port: 8082})

	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	want := []string{
		"villa-llama.container",
		"villa.network",
		"villa-models.volume",
		"villa-openwebui.container",
		"villa-openwebui.volume",
		residentUnitName,
		"villa-llama-gemma3-12b.container",
	}
	got := unitNames(units)
	if len(got) != len(want) {
		t.Fatalf("Render returned %d units, want %d: %v", len(got), len(want), got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("unit[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

// TestRenderResidentPublishesOwnHostPortOnly: a resident slot differs from the primary
// on the HOST port alone. The loopback address and the container-internal port are
// identical, because each container has its own netns — and the primary's own publish
// line is untouched.
func TestRenderResidentPublishesOwnHostPortOnly(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	primary := lineWithPrefix(t, unitByName(t, units, "villa-llama.container"), "PublishPort=")
	resident := lineWithPrefix(t, unitByName(t, units, residentUnitName), "PublishPort=")

	if primary != "PublishPort=127.0.0.1:8080:8080" {
		t.Errorf("primary publish line changed: %q", primary)
	}
	if resident != "PublishPort=127.0.0.1:8081:8080" {
		t.Errorf("resident publish line = %q, want PublishPort=127.0.0.1:8081:8080", resident)
	}

	pParts := strings.Split(strings.TrimPrefix(primary, "PublishPort="), ":")
	rParts := strings.Split(strings.TrimPrefix(resident, "PublishPort="), ":")
	if pParts[0] != rParts[0] {
		t.Errorf("resident bind address %q differs from the primary's %q (loopback is seam-sourced)", rParts[0], pParts[0])
	}
	if pParts[2] != rParts[2] {
		t.Errorf("resident container-internal port %q differs from the primary's %q (every slot serves the same internal port)", rParts[2], pParts[2])
	}
	if pParts[1] == rParts[1] {
		t.Errorf("resident host port %q collides with the primary's", rParts[1])
	}
}

// TestRenderResidentExecCarriesOwnModelAndCtx: each slot loads its OWN model file with
// its OWN single -c, and the primary's Exec is unaffected.
func TestRenderResidentExecCarriesOwnModelAndCtx(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	resident := lineWithPrefix(t, unitByName(t, units, residentUnitName), "Exec=")
	if !strings.Contains(resident, "/"+residentModelID+".gguf") {
		t.Errorf("resident Exec does not load its own model file: %q", resident)
	}
	if !strings.Contains(resident, "-c 32768") {
		t.Errorf("resident Exec does not carry its own -c 32768: %q", resident)
	}
	if strings.Count(resident, "-c ") != 1 {
		t.Errorf("resident Exec must have exactly one -c token: %q", resident)
	}

	primary := lineWithPrefix(t, unitByName(t, units, "villa-llama.container"), "Exec=")
	if !strings.Contains(primary, "qwen3-35b-a3b-moe-64.gguf") || !strings.Contains(primary, "-c 131072") {
		t.Errorf("primary Exec changed when a resident slot was added: %q", primary)
	}
}

// TestRenderResidentSharesPrimaryBackendLiterals: a resident unit takes its image,
// device passthrough, group and security args from the SAME backend seam the primary
// does. A resident slot that drifted from the primary's posture would mean a literal
// had been re-typed instead of parsed out of ContainerArgs.
func TestRenderResidentSharesPrimaryBackendLiterals(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	primary := unitByName(t, units, "villa-llama.container")
	resident := unitByName(t, units, residentUnitName)

	for _, prefix := range []string{"Image=", "AddDevice=/dev/dri", "GroupAdd=", "PodmanArgs=", "Network=", "Volume=", "Description="} {
		if lineWithPrefix(t, primary, prefix) != lineWithPrefix(t, resident, prefix) {
			t.Errorf("resident %q line differs from the primary's:\nprimary:  %s\nresident: %s",
				prefix, lineWithPrefix(t, primary, prefix), lineWithPrefix(t, resident, prefix))
		}
	}

	if !strings.Contains(resident.Text, "ContainerName=villa-llama-qwen3-6-35b-a3b") {
		t.Errorf("resident unit missing its own ContainerName:\n%s", resident.Text)
	}
	if !strings.HasPrefix(resident.Text, "# ~/.config/containers/systemd/"+residentUnitName+"  ") {
		t.Errorf("resident unit header names the wrong file:\n%s", resident.Text)
	}
}

// TestRenderResidentFailsClosedOnUnusableIDs: a hand-edited config whose entries slug
// to nothing, or to the same unit name twice, is refused with an actionable error
// rather than rendering a malformed or silently-deduplicated unit.
func TestRenderResidentFailsClosedOnUnusableIDs(t *testing.T) {
	cases := []struct {
		name     string
		resident []ResidentUnit
		want     string
	}{
		{
			name:     "no usable characters",
			resident: []ResidentUnit{{Model: "...", ModelFile: "x.gguf", Ctx: 1024, Port: 8081}},
			want:     "no usable unit-name characters",
		},
		{
			name: "distinct ids colliding on one slug",
			resident: []ResidentUnit{
				{Model: "qwen3.6", ModelFile: "a.gguf", Ctx: 1024, Port: 8081},
				{Model: "qwen3_6", ModelFile: "b.gguf", Ctx: 1024, Port: 8082},
			},
			want: "collide on unit name",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			in := fixtureInput()
			in.Resident = tc.resident
			_, err := Render(in)
			if err == nil {
				t.Fatalf("Render accepted %+v, want a refusal", tc.resident)
			}
			if !strings.Contains(err.Error(), tc.want) {
				t.Errorf("Render error = %v, want it to mention %q", err, tc.want)
			}
		})
	}
}

// TestRenderOpenWebUIResidentEndpointsGolden: with a resident slot configured, the
// Open WebUI env block is frozen in its plural form.
func TestRenderOpenWebUIResidentEndpointsGolden(t *testing.T) {
	units, err := Render(residentFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.resident.golden", c.Text)
}

// TestRenderOpenWebUIResidentEndpointsPluralPrimaryFirst: Open WebUI is pointed at
// EVERY resident endpoint via the plural OPENAI_API_BASE_URLS/OPENAI_API_KEYS pair,
// with the primary first and one key per URL; the singular keys are gone. With no
// resident slot the singular pair is emitted and the plural keys never appear.
func TestRenderOpenWebUIResidentEndpointsPluralPrimaryFirst(t *testing.T) {
	in := residentFixtureInput()
	in.Cfg.Resident = append(in.Cfg.Resident, config.ResidentModel{Model: "gemma3-12b", Ctx: 8192, Port: 8082})
	in.Resident = append(in.Resident, ResidentUnit{Model: "gemma3-12b", ModelFile: "gemma3-12b.gguf", Ctx: 8192, Port: 8082})

	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")

	wantURLs := "Environment=OPENAI_API_BASE_URLS=" + strings.Join([]string{
		"http://villa-llama:8080/v1",
		"http://villa-llama-qwen3-6-35b-a3b:8080/v1",
		"http://villa-llama-gemma3-12b:8080/v1",
	}, ";")
	if !strings.Contains(c.Text, wantURLs) {
		t.Errorf("OWUI unit missing the plural endpoint list:\nwant substring %q\n%s", wantURLs, c.Text)
	}
	wantKeys := "Environment=OPENAI_API_KEYS=sk-no-key-required;sk-no-key-required;sk-no-key-required"
	if !strings.Contains(c.Text, wantKeys) {
		t.Errorf("OWUI unit missing one API key per endpoint:\nwant substring %q\n%s", wantKeys, c.Text)
	}
	for _, singular := range []string{"Environment=OPENAI_API_BASE_URL=", "Environment=OPENAI_API_KEY="} {
		if strings.Contains(c.Text, singular) {
			t.Errorf("OWUI unit still carries the singular %q alongside the plural form:\n%s", singular, c.Text)
		}
	}

	off, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render(no resident): %v", err)
	}
	cOff := unitByName(t, off, "villa-openwebui.container")
	if !strings.Contains(cOff.Text, "Environment=OPENAI_API_BASE_URL=http://villa-llama:8080/v1") {
		t.Errorf("no-resident OWUI unit lost the singular endpoint:\n%s", cOff.Text)
	}
	for _, plural := range []string{"OPENAI_API_BASE_URLS", "OPENAI_API_KEYS"} {
		if strings.Contains(cOff.Text, plural) {
			t.Errorf("no-resident OWUI unit carries %q (the plural form is resident-only):\n%s", plural, cOff.Text)
		}
	}
}

// TestRenderResidentRefusesPortCollision guards the fail-closed promise for a
// hand-edited config: two slots claiming one host port, or a slot claiming the
// primary's port, render units that cannot both bind. The renderer must refuse
// with an actionable error rather than emit a set that dies at container start.
func TestRenderResidentRefusesPortCollision(t *testing.T) {
	for _, tc := range []struct {
		name     string
		resident []ResidentUnit
	}{
		{"two residents share a port", []ResidentUnit{
			{Model: "a-model", ModelFile: "a.gguf", Ctx: 4096, Port: 8081},
			{Model: "b-model", ModelFile: "b.gguf", Ctx: 4096, Port: 8081},
		}},
		{"resident steals the primary port", []ResidentUnit{
			{Model: "a-model", ModelFile: "a.gguf", Ctx: 4096, Port: 8080},
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			in := fixtureInput()
			in.Resident = tc.resident
			_, err := Render(in)
			if err == nil {
				t.Fatalf("Render accepted a colliding host port; expected a refusal")
			}
			if !strings.Contains(err.Error(), "port") {
				t.Fatalf("refusal does not mention the port: %v", err)
			}
		})
	}
}
