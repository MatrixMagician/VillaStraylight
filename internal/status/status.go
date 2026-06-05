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

	// err is the unexported load/render error carried out of Run (read via Err()).
	// It has no json tag and is unexported, so encoding/json never serializes it —
	// the frozen --json contract is unchanged (Pitfall 1).
	err error
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

	units, err := d.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
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
