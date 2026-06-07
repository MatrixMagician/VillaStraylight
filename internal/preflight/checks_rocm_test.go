package preflight

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// testPolicy is a synthetic ROCmPolicy mirroring the embedded one, so the table
// tests do not depend on the embedded file's exact contents.
func testPolicy() ROCmPolicy {
	return ROCmPolicy{
		KernelFloor:         "6.18.4",
		KernelTested:        "6.18.9",
		MesaFloor:           "25.0.0",
		FirmwareFloor:       "20260110",
		FirmwareDeny:        []string{"20251125"},
		ImageDeny:           []string{"rocm7-nightlies"},
		RequiredHSAOverride: "11.5.1",
	}
}

// statusByID extracts the Status of a check by id from a result slice.
func statusByID(t *testing.T, rs []CheckResult, id string) Status {
	t.Helper()
	for _, r := range rs {
		if r.ID == id {
			return r.Status
		}
	}
	t.Fatalf("no CheckResult with id %q in %v", id, rs)
	return StatusPass
}

// TestRunROCmGfx covers both branches: a Known non-gfx1151 device FAILs; an unknown
// gfx id WARNs.
func TestRunROCmGfx(t *testing.T) {
	bad := checkROCmGfx(detect.HostProfile{
		ROCmPresent: detect.KnownBool(true, "rocminfo"),
		IGPUGfxID:   detect.KnownStr("gfx1100", "rocminfo"),
	})
	if bad.ID != idROCmGfx || bad.Status != StatusFail {
		t.Errorf("non-gfx1151 → %s/%v, want %s/FAIL", bad.ID, bad.Status, idROCmGfx)
	}

	unknown := checkROCmGfx(detect.HostProfile{
		IGPUGfxID: detect.UnknownStr("rocminfo absent", ""),
	})
	if unknown.Status != StatusWarn {
		t.Errorf("unknown gfx id → %v, want WARN", unknown.Status)
	}

	good := checkROCmGfx(detect.HostProfile{
		IGPUGfxID: detect.KnownStr("gfx1151", "rocminfo"),
	})
	if good.Status != StatusPass {
		t.Errorf("gfx1151 → %v, want PASS", good.Status)
	}
}

// TestRunROCmKernel covers both branches: a Known below-floor kernel FAILs; an
// unknown kernel WARNs. compareVersions is the reused comparator.
func TestRunROCmKernel(t *testing.T) {
	pol := testPolicy()

	bad := checkROCmKernel(detect.HostProfile{
		KernelVersion: detect.KnownStr("6.18.3-300.fc44.x86_64", "osrelease"),
	}, pol)
	if bad.Status != StatusFail {
		t.Errorf("kernel 6.18.3 → %v, want FAIL", bad.Status)
	}

	unknown := checkROCmKernel(detect.HostProfile{
		KernelVersion: detect.UnknownStr("not read", ""),
	}, pol)
	if unknown.Status != StatusWarn {
		t.Errorf("unknown kernel → %v, want WARN", unknown.Status)
	}

	good := checkROCmKernel(detect.HostProfile{
		KernelVersion: detect.KnownStr("6.18.9-300.fc44.x86_64", "osrelease"),
	}, pol)
	if good.Status != StatusPass {
		t.Errorf("kernel 6.18.9 → %v, want PASS", good.Status)
	}
}

// TestRunROCmFirmware covers both branches: a Known denied firmware FAILs; an
// unevaluable (typed-Unknown, the v1.0 off-hardware case) firmware WARNs.
func TestRunROCmFirmware(t *testing.T) {
	pol := testPolicy()

	bad := checkROCmFirmware(detect.KnownStr("20251125", "rpm"), pol)
	if bad.Status != StatusFail {
		t.Errorf("firmware 20251125 → %v, want FAIL", bad.Status)
	}

	unknown := checkROCmFirmware(detect.UnknownStr("not probed", ""), pol)
	if unknown.Status != StatusWarn {
		t.Errorf("unprobed firmware → %v, want WARN", unknown.Status)
	}

	good := checkROCmFirmware(detect.KnownStr("20260110", "rpm"), pol)
	if good.Status != StatusPass {
		t.Errorf("firmware 20260110 → %v, want PASS", good.Status)
	}

	// A firmware date BELOW the floor (20260110) but NOT on the denylist must WARN,
	// not PASS — otherwise the preflight gate clean-PASSes sub-floor firmware while
	// the detect-side readiness gate (>= floor) reports it not-ready (the two
	// surfaces disagree). The denylist stays the only hard FAIL.
	subFloor := checkROCmFirmware(detect.KnownStr("20251201", "rpm"), pol)
	if subFloor.Status != StatusWarn {
		t.Errorf("sub-floor firmware 20251201 (not denied) → %v, want WARN", subFloor.Status)
	}

	// A firmware date one day below the floor is still sub-floor → WARN.
	if r := checkROCmFirmware(detect.KnownStr("20260109", "rpm"), pol); r.Status != StatusWarn {
		t.Errorf("firmware 20260109 (one day below floor) → %v, want WARN", r.Status)
	}

	// A non-date firmware string must not be coerced into a false sub-floor WARN:
	// isFirmwareDate rejects it, so it falls through to PASS (not denied, not
	// comparable). Biased against over-blocking a genuinely-working host.
	if r := checkROCmFirmware(detect.KnownStr("not-a-date", "rpm"), pol); r.Status != StatusPass {
		t.Errorf("unparseable firmware string → %v, want PASS (no false sub-floor WARN)", r.Status)
	}
}

// TestRunROCmHSA covers both branches: a Known-absent (unset) override FAILs; an
// unevaluable override WARNs.
func TestRunROCmHSA(t *testing.T) {
	pol := testPolicy()

	bad := checkROCmHSA(detect.KnownStr("", "env"), pol)
	if bad.Status != StatusFail {
		t.Errorf("HSA override unset → %v, want FAIL", bad.Status)
	}

	wrong := checkROCmHSA(detect.KnownStr("11.0.0", "env"), pol)
	if wrong.Status != StatusFail {
		t.Errorf("HSA override wrong value → %v, want FAIL", wrong.Status)
	}

	unknown := checkROCmHSA(detect.UnknownStr("not probed", ""), pol)
	if unknown.Status != StatusWarn {
		t.Errorf("unknown HSA override → %v, want WARN", unknown.Status)
	}

	good := checkROCmHSA(detect.KnownStr("11.5.1", "env"), pol)
	if good.Status != StatusPass {
		t.Errorf("HSA override 11.5.1 → %v, want PASS", good.Status)
	}
}

// TestRunROCmImage covers both branches: a denied image request FAILs; no request
// (the standalone / off-hardware case) WARNs; the in-tree rocm-7.2.4 passes.
func TestRunROCmImage(t *testing.T) {
	pol := testPolicy()

	bad := checkROCmImage("docker.io/kyuz0/amd-strix-halo-toolboxes:rocm7-nightlies", pol)
	if bad.Status != StatusFail {
		t.Errorf("rocm7-nightlies image → %v, want FAIL", bad.Status)
	}

	none := checkROCmImage("", pol)
	if none.Status != StatusWarn {
		t.Errorf("no image requested → %v, want WARN", none.Status)
	}

	good := checkROCmImage("docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-7.2.4", pol)
	if good.Status != StatusPass {
		t.Errorf("rocm-7.2.4 image → %v, want PASS", good.Status)
	}
}

// TestRunROCmOffHardwareNoFalseFail is the bias-not-to-over-block proof (T-07-02):
// every signal Unknown (a bare off-hardware HostProfile, no image requested) yields
// ZERO StatusFail. Every ROCm check must be WARN or PASS.
func TestRunROCmOffHardwareNoFalseFail(t *testing.T) {
	// A bare profile: nothing detected. This is the off-hardware shape.
	results := RunROCmWithPolicy(detect.HostProfile{}, testPolicy(), "")

	for _, r := range results {
		if r.Status == StatusFail {
			t.Errorf("off-hardware: check %s is StatusFail, want WARN/PASS (over-block guard)", r.ID)
		}
		if !strings.HasPrefix(r.ID, "ROCM-PRE-") {
			t.Errorf("check id %q is not in the ROCM-PRE-* namespace", r.ID)
		}
	}
	if len(results) != 5 {
		t.Errorf("RunROCmWithPolicy returned %d checks, want 5", len(results))
	}
}

// TestRunROCmKnownBadProfileFails asserts the full RunROCmWithPolicy pipeline FAILs
// the signals it can confidently evaluate as known-bad from the profile (gfx +
// kernel), and that a denied image request through the pipeline FAILs too.
func TestRunROCmKnownBadProfileFails(t *testing.T) {
	p := detect.HostProfile{
		ROCmPresent:   detect.KnownBool(true, "rocminfo"),
		IGPUGfxID:     detect.KnownStr("gfx1100", "rocminfo"),
		KernelVersion: detect.KnownStr("6.18.3", "osrelease"),
	}
	results := RunROCmWithPolicy(p, testPolicy(), "repo:rocm7-nightlies")

	if statusByID(t, results, idROCmGfx) != StatusFail {
		t.Error("known non-gfx1151 must FAIL gfx check")
	}
	if statusByID(t, results, idROCmKernel) != StatusFail {
		t.Error("below-floor kernel must FAIL kernel check")
	}
	if statusByID(t, results, idROCmImage) != StatusFail {
		t.Error("rocm7-nightlies request must FAIL image check")
	}
}

// TestRunROCmUsesEmbeddedPolicy asserts the exported RunROCm entrypoint loads the
// real embedded policy and returns the ROCM-PRE-* checks (no panic on the embed).
func TestRunROCmUsesEmbeddedPolicy(t *testing.T) {
	results := RunROCm(detect.HostProfile{})
	if len(results) != 5 {
		t.Fatalf("RunROCm returned %d checks, want 5", len(results))
	}
	for _, r := range results {
		if r.Status == StatusFail {
			t.Errorf("off-hardware RunROCm: %s is StatusFail, want WARN/PASS", r.ID)
		}
	}
}

// TestRunROCmForImageEvaluatesDigest asserts RunROCmForImage threads the resolved
// target image into the policy gate so checkROCmImage EVALUATES the actual digest
// against imageDeny (SC#2 / Pitfall 3), rather than the empty-image WARN bypass
// RunROCm uses on the host-prep path. A pinned 6.4.4 digest is NOT denied → PASS.
func TestRunROCmForImageEvaluatesDigest(t *testing.T) {
	const img644 = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm-6.4.4@sha256:c81f30a7fd2641e3ea6ac4c45323ba239dca906ed79cc0dfe5b885f9f150ec62"
	results := RunROCmForImage(detect.HostProfile{}, img644)
	if len(results) != 5 {
		t.Fatalf("RunROCmForImage returned %d checks, want 5", len(results))
	}
	// The image check must PASS (digest evaluated, not WARN "no image requested").
	if got := statusByID(t, results, idROCmImage); got != StatusPass {
		t.Errorf("rocm-6.4.4 image check → %v, want PASS (digest evaluated, not denied)", got)
	}
	// RunROCmForImage must match RunROCmWithPolicy with the same image (it is a
	// thin wrapper over the embedded policy).
	want := RunROCmWithPolicy(detect.HostProfile{}, loadROCmPolicy(), img644)
	if statusByID(t, want, idROCmImage) != statusByID(t, results, idROCmImage) {
		t.Error("RunROCmForImage image-check status must match RunROCmWithPolicy(embedded, image)")
	}
}

// TestRunROCmForImageDeniesNightly asserts the deny-list still bites through the
// new wrapper: a rocm7-nightlies image FAILs the image check (refuse-with-remediation).
func TestRunROCmForImageDeniesNightly(t *testing.T) {
	const nightly = "docker.io/kyuz0/amd-strix-halo-toolboxes:rocm7-nightlies@sha256:deadbeef"
	results := RunROCmForImage(detect.HostProfile{}, nightly)
	if got := statusByID(t, results, idROCmImage); got != StatusFail {
		t.Errorf("rocm7-nightlies image check → %v, want FAIL (denied)", got)
	}
}
