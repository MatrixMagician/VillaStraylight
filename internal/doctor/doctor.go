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
	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
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
//     shape; web-search-OFF output is byte-identical except this bump (nil seams emit no
//     findings). This is doctor's OWN version — INDEPENDENT of status.reportSchemaVersion (5).
const reportSchemaVersion = 3

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

// Deps are the injectable host seams Aggregate composes. The live wiring is a
// liveDoctorDeps() closure in cmd/villa (Plan 02); doctor_test.go replaces them with
// stubs. The core never does I/O of its own.
type Deps struct {
	// Probe returns the host profile that feeds the preflight host-condition checks.
	Probe func() detect.HostProfile
	// LoadConfig is the source of truth (config.LoadVilla). Reserved for the cmd-tier
	// drift wiring; the core reads it only if a future finding needs config directly.
	LoadConfig func() (config.VillaConfig, error)
	// StatusReport returns the running-stack read-model (== status.Run(liveStatusDeps)).
	// It already carries per-service offload Verdicts, so doctor reuses it rather than
	// re-running a second journald/GTT scrape (RESEARCH A1).
	StatusReport func() status.Report
	// DriftPlan renders units from config and Reconciles them against the on-disk unit
	// dir, returning the Plan (the core decides drift). It NEVER writes units. A read
	// error (absent unit dir) degrades to a typed-Unknown WARN finding.
	DriftPlan func() (orchestrate.Plan, error)
	// Backend is the configured backend name, routing the ROCm-family preflight gate
	// via inference.IsROCmFamily.
	Backend string
	// RunROCmImage is the image-AWARE ROCm host-prep gate (Option B): it evaluates the
	// RUNNING ROCm image against the policy denylist so a denied running image is a
	// confident FAIL rather than the un-evaluated "no image requested" WARN. The live
	// wiring (liveDoctorDeps) supplies preflight.RunROCmForImage bound to
	// inference.BackendFor(cfg.Backend).Image() — the image string is resolved ONLY via
	// the inference seam, never typed in this package (TestSeamGrepGate). NIL-SAFE: when
	// nil (e.g. the newDoctorDeps test double, or a non-ROCm backend), Aggregate falls
	// back to preflight.RunROCm(profile) exactly as before.
	RunROCmImage func(detect.HostProfile) []preflight.CheckResult
	// RunMemoryChecks is the opt-in memory host gate (preflight.RunMemory bound by
	// the cmd tier —, composition over re-implementation): the vector-disk +
	// embedder-headroom CheckResults are folded via findingFromCheck and ranked
	// worst-wins exactly like every other check. NIL-SAFE: when nil (memory off)
	// no memory check finding is emitted and output stays byte-identical.
	RunMemoryChecks func(detect.HostProfile) []preflight.CheckResult
	// ResidencyUnderLoad is the chat-model residency-under-embedding-load proof
	// the cmd tier drives a REAL /v1/embeddings workload and samples the
	// chat model's GTT/journal residency MID-DRIVE, returning the Verdict consumed
	// OPAQUELY here (Status/Detail/Remediation only — seam-clean). NIL-SAFE: when
	// nil (memory off) NO MEM-DOC-residency finding is emitted at all — never a
	// PASS-by-default (no-false-green).
	ResidencyUnderLoad func() inference.Verdict
	// AgentToolCall is the coding-agent tool-call round-trip proof:
	// the cmd tier drives a REAL read→edit `crush run` round-trip and maps its
	// completion to an inference.Verdict consumed OPAQUELY here (Status/Detail/
	// Remediation only — seam-clean). NIL-SAFE: when nil (agent off) NO agent-tool-call
	// finding is emitted at all — never a PASS-by-default (no-false-green).
	AgentToolCall func() inference.Verdict
	// AgentResidencyUnderLoad is the coder-model residency-under-tool-call-load proof
	// the cmd tier samples the served CODER model's GTT/journal
	// residency MID-DRIVE under a real tool-call workload, returning the Verdict
	// consumed OPAQUELY here. NIL-SAFE: when nil (agent off) NO agent-residency finding
	// is emitted at all — never a PASS-by-default (honesty dominance).
	AgentResidencyUnderLoad func() inference.Verdict
	// AgentDrift is the binary/version + config drift report from internal/agent
	// the cmd tier feeds the installed-binary SHA + on-disk crush.json
	// + freshly-rendered reference to agent.DetectDrift and hands back the report-only
	// outcome. It is surfaced, NEVER auto-corrected. NIL-SAFE: when nil (agent
	// off) NO agent drift finding is emitted at all.
	AgentDrift func() agent.DriftReport
	// SearchEgressProof is the web-search egress-proof seam: the cmd
	// tier reads the CACHED `villa verify search` result (verifystate.Load) and maps it to
	// a tri-state inference.Verdict consumed OPAQUELY here (Status/Detail/Remediation only
	// — seam-clean): a fresh cached PASS → StatusPass (ready); a real recent non-PASS →
	// StatusFail (degraded-with-reason); a stale/absent cache → StatusWarn (typed-Unknown,
	// NEVER a config-bool-derived PASS). NIL-SAFE: when nil (web search off) NO search-egress
	// finding is emitted at all — never a PASS-by-default (no-false-green).
	SearchEgressProof func() inference.Verdict
	// SearchResidencyUnderLoad is the chat-model residency-under-SEARCH-load proof (
	// the cmd tier drives a bounded search-augmented chat workload (with villa-searxng
	// /villa-websafe up) and samples the served model's GTT/journal residency MID-DRIVE,
	// returning the Verdict consumed OPAQUELY here. A confident CPU fallback under search load
	// is a BLOCK-class FAIL that DOMINATES a healthy-looking HTTP-200; a not-in-flight /
	// unevaluable signal → typed-Unknown WARN (never an idle-sampled false-green). NIL-SAFE:
	// when nil (web search off) NO search-residency finding is emitted at all.
	//
	// SCOPE OMISSION (accepted limit): guard health has NO host-side source — the
	// per-request guard metadata lives in-container only, with no host aggregate. Building a
	// guard counter/health pipeline = NEW behavior = OUT OF SCOPE for this surfacing phase, so
	// NO guard-health finding is emitted (a documented omission, never a fabricated PASS/0).
	// searxng/websafe service READINESS is folded for free: status.Run already emits dedicated
	// villa-searxng/villa-websafe Services rows (Plan 03) which flow through the existing
	// healthFinding loop — no new finding type is added here (composition, RESEARCH A1).
	SearchResidencyUnderLoad func() inference.Verdict
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

// Aggregate composes the shipped cores into a single worst-wins doctor Report. It is
// pure: every host touch is a Deps seam and it never exits or prints.
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
func Aggregate(d Deps) Report {
	var findings []Finding

	// 1. HOST CONDITIONS — re-run the read-only preflight host-prep gate against the
	// running host, routed by the configured backend (ROCm-family → RunROCm).
	profile := d.Probe()
	var checks []preflight.CheckResult
	switch {
	case inference.IsROCmFamily(d.Backend) && d.RunROCmImage != nil:
		// Option B: evaluate the ACTUAL running ROCm image (a denied running image →
		// confident FAIL, never swallowed by the supersession; see fold step 4a). The
		// image literal is supplied by the live wiring, never typed in this core.
		checks = d.RunROCmImage(profile)
	case inference.IsROCmFamily(d.Backend):
		checks = preflight.RunROCm(profile)
	default:
		checks = preflight.Run(profile)
	}
	for _, c := range checks {
		findings = append(findings, findingFromCheck(c))
	}

	// 1b. MEMORY HOST GATE: fold the opt-in vector-disk/headroom checks
	// (preflight.RunMemory, bound by the cmd tier) verbatim via findingFromCheck
	// no new aggregation logic; they rank worst-wins like every other check. A nil
	// seam (memory off) emits nothing, keeping the off path byte-identical.
	if d.RunMemoryChecks != nil {
		for _, c := range d.RunMemoryChecks(profile) {
			findings = append(findings, findingFromCheck(c))
		}
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
	if err := report.Err(); err != nil {
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
			Detail:      "the running-stack state could not be evaluated: " + err.Error(),
			Remediation: "fix the reported condition (check config.toml and `villa status`), then re-run `villa doctor`",
			Provenance:  "status.Run error",
			Raw:         err.Error(),
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
		for _, s := range report.Services {
			findings = append(findings, healthFinding(s))
			if s.OffloadApplies {
				findings = append(findings, offloadFinding(s))
				if inference.IsROCmFamily(d.Backend) && s.Offload.Status == inference.StatusPass {
					rocmResidencyProven = true
				}
			}
		}
	}

	// 2b. RESIDENCY UNDER EMBEDDING LOAD: the chat model must SURVIVE a real
	// embedding workload — a silent eviction to CPU under import load is the exact
	// false-green this phase exists to catch. The proof seam is nil when memory is
	// off: no finding is emitted at all (never a PASS-by-default). The Verdict is
	// consumed opaquely via the offloadFinding precedent below.
	if d.ResidencyUnderLoad != nil {
		findings = append(findings, residencyUnderLoadFinding(d.ResidencyUnderLoad()))
	}

	// 2c. CODING-AGENT FOLD: when the agent is enabled the cmd tier
	// binds the three agent seams; each NIL seam (agent off) emits NO finding (never a
	// PASS-by-default — agent-off output stays byte-identical except the schema bump). A
	// confident agent tool-call / residency FAIL folds worst-wins and DOMINATES a
	// healthy-looking HTTP-200 (the offload-FAIL-dominates switch, cloned below). Drift is
	// surfaced as WARN-with-remediation, never auto-corrected.
	if d.AgentToolCall != nil {
		findings = append(findings, agentToolCallFinding(d.AgentToolCall()))
	}
	if d.AgentResidencyUnderLoad != nil {
		findings = append(findings, agentResidencyFinding(d.AgentResidencyUnderLoad()))
	}
	if d.AgentDrift != nil {
		findings = append(findings, agentDriftFindings(d.AgentDrift())...)
	}

	// 2d. WEB-SEARCH FOLD: when web search is enabled the cmd
	// tier binds the two web-search seams; each NIL seam (web off) emits NO finding (never a
	// PASS-by-default — web-off output stays byte-identical except the schema bump). The
	// egress-proof finding is a tri-state derived from the CACHED `villa verify search`
	// result (NEVER a config bool); the residency finding is offload-asserting — a
	// confident CPU fallback under search load folds worst-wins and DOMINATES a healthy-looking
	// HTTP-200 (the offload-FAIL-dominates switch cloned below). searxng/websafe
	// service READINESS needs NO finding here — status.Run already surfaces dedicated
	// villa-searxng/villa-websafe rows (Plan 03) folded by the healthFinding loop in step 2.
	// Guard health is a documented OMISSION (no host-side source — accepted scope limit).
	if d.SearchEgressProof != nil {
		findings = append(findings, searchEgressFinding(d.SearchEgressProof()))
	}
	if d.SearchResidencyUnderLoad != nil {
		findings = append(findings, searchResidencyFinding(d.SearchResidencyUnderLoad()))
	}

	// 3. DRIFT — config-vs-disk drift is independent of running-stack health: even a
	// fully-healthy stack on stale units is a WARN (Pitfall 4). A read
	// error (absent/unreadable unit dir) degrades to a typed-Unknown WARN.
	plan, err := d.DriftPlan()
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
			Detail:      "on-disk Quadlet units no longer match the rendered-from-config units",
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
	// firmware / Known-wrong HSA / denied RUNNING image, the last reachable only via the
	// RunROCmImage seam) — is NEVER suppressed and still folds to FAIL. A pure ID-set match
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
// Emitted only when Deps.ResidencyUnderLoad is non-nil (nil → no finding at all).
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
// degrades to a typed-Unknown WARN. Emitted only when Deps.AgentToolCall is non-nil.
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
// Deps.AgentResidencyUnderLoad is non-nil.
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

// searchEgressFinding maps the web-search egress-proof Verdict into a
// doctor Finding. The Verdict is consumed OPAQUELY (Status/Detail/Remediation only
// seam-clean): the cmd tier has already mapped the CACHED `villa verify search` result to
// a tri-state (PASS+fresh → StatusPass "ready"; a real recent non-PASS → StatusFail
// "degraded-with-reason"; stale/absent → StatusWarn typed-Unknown). This switch mirrors
// offloadFinding's grammar but the egress-proof tiers differ: a degraded egress proof is a
// real, confident security-property FAILURE (a verify search that did NOT pass) — a
// BLOCK-class FAIL, never swallowed; a stale/absent cache is a WARN-tier typed-Unknown
// (the property must be re-proven, never trusted indefinitely — and never a config-bool
// PASS). Every non-PASS branch carries a Remediation. Emitted only when
// Deps.SearchEgressProof is non-nil.
func searchEgressFinding(v inference.Verdict) Finding {
	f := Finding{
		ID:         "search-egress",
		Name:       "Web-search outbound-bounded proof",
		Detail:     v.Detail,
		Provenance: "cached `villa verify search` result (verifystate.Load) + freshness gate",
	}
	switch v.Status {
	case inference.StatusPass:
		// A fresh cached verify-search PASS — outbound is proven bounded (ready).
		f.Tier = tierBlock
		f.Status = statusPass
	case inference.StatusFail:
		// A real RECENT verify-search non-PASS — the outbound-bounded security property is
		// confidently NOT holding (degraded-with-reason). A real fault, never a false-green.
		f.Tier = tierBlock
		f.Status = statusFail
		f.Remediation = nonEmpty(v.Remediation, "the last `villa verify search` did not pass — re-run `villa verify search` and check `villa logs`")
	default: // StatusWarn — no fresh evaluable proof (stale/absent cache)
		// typed-Unknown: a security property must be re-proven, NEVER trusted from a stale
		// cache and NEVER inferred from cfg.WebSearchEnabled.
		f.Tier = tierWarn
		f.Status = statusWarn
		f.Remediation = nonEmpty(v.Remediation, "no fresh verified outbound-bounded result — run `villa verify search`, then re-run `villa doctor`")
	}
	return f
}

// searchResidencyFinding maps the chat-model residency-under-SEARCH-load proof Verdict
// into a doctor Finding, using the IDENTICAL offload-FAIL-dominates
// switch (the project's offload-asserting invariant applied to the search path): a confident
// CPU fallback of the served model under search load is a BLOCK-class FAIL that DOMINATES a
// health-200 — never a false-green; a not-in-flight / unevaluable proof degrades to a
// typed-Unknown WARN (never an idle-sampled false-green PASS). Emitted only when
// Deps.SearchResidencyUnderLoad is non-nil.
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

// agentDriftFindings maps a report-only agent.DriftReport into doctor Findings.
// Drift is SURFACED, never auto-corrected: each non-clean signal is a WARN-with-remediation,
// never a BLOCK FAIL (a drifted/absent binary or hand-edited config is an operator decision,
// not a silent-degradation fault). The honesty discipline:
//   - BinaryAbsent           → WARN + Phase-27 install remediation (agent-binary-drift).
//   - BinaryDriftUnknown     → typed-Unknown WARN (the policy hash is not yet pinned).
//
// - BinaryDrift → WARN + re-install remediation (never auto-corrected).
// - ConfigDrift → WARN + review/re-render remediation (never overwritten).
//   - ConfigAbsent ALONE     → NO finding (the first-run render trigger, parallels BinaryAbsent
//     — NOT drift; emitting a finding here would mis-report a false drift at the first run).
//   - all clean              → a single PASS finding (agent-drift).
//
// The DriftReport.Reason carries the human remediation text; doctor surfaces it as the
// finding Detail. Emitted only when Deps.AgentDrift is non-nil.
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
	if r.ConfigDrift {
		out = append(out, Finding{
			ID:          "agent-config-drift",
			Name:        "Coding-agent config drift",
			Tier:        tierWarn,
			Status:      statusWarn,
			Detail:      nonEmpty(r.Reason, "on-disk crush.json differs from what villa would render from config.toml"),
			Remediation: "review your crush.json edits or re-render from config.toml; villa surfaces drift but never overwrites your file automatically",
			Provenance:  "agent.DetectDrift (ConfigDrift)",
		})
	}

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
