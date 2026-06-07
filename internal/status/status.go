// Package status is the extracted `villa status` read-model core (DASH-01): the
// JSON-neutral aggregation that turns the injected host seams into a frozen
// StatusReport (here exported as Report) and the worst-wins overall Verdict.
//
// It was moved VERBATIM out of cmd/villa/status.go (Pitfall 1: JSON-neutral move)
// so the Phase-5 dashboard backend can call the SAME logic the CLI does, not a
// fork. Every json:"..." tag and field order is preserved byte-for-byte; the
// byte-frozen `status --json` golden in cmd/villa stays green with zero edits.
//
// All host-touching actions are injected via Deps so both the cobra caller
// (cmd/villa) and the dashboard handler can drive it with their own live wiring;
// internal/status itself stays free of http/journald/systemd coupling.
package status

import (
	"errors"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// noTelemetryStatement is the PRIV-03 assertion the report always carries.
const noTelemetryStatement = "no telemetry; outbound = image/model pulls only"

// activeErrored is the synthetic active-state set when `systemctl is-active` ran but
// errored with no parseable state (orchestrate.ErrCommandFailed). It is distinct from
// the empty "" (clean-but-silent → WARN) and "unknown" (cannot measure → WARN) tokens
// so an indeterminate-but-bad unit drives FAIL (CR-02 tighten).
const activeErrored = "errored"

// HealthState is the mapped container-health outcome (D-14): 200→ready, 503→loading
// (Unknown, NOT down — WR-07), transport error→down.
type HealthState string

const (
	HealthReady   HealthState = "ready"   // /health 200
	HealthLoading HealthState = "loading" // /health 503 — up but still loading (Unknown)
	HealthDown    HealthState = "down"    // transport error / unreachable
	HealthUnknown HealthState = "unknown" // could not probe (no endpoint)
)

// ServiceStatus is the per-service aggregate row in the report.
type ServiceStatus struct {
	Service string      `json:"service"`
	Active  string      `json:"active"` // systemctl is-active state
	Health  HealthState `json:"health"` // mapped /health (or owui /v1/models reachability)

	// Offload is the running-server GPU-offload Verdict. It is only meaningful for the
	// inference service; for non-GPU managed services (Open WebUI) it is an N/A
	// representation that does NOT fold into the overall verdict (OffloadApplies=false).
	Offload inference.Verdict `json:"offload"`
	// OffloadApplies marks whether the offload Verdict is a real GPU-offload assertion
	// (inference) versus an N/A placeholder (a non-GPU service). Aggregate folds
	// the offload Status ONLY when this is true, so a non-GPU service can never record a
	// spurious offload PASS nor a false offload FAIL (D-12).
	OffloadApplies bool `json:"offload_applies"`
	// OffloadOK is a convenience: a proven offload PASS. Always false when offload is
	// N/A (a non-GPU service is never "offload OK").
	OffloadOK bool `json:"offload_ok"`
}

// naOffloadVerdict is the typed-Unknown / N-A offload representation for a non-GPU
// managed service (Open WebUI). It is deliberately a WARN-typed Verdict (uncertainty,
// never a false PASS) but is EXCLUDED from the worst-wins fold via OffloadApplies=false,
// so it neither bumps the overall verdict to a spurious PASS nor FAILs a service that
// legitimately has no GPU offload (D-12).
func naOffloadVerdict() inference.Verdict {
	return inference.Verdict{
		Status:     inference.StatusWarn,
		Detail:     "N/A — this service has no GPU offload",
		Provenance: "not an inference service (no llama-server residency to assert)",
	}
}

// PortBinding is one published port and its host bind address (privacy posture).
type PortBinding struct {
	HostAddr      string `json:"host_addr"`
	ContainerPort string `json:"container_port"`
	Loopback      bool   `json:"loopback"`
}

// Report is the aggregated `--json` contract (D-14 — the Phase-5 dashboard
// struct). Its shape is frozen by a golden test, like HostProfile/Recommendation.
// (Moved verbatim from cmd/villa/status.go StatusReport — the Go type name changed
// only; every json tag and field order is preserved byte-for-byte, Pitfall 1.)
type Report struct {
	Services []ServiceStatus `json:"services"`

	// Privacy posture (PRIV-01/PRIV-03), sourced from the generated PublishPort.
	Ports        []PortBinding `json:"ports"`
	LoopbackOnly bool          `json:"loopback_only"`
	NoTelemetry  string        `json:"no_telemetry"`

	// Overall is the aggregated PASS/WARN/FAIL across every service offload + health.
	Overall string `json:"overall"`

	// Backend is the active inference backend's short identifier (e.g. "vulkan"),
	// and Image its digest-pinned container image — sourced from the RESOLVED
	// inference.Backend (BackendFor(cfg.Backend)), never a literal (D-01). This is
	// the single authoritative active-backend surface both `villa status` and the
	// dashboard /api/status read. Appended at the tail (D-06) — nothing above moved.
	Backend string `json:"backend"`
	Image   string `json:"image"`

	// GenTokensPerSec is the live token-generation throughput
	// (llamacpp:predicted_tokens_seconds) for the ACTIVE backend, populated ONLY
	// while the server is generating (metrics.IsGenerating). It is *float64 +
	// omitempty so an idle snapshot or a failed/absent /metrics scrape omits it
	// entirely — a typed-Unknown, NEVER a fabricated 0.0 (D-03 / no-false-green).
	GenTokensPerSec *float64 `json:"gen_tokens_per_sec,omitempty"`

	// ROCmReadiness is the tri-state indicator folded (consumed, never recomputed)
	// from the detect rocm_readiness sub-tree: any unevaluable signal yields
	// "unknown" — never a fabricated "not-ready" (D-04 / no-false-green).
	ROCmReadiness ROCmReadinessIndicator `json:"rocm_readiness"`

	// Usage is the cumulative per-model token totals read (read-only) from the usage
	// store (usage.json) — the Phase-15 USAGE-02 surface (D-09). It is a
	// *usage.UsageTotals + omitempty so an absent/empty store OMITS the key entirely:
	// a typed-Unknown, NEVER a fabricated 0 total. The CLI populates it via a
	// read-only ReadUsage seam (usage.Load only — the CLI never writes the store, D-07);
	// the dashboard (Plan 04) reads the SAME field through handleStatus, no new endpoint
	// (D-10). Tail-appended above SchemaVersion (append-only; nothing above moved).
	Usage *usage.UsageTotals `json:"usage,omitempty"`

	// SchemaVersion is the Report contract self-version (D-07). It MUST stay the
	// LAST tagged field (append-only; new tagged fields go above it, the unexported
	// err stays after it and never serializes).
	SchemaVersion int `json:"schema_version"`

	// err is the unexported load/render error carried out of Run (read via Err()).
	// It has no json tag and is unexported, so encoding/json never serializes it —
	// the frozen --json contract is unchanged (Pitfall 1).
	err error
}

// reportSchemaVersion is the Report contract self-version. Version 1 carried the
// Phase-10 backend-aware tail-append fields (Backend, Image, GenTokensPerSec,
// ROCmReadiness). Version 2 (Phase-15, D-09) tail-appends the cumulative usage
// field (Usage) above SchemaVersion. It is itself a tail-appended additive marker
// (D-07); bumped on any additive change to the Report --json contract.
const reportSchemaVersion = 2

// ROCmReadinessIndicator is the tri-state surfaced from the detect rocm_readiness
// sub-tree. It is a string enum so the --json contract is stable and the dashboard
// badge maps it directly.
type ROCmReadinessIndicator string

const (
	// ROCmReady means every readiness signal is Known-good.
	ROCmReady ROCmReadinessIndicator = "ready"
	// ROCmNotReady means at least one signal is Known-BAD and all others are Known
	// (a confidently-detected blocker — never inferred from an unevaluable signal).
	ROCmNotReady ROCmReadinessIndicator = "not-ready"
	// ROCmUnknown means at least one signal is unevaluable (off-hardware default).
	// Unknown wins over not-ready (no-false-green, D-04/D-08).
	ROCmUnknown ROCmReadinessIndicator = "unknown"
)

// foldROCmReadiness reads (never recomputes) the detect rocm_readiness sub-tree and
// folds it worst-wins with UNKNOWN winning over NOT-READY (no-false-green, D-04/D-08):
// a single unevaluable (Known=false) signal makes the whole indicator "unknown", so
// off-hardware (most fields unset) the honest answer is "unknown", and a
// confidently-bad signal only yields "not-ready" when every other signal is Known.
// It is pure — it reads the passed struct only, performing no I/O and no re-probe.
// Because any !Known short-circuits to "unknown", fold order is irrelevant to
// correctness: unknown can never be masked by a later not-ready.
func foldROCmReadiness(r detect.ROCmReadiness) ROCmReadinessIndicator {
	bools := []detect.Bool{
		r.HSAOverrideViable, r.FirmwareDateOK, r.KernelFloorOK,
		r.RocminfoGfx1151, r.ImagePolicyOK,
	}
	sawBad := false
	for _, b := range bools {
		if !b.Known {
			return ROCmUnknown // any unevaluable signal → unknown (never not-ready)
		}
		if !b.Value {
			sawBad = true
		}
	}
	if sawBad {
		return ROCmNotReady
	}
	return ROCmReady
}

// Deps are the injectable seams Run drives. Defaults wire the real host
// (cmd/villa liveStatusDeps); status_test.go and the dashboard replace them with
// stubs / their own live wiring.
type Deps struct {
	LoadConfig func() (config.VillaConfig, error)
	ModelFile  func(config.VillaConfig) (string, error)
	ModelsDir  func() string
	Render     func(orchestrate.RenderInput) ([]orchestrate.Unit, error)

	IsActive    func(service string) (string, error)
	Health      func(endpoint string) HealthState
	OWUIHealth  func(endpoint string) HealthState
	JournalText func(service string) (string, bool)
	Props       func(endpoint string) *inference.PropsInfo
	GTTUsed     func() detect.Bytes
	WeightBytes func(config.VillaConfig) uint64
	Endpoint    func() string

	// GenTokensPerSec is the live token-generation tok/s seam (D-03), wired in
	// cmd/villa liveStatusDeps to reuse metrics.ScrapeMetrics. It returns nil on an
	// idle server or a failed/absent /metrics scrape so Run omits the figure
	// (typed-Unknown, never a fabricated 0). internal/status stays free of HTTP
	// coupling; status_test.go stubs it like the other seams. A nil seam is treated
	// as "no reading" (Run guards it).
	GenTokensPerSec func(endpoint string) *float64
	// ROCmReadiness is the detect rocm_readiness probe seam (D-04), wired in
	// liveStatusDeps to detect.Probe().ROCmReadiness. internal/status folds the
	// returned sub-tree via foldROCmReadiness; a nil seam leaves the indicator
	// "unknown" (no false-green). status_test.go stubs it to drive the fold.
	ROCmReadiness func() detect.ROCmReadiness

	// ReadUsage is the READ-ONLY cumulative-usage seam (D-07/D-09), wired in
	// liveStatusDeps to a usage.Load over usage.UsagePath(). It returns the loaded
	// *usage.UsageTotals, or nil when the store is absent/empty so Run OMITS the Usage
	// field (typed-Unknown, never a fabricated 0). It MUST never write usage.json — the
	// CLI is one-shot and read-only; the dashboard (Plan 04) is the sole writer (D-07).
	// internal/status stays free of filesystem coupling; status_test.go stubs it. A nil
	// seam is treated as "no reading" (Run guards it, leaving Usage nil).
	ReadUsage func() *usage.UsageTotals

	// OWUIService is the villa-openwebui.service unit name the owui-row branch
	// targets (D-12). It is a Deps field so internal/status need not import the
	// cmd-layer install.go constant (which would create a package-main cycle).
	OWUIService string

	// Dashboard self-row seams (Plan 05-05 / D-04). The control dashboard is a NATIVE
	// systemd --user service (not a Quadlet .container), so it is NOT derived from the
	// rendered units — it is folded as an explicit extra row: its systemd active-state
	// plus a bounded GET to its own /api/healthz. Like the owui row it has no GPU
	// offload, so its offload is the N/A representation EXCLUDED from the worst-wins
	// fold (OffloadApplies=false). DashboardService is empty when the caller does not
	// want a dashboard row (e.g. a legacy caller), in which case Run skips it.
	DashboardService string
	// DashboardAddr is the loopback base URL of the dashboard (e.g.
	// http://127.0.0.1:8888) passed to DashboardHealth. The probe itself is the
	// cmd-layer seam (bounded Timeout + io.LimitReader) so internal/status stays free
	// of HTTP coupling and a wedged dashboard can never hang Run (Pitfall 6).
	DashboardAddr   string
	DashboardHealth func(addr string) HealthState
}

// Errored returns the synthetic active-state token Run records when `systemctl
// is-active` ran but errored with no parseable state (CR-02 tighten). Exposed so
// the cmd-layer table/test code can reference the same constant.
func Errored() string { return activeErrored }

// serviceUnits returns the systemd service names a rendered stack produces. Only
// .container units map to a service (Quadlet villa-llama.container →
// villa-llama.service); .network/.volume units are not services. Moved here (pure)
// so Run no longer depends on the cmd-layer helper.
func serviceUnits(units []orchestrate.Unit) []string {
	var svcs []string
	for _, u := range units {
		if name, ok := strings.CutSuffix(u.Name, ".container"); ok {
			svcs = append(svcs, name+".service")
		}
	}
	return svcs
}

// Run builds the Report from the injected seams (the body of the old runStatus,
// minus printing/exit). It performs no I/O of its own; every host touch is a Deps
// seam. The result is the frozen --json contract the CLI encodes and the dashboard
// serializes.
func Run(d Deps) Report {
	cfg, err := d.LoadConfig()
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	modelFile, err := d.ModelFile(cfg)
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}
	units, err := d.Render(orchestrate.RenderInput{
		Backend:   backend,
		Cfg:       cfg,
		ModelFile: modelFile,
		ModelsDir: d.ModelsDir(),
	})
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	endpoint := d.Endpoint()
	report := Report{
		NoTelemetry: noTelemetryStatement,
		Ports:       publishedPorts(units),
	}
	report.LoopbackOnly = allLoopback(report.Ports)

	// Active-backend identity from the ALREADY-RESOLVED backend (D-01) — the same
	// accessors backendShowEntry uses, never a literal. SC#1 residency correctness is
	// wired below at the RunningOffloadVerdict call (Markers: backend.ResidencyProof());
	// this only surfaces the visible identity.
	report.Backend = backend.Name()
	report.Image = backend.Image()
	report.SchemaVersion = reportSchemaVersion
	// Live tok/s (D-03): typed-optional via the seam — nil on idle/unavailable so it
	// serializes as omitted, never a fabricated 0. Guard a nil seam defensively.
	if d.GenTokensPerSec != nil {
		report.GenTokensPerSec = d.GenTokensPerSec(endpoint)
	}
	// ROCm-readiness tri-state (D-04): fold the detect sub-tree from the seam. A nil
	// seam leaves the indicator "unknown" (no false-green).
	report.ROCmReadiness = ROCmUnknown
	if d.ROCmReadiness != nil {
		report.ROCmReadiness = foldROCmReadiness(d.ROCmReadiness())
	}
	// Cumulative usage (D-09): read-only via the seam. A nil seam OR a nil result
	// (absent/empty store) leaves report.Usage nil so it serializes as omitted —
	// typed-Unknown, never a fabricated 0. The seam never writes usage.json (D-07).
	if d.ReadUsage != nil {
		report.Usage = d.ReadUsage()
	}

	weight := d.WeightBytes(cfg)
	for _, svc := range serviceUnits(units) {
		ss := ServiceStatus{Service: svc}

		active, aerr := d.IsActive(svc)
		switch {
		case aerr == nil:
			ss.Active = active
		case errors.As(aerr, &orchestrate.ErrCommandFailed{}):
			// systemctl ran but the unit/manager errored with no parseable state —
			// indeterminate-but-bad, must drive FAIL not a soft WARN (CR-02 tighten).
			ss.Active = activeErrored
		default:
			// Cannot measure at all (e.g. systemctl missing) — never a false FAIL.
			ss.Active = "unknown"
		}

		if svc == d.OWUIService {
			// Open WebUI row (D-12 / CHAT-01 SC#1): health = reachability + a
			// NON-EMPTY upstream model list. It has no GPU offload, so its offload
			// is the N/A representation that does NOT fold into the overall verdict.
			ss.Health = d.OWUIHealth(endpoint)
			ss.Offload = naOffloadVerdict()
			ss.OffloadApplies = false
			ss.OffloadOK = false
			report.Services = append(report.Services, ss)
			continue
		}

		ss.Health = d.Health(endpoint)

		journal, _ := d.JournalText(svc)
		ss.Offload = inference.RunningOffloadVerdict(inference.RunningOffloadInput{
			JournalText:   journal,
			Props:         d.Props(endpoint),
			GTTUsedBytes:  d.GTTUsed(),
			WeightBytes:   weight,
			ConfigModel:   modelFile,
			ConfigContext: cfg.Ctx,
			Markers:       backend.ResidencyProof(),
			// GPUBusyPercent left Unknown (busy fold skipped) — the live decode-time
			// gpu_busy_percent read lands in Phase 8 (D-07); Phase 6 wires the input.
		})
		ss.OffloadApplies = true
		ss.OffloadOK = ss.Offload.Status == inference.StatusPass
		report.Services = append(report.Services, ss)
	}

	// Dashboard self-row (Plan 05-05 / D-04): the control dashboard is a managed,
	// observable member of the stack, but a NATIVE systemd --user service — not a
	// Quadlet .container — so it is NOT in serviceUnits(units). Fold it as an explicit
	// extra row AFTER the container rows: its systemd active-state plus a bounded
	// /api/healthz probe (the seam is the cmd-layer bounded GET, so a wedged dashboard
	// cannot hang Run — Pitfall 6). Like the owui row it has NO GPU offload, so its
	// offload is the N/A representation EXCLUDED from the worst-wins fold
	// (OffloadApplies=false): it never records a spurious offload PASS nor a false
	// offload FAIL (D-12). Skipped when no DashboardService is configured.
	if d.DashboardService != "" {
		ds := ServiceStatus{Service: d.DashboardService}
		active, aerr := d.IsActive(d.DashboardService)
		switch {
		case aerr == nil:
			ds.Active = active
		case errors.As(aerr, &orchestrate.ErrCommandFailed{}):
			ds.Active = activeErrored
		default:
			ds.Active = "unknown"
		}
		if d.DashboardHealth != nil {
			ds.Health = d.DashboardHealth(d.DashboardAddr)
		} else {
			ds.Health = HealthUnknown
		}
		ds.Offload = naOffloadVerdict()
		ds.OffloadApplies = false
		ds.OffloadOK = false
		report.Services = append(report.Services, ds)
	}

	report.Overall = Aggregate(report).String()
	return report
}

// Err exposes the load/render error Run encountered, if any. Run returns a Report
// with Overall=FAIL on a config/model/render error; the cmd-layer caller checks
// Err to surface the precise message and map to exitBlocked.
func (r Report) Err() error { return r.err }

// Aggregate folds every service's offload Verdict, mapped health, and the
// loopback posture into the worst-wins overall status: any FAIL → FAIL; else any
// WARN → WARN; else PASS. A non-loopback bind (PRIV-01 breach) is a FAIL.
func Aggregate(r Report) inference.Status {
	worst := inference.StatusPass
	bump := func(s inference.Status) {
		if s > worst {
			worst = s
		}
	}
	if !r.LoopbackOnly {
		bump(inference.StatusFail)
	}
	for _, s := range r.Services {
		// Only a real GPU-offload assertion folds into the verdict. A non-GPU service
		// (Open WebUI) carries an N/A offload (OffloadApplies=false) that must neither
		// bump to a spurious PASS nor FAIL a service that legitimately has no offload (D-12).
		if s.OffloadApplies {
			bump(s.Offload.Status)
		}
		bump(HealthStatus(s.Health))
		bump(ActiveStatus(s.Active))
	}
	return worst
}

// ActiveStatus maps a systemctl is-active state to the PASS/WARN/FAIL vocabulary so
// a genuinely down unit drives the overall verdict to FAIL (CR-02). A clean "active"
// is PASS; transient/unknown/empty states are WARN; every terminal-bad state
// (failed, inactive, deactivating) is FAIL.
func ActiveStatus(a string) inference.Status {
	switch a {
	case "active":
		return inference.StatusPass
	case "activating", "reloading", "unknown", "":
		return inference.StatusWarn
	case activeErrored:
		return inference.StatusFail
	default: // failed, inactive, deactivating
		return inference.StatusFail
	}
}

// HealthStatus maps a mapped health state to the PASS/WARN/FAIL vocabulary: ready →
// PASS, loading/unknown → WARN (up-but-not-confirmed, never a confident FAIL —
// WR-07), down → FAIL.
func HealthStatus(h HealthState) inference.Status {
	switch h {
	case HealthReady:
		return inference.StatusPass
	case HealthDown:
		return inference.StatusFail
	default: // loading / unknown
		return inference.StatusWarn
	}
}

// publishedPorts parses the generated container unit(s) for PublishPort= lines (the
// generator-enforced privacy mechanism, D-15) and records each host bind address.
// It deliberately reads ONLY PublishPort= lines — never the Exec= line.
func publishedPorts(units []orchestrate.Unit) []PortBinding {
	var ports []PortBinding
	for _, u := range units {
		for _, line := range strings.Split(u.Text, "\n") {
			line = strings.TrimSpace(line)
			val, ok := strings.CutPrefix(line, "PublishPort=")
			if !ok {
				continue
			}
			ports = append(ports, parsePublishPort(val))
		}
	}
	return ports
}

// parsePublishPort splits a PublishPort value (ADDR:HOSTPORT:CONTAINERPORT, or
// HOSTPORT:CONTAINERPORT with an implicit all-interfaces bind) into a PortBinding.
// A value with no explicit host address is treated as a NON-loopback bind. A
// bracketed IPv6 host address ([::1]:HOSTPORT:CONTAINERPORT) is handled explicitly
// (WR-02) so a `::1` loopback bind is not misread as exposed by a naive colon split.
func parsePublishPort(val string) PortBinding {
	if strings.HasPrefix(val, "[") {
		if end := strings.Index(val, "]"); end > 0 {
			addr := val[1:end]
			rest := strings.TrimPrefix(val[end+1:], ":")
			parts := strings.Split(rest, ":")
			containerPort := ""
			if len(parts) >= 2 {
				containerPort = parts[len(parts)-1]
			}
			return PortBinding{HostAddr: addr, ContainerPort: containerPort, Loopback: isLoopbackAddr(addr)}
		}
		// Malformed bracket — treat conservatively as non-loopback.
		return PortBinding{HostAddr: val, ContainerPort: "", Loopback: false}
	}

	parts := strings.Split(val, ":")
	switch len(parts) {
	case 3:
		// ADDR:HOSTPORT:CONTAINERPORT
		addr := parts[0]
		return PortBinding{HostAddr: addr, ContainerPort: parts[2], Loopback: isLoopbackAddr(addr)}
	case 2:
		// HOSTPORT:CONTAINERPORT — no explicit address ⇒ all-interfaces (not loopback).
		return PortBinding{HostAddr: "0.0.0.0", ContainerPort: parts[1], Loopback: false}
	default:
		return PortBinding{HostAddr: val, ContainerPort: "", Loopback: false}
	}
}

// isLoopbackAddr reports whether a host bind address is the IPv4/IPv6 loopback.
func isLoopbackAddr(addr string) bool {
	return addr == "127.0.0.1" || addr == "::1" || addr == "localhost"
}

// allLoopback reports whether every published port binds loopback (PRIV-01). An
// empty port set is vacuously loopback-only (nothing exposed).
func allLoopback(ports []PortBinding) bool {
	for _, p := range ports {
		if !p.Loopback {
			return false
		}
	}
	return true
}
