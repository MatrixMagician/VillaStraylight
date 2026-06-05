// Package preflight is the REUSABLE host-prep gate for VillaStraylight (D-18).
//
// It answers one question per host requirement — "is this machine ready to safely
// install and run the local AI stack?" — and returns the answers as typed,
// renderable values: a []CheckResult. It is a pure library: it NEVER calls
// os.Exit and NEVER prints. Exit-code mapping, table/JSON rendering, and the
// --force override summary all live in the command layer (cmd/villa/preflight.go),
// so Phase 3 `install` and a future `villa doctor` can reuse the exact same checks
// without inheriting any CLI behavior (Pitfall 5, D-18).
//
// Two tiers (D-02):
//   - TierBlock  — a wrong answer crashes/OOMs the install (missing Vulkan ICD /
//     no enumerated iGPU, free disk < model size, free memory < envelope).
//   - TierWarn   — degrades or affects boot-survival but does not immediately
//     crash (user lingering off, kernel/firmware below the tested baseline, ROCm
//     absent — Vulkan is the default so that is informational).
//
// Degradation rule (D-15): a TierBlock check that cannot be EVALUATED (a tool is
// missing, output is unparseable, a fact is Unknown) downgrades to a WARN
// ("could not verify — proceed with caution") rather than a false hard block. It
// surfaces uncertainty (exit 2) instead of either crashing or silently passing.
//
// Every CheckResult carries a Remediation hint and a Provenance string (which
// tool / path produced it), consistent with the detect package's typed-Unknown
// contract.
package preflight

import "github.com/MatrixMagician/VillaStraylight/internal/detect"

// Tier classifies how seriously a failed check should be treated (D-02).
type Tier int

const (
	// TierWarn marks a check whose failure degrades the experience but does not
	// crash the install. WARN failures map to exit code 2.
	TierWarn Tier = iota
	// TierBlock marks a check whose failure would crash/OOM the install. A BLOCK
	// failure maps to exit code 1 unless explicitly overridden with --force.
	TierBlock
)

// String renders a Tier for tables and goldens.
func (t Tier) String() string {
	switch t {
	case TierBlock:
		return "BLOCK"
	case TierWarn:
		return "WARN"
	default:
		return "UNKNOWN"
	}
}

// Status is the outcome of a single check.
type Status int

const (
	// StatusPass means the requirement is satisfied.
	StatusPass Status = iota
	// StatusWarn means the requirement is not satisfied but the failure is
	// non-blocking — either an inherently WARN-tier check, or a BLOCK-tier check
	// that could not be evaluated and was downgraded per D-15.
	StatusWarn
	// StatusFail means a BLOCK-tier requirement is positively NOT satisfied (a
	// confident known-bad). StatusFail is only meaningful on TierBlock checks.
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

// CheckResult is the typed, renderable outcome of a single preflight check. It is
// a pure value — the command layer maps a slice of these to an exit code and a
// table; the package itself never acts on them.
type CheckResult struct {
	// ID is the requirement id this check satisfies (e.g. "PRE-01").
	ID string `json:"id"`
	// Name is a short human label for the check.
	Name string `json:"name"`
	// Tier is the classification (BLOCK or WARN) of this check.
	Tier Tier `json:"tier"`
	// Status is the outcome (PASS / WARN / FAIL).
	Status Status `json:"status"`
	// Detail is a one-line human explanation of the outcome.
	Detail string `json:"detail"`
	// Remediation is an actionable hint for fixing a non-PASS result. Every
	// CheckResult populates this, including PASS (where it is empty or a no-op).
	Remediation string `json:"remediation"`
	// Provenance records which tool / path / fact produced this result, for -v.
	Provenance string `json:"provenance"`
	// Raw captures untrusted raw output when a parse failed, surfaced under -v
	// (mirrors detect's D-16 contract). Never serialized to the --json contract.
	Raw string `json:"-"`
}

// Standalone-preflight resource FLOORS (WR-02/WR-03).
//
// Model-agnostic `villa preflight` cannot know which model the user will install,
// so it must NOT gate on the full GTT envelope (~62.5 GiB) — a model needs only
// `weights + KV + headroom`, not the entire memory ceiling, so gating on the
// envelope hard-BLOCKs perfectly capable hosts (the opposite of the OOM failure,
// but still a wrong verdict). Instead it gates on the SMALLEST-installable-model
// floor: if a host cannot clear even these, nothing in the catalog will install.
// Phase 3 install knows the concrete model and gates on real numbers via
// RunWithResources — these floors only set the standalone default.
const (
	// minRunnableMemFloorBytes is a conservative minimal free-memory floor: a host
	// below this cannot run even the smallest catalog model (the ~1.2 GiB bootstrap
	// model plus its KV/headroom). It is intentionally small — the precise
	// per-model memory requirement is checked at install time, not here.
	minRunnableMemFloorBytes uint64 = 4 << 30 // 4 GiB
	// minModelDiskFloorBytes is a conservative smallest-installable-model disk
	// floor, sized to the catalog's ~1.2 GB bootstrap model weights plus margin.
	// The disk requirement is model WEIGHT size — never the runtime memory
	// envelope (weights and RAM are unrelated quantities, WR-03).
	minModelDiskFloorBytes uint64 = 2 << 30 // 2 GiB
)

// Run executes every preflight check against the supplied HostProfile and returns
// one CheckResult per requirement (PRE-01..04). It is pure: no os.Exit, no
// printing, no host mutation.
//
// Because standalone `villa preflight` is model-agnostic, its resource thresholds
// are the smallest-installable-model FLOORS (free memory ≥ a minimal runnable
// floor; free disk ≥ the smallest model's weight size) — NOT the full GTT envelope,
// which would over-block capable hosts (WR-02/WR-03). Phase 3 install supplies the
// real per-model `weights + KV + headroom` numbers via RunWithResources.
//
// Ordering is stable (PRE-01, PRE-02, PRE-03, PRE-04, then the WARN-tier
// kernel/firmware floor checks) so goldens and tables are deterministic.
func Run(p detect.HostProfile) []CheckResult {
	return RunWithResources(p, ResourceReq{
		// Disk: the smallest installable model's weight size (plus margin), NOT the
		// envelope — weights and RAM are unrelated quantities (WR-03).
		MinDiskBytes: minModelDiskFloorBytes,
		// Memory: a minimal runnable floor, NOT the full envelope (WR-02). A host
		// below this cannot run even the smallest catalog model.
		MinMemBytes: minRunnableMemFloorBytes,
		// Default to the data dir the install will populate.
		DataDir: defaultDataDir(),
	})
}

// RunWithResources is Run with explicit resource thresholds and data dir, so
// Phase 3 install (which knows the concrete model size and target dir) can gate on
// real numbers instead of the envelope-derived defaults.
func RunWithResources(p detect.HostProfile, req ResourceReq) []CheckResult {
	return []CheckResult{
		checkVulkanIGPU(p),
		checkPodmanRootless(livePodmanDeps()),
		checkLinger(liveLingerDeps()),
		checkSELinuxContainerDevices(liveSELinuxDeps()),
		checkResources(p, req, liveStatfs),
		checkKernelFloor(p),
		checkFirmwareFloor(p),
	}
}

// pass builds a passing CheckResult.
func pass(id, name string, tier Tier, detail, provenance string) CheckResult {
	return CheckResult{ID: id, Name: name, Tier: tier, Status: StatusPass, Detail: detail, Provenance: provenance}
}

// warn builds a WARN CheckResult with a remediation hint.
func warn(id, name string, tier Tier, detail, remediation, provenance, raw string) CheckResult {
	return CheckResult{ID: id, Name: name, Tier: tier, Status: StatusWarn, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}

// fail builds a StatusFail CheckResult (only meaningful on TierBlock). The caller
// is responsible for passing TierBlock.
func fail(id, name string, detail, remediation, provenance, raw string) CheckResult {
	return CheckResult{ID: id, Name: name, Tier: TierBlock, Status: StatusFail, Detail: detail, Remediation: remediation, Provenance: provenance, Raw: raw}
}
