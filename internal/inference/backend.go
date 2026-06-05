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
// opt-in ROCm 7.2.4 backend. Any other value is an error (fail-closed) — the caller
// must surface it, not paper over it with a default.
func BackendFor(name string) (Backend, error) {
	switch name {
	case "", "vulkan":
		return backendVulkan{}, nil
	case "rocm":
		return backendROCm{}, nil
	default:
		return nil, fmt.Errorf("unknown inference backend %q: set backend = \"vulkan\" (default) or \"rocm\" in config.toml", name)
	}
}
