package main

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
)

// update is declared in detect_test.go (shared -update flag for this package).

// passResults is an all-pass fixture (exit 0).
func passResults() []preflight.CheckResult {
	return []preflight.CheckResult{
		{ID: "PRE-01", Name: "Vulkan ICD + iGPU enumeration", Tier: preflight.TierBlock, Status: preflight.StatusPass, Detail: "RADV ICD present; 2 /dev/dri node(s)", Provenance: "icd; /dev/dri"},
		{ID: "PRE-02", Name: "Podman rootless-ready", Tier: preflight.TierBlock, Status: preflight.StatusPass, Detail: "podman present; subuid/subgid mapped; systemd --user reachable", Provenance: "podman --version"},
		{ID: "PRE-03", Name: "User lingering enabled", Tier: preflight.TierWarn, Status: preflight.StatusPass, Detail: "lingering is enabled", Provenance: "loginctl"},
		{ID: "PRE-04", Name: "Free disk + free memory", Tier: preflight.TierBlock, Status: preflight.StatusPass, Detail: "free memory and disk sufficient", Provenance: "statfs"},
	}
}

// warnResults adds a WARN (linger off) so the aggregate is exit 2.
func warnResults() []preflight.CheckResult {
	r := passResults()
	r[2] = preflight.CheckResult{ID: "PRE-03", Name: "User lingering enabled", Tier: preflight.TierWarn, Status: preflight.StatusWarn, Detail: "lingering is NOT enabled", Remediation: "loginctl enable-linger user", Provenance: "loginctl"}
	return r
}

// blockResults has a BLOCK fail (exit 1 without --force, override summary with).
func blockResults() []preflight.CheckResult {
	r := passResults()
	r[1] = preflight.CheckResult{ID: "PRE-02", Name: "Podman rootless-ready", Tier: preflight.TierBlock, Status: preflight.StatusFail, Detail: "no subordinate-id range — rootless not ready", Remediation: "add subuid/subgid ranges", Provenance: "/etc/subuid"}
	return r
}

func goldenPath(name string) string { return filepath.Join("testdata", name) }

func assertGolden(t *testing.T, name string, got []byte) {
	t.Helper()
	path := goldenPath(name)
	if *update {
		if err := os.WriteFile(path, got, 0o644); err != nil {
			t.Fatalf("write golden %s: %v", name, err)
		}
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden %s: %v (run -update)", name, err)
	}
	if !bytes.Equal(got, want) {
		t.Errorf("output mismatch for %s:\n--- got ---\n%s\n--- want ---\n%s", name, got, want)
	}
}

func TestPreflightExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		results  []preflight.CheckResult
		forced   bool
		wantCode int
		golden   string
	}{
		{"pass", passResults(), false, exitPass, "preflight-pass.golden"},
		{"warn", warnResults(), false, exitWarn, "preflight-warn.golden"},
		{"blocked", blockResults(), false, exitBlocked, ""},
		{"forced", blockResults(), true, exitWarn, "preflight-force.golden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := renderPreflight(&buf, tc.results, false, false, tc.forced)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if tc.golden != "" {
				assertGolden(t, tc.golden, buf.Bytes())
			}
		})
	}
}

func TestPreflightForceSummaryListsBypassedBlocks(t *testing.T) {
	var buf bytes.Buffer
	renderPreflight(&buf, blockResults(), false, false, true)
	out := buf.String()
	if !bytes.Contains(buf.Bytes(), []byte("Overridden BLOCK checks")) {
		t.Errorf("force output must contain the override summary header, got:\n%s", out)
	}
	if !bytes.Contains(buf.Bytes(), []byte("PRE-02")) {
		t.Errorf("force summary must name the bypassed BLOCK check PRE-02, got:\n%s", out)
	}
}

func TestPreflightBlockedWithoutForce(t *testing.T) {
	var buf bytes.Buffer
	code := renderPreflight(&buf, blockResults(), false, false, false)
	if code != exitBlocked {
		t.Errorf("code = %d, want %d", code, exitBlocked)
	}
	if !bytes.Contains(buf.Bytes(), []byte("BLOCKED")) {
		t.Errorf("blocked output must state it is BLOCKED, got:\n%s", buf.String())
	}
}

// TestPreflightConfirmedAbsentGPUBlocks is the WR-04 end-to-end regression: a
// CONFIRMED-absent Vulkan ICD and an empty /dev/dri enumeration (the probe ran and
// found nothing — KnownStr("") / KnownInt(0)) must drive PRE-01 to a BLOCK FAIL and
// map to exit 1, NOT downgrade to WARN/exit 2. This is the silent-CPU-fallback gate.
func TestPreflightConfirmedAbsentGPUBlocks(t *testing.T) {
	profile := detect.HostProfile{
		// Confirmed-absent: probe ran successfully and found nothing.
		VulkanICDPath: detect.KnownStr("", "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json"),
		DRINodeCount:  detect.KnownInt(0, "/dev/dri"),
		// Keep the other BLOCK checks evaluable enough that PRE-01 is the decider.
		MemAvailableBytes: detect.KnownBytes(64<<30, "/proc/meminfo"),
		KernelVersion:     detect.KnownStr("7.0.10-201.fc44.x86_64", "osrelease"),
	}
	results := preflight.Run(profile)

	var pre01 preflight.CheckResult
	for _, r := range results {
		if r.ID == "PRE-01" {
			pre01 = r
		}
	}
	if pre01.Tier != preflight.TierBlock || pre01.Status != preflight.StatusFail {
		t.Fatalf("confirmed-absent GPU: PRE-01 = tier %v/status %v, want BLOCK/FAIL", pre01.Tier, pre01.Status)
	}

	var buf bytes.Buffer
	code := renderPreflight(&buf, results, false, false, false)
	if code != exitBlocked {
		t.Errorf("confirmed-absent GPU exit code = %d, want %d (BLOCK)", code, exitBlocked)
	}
}

// TestPreflightBackendROCmOffHardware is the Pitfall-4 guard: an off-hardware
// `--backend rocm` invocation (every ROCm signal Unknown) renders the ROCM-PRE-*
// rows and maps to exit code 2 (WARN), NEVER exit 1. It drives the real
// preflight.RunROCm against a bare profile through the renderPreflight seam.
func TestPreflightBackendROCmOffHardware(t *testing.T) {
	results := preflight.RunROCm(detect.HostProfile{})

	var buf bytes.Buffer
	code := renderPreflight(&buf, results, false, false, false)
	if code != exitWarn {
		t.Errorf("off-hardware --backend rocm exit code = %d, want %d (WARN, never blocked)", code, exitWarn)
	}
	if code == exitBlocked {
		t.Fatalf("off-hardware --backend rocm must NEVER exit %d (BLOCKED)", exitBlocked)
	}
	out := buf.String()
	for _, id := range []string{"ROCM-PRE-gfx", "ROCM-PRE-kernel", "ROCM-PRE-image"} {
		if !bytes.Contains(buf.Bytes(), []byte(id)) {
			t.Errorf("--backend rocm output must render the %s row, got:\n%s", id, out)
		}
	}
}

// TestPreflightStandalonePathUnchanged confirms the default (no --backend) path
// still renders the v1.0 PRE-0N host checks and not the ROCm rows (D-03).
func TestPreflightStandalonePathUnchanged(t *testing.T) {
	var buf bytes.Buffer
	renderPreflight(&buf, passResults(), false, false, false)
	if !bytes.Contains(buf.Bytes(), []byte("PRE-01")) {
		t.Errorf("standalone preflight must render the PRE-01 host check, got:\n%s", buf.String())
	}
	if bytes.Contains(buf.Bytes(), []byte("ROCM-PRE-")) {
		t.Errorf("standalone preflight must NOT render ROCM-PRE-* rows, got:\n%s", buf.String())
	}
}

func TestPreflightJSONMode(t *testing.T) {
	var buf bytes.Buffer
	renderPreflight(&buf, passResults(), true, false, false)
	if !bytes.Contains(buf.Bytes(), []byte(`"id": "PRE-01"`)) {
		t.Errorf("--json output should include the check ids, got:\n%s", buf.String())
	}
}

// TestPreflightMemoryGateAppendsRows is the CTRL-06/D-06 memory-ON half: with
// the memory gate seam reporting enabled, the rendered preflight output carries
// the MEM-PRE-disk + MEM-PRE-headroom rows AFTER the standalone results,
// flowing through the unchanged renderPreflight + exit constants.
func TestPreflightMemoryGateAppendsRows(t *testing.T) {
	orig := memoryGateResults
	defer func() { memoryGateResults = orig }()
	memoryGateResults = func(p detect.HostProfile) []preflight.CheckResult {
		return preflight.RunMemory(p, preflight.MemoryGateInput{
			EmbeddingModel: "nomic-embed-text-v1.5",
			VolumeRoot:     func() (string, bool) { return "/volroot", true },
			Statfs:         func(string) (uint64, bool) { return 100 << 30, true },
		})
	}
	profile := detect.HostProfile{MemAvailableBytes: detect.KnownBytes(64<<30, "/proc/meminfo:MemAvailable")}
	results := append(passResults(), memoryGateResults(profile)...)

	var buf bytes.Buffer
	code := renderPreflight(&buf, results, false, false, false)
	if code != exitPass {
		t.Fatalf("all-pass with memory rows should exit %d, got %d:\n%s", exitPass, code, buf.String())
	}
	for _, id := range []string{"MEM-PRE-disk", "MEM-PRE-headroom"} {
		if !bytes.Contains(buf.Bytes(), []byte(id)) {
			t.Errorf("memory-on preflight output must render the %s row, got:\n%s", id, buf.String())
		}
	}
}

// TestLiveMemoryGateOffPath is the CTRL-06/D-06/T-22-08 memory-OFF half: an
// absent config (fail-soft) and a persisted memory-off config both yield nil —
// nothing is appended, so the memory-off output stays byte-identical to v1.2
// (the frozen preflight goldens are the net) and the verb gains no error path.
// The memory-ON live path returns exactly the two MEM-PRE checks in order.
func TestLiveMemoryGateOffPath(t *testing.T) {
	t.Run("absent config fails soft to nil", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		if got := liveMemoryGateResults(detect.HostProfile{}); got != nil {
			t.Fatalf("absent config must append NO memory checks, got %d", len(got))
		}
	})

	t.Run("persisted memory-off config yields nil", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		cfg := config.DefaultVillaConfig()
		if cfg.MemoryEnabled {
			t.Fatal("DefaultVillaConfig must be memory-off (D-04)")
		}
		if err := config.SaveVilla(cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}
		if got := liveMemoryGateResults(detect.HostProfile{}); got != nil {
			t.Fatalf("memory-off config must append NO memory checks, got %d", len(got))
		}
	})

	t.Run("persisted memory-on config yields both MEM-PRE checks", func(t *testing.T) {
		t.Setenv("XDG_CONFIG_HOME", t.TempDir())
		cfg := config.DefaultVillaConfig()
		cfg.MemoryEnabled = true
		if err := config.SaveVilla(cfg); err != nil {
			t.Fatalf("save config: %v", err)
		}
		got := liveMemoryGateResults(detect.HostProfile{})
		if len(got) != 2 {
			t.Fatalf("memory-on gate must return exactly 2 checks, got %d", len(got))
		}
		if got[0].ID != "MEM-PRE-disk" || got[1].ID != "MEM-PRE-headroom" {
			t.Fatalf("want stable order [MEM-PRE-disk, MEM-PRE-headroom], got [%s, %s]", got[0].ID, got[1].ID)
		}
	})
}
