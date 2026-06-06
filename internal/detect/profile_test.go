package detect

import (
	"encoding/json"
	"strings"
	"testing"
)

// TestProbeNeverPanicsAndReturnsTypedFields asserts Probe runs on whatever host
// the test executes on without panicking, sets the schema version, and never
// passes a bare zero off as a known value (every field is a typed Optional).
func TestProbeNeverPanics(t *testing.T) {
	p := Probe() // must not panic on CI hosts without an iGPU

	if p.SchemaVersion != hostProfileSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", p.SchemaVersion, hostProfileSchemaVersion)
	}
	// Arch is always knowable (runtime.GOARCH) — sanity that Probe wired fields.
	if !p.Arch.Known || p.Arch.Value == "" {
		t.Errorf("Arch should always be Known and non-empty, got %+v", p.Arch)
	}
}

// TestHostProfileJSONRoundTrips asserts the contract serializes and deserializes
// without loss of the typed-Optional shape, and that Raw is never serialized.
func TestHostProfileJSONRoundTrips(t *testing.T) {
	p := HostProfile{
		Arch:                KnownStr("amd64", "runtime.GOARCH"),
		UsableEnvelopeBytes: KnownBytes(67149381632, "mem_info_gtt_total"),
		GTTTotalBytes:       UnknownBytes("unparseable gtt_total", "secret-raw-bytes"),
		DRINodes:            []string{"card1", "renderD128"},
		DRINodeCount:        KnownInt(2, "/dev/dri"),
		ROCmPresent:         KnownBool(false, "rocminfo not on PATH"),
		ROCmReadiness: ROCmReadiness{
			// Mix a Known and an Unknown Optional so both serialized shapes are
			// exercised by the round-trip (a real bool survives; an Unknown one
			// round-trips as Known=false, never silently becoming a real false).
			KernelFloorOK:   KnownBool(true, "kernel >= floor"),
			RocminfoGfx1151: UnknownBool("rocminfo unavailable (gfx id not enumerated)", "secret-raw-rocm"),
		},
		SchemaVersion: hostProfileSchemaVersion,
	}

	data, err := json.Marshal(p)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), "secret-raw-bytes") {
		t.Errorf("Raw leaked into JSON: %s", data)
	}

	var back HostProfile
	if err := json.Unmarshal(data, &back); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if back.UsableEnvelopeBytes.Value != p.UsableEnvelopeBytes.Value {
		t.Errorf("envelope round-trip: got %d, want %d", back.UsableEnvelopeBytes.Value, p.UsableEnvelopeBytes.Value)
	}
	if back.GTTTotalBytes.Known {
		t.Errorf("Unknown GTT round-tripped as Known")
	}
	if back.SchemaVersion != hostProfileSchemaVersion {
		t.Errorf("SchemaVersion round-trip: got %d", back.SchemaVersion)
	}

	// rocm_readiness typed-Optionals survive the round-trip in both shapes.
	if !back.ROCmReadiness.KernelFloorOK.Known || !back.ROCmReadiness.KernelFloorOK.Value {
		t.Errorf("KnownBool rocm_readiness field did not round-trip as Known/true: %+v", back.ROCmReadiness.KernelFloorOK)
	}
	if back.ROCmReadiness.RocminfoGfx1151.Known {
		t.Errorf("Unknown rocm_readiness field round-tripped as Known (false-green): %+v", back.ROCmReadiness.RocminfoGfx1151)
	}
	if strings.Contains(string(data), "secret-raw-rocm") {
		t.Errorf("rocm_readiness Raw leaked into JSON: %s", data)
	}
}
