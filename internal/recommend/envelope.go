package recommend

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// headroomFraction reserves a slice of the usable envelope for the OS, compute
// buffers, and KV growth beyond the modelled context (Open Question 1 / Assumption
// A3). ~12% is a starting estimate, NOT an authoritative constant — it is tunable
// and must be validated against a Phase-2 near-max-context dry-load and adjusted.
// It deliberately lives as a named constant so that tuning is a one-line change.
const headroomFraction = 0.12

// degradedFloorFraction is the conservative fraction of total RAM used as a
// fallback envelope when the real GTT envelope is Unknown (D-14, Pitfall 1).
// Strix Halo's default GTT/ttm map is ~50% of RAM, so 50% is a safe floor that
// never guesses high. When even total RAM is unknown, no floor is derivable and
// recommend refuses.
const degradedFloorFraction = 0.50

// headroomBytes returns the reserved headroom for a given usable envelope.
func headroomBytes(envelope uint64) uint64 {
	return uint64(float64(envelope) * headroomFraction)
}

// conservativeFloor derives a safe fallback envelope when the real GTT envelope
// could not be detected (D-14). It returns a floor of ~50% of total RAM when RAM
// is known, and (0, false) when no safe floor is derivable — in which case the
// caller must refuse rather than guess high (no OOM by optimism).
func conservativeFloor(p detect.HostProfile) (uint64, bool) {
	if !p.TotalRAMBytes.Known || p.TotalRAMBytes.Value == 0 {
		return 0, false
	}
	floor := uint64(float64(p.TotalRAMBytes.Value) * degradedFloorFraction)
	if floor == 0 {
		return 0, false
	}
	return floor, true
}

// resolveEnvelope returns the usable envelope to size against, whether it is a
// degraded estimate, and whether any envelope is derivable at all.
//
//   - Real GTT envelope known     → (value, degraded=false, ok=true)
//   - GTT unknown, RAM known      → (conservative floor, degraded=true, ok=true)
//   - neither known               → (0, false, ok=false) — caller refuses
func resolveEnvelope(p detect.HostProfile) (envelope uint64, degraded bool, ok bool) {
	if p.UsableEnvelopeBytes.Known && p.UsableEnvelopeBytes.Value > 0 {
		return p.UsableEnvelopeBytes.Value, false, true
	}
	if floor, derivable := conservativeFloor(p); derivable {
		return floor, true, true
	}
	return 0, false, false
}
