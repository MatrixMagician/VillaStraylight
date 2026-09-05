package inference

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// projector_test.go covers the start-time projector scrape and its fold. The
// projector is deliberately NOT part of the two-signal residency proof (ADR-0001):
// a projector on the CPU is slow image encoding, not a CPU fallback of the model,
// so it warns with detail and never fails.

// TestScrapeProjectorLog covers the three outcomes the clip_ctx line can produce.
func TestScrapeProjectorLog(t *testing.T) {
	markers := rocmMarkersForTest(t)

	cases := []struct {
		name       string
		stderr     string
		wantStatus Status
		wantKnown  bool
		wantDetail string
	}{
		{"projector on the device", readFixture(t, "rocm_projector_pass.stderr"), StatusPass, true, "ROCm0"},
		{"projector on the CPU", readFixture(t, "rocm_projector_cpu.stderr"), StatusWarn, true, "image encoding will be slow"},
		{"no clip_ctx line at all", readFixture(t, "rocm_devinfo_pass.stderr"), StatusWarn, false, "absent"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scrapeProjectorLog(tc.stderr, markers)
			if got.Status != tc.wantStatus {
				t.Errorf("Status = %v, want %v (detail %q)", got.Status, tc.wantStatus, got.Detail)
			}
			if got.Signal.Known != tc.wantKnown {
				t.Errorf("Signal.Known = %v, want %v", got.Signal.Known, tc.wantKnown)
			}
			if !strings.Contains(got.Detail, tc.wantDetail) {
				t.Errorf("Detail = %q, want it to mention %q", got.Detail, tc.wantDetail)
			}
		})
	}
}

// TestValidateProjectorDowngradesPass asserts a run that expects a projector and
// cannot prove one is a WARN rather than a clean PASS: the model's residency is
// untouched, but the operator asked for vision and must not be told it is fine.
func TestValidateProjectorDowngradesPass(t *testing.T) {
	fr := &fakeRunner{stderr: readFixture(t, "rocm_devinfo_pass.stderr")}
	in := baseInput(t, fr)
	in.Markers = rocmMarkersForTest(t)
	in.Projector = true

	v := Validate(t.Context(), in)
	if v.Status != StatusWarn {
		t.Fatalf("Status = %v, want WARN (detail %q)", v.Status, v.Detail)
	}
	if !strings.Contains(v.Detail, "projector") {
		t.Errorf("Detail = %q, want the projector finding appended", v.Detail)
	}
}

// TestValidateProjectorOffLeavesVerdictsUnchanged asserts the scrape is inert
// until the config says vision is on: the same run with Projector false must
// produce a byte-identical Verdict, which is what keeps every text-only stack's
// output unchanged by this field existing.
func TestValidateProjectorOffLeavesVerdictsUnchanged(t *testing.T) {
	run := func(projector bool, fixture string) Verdict {
		fr := &fakeRunner{stderr: readFixture(t, fixture)}
		in := baseInput(t, fr)
		in.Markers = rocmMarkersForTest(t)
		in.ReadGTTUsed = gttSeam(detect.KnownBytes(1<<30, "before"), detect.KnownBytes(1<<30+400<<20, "after"))
		in.Projector = projector
		return Validate(t.Context(), in)
	}
	for _, fixture := range []string{"rocm_devinfo_pass.stderr", "rocm_projector_pass.stderr"} {
		off := run(false, fixture)
		if off.Status != StatusPass {
			t.Fatalf("%s with Projector false: Status = %v, want PASS (detail %q)", fixture, off.Status, off.Detail)
		}
		if strings.Contains(off.Detail, "projector") {
			t.Errorf("%s with Projector false: detail mentions the projector: %q", fixture, off.Detail)
		}
	}
	if on := run(true, "rocm_projector_pass.stderr"); on.Status != StatusPass {
		t.Errorf("a proven projector must not downgrade the verdict: %v (%q)", on.Status, on.Detail)
	}
}
