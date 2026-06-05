package inference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// readFixture reads a testdata file or fails the test.
func readFixture(t *testing.T, rel string) string {
	t.Helper()
	b, err := os.ReadFile(filepath.Join("testdata", rel))
	if err != nil {
		t.Fatalf("read fixture %s: %v", rel, err)
	}
	return string(b)
}

// readSysfsBytes reads a sysfs delta fixture into a Known detect.Bytes.
func readSysfsBytes(t *testing.T, rel string) detect.Bytes {
	t.Helper()
	v := gttUsedFromFile(t, filepath.Join("testdata", "sysfs", rel))
	return v
}

// gttUsedFromFile reads a single numeric sysfs fixture file into a detect.Bytes
// using the detect reader through a temp dir (mirrors how the live path reads a
// card dir). It is a test helper, not production code.
func gttUsedFromFile(t *testing.T, path string) detect.Bytes {
	t.Helper()
	dir := t.TempDir()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mem_info_gtt_used"), b, 0o644); err != nil {
		t.Fatal(err)
	}
	return detect.GTTUsedBytesForTest(dir)
}

const fixtureWeightBytes = 491400032 // 0.5B model on-disk weight (RESEARCH seed)

// TestOffloadLogScrape: RADV+offloaded-N/N → PASS; llvmpipe → FAIL; offloaded-0 →
// FAIL; empty/truncated stderr → Unknown (WARN, not FAIL). The renderer denylist is
// reused from internal/detect (isSoftwareRendererName).
func TestOffloadLogScrape(t *testing.T) {
	cases := []struct {
		name    string
		fixture string
		want    Status
	}{
		{"radv pass (old fmt)", "radv_pass.stderr", StatusPass},
		{"radv pass (new device_info fmt)", "radv_devinfo_pass.stderr", StatusPass},
		{"llvmpipe fail (old fmt)", "llvmpipe_fail.stderr", StatusFail},
		{"llvmpipe fail (new device_info fmt)", "llvmpipe_devinfo_fail.stderr", StatusFail},
		{"offloaded zero fail", "offloaded_zero.stderr", StatusFail},
		{"loading/empty unknown", "loading_503.stderr", StatusWarn},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := scrapeOffloadLog(readFixture(t, tc.fixture))
			if got.Status != tc.want {
				t.Errorf("scrapeOffloadLog(%s): Status=%v, want %v (detail=%q)", tc.fixture, got.Status, tc.want, got.Detail)
			}
		})
	}
}

// TestOffloadSysfsDelta: delta ≥ 0.5×weight → PASS; delta < 0.1×weight → FAIL;
// in-between → WARN; an unreadable read → Unknown (WARN). Uses the detect sysfs
// reader (amdSysfsCardDirs seam) — never hard-codes card0.
func TestOffloadSysfsDelta(t *testing.T) {
	before := readSysfsBytes(t, "gtt_before")

	pass := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_pass"), fixtureWeightBytes)
	if pass.Status != StatusPass {
		t.Errorf("sysfs delta (pass band): Status=%v, want PASS (delta=%d)", pass.Status, pass.DeltaBytes)
	}
	fail := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_fail"), fixtureWeightBytes)
	if fail.Status != StatusFail {
		t.Errorf("sysfs delta (fail band): Status=%v, want FAIL (delta=%d)", fail.Status, fail.DeltaBytes)
	}
	warn := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_warn"), fixtureWeightBytes)
	if warn.Status != StatusWarn {
		t.Errorf("sysfs delta (warn band): Status=%v, want WARN (delta=%d)", warn.Status, warn.DeltaBytes)
	}
	// Unknown read → WARN, never FAIL.
	unknown := offloadSysfsDelta(detect.UnknownBytes("gtt unreadable", ""), readSysfsBytes(t, "gtt_after_pass"), fixtureWeightBytes)
	if unknown.Status != StatusWarn {
		t.Errorf("sysfs delta (unknown read): Status=%v, want WARN", unknown.Status)
	}
	// weightBytes==0 must NOT fail-open to PASS: the band collapses to 0, so a
	// large delta would otherwise be reported as proven offload off an unknown
	// weight. Degrade to typed-Unknown WARN instead (D-09.2 contract).
	zeroWeight := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_pass"), 0)
	if zeroWeight.Status != StatusWarn {
		t.Errorf("sysfs delta (weightBytes==0): Status=%v, want WARN (must not fail-open to PASS)", zeroWeight.Status)
	}
	if zeroWeight.Signal.Known {
		t.Errorf("sysfs delta (weightBytes==0): Signal.Known=true, want Unknown (uncertainty must not read as success)")
	}
}

// TestOffloadVerdict: combined dual-assert is PASS only when BOTH log-scrape AND
// sysfs delta pass; if either is FAIL → FAIL; if either is Unknown (and neither
// FAIL) → WARN (D-09).
func TestOffloadVerdict(t *testing.T) {
	before := readSysfsBytes(t, "gtt_before")
	logPass := scrapeOffloadLog(readFixture(t, "radv_pass.stderr"))
	logFail := scrapeOffloadLog(readFixture(t, "llvmpipe_fail.stderr"))
	logUnknown := scrapeOffloadLog(readFixture(t, "loading_503.stderr"))
	sysPass := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_pass"), fixtureWeightBytes)
	sysFail := offloadSysfsDelta(before, readSysfsBytes(t, "gtt_after_fail"), fixtureWeightBytes)
	sysUnknown := offloadSysfsDelta(detect.UnknownBytes("x", ""), readSysfsBytes(t, "gtt_after_pass"), fixtureWeightBytes)

	if v := combineOffload(logPass, sysPass); v.Status != StatusPass {
		t.Errorf("both pass: Status=%v, want PASS", v.Status)
	}
	if v := combineOffload(logFail, sysPass); v.Status != StatusFail {
		t.Errorf("log fail: Status=%v, want FAIL", v.Status)
	}
	if v := combineOffload(logPass, sysFail); v.Status != StatusFail {
		t.Errorf("sysfs fail: Status=%v, want FAIL", v.Status)
	}
	if v := combineOffload(logUnknown, sysPass); v.Status != StatusWarn {
		t.Errorf("log unknown: Status=%v, want WARN", v.Status)
	}
	if v := combineOffload(logPass, sysUnknown); v.Status != StatusWarn {
		t.Errorf("sysfs unknown: Status=%v, want WARN", v.Status)
	}
	// FAIL dominates Unknown.
	if v := combineOffload(logUnknown, sysFail); v.Status != StatusFail {
		t.Errorf("unknown+fail: Status=%v, want FAIL", v.Status)
	}
}
