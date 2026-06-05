package preflight

import (
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

const gib = uint64(1) << 30

func memProfile(availBytes uint64, known bool) detect.HostProfile {
	if !known {
		return detect.HostProfile{MemAvailableBytes: detect.UnknownBytes("MemAvailable unreadable", "")}
	}
	return detect.HostProfile{MemAvailableBytes: detect.KnownBytes(availBytes, "/proc/meminfo:MemAvailable")}
}

func statfsReturning(free uint64, ok bool) statfsFunc {
	return func(string) (uint64, bool) { return free, ok }
}

func TestCheckResources(t *testing.T) {
	t.Run("both sufficient passes (BLOCK)", func(t *testing.T) {
		p := memProfile(64*gib, true)
		req := ResourceReq{MinDiskBytes: 30 * gib, MinMemBytes: 32 * gib, DataDir: "/tmp"}
		got := checkResources(p, req, statfsReturning(100*gib, true))
		if got.Tier != TierBlock || got.Status != StatusPass {
			t.Fatalf("want BLOCK/PASS, got tier=%v status=%v (%s)", got.Tier, got.Status, got.Detail)
		}
	})

	t.Run("free memory below envelope fails (BLOCK)", func(t *testing.T) {
		p := memProfile(16*gib, true)
		req := ResourceReq{MinDiskBytes: 30 * gib, MinMemBytes: 32 * gib, DataDir: "/tmp"}
		got := checkResources(p, req, statfsReturning(100*gib, true))
		if got.Tier != TierBlock || got.Status != StatusFail {
			t.Fatalf("want BLOCK/FAIL on low memory, got tier=%v status=%v", got.Tier, got.Status)
		}
	})

	t.Run("free disk below model size fails (BLOCK)", func(t *testing.T) {
		p := memProfile(64*gib, true)
		req := ResourceReq{MinDiskBytes: 30 * gib, MinMemBytes: 32 * gib, DataDir: "/tmp"}
		got := checkResources(p, req, statfsReturning(5*gib, true))
		if got.Status != StatusFail {
			t.Fatalf("want FAIL on low disk, got %v", got.Status)
		}
	})

	t.Run("unknown MemAvailable downgrades to WARN (D-15)", func(t *testing.T) {
		p := memProfile(0, false)
		req := ResourceReq{MinDiskBytes: 30 * gib, MinMemBytes: 32 * gib, DataDir: "/tmp"}
		got := checkResources(p, req, statfsReturning(100*gib, true))
		if got.Tier != TierBlock || got.Status != StatusWarn {
			t.Fatalf("unknown mem should downgrade to BLOCK/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
	})

	t.Run("zero envelope threshold downgrades to WARN (D-15)", func(t *testing.T) {
		p := memProfile(64*gib, true)
		req := ResourceReq{MinDiskBytes: 0, MinMemBytes: 0, DataDir: "/tmp"}
		got := checkResources(p, req, statfsReturning(100*gib, true))
		if got.Status != StatusWarn {
			t.Fatalf("zero thresholds should WARN, got %v", got.Status)
		}
	})

	t.Run("statfs failure downgrades to WARN (D-15)", func(t *testing.T) {
		p := memProfile(64*gib, true)
		req := ResourceReq{MinDiskBytes: 30 * gib, MinMemBytes: 32 * gib, DataDir: "/nonexistent"}
		got := checkResources(p, req, statfsReturning(0, false))
		if got.Status != StatusWarn {
			t.Fatalf("statfs failure should WARN, got %v", got.Status)
		}
	})
}

func TestLiveStatfsRealPath(t *testing.T) {
	// liveStatfs must read a real filesystem without panicking and report > 0 free.
	free, ok := liveStatfs("/tmp")
	if !ok {
		t.Fatal("liveStatfs(/tmp) should succeed on this host")
	}
	if free == 0 {
		t.Error("expected non-zero free bytes on /tmp")
	}
}
