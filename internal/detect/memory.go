package detect

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/jaypipes/ghw"
)

// pageSize is the assumed page size for the ttm.pages_limit cross-check (amdgpu
// reports pages_limit in 4 KiB pages on x86-64).
const pageSize = 4096

// memInfo returns total physical RAM (via ghw) and the live MemAvailable (hand
// parsed from /proc/meminfo). Neither is the GPU envelope — see usableEnvelope.
func memInfo(procMeminfoPath string) (total Bytes, available Bytes) {
	mem, err := ghw.Memory()
	if err != nil || mem == nil || mem.TotalPhysicalBytes <= 0 {
		reason := "ghw memory total unavailable"
		if err != nil {
			reason = "ghw.Memory error"
		}
		total = UnknownBytes(reason, errString(err))
	} else {
		total = KnownBytes(uint64(mem.TotalPhysicalBytes), "ghw.Memory")
	}

	available = memAvailableBytes(procMeminfoPath)
	return total, available
}

// memAvailableBytes parses the MemAvailable line of /proc/meminfo (reported in
// kB). We hand-parse this because ghw's "usable" is DMI-derived, not the live
// kernel MemAvailable that preflight's free-memory check needs.
func memAvailableBytes(path string) Bytes {
	f, err := os.Open(path)
	if err != nil {
		return UnknownBytes("meminfo unreadable", errString(err))
	}
	defer f.Close()

	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := sc.Text()
		if !strings.HasPrefix(line, "MemAvailable:") {
			continue
		}
		fields := strings.Fields(line)
		// Expected shape: "MemAvailable:  123456 kB"
		if len(fields) < 2 {
			return UnknownBytes("MemAvailable malformed", line)
		}
		kb, err := strconv.ParseUint(fields[1], 10, 64)
		if err != nil {
			return UnknownBytes("MemAvailable unparseable", line)
		}
		return KnownBytes(kb*1024, path+":MemAvailable")
	}
	// A scan error (truncated/interrupted read) is otherwise indistinguishable
	// from a clean EOF; surface it as the real reason instead of mislabeling an
	// I/O failure as "MemAvailable not found" (WR-05, D-16).
	if err := sc.Err(); err != nil {
		return UnknownBytes("meminfo read error", err.Error())
	}
	return UnknownBytes("MemAvailable not found", "")
}

// amdSysfsCardDirs returns the device directories of AMD DRM cards (vendor
// 0x1002) under the given /sys/class/drm root, never hard-coding card0
// (Pitfall 2 — the AMD iGPU may be card1+). The root is a seam so tests can
// point it at testdata/.
//
// When the root contains AMD-vendor cards we return only those; when no
// vendor file is present at all (e.g. a flat testdata dir holding raw fixture
// files), the caller falls back to scanning the root directly.
func amdSysfsCardDirs(drmRoot string) []string {
	matches, _ := filepath.Glob(filepath.Join(drmRoot, "card*", "device"))
	var amd []string
	for _, dir := range matches {
		v, err := os.ReadFile(filepath.Join(dir, "vendor"))
		if err != nil {
			continue
		}
		if strings.TrimSpace(string(v)) == "0x1002" {
			amd = append(amd, dir)
		}
	}
	return amd
}

// gttTotalBytes reads mem_info_gtt_total — the authoritative live GTT ceiling —
// from the AMD card under drmRoot. This is THE usable-envelope source; it is
// never MemTotal and never the BIOS VRAM carve-out (Pitfall 1).
func gttTotalBytes(drmRoot string) Bytes {
	return readAMDCardBytes(drmRoot, "mem_info_gtt_total", 1, "unparseable gtt_total", "mem_info_gtt_total not found")
}

// gttUsedBytes reads mem_info_gtt_used — the live GTT (unified-memory) bytes
// currently in use — from the AMD card under drmRoot. It is the before/after
// signal for the Phase-2 offload delta assert (D-09.2): with --no-mmap, a model's
// weights become resident in GTT, so a positive used-delta across container start
// proves real iGPU offload. It inherits vendor-0x1002 discovery + typed-Unknown
// degradation from readAMDCardBytes for free, and never hard-codes card0.
func gttUsedBytes(drmRoot string) Bytes {
	return readAMDCardBytes(drmRoot, "mem_info_gtt_used", 1, "unparseable gtt_used", "mem_info_gtt_used not found")
}

// vramUsedBytes reads mem_info_vram_used — the live BIOS-VRAM-carveout bytes in
// use — a secondary offload-delta signal alongside gttUsedBytes (D-09.2). Same
// typed-Unknown contract.
func vramUsedBytes(drmRoot string) Bytes {
	return readAMDCardBytes(drmRoot, "mem_info_vram_used", 1, "unparseable vram_used", "mem_info_vram_used not found")
}

// GTTUsedBytes reads the LIVE amdgpu mem_info_gtt_used from the real host DRM root.
// It is the production before/after offload-delta signal the Phase-2 inference
// validate verb folds into its Verdict (D-09.2): with --no-mmap a model's weights
// become resident in GTT, so a positive used-delta across container start proves
// real iGPU offload. It inherits vendor-0x1002 discovery + typed-Unknown degradation
// from readAMDCardBytes (never card0, never a bare zero on a missing file).
func GTTUsedBytes() Bytes { return gttUsedBytes(liveDRMRoot) }

// GTTUsedBytesForTest exposes gttUsedBytes against an INJECTED drmRoot so sibling-
// package (internal/inference) tests can read a sysfs-used fixture through the real
// seam. It is test-only; production code uses GTTUsedBytes (live host root).
func GTTUsedBytesForTest(drmRoot string) Bytes { return gttUsedBytes(drmRoot) }

// biosVRAMBytes reads mem_info_vram_total — the BIOS VRAM carve-out. Recorded
// for provenance but NEVER used as the envelope.
func biosVRAMBytes(drmRoot string) Bytes {
	return readAMDCardBytes(drmRoot, "mem_info_vram_total", 1, "unparseable vram_total", "mem_info_vram_total not found")
}

// readAMDCardBytes reads a single numeric sysfs file from the AMD card device
// dir, multiplies by mult, and returns a typed value. It first tries
// vendor-filtered card dirs, then falls back to a flat drmRoot (so tests can
// drop the raw file directly into testdata/).
func readAMDCardBytes(drmRoot, file string, mult uint64, parseErr, missingErr string) Bytes {
	candidates := amdSysfsCardDirs(drmRoot)
	candidates = append(candidates, drmRoot) // flat-fixture fallback

	for _, dir := range candidates {
		p := filepath.Join(dir, file)
		b, err := os.ReadFile(p)
		if err != nil {
			continue
		}
		raw := strings.TrimSpace(string(b))
		v, err := strconv.ParseUint(raw, 10, 64)
		if err != nil {
			return UnknownBytes(parseErr, string(b))
		}
		return KnownBytes(v*mult, p)
	}
	return UnknownBytes(missingErr, "")
}

// ttmLimitBytes reads /sys/module/ttm/parameters/pages_limit (in pages) and
// multiplies by the page size to cross-check / back up the GTT ceiling. The
// path is a seam for fixture testing.
func ttmLimitBytes(ttmPagesLimitPath string) Bytes {
	b, err := os.ReadFile(ttmPagesLimitPath)
	if err != nil {
		return UnknownBytes("ttm pages_limit unreadable", errString(err))
	}
	raw := strings.TrimSpace(string(b))
	pages, err := strconv.ParseUint(raw, 10, 64)
	if err != nil {
		return UnknownBytes("ttm pages_limit unparseable", string(b))
	}
	return KnownBytes(pages*pageSize, ttmPagesLimitPath)
}

// kernelVersion reads the running kernel release from /proc/sys/kernel/osrelease.
// It is a floor gate (preflight refuses/warns below a threshold), never an
// envelope multiplier. The path is a seam for fixture testing.
func kernelVersion(osreleasePath string) Str {
	b, err := os.ReadFile(osreleasePath)
	if err != nil {
		return UnknownStr("kernel osrelease unreadable", errString(err))
	}
	v := strings.TrimSpace(string(b))
	if v == "" {
		return UnknownStr("kernel osrelease blank", "")
	}
	return KnownStr(v, osreleasePath)
}

// usableEnvelope computes the usable unified-memory ceiling. It returns GTT when
// known, else falls back to the ttm cross-check, else typed Unknown. It NEVER
// derives the envelope from MemTotal — kernel version is a floor gate, not an
// envelope multiplier (Pitfall 1, the OOM guard).
func usableEnvelope(gtt, ttm Bytes) Bytes {
	if gtt.Known {
		return KnownBytes(gtt.Value, "mem_info_gtt_total ("+gtt.Source+")")
	}
	if ttm.Known {
		return KnownBytes(ttm.Value, "ttm.pages_limit fallback ("+ttm.Source+")")
	}
	return UnknownBytes("envelope: neither mem_info_gtt_total nor ttm.pages_limit readable", "")
}
