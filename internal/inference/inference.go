// Package inference is the backend-neutral seam for running llama-server and
// proving GPU offload for VillaStraylight (INF-01/INF-03, D-03/D-09).
//
// It answers one question — "did llama-server actually offload onto the gfx1151
// iGPU (not silently fall back to CPU)?" — behind two abstractions:
//
//   - Runner   — HOW the server is started/stopped/health-probed and how its
//     stderr is captured. The container implementation (runner_podman.go) shells
//     to the rootless `podman` CLI with fixed-arg exec.Command.
//   - Backend  — WHICH GPU backend/image/flag-set/device-args apply. The Vulkan
//     RADV implementation (backend_vulkan.go) is the ONLY file in the package that
//     holds the image literal, /dev/dri device args, and mandatory Strix Halo
//     flags. ROCm/Metal backends slot in here later without touching callers.
//
// It is a pure library in the same sense as internal/preflight: it NEVER calls
// os.Exit and NEVER prints. Exit-code mapping and table/JSON rendering live in the
// command layer, so Phase 3 Quadlet generation and the Phase 5 dashboard can reuse
// the exact same Verdict without inheriting any CLI behavior.
//
// Every offload signal is a typed-Unknown value (detect.Bool / detect.Bytes): a
// signal that could not be evaluated (unreadable stderr, missing sysfs file)
// degrades to WARN, which is DISTINCT from a confidently-false offload (FAIL).
// This is the anti-silent-CPU-fallback contract (D-09).
package inference

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// Runner abstracts how an llama-server instance is started, stopped, readiness-
// probed, and how its stderr is captured for the offload log-scrape. The exact
// method set is intentionally small (D-03): callers depend on this interface, not
// on podman/Vulkan specifics, so a future ROCm or Metal backend reuses it.
type Runner interface {
	// Start launches the server for the given run spec and returns once the
	// container/process has been created (not necessarily ready — use Health).
	Start(spec RunSpec) error
	// Stop tears down the server on the host, even if Start partially failed. It
	// is idempotent so callers can always defer it.
	Stop() error
	// Health reports whether the server is accepting requests at its endpoint
	// (readiness), degrading to a typed-Unknown on an unevaluable probe.
	Health() detect.Bool
	// Endpoint is the loopback base URL the OpenAI-compatible API is published on
	// (e.g. "http://127.0.0.1:8080").
	Endpoint() string
	// Logs returns the captured stderr of the server, bounded to a safe size, for
	// the offload log-scrape. The bool reports whether any output was captured.
	Logs() (stderr string, captured bool)
}

// Backend abstracts which GPU backend applies: the container image, the mandatory
// runtime flag set, and the device/group/security args needed to expose the iGPU.
// It is the seam SC#4 protects — every backend literal lives behind it, never in
// a caller. The Vulkan RADV implementation is backendVulkan in backend_vulkan.go.
type Backend interface {
	// Name is a short backend identifier (e.g. "vulkan") for provenance/--json.
	Name() string
	// Image is the digest-pinned container image the runner pulls/runs.
	Image() string
	// ContainerArgs renders the `podman run` argument slice (device/group/security
	// args, host-publish, model bind, image, and the llama-server flags) for the
	// given run spec. The model name is the catalog-resolved file, never shell-
	// interpolated. This is the ONLY place /dev/dri, the image, and the loopback
	// publish literal are assembled.
	ContainerArgs(spec RunSpec) []string
}

// RunSpec is the backend-neutral description of one inference run: which model
// file to load (resolved through the Plan-01 catalog, never user-interpolated),
// the context length, and the host models directory to bind read-only.
type RunSpec struct {
	// ContainerName is the transient (--rm) container name for start/stop/logs.
	ContainerName string
	// ModelFile is the GGUF filename inside the bound models dir (catalog-resolved).
	ModelFile string
	// ModelsDir is the host directory bind-mounted read-only at the container's
	// models path. It is a host path, never interpolated into a shell string.
	ModelsDir string
	// ContextLen is the -c context length llama-server is started with.
	ContextLen int
}

// Status is the outcome of an offload assertion. It mirrors preflight.Status so
// the command/dashboard layers render a single PASS/WARN/FAIL vocabulary.
type Status int

const (
	// StatusPass means offload was positively proven.
	StatusPass Status = iota
	// StatusWarn means offload could not be EVALUATED (unreadable stderr / sysfs),
	// distinct from a confidently-false offload — surfaces uncertainty (D-09).
	StatusWarn
	// StatusFail means offload is positively NOT happening (software renderer,
	// offloaded 0/N, or a ~zero GTT delta) — the silent-CPU-fallback this exists
	// to catch.
	StatusFail
)

// String renders a Status for tables and goldens.
func (s Status) String() string {
	switch s {
	case StatusPass:
		return "PASS"
	case StatusWarn:
		return "WARN"
	case StatusFail:
		return "FAIL"
	default:
		return "UNKNOWN"
	}
}

// Verdict is the typed, renderable offload-asserting result and the --json /
// Phase-5 dashboard contract. It mirrors preflight.CheckResult: a pure value the
// command layer maps to an exit code and a table; the package never acts on it.
type Verdict struct {
	// Status is the overall dual-assert outcome (PASS/WARN/FAIL).
	Status Status `json:"status"`
	// Detail is a one-line human explanation of the outcome.
	Detail string `json:"detail"`
	// Remediation is an actionable hint for a non-PASS result.
	Remediation string `json:"remediation"`
	// Provenance records which signals produced this verdict, for -v.
	Provenance string `json:"provenance"`

	// LogOffload is the log-scrape offload signal: Known=true,Value=true → a real
	// RADV device + offloaded N/N (N>0); Known=true,Value=false → confirmed CPU
	// fallback (FAIL); Known=false → unreadable stderr (WARN).
	LogOffload detect.Bool `json:"log_offload"`
	// SysfsOffload is the sysfs GTT-used-delta offload signal, same typed-Unknown
	// contract as LogOffload.
	SysfsOffload detect.Bool `json:"sysfs_offload"`
	// GTTDeltaBytes is the observed before/after GTT-used delta, recorded for
	// Phase-5 threshold calibration (A1). Zero when the sysfs read was Unknown.
	GTTDeltaBytes uint64 `json:"gtt_delta_bytes"`

	// Raw captures untrusted raw output (the offending stderr) when a parse failed,
	// surfaced under -v. Never serialized to the --json contract (mirrors detect).
	Raw string `json:"-"`
}

// pass builds a passing Verdict.
func pass(detail, provenance string) Verdict {
	return Verdict{Status: StatusPass, Detail: detail, Provenance: provenance}
}

// warn builds a WARN Verdict with a remediation hint.
func warn(detail, remediation, provenance, raw string) Verdict {
	return Verdict{Status: StatusWarn, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}

// fail builds a FAIL Verdict (offload positively not happening).
func fail(detail, remediation, provenance, raw string) Verdict {
	return Verdict{Status: StatusFail, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}
