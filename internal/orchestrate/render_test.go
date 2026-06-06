package orchestrate

import (
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
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
	units, err := Render(fixtureInput())
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	c := unitByName(t, units, "villa-openwebui.container")

	// Bidirectional freeze (WR-02): derive the expected env from the single source of
	// truth (buildOpenWebUIView), require EVERY Key=Value line is rendered, AND assert
	// the rendered unit carries EXACTLY that many Environment= lines. A subset check
	// would let a contributor add/drop a var (e.g. a new telemetry channel, or dropping
	// OPENAI_API_KEY) without tripping this guard — only the byte golden would catch it.
	// Counting + full-set matching makes the PRIV-02 re-audit-on-bump guarantee real,
	// not merely decorative.
	env := buildOpenWebUIView().Env
	for _, p := range env {
		want := "Environment=" + p.Key + "=" + p.Value
		if !strings.Contains(c.Text, want) {
			t.Errorf("Open WebUI unit missing frozen env line %q (PRIV-02 re-audit guard):\n%s", want, c.Text)
		}
	}
	got := strings.Count(c.Text, "Environment=")
	if got != len(env) {
		t.Errorf("Open WebUI unit has %d Environment= lines, want exactly %d — the env set drifted from buildOpenWebUIView (PRIV-02 re-audit: update this test AND re-confirm the telemetry-kill posture):\n%s", got, len(env), c.Text)
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
