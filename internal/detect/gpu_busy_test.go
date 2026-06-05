package detect

import (
	"os"
	"path/filepath"
	"testing"
)

// TestGPUBusyPercentFromFixture reads the flat-fixture gpu_busy_percent ("37") via
// the same flat-root fallback the memory readers use, asserting KnownInt(37) (DASH-03
// utilization, typed Int not Bytes).
func TestGPUBusyPercentFromFixture(t *testing.T) {
	got := gpuBusyPercent("testdata")
	if !got.Known {
		t.Fatalf("gpuBusyPercent: Known=false, source=%q", got.Source)
	}
	if got.Value != 37 {
		t.Errorf("gpuBusyPercent: Value=%d, want 37", got.Value)
	}
}

// TestGPUBusyPercentVendorDiscovery asserts discovery globs card*/device + filters on
// vendor 0x1002 (reusing amdSysfsCardDirs) and NEVER hard-codes card0: the busy% file
// lives only under card1 (a non-zero card index) and must still be found.
func TestGPUBusyPercentVendorDiscovery(t *testing.T) {
	root := t.TempDir()
	// card0 is a NON-AMD card (vendor 0x10de) with a misleading busy% — must be skipped.
	card0 := filepath.Join(root, "card0", "device")
	if err := os.MkdirAll(card0, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(card0, "vendor"), []byte("0x10de\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(card0, "gpu_busy_percent"), []byte("99\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	// card1 is the AMD iGPU (vendor 0x1002) with the real busy%.
	card1 := filepath.Join(root, "card1", "device")
	if err := os.MkdirAll(card1, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(card1, "vendor"), []byte("0x1002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(card1, "gpu_busy_percent"), []byte("42\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := gpuBusyPercent(root)
	if !got.Known {
		t.Fatalf("gpuBusyPercent: Known=false, source=%q", got.Source)
	}
	if got.Value != 42 {
		t.Errorf("gpuBusyPercent: Value=%d, want the AMD card's 42 (not card0's 99)", got.Value)
	}
}

// TestGPUBusyPercentUnparseableYieldsUnknown asserts a garbage gpu_busy_percent
// degrades to typed-Unknown — never a fabricated number (D-06 memory-first).
func TestGPUBusyPercentUnparseableYieldsUnknown(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "card1", "device")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "vendor"), []byte("0x1002\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gpu_busy_percent"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	got := gpuBusyPercent(root)
	if got.Known {
		t.Errorf("gpuBusyPercent(garbage): Known=true (%d), want Unknown", got.Value)
	}
	if got.Raw == "" {
		t.Errorf("gpuBusyPercent(garbage): Raw empty, want captured offending output")
	}
}

// TestGPUBusyPercentMissingYieldsUnknown asserts a card dir with NO gpu_busy_percent
// file across all candidates → typed-Unknown "not found" (→ "unavailable", D-06).
func TestGPUBusyPercentMissingYieldsUnknown(t *testing.T) {
	root := t.TempDir() // empty: no card dirs, no flat busy% file
	got := gpuBusyPercent(root)
	if got.Known {
		t.Errorf("gpuBusyPercent(missing): Known=true (%d), want Unknown", got.Value)
	}
}

// TestGPUBusyPercentForTestSeam asserts the test-only injected-root seam mirrors
// GTTUsedBytesForTest so a sibling/dashboard test can read a busy% fixture.
func TestGPUBusyPercentForTestSeam(t *testing.T) {
	got := GPUBusyPercentForTest("testdata")
	if !got.Known || got.Value != 37 {
		t.Errorf("GPUBusyPercentForTest(testdata) = %+v, want KnownInt(37)", got)
	}
}
