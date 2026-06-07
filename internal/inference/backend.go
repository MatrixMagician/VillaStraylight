package inference

import "fmt"

// backend.go holds BackendFor — the SINGLE polymorphism point that maps a config
// `backend` string to a Backend implementation (D-01). It is the only place a caller
// chooses a backend by name; every other site depends on the Backend interface, never
// on a concrete backendVulkan/backendROCm. Plan 03 re-routes the 8 existing
// VulkanBackend() call sites through here so `backend = rocm` in config.toml flips the
// whole inference path with no other change.
//
// It FAILS CLOSED (D-02, T-6-03): an unknown/typo'd backend value returns an
// actionable error, NEVER a silent fallback to Vulkan (or any privileged backend). A
// hand-edited config string is untrusted input; silently coercing it would hide a
// misconfiguration and could select a privileged device path the user did not intend.

// BackendFor resolves a config `backend` string to its Backend implementation. The
// empty string and "vulkan" select the default Vulkan RADV backend; "rocm" selects the
// opt-in ROCm 7.2.4 backend (unchanged digest — D-02 coexistence); "rocm-6.4.4" and
// "rocm-6.4.4-rocwmma" select the two additive digest-pinned ROCm 6.4.4 backends (D-01).
// Any other value is an error (fail-closed) — the caller must surface it, not paper over
// it with a default. Each ROCm variant is the same image-parameterized backendROCm delta
// (D-06); only the pinned digest (and the reported Name) differs.
func BackendFor(name string) (Backend, error) {
	switch name {
	case "", "vulkan":
		return backendVulkan{}, nil
	case "rocm":
		return backendROCm{name: "rocm", image: rocmImage724}, nil
	case "rocm-6.4.4":
		return backendROCm{name: "rocm-6.4.4", image: rocmImage644}, nil
	case "rocm-6.4.4-rocwmma":
		return backendROCm{name: "rocm-6.4.4-rocwmma", image: rocmImage644wmma}, nil
	default:
		return nil, fmt.Errorf("unknown inference backend %q: set backend = "+
			"\"vulkan\" (default), \"rocm\" (7.2.4), \"rocm-6.4.4\", or "+
			"\"rocm-6.4.4-rocwmma\" in config.toml", name)
	}
}

// IsROCmFamily reports whether a config backend string selects a ROCm-family backend
// ("rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma"). It is the SINGLE place the ROCm-name set
// is enumerated (D-08): callers use it instead of comparing `== "rocm"` so a new ROCm
// digest is gated identically by the ROCm preflight (refuse-with-remediation) and routed
// to the ROCm lifecycle path. It holds only backend NAME strings (untrusted config
// values), never an image literal, so it stays seam-clean (no TestSeamGrepGate concern).
func IsROCmFamily(name string) bool {
	switch name {
	case "rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma":
		return true
	default:
		return false
	}
}
