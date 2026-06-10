// checks_memory.go implements the OPT-IN memory-stack host-fitness gates
// (CTRL-06, D-06/D-07): MEM-PRE-disk proves the rootless Podman volume storage
// root (where the Qdrant vector index lives) clears a free-disk floor, and
// MEM-PRE-headroom proves free memory clears the embedding-model footprint
// reservation. Both are TierBlock with refuse-with-remediation on every non-PASS
// result. A confident shortage is a FAIL; an unevaluable probe (podman missing,
// statfs error, Unknown MemAvailable) is a typed-Unknown WARN — never a false
// hard block, never a false-green (D-15 discipline).
//
// Emission is the CALLER's decision (D-06): RunMemory is a separate exported
// runner the cmd tier appends only when memory_enabled — Run/RunWithResources
// and the frozen PRE-01..07 sequence are untouched, so the memory-off preflight
// output stays byte-identical. This package never loads config (pure-core rule).
package preflight

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
)

// minVectorDiskFloorBytes is the conservative free-disk floor at the podman
// volume storage root for the Qdrant vector index. A 768-dim chunk costs roughly
// 5-6 KiB including HNSW links and payload, so 1 GiB covers on the order of
// ~180k-230k indexed chunks — this is a coarse floor that catches a nearly-full
// disk, NOT a sizing envelope (the index grows with indexed chats/documents).
const minVectorDiskFloorBytes uint64 = 1 << 30 // 1 GiB

// volumeRootFn is the injectable rootless-podman volume-root resolver seam
// (mirrors statfsFunc) so tests assert the disk gate without a real podman. It
// returns the volume storage root path and whether resolution succeeded.
type volumeRootFn func() (path string, ok bool)

// liveVolumeRoot resolves the rootless Podman volume storage root (live-verified
// shape: ~/.local/share/containers/storage/volumes — NEVER hardcoded, D-07) by
// asking podman itself through the package's bounded fixed-arg runTool seam
// (T-22-05: no shell, stdout capped at maxToolOutput; the output is used ONLY as
// a statfs path, never executed or interpolated). A missing podman, non-zero
// exit, or empty output yields ok=false so the check downgrades to WARN (D-15).
func liveVolumeRoot() (string, bool) {
	out, found, ok := runTool("podman", "system", "info", "--format", "{{.Store.VolumePath}}")
	if !found || !ok {
		return "", false
	}
	root := strings.TrimSpace(out)
	if root == "" {
		return "", false
	}
	return root, true
}

// MemoryGateInput carries the memory-gate thresholds plus the injected probe
// seams (mirroring ResourceReq + the statfs-injection idiom) so tests assert
// disk-too-small without a real undersized filesystem. Zero-value seams and a
// zero MinDiskBytes are bound to live defaults by RunMemory.
type MemoryGateInput struct {
	// EmbeddingModel is the configured embedding model id whose footprint
	// (memory.Footprint, single source D-01/D-02) sets the headroom floor.
	EmbeddingModel string
	// MinDiskBytes is the free-disk floor at the volume root; 0 means the
	// minVectorDiskFloorBytes default.
	MinDiskBytes uint64
	// VolumeRoot resolves the rootless podman volume storage root (nil → live).
	VolumeRoot volumeRootFn
	// Statfs reads free bytes at a path (nil → the package's liveStatfs).
	Statfs statfsFunc
}

// checkVectorDisk is MEM-PRE-disk (BLOCK): free disk at the rootless Podman
// volume storage root — where the Qdrant vector index lives — must clear the
// floor or indexing fills the disk. Confident shortage → FAIL; unresolvable
// root or failed statfs → typed-Unknown WARN (D-07).
func checkVectorDisk(in MemoryGateInput) CheckResult {
	const (
		id   = "MEM-PRE-disk"
		name = "Vector-index disk space"
	)

	root, ok := in.VolumeRoot()
	if !ok {
		return warn(id, name, TierBlock,
			"could not resolve the rootless podman volume storage root",
			"Ensure podman is installed and `podman system info` works for this user, then re-run `villa preflight`; or disable memory_enabled in config.toml.",
			"podman system info --format {{.Store.VolumePath}}", "")
	}

	freeDisk, ok := in.Statfs(root)
	if !ok {
		return warn(id, name, TierBlock,
			fmt.Sprintf("could not statfs %q to verify free disk for the vector index", root),
			"Verify the podman volume storage root exists and is readable, then re-run `villa preflight`; or disable memory_enabled in config.toml.",
			"syscall.Statfs", "")
	}

	if freeDisk < in.MinDiskBytes {
		return fail(id, name,
			fmt.Sprintf("free disk %s at %q < required floor %s for the vector index", humanGiB(freeDisk), root, humanGiB(in.MinDiskBytes)),
			fmt.Sprintf("Free up disk under %q — the Qdrant vector index lives there and grows with indexed chats/documents; or disable memory_enabled in config.toml.", root),
			"syscall.Statfs:"+root, "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("free disk %s ≥ %s at %q", humanGiB(freeDisk), humanGiB(in.MinDiskBytes), root),
		"syscall.Statfs:"+root)
}

// checkEmbedHeadroom is MEM-PRE-headroom (BLOCK): free memory must clear the
// embedding-model footprint reservation or the embedder OOMs the shared gfx1151
// unified-memory pool. The floor comes from memory.Footprint (single source);
// an unrecognized model falls back to memory.ConservativeFootprintBytes() (D-02
// — never a zero floor) and the check still evaluates. Unknown MemAvailable →
// typed-Unknown WARN with the probe's provenance (D-07).
func checkEmbedHeadroom(p detect.HostProfile, in MemoryGateInput) CheckResult {
	const (
		id   = "MEM-PRE-headroom"
		name = "Embedder memory headroom"
	)
	remediation := "Close memory-heavy processes to free RAM for the embedding server, or disable memory_enabled in config.toml."

	fp := memory.Footprint(in.EmbeddingModel)
	floor := fp.Value
	floorProvenance := fp.Source
	if !fp.Known {
		// D-02: unrecognized embedding model → conservative default, never 0.
		floor = memory.ConservativeFootprintBytes()
		floorProvenance = "memory.ConservativeFootprintBytes (no pinned footprint for " + in.EmbeddingModel + ")"
	}

	if !p.MemAvailableBytes.Known {
		return warn(id, name, TierBlock,
			"could not verify free memory against the embedding reservation",
			remediation,
			p.MemAvailableBytes.Source,
			p.MemAvailableBytes.Raw)
	}

	if p.MemAvailableBytes.Value < floor {
		return fail(id, name,
			fmt.Sprintf("free memory %s < embedding reservation %s", humanGiB(p.MemAvailableBytes.Value), humanGiB(floor)),
			remediation, joinProvenance(p.MemAvailableBytes.Source, floorProvenance), "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("free memory %s ≥ embedding reservation %s", humanGiB(p.MemAvailableBytes.Value), humanGiB(floor)),
		joinProvenance(p.MemAvailableBytes.Source, floorProvenance))
}

// RunMemory executes the opt-in memory-stack gates and returns them in stable
// order: MEM-PRE-disk then MEM-PRE-headroom. Nil seams and a zero disk floor
// bind to live defaults (liveVolumeRoot, liveStatfs, minVectorDiskFloorBytes).
// It is pure beyond the injected probes: no os.Exit, no printing, no config
// load — whether these checks are EMITTED at all is the caller's decision
// (D-06), keyed on memory_enabled in the cmd tier.
func RunMemory(p detect.HostProfile, in MemoryGateInput) []CheckResult {
	if in.VolumeRoot == nil {
		in.VolumeRoot = liveVolumeRoot
	}
	if in.Statfs == nil {
		in.Statfs = liveStatfs
	}
	if in.MinDiskBytes == 0 {
		in.MinDiskBytes = minVectorDiskFloorBytes
	}
	return []CheckResult{
		checkVectorDisk(in),
		checkEmbedHeadroom(p, in),
	}
}
