// Package doctor is the pure `villa doctor` health-diagnosis core (DOCTOR-01/02/03):
// the read-only, compositional twin of the install-time preflight gate. Where
// preflight answers "is this host ready to install?", doctor answers "is this
// *running* install still healthy?" — composing the already-shipped cores
// (preflight host-prep checks, the status read-model + its per-service offload
// Verdict, and an orchestrate.Reconcile config-vs-disk drift Plan) into ONE
// worst-wins Report.
//
// Contract (mirrors internal/status + internal/preflight):
//   - PURE: it NEVER calls os.Exit and NEVER prints. Exit-code mapping and rendering
//     live in the command layer (cmd/villa/doctor.go), so the worst-wins fold is
//     unit-testable off-hardware.
//   - Every host touch is an injected Deps func-field — there is no host I/O here.
//   - doctor owns its OWN Report type and its OWN golden. It only READS the
//     byte-frozen status.Report; it never extends or mutates it.
//   - COMPOSITION ONLY: it never re-implements a probe a shipped core produces.
//   - Backend marker literals stay behind the inference seam: doctor consumes
//     inference.Verdict values OPAQUELY (Status/Detail/Remediation only) and routes
//     ROCm-family backends via inference.IsROCmFamily — never typing Vulkan0/ROCm0/
//     image tags (TestSeamGrepGate walks internal/).
//
// Severity / exit mapping (Pitfall 1 — the shipped preflight constants are
// AUTHORITATIVE, NOT the inverted ROADMAP prose): a confident BLOCK-class FAIL
// (preflight BLOCK FAIL, a confident residency/offload FAIL) → the blocked tier
// (exit 1); a WARN (preflight WARN, config-vs-disk drift, a typed-Unknown /
// unevaluable signal, a down stack) → the warn tier (exit 2); all healthy → 0.
package doctor

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// Tier/Status string vocabulary — doctor normalizes every composed signal
// (preflight CheckResult, status health, an inference.Verdict, a drift Plan) into
// this single PASS/WARN/FAIL + BLOCK/WARN grammar so the worst-wins fold and the
// (Plan-02) golden are contract-independent of the upstream struct shapes.
const (
	tierBlock = "BLOCK"
	tierWarn  = "WARN"

	statusPass = "PASS"
	statusWarn = "WARN"
	statusFail = "FAIL"
)

// reportSchemaVersion is doctor's OWN --json contract self-version, distinct
// from status.reportSchemaVersion. Bumped append-only on any additive change to the
// doctor Report contract.
//
// Version history (append-only — never renumber a shipped version):
//   - v1: the original host-prep + running-stack + drift + memory fold.
//   - v2: the coding-agent fold (Phase 28-01) — additive agent findings
//     (agent-tool-call / agent-residency / agent-binary-drift / agent-config-drift).
//     No existing field changed shape; agent-off output is byte-identical except this bump.
//   - v3: the web-search fold (Phase 34-04) — additive web-search findings
//     (search-egress / search-residency). searxng/websafe service readiness is composed
//     from the status read-model rows (no new finding type). No existing field changed
//     shape; web-search-OFF output is byte-identical except this bump (a gated-off fold emits no
//     findings). This is doctor's OWN version — INDEPENDENT of status.reportSchemaVersion (5).
//   - v4: PRE-08 compute device access (issue #120) — no new doctor-owned finding
//     type; the check is folded in for free through the existing host-condition
//     preflight.Run/RunROCm(*) composition in step 1, appending one more finding
//     via the unchanged findingFromCheck path. Bumped because the fold is additive
//     data every doctor Report now carries, mirroring how v2/v3 bumped for their
//     own additive host-condition/service findings.
//   - v5: the CAT-01 catalog-geometry fold (issue #133) — one additive finding per
//     catalog entry present on this host whose GGUF header is cross-checked against
//     the entry's declared fit dimensions. Like v4 it introduces no doctor-owned
//     finding type: the CheckResults arrive through the unchanged findingFromCheck
//     path. Bumped on the same rule v4 used — the fold is additive data every doctor
//     Report now carries.
//   - v6: the websafe-binary finding (issue #141) — a NEW doctor-owned finding type,
//     unlike v4/v5. The running villa binary's path is host state, not config state, so
//     a binary at a path other than the one villa-websafe mounts gets its own WARN
//     instead of folding into config-vs-disk drift. Web-search-OFF output is
//     byte-identical except this bump (the gate emits no finding).
//   - v7: the sandbox fold (issue #176) — SBX-01, the read-only twin of PRE-09
//     (folded for free through the unchanged findingFromCheck path, like v4/v5), plus
//     SBX-02, a NEW doctor-owned finding type (the villa-sandbox.network unit exists
//     and the inference unit is joined to it — a host-state fact, not a preflight
//     CheckResult). Sandbox-OFF output is byte-identical except this bump (both nil
//     seams emit no finding).
//   - v8: the TMD-01 tools-mode drift finding (spec v1.11 §3.5/§10) — a NEW
//     doctor-owned finding type like v6. The served inference unit must carry the
//     tool-calling flag iff subsystem.ToolsOn, and a mismatch is a confident FAIL:
//     tool calls against a unit rendered without it fail at the server, with nothing
//     in config to explain why. Unlike the subsystem folds this finding is NOT gated
//     on an opt-in — the assertion is meaningful in both directions — so every doctor
//     Report gains one line.
//   - v9: the villa-rerank.service health finding (ADR-0028), folded for free from
//     the status rows like v4/v5: one more health finding when the reranker is
//     rendered, no new finding type. Reranker-off output is byte-identical except
//     this bump.
const reportSchemaVersion = 9

// The three typed-Unknown ROCm host-prep check IDs that a PROVEN ROCm residency
// supersedes (down-ranks, never deletes). They INTENTIONALLY duplicate the preflight
// check-ID strings (internal/preflight/checks_rocm.go idROCmFirmware/idROCmHSA/idROCmImage),
// which are unexported there: doctor matches on the STABLE ID string, not by importing
// the consts. These are finding IDs, NOT backend marker literals (no ROCm0/HSA_OVERRIDE/
// image tag), so they are seam-safe (TestSeamGrepGate).
const (
	idROCmFirmware = "ROCM-PRE-firmware"
	idROCmHSA      = "ROCM-PRE-hsa"
	idROCmImage    = "ROCM-PRE-image"
)

// supersededROCmHostPrepID reports whether id is one of the three typed-Unknown ROCm
// host-prep findings a proven ROCm residency may down-rank.
func supersededROCmHostPrepID(id string) bool {
	switch id {
	case idROCmFirmware, idROCmHSA, idROCmImage:
		return true
	default:
		return false
	}
}

// Finding is doctor's normalized, renderable health finding — a doctor-OWNED wrapper
// (mirroring preflight.CheckResult's field set, spirit) so doctor's golden never
// couples to an upstream struct. Every non-PASS Finding MUST carry a non-empty
// Remediation.
type Finding struct {
	// ID is a short stable identifier for the finding (e.g. "drift", "offload").
	ID string `json:"id"`
	// Name is a short human label.
	Name string `json:"name"`
	// Tier is the severity class: BLOCK (a real fault) or WARN (degraded / unevaluable).
	Tier string `json:"tier"`
	// Status is the outcome: PASS | WARN | FAIL.
	Status string `json:"status"`
	// Detail is a one-line human explanation.
	Detail string `json:"detail"`
	// Remediation is an actionable hint for a non-PASS result. Empty on PASS.
	Remediation string `json:"remediation"`
	// Provenance records which composed core / signal produced this finding, for -v.
	Provenance string `json:"provenance"`
	// Raw captures untrusted raw output, surfaced under -v only — NEVER serialized to
	// the --json contract (mirrors preflight.CheckResult.Raw / inference.Verdict.Raw).
	Raw string `json:"-"`
}

// Report is doctor's OWN aggregated --json contract. It is NOT status.Report.
type Report struct {
	// Findings is every normalized finding from the composed cores + the drift check.
	Findings []Finding `json:"findings"`
	// Overall is the worst-wins verdict string: PASS | WARN | FAIL.
	Overall string `json:"overall"`
	// SchemaVersion is the contract self-version. It MUST stay the LAST tagged field
	// (append-only; new tagged fields go above it). =1 from day one.
	SchemaVersion int `json:"schema_version"`
}

// Deps are the injectable host seams Aggregate composes, and every one of them is a
// read or a driver: none decides. Which subsystems are on, whether a unit has drifted,
// whether the sandbox network is joined and what tools mode expects are all answered
// in this package from the config the run loaded (ADR-0017), so they need no wiring
// and no nil-seam gate. The live wiring is liveDoctorDeps in cmd/villa; doctor_test.go
// replaces the seams with stubs. The core never does I/O of its own.
type Deps struct {
	// Probe returns the host profile that feeds the preflight host-condition checks.
	Probe func() detect.HostProfile
	// StatusReport returns the running-stack read-model (== status.Run(liveStatusDeps)).
	// It already carries per-service offload Verdicts, so doctor reuses it rather than
	// re-running a second journald/GTT scrape (RESEARCH A1).
	StatusReport func() status.Report
	// IsActive reads one unit's systemd active-state (the memory headroom check needs
	// to know whether the embedder is already resident). An error, or a state other
	// than "active", keeps the strict pre-install semantics.
	IsActive func(unit string) (string, error)
	// RunMemoryChecks is the opt-in memory host gate (preflight.RunMemory, bound by the
	// cmd tier — composition over re-implementation; it is a seam because the gate's
	// own probes read the host). Doctor builds the input from the config and the
	// embedder's active-state, and folds the CheckResults via findingFromCheck. It is
	// called only when memory is on.
	RunMemoryChecks func(detect.HostProfile, preflight.MemoryGateInput) []preflight.CheckResult
	// ReadVerifyState reads the last recorded `villa verify search` result. nil means
	// the store could not be read; a zero State means none is recorded.
	ReadVerifyState func() *verifystate.State
	// CatalogGeometry is the CAT-01 gate (preflight.RunCatalogGeometry, bound by the
	// cmd tier with the models dir as its open seam): does each catalog entry present
	// on this host still describe the GGUF header of its file? Its CheckResults fold
	// through findingFromCheck and rank worst-wins exactly like every other check.
	// It is NOT subsystem-gated — the catalog is always in play. NIL-SAFE: when nil
	// (a caller that could not load the catalog) no CAT-01 finding is emitted at all,
	// never a PASS-by-default.
	CatalogGeometry func() []preflight.CheckResult
	// RunSandboxChecks is SBX-01, the read-only twin of the PRE-09 sandbox-runtime
	// gate (preflight.RunSandbox, bound by the cmd tier with the live podman/rpm/
	// kvm/probe seams — composition over re-implementation). Its CheckResults fold
	// through findingFromCheck exactly like every other host-condition check. It is
	// called only when the workspace agent is on.
	RunSandboxChecks func(detect.HostProfile) []preflight.CheckResult

	// The four proofs that drive a real workload. Each returns an inference.Verdict
	// consumed OPAQUELY here (Status/Detail/Remediation only — seam-clean), and each is
	// called only when its subsystem is on, so a caller wires them unconditionally.
	//
	// ResidencyUnderLoad is the chat-model residency-under-embedding-load proof: the
	// cmd tier drives a REAL /v1/embeddings workload and samples the chat model's
	// GTT/journal residency MID-DRIVE (memory).
	ResidencyUnderLoad func() inference.Verdict
	// AgentToolCall is the coding-agent tool-call round-trip proof: a REAL read→edit
	// `crush run` round-trip mapped to a Verdict (agent).
	AgentToolCall func() inference.Verdict
	// AgentResidencyUnderLoad is the coder-model residency-under-tool-call-load proof
	// (agent).
	AgentResidencyUnderLoad func() inference.Verdict
	// SearchResidencyUnderLoad is the chat-model residency proof with the search stack
	// up: a bounded drive of plain chat completions sent while villa-searxng and
	// villa-websafe run beside the model, never querying either (web search). A confident CPU fallback under search load is a BLOCK-class FAIL that
	// DOMINATES a healthy-looking HTTP-200; a not-in-flight / unevaluable signal →
	// typed-Unknown WARN (never an idle-sampled false-green).
	//
	// SCOPE OMISSION (accepted limit): guard health has NO host-side source — the
	// per-request guard metadata lives in-container only, with no host aggregate. Building a
	// guard counter/health pipeline = NEW behavior = OUT OF SCOPE for this surfacing phase, so
	// NO guard-health finding is emitted (a documented omission, never a fabricated PASS/0).
	// searxng/websafe service READINESS is folded for free: status.Run already emits dedicated
	// villa-searxng/villa-websafe Services rows (Plan 03) which flow through the existing
	// healthFinding loop — no new finding type is added here (composition, RESEARCH A1).
	SearchResidencyUnderLoad func() inference.Verdict

	// The Quadlet unit directory, read-only. Doctor never creates it.
	//
	// UnitDirExists reports whether the directory is there. An error means it could not
	// be resolved or examined, which is not the same claim as "absent".
	UnitDirExists func() (bool, error)
	// ReadUnit returns the bytes of one on-disk unit. An absent unit is an error that
	// satisfies errors.Is(err, fs.ErrNotExist).
	ReadUnit func(name string) ([]byte, error)
	// RenderUnits renders the units cfg calls for, binding the villa binary at
	// hostVilla into the ones that mount it. It writes nothing.
	RenderUnits func(cfg config.VillaConfig, hostVilla string) ([]orchestrate.Unit, error)
	// RunningVilla is the path of the villa binary running this doctor.
	RunningVilla func() string

	// The coding-agent drift inputs (agent), read from the host.
	//
	// AgentBinarySHA hashes the villa-owned Crush binary; present is false, with no
	// error, when it is not installed.
	AgentBinarySHA func() (sha string, present bool, err error)
	// ReadCrushConfig reads the on-disk crush.json; present is false, with no error,
	// on the first run.
	ReadCrushConfig func() (b []byte, present bool, err error)
	// RenderCrushConfig renders the reference crush.json cfg would produce.
	RenderCrushConfig func(cfg config.VillaConfig) ([]byte, error)
}

// changedUnitNames joins the drifted unit names in Plan order for the drift Detail
// (issue #141). A drift verdict that names no unit leaves the operator nothing to diff.
func changedUnitNames(changed []orchestrate.Unit) string {
	names := make([]string, 0, len(changed))
	for _, u := range changed {
		names = append(names, u.Name)
	}
	return strings.Join(names, ", ")
}

// websafeBinaryFinding compares the villa binary villa-websafe mounts against the one
// running doctor. They differ whenever villa was moved, copied, or rebuilt somewhere else,
// which the container will keep serving from the OLD path until install rewrites the unit.
// WARN, never BLOCK: the served stack is intact, it is just not this binary (issue #141).
func websafeBinaryFinding(mounted, running string) Finding {
	f := Finding{
		ID:         "websafe-binary",
		Name:       "websafe binary",
		Tier:       tierWarn,
		Status:     statusPass,
		Detail:     "villa-websafe mounts the running villa binary (" + mounted + ")",
		Provenance: "villa-websafe.container Volume= vs os.Executable",
	}
	if mounted != running {
		f.Status = statusWarn
		f.Detail = "the running villa (" + running + ") is not the binary villa-websafe mounts (" + mounted + ")"
		f.Remediation = "re-run `villa install` from the binary you intend to serve, or run villa from " + mounted
	}
	return f
}

// sandboxNetworkProvenance names where SBX-02's facts come from.
const sandboxNetworkProvenance = "villa-sandbox.network + villa-inferproxy unit (on-disk)"

// sandboxNetworkFinding is SBX-02 (issue #176): the internal task network a
// running workspace-agent task actually needs must be on disk, and the inference
// proxy unit must be joined to it — both are BLOCK-tier: a task with no network
// cannot reach the served model, and a task container is started per-task, so this
// is caught here rather than as a per-task failure the operator never sees in
// `villa doctor`. The proxy, not villa-llama, is the unit on the sandbox network
// since ADR-0011.
func sandboxNetworkFinding(networkPresent, proxyJoined bool) Finding {
	f := Finding{
		ID:         "SBX-02",
		Name:       "Sandbox network",
		Tier:       tierBlock,
		Provenance: sandboxNetworkProvenance,
	}
	switch {
	case !networkPresent:
		f.Status = statusFail
		f.Detail = "the villa-sandbox.network unit is not on disk"
		f.Remediation = "re-run `villa install` to render the sandbox network unit, then `villa up`"
	case !proxyJoined:
		f.Status = statusFail
		f.Detail = "the villa-inferproxy unit is not joined to villa-sandbox"
		f.Remediation = "re-run `villa install` to regenerate the inference proxy unit with the sandbox network attached, then `villa up`"
	default:
		f.Status = statusPass
		f.Detail = "villa-sandbox.network exists and the villa-inferproxy unit is joined to it"
	}
	return f
}

// statusOrder maps the doctor status vocabulary to a worst-wins rank (PASS<WARN<FAIL).
func statusRank(s string) int {
	switch s {
	case statusFail:
		return 2
	case statusWarn:
		return 1
	default:
		return 0
	}
}

// Aggregate composes the shipped cores into a single worst-wins doctor Report. It
// decides from cfg, the config the run loaded (ADR-0017): which subsystems' findings
// exist, the unit drift plan, the SBX-02 network scan, tools-mode drift and agent
// drift are answered here, from raw reads. It is pure: every host touch is a Deps
// seam and it never exits or prints.
//
// Residency-supersession (step 4a): when ROCm residency is PROVEN (ROCm-family backend
// + a service with OffloadApplies + a confident offload StatusPass), every WARN-status
// ROCm host-prep finding on ROCM-PRE-firmware/-hsa/-image is kept VISIBLE but no longer
// raises the worst-wins rank — restoring the DOCTOR-01 "exit 0 = healthy" contract on the
// opt-in ROCm path (13-UAT.md Test 1). This is predominantly the structural typed-Unknown
// "could-not-evaluate off-host" advisories, but ALSO the Known sub-floor-firmware WARN:
// the doctor layer consumes CheckResult opaquely and cannot distinguish the two, and a
// proven residency empirically moots a sub-floor concern anyway. The downgrade matches the
// (superseded-ID AND Status==statusWarn) CONJUNCTION ONLY: a confident StatusFail on the
// SAME IDs (Known-bad firmware/HSA, denied running image) is NEVER suppressed and still
// folds to FAIL — preserving no-false-green (DOCTOR-02).
func Aggregate(cfg config.VillaConfig, d Deps) Report {
	var findings []Finding

	// 1. HOST CONDITIONS — re-run the read-only preflight host-prep gate against the
	// running host, routed by the configured backend (ROCm-family → RunROCm).
	profile := d.Probe()
	var checks []preflight.CheckResult
	if inference.IsROCmFamily(cfg.Backend) {
		// Evaluate the ACTUAL running ROCm image (a denied running image → confident
		// FAIL, never swallowed by the supersession; see fold step 4a). It is resolved
		// only through the inference seam. A backend that IsROCmFamily but does not
		// resolve fails closed as its own BLOCK finding — never the un-evaluated "no
		// image requested" WARN, which the residency-supersession could then swallow.
		checks = preflight.RunROCm(profile)
		if b, err := inference.BackendFor(cfg.Backend); err != nil {
			findings = append(findings, Finding{
				ID:          "backend",
				Name:        "Inference backend",
				Tier:        tierBlock,
				Status:      statusFail,
				Detail:      "could not resolve the ROCm backend image: " + err.Error(),
				Remediation: "fix the backend field in config.toml (`villa backend set`), then re-run `villa doctor`",
				Provenance:  "inference.BackendFor",
				Raw:         err.Error(),
			})
		} else {
			checks = preflight.RunROCmForImage(profile, b.Image())
		}
	} else {
		checks = preflight.Run(profile)
	}
	for _, c := range checks {
		findings = append(findings, findingFromCheck(c))
	}

	// 1a. CATALOG GEOMETRY: fold the CAT-01 cross-check between each present
	// catalog entry and the GGUF header of its file. It sits with the host
	// conditions because it is a disk fact about this machine, not a claim about the
	// running stack. A nil seam (no loadable catalog) emits nothing.
	if d.CatalogGeometry != nil {
		for _, c := range d.CatalogGeometry() {
			findings = append(findings, findingFromCheck(c))
		}
	}

	// 1b. MEMORY HOST GATE: fold the opt-in vector-disk/headroom checks
	// (Deps.RunMemoryChecks = preflight.RunMemory) verbatim via findingFromCheck — composition over
	// re-implementation; they rank worst-wins like every other check.
	//
	// EmbedderActive: doctor runs MEM-PRE-headroom against a possibly-RUNNING stack,
	// where the embedder's own consumption is already subtracted from MemAvailable —
	// without this flag the check would demand a SECOND reservation on top of the
	// resident one and fabricate a blocking fault on a healthy memory-tight host. Any
	// error or non-active state keeps the strict pre-install semantics (false).
	if subsystem.MemoryOn(cfg) {
		state, aerr := d.IsActive(embedServiceName())
		for _, c := range d.RunMemoryChecks(profile, preflight.MemoryGateInput{
			EmbeddingModel: cfg.EmbeddingModel,
			EmbedderActive: aerr == nil && state == "active",
		}) {
			findings = append(findings, findingFromCheck(c))
		}
	}

	// 1c. SANDBOX HOST GATE (SBX-01, issue #176): fold the PRE-09 sandbox-runtime
	// gate verbatim via findingFromCheck, exactly like the memory host gate above.
	//
	// 1d. SANDBOX NETWORK (SBX-02, issue #176): the villa-sandbox.network unit must
	// be on disk and the inference proxy unit must be joined to it.
	if subsystem.SandboxOn(cfg) {
		for _, c := range d.RunSandboxChecks(profile) {
			findings = append(findings, findingFromCheck(c))
		}
		findings = append(findings, d.sandboxNetwork())
	}

	// 2. RUNNING-STACK HEALTH — fold the status read-model. A confident offload FAIL
	// becomes a BLOCK-class FAIL that DOMINATES a HealthReady (Pitfall 3); a
	// HealthDown / unevaluable signal degrades to a typed-Unknown WARN.
	//
	// rocmResidencyProven keys the residency-supersession step (4a) below: it is true
	// only when the configured backend is ROCm-family AND some service has OffloadApplies
	// AND its offload Verdict is a CONFIDENT StatusPass. Gating on OffloadApplies (not just
	// the Status) is load-bearing: StatusPass is iota 0, so a zero-value Verdict on a
	// non-offload service must NEVER spuriously prove residency.
	rocmResidencyProven := false
	report := d.StatusReport()
	// reportErr is why the read-model could not be evaluated, nil when it could. The
	// web-search egress finding (step 2d) does not depend on the report, but names it.
	reportErr := report.Err()
	if reportErr != nil {
		// 2-pre. ERRORED READ-MODEL (phase-22): status.Run returns an errored
		// ZERO-VALUE Report (LoopbackOnly=false, no Services) on any internal failure
		// config load, ModelFile resolution, BackendFor, Render. That zero value is an
		// UNEVALUABLE signal, not an observation: folding it would FABRICATE a confident
		// loopback "privacy breach" BLOCK FAIL on (e.g.) a never-installed host whose
		// cfg.Model is absent from the catalog — the exact failure mode the typed-Unknown
		// discipline forbids ("never a FAIL fabricated from a signal that could not be
		// evaluated"). Degrade to ONE typed-Unknown WARN carrying the real cause and fold
		// NEITHER LoopbackOnly NOR Services (there is nothing evaluable to fold).
		findings = append(findings, Finding{
			ID:          "stack",
			Name:        "Running-stack read-model",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      "the running-stack state could not be evaluated: " + reportErr.Error(),
			Remediation: "fix the reported condition (check config.toml and `villa status`), then re-run `villa doctor`",
			Provenance:  "status.Run error",
			Raw:         reportErr.Error(),
		})
	} else {
		if !report.LoopbackOnly {
			findings = append(findings, Finding{
				ID:          "loopback",
				Name:        "Loopback-only bind",
				Tier:        tierBlock,
				Status:      statusFail,
				Detail:      "a published port binds a non-loopback address (privacy breach)",
				Remediation: "re-run `villa install` to regenerate loopback-only units, then `villa down && villa up`",
				Provenance:  "status.Report.LoopbackOnly",
			})
		}
		findings = append(findings, updatesFinding(report.Updates))
		for _, s := range report.Services {
			findings = append(findings, healthFinding(s))
			if s.OffloadApplies {
				findings = append(findings, offloadFinding(s))
				if inference.IsROCmFamily(cfg.Backend) && s.Offload.Status == inference.StatusPass {
					rocmResidencyProven = true
				}
			}
		}
	}

	// 2b. RESIDENCY UNDER EMBEDDING LOAD: the chat model must SURVIVE a real
	// embedding workload — a silent eviction to CPU under import load is the exact
	// false-green this phase exists to catch. Only when memory is on: the finding is
	// never a PASS-by-default. The Verdict is consumed opaquely via the offloadFinding
	// precedent below.
	if subsystem.MemoryOn(cfg) {
		findings = append(findings, residencyUnderLoadFinding(d.ResidencyUnderLoad()))
	}

	// 2c. CODING-AGENT FOLD: when the agent is on, the tool-call and residency proofs
	// and the drift report. A confident agent tool-call / residency FAIL folds
	// worst-wins and DOMINATES a healthy-looking HTTP-200 (the offload-FAIL-dominates
	// switch, cloned below). Drift is surfaced as WARN-with-remediation, never
	// auto-corrected.
	if subsystem.AgentOn(cfg) {
		findings = append(findings, agentToolCallFinding(d.AgentToolCall()))
		findings = append(findings, agentResidencyFinding(d.AgentResidencyUnderLoad()))
		findings = append(findings, agentDriftFindings(d.agentDrift(cfg))...)
	}

	// 2d. WEB-SEARCH FOLD: the egress finding is the last recorded `villa verify search`
	// verdict under the freshness rule `villa status` applies (status.WebSearchSection),
	// and never a config bool. It is read here rather than taken from the status
	// report, so it survives a report that could not be evaluated (#266). The residency
	// finding is offload-asserting — a confident CPU fallback under search load folds
	// worst-wins and DOMINATES a healthy-looking HTTP-200 (the offload-FAIL-dominates
	// switch cloned below). searxng/websafe service READINESS needs NO finding here —
	// status.Run already surfaces dedicated villa-searxng/villa-websafe rows (Plan 03)
	// folded by the healthFinding loop in step 2. Guard health is a documented OMISSION
	// (no host-side source — accepted scope limit).
	if subsystem.WebSearchOn(cfg) {
		findings = append(findings, searchEgressFinding(d.ReadVerifyState(), reportErr))
		findings = append(findings, searchResidencyFinding(d.SearchResidencyUnderLoad()))
	}

	// 3. DRIFT — config-vs-disk drift is independent of running-stack health: even a
	// fully-healthy stack on stale units is a WARN (Pitfall 4). A read
	// error (absent/unreadable unit dir) degrades to a typed-Unknown WARN.
	plan, err := d.unitDrift(cfg)
	switch {
	case err != nil:
		findings = append(findings, Finding{
			ID:          "drift",
			Name:        "Config-vs-disk drift",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      "could not read the on-disk unit dir to check for drift (units not yet written / unreadable)",
			Remediation: "run `villa install` to write the Quadlet units, then re-run `villa doctor`",
			Provenance:  "orchestrate.Reconcile read error",
			Raw:         err.Error(),
		})
	case len(plan.Changed) > 0:
		findings = append(findings, Finding{
			ID:          "drift",
			Name:        "Config-vs-disk drift",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      "on-disk Quadlet units no longer match the rendered-from-config units: " + changedUnitNames(plan.Changed),
			Remediation: "re-run `villa install` to reconcile config-vs-disk drift",
			Provenance:  "orchestrate.Reconcile (non-empty Plan.Changed)",
		})
	default:
		findings = append(findings, Finding{
			ID:         "drift",
			Name:       "Config-vs-disk drift",
			Tier:       tierWarn,
			Status:     statusPass,
			Detail:     "on-disk units match the rendered-from-config units",
			Provenance: "orchestrate.Reconcile (empty Plan.Changed)",
		})
	}

	// 3a. MOVED BINARY — the running villa's path is host state, not config state, so it
	// gets its own line rather than folding into the drift verdict above (issue #141). It
	// exists only when web search is on and the villa-websafe unit is installed.
	if subsystem.WebSearchOn(cfg) {
		if mounted, ok := d.mountedVilla(); ok {
			findings = append(findings, websafeBinaryFinding(mounted, d.RunningVilla()))
		}
	}

	// 3b. TOOLS-MODE DRIFT (TMD-01) — the served unit must carry the tool-calling
	// flag iff the gate is answered on. It sits beside drift rather than inside it
	// because the fault is specific and the remediation is a different verb: a unit
	// that lost the flag makes every tool call fail at the server with nothing in
	// config to explain it. It is deliberately NOT subsystem-gated: "the unit must not
	// carry the flag when tools mode is off" is as much a fault as its absence when on.
	served, want, ok := d.toolsDrift(cfg)
	findings = append(findings, toolsDriftFinding(served, want, ok))

	// 4. WORST-WINS FOLD — any FAIL → "FAIL"; else any WARN → "WARN"; else "PASS".
	//
	// 4a. RESIDENCY SUPERSESSION (the gap-closure rule, 13-UAT.md Test 1 / DOCTOR-01):
	// when ROCm residency is PROVEN (computed above: ROCm-family backend + OffloadApplies
	// + a confident offload StatusPass), every WARN-status ROCm host-prep finding on
	// ROCM-PRE-firmware/-hsa/-image is already answered by the proven residency. These are
	// predominantly the structural "could-not-evaluate off the running host" typed-Unknown
	// advisories (checks_rocm.go hardcodes firmware/hsa as typed-Unknown and the standalone
	// gate has no requested image), but the predicate also matches a Known sub-floor-firmware
	// WARN — the doctor layer consumes the CheckResult opaquely and cannot distinguish the
	// two, and proven residency moots a sub-floor concern empirically. They are DOWN-RANKED
	// (their rank contribution suppressed) but kept VISIBLE in Findings — the rendered
	// table/JSON still SHOWS them with their unchanged WARN status; only their contribution
	// to the worst-wins rank is dropped.
	//
	// HARD NO-FALSE-GREEN INVARIANT (DOCTOR-02): the downgrade predicate is the
	// CONJUNCTION (ID in the superseded set) AND (Status==statusWarn). A Status==statusFail
	// on ANY ID — INCLUDING the very ROCM-PRE-firmware/-hsa/-image IDs (a Known deny-listed
	// firmware / Known-wrong HSA / denied RUNNING image, resolved from the configured backend)
	// — is NEVER suppressed and still folds to FAIL. A pure ID-set match
	// that ignored Status would wrongly swallow a confident FAIL on those IDs and is
	// FORBIDDEN. The suppression touches NOTHING else — not drift, health, loopback,
	// offload, or any non-ROCm-host-prep finding — and fires ONLY under proven ROCm residency.
	superseded := func(f Finding) bool {
		return rocmResidencyProven && f.Status == statusWarn && supersededROCmHostPrepID(f.ID)
	}
	// (Historical 4b: a MEMORY-SERVICE OFFLOAD DOWN-RANK predicate lived here while
	// the status fold mis-classified villa-qdrant/villa-embed as GPU rows carrying a
	// typed-Unknown offload WARN. Phase 23 (Plan 23-01) fixed the classification at
	// the source: memory rows are OffloadApplies=false in status.Run, so the
	// offloadFinding gate above (`if s.OffloadApplies`) never creates an
	// offload:<memory-svc> finding and the down-rank was unreachable dead code
	// deleted, together with the MemoryEnabled/MemoryServices Deps fields that
	// existed only to key it.)
	worst := 0
	for _, f := range findings {
		if superseded(f) {
			continue // visible but non-rank-raising under proven ROCm residency
		}
		if r := statusRank(f.Status); r > worst {
			worst = r
		}
	}
	overall := statusPass
	switch worst {
	case 2:
		overall = statusFail
	case 1:
		overall = statusWarn
	}

	return Report{
		Findings:      findings,
		Overall:       overall,
		SchemaVersion: reportSchemaVersion,
	}
}

// toolsDriftFinding builds TMD-01 from the seam's three-valued answer.
//
// A mismatch is a BLOCK-tier FAIL, not the WARN config-vs-disk drift carries: a unit
// rendered without the flag rejects every tool call at the server, and one rendered
// with it when the operator turned tools mode off is serving a template they did not
// ask for. An unanswerable question is a typed-Unknown WARN — never a PASS.
func toolsDriftFinding(served, want, ok bool) Finding {
	const (
		id   = "TMD-01"
		name = "Tools-mode drift"
	)
	if !ok {
		return Finding{
			ID:          id,
			Name:        name,
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      "could not read the on-disk inference unit to check whether it is served for tool calling",
			Remediation: "run `villa install` to write the Quadlet units, then re-run `villa doctor`",
			Provenance:  "on-disk villa-llama unit (unreadable)",
		}
	}
	if served == want {
		return Finding{
			ID:         id,
			Name:       name,
			Tier:       tierBlock,
			Status:     statusPass,
			Detail:     "the served unit's tool-calling flag matches tools mode (" + onOff(want) + ")",
			Provenance: "on-disk villa-llama unit vs subsystem.ToolsOn",
		}
	}
	if want {
		return Finding{
			ID:          id,
			Name:        name,
			Tier:        tierBlock,
			Status:      statusFail,
			Detail:      "tools mode is on but the served unit is not rendered for tool calling — every tool call will fail at the server",
			Remediation: "re-run `villa install` to reconcile the unit, or `villa tools-mode exit` if tool calling is not wanted",
			Provenance:  "on-disk villa-llama unit vs subsystem.ToolsOn",
		}
	}
	return Finding{
		ID:          id,
		Name:        name,
		Tier:        tierBlock,
		Status:      statusFail,
		Detail:      "tools mode is off but the served unit is rendered for tool calling — the unit is serving a chat template nobody asked for",
		Remediation: "re-run `villa install` to reconcile the unit, or `villa tools-mode enter` to persist the state the unit is already in",
		Provenance:  "on-disk villa-llama unit vs subsystem.ToolsOn",
	}
}

// onOff names a gate state for a finding detail.
func onOff(on bool) string {
	if on {
		return "on"
	}
	return "off"
}

// findingFromCheck normalizes a preflight.CheckResult into a doctor Finding,
// preserving its tier/status/detail/remediation/provenance.
func findingFromCheck(c preflight.CheckResult) Finding {
	return Finding{
		ID:          c.ID,
		Name:        c.Name,
		Tier:        c.Tier.String(),   // "BLOCK" | "WARN"
		Status:      c.Status.String(), // "PASS" | "WARN" | "FAIL"
		Detail:      c.Detail,
		Remediation: c.Remediation,
		Provenance:  c.Provenance,
		Raw:         c.Raw,
	}
}

// healthFinding maps a service's mapped health to a WARN-tier finding: HealthReady →
// PASS; HealthDown → WARN (a down/stopped stack is an expected, visible operational
// state, not a blocking fault — / the package contract reserves the blocking
// tier for the silent-degradation faults: a confident offload FAIL over a health-200,
// a preflight BLOCK, or a loopback breach); loading / unknown → typed-Unknown WARN
// (up-but-not-confirmed). Every branch stays in tierWarn, so a health signal NEVER
// escalates doctor to the blocking exit tier — keeping FAIL ⟺ BLOCK-class invariant.
func healthFinding(s status.ServiceStatus) Finding {
	f := Finding{
		ID:         "health:" + s.Service,
		Name:       s.Service + " health",
		Tier:       tierWarn,
		Provenance: "status.Report.Services[].Health",
	}
	switch s.Health {
	case status.HealthReady:
		f.Status = statusPass
		f.Detail = "/health is ready (200)"
	case status.HealthDown:
		f.Status = statusWarn
		f.Detail = "/health is unreachable — the service is not running"
		f.Remediation = "run `villa up` if the stack is stopped; otherwise check `villa status` / `villa logs`"
	default: // loading / unknown
		f.Status = statusWarn
		f.Detail = "health could not be confirmed (loading or unevaluable)"
		f.Remediation = "wait for the model to finish loading, then re-run `villa doctor`; check `villa logs`"
	}
	return f
}

// offloadFinding maps a service's running offload Verdict (consumed OPAQUELY) into a
// doctor Finding. A confident inference.StatusFail becomes a BLOCK-class FAIL
// that dominates a HealthReady (Pitfall 3 — no false-green over a health-200); an
// unevaluable StatusWarn degrades to a typed-Unknown WARN; a proven
// StatusPass is a PASS.
func offloadFinding(s status.ServiceStatus) Finding {
	v := s.Offload // inference.Verdict — read Status/Detail/Remediation ONLY (seam-clean)
	f := Finding{
		ID:         "offload:" + s.Service,
		Name:       s.Service + " GPU offload",
		Detail:     v.Detail,
		Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback / degraded backend = a real fault (BLOCK FAIL).
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "GPU offload is not happening — check the backend (`villa backend set`) and `villa logs`")
	default: // StatusWarn — offload could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "offload could not be verified — ensure the stack is running, then re-run `villa doctor`")
	}
	return f
}

// residencyUnderLoadFinding maps the chat-model residency-under-embedding-load proof
// Verdict (consumed OPAQUELY — Status/Detail/Remediation only, seam-clean) into a
// doctor Finding, copying offloadFinding's switch: a confident CPU fallback
// under embedding load is a BLOCK-class FAIL (the silent-degradation fault this
// finding exists to catch); an unevaluable proof (stack down, scrape failed, drive
// could not complete) degrades to a typed-Unknown WARN — NEVER a false-green PASS.
// Emitted only when subsystem.MemoryOn(cfg) (no finding at all otherwise).
func residencyUnderLoadFinding(v inference.Verdict) Finding {
	f := Finding{
		ID:         "MEM-DOC-residency",
		Name:       "Chat-model residency under embedding load",
		Detail:     v.Detail,
		Provenance: "embed-load drive + inference.RunningOffloadVerdict",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback of the CHAT model under embedding load = a real
		// fault (BLOCK FAIL) — never a false-green over a healthy-looking stack.
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "the chat model fell back to CPU under embedding load — check the backend (`villa backend set`) and `villa logs`")
	default: // StatusWarn — residency under load could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "could not evaluate residency under embedding load — ensure the stack is running, then re-run `villa doctor`")
	}
	return f
}

// agentToolCallFinding maps the coding-agent tool-call round-trip proof Verdict (consumed
// OPAQUELY — Status/Detail/Remediation only, seam-clean) into a doctor Finding, cloning
// offloadFinding's offload-FAIL-dominates switch: a confident StatusFail (the agent
// could not complete a real read→edit `crush run` round-trip) is a BLOCK-class FAIL that
// DOMINATES a healthy-looking HTTP-200 — never a false-green; an unevaluable proof
// degrades to a typed-Unknown WARN. Emitted only when subsystem.AgentOn(cfg).
func agentToolCallFinding(v inference.Verdict) Finding {
	f := Finding{
		ID:         "agent-tool-call",
		Name:       "Coding-agent tool-call round-trip",
		Detail:     v.Detail,
		Provenance: "crush-run tool-call round-trip (liveAgentToolCallProbe)",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident failure of the agent tool-call round-trip = a real fault (BLOCK FAIL),
		// never a false-green over a healthy-looking inference endpoint.
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "the agent tool-call round-trip failed — check `villa verify agent` and `villa logs`")
	default: // StatusWarn — the round-trip could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "could not evaluate the agent tool-call round-trip — ensure the stack and the agent are installed, then re-run `villa doctor`")
	}
	return f
}

// agentResidencyFinding maps the coder-model residency-under-tool-call-load proof Verdict
// (consumed OPAQUELY) into a doctor Finding, using the IDENTICAL offload-FAIL-dominates
// switch (honesty dominance): a confident CPU fallback of the CODER model under
// tool-call load is a BLOCK-class FAIL that dominates a health-200; an unevaluable proof
// degrades to a typed-Unknown WARN — NEVER a false-green PASS. Emitted only when
// subsystem.AgentOn(cfg).
func agentResidencyFinding(v inference.Verdict) Finding {
	f := Finding{
		ID:         "agent-residency",
		Name:       "Coder-model residency under tool-call load",
		Detail:     v.Detail,
		Provenance: "tool-call drive + inference.RunningOffloadVerdict",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback of the CODER model under tool-call load = a real fault
		// (BLOCK FAIL) — never a false-green over a healthy-looking stack.
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "the coder model fell back to CPU under tool-call load — check the backend (`villa backend set`) and `villa logs`")
	default: // StatusWarn — residency under load could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "could not evaluate coder residency under tool-call load — ensure the stack is running, then re-run `villa doctor`")
	}
	return f
}

// searchEgressFinding maps the last recorded `villa verify search` result into a
// doctor Finding, through status.WebSearchSection so doctor and `villa status` apply
// ONE freshness rule to it, never a config bool. The section's outbound-bounded
// tri-state is the status core's answer; doctor adds severity, wording and remediation,
// and nothing else:
//
//   - "bounded" (a fresh verify PASS) → a PASS;
//   - "not-bounded" (a fresh verify that did NOT pass) → a BLOCK-class FAIL, a real,
//     confident security-property failure that is never swallowed;
//   - anything else ("unknown": unreadable, absent, stale, future-dated or undated) →
//     a WARN-tier typed-Unknown, because the property must be re-proven, never trusted
//     indefinitely from a stale cache. The detail says which of those it was.
//
// reportErr is why the status report could not be evaluated, nil when it could. It
// never changes the answer, which does not depend on the report; it is named on the
// WARN so an operator sees both faults at once. Every non-PASS branch carries a
// Remediation. Emitted only when web search is on.
func searchEgressFinding(st *verifystate.State, reportErr error) Finding {
	w := status.WebSearchSection(func() *verifystate.State { return st })
	f := Finding{
		ID:         "search-egress",
		Name:       "Web-search outbound-bounded proof",
		Provenance: "cached `villa verify search` result + freshness gate (status.WebSearchSection)",
	}
	switch w.OutboundBounded {
	case status.OutboundBounded:
		f.Tier = tierBlock
		f.Status = statusPass
		f.Detail = "outbound bounded: a recent `villa verify search` PASS (checked " + w.VerifyCheckedAt + ")"
	case status.OutboundNotBounded:
		f.Tier = tierBlock
		f.Status = statusFail
		f.Detail = "the last `villa verify search` did not pass (verdict " + st.Verdict + ", checked " + w.VerifyCheckedAt + ")"
		f.Remediation = "re-run `villa verify search` and check `villa logs` — outbound is not proven bounded"
	default:
		f.Tier = tierWarn
		f.Status = statusWarn
		switch {
		case st == nil:
			f.Detail = "outbound is not proven bounded: the recorded `villa verify search` result could not be read"
		case st.CheckedAt == "":
			f.Detail = "outbound is not proven bounded: no `villa verify search` result is recorded"
		default:
			f.Detail = "outbound is not proven bounded: the last `villa verify search` (checked " + st.CheckedAt + ") is stale or cannot be dated"
		}
		f.Remediation = "run `villa verify search` to re-prove outbound is bounded, then re-run `villa doctor`"
		if reportErr != nil {
			f.Detail += "; the status report also failed: " + reportErr.Error()
		}
	}
	return f
}

// searchResidencyFinding maps the chat-model residency-under-SEARCH-load proof Verdict
// into a doctor Finding, using the IDENTICAL offload-FAIL-dominates
// switch (the project's offload-asserting invariant applied to the search path): a confident
// CPU fallback of the served model under search load is a BLOCK-class FAIL that DOMINATES a
// health-200 — never a false-green; a not-in-flight / unevaluable proof degrades to a
// typed-Unknown WARN (never an idle-sampled false-green PASS). Emitted only when
// subsystem.WebSearchOn(cfg).
func searchResidencyFinding(v inference.Verdict) Finding {
	f := Finding{
		ID:         "search-residency",
		Name:       "Chat-model residency under search load",
		Detail:     v.Detail,
		Provenance: "search-load drive + inference.RunningOffloadVerdict",
	}
	switch v.Status {
	case inference.StatusPass:
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// Confident CPU fallback of the served model under search load = a real fault
		// (BLOCK FAIL) — never a false-green over a healthy-looking HTTP-200.
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "the chat model fell back to CPU under search load — check the backend (`villa backend set`) and `villa logs`")
	default: // StatusWarn — residency under search load could not be EVALUATED
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "could not evaluate residency under search load — ensure the stack (incl. villa-searxng/villa-websafe) is running, then re-run `villa doctor`")
	}
	return f
}

// agentConfigDriftFindings is the config half of agentDriftFindings: one WARN when
// crush.json drifted, none otherwise. A key-only drift (ADR-0019) names the heal a
// stack apply or `villa code` performs; doctor itself stays read-only (ADR-0017).
func agentConfigDriftFindings(r agent.DriftReport) []Finding {
	if !r.ConfigDrift {
		return nil
	}
	f := Finding{
		ID:          "agent-config-drift",
		Name:        "Coding-agent config drift",
		Tier:        tierWarn,
		Status:      statusWarn,
		Detail:      nonEmpty(r.Reason, "on-disk crush.json differs from what villa would render from config.toml"),
		Remediation: "review your crush.json edits or re-render from config.toml; villa surfaces drift but never overwrites your file automatically",
		Provenance:  "agent.DetectDrift (ConfigDrift)",
	}
	if r.ConfigKeyOnly {
		f.Remediation = "nothing to edit — run `villa up` (or any stack apply) or `villa code`; either rewrites only the stale inference key and keeps the old file as crush.json.bak"
		f.Provenance = "agent.DetectDrift (ConfigDrift, key only)"
	}
	return []Finding{f}
}

// agentDriftFindings maps a report-only agent.DriftReport into doctor Findings.
// Drift is SURFACED, never auto-corrected: each non-clean signal is a WARN-with-remediation,
// never a BLOCK FAIL (a drifted/absent binary or hand-edited config is an operator decision,
// not a silent-degradation fault). The honesty discipline:
//
//   - BinaryAbsent           → WARN + Phase-27 install remediation (agent-binary-drift).
//   - BinaryDriftUnknown     → typed-Unknown WARN (the policy hash is not yet pinned).
//   - BinaryDrift            → WARN + re-install remediation (never auto-corrected).
//   - ConfigDrift            → WARN + review/re-render remediation (never overwritten); a
//     key-only drift names the stack-apply / `villa code` heal instead (ADR-0019).
//   - ConfigAbsent ALONE     → NO finding (the first-run render trigger, parallels BinaryAbsent
//     — NOT drift; emitting a finding here would mis-report a false drift at the first run).
//   - all clean              → a single PASS finding (agent-drift).
//
// The DriftReport.Reason carries the human remediation text; doctor surfaces it as the
// finding Detail. Emitted only when subsystem.AgentOn(cfg).
func agentDriftFindings(r agent.DriftReport) []Finding {
	var out []Finding

	// Binary signal — at most one binary finding.
	switch {
	case r.BinaryAbsent:
		out = append(out, Finding{
			ID:          "agent-binary-drift",
			Name:        "Coding-agent binary",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      nonEmpty(r.Reason, "the villa-owned Crush binary is not installed"),
			Remediation: "run the agent install (the Phase-27 `villa install` addon) to place the pinned binary, then re-run `villa doctor`",
			Provenance:  "agent.DetectDrift (BinaryAbsent)",
		})
	case r.BinaryDriftUnknown:
		out = append(out, Finding{
			ID:          "agent-binary-drift",
			Name:        "Coding-agent binary drift",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      nonEmpty(r.Reason, "binary drift could not be confirmed — the policy binary checksum is not yet pinned"),
			Remediation: "no action needed yet — the binary-drift gate activates once the policy checksum is pinned on-hardware",
			Provenance:  "agent.DetectDrift (BinaryDriftUnknown)",
		})
	case r.BinaryDrift:
		out = append(out, Finding{
			ID:          "agent-binary-drift",
			Name:        "Coding-agent binary drift",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      nonEmpty(r.Reason, "installed Crush binary checksum does not match the pinned policy"),
			Remediation: "re-install the pinned binary (the Phase-27 `villa install` addon); villa never auto-corrects a drifted binary",
			Provenance:  "agent.DetectDrift (BinaryDrift)",
		})
	}

	// Config signal — ConfigAbsent ALONE emits NO finding (first-run trigger, not drift).
	out = append(out, agentConfigDriftFindings(r)...)

	// All clean (binary present + matched + config present + matched, OR the unpinned/
	// first-run benign states that emitted no WARN above): a single PASS finding so the
	// agent-on doctor table always carries a positive drift signal. ConfigAbsent is benign
	// (first-run) and BinaryDriftUnknown is benign-but-WARN; we only emit the PASS when
	// NOTHING above produced a finding.
	if len(out) == 0 {
		out = append(out, Finding{
			ID:         "agent-drift",
			Name:       "Coding-agent drift",
			Tier:       tierWarn,
			Status:     statusPass,
			Detail:     "the installed Crush binary and on-disk crush.json match the pinned policy and rendered reference",
			Provenance: "agent.DetectDrift (clean)",
		})
	}
	return out
}

// nonEmpty returns the upstream remediation when present, else a doctor default — so
// every non-PASS finding always carries actionable text.
func nonEmpty(upstream, fallback string) string {
	if upstream != "" {
		return upstream
	}
	return fallback
}

// updatesFinding folds the LAST RECORDED update check (issue #216). It reads the
// status report and fetches nothing: checks stay strictly on-command, so the only
// remediation this finding ever names is the command that runs one. Never checked
// and stale are both WARN, because on this verb the operator asked what to look
// at, and an answer nobody has fetched in a month is not evidence about today.
func updatesFinding(u status.UpdatesInfo) Finding {
	f := Finding{
		ID:         "UPD-01",
		Name:       "Pin-manifest check",
		Tier:       tierWarn,
		Provenance: "status.Report.Updates (pin-state.json; read, never fetched)",
	}
	if u.State != status.UpdatesChecked || u.AgeDays == nil {
		f.Status = statusWarn
		f.Detail = "villa has never checked for updates on this host"
		f.Remediation = "run `villa update --check`"
		return f
	}
	days := *u.AgeDays
	age := fmt.Sprintf("%d days ago", days)
	switch days {
	case 0:
		age = "today"
	case 1:
		age = "1 day ago"
	}
	f.Detail = fmt.Sprintf("last checked %s (%s)", age, u.CheckedAt)
	if days >= status.UpdateStaleDays {
		f.Status = statusWarn
		f.Detail += "; the recorded answer is no longer evidence about today"
		f.Remediation = "run `villa update --check`"
		return f
	}
	f.Status = statusPass
	return f
}
