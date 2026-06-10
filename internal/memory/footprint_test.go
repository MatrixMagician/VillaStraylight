// footprint_test.go guards the D-02 conservative-default accessor: the exported
// ConservativeFootprintBytes() must be single-source coherent with the pinned
// nomic-embed-text-v1.5 footprint in embedFootprints — downstream readers
// (recommend's typed-Unknown fallback) never re-type the literal, and the
// conservative default is NEVER a silent zero reservation.
package memory

import "testing"

// TestConservativeFootprintBytesMatchesPinnedNomic asserts single-source
// coherence (D-02): the conservative default equals the pinned
// nomic-embed-text-v1.5 reservation and is never zero.
func TestConservativeFootprintBytesMatchesPinnedNomic(t *testing.T) {
	fp := Footprint("nomic-embed-text-v1.5")
	if !fp.Known {
		t.Fatalf("precondition: pinned nomic-embed-text-v1.5 footprint must be Known, got %+v", fp)
	}
	got := ConservativeFootprintBytes()
	if got == 0 {
		t.Fatalf("ConservativeFootprintBytes() = 0 — the conservative default must never be a silent zero reservation (D-02)")
	}
	if got != fp.Value {
		t.Errorf("ConservativeFootprintBytes() = %d, want the pinned nomic-embed-text-v1.5 footprint %d (single-source coherence, D-02)", got, fp.Value)
	}
}
