package detect

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const liveGTTBytes = 67149381632 // captured fixture: 62.5 GiB GTT ceiling

// TestGTTTotalFromFixture parses the captured live mem_info_gtt_total fixture
// via the flat-fixture fallback (testdata/ holds the raw file directly).
func TestGTTTotalFromFixture(t *testing.T) {
	gtt := gttTotalBytes("testdata")
	if !gtt.Known {
		t.Fatalf("gttTotalBytes: Known=false, source=%q", gtt.Source)
	}
	if gtt.Value != liveGTTBytes {
		t.Errorf("gttTotalBytes: Value=%d, want %d", gtt.Value, liveGTTBytes)
	}
}

// TestGTTTotalGarbageYieldsUnknown asserts that unparseable bytes degrade to a
// typed Unknown with the raw captured — never 0-as-known, never a panic.
func TestGTTTotalGarbageYieldsUnknown(t *testing.T) {
	// Build an isolated dir containing only the garbage file named as the
	// canonical sysfs file, so the reader hits the parse-error path.
	dir := t.TempDir()
	garbage, err := os.ReadFile("testdata/mem_info_gtt_total.garbage")
	if err != nil {
		t.Fatalf("read garbage fixture: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "mem_info_gtt_total"), garbage, 0o644); err != nil {
		t.Fatalf("write garbage: %v", err)
	}
	gtt := gttTotalBytes(dir)
	if gtt.Known {
		t.Fatalf("gttTotalBytes(garbage): Known=true, want false")
	}
	if gtt.Value != 0 {
		t.Errorf("gttTotalBytes(garbage): Value=%d, want 0", gtt.Value)
	}
	if gtt.Raw == "" {
		t.Errorf("gttTotalBytes(garbage): Raw empty, want captured garbage")
	}
}

// TestUsableEnvelopeIsGTTNeverMemTotal is the OOM guard (Pitfall 1): the usable
// envelope must equal the GTT ceiling, NOT total RAM — even when MemTotal is ~2x.
func TestUsableEnvelopeIsGTTNeverMemTotal(t *testing.T) {
	const syntheticMemTotal = liveGTTBytes * 2 // ~125 GiB, as on the live box

	gtt := gttTotalBytes("testdata")
	ttm := ttmLimitBytes("testdata/ttm-pages-limit")
	env := usableEnvelope(gtt, ttm)

	if !env.Known {
		t.Fatalf("usableEnvelope: Known=false, source=%q", env.Source)
	}
	if env.Value != liveGTTBytes {
		t.Errorf("usableEnvelope: Value=%d, want GTT %d", env.Value, liveGTTBytes)
	}
	if env.Value == syntheticMemTotal {
		t.Errorf("usableEnvelope: equals MemTotal %d — OOM-guard violated", syntheticMemTotal)
	}
	if env.Value >= syntheticMemTotal {
		t.Errorf("usableEnvelope %d >= MemTotal %d — would over-recommend and OOM", env.Value, syntheticMemTotal)
	}
}

// TestUsableEnvelopeFallsBackToTTM asserts the ttm cross-check is used when GTT
// is unreadable, and that neither-readable yields Unknown (never MemTotal).
func TestUsableEnvelopeFallsBackToTTM(t *testing.T) {
	ttm := ttmLimitBytes("testdata/ttm-pages-limit")
	if !ttm.Known {
		t.Fatalf("ttmLimitBytes: Known=false")
	}
	env := usableEnvelope(UnknownBytes("gtt missing", ""), ttm)
	if !env.Known || env.Value != ttm.Value {
		t.Errorf("usableEnvelope(unknown gtt, known ttm): got %+v, want ttm value %d", env, ttm.Value)
	}

	none := usableEnvelope(UnknownBytes("gtt missing", ""), UnknownBytes("ttm missing", ""))
	if none.Known {
		t.Errorf("usableEnvelope(unknown,unknown): Known=true, want Unknown")
	}
}

func TestTTMLimitFromFixture(t *testing.T) {
	ttm := ttmLimitBytes("testdata/ttm-pages-limit")
	if !ttm.Known {
		t.Fatalf("ttmLimitBytes: Known=false")
	}
	// 16393892 pages * 4096 = 67149381632 (matches GTT on this box).
	if ttm.Value != liveGTTBytes {
		t.Errorf("ttmLimitBytes: Value=%d, want %d", ttm.Value, liveGTTBytes)
	}
}

func TestTTMLimitUnreadableYieldsUnknown(t *testing.T) {
	ttm := ttmLimitBytes(filepath.Join(t.TempDir(), "does-not-exist"))
	if ttm.Known {
		t.Errorf("ttmLimitBytes(missing): Known=true, want false")
	}
}

func TestMemAvailableUnreadableYieldsUnknown(t *testing.T) {
	m := memAvailableBytes(filepath.Join(t.TempDir(), "no-meminfo"))
	if m.Known {
		t.Errorf("memAvailableBytes(missing): Known=true, want false")
	}
}

// TestMemAvailableScanErrorYieldsUnknown asserts WR-05: a read/scan error (here
// induced by opening a directory, which os.Open allows but bufio.Scan errors on)
// degrades to a typed Unknown labeled as a read error — never a silent
// "MemAvailable not found" mislabeling of an I/O failure.
func TestMemAvailableScanErrorYieldsUnknown(t *testing.T) {
	m := memAvailableBytes(t.TempDir()) // a directory path: open ok, scan errors.
	if m.Known {
		t.Errorf("memAvailableBytes(scan error): Known=true, want Unknown")
	}
	if !strings.Contains(m.Source, "read error") {
		t.Errorf("memAvailableBytes(scan error): Source=%q, want a read-error reason", m.Source)
	}
}

// TestGTTUsedFromFixture parses the captured mem_info_gtt_used fixture via the
// flat-fixture fallback (testdata/ holds the raw file directly), confirming the
// new offload-delta reader inherits the readAMDCardBytes seam.
func TestGTTUsedFromFixture(t *testing.T) {
	gtt := gttUsedBytes("testdata")
	if !gtt.Known {
		t.Fatalf("gttUsedBytes: Known=false, source=%q", gtt.Source)
	}
	if gtt.Value != 1516001852 {
		t.Errorf("gttUsedBytes: Value=%d, want 1516001852", gtt.Value)
	}
}

// TestVRAMUsedFromFixture parses the captured mem_info_vram_used fixture.
func TestVRAMUsedFromFixture(t *testing.T) {
	vram := vramUsedBytes("testdata")
	if !vram.Known {
		t.Fatalf("vramUsedBytes: Known=false, source=%q", vram.Source)
	}
	if vram.Value != 979130940 {
		t.Errorf("vramUsedBytes: Value=%d, want 979130940", vram.Value)
	}
}

// TestGTTUsedMissingYieldsUnknown asserts a missing/unreadable sysfs file degrades
// to a typed Unknown (→ WARN at the offload layer), never a known zero.
func TestGTTUsedMissingYieldsUnknown(t *testing.T) {
	gtt := gttUsedBytes(t.TempDir()) // empty dir: no mem_info_gtt_used present
	if gtt.Known {
		t.Errorf("gttUsedBytes(missing): Known=true, want Unknown")
	}
}

// TestGTTUsedGarbageYieldsUnknown asserts unparseable bytes degrade to Unknown with
// the raw captured.
func TestGTTUsedGarbageYieldsUnknown(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "mem_info_gtt_used"), []byte("not-a-number\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	gtt := gttUsedBytes(dir)
	if gtt.Known {
		t.Errorf("gttUsedBytes(garbage): Known=true, want Unknown")
	}
	if gtt.Raw == "" {
		t.Errorf("gttUsedBytes(garbage): Raw empty, want captured garbage")
	}
}

func TestKernelVersionFromSeam(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "osrelease")
	if err := os.WriteFile(p, []byte("7.0.10-201.fc44.x86_64\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	k := kernelVersion(p)
	if !k.Known || k.Value != "7.0.10-201.fc44.x86_64" {
		t.Errorf("kernelVersion: got %+v", k)
	}
	if bad := kernelVersion(filepath.Join(dir, "missing")); bad.Known {
		t.Errorf("kernelVersion(missing): Known=true, want false")
	}
}
