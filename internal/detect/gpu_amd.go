package detect

import (
	"bufio"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
)

// This file is the BACKEND SEAM: it is the only file in internal/detect allowed
// to know about Vulkan, /dev/dri, ROCm, or AMD specifics. The Phase 2 Backend
// interface slots in here without reshaping the (backend-neutral) HostProfile.
// A grep gate in the plan asserts these tokens appear nowhere else in the package.

// maxToolOutput bounds how much untrusted tool stdout we read/capture, so a
// runaway vulkaninfo/rocminfo cannot exhaust memory (threat T-01-02).
const maxToolOutput = 8 << 10 // 8 KiB

// radeonICDPath is the Mesa RADV ICD manifest. Its existence is the primary,
// structural Vulkan signal (vulkaninfo only enriches the device name).
const radeonICDPath = "/usr/share/vulkan/icd.d/radeon_icd.x86_64.json"

// liveDRIRoot is the device-node directory enumerated for GPU presence. It lives
// here (the backend seam) rather than in detect.go so the orchestrator carries
// no backend-specific path literals.
const liveDRIRoot = "/dev/dri"

// gpuInfo bundles every backend-specific (Vulkan/ROCm/DRI/AMD) detection result.
// detect.go consumes this struct and never names a backend itself, keeping the
// HostProfile assembly backend-neutral (the Phase 2 Backend seam).
type gpuInfo struct {
	deviceName  Str
	gfxID       Str
	icdPath     Str
	driNodes    []string
	driCount    Int
	rocmPresent Bool
	mesaVersion Str
}

// probeGPU performs all live backend probing behind the seam and returns a
// single struct. It never errors or panics; missing data degrades to Unknown.
func probeGPU() gpuInfo {
	names, count := driNodes(liveDRIRoot)
	return gpuInfo{
		deviceName:  liveVulkanDevice(),
		gfxID:       igpuGfxID(),
		icdPath:     vulkanICD(radeonICDPath),
		driNodes:    names,
		driCount:    count,
		rocmPresent: rocmPresent(),
		mesaVersion: liveMesaVersion(),
	}
}

// driNodes enumerates /dev/dri render/card device nodes. Structural enumeration
// is the primary GPU signal (preferred over text-scraping vulkaninfo). driRoot
// is a seam so tests can point at testdata/.
//
// It distinguishes "ran and found nothing" from "could not run" (WR-04, D-15):
//   - directory present but EMPTY → KnownInt(0): a confident known-absence the
//     PRE-01 BLOCK gate must hard-FAIL on (the iGPU is genuinely not visible).
//   - directory absent/unreadable → UnknownInt: the probe could not be evaluated,
//     which PRE-01 downgrades to WARN rather than a false block.
func driNodes(driRoot string) ([]string, Int) {
	// If the root itself cannot be stat'd, we could not evaluate enumeration at
	// all → Unknown (downgrade to WARN), distinct from "looked and found none".
	if _, err := os.Stat(driRoot); err != nil {
		return nil, UnknownInt("/dev/dri unreadable (could not enumerate)", errString(err))
	}
	render, _ := filepath.Glob(filepath.Join(driRoot, "render*"))
	cards, _ := filepath.Glob(filepath.Join(driRoot, "card*"))
	all := append(render, cards...)
	if len(all) == 0 {
		// Ran successfully and found nothing — a confident known-absence (BLOCK).
		return nil, KnownInt(0, driRoot)
	}
	names := make([]string, len(all))
	for i, p := range all {
		names[i] = filepath.Base(p)
	}
	return names, KnownInt(len(all), driRoot)
}

// vulkanICD reports the RADV ICD manifest path. icdPath is a seam for fixture
// testing.
//
// It distinguishes "ran and found nothing" from "could not run" (WR-04, D-15):
//   - manifest absent but its directory is readable → KnownStr(""): a confident
//     known-absence the PRE-01 BLOCK gate must hard-FAIL on.
//   - the manifest's directory cannot even be read (e.g. permission error) →
//     UnknownStr: the probe could not be evaluated → PRE-01 downgrades to WARN.
func vulkanICD(icdPath string) Str {
	if _, err := os.Stat(icdPath); err == nil {
		return KnownStr(icdPath, icdPath)
	}
	// The manifest is not present. Decide whether we could actually look: if the
	// containing directory is readable, this is a confident known-absence; if not,
	// we could not evaluate it.
	dir := filepath.Dir(icdPath)
	if _, err := os.ReadDir(dir); err != nil {
		return UnknownStr("RADV ICD dir unreadable (could not verify)", errString(err))
	}
	// Directory readable, manifest absent → confident known-absence (empty value).
	return KnownStr("", icdPath)
}

// isSoftwareRendererName reports whether a Vulkan deviceName belongs to a known
// CPU/software rasterizer rather than a real GPU. These renderers (llvmpipe,
// softpipe, lavapipe, swrast) are the silent-CPU-fallback path the detect/preflight
// stack exists to catch (Pitfall 3) — they must never be reported as the iGPU.
func isSoftwareRendererName(name string) bool {
	lower := strings.ToLower(name)
	for _, sw := range []string{"llvmpipe", "softpipe", "lavapipe", "swrast"} {
		if strings.Contains(lower, sw) {
			return true
		}
	}
	return false
}

// IsSoftwareRendererName is the exported reuse seam for the Phase-2 offload
// log-scrape (internal/inference): it shares the SAME renderer denylist as the
// detect/preflight stack so both layers reject the identical silent-CPU-fallback
// devices (llvmpipe/softpipe/lavapipe/swrast) without duplicating the list.
func IsSoftwareRendererName(name string) bool { return isSoftwareRendererName(name) }

// vulkanDeviceBlock holds the fields of one GPU block in `vulkaninfo --summary`.
type vulkanDeviceBlock struct {
	deviceName    string
	deviceType    string
	driverVersion string
}

// isSoftware reports whether this block is a CPU software renderer (by deviceType
// or by a known software-renderer deviceName). Device enumeration order is NOT
// guaranteed to put the real GPU first, so callers must filter on this, not pick
// the first block (WR-01).
func (b vulkanDeviceBlock) isSoftware() bool {
	if strings.Contains(strings.ToUpper(b.deviceType), "CPU") {
		return true
	}
	return isSoftwareRendererName(b.deviceName)
}

// parseVulkanDeviceBlocks splits `vulkaninfo --summary` output into per-GPU blocks
// (delimited by "GPUn:" headers), capturing each block's deviceName / deviceType /
// driverVersion. It is tolerant of missing fields — a block simply carries empty
// strings for anything not present.
func parseVulkanDeviceBlocks(vulkaninfoOutput string) []vulkanDeviceBlock {
	var blocks []vulkanDeviceBlock
	var cur *vulkanDeviceBlock
	flush := func() {
		if cur != nil {
			blocks = append(blocks, *cur)
			cur = nil
		}
	}
	sc := bufio.NewScanner(strings.NewReader(vulkaninfoOutput))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		switch {
		case strings.HasPrefix(line, "GPU") && strings.HasSuffix(line, ":"):
			// New device block (e.g. "GPU0:"). Flush any in-progress block.
			flush()
			cur = &vulkanDeviceBlock{}
		case cur == nil:
			// Field lines before the first GPU header are header/instance noise.
			continue
		case strings.HasPrefix(line, "deviceName"):
			if _, v, ok := strings.Cut(line, "="); ok {
				cur.deviceName = strings.TrimSpace(v)
			}
		case strings.HasPrefix(line, "deviceType"):
			if _, v, ok := strings.Cut(line, "="); ok {
				cur.deviceType = strings.TrimSpace(v)
			}
		case strings.HasPrefix(line, "driverVersion"):
			if _, v, ok := strings.Cut(line, "="); ok {
				cur.driverVersion = strings.TrimSpace(v)
			}
		}
	}
	flush()
	return blocks
}

// firstRealGPUBlock returns the first non-software GPU block, or nil if every
// enumerated device is a CPU software renderer (or none enumerated). Selecting the
// real GPU — explicitly skipping CPU renderers regardless of enumeration order —
// is the core WR-01 fix.
func firstRealGPUBlock(blocks []vulkanDeviceBlock) *vulkanDeviceBlock {
	for i := range blocks {
		if blocks[i].deviceName == "" {
			continue
		}
		if blocks[i].isSoftware() {
			continue
		}
		return &blocks[i]
	}
	return nil
}

// vulkanDevice extracts the real GPU deviceName from `vulkaninfo --summary` output,
// explicitly skipping CPU software renderers (llvmpipe/softpipe/lavapipe/swrast and
// any PHYSICAL_DEVICE_TYPE_CPU). Device enumeration order is not guaranteed to put
// the RADV iGPU first, so picking the first deviceName line would silently report
// the CPU fallback as the GPU (WR-01, Pitfall 3). If only a software renderer is
// present, that is NOT a usable GPU → typed Unknown (never the CPU device).
//
// It is tolerant: a parse miss yields typed Unknown with captured raw (D-15/D-16),
// never "absent". vulkaninfoOutput is the already-read tool output (so tests pass
// fixture bytes; production passes the live capture).
func vulkanDevice(vulkaninfoOutput string) Str {
	if strings.TrimSpace(vulkaninfoOutput) == "" {
		return UnknownStr("vulkaninfo produced no output", "")
	}
	blocks := parseVulkanDeviceBlocks(vulkaninfoOutput)
	if gpu := firstRealGPUBlock(blocks); gpu != nil {
		return KnownStr(gpu.deviceName, "vulkaninfo --summary:deviceName")
	}
	// Either no deviceName was found at all, or every enumerated device was a CPU
	// software renderer — neither is a usable GPU.
	for _, b := range blocks {
		if b.deviceName != "" && b.isSoftware() {
			return UnknownStr("only a CPU software renderer enumerated (no real GPU)", capRaw(vulkaninfoOutput))
		}
	}
	return UnknownStr("vulkaninfo deviceName not found", capRaw(vulkaninfoOutput))
}

// runTool invokes a tool with a FIXED argument slice (never sh -c, threat
// T-01-01) and returns its combined output bounded to maxToolOutput bytes. A
// missing binary or non-zero exit yields ok=false; the bounded output is still
// returned for raw capture.
func runTool(name string, args ...string) (out string, ok bool) {
	if _, err := exec.LookPath(name); err != nil {
		return "", false
	}
	cmd := exec.Command(name, args...) // fixed args; no shell interpolation
	raw, err := cmd.Output()
	bounded := raw
	if len(bounded) > maxToolOutput {
		bounded = bounded[:maxToolOutput]
	}
	return string(bounded), err == nil
}

// capRaw bounds a captured raw string for the -v provenance field.
func capRaw(s string) string {
	if len(s) > maxToolOutput {
		return s[:maxToolOutput]
	}
	return s
}

// rocmPresent reports whether rocminfo is installed. ROCm is the opt-in
// performance backend (Vulkan is the gfx1151 default), so absence is a confident
// false, not Unknown — informational, never blocking here (D-02/D-15).
func rocmPresent() Bool {
	if _, err := exec.LookPath("rocminfo"); err != nil {
		return KnownBool(false, "rocminfo not on PATH")
	}
	return KnownBool(true, "rocminfo present")
}

// liveVulkanDevice runs vulkaninfo on the live host and parses the device name.
func liveVulkanDevice() Str {
	out, ok := runTool("vulkaninfo", "--summary")
	if !ok {
		return UnknownStr("vulkaninfo unavailable or failed", capRaw(out))
	}
	return vulkanDevice(out)
}

// mesaVersion extracts the Mesa/RADV driverVersion from vulkaninfo output, scoped
// to the REAL GPU block (skipping CPU software renderers) so it never reports the
// llvmpipe driver version (WR-01). This gates Vulkan reliability in preflight;
// parse-fail degrades to Unknown (D-15).
func mesaVersion(vulkaninfoOutput string) Str {
	blocks := parseVulkanDeviceBlocks(vulkaninfoOutput)
	if gpu := firstRealGPUBlock(blocks); gpu != nil && gpu.driverVersion != "" {
		return KnownStr(gpu.driverVersion, "vulkaninfo --summary:driverVersion")
	}
	return UnknownStr("vulkaninfo driverVersion not found", capRaw(vulkaninfoOutput))
}

// liveMesaVersion runs vulkaninfo on the live host and parses the driver version.
func liveMesaVersion() Str {
	out, ok := runTool("vulkaninfo", "--summary")
	if !ok {
		return UnknownStr("vulkaninfo unavailable or failed", capRaw(out))
	}
	return mesaVersion(out)
}

// isGfxTargetID reports whether s is a bare gfx target ID of the form "gfx" + at
// least one digit (e.g. "gfx1151"), with no trailing junk. This rejects ISA-name
// lines like "amdgcn-amd-amdhsa--gfx1151" (which carry a prefix) and instruction-
// set blocks, anchoring igpuGfxID on the marketing Name field (IN-05).
func isGfxTargetID(s string) bool {
	if !strings.HasPrefix(s, "gfx") {
		return false
	}
	digits := s[len("gfx"):]
	if digits == "" {
		return false
	}
	for _, r := range digits {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

// igpuGfxID reports the gfx target ID (e.g. "gfx1151") from rocminfo when
// available, else Unknown. The iGPU still functions on Vulkan without rocminfo,
// so absence is informational.
//
// It anchors on the `Name:` field whose value is a BARE gfx target ID (IN-05),
// rather than accepting any "gfx"-bearing line — rocminfo output contains many
// such lines (ISA names like "amdgcn-amd-amdhsa--gfx1151", instruction-set blocks)
// and the first match is not guaranteed to be the marketing Name field.
func igpuGfxID() Str {
	out, ok := runTool("rocminfo")
	if !ok {
		return UnknownStr("rocminfo unavailable (gfx id not enumerated)", "")
	}
	return parseGfxID(out)
}

// parseGfxID extracts the bare gfx target ID from rocminfo output. It is the
// testable seam for igpuGfxID (tests pass fixture bytes). A parse miss degrades to
// typed Unknown with the raw captured (D-16), never a panic.
func parseGfxID(rocminfoOutput string) Str {
	sc := bufio.NewScanner(strings.NewReader(rocminfoOutput))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		key, val, ok := strings.Cut(line, ":")
		if !ok {
			continue
		}
		if strings.TrimSpace(key) != "Name" {
			continue
		}
		if id := strings.TrimSpace(val); isGfxTargetID(id) {
			return KnownStr(id, "rocminfo:Name")
		}
	}
	return UnknownStr("rocminfo gfx id not found", capRaw(rocminfoOutput))
}

// GPUBusyPercent reads the LIVE amdgpu gpu_busy_percent (0..100) from the real host
// DRM root — the DASH-03 iGPU utilization headline's best-effort overlay. amd-smi /
// rocm-smi report N/A for gfx1151 (ROCm #6035), so kernel sysfs is the source of
// truth (CLAUDE.md "never amd-smi"). The value is BEST-EFFORT (D-06, memory-first):
// it degrades to typed-Unknown ("Unavailable" in the panel) on a missing/garbage file
// rather than ever fabricating a number. It inherits the vendor-0x1002 discovery
// (never card0) + typed-Unknown shape from the memory readers' seam.
func GPUBusyPercent() Int { return gpuBusyPercent(liveDRMRoot) }

// GPUBusyPercentForTest exposes gpuBusyPercent against an INJECTED drmRoot so a
// sibling/dashboard test can read a busy% fixture through the real seam, mirroring
// GTTUsedBytesForTest. Test-only; production code uses GPUBusyPercent (live host root).
func GPUBusyPercentForTest(drmRoot string) Int { return gpuBusyPercent(drmRoot) }

// gpuBusyPercent reads gpu_busy_percent (a 0..100 integer) from the AMD card under
// drmRoot. It mirrors readAMDCardBytes (memory.go) but returns a detect.Int (busy% is
// a percentage, not a byte count): vendor-0x1002 card discovery via amdSysfsCardDirs
// (NEVER card0), with the flat drmRoot appended as a fixture fallback. A parse error →
// UnknownInt with the offending raw captured; not found across every candidate →
// UnknownInt "not found" (→ "unavailable", D-06). It never panics and never returns a
// bare zero as a real reading.
func gpuBusyPercent(drmRoot string) Int {
	candidates := amdSysfsCardDirs(drmRoot)
	candidates = append(candidates, drmRoot) // flat-fixture fallback

	for _, dir := range candidates {
		p := filepath.Join(dir, "gpu_busy_percent")
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		raw := strings.TrimSpace(string(b))
		v, err := strconv.Atoi(raw)
		if err != nil {
			return UnknownInt("gpu_busy_percent unparseable", string(b))
		}
		return KnownInt(v, p)
	}
	return UnknownInt("gpu_busy_percent not found", "")
}
