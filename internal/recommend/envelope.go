package recommend

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
)

// headroomFraction reserves a slice of the usable envelope for the OS, compute
// buffers, and KV growth beyond the modelled context (Open Question 1 / Assumption
// A3). ~12% is a starting estimate, NOT an authoritative constant — it is tunable
// and must be validated against a Phase-2 near-max-context dry-load and adjusted.
// It deliberately lives as a named constant so that tuning is a one-line change.
const headroomFraction = 0.12

// degradedFloorFraction is the conservative fraction of total RAM used as a
// fallback envelope when the real GTT envelope is Unknown (Pitfall 1).
// Strix Halo's default GTT/ttm map is ~50% of RAM, so 50% is a safe floor that
// never guesses high. When even total RAM is unknown, no floor is derivable and
// recommend refuses.
const degradedFloorFraction = 0.50

// headroomBytes returns the reserved headroom for a given usable envelope.
func headroomBytes(envelope uint64) uint64 {
	return uint64(float64(envelope) * headroomFraction)
}

// conservativeFloor derives a safe fallback envelope when the real GTT envelope
// could not be detected. It returns a floor of ~50% of total RAM when RAM
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

// Bounds of a simulated envelope. Below the floor no catalog entry can fit, so the
// value is a typo (512 for 512 GiB) rather than a host; above the ceiling no
// Strix-class or Apple-Silicon machine exists.
const (
	minSimulatedEnvelope = 1 << 30
	maxSimulatedEnvelope = 4 << 40
	envelopeForms        = "a number with a unit, such as 64GiB, 64G or 1.5TiB (units M, G, T with optional iB or B, all binary; 1 GiB to 4 TiB)"
)

// ParseEnvelope reads a --envelope value into bytes. The unit is mandatory so a
// bare "64" is never guessed as bytes or GiB, every unit is binary to match the
// rest of villa's memory math, and anything else fails closed.
func ParseEnvelope(s string) (uint64, error) {
	refuse := func(why string) (uint64, error) {
		return 0, fmt.Errorf("invalid envelope %q: %s; accepted forms: %s", s, why, envelopeForms)
	}
	t := strings.TrimSpace(s)
	i := 0
	for i < len(t) && (t[i] >= '0' && t[i] <= '9' || t[i] == '.') {
		i++
	}
	num, unit := t[:i], strings.ToUpper(strings.TrimSpace(t[i:]))
	if num == "" {
		return refuse("no leading number")
	}
	v, err := strconv.ParseFloat(num, 64)
	if err != nil {
		return refuse("the number is malformed")
	}
	var scale float64
	switch unit {
	case "M", "MB", "MIB":
		scale = 1 << 20
	case "G", "GB", "GIB":
		scale = 1 << 30
	case "T", "TB", "TIB":
		scale = 1 << 40
	case "":
		return refuse("the unit is missing")
	default:
		return refuse("unknown unit " + strconv.Quote(unit))
	}
	b := v * scale
	if b < minSimulatedEnvelope || b > maxSimulatedEnvelope {
		return refuse("outside the plausible range")
	}
	return uint64(b), nil
}
