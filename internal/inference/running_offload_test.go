package inference

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// running_offload_test.go covers the ALREADY-RUNNING-server offload Verdict
// (D-12/D-13): residency proven by the `load_tensors: Vulkan0 model buffer size
// = N MiB` journald line (WR-05 — NOT /props), corroborated by a point-in-time
// mem_info_gtt_used floor (CR-03 — NOT a fragile before/after delta), with /props
// used only as a config-identity drift overlay. Every signal degrades to a typed
// Unknown → WARN, never a false PASS.

// readFixture is shared with offload_test.go (reads testdata/<rel>).

// weight is a representative ~21.5 GiB model weight (matches the Vulkan0 fixture's
// 21504.49 MiB buffer size) so the floor band lines up with the residency fixture.
const testWeightBytes = 21504 * 1024 * 1024

// TestRunningServerOffloadVerdict drives the journald residency scrape: a Vulkan0
// N>0 line → PASS, a CPU-only journal → FAIL, an empty/absent journal → WARN
// (typed-Unknown, never a false PASS).
func TestRunningServerOffloadVerdict(t *testing.T) {
	vulkanJournal := readFixture(t, "load_tensors_vulkan.txt")
	cpuJournal := readFixture(t, "load_tensors_cpu.txt")

	// A GTT floor that clears (used ≥ weight) so it does not mask the residency
	// signal under test. drmRoot with the gtt_used fixture written below.
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)

	props := &PropsInfo{ModelPath: "/models/qwen3.gguf", NCtx: 131072}

	tests := []struct {
		name    string
		journal string
		props   *PropsInfo
		gtt     detect.Bytes
		want    Status
	}{
		{"vulkan residency → PASS", vulkanJournal, props, gttUsed, StatusPass},
		{"cpu-only journal → FAIL", cpuJournal, props, gttUsed, StatusFail},
		{"empty journal → WARN", "", props, gttUsed, StatusWarn},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := RunningOffloadVerdict(RunningOffloadInput{
				JournalText:  tc.journal,
				Props:        tc.props,
				GTTUsedBytes: tc.gtt,
				WeightBytes:  testWeightBytes,
			})
			if v.Status != tc.want {
				t.Fatalf("status = %s, want %s (detail: %s)", v.Status, tc.want, v.Detail)
			}
		})
	}
}

// TestRunningServerOffloadPropsDrift asserts /props config-identity drift is a WARN
// overlay (not the residency proof): a residency-PASS journal with a /props
// model_path that does NOT match config downgrades to WARN.
func TestRunningServerOffloadPropsDrift(t *testing.T) {
	vulkanJournal := readFixture(t, "load_tensors_vulkan.txt")
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)

	// Residency PASS + GTT PASS, but /props reports a different loaded model than
	// the configured one → config drift → WARN (never a confident PASS).
	v := RunningOffloadVerdict(RunningOffloadInput{
		JournalText:   vulkanJournal,
		Props:         &PropsInfo{ModelPath: "/models/SOMETHING-ELSE.gguf", NCtx: 4096},
		GTTUsedBytes:  gttUsed,
		WeightBytes:   testWeightBytes,
		ConfigModel:   "/models/qwen3.gguf",
		ConfigContext: 131072,
	})
	if v.Status != StatusWarn {
		t.Fatalf("props drift status = %s, want WARN (detail: %s)", v.Status, v.Detail)
	}

	// nil Props (unavailable) is Unknown → it must NOT upgrade a residency-PASS to
	// FAIL; with both residency and GTT PASS the overall verdict stays PASS (props
	// is corroboration, never the proof).
	v2 := RunningOffloadVerdict(RunningOffloadInput{
		JournalText:  vulkanJournal,
		Props:        nil,
		GTTUsedBytes: gttUsed,
		WeightBytes:  testWeightBytes,
	})
	if v2.Status != StatusPass {
		t.Fatalf("nil props status = %s, want PASS (props is corroboration only; detail: %s)", v2.Status, v2.Detail)
	}
}

// TestGTTFloorCorroboration drives the point-in-time GTT floor: used ≥ weight →
// corroborate (PASS), used < weight → FAIL, Unknown used → WARN.
func TestGTTFloorCorroboration(t *testing.T) {
	tests := []struct {
		name string
		used detect.Bytes
		want Status
	}{
		{"used ≥ weight → PASS", detect.KnownBytes(testWeightBytes+1, "test"), StatusPass},
		{"used < weight → FAIL", detect.KnownBytes(testWeightBytes/4, "test"), StatusFail},
		{"unknown used → WARN", detect.UnknownBytes("sysfs unreadable", ""), StatusWarn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			r := gttFloor(tc.used, testWeightBytes)
			if r.Status != tc.want {
				t.Fatalf("gttFloor status = %s, want %s (detail: %s)", r.Status, tc.want, r.Detail)
			}
		})
	}

	// Unknown weight (weightBytes==0) is not a computable floor → WARN, never a
	// fail-open PASS.
	if r := gttFloor(detect.KnownBytes(1<<30, "test"), 0); r.Status != StatusWarn {
		t.Fatalf("zero-weight floor status = %s, want WARN", r.Status)
	}
}
