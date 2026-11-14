package doctor

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
)

// TestCapabilityFindingsCrossCheckTheServedModel is CAP-01 (vision) and CAP-02
// (tool support): config claims against /props evidence. A config that claims nothing
// emits no finding; an unreadable or silent /props is a typed-Unknown WARN, never a
// PASS; a confident disagreement is a FAIL.
func TestCapabilityFindingsCrossCheckTheServedModel(t *testing.T) {
	yes, no, unk := detect.Bool{Value: true, Known: true}, detect.Bool{Value: false, Known: true}, detect.Bool{}
	props := func(v, tl detect.Bool) func() *inference.PropsInfo {
		return func() *inference.PropsInfo { return &inference.PropsInfo{Vision: v, ToolsSupported: tl} }
	}
	unreadable := func() *inference.PropsInfo { return nil }
	token, err := toolsFlagToken("vulkan")
	if err != nil {
		t.Fatal(err)
	}
	toolsUnit := "[Container]\nExec=llama-server -m x " + token + " --port 8080\n"
	for _, tc := range []struct {
		name       string
		vision     bool
		tools      bool
		coding     bool
		props      func() *inference.PropsInfo
		wantVision string // "" means no finding
		wantTools  string
	}{
		{"claims nothing", false, false, false, props(no, no), "", ""},
		{"claims nothing, props unreadable", false, false, false, unreadable, "", ""},
		{"vision agrees", true, false, false, props(yes, no), statusPass, ""},
		{"vision claimed, server says false", true, false, false, props(no, yes), statusFail, ""},
		{"vision claimed, field absent", true, false, false, props(unk, yes), statusWarn, ""},
		{"vision claimed, props unreadable", true, false, false, unreadable, statusWarn, ""},
		{"vision claimed, seam unwired", true, false, false, nil, statusWarn, ""},
		{"tools agree", false, true, false, props(no, yes), "", statusPass},
		{"tools on, template lacks tools", false, true, false, props(yes, no), "", statusFail},
		{"tools on, field absent", false, true, false, props(yes, unk), "", statusWarn},
		{"tools on, props unreadable", false, true, false, unreadable, "", statusWarn},
		{"coding mode counts as tools on", false, false, true, props(yes, no), "", statusFail},
		{"both claimed, both agree", true, true, false, props(yes, yes), statusPass, statusPass},
		{"both claimed, both disagree", true, true, false, props(no, no), statusFail, statusFail},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			d.cfg.Vision = tc.vision
			d.cfg.ToolsMode = tc.tools
			d.cfg.CodingMode = tc.coding
			if tc.tools || tc.coding {
				d.units[inferenceUnitName()] = toolsUnit
			}
			d.Props = tc.props
			r := d.aggregate()
			for id, want := range map[string]string{"CAP-01": tc.wantVision, "CAP-02": tc.wantTools} {
				f, ok := findingByID(r, id)
				if want == "" {
					if ok {
						t.Errorf("%s emitted (%s) for a config that does not claim it", id, f.Status)
					}
					continue
				}
				if !ok {
					t.Fatalf("%s absent", id)
				}
				if f.Status != want {
					t.Errorf("%s = %s %q, want %s", id, f.Status, f.Detail, want)
				}
				if f.Status != statusPass && f.Remediation == "" {
					t.Errorf("%s non-PASS carries no remediation", id)
				}
				if f.Status == statusFail && r.Overall != statusFail {
					t.Errorf("Overall = %q, want FAIL", r.Overall)
				}
			}
		})
	}
}
