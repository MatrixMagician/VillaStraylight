package preflight

import (
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/memory"
)

// volumeRootReturning is the injected volume-root resolver fake, mirroring
// statfsReturning so the disk gate is assertable without a real podman.
func volumeRootReturning(root string, ok bool) volumeRootFn {
	return func() (string, bool) { return root, ok }
}

// memGateInput builds a MemoryGateInput with both seams injected, so no test in
// this file ever touches the live podman/statfs host paths.
func memGateInput(model string, minDisk uint64, root volumeRootFn, statfs statfsFunc) MemoryGateInput {
	return MemoryGateInput{
		EmbeddingModel: model,
		MinDiskBytes:   minDisk,
		VolumeRoot:     root,
		Statfs:         statfs,
	}
}

// pinnedEmbedModel is the D-08 pinned embedding model id whose footprint is a
// Known 512 MiB — the headroom tests pivot around it.
const pinnedEmbedModel = "nomic-embed-text-v1.5"

// TestRunMemoryOrderAndIDs guards the stable check ordering contract: RunMemory
// returns exactly [MEM-PRE-disk, MEM-PRE-headroom], both TierBlock, so tables
// and goldens downstream are deterministic (D-06).
func TestRunMemoryOrderAndIDs(t *testing.T) {
	p := memProfile(64*gib, true)
	in := memGateInput(pinnedEmbedModel, gib, volumeRootReturning("/tmp", true), statfsReturning(100*gib, true))
	got := RunMemory(p, in)
	if len(got) != 2 {
		t.Fatalf("want exactly 2 memory checks, got %d", len(got))
	}
	if got[0].ID != "MEM-PRE-disk" || got[1].ID != "MEM-PRE-headroom" {
		t.Fatalf("want stable order [MEM-PRE-disk, MEM-PRE-headroom], got [%s, %s]", got[0].ID, got[1].ID)
	}
	for _, c := range got {
		if c.Tier != TierBlock {
			t.Errorf("%s: want TierBlock, got %v", c.ID, c.Tier)
		}
	}
}

// TestRunMemoryDefaultDiskFloor guards the live-default binding: a zero
// MinDiskBytes resolves to the named 1 GiB minVectorDiskFloorBytes const —
// never an accidental always-pass zero floor.
func TestRunMemoryDefaultDiskFloor(t *testing.T) {
	p := memProfile(64*gib, true)

	t.Run("free just below the default floor FAILs", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, 0, volumeRootReturning("/tmp", true), statfsReturning(minVectorDiskFloorBytes-1, true))
		disk := RunMemory(p, in)[0]
		if disk.Status != StatusFail {
			t.Fatalf("free below default floor should FAIL, got %v (%s)", disk.Status, disk.Detail)
		}
	})

	t.Run("free at the default floor PASSes", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, 0, volumeRootReturning("/tmp", true), statfsReturning(minVectorDiskFloorBytes, true))
		disk := RunMemory(p, in)[0]
		if disk.Status != StatusPass {
			t.Fatalf("free at default floor should PASS, got %v (%s)", disk.Status, disk.Detail)
		}
	})
}

// TestCheckVectorDisk pins every MEM-PRE-disk branch: PASS, confident-shortage
// FAIL, resolver-unevaluable WARN, statfs-unevaluable WARN (D-06/D-07) — and
// that every non-PASS row carries refuse-with-remediation text.
func TestCheckVectorDisk(t *testing.T) {
	p := memProfile(64*gib, true)

	t.Run("free above floor passes (BLOCK)", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, gib, volumeRootReturning("/volroot", true), statfsReturning(10*gib, true))
		got := RunMemory(p, in)[0]
		if got.Tier != TierBlock || got.Status != StatusPass {
			t.Fatalf("want BLOCK/PASS, got tier=%v status=%v (%s)", got.Tier, got.Status, got.Detail)
		}
		if !strings.Contains(got.Provenance, "/volroot") {
			t.Errorf("PASS provenance should name the resolved path, got %q", got.Provenance)
		}
	})

	t.Run("free below floor fails (BLOCK, confident shortage)", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, gib, volumeRootReturning("/volroot", true), statfsReturning(gib/2, true))
		got := RunMemory(p, in)[0]
		if got.Tier != TierBlock || got.Status != StatusFail {
			t.Fatalf("want BLOCK/FAIL on low disk, got tier=%v status=%v", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("FAIL must carry refuse-with-remediation text")
		}
		if !strings.Contains(got.Remediation, "/volroot") {
			t.Errorf("remediation should name the resolved path, got %q", got.Remediation)
		}
		if !strings.Contains(got.Detail+got.Remediation, "vector index") {
			t.Errorf("FAIL should explain the vector index grows, got detail=%q remediation=%q", got.Detail, got.Remediation)
		}
	})

	t.Run("resolver failure downgrades to WARN (typed-Unknown, D-07)", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, gib, volumeRootReturning("", false), statfsReturning(10*gib, true))
		got := RunMemory(p, in)[0]
		if got.Tier != TierBlock || got.Status != StatusWarn {
			t.Fatalf("resolver failure should downgrade to BLOCK/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("WARN must carry remediation text")
		}
		if !strings.Contains(got.Provenance, "podman") {
			t.Errorf("WARN provenance should name the podman command, got %q", got.Provenance)
		}
	})

	t.Run("statfs failure downgrades to WARN (typed-Unknown, D-07)", func(t *testing.T) {
		in := memGateInput(pinnedEmbedModel, gib, volumeRootReturning("/volroot", true), statfsReturning(0, false))
		got := RunMemory(p, in)[0]
		if got.Tier != TierBlock || got.Status != StatusWarn {
			t.Fatalf("statfs failure should downgrade to BLOCK/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("WARN must carry remediation text")
		}
		if !strings.Contains(got.Detail, "/volroot") {
			t.Errorf("WARN detail should name the path that failed statfs, got %q", got.Detail)
		}
	})
}

// TestCheckEmbedHeadroom pins every MEM-PRE-headroom branch: PASS, confident
// shortage FAIL, Unknown-MemAvailable WARN, and the D-02 conservative-default
// floor for an unrecognized embedding model — never a zero floor, never a
// false-green.
func TestCheckEmbedHeadroom(t *testing.T) {
	diskOK := func() (volumeRootFn, statfsFunc) {
		return volumeRootReturning("/tmp", true), statfsReturning(100*gib, true)
	}

	t.Run("free above the pinned footprint passes (BLOCK)", func(t *testing.T) {
		root, statfs := diskOK()
		in := memGateInput(pinnedEmbedModel, gib, root, statfs)
		got := RunMemory(memProfile(gib, true), in)[1]
		if got.Tier != TierBlock || got.Status != StatusPass {
			t.Fatalf("want BLOCK/PASS, got tier=%v status=%v (%s)", got.Tier, got.Status, got.Detail)
		}
	})

	t.Run("free below the pinned footprint fails (BLOCK, confident shortage)", func(t *testing.T) {
		root, statfs := diskOK()
		in := memGateInput(pinnedEmbedModel, gib, root, statfs)
		got := RunMemory(memProfile(256<<20, true), in)[1]
		if got.Tier != TierBlock || got.Status != StatusFail {
			t.Fatalf("want BLOCK/FAIL on low memory, got tier=%v status=%v", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("FAIL must carry refuse-with-remediation text")
		}
		if !strings.Contains(got.Detail, "embedding reservation") {
			t.Errorf("FAIL detail should name the embedding reservation, got %q", got.Detail)
		}
	})

	t.Run("Unknown MemAvailable downgrades to WARN with provenance (D-07)", func(t *testing.T) {
		root, statfs := diskOK()
		in := memGateInput(pinnedEmbedModel, gib, root, statfs)
		got := RunMemory(memProfile(0, false), in)[1]
		if got.Tier != TierBlock || got.Status != StatusWarn {
			t.Fatalf("Unknown MemAvailable should downgrade to BLOCK/WARN, got tier=%v status=%v", got.Tier, got.Status)
		}
		if got.Remediation == "" {
			t.Error("WARN must carry remediation text")
		}
	})

	t.Run("unrecognized model evaluates against the conservative default floor (D-02)", func(t *testing.T) {
		root, statfs := diskOK()
		in := memGateInput("no-such-embedder", gib, root, statfs)
		floor := memory.ConservativeFootprintBytes()

		below := RunMemory(memProfile(floor-1, true), in)[1]
		if below.Status != StatusFail {
			t.Fatalf("free below the conservative floor should FAIL (never a zero floor), got %v (%s)", below.Status, below.Detail)
		}
		above := RunMemory(memProfile(floor, true), in)[1]
		if above.Status != StatusPass {
			t.Fatalf("free at the conservative floor should PASS, got %v (%s)", above.Status, above.Detail)
		}
	})
}
