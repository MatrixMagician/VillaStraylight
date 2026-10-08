package inference

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// The fixture lines are the gfx1151 sd-server journal of 2026-10-08 (the pinned
// master-vulkan digest, --backend Vulkan0 --params-backend Vulkan0 --eager-load):
// the device enumeration, the placement line printed before `listening on`, and
// the listening line. The RAM-placed, all-RAM, software-renderer and spaced-unit
// variants are constructed from the same grammar.
const (
	sdDeviceLine    = "ggml_vulkan: 0 = Radeon 8060S Graphics (RADV GFX1151) (radv) | uma: 1 | fp16: 1 | bf16: 0 | fp4: 0 | warp size: 64 | shared memory: 65536 | int dot: 0 | matrix cores: KHR_coopmat"
	sdPlacementQ8   = "[I] total params memory size = 8808.62MB (VRAM 8808.62MB, RAM 0.00MB): text_encoders 2375.91MB(VRAM), diffusion_model 6272.71MB(VRAM), vae 160.00MB(VRAM), controlnet 0.00MB(N/A), extensions 0.00MB(N/A) --- diffusion_engine.cpp:1328"
	sdListeningLine = "[I] listening on: http://0.0.0.0:1234 --- main.cpp:151"

	sdPlacementPartialRAM = "[I] total params memory size = 8808.62MB (VRAM 6432.71MB, RAM 2375.91MB): text_encoders 2375.91MB(RAM), diffusion_model 6272.71MB(VRAM), vae 160.00MB(VRAM), controlnet 0.00MB(N/A), extensions 0.00MB(N/A) --- diffusion_engine.cpp:1328"
	sdPlacementAllRAM     = "[I] total params memory size = 8808.62MB (VRAM 0.00MB, RAM 8808.62MB): text_encoders 2375.91MB(RAM), diffusion_model 6272.71MB(RAM), vae 160.00MB(RAM), controlnet 0.00MB(N/A), extensions 0.00MB(N/A) --- diffusion_engine.cpp:1328"
	sdPlacementSpaced     = "[I] total params memory size = 8.6 GiB (VRAM 8808.62 MiB, RAM 0 MB): text_encoders 2375.91MB(VRAM) --- diffusion_engine.cpp:1328"
	sdSoftwareDeviceLine  = "ggml_vulkan: 0 = llvmpipe (LLVM 17.0.6, 256 bits) (llvmpipe) | uma: 0 | fp16: 1 | bf16: 0 | fp4: 0 | warp size: 8 | shared memory: 32768 | int dot: 0 | matrix cores: none"

	// q8WeightBytes is the Q8_0 row's weight_bytes, the witness reference; the
	// placement line's 8808.62MB is within 1% of it.
	q8WeightBytes = 9236591616
	// q4WeightBytes is the Q4_K row's weight_bytes, 30% below the Q8_0 line.
	q4WeightBytes = 6523315200
)

func journal(lines ...string) string { return strings.Join(lines, "\n") + "\n" }

// imageInput is the fold input a proof hands over: a clearing GTT floor, a busy
// reading sampled during the drive, and the Vulkan markers villa-image always
// runs under.
func imageInput(journalText string, weight uint64) RunningOffloadInput {
	return RunningOffloadInput{
		JournalText:    journalText,
		GTTUsedBytes:   detect.KnownBytes(42<<30, "mem_info_gtt_used"),
		WeightBytes:    weight,
		Markers:        VulkanBackend().ResidencyProof(),
		GPUBusyPercent: detect.KnownInt(100, "gpu_busy_percent"),
	}
}

// TestImageOffloadVerdictPlacement is the placement rule: every param byte on the
// device is a PASS; any byte in system RAM is a FAIL, because a partial CPU fallback
// is a FAIL in this repo; no params on the device is a FAIL; a journal with no
// placement line, or no journal at all, is a WARN that never reads as either.
func TestImageOffloadVerdictPlacement(t *testing.T) {
	cases := []struct {
		name    string
		journal string
		want    Status
		detail  string
	}{
		{"eager load, all on Vulkan0", journal(sdDeviceLine, sdPlacementQ8, sdListeningLine), StatusPass, "8808.62 MiB on VRAM, 0 B in RAM"},
		{"text encoder placed in RAM", journal(sdDeviceLine, sdPlacementPartialRAM, sdListeningLine), StatusFail, "2375.91 MiB of params in system RAM"},
		{"everything in RAM", journal(sdDeviceLine, sdPlacementAllRAM, sdListeningLine), StatusFail, "8808.62 MiB of params in system RAM"},
		{"lazy load: ready with no placement line", journal(sdDeviceLine, sdListeningLine), StatusWarn, "no params placement line"},
		{"empty journal", "", StatusWarn, "journal empty"},
		{"software renderer enumerated", journal(sdSoftwareDeviceLine, sdPlacementQ8, sdListeningLine), StatusFail, "software renderer"},
		{"spaced units and GiB total", journal(sdDeviceLine, sdPlacementSpaced, sdListeningLine), StatusPass, "8808.62 MiB on VRAM"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := ImageOffloadVerdict(imageInput(tc.journal, q8WeightBytes))
			if v.Status != tc.want {
				t.Fatalf("status = %s, want %s (detail: %s)", v.Status, tc.want, v.Detail)
			}
			if !strings.Contains(v.Detail, tc.detail) {
				t.Errorf("detail %q does not name %q", v.Detail, tc.detail)
			}
		})
	}
}

// TestImageOffloadVerdictFloorsAreShared: the GTT floor and the busy fold are the
// chat proof's own, so a floor below the weight or a 0% busy reading during the
// drive FAILs a placement PASS, and an unreadable floor degrades it to WARN.
func TestImageOffloadVerdictFloorsAreShared(t *testing.T) {
	good := journal(sdDeviceLine, sdPlacementQ8, sdListeningLine)

	low := imageInput(good, q8WeightBytes)
	low.GTTUsedBytes = detect.KnownBytes(1<<30, "mem_info_gtt_used")
	if v := ImageOffloadVerdict(low); v.Status != StatusFail {
		t.Errorf("GTT-used below the weight = %s, want FAIL (%s)", v.Status, v.Detail)
	}

	idle := imageInput(good, q8WeightBytes)
	idle.GPUBusyPercent = detect.KnownInt(0, "gpu_busy_percent")
	if v := ImageOffloadVerdict(idle); v.Status != StatusFail {
		t.Errorf("0%% busy during the drive = %s, want FAIL (%s)", v.Status, v.Detail)
	}

	unread := imageInput(good, q8WeightBytes)
	unread.GTTUsedBytes = detect.UnknownBytes("sysfs unreadable", "")
	if v := ImageOffloadVerdict(unread); v.Status != StatusWarn {
		t.Errorf("unreadable GTT = %s, want WARN (%s)", v.Status, v.Detail)
	}
	if v := ImageOffloadVerdict(imageInput(good, q8WeightBytes)); !strings.Contains(v.Provenance, "gpu_busy_percent") || !strings.Contains(v.Provenance, "placement") {
		t.Errorf("provenance %q does not name the placement line and the busy corroboration", v.Provenance)
	}
}

// TestImageOffloadVerdictWitnessesTheCatalog is the ADR-0007 overlay: a running
// server whose params figure disagrees with the image table's weight_bytes by more
// than 5% turns a PASS into a WARN that names both numbers. It never rescues a FAIL
// and never fires on a figure within tolerance.
func TestImageOffloadVerdictWitnessesTheCatalog(t *testing.T) {
	good := journal(sdDeviceLine, sdPlacementQ8, sdListeningLine)

	v := ImageOffloadVerdict(imageInput(good, q4WeightBytes))
	if v.Status != StatusWarn {
		t.Fatalf("Q8 placement against the Q4 row = %s, want WARN (%s)", v.Status, v.Detail)
	}
	for _, want := range []string{"8808.62 MiB", "6221.12 MiB", "images.json"} {
		if !strings.Contains(v.Detail, want) {
			t.Errorf("witness detail %q does not carry %q", v.Detail, want)
		}
	}
	if v := ImageOffloadVerdict(imageInput(good, q8WeightBytes)); v.Status != StatusPass {
		t.Errorf("Q8 placement against the Q8 row = %s, want PASS (%s)", v.Status, v.Detail)
	}
	ram := ImageOffloadVerdict(imageInput(journal(sdDeviceLine, sdPlacementAllRAM), q4WeightBytes))
	if ram.Status != StatusFail {
		t.Errorf("a FAIL with a witness mismatch = %s, want FAIL kept", ram.Status)
	}
}

// TestRunningOffloadVerdictUnchangedByTheSharedFold pins the chat verdict's bytes
// across the foldFloors extraction: the provenance string is part of the frozen
// status --json contract.
func TestRunningOffloadVerdictUnchangedByTheSharedFold(t *testing.T) {
	v := RunningOffloadVerdict(RunningOffloadInput{
		JournalText:  readFixture(t, "load_tensors_vulkan.txt"),
		GTTUsedBytes: detect.KnownBytes(23068672000, "mem_info_gtt_used"),
		WeightBytes:  testWeightBytes,
		Markers:      VulkanBackend().ResidencyProof(),
	})
	if v.Status != StatusPass {
		t.Fatalf("status = %s (%s)", v.Status, v.Detail)
	}
	if v.Provenance != "journald load_tensors Vulkan0 residency + point-in-time mem_info_gtt_used floor" {
		t.Errorf("provenance changed: %q", v.Provenance)
	}
}
