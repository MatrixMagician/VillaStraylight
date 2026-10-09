package inference

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// The whisper fixtures are the invocation journals of the pinned main-vulkan
// whisper-server, captured on gfx1151 on 2026-10-09 as transient user units (the
// Quadlet journal shape) with the unit's arguments: whisper_vulkan_journal.txt with
// --device /dev/dri, whisper_cpu_journal.txt with no device. Both transcribed the
// same clip; the Vulkan run in 1.80 s, the CPU run in 27.16 s.

const (
	whisperWeightBytes  = 1624555275
	whisperDeviceLine   = "Oct 09 11:31:59 neurodev villa-stt-332-vulkan[1934936]: ggml_vulkan: 0 = Radeon 8060S Graphics (RADV GFX1151) (radv) | uma: 1 | fp16: 1 | bf16: 0 | fp4: 0 | warp size: 64 | shared memory: 65536 | int dot: 0 | matrix cores: KHR_coopmat"
	whisperLlvmpipeLine = "Oct 09 11:31:59 neurodev villa-stt-332-vulkan[1934936]: ggml_vulkan: 0 = llvmpipe (LLVM 19.1.7, 256 bits) (llvmpipe) | uma: 0 | fp16: 1 | bf16: 0 | fp4: 0 | warp size: 8 | shared memory: 32768 | int dot: 0 | matrix cores: none"
	whisperLoadLine     = "Oct 09 11:31:59 neurodev villa-stt-332-vulkan[1934936]: whisper_model_load:      Vulkan0 total size =  1623.92 MB"
)

// whisperInput is the fold input the voice proof hands over: a clearing GTT floor,
// the busy reading sampled during the round trip, and the Vulkan markers villa-stt
// always runs under.
func whisperInput(journalText string, busy int) RunningOffloadInput {
	return RunningOffloadInput{
		JournalText:    journalText,
		GTTUsedBytes:   detect.KnownBytes(30908923904, "mem_info_gtt_used"),
		WeightBytes:    whisperWeightBytes,
		Markers:        VulkanBackend().ResidencyProof(),
		GPUBusyPercent: detect.KnownInt(busy, "gpu_busy_percent"),
	}
}

// TestWhisperOffloadVerdictReadsTheRealJournal: the model-load line decides. The
// Vulkan journal passes; the CPU journal fails with the Vulkan remediation even at
// 18% GPU busy, the most the CPU run measured while other work shared the iGPU, so
// the busy reading alone could not have caught the fallback.
func TestWhisperOffloadVerdictReadsTheRealJournal(t *testing.T) {
	cases := []struct {
		name        string
		journal     string
		busy        int
		status      Status
		detail      string
		remediation string
	}{
		{
			name:    "Vulkan",
			journal: readFixture(t, "whisper_vulkan_journal.txt"),
			busy:    59,
			status:  StatusPass,
			detail:  "offload proven (log + sysfs) — log: Vulkan0 model buffer 1623.92 MB resident on the iGPU; sysfs: GTT-used 30908923904 bytes ≥ 1624555275 weight footprint (resident); busy: gpu_busy_percent 59% during decode — corroborates GPU residency",
		},
		{
			name:        "CPU",
			journal:     readFixture(t, "whisper_cpu_journal.txt"),
			busy:        18,
			status:      StatusFail,
			detail:      "offload FAILED — log: only a CPU model buffer was loaded — server fell back to CPU; sysfs: GTT-used 30908923904 bytes ≥ 1624555275 weight footprint (resident); busy: gpu_busy_percent 18% during decode — corroborates GPU residency",
			remediation: "GPU offload did not engage — check /dev/dri passthrough, keep-groups, and that the RADV ICD is present (not llvmpipe)",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := WhisperOffloadVerdict(whisperInput(tc.journal, tc.busy))
			if v.Status != tc.status || v.Detail != tc.detail || v.Remediation != tc.remediation {
				t.Errorf("WhisperOffloadVerdict = {%v %q %q}\nwant                    {%v %q %q}", v.Status, v.Detail, v.Remediation, tc.status, tc.detail, tc.remediation)
			}
		})
	}
}

// TestWhisperOffloadVerdictPlacement is the rest of the placement rule over the
// same journal shape: a software renderer is not a GPU even with a Vulkan0 buffer,
// a zero-sized device buffer holds no weights, and a journal with no load line or
// no text at all could not be evaluated.
func TestWhisperOffloadVerdictPlacement(t *testing.T) {
	vulkan := readFixture(t, "whisper_vulkan_journal.txt")
	cases := []struct {
		name    string
		journal string
		status  Status
		signal  string
	}{
		{"llvmpipe enumerated", strings.Replace(vulkan, whisperDeviceLine, whisperLlvmpipeLine, 1), StatusFail, "vulkan device line"},
		{"zero-sized device buffer", strings.Replace(vulkan, "1623.92 MB\n", "0.00 MB\n", 1), StatusFail, "whisper_model_load Vulkan0 total size"},
		{"no model-load line", strings.Replace(vulkan, whisperLoadLine+"\n", "", 1), StatusWarn, "no whisper_model_load buffer line found in journal"},
		{"empty journal", "", StatusWarn, "journal empty/unreadable (could not evaluate residency)"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			v := WhisperOffloadVerdict(whisperInput(tc.journal, 59))
			if v.Status != tc.status || v.LogOffload.Source != tc.signal {
				t.Errorf("WhisperOffloadVerdict = %v (signal %q), want %v (signal %q); detail %q", v.Status, v.LogOffload.Source, tc.status, tc.signal, v.Detail)
			}
		})
	}
}

// TestWhisperOffloadVerdictFloorsAreShared: the GTT floor and the busy fold are the
// chat verdict's. A Vulkan journal at 0% busy during the drive is a FAIL, and the
// provenance names the whisper placement line and both floors.
func TestWhisperOffloadVerdictFloorsAreShared(t *testing.T) {
	vulkan := readFixture(t, "whisper_vulkan_journal.txt")
	if v := WhisperOffloadVerdict(whisperInput(vulkan, 0)); v.Status != StatusFail {
		t.Errorf("0%% busy during the drive = %v, want FAIL", v.Status)
	}
	want := "journald whisper_model_load Vulkan0 placement + point-in-time mem_info_gtt_used floor + gpu_busy_percent corroboration"
	if v := WhisperOffloadVerdict(whisperInput(vulkan, 59)); v.Provenance != want {
		t.Errorf("provenance = %q, want %q", v.Provenance, want)
	}
}
