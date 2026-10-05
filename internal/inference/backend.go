package inference

import "fmt"

// backend.go holds BackendFor — the SINGLE polymorphism point that maps a config
// `backend` string to a Backend implementation. It is the only place a caller
// chooses a backend by name; every other site depends on the Backend interface, never
// on a concrete backendVulkan/backendROCm, so flipping the `backend` value in
// config.toml flips the whole inference path with no other change.
//
// It FAILS CLOSED (T-6-03): an unknown/typo'd backend value returns an
// actionable error, NEVER a silent fallback to Vulkan (or any privileged backend). A
// hand-edited config string is untrusted input; silently coercing it would hide a
// misconfiguration and could select a privileged device path the user did not intend.

// BackendFor resolves a config `backend` string to its Backend implementation. The
// empty string and "rocm" select the DEFAULT ROCm backend, the ROCm 10.0 channel
// (ADR-0022): "rocm" is the default's name, not a version, so it reports Name() "rocm"
// on the 10.0 image; "rocm-10.0" names that image explicitly; "rocm-7.2.4" keeps the
// former default's image reachable by name; "rocm-6.4.4" and "rocm-6.4.4-rocwmma" select
// the two additive digest-pinned ROCm 6.4.4 backends; "vulkan" selects the Vulkan RADV backend,
// now the explicit opt-in fallback. Any other value is an error (fail-closed) — the
// caller must surface it, not paper over it with a default. Each ROCm variant is the
// same image-parameterized backendROCm delta; only the pinned digest (and the
// reported Name) differs.
func BackendFor(name string) (Backend, error) {
	switch name {
	case "vulkan":
		return backendVulkan{}, nil
	case "", "rocm":
		return backendROCm{name: "rocm", image: rocmImage100, load: loadResident}, nil
	case "rocm-7.2.4":
		return backendROCm{name: "rocm-7.2.4", image: rocmImage724, load: loadResidentLegacy}, nil
	case "rocm-10.0":
		return backendROCm{name: "rocm-10.0", image: rocmImage100, load: loadResident}, nil
	case "rocm-6.4.4":
		return backendROCm{name: "rocm-6.4.4", image: rocmImage644, load: loadResident}, nil
	case "rocm-6.4.4-rocwmma":
		return backendROCm{name: "rocm-6.4.4-rocwmma", image: rocmImage644wmma, load: loadResidentLegacy}, nil
	default:
		return nil, fmt.Errorf("unknown inference backend %q: set backend = "+
			"\"rocm\" (10.0, default), \"rocm-10.0\", \"rocm-7.2.4\", "+
			"\"rocm-6.4.4\", \"rocm-6.4.4-rocwmma\", or \"vulkan\" in config.toml", name)
	}
}

// IsROCmFamily reports whether a config backend string selects a ROCm-family backend
// ("" — the default, "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4", "rocm-6.4.4-rocwmma").
// It is the SINGLE place
// the ROCm-name set is enumerated: callers use it instead of comparing
// `== "rocm"` so a new ROCm digest is gated identically by the ROCm preflight
// (refuse-with-remediation) and routed to the ROCm lifecycle path. The empty string is
// included because it resolves to the default ROCm backend in BackendFor — the two MUST
// agree or an unset config would run ROCm while skipping the ROCm gate. It holds only
// backend NAME strings (untrusted config values), never an image literal, so it stays
// seam-clean (no TestSeamGrepGate concern).
func IsROCmFamily(name string) bool {
	switch name {
	case "", "rocm", "rocm-10.0", "rocm-7.2.4", "rocm-6.4.4", "rocm-6.4.4-rocwmma":
		return true
	default:
		return false
	}
}
