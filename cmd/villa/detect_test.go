package main

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

var update = flag.Bool("update", false, "regenerate golden files")

// fixtureProfile is a deterministic HostProfile (NOT live hardware) so the
// golden JSON is stable in CI. It mixes Known and Unknown fields to lock the
// full --json contract shape (D-05 dashboard contract).
func fixtureProfile() detect.HostProfile {
	return detect.HostProfile{
		CPUModel:            detect.KnownStr("AMD RYZEN AI MAX+ 395 w/ Radeon 8060S", "ghw.CPU"),
		Arch:                detect.KnownStr("amd64", "runtime.GOARCH"),
		TotalRAMBytes:       detect.KnownBytes(137438953472, "ghw.Memory"),
		MemAvailableBytes:   detect.KnownBytes(122191437824, "/proc/meminfo:MemAvailable"),
		IGPUName:            detect.KnownStr("AMD Radeon 8060S Graphics (RADV STRIX_HALO)", "vulkaninfo --summary:deviceName"),
		IGPUGfxID:           detect.UnknownStr("rocminfo unavailable (gfx id not enumerated)", ""),
		VulkanICDPath:       detect.KnownStr("/usr/share/vulkan/icd.d/radeon_icd.x86_64.json", "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json"),
		VulkanDevice:        detect.KnownStr("AMD Radeon 8060S Graphics (RADV STRIX_HALO)", "vulkaninfo --summary:deviceName"),
		DRINodes:            []string{"renderD128", "card1"},
		DRINodeCount:        detect.KnownInt(2, "/dev/dri"),
		ROCmPresent:         detect.KnownBool(false, "rocminfo not on PATH"),
		UsableEnvelopeBytes: detect.KnownBytes(67149381632, "mem_info_gtt_total (/sys/class/drm/card1/device/mem_info_gtt_total)"),
		GTTTotalBytes:       detect.KnownBytes(67149381632, "/sys/class/drm/card1/device/mem_info_gtt_total"),
		TTMLimitBytes:       detect.KnownBytes(67149381632, "/sys/module/ttm/parameters/pages_limit"),
		BIOSVRAMBytes:       detect.KnownBytes(536870912, "/sys/class/drm/card1/device/mem_info_vram_total"),
		KernelVersion:       detect.KnownStr("7.0.10-201.fc44.x86_64", "/proc/sys/kernel/osrelease"),
		MesaVersion:         detect.KnownStr("26.0.8", "vulkaninfo --summary:driverVersion"),
		SchemaVersion:       1,
	}
}

// TestJSONGolden asserts `villa detect --json` over the injected fixture profile
// matches cmd/villa/testdata/detect.golden.json byte-for-byte (D-05 contract
// stability). Run with -update to regenerate.
func TestJSONGolden(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDetect(&buf, fixtureProfile(), true /*json*/, false); err != nil {
		t.Fatalf("renderDetect: %v", err)
	}

	golden := filepath.Join("testdata", "detect.golden.json")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, buf.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}

	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(buf.Bytes(), want) {
		t.Errorf("JSON output does not match golden.\n--- got ---\n%s\n--- want ---\n%s", buf.String(), want)
	}
}

// TestDetectTableRendersUnknown asserts the default table surfaces a one-line
// "unknown (reason)" for an Unknown field rather than a bare blank or zero.
func TestDetectTableRendersUnknown(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDetect(&buf, fixtureProfile(), false /*table*/, false); err != nil {
		t.Fatalf("renderDetect: %v", err)
	}
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("unknown (rocminfo unavailable")) {
		t.Errorf("table did not surface Unknown gfx id reason:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("62.538 GiB")) {
		t.Errorf("table did not render envelope in GiB:\n%s", out)
	}
}

// TestDetectVerboseAddsProvenance asserts -v appends the source column.
func TestDetectVerboseAddsProvenance(t *testing.T) {
	var buf bytes.Buffer
	if err := renderDetect(&buf, fixtureProfile(), false, true /*verbose*/); err != nil {
		t.Fatalf("renderDetect: %v", err)
	}
	if !bytes.Contains(buf.Bytes(), []byte("(ghw.CPU)")) {
		t.Errorf("verbose table missing provenance source column:\n%s", buf.String())
	}
}
