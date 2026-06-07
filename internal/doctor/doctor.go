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
//   - doctor owns its OWN Report type and its OWN golden (D-02). It only READS the
//     byte-frozen status.Report; it never extends or mutates it.
//   - COMPOSITION ONLY (D-01): it never re-implements a probe a shipped core produces.
//   - Backend marker literals stay behind the inference seam: doctor consumes
//     inference.Verdict values OPAQUELY (Status/Detail/Remediation only) and routes
//     ROCm-family backends via inference.IsROCmFamily — never typing Vulkan0/ROCm0/
//     image tags (TestSeamGrepGate walks internal/).
//
// Severity / exit mapping (D-04, Pitfall 1 — the shipped preflight constants are
// AUTHORITATIVE, NOT the inverted ROADMAP prose): a confident BLOCK-class FAIL
// (preflight BLOCK FAIL, a confident residency/offload FAIL) → the blocked tier
// (exit 1); a WARN (preflight WARN, config-vs-disk drift, a typed-Unknown /
// unevaluable signal, a down stack) → the warn tier (exit 2); all healthy → 0.
package doctor

import (
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

// reportSchemaVersion is doctor's OWN --json contract self-version (D-09), distinct
// from status.reportSchemaVersion. =1 from day one; bumped append-only on any future
// additive change to the doctor Report contract.
const reportSchemaVersion = 1

// Finding is doctor's normalized, renderable health finding — a doctor-OWNED wrapper
// (mirroring preflight.CheckResult's field set, D-02 spirit) so doctor's golden never
// couples to an upstream struct. Every non-PASS Finding MUST carry a non-empty
// Remediation (D-11).
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
	// Remediation is an actionable hint for a non-PASS result (D-11). Empty on PASS.
	Remediation string `json:"remediation"`
	// Provenance records which composed core / signal produced this finding, for -v.
	Provenance string `json:"provenance"`
	// Raw captures untrusted raw output, surfaced under -v only — NEVER serialized to
	// the --json contract (mirrors preflight.CheckResult.Raw / inference.Verdict.Raw).
	Raw string `json:"-"`
}

// Report is doctor's OWN aggregated --json contract. It is NOT status.Report (D-02).
type Report struct {
	// Findings is every normalized finding from the composed cores + the drift check.
	Findings []Finding `json:"findings"`
	// Overall is the worst-wins verdict string: PASS | WARN | FAIL.
	Overall string `json:"overall"`
	// SchemaVersion is the contract self-version. It MUST stay the LAST tagged field
	// (append-only; new tagged fields go above it). =1 from day one (D-09).
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
	// error (absent unit dir) degrades to a typed-Unknown WARN finding (D-08).
	DriftPlan func() (orchestrate.Plan, error)
	// Backend is the configured backend name, routing the ROCm-family preflight gate
	// via inference.IsROCmFamily.
	Backend string
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
func Aggregate(d Deps) Report {
	var findings []Finding

	// 1. HOST CONDITIONS — re-run the read-only preflight host-prep gate against the
	// running host, routed by the configured backend (ROCm-family → RunROCm).
	profile := d.Probe()
	var checks []preflight.CheckResult
	if inference.IsROCmFamily(d.Backend) {
		checks = preflight.RunROCm(profile)
	} else {
		checks = preflight.Run(profile)
	}
	for _, c := range checks {
		findings = append(findings, findingFromCheck(c))
	}

	// 2. RUNNING-STACK HEALTH — fold the status read-model. A confident offload FAIL
	// becomes a BLOCK-class FAIL that DOMINATES a HealthReady (Pitfall 3 / D-05); a
	// HealthDown / unevaluable signal degrades to a typed-Unknown WARN (D-06/D-08).
	report := d.StatusReport()
	if !report.LoopbackOnly {
		findings = append(findings, Finding{
			ID:          "loopback",
			Name:        "Loopback-only bind",
			Tier:        tierBlock,
			Status:      statusFail,
			Detail:      "a published port binds a non-loopback address (privacy breach, PRIV-01)",
			Remediation: "re-run `villa install` to regenerate loopback-only units, then `villa down && villa up`",
			Provenance:  "status.Report.LoopbackOnly",
		})
	}
	for _, s := range report.Services {
		findings = append(findings, healthFinding(s))
		if s.OffloadApplies {
			findings = append(findings, offloadFinding(s))
		}
	}

	// 3. DRIFT — config-vs-disk drift is independent of running-stack health: even a
	// fully-healthy stack on stale units is a WARN (Pitfall 4 / D-05/D-10). A read
	// error (absent/unreadable unit dir) degrades to a typed-Unknown WARN (D-08).
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
	worst := 0
	for _, f := range findings {
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
		Tier:        c.Tier.String(),    // "BLOCK" | "WARN"
		Status:      c.Status.String(),  // "PASS" | "WARN" | "FAIL"
		Detail:      c.Detail,
		Remediation: c.Remediation,
		Provenance:  c.Provenance,
		Raw:         c.Raw,
	}
}

// healthFinding maps a service's mapped health to a WARN-tier finding: HealthReady →
// PASS; HealthDown → WARN (a down/stopped stack is an expected, visible operational
// state, not a blocking fault — D-08 / the package contract reserves the blocking
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
// doctor Finding. A confident inference.StatusFail becomes a BLOCK-class FAIL (D-05)
// that dominates a HealthReady (Pitfall 3 — no false-green over a health-200); an
// unevaluable StatusWarn degrades to a typed-Unknown WARN (D-06/D-08); a proven
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

// nonEmpty returns the upstream remediation when present, else a doctor default — so
// every non-PASS finding always carries actionable text (D-11).
func nonEmpty(upstream, fallback string) string {
	if upstream != "" {
		return upstream
	}
	return fallback
}
