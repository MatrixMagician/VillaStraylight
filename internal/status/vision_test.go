package status

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestReportCarriesVision asserts the report says whether the running unit has
// vision. A user about to attach an image in chat asks status first, and the
// persisted decision is what the unit was actually started with.
func TestReportCarriesVision(t *testing.T) {
	for _, want := range []bool{false, true} {
		d := newDeps(t, loopbackUnits(t))
		base := d.LoadConfig
		d.LoadConfig = func() (config.VillaConfig, error) {
			cfg, err := base()
			cfg.Vision = want
			return cfg, err
		}
		if got := Run(d); got.Vision != want {
			t.Errorf("Vision = %v, want %v", got.Vision, want)
		}
	}
}
