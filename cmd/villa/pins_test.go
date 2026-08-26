package main

// pins_test.go proves the loop closes: a pin recorded in the state store reaches
// the rendered unit, for every component the update path can move.
//
// The migration this guards is the one the spec singles out as hazardous. Most
// EmbedImage() callers are NOT pins — they use it as a generic "helper image that
// ships curl" for probes — so a mechanical find-and-replace would have dragged the
// probe machinery into the update path. TestProbeHelpersAreNotPinned asserts that
// did not happen, and would fail if a later change did it.

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
)

// pinnedFixtureConfig turns every optional subsystem on, so one render produces
// every unit a pin can appear in. Without memory and web search enabled, four of
// the five managed-service components render no unit at all and the assertions
// below would pass vacuously.
func pinnedFixtureConfig() config.VillaConfig {
	return config.VillaConfig{
		Model:            "qwen3-30b",
		Quant:            "Q4_K_M",
		Ctx:              8192,
		Backend:          "vulkan",
		MemoryEnabled:    true,
		EmbeddingModel:   "nomic-embed-text-v1.5",
		EmbeddingDim:     768,
		WebSearchEnabled: true,
	}
}

// renderWithState renders the whole stack against a given pin state, through the
// same seams livePinnedRender wires.
func renderWithState(t *testing.T, state pinstate.State) []orchestrate.Unit {
	t.Helper()
	cfg := pinnedFixtureConfig()
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		t.Fatalf("BackendFor: %v", err)
	}
	r := pinresolve.New(state)
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:       livePinnedBackend(r, backend),
		Cfg:           cfg,
		ModelFile:     "model.gguf",
		ModelsDir:     "/models",
		HostVillaPath: "/usr/local/bin/villa",
		Pin:           livePinFunc(r),
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	return units
}

// unitsText joins every rendered unit, for substring assertions over the whole
// stack rather than per file.
func unitsText(units []orchestrate.Unit) string {
	var b strings.Builder
	for _, u := range units {
		b.WriteString(u.Text)
		b.WriteString("\n")
	}
	return b.String()
}

// TestAnEmptyStoreRendersTheVettedPins is the byte-identical guarantee. With no pin
// state — every host that has never run `villa update` — the rendered stack must
// carry exactly the compiled-in pins, or this migration changed behaviour it
// promised not to.
func TestAnEmptyStoreRendersTheVettedPins(t *testing.T) {
	units := renderWithState(t, pinstate.State{})
	text := unitsText(units)

	for _, e := range pins.Table() {
		if e.Shape == pins.ChecksummedAsset {
			continue // the Crush binary renders no unit
		}
		ref := e.Vetted().Ref
		// The three non-active backends render nothing, so only assert the ones
		// that appear.
		switch e.Component {
		case pins.BackendROCm724, pins.BackendROCm644, pins.BackendROCm644WMMA:
			continue
		}
		if !strings.Contains(text, ref) {
			t.Errorf("%s: the vetted pin does not appear in the rendered stack", e.Component)
		}
	}
}

// TestARecordedPinReachesTheRenderedUnit is the loop closing, per component. Each
// of the six render-path sites is driven independently, because a single combined
// assertion would pass while five of the six were unwired.
func TestARecordedPinReachesTheRenderedUnit(t *testing.T) {
	cases := []struct {
		component pins.ComponentID
		ref       string
	}{
		{pins.BackendVulkan, "example.invalid/backend@sha256:aaaa"},
		{pins.OpenWebUI, "example.invalid/owui@sha256:bbbb"},
		{pins.Qdrant, "example.invalid/qdrant@sha256:cccc"},
		{pins.Embedder, "example.invalid/embed@sha256:dddd"},
		{pins.SearXNG, "example.invalid/searxng@sha256:eeee"},
		{pins.Websafe, "example.invalid/websafe@sha256:ffff"},
	}

	for _, tc := range cases {
		t.Run(string(tc.component), func(t *testing.T) {
			state := pinstate.State{Pins: map[string]pinstate.Effective{
				string(tc.component): {Ref: tc.ref},
			}}
			text := unitsText(renderWithState(t, state))

			if !strings.Contains(text, tc.ref) {
				t.Errorf("the recorded effective pin %q never reached a rendered unit; "+
					"an update to %s would record a pin nothing runs", tc.ref, tc.component)
			}

			// The pin it replaced must be gone, or the update would leave the host
			// running the old image while claiming the new one.
			entry, ok := pins.Lookup(tc.component)
			if !ok {
				t.Fatalf("%s is not in the table", tc.component)
			}
			vetted := entry.Vetted().Ref
			// The embedder and the vulkan backend share one vetted pin today, so
			// repinning one legitimately leaves the other still naming it.
			if tc.component != pins.Embedder && tc.component != pins.BackendVulkan {
				if strings.Contains(text, vetted) {
					t.Errorf("the superseded pin %q is still rendered alongside the effective one", vetted)
				}
			}
		})
	}
}

// TestRepinningOneOfTwoRolesLeavesTheOtherAlone is the shared-image case, and the
// reason the embedder and the vulkan backend are two components rather than one.
// They resolve to the same digest today, so updating memory must move the embedder
// unit and leave the inference unit exactly as it was.
func TestRepinningOneOfTwoRolesLeavesTheOtherAlone(t *testing.T) {
	entry, _ := pins.Lookup(pins.Embedder)
	shared := entry.Vetted().Ref
	newEmbed := "example.invalid/embed@sha256:9999"

	units := renderWithState(t, pinstate.State{Pins: map[string]pinstate.Effective{
		string(pins.Embedder): {Ref: newEmbed},
	}})

	var embedUnit, llamaUnit string
	for _, u := range units {
		switch {
		case strings.Contains(u.Name, "embed"):
			embedUnit = u.Text
		case u.Name == "villa-llama.container":
			llamaUnit = u.Text
		}
	}
	if embedUnit == "" || llamaUnit == "" {
		t.Fatal("the fixture did not render both the embed and the inference unit")
	}
	if !strings.Contains(embedUnit, newEmbed) {
		t.Error("the embedder unit did not take the recorded pin")
	}
	if !strings.Contains(llamaUnit, shared) {
		t.Error("the inference unit lost the shared vetted pin; updating memory moved the backend too")
	}
	if strings.Contains(llamaUnit, newEmbed) {
		t.Error("the inference unit took the embedder's pin; two roles were collapsed into one component")
	}
}

// TestRepinnedBackendKeepsEverythingButTheImage: the backend's pin is applied by
// wrapping the seam, so every device arg, env var and residency marker must survive
// unchanged. Substituting markers along with the image would break the residency
// proof on exactly the runs an update most needs it to work.
func TestRepinnedBackendKeepsEverythingButTheImage(t *testing.T) {
	backend, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("BackendFor: %v", err)
	}
	const ref = "example.invalid/backend@sha256:1234"
	repinned := inference.Repinned(backend, ref)

	if repinned.Name() != backend.Name() {
		t.Errorf("Name changed to %q; the name selects the residency markers and the preflight gate", repinned.Name())
	}
	if repinned.Image() != ref {
		t.Errorf("Image = %q, want the replacement ref", repinned.Image())
	}
	if repinned.ResidencyProof() != backend.ResidencyProof() {
		t.Error("the residency markers changed; the offload assert would stop matching")
	}

	spec := inference.RunSpec{ContainerName: "villa-llama", ModelFile: "m.gguf", ModelsDir: "/models", ContextLen: 4096}
	before, after := backend.ContainerArgs(spec), repinned.ContainerArgs(spec)
	if len(before) != len(after) {
		t.Fatalf("the argument slice changed length: %d then %d", len(before), len(after))
	}
	diffs := 0
	for i := range before {
		if before[i] != after[i] {
			diffs++
			if after[i] != ref {
				t.Errorf("arg %d changed to %q, which is not the replacement ref", i, after[i])
			}
		}
	}
	if diffs != 1 {
		t.Errorf("%d args changed; exactly one (the image token) should have", diffs)
	}
}

// TestAnEmptyRefIsNotRepinned: the fresh-install path. Repinned must return the
// backend unchanged rather than wrapping it with a blank image, which would render
// a unit with no Image= line.
func TestAnEmptyRefIsNotRepinned(t *testing.T) {
	backend, _ := inference.BackendFor("vulkan")
	if got := inference.Repinned(backend, ""); got.Image() != backend.Image() {
		t.Errorf("an empty ref produced image %q, want the compiled-in pin", got.Image())
	}
}

// TestProbeHelpersAreNotPinned is the §7.1 hazard as a test.
//
// EmbedImage() has roughly ten callers and MOST ARE NOT PINS: verify_memory,
// verify_search, verify_agent, doctor, status and install_searxng use it as a
// generic "helper image that ships curl" for a throwaway probe container. A probe
// helper has no effective pin — it is not a service this host runs, it is a tool
// villa shells out with — and routing one through the resolver would drag the probe
// machinery into the update path.
//
// This asserts those call sites still read the accessor directly. It fails if a
// future mechanical rewrite of EmbedImage() sweeps them up.
func TestProbeHelpersAreNotPinned(t *testing.T) {
	probeFiles := []string{
		"verify_memory.go",
		"verify_search.go",
		"verify_agent.go",
		"doctor.go",
		"status.go",
		"install_searxng.go",
		"install_memory.go",
	}

	for _, name := range probeFiles {
		data, err := os.ReadFile(filepath.Join(".", name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		src := string(data)
		if !strings.Contains(src, "orchestrate.EmbedImage()") {
			t.Errorf("%s no longer calls orchestrate.EmbedImage() directly; a probe helper has no effective pin "+
				"and must not be routed through the resolver (spec §7.1)", name)
		}
		if strings.Contains(src, "livePinFunc") || strings.Contains(src, "liveResolver()") {
			t.Errorf("%s resolves a pin; it uses the embed image as a curl-shipping probe helper, not as a pin", name)
		}
	}
}

// TestEveryRenderCallGoesThroughThePinnedEntryPoint: eight independently-wired call
// sites would eventually be seven, and the one that got missed would silently
// ignore every recorded pin. The rule is enforced rather than remembered.
//
// It matches the symbol in every form it is USED, not just as a call, because the
// seam is wired BY VALUE in two places (`render: orchestrate.Render` in install and
// lifecycle, `Render: orchestrate.Render` in status). Matching only `Render(` would
// let those two slip through, which is exactly the shape the test exists to catch.
//
// The two forms are matched explicitly rather than by a bare substring, because
// orchestrate exports several sibling Render* functions — RenderSearxngSettings,
// RenderWebsafeSecretEnv, RenderSearxngSecretEnv, RenderDashboardUnit — that render
// non-unit artifacts, carry no image, and legitimately stay direct calls.
func TestEveryRenderCallGoesThroughThePinnedEntryPoint(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") || name == "pins.go" {
			continue
		}
		data, err := os.ReadFile(name)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		for i, line := range strings.Split(string(data), "\n") {
			code := line
			if idx := strings.Index(code, "//"); idx >= 0 {
				code = code[:idx] // a comment may name the function freely
			}
			// A call, and the two by-value wirings: the symbol followed by "(" or
			// by a comma / end of line.
			leaked := strings.Contains(code, "orchestrate.Render(") ||
				strings.Contains(code, "orchestrate.Render,") ||
				strings.HasSuffix(strings.TrimSpace(code), "orchestrate.Render")
			if leaked {
				t.Errorf("%s:%d references orchestrate.Render directly; use livePinnedRender so a recorded effective pin reaches the unit:\n  %s",
					name, i+1, strings.TrimSpace(line))
			}
		}
	}
}
