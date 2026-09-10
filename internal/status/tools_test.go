package status

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestReportCarriesTheAnsweredToolsGate asserts the report says whether the running
// unit serves tool calls, and that it reports the ANSWERED gate: coding mode implies
// tools mode, so a coding-mode stack that read only the raw flag would tell an
// operator no tools were served while the unit served them (spec v1.11 §3.5).
func TestReportCarriesTheAnsweredToolsGate(t *testing.T) {
	for _, tc := range []struct {
		name  string
		apply func(*config.VillaConfig)
		want  bool
	}{
		{"off by default", func(*config.VillaConfig) {}, false},
		{"explicit tools mode", func(c *config.VillaConfig) { c.ToolsMode = true }, true},
		{"implied by coding mode", func(c *config.VillaConfig) { c.CodingMode = true }, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDeps(t, loopbackUnits(t))
			base := d.LoadConfig
			d.LoadConfig = func() (config.VillaConfig, error) {
				cfg, err := base()
				tc.apply(&cfg)
				return cfg, err
			}
			if got := Run(d); got.Tools != tc.want {
				t.Errorf("Tools = %v, want %v", got.Tools, tc.want)
			}
		})
	}
}
