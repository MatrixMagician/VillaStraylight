package inference

import (
	"os"
	"path/filepath"
	"strings"
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
				Markers:      VulkanBackend().ResidencyProof(),
				// GPUBusyPercent left UNSET (zero value = typed-Unknown) so the busy
				// fold is SKIPPED and these Vulkan verdicts stay byte-identical.
			})
			if v.Status != tc.want {
				t.Fatalf("status = %s, want %s (detail: %s)", v.Status, tc.want, v.Detail)
			}
		})
	}
}

// TestRunningServerBusySignalFold asserts the D-06 gpu_busy_percent fold: a Known
// non-zero busy reading CORROBORATES a residency PASS (stays PASS), a Known-ZERO
// busy reading on a claimed-healthy decode FAILs (silent CPU fallback), and an
// absent/Unknown busy reading is combine-neutral so a residency-proven Vulkan PASS
// stays PASS (the regression guard — Vulkan supplies no busy signal, D-07/Q2).
func TestRunningServerBusySignalFold(t *testing.T) {
	vulkanJournal := readFixture(t, "load_tensors_vulkan.txt")
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)
	markers := VulkanBackend().ResidencyProof()

	base := func(busy detect.Int) RunningOffloadInput {
		return RunningOffloadInput{
			JournalText:    vulkanJournal,
			GTTUsedBytes:   gttUsed,
			WeightBytes:    testWeightBytes,
			Markers:        markers,
			GPUBusyPercent: busy,
		}
	}

	tests := []struct {
		name string
		busy detect.Int
		want Status
	}{
		{"Known non-zero busy corroborates residency PASS", detect.KnownInt(42, "test"), StatusPass},
		{"Known-zero busy on claimed-healthy decode → FAIL", detect.KnownInt(0, "test"), StatusFail},
		{"Unknown busy is neutral — residency PASS stays PASS", detect.UnknownInt("unavailable", ""), StatusPass},
		{"absent (zero-value) busy is neutral — PASS stays PASS", detect.Int{}, StatusPass},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			v := RunningOffloadVerdict(base(tc.busy))
			if v.Status != tc.want {
				t.Fatalf("busy fold status = %s, want %s (detail: %s)", v.Status, tc.want, v.Detail)
			}
		})
	}
}

// TestRunningServerBusyFoldPreservesContract is the CR-01 regression guard: when a Known
// gpu_busy_percent reading is folded in, the busy signal is a STATUS corroborator only — it
// MUST NOT overwrite the --json contract's SysfsOffload (the real GTT-floor signal), zero the
// GTTDeltaBytes calibration record, or nest the Detail string. (Before the fix the re-fold
// routed busy through combineOffload's sysfs slot, corrupting all three.)
func TestRunningServerBusyFoldPreservesContract(t *testing.T) {
	vulkanJournal := readFixture(t, "load_tensors_vulkan.txt")
	drm := t.TempDir()
	const gttUsedValue = uint64(23068672000)
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)
	markers := VulkanBackend().ResidencyProof()

	in := RunningOffloadInput{
		JournalText:    vulkanJournal,
		GTTUsedBytes:   gttUsed,
		WeightBytes:    testWeightBytes,
		Markers:        markers,
		GPUBusyPercent: detect.KnownInt(42, "test"),
	}
	v := RunningOffloadVerdict(in)

	if v.Status != StatusPass {
		t.Fatalf("status = %s, want PASS (detail: %s)", v.Status, v.Detail)
	}
	// SysfsOffload must remain the GTT-floor signal, NOT the busy signal.
	if strings.Contains(v.SysfsOffload.Source, "gpu_busy_percent") {
		t.Errorf("SysfsOffload.Source = %q — busy signal leaked into the sysfs contract slot (CR-01)", v.SysfsOffload.Source)
	}
	// GTTDeltaBytes must keep the floor value, not be zeroed by the busy re-fold.
	if v.GTTDeltaBytes != gttUsedValue {
		t.Errorf("GTTDeltaBytes = %d, want %d — busy re-fold zeroed the GTT calibration record (CR-01)", v.GTTDeltaBytes, gttUsedValue)
	}
	// Detail must carry the busy corroboration without nesting the already-joined string.
	if strings.Count(v.Detail, "offload proven (log + sysfs)") != 1 {
		t.Errorf("Detail nests the combined headline (CR-01): %q", v.Detail)
	}
	if !strings.Contains(v.Detail, "busy:") {
		t.Errorf("Detail missing busy corroboration clause: %q", v.Detail)
	}
}

// rocmMarkersForTest resolves the ROCm residency descriptor through BackendFor so the
// running-path ROCm cases key on the real backend-owned markers (ROCm0 / fault string).
func rocmMarkersForTest(t *testing.T) ResidencyMarkers {
	t.Helper()
	b, err := BackendFor("rocm")
	if err != nil {
		t.Fatalf("BackendFor(rocm): %v", err)
	}
	return b.ResidencyProof()
}

// busyFromTempDRM writes a gpu_busy_percent fixture under a temp drmRoot and reads it
// back through the REAL detect seam (detect.GPUBusyPercentForTest) — no new detect
// code, mirroring the mem_info_gtt_used fixture pattern. value=="" writes NO file so
// the read degrades to a typed-Unknown (absent busy).
func busyFromTempDRM(t *testing.T, value string) detect.Int {
	t.Helper()
	dir := t.TempDir()
	if value != "" {
		if err := os.WriteFile(filepath.Join(dir, "gpu_busy_percent"), []byte(value+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return detect.GPUBusyPercentForTest(dir)
}

// TestRunningServerROCmResidency drives the RUNNING-path verdict with the ROCm
// descriptor: a ROCm0 N/N journal → PASS, a CPU-only journal → FAIL, a GPU-fault
// journal → FAIL (the fault string voids residency before any buffer-line PASS,
// Pitfall 4), and an empty journal → WARN (typed-Unknown). Every existing Vulkan case
// stays untouched.
func TestRunningServerROCmResidency(t *testing.T) {
	markers := rocmMarkersForTest(t)
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)

	tests := []struct {
		name    string
		fixture string // "" → empty journal
		want    Status
	}{
		{"rocm0 N/N residency → PASS", "load_tensors_rocm.txt", StatusPass},
		{"rocm cpu-only journal → FAIL", "load_tensors_rocm_cpu.txt", StatusFail},
		{"rocm gpu-fault journal → FAIL", "load_tensors_rocm_fault.txt", StatusFail},
		{"empty journal → WARN", "", StatusWarn},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			journal := ""
			if tc.fixture != "" {
				journal = readFixture(t, tc.fixture)
			}
			v := RunningOffloadVerdict(RunningOffloadInput{
				JournalText:  journal,
				GTTUsedBytes: gttUsed,
				WeightBytes:  testWeightBytes,
				Markers:      markers,
				// GPUBusyPercent left UNSET (typed-Unknown) → busy fold skipped here.
			})
			if v.Status != tc.want {
				t.Fatalf("rocm residency status = %s, want %s (detail: %s)", v.Status, tc.want, v.Detail)
			}
		})
	}
}

// TestRunningServerROCmBusySignal exercises the D-06 gpu_busy_percent residency signal
// on the ROCm path through the REAL detect.GPUBusyPercentForTest reader (a temp drmRoot
// gpu_busy_percent fixture): a Known non-zero busy reading CORROBORATES the ROCm0 N/N
// PASS (stays PASS), and an Unknown/absent busy reading is NEUTRAL-for-PASS — the
// residency-proven PASS stays PASS, never a false-FAIL (D-07/Q2). (The Known-zero→FAIL
// rule is unit-proven in Plan 01 Task 2 — TestRunningServerBusySignalFold; the live
// decode-time FAIL is the Phase-8 follow-on, so no live-decode fixture here.)
func TestRunningServerROCmBusySignal(t *testing.T) {
	markers := rocmMarkersForTest(t)
	rocmJournal := readFixture(t, "load_tensors_rocm.txt")
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gttUsed := detect.GTTUsedBytesForTest(drm)

	base := func(busy detect.Int) RunningOffloadInput {
		return RunningOffloadInput{
			JournalText:    rocmJournal,
			GTTUsedBytes:   gttUsed,
			WeightBytes:    testWeightBytes,
			Markers:        markers,
			GPUBusyPercent: busy,
		}
	}

	// Known non-zero busy (37%) read through the real seam → corroborates PASS.
	knownBusy := busyFromTempDRM(t, "37")
	if !knownBusy.Known || knownBusy.Value != 37 {
		t.Fatalf("GPUBusyPercentForTest(37) = %+v, want Known 37", knownBusy)
	}
	if v := RunningOffloadVerdict(base(knownBusy)); v.Status != StatusPass {
		t.Fatalf("known non-zero busy: status = %s, want PASS (busy must corroborate; detail: %s)", v.Status, v.Detail)
	}

	// Unknown/absent busy (no gpu_busy_percent file) read through the real seam →
	// neutral-for-PASS: the ROCm0 N/N residency PASS stays PASS, never a false-FAIL.
	absentBusy := busyFromTempDRM(t, "")
	if absentBusy.Known {
		t.Fatalf("GPUBusyPercentForTest(absent) = %+v, want Unknown", absentBusy)
	}
	if v := RunningOffloadVerdict(base(absentBusy)); v.Status != StatusPass {
		t.Fatalf("absent busy: status = %s, want PASS (Unknown busy is neutral, never a false-FAIL; detail: %s)", v.Status, v.Detail)
	}
}

// TestScrapeLoadTensorsResidencyFault asserts a non-empty FaultString found in the
// journal VOIDS residency (FAIL) before any buffer-line PASS, and that the empty
// Vulkan FaultString makes the scan a no-op (the Vulkan residency journal still PASSes).
func TestScrapeLoadTensorsResidencyFault(t *testing.T) {
	vulkanJournal := readFixture(t, "load_tensors_vulkan.txt")

	// Vulkan markers (empty FaultString) → fault scan is a no-op → PASS.
	if r := scrapeLoadTensorsResidency(vulkanJournal, VulkanBackend().ResidencyProof()); r.Status != StatusPass {
		t.Fatalf("vulkan residency status = %s, want PASS (fault scan must be a no-op)", r.Status)
	}

	// A backend with a fault marker present in the journal → FAIL before the
	// buffer-line PASS.
	faultMarkers := ResidencyMarkers{DeviceToken: "Vulkan0", FaultString: "Memory access fault by GPU node"}
	faulted := vulkanJournal + "\nMemory access fault by GPU node-1 (Agent handle: 0x...) on address 0x...\n"
	if r := scrapeLoadTensorsResidency(faulted, faultMarkers); r.Status != StatusFail {
		t.Fatalf("faulted journal status = %s, want FAIL (fault voids residency)", r.Status)
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
		Markers:       VulkanBackend().ResidencyProof(),
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
		Markers:      VulkanBackend().ResidencyProof(),
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
