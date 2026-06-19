package orchestrate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
)

// update regenerates the golden fixtures (mirrors cmd/villa/recommend_test.go's
// `-update` harness). Run `go test ./internal/orchestrate/... -update` to refreeze.
var update = flag.Bool("update", false, "regenerate golden unit fixtures")

// fixtureInput is the deterministic RenderInput the goldens are frozen against.
// ModelsDir is a fixed absolute host path (NOT live $HOME) so the golden is stable
// in CI. The image digest is sourced THROUGH the Vulkan backend seam, never hand-
// typed in the test (so the golden tracks Backend.Image()).
func fixtureInput() RenderInput {
	return RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       config.VillaConfig{Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan"},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	}
}

// unitByName returns the rendered Unit with the given name (or fails the test).
func unitByName(t *testing.T, units []Unit, name string) Unit {
	t.Helper()
	for _, u := range units {
		if u.Name == name {
			return u
		}
	}
	t.Fatalf("rendered units missing %q (got %v)", name, unitNames(units))
	return Unit{}
}

func unitNames(units []Unit) []string {
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.Name)
	}
	return names
}

// goldenCompare asserts got equals testdata/<file> byte-for-byte, regenerating it
// under -update (the recommend_test.go golden discipline).
func goldenCompare(t *testing.T, file, got string) {
	t.Helper()
	golden := filepath.Join("testdata", file)
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, []byte(got), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden %s (run with -update to create): %v", golden, err)
	}
	if got != string(want) {
		t.Errorf("rendered unit does not match %s.\n--- got ---\n%s\n--- want ---\n%s", file, got, want)
	}
}

// TestRenderContainerGolden: the rendered .container for a fixed RenderInput equals
// testdata/villa-llama.container.golden byte-for-byte (regen with -update).
func TestRenderContainerGolden(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama.container.golden", c.Text)
}

// codingFixtureInput is the deterministic RenderInput the coding-mode-ON golden is
// frozen against (CMODE-01, D-05). It mirrors fixtureInput() but carries the pre-
// translated coding-mode descriptor + the resolved agent ctx on RenderInput — exactly
// what the Plan-02 live wiring will populate after resolving the coder catalog entry.
// cfg.CodingMode + cfg.Coder* are set too (the persisted source of truth, D-04), though
// it is RenderInput.CodingMode that drives the render delta. The flag spellings baked
// into the golden are A1-confirmed against build-9496 (llama-server --help).
func codingFixtureInput(cacheReuseSafe bool) RenderInput {
	return RenderInput{
		Backend: inference.VulkanBackend(),
		Cfg: config.VillaConfig{
			Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
			CodingMode: true, CoderModel: "qwen3-coder-30b", CoderQuant: "UD-Q4_K_M", CoderAgentCtx: 65536,
		},
		ModelFile: "qwen3-coder-30b.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
		CodingMode: &inference.CodingModeSpec{
			Sampling:       &inference.Sampling{Temperature: 0.7, TopP: 0.8, TopK: 20, RepeatPenalty: 1.05},
			CacheReuseSafe: cacheReuseSafe,
		},
		CoderAgentCtx: 65536,
	}
}

// TestRenderCodingMode: with the coding-mode descriptor present, the rendered villa-llama
// unit matches the NEW append-only villa-llama-coding.container.golden byte-for-byte. The
// Exec line carries -c 65536 (the agent ctx, NOT the chat 131072), --jinja, the sampling
// preset, and --cache-reuse 256 (CacheReuseSafe=true). The off-path goldens are NOT
// regenerated — append-only discipline (D-02).
func TestRenderCodingMode(t *testing.T) {
	units, err := Render(codingFixtureInput(true))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama-coding.container.golden", c.Text)

	// Belt-and-braces structural assertions on the Exec line (independent of the golden):
	// the single -c carries the AGENT ctx (Pitfall 1), and the delta flags are present.
	if !strings.Contains(c.Text, "-c 65536") {
		t.Errorf("coding unit Exec must carry -c 65536 (the agent ctx, single -c):\n%s", c.Text)
	}
	if strings.Contains(c.Text, "-c 131072") {
		t.Errorf("coding unit Exec must NOT carry the chat ctx 131072 (Pitfall 1):\n%s", c.Text)
	}
	if strings.Count(c.Text, "-c ") != 1 {
		t.Errorf("coding unit Exec must have exactly one -c token:\n%s", c.Text)
	}
	for _, want := range []string{"--jinja", "--temp 0.7", "--top-p 0.8", "--top-k 20", "--repeat-penalty 1.05", "--cache-reuse 256"} {
		if !strings.Contains(c.Text, want) {
			t.Errorf("coding unit Exec missing %q:\n%s", want, c.Text)
		}
	}
}

// TestRenderCodingModeFailClosedCacheReuse: with the coder entry CacheReuseSafe=false,
// the on-path Exec line OMITS --cache-reuse (fail-closed render, D-03) while still
// carrying --jinja + the sampling preset.
func TestRenderCodingModeFailClosedCacheReuse(t *testing.T) {
	units, err := Render(codingFixtureInput(false))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if strings.Contains(c.Text, "--cache-reuse") {
		t.Errorf("coding unit must OMIT --cache-reuse when CacheReuseSafe=false (D-03 fail-closed):\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "--jinja") {
		t.Errorf("coding unit missing --jinja with reuse-off:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "--temp 0.7") {
		t.Errorf("coding unit missing the sampling preset with reuse-off:\n%s", c.Text)
	}
}

// TestRenderCodingModeOffPathUnchanged is the regression guard proving the delta is
// opt-in: with RenderInput.CodingMode == nil the off-path container golden is unchanged
// and still passes byte-for-byte (D-02). This is the same fixture as
// TestRenderContainerGolden but asserted here alongside the coding cases so a reviewer
// sees the opt-in contract in one place.
func TestRenderCodingModeOffPathUnchanged(t *testing.T) {
	in := fixtureInput() // CodingMode nil, CoderAgentCtx 0
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama.container.golden", c.Text)
	if strings.Contains(c.Text, "--jinja") || strings.Contains(c.Text, "--cache-reuse") {
		t.Errorf("off-path unit leaked a coding-mode flag (D-02 break):\n%s", c.Text)
	}
}

// rocmFixtureInput is the deterministic RenderInput the ROCm golden is frozen against.
// It mirrors fixtureInput() but selects the opt-in ROCm backend through the resolver
// (BackendFor) — the image/device/group/env delta is sourced THROUGH the seam, never
// hand-typed, so the golden tracks backend_rocm.go.
func rocmFixtureInput(t *testing.T) RenderInput {
	t.Helper()
	rocm, err := inference.BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	return RenderInput{
		Backend:   rocm,
		Cfg:       config.VillaConfig{Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "rocm"},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	}
}

// TestRenderROCmContainerGolden: the rendered ROCm .container equals
// testdata/villa-llama-rocm.container.golden byte-for-byte (regen with -update). The
// golden's delta over the Vulkan golden is exactly image + /dev/kfd + render group +
// HSA/hipBLASLt env (ROCM-03 / D-09) — a reviewer diffs the two units to see it.
func TestRenderROCmContainerGolden(t *testing.T) {
	units, err := Render(rocmFixtureInput(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	goldenCompare(t, "villa-llama-rocm.container.golden", c.Text)
}

// rocm644FixtureInput mirrors rocmFixtureInput but selects the v1.2 rocm-6.4.4
// backend through the resolver, so the additive golden tracks backend_rocm.go's
// 6.4.4 digest and render.go's widened backendLabel (Pitfall 2). The image/device/
// group/env delta is sourced THROUGH the seam, never hand-typed.
func rocm644FixtureInput(t *testing.T) RenderInput {
	t.Helper()
	rocm644, err := inference.BackendFor("rocm-6.4.4")
	if err != nil {
		t.Fatalf("BackendFor(rocm-6.4.4): %v", err)
	}
	return RenderInput{
		Backend:   rocm644,
		Cfg:       config.VillaConfig{Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "rocm-6.4.4"},
		ModelFile: "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	}
}

// TestRenderROCm644ContainerGolden: the rendered rocm-6.4.4 .container equals the
// ADDITIVE testdata/villa-llama-rocm-6.4.4.container.golden byte-for-byte. This is a
// NEW fixture (not a refreeze of the Vulkan/7.2.4 goldens, which stay byte-frozen
// per D-09). Its Description= must show the honest "ROCm 6.4.4 (HIP)" label, NOT the
// misleading "Vulkan RADV" default (Pitfall 2).
func TestRenderROCm644ContainerGolden(t *testing.T) {
	units, err := Render(rocm644FixtureInput(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "ROCm 6.4.4 (HIP)") {
		t.Errorf("rocm-6.4.4 unit Description must show the honest ROCm label, got:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "Vulkan RADV") {
		t.Errorf("rocm-6.4.4 unit must NOT fall through to the Vulkan RADV label:\n%s", c.Text)
	}
	goldenCompare(t, "villa-llama-rocm-6.4.4.container.golden", c.Text)
}

// TestRenderROCmEnvGroupFrozen mirrors TestRenderOpenWebUITelemetryFrozen: it guards
// against the Pitfall-1 silent drop of the second group-add or the env block even if
// the golden were regenerated wrong. It asserts the rendered ROCm unit contains EXACTLY
// the two expected Environment= lines AND both AddDevice= lines AND GroupAdd=render,
// using a count + full-set match (not a subset).
func TestRenderROCmEnvGroupFrozen(t *testing.T) {
	units, err := Render(rocmFixtureInput(t))
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")

	wantEnv := []string{
		"Environment=HSA_OVERRIDE_GFX_VERSION=11.5.1",
		"Environment=ROCBLAS_USE_HIPBLASLT=1",
	}
	for _, line := range wantEnv {
		if !strings.Contains(c.Text, line) {
			t.Errorf("ROCm unit missing frozen env line %q (Pitfall-1 silent-drop guard):\n%s", line, c.Text)
		}
	}
	if got := strings.Count(c.Text, "Environment="); got != len(wantEnv) {
		t.Errorf("ROCm unit has %d Environment= lines, want exactly %d (env set drifted):\n%s", got, len(wantEnv), c.Text)
	}

	wantDevices := []string{"AddDevice=/dev/kfd", "AddDevice=/dev/dri"}
	for _, line := range wantDevices {
		if !strings.Contains(c.Text, line) {
			t.Errorf("ROCm unit missing device line %q:\n%s", line, c.Text)
		}
	}
	if got := strings.Count(c.Text, "AddDevice="); got != len(wantDevices) {
		t.Errorf("ROCm unit has %d AddDevice= lines, want exactly %d:\n%s", got, len(wantDevices), c.Text)
	}

	// keep-groups ONLY: the render GID is carried into the container by keep-groups,
	// and podman REJECTS keep-groups combined with any other --group-add (CR-G1). The
	// unit must therefore have EXACTLY one GroupAdd= line.
	wantGroups := []string{"GroupAdd=keep-groups"}
	for _, line := range wantGroups {
		if !strings.Contains(c.Text, line) {
			t.Errorf("ROCm unit missing group line %q:\n%s", line, c.Text)
		}
	}
	if strings.Contains(c.Text, "GroupAdd=render") {
		t.Errorf("ROCm unit has an illegal GroupAdd=render (podman rejects keep-groups + any other --group-add, CR-G1):\n%s", c.Text)
	}
	if got := strings.Count(c.Text, "GroupAdd="); got != len(wantGroups) {
		t.Errorf("ROCm unit has %d GroupAdd= lines, want exactly %d:\n%s", got, len(wantGroups), c.Text)
	}
}

// TestRenderNetworkGolden: the .network (NetworkName=villa) and the models .volume
// goldens match; the container DNS name is villa-llama.
func TestRenderNetworkGolden(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	n := unitByName(t, units, "villa.network")
	goldenCompare(t, "villa.network.golden", n.Text)
	if !strings.Contains(n.Text, "NetworkName=villa") {
		t.Errorf(".network missing NetworkName=villa:\n%s", n.Text)
	}

	v := unitByName(t, units, "villa-models.volume")
	goldenCompare(t, "villa-models.volume.golden", v.Text)

	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "ContainerName=villa-llama") {
		t.Errorf(".container missing stable DNS name ContainerName=villa-llama:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "Network=villa.network") {
		t.Errorf(".container missing Network=villa.network:\n%s", c.Text)
	}
}

// TestInstallSectionPresent: the rendered .container contains [Install] and
// WantedBy=default.target (ORCH-03).
func TestInstallSectionPresent(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "[Install]") {
		t.Errorf(".container missing [Install] section:\n%s", c.Text)
	}
	if !strings.Contains(c.Text, "WantedBy=default.target") {
		t.Errorf(".container missing WantedBy=default.target:\n%s", c.Text)
	}
}

// TestRenderFiveUnitOrder: Render grows from 3 to 5 units (D-02) in a fixed
// deterministic order — callers and goldens depend on this exact sequence.
func TestRenderFiveUnitOrder(t *testing.T) {
	units, err := Render(fixtureInput())
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
			t.Errorf("unit[%d] = %q, want %q (full order: %v)", i, got[i], want[i], got)
		}
	}
}

// TestRenderOpenWebUIContainerGolden: the villa-openwebui.container unit matches its
// golden byte-for-byte (regen with -update).
func TestRenderOpenWebUIContainerGolden(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.golden", c.Text)
}

// TestRenderOpenWebUIMemoryContainerGolden: the memory-ON villa-openwebui.container
// unit matches its dedicated golden byte-for-byte (Phase-20 D-05). This is the single
// deliberate re-freeze target for the appended D-09 RAG/Qdrant/memory env block (24
// Environment= lines incl. ENABLE_PERSISTENT_CONFIG=False). The memory-OFF golden
// (TestRenderOpenWebUIContainerGolden above) MUST stay byte-identical — do NOT regen
// it; only this memory golden is intentionally re-frozen with -update.
func TestRenderOpenWebUIMemoryContainerGolden(t *testing.T) {
	units, err := Render(memoryFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.memory.golden", c.Text)
}

// TestRenderOpenWebUIWebSearchContainerGolden: the web-search-ON villa-openwebui.container
// unit matches its dedicated golden byte-for-byte (Phase-30 D-08, SC#1). This is the single
// deliberate re-freeze target for the appended web-search env block (ENABLE_WEB_SEARCH=True,
// WEB_SEARCH_ENGINE=searxng, SEARXNG_QUERY_URL, WEB_SEARCH_RESULT_COUNT) + a single trailing
// ENABLE_PERSISTENT_CONFIG=False. The memory-OFF / memory-ON goldens MUST stay byte-identical
// — only this web-search golden is intentionally re-frozen with -update.
func TestRenderOpenWebUIWebSearchContainerGolden(t *testing.T) {
	// Phase-31 GOLDEN-FREEZE DEFERRAL: this plan (31-03) wires the external-loader keys
	// (WEB_LOADER_ENGINE=external + EXTERNAL_WEB_LOADER_URL), flips
	// BYPASS_WEB_SEARCH_EMBEDDING_AND_RETRIEVAL True→False, adds the retrieval-fix key
	// (ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS), and carries the bearer via a 0600
	// EnvironmentFile on the OWUI unit — but the search-ON OWUI golden is NOT re-frozen here.
	// The retrieval-fix key is the A1 HIGH-risk unknown: it must be CONFIRMED to exist + to
	// actually ground at the pinned OWUI digest ON-HARDWARE (Plan 31-04) before the golden is
	// frozen. Plan 04 re-runs this with -update after on-hardware confirmation. The drift test
	// (TestRenderOpenWebUITelemetryFrozen websearch-on case) already binds every web-search
	// KEY to buildOpenWebUIView by construction, so env-name regressions are still caught now;
	// only the byte-for-byte golden assertion is deferred.
	t.Skip("re-frozen in Plan 31-04 after on-hardware retrieval-fix (ENABLE_RETRIEVAL_UNSCOPED_COLLECTIONS) confirmation")

	units, err := Render(searxngFixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	goldenCompare(t, "villa-openwebui.container.websearch.golden", c.Text)
}

// TestRenderOpenWebUIWebSearchConfigDriven (WR-01): the OWUI SEARXNG_QUERY_URL host:port is
// composed from the resolved cfg.SearxngAddr/cfg.SearxngPort, NOT an orchestrate-local const.
// A non-default addr+port must surface in the rendered SEARXNG_QUERY_URL. Do NOT couple any
// assertion to OWUI forwarding &format=json from the URL (Pitfall 1: at this digest OWUI
// strips the URL query string and supplies format=json itself — the suffix is frozen for SC#1
// literal compliance only, not relied on for grounding).
func TestRenderOpenWebUIWebSearchConfigDriven(t *testing.T) {
	in := searxngFixtureInput()
	in.Cfg.SearxngAddr = "villa-searxng-custom"
	in.Cfg.SearxngPort = 9090
	units, err := Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	want := "Environment=SEARXNG_QUERY_URL=http://villa-searxng-custom:9090/search?q=<query>&format=json"
	if !strings.Contains(c.Text, want) {
		t.Errorf("OWUI SEARXNG_QUERY_URL not composed from the config-resolved host:port (rendered from a const?):\nwant substring %q\n%s", want, c.Text)
	}
}

// TestRenderOpenWebUIPersistentConfigSingleEmit (Phase-30 D-04, Pitfall 2): the load-bearing
// ENABLE_PERSISTENT_CONFIG=False is emitted EXACTLY once, as the LAST Environment= line, for
// BOTH web-on/memory-off (searxngFixtureInput) AND memory-on/web-off (memoryFixtureInput) —
// catching a duplicate (one emit per group) or a drop (omitted when web is on but memory off).
func TestRenderOpenWebUIPersistentConfigSingleEmit(t *testing.T) {
	cases := []struct {
		name string
		in   RenderInput
	}{
		{name: "websearch-on/memory-off", in: searxngFixtureInput()},
		{name: "memory-on/websearch-off", in: memoryFixtureInput()},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			units, err := Render(tc.in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			c := unitByName(t, units, "villa-openwebui.container")
			if got := strings.Count(c.Text, "Environment=ENABLE_PERSISTENT_CONFIG="); got != 1 {
				t.Errorf("ENABLE_PERSISTENT_CONFIG emitted %d times, want exactly 1 (D-04 single-emit):\n%s", got, c.Text)
			}
			// It must be the LAST Environment= line.
			envLines := []string{}
			for _, line := range strings.Split(c.Text, "\n") {
				if strings.HasPrefix(line, "Environment=") {
					envLines = append(envLines, line)
				}
			}
			if len(envLines) == 0 {
				t.Fatalf("no Environment= lines rendered:\n%s", c.Text)
			}
			last := envLines[len(envLines)-1]
			if last != "Environment=ENABLE_PERSISTENT_CONFIG=False" {
				t.Errorf("last Environment= line = %q, want Environment=ENABLE_PERSISTENT_CONFIG=False (must be LAST):\n%s", last, c.Text)
			}
		})
	}
}

// TestRenderOpenWebUIVolumeGolden: the villa-openwebui.volume unit matches its golden.
func TestRenderOpenWebUIVolumeGolden(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	v := unitByName(t, units, "villa-openwebui.volume")
	goldenCompare(t, "villa-openwebui.volume.golden", v.Text)
}

// TestRenderOpenWebUITelemetryFrozen is the PRIV-02 re-audit-on-bump guard (D-07): it
// asserts every telemetry-kill var AND every connection var is present in the rendered
// Open WebUI unit. A deliberate single-env-line removal in the template turns it red,
// forcing a re-audit of the privacy posture on any image bump. It complements the
// container golden (which catches ANY drift) by documenting the load-bearing intent
// and surviving incidental whitespace edits.
func TestRenderOpenWebUITelemetryFrozen(t *testing.T) {
	// Bidirectional freeze (WR-02): derive the expected env from the single source of
	// truth (buildOpenWebUIView), require EVERY Key=Value line is rendered, AND assert
	// the rendered unit carries EXACTLY that many Environment= lines. A subset check
	// would let a contributor add/drop a var (e.g. a new telemetry channel, or dropping
	// OPENAI_API_KEY) without tripping this guard — only the byte golden would catch it.
	// Counting + full-set matching makes the PRIV-02 re-audit-on-bump guarantee real,
	// not merely decorative.
	//
	// Phase-20 (D-05) memory-aware re-audit: the env set now depends on memory_enabled.
	// Assert BOTH views against the SAME buildOpenWebUIView source of truth — the
	// memory-ON unit carries exactly the memory-ON view's lines (24, incl. the appended
	// D-09 block + ENABLE_PERSISTENT_CONFIG=False) and the memory-OFF unit carries
	// exactly the 11 baseline lines (byte-identical to the v1.2 golden). This re-confirms
	// the telemetry-kill posture (ANONYMIZED_TELEMETRY/DO_NOT_TRACK/SCARF_NO_ANALYTICS/
	// OFFLINE_MODE/HF_HUB_OFFLINE) survives in BOTH views.
	cases := []struct {
		name string
		in   RenderInput
		env  []envPair
	}{
		{
			name: "memory-off",
			in:   fixtureInput(),
			env:  buildOpenWebUIView(memory.RenderView(fixtureInput().Cfg), false, false, "", 0, 0, "", 0).Env,
		},
		{
			name: "memory-on",
			in:   memoryFixtureInput(),
			env:  buildOpenWebUIView(memory.RenderView(memoryFixtureInput().Cfg), true, false, "", 0, 0, "", 0).Env,
		},
		{
			// Phase-30 SC#4 drift guard: the web-search-on view binds every web-search
			// KEY to buildOpenWebUIView's output. Because the loop below asserts each
			// Key=Value line is rendered AND that Environment= line COUNT == len(env),
			// an env-name regression (e.g. reverting ENABLE_WEB_SEARCH to the old
			// ENABLE_RAG_WEB_SEARCH name) or a duplicated/dropped ENABLE_PERSISTENT_CONFIG
			// fails by construction. searxngFixtureInput() is WebSearchEnabled (memory
			// off); the literal 3 matches its WebSearchResultCount and the rendered unit.
			name: "websearch-on",
			in:   searxngFixtureInput(),
			env:  buildOpenWebUIView(memory.RenderView(searxngFixtureInput().Cfg), false, true, "villa-searxng", 8080, 3, "villa-websafe", 8090).Env,
		},
	}

	// Telemetry-kill set that MUST survive in BOTH views (D-05 re-audit).
	telemetryKill := []string{
		"Environment=ANONYMIZED_TELEMETRY=False",
		"Environment=DO_NOT_TRACK=True",
		"Environment=SCARF_NO_ANALYTICS=True",
		"Environment=OFFLINE_MODE=True",
		"Environment=HF_HUB_OFFLINE=1",
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			units, err := Render(tc.in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			c := unitByName(t, units, "villa-openwebui.container")

			for _, p := range tc.env {
				want := "Environment=" + p.Key + "=" + p.Value
				if !strings.Contains(c.Text, want) {
					t.Errorf("Open WebUI unit missing frozen env line %q (PRIV-02 re-audit guard):\n%s", want, c.Text)
				}
			}
			got := strings.Count(c.Text, "Environment=")
			if got != len(tc.env) {
				t.Errorf("Open WebUI unit has %d Environment= lines, want exactly %d — the env set drifted from buildOpenWebUIView (PRIV-02 re-audit: update this test AND re-confirm the telemetry-kill posture):\n%s", got, len(tc.env), c.Text)
			}
			for _, k := range telemetryKill {
				if !strings.Contains(c.Text, k) {
					t.Errorf("Open WebUI %s unit dropped telemetry-kill line %q (PRIV-02/D-05 re-audit):\n%s", tc.name, k, c.Text)
				}
			}
		})
	}
}

// TestRenderOpenWebUILoopbackOnly: the Open WebUI unit publishes loopback only
// (127.0.0.1:3000:8080) and contains no 0.0.0.0: host-publish (PRIV-01 continuity, D-04).
func TestRenderOpenWebUILoopbackOnly(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	if !strings.Contains(c.Text, "PublishPort=127.0.0.1:3000:8080") {
		t.Errorf("Open WebUI unit missing loopback PublishPort:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "PublishPort=0.0.0.0:") {
		t.Errorf("Open WebUI unit publishes a 0.0.0.0 host port (privacy leak):\n%s", c.Text)
	}
}

// TestRenderOpenWebUIVolumeMount: the container mounts the named :Z data volume at
// /app/backend/data (D-11), and the volume unit is a plain named volume with no bind
// fields.
func TestRenderOpenWebUIVolumeMount(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")
	if !strings.Contains(c.Text, "Volume=villa-openwebui.volume:/app/backend/data:Z") {
		t.Errorf("Open WebUI unit missing named :Z volume mount:\n%s", c.Text)
	}
	v := unitByName(t, units, "villa-openwebui.volume")
	if !strings.Contains(v.Text, "VolumeName=villa-openwebui") {
		t.Errorf(".volume missing VolumeName=villa-openwebui:\n%s", v.Text)
	}
	if strings.Contains(v.Text, "Device=") || strings.Contains(v.Text, "Options=bind") {
		t.Errorf(".volume must be a plain named volume (no Device=/Options=bind):\n%s", v.Text)
	}
}

// TestRenderedPublishLoopbackOnly: the rendered .container publishes loopback only
// (127.0.0.1) and contains no 0.0.0.0: host-publish substring (mirrors Phase-2
// TestLoopbackPublish; ORCH-04/PRIV-01).
func TestRenderedPublishLoopbackOnly(t *testing.T) {
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-llama.container")
	if !strings.Contains(c.Text, "PublishPort=127.0.0.1:8080:8080") {
		t.Errorf(".container missing loopback PublishPort:\n%s", c.Text)
	}
	if strings.Contains(c.Text, "PublishPort=0.0.0.0:") {
		t.Errorf(".container publishes a 0.0.0.0 host port (privacy leak):\n%s", c.Text)
	}
}

// TestBackendLabelROCmFamily asserts backendLabel maps each ROCm-family name to a
// distinct, honest ROCm Description= label (Pitfall 2) — a 6.4.4 unit must NOT
// fall through to the misleading "Vulkan RADV" default — while the Vulkan default
// and the existing rocm-7.2.4 label stay byte-unchanged (D-09 additivity).
func TestBackendLabelROCmFamily(t *testing.T) {
	cases := map[string]string{
		"rocm":               "ROCm 7.2.4 (HIP)",
		"rocm-6.4.4":         "ROCm 6.4.4 (HIP)",
		"rocm-6.4.4-rocwmma": "ROCm 6.4.4 rocWMMA (HIP)",
		"vulkan":             "Vulkan RADV",
		"":                   "Vulkan RADV",
	}
	for name, want := range cases {
		if got := backendLabel(name); got != want {
			t.Errorf("backendLabel(%q) = %q, want %q", name, got, want)
		}
	}
}
