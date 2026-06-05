package preflight

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// ResourceReq are the thresholds PRE-04 gates on: the install needs at least
// MinDiskBytes of free space at DataDir (for model weights) and at least
// MinMemBytes of available memory (the model's runtime requirement). Standalone
// `villa preflight` (Run) is model-agnostic and supplies the smallest-installable-
// model FLOORS here (not the full GTT envelope, which would over-block — WR-02/
// WR-03); Phase 3 install supplies real per-model weights+KV+headroom numbers via
// RunWithResources.
type ResourceReq struct {
	MinDiskBytes uint64
	MinMemBytes  uint64
	DataDir      string
}

// statfsFunc is the injectable free-disk seam so tests assert disk-too-small
// without needing a real undersized filesystem. It returns the free bytes at path
// and whether the statfs succeeded.
type statfsFunc func(path string) (freeBytes uint64, ok bool)

// liveStatfs reads real free space via syscall.Statfs — structured, locale-proof,
// and NOT shelling to `df` ("Don't Hand-Roll", T-03-01). It walks up to an
// existing ancestor so a not-yet-created data dir still reports its filesystem's
// free space.
func liveStatfs(path string) (uint64, bool) {
	p := existingAncestor(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, false
	}
	// Available blocks to an unprivileged user × block size.
	return st.Bavail * uint64(st.Bsize), true
}

// existingAncestor returns path if it exists, else the nearest existing parent
// (down to "/"), so statfs has a real path to stat for a target dir the install
// has not created yet.
func existingAncestor(path string) string {
	if path == "" {
		return "/"
	}
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "/"
		}
		p = parent
	}
}

// defaultDataDir is where the install will place model weights — the XDG data dir
// for villa, defaulting under $HOME. Used as the statfs target for the disk check.
func defaultDataDir() string {
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa")
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa")
	}
	return "/var/tmp"
}

// checkResources is PRE-04 (BLOCK): free disk must clear the model weight size and
// free memory must clear the runtime envelope, or the install OOMs / runs out of
// space. Free disk comes from syscall.Statfs (the injected seam) and free memory
// from the HostProfile's MemAvailableBytes (the live kernel MemAvailable).
//
// Degradation (D-15): if a requirement is unknown (a zero threshold because the
// envelope was Unknown, or MemAvailable/statfs unreadable), that sub-check cannot
// be evaluated and downgrades to WARN rather than a false BLOCK. A confidently
// insufficient resource is a true FAIL.
func checkResources(p detect.HostProfile, req ResourceReq, statfs statfsFunc) CheckResult {
	const (
		id   = "PRE-04"
		name = "Free disk + free memory"
	)
	remediation := "Free up disk under the model data dir and/or close memory-heavy processes; a model needs free disk ≥ its weight size and free memory ≥ its runtime envelope."

	// --- Memory: free RAM must clear the envelope. ---
	if req.MinMemBytes == 0 || !p.MemAvailableBytes.Known {
		// Cannot evaluate the memory requirement (unknown envelope or MemAvailable).
		return warn(id, name, TierBlock,
			"could not verify free memory against the runtime envelope",
			remediation,
			joinProvenance("usable_envelope_bytes", p.MemAvailableBytes.Source),
			p.MemAvailableBytes.Raw)
	}
	if p.MemAvailableBytes.Value < req.MinMemBytes {
		return fail(id, name,
			fmt.Sprintf("free memory %s < required envelope %s", humanGiB(p.MemAvailableBytes.Value), humanGiB(req.MinMemBytes)),
			remediation, p.MemAvailableBytes.Source, "")
	}

	// --- Disk: free space at the data dir must clear the model weight size. ---
	if req.MinDiskBytes == 0 {
		return warn(id, name, TierBlock,
			"could not verify free disk (no model size requirement available)",
			remediation, "usable_envelope_bytes", "")
	}
	freeDisk, ok := statfs(req.DataDir)
	if !ok {
		return warn(id, name, TierBlock,
			fmt.Sprintf("could not statfs %q to verify free disk", req.DataDir),
			remediation, "syscall.Statfs", "")
	}
	if freeDisk < req.MinDiskBytes {
		return fail(id, name,
			fmt.Sprintf("free disk %s at %q < required %s", humanGiB(freeDisk), req.DataDir, humanGiB(req.MinDiskBytes)),
			remediation, "syscall.Statfs:"+req.DataDir, "")
	}

	return pass(id, name, TierBlock,
		fmt.Sprintf("free memory %s ≥ %s; free disk %s ≥ %s at %q",
			humanGiB(p.MemAvailableBytes.Value), humanGiB(req.MinMemBytes),
			humanGiB(freeDisk), humanGiB(req.MinDiskBytes), req.DataDir),
		joinProvenance(p.MemAvailableBytes.Source, "syscall.Statfs:"+req.DataDir))
}

// humanGiB renders bytes as a GiB string for human details.
func humanGiB(b uint64) string {
	return fmt.Sprintf("%.2f GiB", float64(b)/(1<<30))
}
