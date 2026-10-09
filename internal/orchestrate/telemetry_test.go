package orchestrate

import (
	"strings"
	"testing"
)

// telemetrySwitches is the zero-telemetry constraint (outbound limited to image and
// model pulls) as data: every rendered unit whose pinned image phones home by
// default, with the Environment= lines that switch it off. Each entry was read off
// the pinned image (issue #348): Qdrant's config.yaml ships telemetry_disabled:
// false; Open WebUI and the Kokoro hub client read the keys below.
var telemetrySwitches = map[string][]string{
	"villa-qdrant.container": {"Environment=QDRANT__TELEMETRY_DISABLED=true"},
	"villa-openwebui.container": {
		"Environment=ANONYMIZED_TELEMETRY=False",
		"Environment=DO_NOT_TRACK=True",
		"Environment=SCARF_NO_ANALYTICS=True",
	},
	"villa-tts.container": {"Environment=HF_HUB_DISABLE_TELEMETRY=1"},
}

// telemetryAudited names the rendered containers with no telemetry switch, and why.
// A new container unit fails TestEveryContainerUnitAuditedForTelemetry until it is
// added to one of the two tables.
var telemetryAudited = map[string]string{
	"villa-llama":      "llama-server has no reporting code",
	"villa-embed":      "llama-server has no reporting code",
	"villa-rerank":     "llama-server has no reporting code",
	"villa-stt":        "whisper.cpp server has no reporting code",
	"villa-image":      "stable-diffusion.cpp sd-server has no reporting code",
	"villa-extract":    "Apache Tika server has no reporting code",
	"villa-searxng":    "no telemetry key in its settings; its upstream search queries are the feature",
	"villa-websafe":    "villa binary in a distroless image",
	"villa-inferproxy": "villa binary in a distroless image",
}

func telemetryFixtures() []RenderInput {
	return []RenderInput{
		memoryFixtureInput(), extractFixtureInput(), rerankFixtureInput(), imageFixtureInput(),
		voiceFixtureInput(), searxngFixtureInput(), sandboxOnFixtureInput(), residentFixtureInput(),
	}
}

// TestTelemetrySwitchesRendered guards the zero-telemetry constraint: each unit in
// telemetrySwitches renders every line that turns its service's reporting off.
func TestTelemetrySwitchesRendered(t *testing.T) {
	for unit, want := range telemetrySwitches {
		found := false
		for _, in := range telemetryFixtures() {
			units, err := Render(in)
			if err != nil {
				t.Fatalf("Render: %v", err)
			}
			for _, u := range units {
				if u.Name != unit {
					continue
				}
				found = true
				for _, line := range want {
					if !strings.Contains(u.Text, "\n"+line+"\n") {
						t.Errorf("%s lacks telemetry switch %q:\n%s", unit, line, u.Text)
					}
				}
			}
		}
		if !found {
			t.Errorf("no fixture renders %s", unit)
		}
	}
}

// TestEveryContainerUnitAuditedForTelemetry: every rendered container is either in
// telemetrySwitches or in telemetryAudited, so adding a service forces the audit.
func TestEveryContainerUnitAuditedForTelemetry(t *testing.T) {
	for _, in := range telemetryFixtures() {
		units, err := Render(in)
		if err != nil {
			t.Fatalf("Render: %v", err)
		}
		for _, u := range units {
			if !strings.HasSuffix(u.Name, ".container") {
				continue
			}
			if _, ok := telemetrySwitches[u.Name]; ok {
				continue
			}
			base := strings.TrimSuffix(u.Name, ".container")
			audited := false
			for prefix := range telemetryAudited {
				if base == prefix || strings.HasPrefix(base, prefix+"-") {
					audited = true
				}
			}
			if !audited {
				t.Errorf("%s is neither in telemetrySwitches nor telemetryAudited", u.Name)
			}
		}
	}
}
