package status

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// TestReportMarksAnOperatorSuppliedModel (ADR-0041): the report carries the origin of
// the configured model when it is not the seed's own, so `status --json` and the
// dashboard (which serves the same document) can say so.
func TestReportMarksAnOperatorSuppliedModel(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	d.ModelOrigin = func(cfg config.VillaConfig) string { return "operator-supplied" }
	r := Run(d)
	if r.ModelOrigin != "operator-supplied" {
		t.Fatalf("ModelOrigin = %q", r.ModelOrigin)
	}
	b, err := json.Marshal(r)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(b), `"model_origin":"operator-supplied"`) {
		t.Fatalf("the JSON contract lacks model_origin: %s", b)
	}
}

// TestReportOmitsTheOriginForASeedModel: a seed model, an unwired seam and an
// unresolvable model all leave the key out, so the contract differs from v12 only in
// schema_version and a consumer never reads an absent origin as a verdict.
func TestReportOmitsTheOriginForASeedModel(t *testing.T) {
	for name, seam := range map[string]func(config.VillaConfig) string{
		"seam returns empty": func(config.VillaConfig) string { return "" },
		"seam unwired":       nil,
	} {
		t.Run(name, func(t *testing.T) {
			d := newDeps(t, loopbackUnits(t))
			d.ModelOrigin = seam
			b, err := json.Marshal(Run(d))
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(b), "model_origin") {
				t.Fatalf("model_origin must be omitted: %s", b)
			}
		})
	}
}

// TestReportSchemaVersionIsFourteen: the additive key is a contract change, so the
// report's own version moves with it (every additive change bumps it). v13 was the
// tool server's rows (ADR-0042); the model origin is v14.
func TestReportSchemaVersionIsFourteen(t *testing.T) {
	if got := Run(newDeps(t, loopbackUnits(t))).SchemaVersion; got != 14 || SchemaVersion() != 14 {
		t.Fatalf("schema_version = %d, SchemaVersion() = %d, want 14", got, SchemaVersion())
	}
}
