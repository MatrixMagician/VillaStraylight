package detect

import (
	"os"
	"path/filepath"
	"testing"
)

func TestVulkanDeviceFromFixture(t *testing.T) {
	out, err := os.ReadFile("testdata/vulkaninfo-summary.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dev := vulkanDevice(string(out))
	if !dev.Known {
		t.Fatalf("vulkanDevice: Known=false, source=%q", dev.Source)
	}
	const want = "AMD Radeon 8060S Graphics (RADV STRIX_HALO)"
	if dev.Value != want {
		t.Errorf("vulkanDevice: Value=%q, want %q", dev.Value, want)
	}
}

func TestVulkanDeviceGarbageYieldsUnknown(t *testing.T) {
	dev := vulkanDevice("this output has no deviceName line at all\n")
	if dev.Known {
		t.Errorf("vulkanDevice(garbage): Known=true, want false")
	}
	if dev.Raw == "" {
		t.Errorf("vulkanDevice(garbage): Raw empty, want captured output")
	}

	empty := vulkanDevice("")
	if empty.Known {
		t.Errorf("vulkanDevice(empty): Known=true, want false")
	}
}

// TestVulkanDeviceSkipsCPUWhenEnumeratedFirst is the WR-01 regression: the
// llvmpipe CPU software renderer enumerates as GPU0 (BEFORE the RADV iGPU at GPU1).
// vulkanDevice must select the real iGPU, never the CPU fallback — reporting the
// software renderer as the GPU is the exact silent-CPU-fallback failure mode the
// stack exists to catch (Pitfall 3).
func TestVulkanDeviceSkipsCPUWhenEnumeratedFirst(t *testing.T) {
	out, err := os.ReadFile("testdata/vulkaninfo-cpu-first.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dev := vulkanDevice(string(out))
	if !dev.Known {
		t.Fatalf("vulkanDevice: Known=false, source=%q", dev.Source)
	}
	const want = "AMD Radeon 8060S Graphics (RADV STRIX_HALO)"
	if dev.Value != want {
		t.Errorf("vulkanDevice: Value=%q, want the iGPU %q (must not pick llvmpipe)", dev.Value, want)
	}
	if isSoftwareRendererName(dev.Value) {
		t.Errorf("vulkanDevice picked a CPU software renderer %q", dev.Value)
	}

	// mesaVersion must also bind to the iGPU block, not the llvmpipe block.
	mesa := mesaVersion(string(out))
	if !mesa.Known || mesa.Value != "26.0.8" {
		t.Errorf("mesaVersion: got %+v, want the iGPU driverVersion 26.0.8", mesa)
	}
}

// TestVulkanDeviceCPUOnlyYieldsUnknown asserts that when ONLY a CPU software
// renderer is enumerated (no real GPU), vulkanDevice returns typed Unknown — never
// the CPU device — so preflight does not treat a software fallback as a usable GPU.
func TestVulkanDeviceCPUOnlyYieldsUnknown(t *testing.T) {
	out, err := os.ReadFile("testdata/vulkaninfo-cpu-only.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	dev := vulkanDevice(string(out))
	if dev.Known {
		t.Errorf("vulkanDevice(cpu-only): Known=true (value=%q), want Unknown", dev.Value)
	}
	if dev.Raw == "" {
		t.Errorf("vulkanDevice(cpu-only): Raw empty, want captured output")
	}
}

func TestMesaVersionFromFixture(t *testing.T) {
	out, err := os.ReadFile("testdata/vulkaninfo-summary.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	mesa := mesaVersion(string(out))
	if !mesa.Known {
		t.Fatalf("mesaVersion: Known=false, source=%q", mesa.Source)
	}
	if mesa.Value != "26.0.8" {
		t.Errorf("mesaVersion: Value=%q, want 26.0.8", mesa.Value)
	}
}

// TestDriNodesFromFixture builds a fake /dev/dri from the captured listing and
// asserts a positive node count.
func TestDriNodesFromFixture(t *testing.T) {
	dir := t.TempDir()
	for _, name := range []string{"card1", "renderD128"} {
		if err := os.WriteFile(filepath.Join(dir, name), nil, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	names, count := driNodes(dir)
	if !count.Known {
		t.Fatalf("driNodes: Known=false")
	}
	if count.Value != 2 {
		t.Errorf("driNodes: count=%d, want 2", count.Value)
	}
	if len(names) != 2 {
		t.Errorf("driNodes: names=%v, want 2 entries", names)
	}
}

// TestDriNodesEmptyIsKnownAbsent asserts the WR-04 contract: a readable-but-empty
// /dev/dri is a CONFIDENT known-absence (KnownInt(0)) — distinct from "could not
// enumerate" — so PRE-01 can BLOCK-FAIL on a genuinely invisible iGPU.
func TestDriNodesEmptyIsKnownAbsent(t *testing.T) {
	_, count := driNodes(t.TempDir())
	if !count.Known {
		t.Errorf("driNodes(empty dir): Known=false, want a confident known-absence (0)")
	}
	if count.Value != 0 {
		t.Errorf("driNodes(empty dir): Value=%d, want 0", count.Value)
	}
}

// TestDriNodesUnreadableYieldsUnknown asserts that an absent/unreadable /dev/dri
// root is Unknown ("could not enumerate") — distinct from empty — so PRE-01
// downgrades to WARN per D-15 rather than a false block.
func TestDriNodesUnreadableYieldsUnknown(t *testing.T) {
	_, count := driNodes(filepath.Join(t.TempDir(), "does-not-exist"))
	if count.Known {
		t.Errorf("driNodes(unreadable root): Known=true, want Unknown")
	}
}

func TestVulkanICDPresence(t *testing.T) {
	dir := t.TempDir()
	icd := filepath.Join(dir, "radeon_icd.x86_64.json")
	if err := os.WriteFile(icd, []byte("{}"), 0o644); err != nil {
		t.Fatal(err)
	}
	if got := vulkanICD(icd); !got.Known || got.Value != icd {
		t.Errorf("vulkanICD(present): got %+v", got)
	}
	// WR-04: manifest absent but its directory is readable → CONFIDENT known-
	// absence (Known=true, empty value) so PRE-01 can BLOCK-FAIL.
	if got := vulkanICD(filepath.Join(dir, "absent.json")); !got.Known || got.Value != "" {
		t.Errorf("vulkanICD(absent, dir readable): got %+v, want Known empty (known-absence)", got)
	}
	// Manifest absent AND its directory unreadable → Unknown (could not verify).
	if got := vulkanICD(filepath.Join(dir, "no-such-dir", "absent.json")); got.Known {
		t.Errorf("vulkanICD(absent, dir unreadable): Known=true, want Unknown")
	}
}

// TestParseGfxIDFromFixture is the IN-05 regression: rocminfo output contains
// several "gfx"-bearing lines (the ISA name "amdgcn-amd-amdhsa--gfx1151" and an
// ISA block with its own "Name:" key) — the parser must anchor on the agent's
// bare "Name: gfx1151" field and return exactly "gfx1151", never an ISA token.
func TestParseGfxIDFromFixture(t *testing.T) {
	out, err := os.ReadFile("testdata/rocminfo-present.txt")
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	got := parseGfxID(string(out))
	if !got.Known {
		t.Fatalf("parseGfxID: Known=false, source=%q", got.Source)
	}
	if got.Value != "gfx1151" {
		t.Errorf("parseGfxID: Value=%q, want bare \"gfx1151\" (not an ISA token)", got.Value)
	}
}

func TestIsGfxTargetID(t *testing.T) {
	cases := map[string]bool{
		"gfx1151":                    true,
		"gfx900":                     true,
		"amdgcn-amd-amdhsa--gfx1151": false, // ISA name with a prefix
		"gfx":                        false, // no digits
		"gfx1151 ":                   false, // trailing junk (callers Trim, but guard anyway)
		"GFX1151":                    false, // case-sensitive bare token
		"":                           false,
	}
	for in, want := range cases {
		if got := isGfxTargetID(in); got != want {
			t.Errorf("isGfxTargetID(%q) = %v, want %v", in, got, want)
		}
	}
}

func TestParseGfxIDGarbageYieldsUnknown(t *testing.T) {
	got := parseGfxID("Name: amdgcn-amd-amdhsa--gfx1151\nName: not-a-gfx\n")
	if got.Known {
		t.Errorf("parseGfxID(no bare gfx Name): Known=true (%q), want Unknown", got.Value)
	}
}

// TestRocmAbsentNeverPanics asserts rocmPresent yields a typed Bool (not a
// panic) regardless of whether rocminfo is installed on the test host. On this
// dev box rocminfo is absent (rocminfo-absent.txt fixture), so we expect a
// confident Known=false; on a box with rocminfo it would be Known=true. Either
// way it must be Known (informational, never Unknown) and never panic.
func TestRocmAbsentNeverPanics(t *testing.T) {
	got := rocmPresent()
	if !got.Known {
		t.Errorf("rocmPresent: Known=false, want a confident true/false (D-02)")
	}
}

// TestRocmImagePolicyOK644 asserts rocmImagePolicyOK recognizes the rocm-6.4.4
// images as pinned-stable (Pitfall 3 — honest rocm_readiness.image_policy_ok), the
// rocm7-nightlies tag stays a confident KnownBool(false), and an unrecognized image
// still degrades to UnknownBool (never a false confident verdict).
func TestRocmImagePolicyOK644(t *testing.T) {
	t.Run("rocm-6.4.4 is pinned stable", func(t *testing.T) {
		got := rocmImagePolicyOK("docker.io/kyuz0/x:rocm-6.4.4@sha256:abc")
		if !got.Known || !got.Value {
			t.Errorf("rocm-6.4.4 → Known=%v Value=%v, want KnownBool(true)", got.Known, got.Value)
		}
	})
	t.Run("rocm-6.4.4-rocwmma is pinned stable (substring superset)", func(t *testing.T) {
		got := rocmImagePolicyOK("docker.io/kyuz0/x:rocm-6.4.4-rocwmma@sha256:def")
		if !got.Known || !got.Value {
			t.Errorf("rocm-6.4.4-rocwmma → Known=%v Value=%v, want KnownBool(true)", got.Known, got.Value)
		}
	})
	t.Run("rocm7-nightlies stays denied", func(t *testing.T) {
		got := rocmImagePolicyOK("docker.io/kyuz0/x:rocm7-nightlies@sha256:bad")
		if !got.Known || got.Value {
			t.Errorf("rocm7-nightlies → Known=%v Value=%v, want KnownBool(false)", got.Known, got.Value)
		}
	})
	t.Run("unrecognized image stays Unknown", func(t *testing.T) {
		got := rocmImagePolicyOK("docker.io/kyuz0/x:rocm-9.9.9@sha256:huh")
		if got.Known {
			t.Errorf("unrecognized image → Known=true, want UnknownBool")
		}
	})
}
