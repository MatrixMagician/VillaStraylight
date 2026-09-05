// Package inference is the backend-neutral seam for running llama-server and
// proving GPU offload for VillaStraylight.
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
// This is the anti-silent-CPU-fallback contract.
package inference

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// Runner abstracts how an llama-server instance is started, stopped, readiness-
// probed, and how its stderr is captured for the offload log-scrape. The exact
// method set is intentionally small: callers depend on this interface, not
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
// It is the seam protects — every backend literal lives behind it, never in
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
	// ResidencyProof returns the backend-owned descriptor of the log/journal markers
	// the offload-assert keys on. It keeps every backend marker literal
	// (Vulkan0 vs ROCm0, the device-init prefix, the abort fault string) BEHIND the
	// seam: the start-time scrape (offload.go) and the running scrape
	// (running_offload.go) are both parameterized by it, so a ROCm backend slots in
	// without re-rolling the offload math or leaking its tokens to callers.
	ResidencyProof() ResidencyMarkers
}

// ResidencyMarkers is the backend-owned, log-shape-only descriptor that drives BOTH
// offload-assert scrape paths. It carries ONLY the journald/stderr marker
// literals a backend emits — never runtime signals (the gpu_busy_percent reading is
// a per-run input on RunningOffloadInput, not a per-backend literal). Each backend
// file owns its own values; the Vulkan implementation reproduces today's hardcoded
// literals byte-for-byte so the refactor is a no-op for the existing Vulkan tests.
type ResidencyMarkers struct {
	// DeviceToken is the load_tensors buffer-line device token proving residency on
	// the GPU (e.g. "Vulkan0"/"ROCm0"). The running scrape matches it with
	// strings.Contains; per Pitfall 2 "ROCm0" does NOT match "ROCm_Host", so the
	// substring match stays exact enough for a direct port.
	DeviceToken string
	// DeviceLabel is the start-time device_info enumeration prefix for the selected
	// device (e.g. "- Vulkan"/"- ROCm"). The start-time scrape keys on it to read the
	// enumerated device name (and reject a software renderer).
	DeviceLabel string
	// StartLogDevicePrefix is the older single-line device-init prefix the start-time
	// scrape also accepts (e.g. "ggml_vulkan:"). Empty disables that branch (a backend
	// whose builds never emit the old single-line format).
	StartLogDevicePrefix string
	// FaultString is the journal abort marker that VOIDS residency before any
	// buffer-line PASS (e.g. "Memory access fault by GPU node"). Empty = no-op (the
	// fault scan is skipped), which keeps Vulkan byte-identical.
	FaultString string
	// RejectSoftwareRenderer gates the llvmpipe/softpipe reject in the start-time
	// scrape. True for Vulkan (a software-renderer ICD is a real risk); a backend with
	// no software-renderer analog (ROCm) sets it false.
	RejectSoftwareRenderer bool
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
	// ContextLen is the -c context length llama-server is started with. For coding
	// mode the render layer sets this to the resolved agent ctx (CoderAgentCtx) so the
	// SINGLE -c carries the agent context — never a second -c (Pitfall 1).
	ContextLen int
	// CodingMode is the OPTIONAL coding-mode render descriptor.
	// nil ⇒ off path BY CONSTRUCTION: ContainerArgs emits exactly the v1.3 base args, so
	// the existing villa-llama*.container goldens are byte-identical. Non-nil ⇒
	// ContainerArgs appends the tool-calling delta (--jinja, the sampling preset, and
	// --cache-reuse 256 gated on CacheReuseSafe) behind the backend seam. Populated only
	// by orchestrate.Render when cfg.CodingMode is true.
	CodingMode *CodingModeSpec
	// Speculation is the OPTIONAL speculative-decoding render delta (ADR-0006).
	// nil renders byte-identical args, which is what keeps every unit rendered
	// before this field existed unchanged on upgrade.
	Speculation *SpeculationSpec
}

// SpeculationSpec is the OPTIONAL speculation render delta carried on RunSpec.
// Mode is the config vocabulary's value, already validated at the config
// boundary, so the seam renders what it recognises and nothing for anything else.
type SpeculationSpec struct {
	// Mode is the speculation mode; "ngram" is the only one that renders a flag.
	Mode string
}

// CodingModeSpec is the OPTIONAL tool-calling render delta carried on RunSpec
// It is populated only when coding mode is active; when a
// RunSpec's CodingMode is nil the rendered unit is byte-identical to v1.3. The
// agent context is NOT a field here — it is delivered via RunSpec.ContextLen =
// AgentCtx so the single -c carries it (Pitfall 1: never a second -c).
type CodingModeSpec struct {
	// Sampling is the catalog-qualified agent sampling preset. nil ⇒ the
	// sampling flags are omitted (the --jinja toggle still renders). The orchestrate
	// layer translates catalog.AgentSampling → inference.Sampling so internal/inference
	// stays free of an internal/catalog import (clean dependency direction).
	Sampling *Sampling
	// CacheReuseSafe gates --cache-reuse 256 (carried from Phase 24). The Go zero
	// value false is the FAIL-CLOSED default: --cache-reuse is appended ONLY when the
	// catalog entry positively declares cache_reuse_safe=true (build-9496-scoped; the
	// 24-TOOLBOX-DECISION.md Check 3 re-probe gate fires on any toolbox re-pin).
	CacheReuseSafe bool
}

// Sampling is the inference-layer mirror of catalog.AgentSampling. It is defined
// HERE (not imported from internal/catalog) so the backend seam stays
// catalog-independent; the orchestrate layer does the catalog→inference translation.
// The values are formatted into the llama-server sampling flags inside ContainerArgs
// (%g for the floats, %d for the int) — typed, never shell-interpolated.
type Sampling struct {
	Temperature   float64
	TopP          float64
	TopK          int
	RepeatPenalty float64
}

// Status is the outcome of an offload assertion. It mirrors preflight.Status so
// the command/dashboard layers render a single PASS/WARN/FAIL vocabulary.
type Status int

const (
	// StatusPass means offload was positively proven.
	StatusPass Status = iota
	// StatusWarn means offload could not be EVALUATED (unreadable stderr / sysfs),
	// distinct from a confidently-false offload — surfaces uncertainty.
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

// warn builds a WARN Verdict with a remediation hint.
func warn(detail, remediation, provenance, raw string) Verdict {
	return Verdict{Status: StatusWarn, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}

// fail builds a FAIL Verdict (offload positively not happening).
func fail(detail, remediation, provenance, raw string) Verdict {
	return Verdict{Status: StatusFail, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}
