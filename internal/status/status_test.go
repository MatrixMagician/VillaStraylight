package status

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
)

// status_test.go holds the read-model-level asserts that prove the extracted core
// (Run + Aggregate + the active/health maps + the publish-port parser) behaves the
// same as the old cmd/villa runStatus body — driven entirely through stubbed Deps
// with no live podman/systemd/journald. The byte-frozen `status --json` golden stays
// in cmd/villa (the contract pass criterion lives next to the cobra wiring).

const owuiService = "villa-openwebui.service"

// statusFixtureWeight matches the Vulkan0 residency fixture so the GTT floor clears.
const statusFixtureWeight = 21504 * 1024 * 1024

// loopbackUnits renders the real stack via orchestrate.Render so the privacy
// assertion reflects the actual generated PublishPort=127.0.0.1 mechanism.
func loopbackUnits(t *testing.T) []orchestrate.Unit {
	t.Helper()
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"},
		ModelFile: "qwen3.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	})
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return units
}

// newDeps builds fully-stubbed Deps: a healthy residency journal, a matching /props,
// a GTT reading that clears the floor, and the rendered loopback stack.
func newDeps(t *testing.T, units []orchestrate.Unit) Deps {
	t.Helper()
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return Deps{
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"}, nil
		},
		ModelFile: func(config.VillaConfig) (string, error) { return "qwen3.gguf", nil },
		ModelsDir: func() string { return "/home/villa/.local/share/villa/models" },
		Render:    func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil },
		IsActive:  func(string) (string, error) { return "active", nil },
		Health:    func(string) HealthState { return HealthReady },
		JournalText: func(string) (string, bool) {
			return "load_tensors:      Vulkan0 model buffer size = 21504.49 MiB\n", true
		},
		Props: func(string) *inference.PropsInfo {
			return &inference.PropsInfo{ModelPath: "/models/qwen3.gguf", NCtx: 131072}
		},
		GTTUsed:     func() detect.Bytes { return detect.GTTUsedBytesForTest(drm) },
		WeightBytes: func(config.VillaConfig) uint64 { return statusFixtureWeight },
		Endpoint:    func() string { return "http://127.0.0.1:8080" },
		OWUIHealth:  func(string) HealthState { return HealthReady },
		OWUIService: owuiService,
		// Dashboard self-row (Plan 05-05 / D-04): a healthy /api/healthz probe by
		// default; tests override DashboardHealth to exercise the wedged case.
		DashboardService: dashboardService,
		DashboardAddr:    "http://127.0.0.1:8888",
		DashboardHealth:  func(string) HealthState { return HealthReady },
	}
}

const dashboardService = "villa-dashboard.service"

// dashRow finds the dashboard self-row in a freshly-run report.
func dashRow(t *testing.T, d Deps) ServiceStatus {
	t.Helper()
	for _, s := range Run(d).Services {
		if s.Service == dashboardService {
			return s
		}
	}
	t.Fatalf("status report has no %s row", dashboardService)
	return ServiceStatus{}
}

// TestRunDashboardRow (Plan 05-05 / D-04): Run folds a villa-dashboard.service row
// whose Active is its systemd state and whose Health is the bounded /api/healthz
// probe, with OffloadApplies=false (no GPU offload — never a spurious offload verdict,
// same treatment as the owui row).
func TestRunDashboardRow(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	row := dashRow(t, d)

	if row.Active != "active" {
		t.Errorf("dashboard Active = %q, want active", row.Active)
	}
	if row.Health != HealthReady {
		t.Errorf("dashboard Health = %q, want ready", row.Health)
	}
	if row.OffloadApplies || row.OffloadOK {
		t.Errorf("dashboard row must be offload N/A (applies/ok false), got applies=%v ok=%v",
			row.OffloadApplies, row.OffloadOK)
	}
}

// TestRunDashboardWedged: a wedged/unreachable dashboard (typed-down health) yields a
// down row that folds to FAIL — and because the probe is bounded by the cmd-layer
// seam, status never hangs. Here the seam returns HealthDown directly (the bound is
// asserted in the cmd layer where the real HTTP probe lives).
func TestRunDashboardWedged(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	d.DashboardHealth = func(string) HealthState { return HealthDown }
	row := dashRow(t, d)

	if row.Health != HealthDown {
		t.Fatalf("wedged dashboard Health = %q, want down", row.Health)
	}
	if HealthStatus(row.Health) != inference.StatusFail {
		t.Fatalf("a down dashboard health must fold to FAIL, got %v", HealthStatus(row.Health))
	}
}

// TestRunDashboardUnknownActive: when IsActive cannot be measured the dashboard row
// degrades to a typed-unknown active state (WARN), never a false FAIL.
func TestRunDashboardUnknownActive(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	d.IsActive = func(svc string) (string, error) {
		if svc == dashboardService {
			return "", orchestrate.ErrToolNotFound{Tool: "systemctl"}
		}
		return "active", nil
	}
	row := dashRow(t, d)
	if row.Active != "unknown" {
		t.Errorf("unmeasurable dashboard Active = %q, want unknown (typed, never false FAIL)", row.Active)
	}
}

// TestRunProducesReport drives Run with the healthy stub set and asserts the Report
// shape: the no-telemetry statement, loopback-only true, an inference row with a
// proven offload PASS, and an owui row with N/A offload — the same fold the old
// runStatus body produced.
func TestRunProducesReport(t *testing.T) {
	r := Run(newDeps(t, loopbackUnits(t)))
	if r.Err() != nil {
		t.Fatalf("Run err: %v", r.Err())
	}
	if r.NoTelemetry != noTelemetryStatement {
		t.Errorf("NoTelemetry = %q, want %q", r.NoTelemetry, noTelemetryStatement)
	}
	if !r.LoopbackOnly {
		t.Errorf("loopback stack must be LoopbackOnly=true; ports=%v", r.Ports)
	}
	if r.Overall != inference.StatusPass.String() {
		t.Errorf("Overall = %q, want PASS", r.Overall)
	}

	var inf, owui ServiceStatus
	for _, s := range r.Services {
		switch s.Service {
		case "villa-llama.service":
			inf = s
		case owuiService:
			owui = s
		}
	}
	if !inf.OffloadApplies || inf.Offload.Status != inference.StatusPass {
		t.Errorf("inference row must carry a proven offload PASS, got applies=%v status=%v",
			inf.OffloadApplies, inf.Offload.Status)
	}
	if owui.OffloadApplies || owui.OffloadOK {
		t.Errorf("owui row must be offload N/A (OffloadApplies/OffloadOK false), got applies=%v ok=%v",
			owui.OffloadApplies, owui.OffloadOK)
	}
}

// TestAggregateWorstWins exercises the worst-wins fold (PASS excludes N/A-offload):
// a healthy stack is PASS, a 503 health WARNs, a CPU-only journal FAILs, and a
// non-loopback publish FAILs.
func TestAggregateWorstWins(t *testing.T) {
	units := loopbackUnits(t)

	t.Run("all PASS", func(t *testing.T) {
		if got := Aggregate(Run(newDeps(t, units))); got != inference.StatusPass {
			t.Fatalf("Aggregate = %v, want PASS", got)
		}
	})

	t.Run("503 health → WARN", func(t *testing.T) {
		d := newDeps(t, units)
		d.Health = func(string) HealthState { return HealthLoading }
		if got := Aggregate(Run(d)); got != inference.StatusWarn {
			t.Fatalf("Aggregate = %v, want WARN", got)
		}
	})

	t.Run("CPU-only journal → FAIL", func(t *testing.T) {
		d := newDeps(t, units)
		d.JournalText = func(string) (string, bool) {
			return "load_tensors:   CPU_Mapped model buffer size = 21819.81 MiB\n", true
		}
		if got := Aggregate(Run(d)); got != inference.StatusFail {
			t.Fatalf("Aggregate = %v, want FAIL", got)
		}
	})

	t.Run("non-loopback publish → FAIL", func(t *testing.T) {
		exposed := []orchestrate.Unit{
			{Name: "villa-llama.container", Text: "[Container]\nPublishPort=0.0.0.0:8080:8080\n"},
		}
		d := newDeps(t, exposed)
		if got := Aggregate(Run(d)); got != inference.StatusFail {
			t.Fatalf("Aggregate = %v, want FAIL on a 0.0.0.0 publish", got)
		}
	})
}

// TestActiveStatusMap proves the systemctl is-active → PASS/WARN/FAIL mapping
// (CR-02): active→PASS; transient/ambiguous/empty→WARN; terminal-bad/errored→FAIL.
func TestActiveStatusMap(t *testing.T) {
	cases := map[string]inference.Status{
		"active":       inference.StatusPass,
		"activating":   inference.StatusWarn,
		"reloading":    inference.StatusWarn,
		"unknown":      inference.StatusWarn,
		"":             inference.StatusWarn,
		"failed":       inference.StatusFail,
		"inactive":     inference.StatusFail,
		"deactivating": inference.StatusFail,
		activeErrored:  inference.StatusFail,
	}
	for state, want := range cases {
		if got := ActiveStatus(state); got != want {
			t.Errorf("ActiveStatus(%q) = %v, want %v", state, got, want)
		}
	}
}

// TestParsePublishPortIPv6 verifies a bracketed IPv6 loopback bind is classified as
// loopback rather than misread as exposed (WR-02).
func TestParsePublishPortIPv6(t *testing.T) {
	cases := []struct {
		val          string
		wantAddr     string
		wantPort     string
		wantLoopback bool
	}{
		{"[::1]:8080:8080", "::1", "8080", true},
		{"[::]:8080:8080", "::", "8080", false},
		{"127.0.0.1:8080:8080", "127.0.0.1", "8080", true},
		{"8080:8080", "0.0.0.0", "8080", false},
		{"[2001:db8::1]:8080:8080", "2001:db8::1", "8080", false},
	}
	for _, c := range cases {
		got := parsePublishPort(c.val)
		if got.HostAddr != c.wantAddr || got.ContainerPort != c.wantPort || got.Loopback != c.wantLoopback {
			t.Errorf("parsePublishPort(%q) = {%q, %q, %v}, want {%q, %q, %v}",
				c.val, got.HostAddr, got.ContainerPort, got.Loopback, c.wantAddr, c.wantPort, c.wantLoopback)
		}
	}
}

// TestReadinessFold proves the tri-state fold of the detect rocm_readiness sub-tree
// honors no-false-green (D-04/D-08): any unevaluable (Unknown) signal short-circuits
// to "unknown" (never a fabricated "not-ready"); "not-ready" is reported ONLY when
// every signal is Known and at least one is Known-false; "ready" only when every
// signal is Known-true. Off-hardware (all-unset) the honest answer is "unknown".
func TestReadinessFold(t *testing.T) {
	good := detect.KnownBool(true, "test")
	bad := detect.KnownBool(false, "test")
	unset := detect.UnknownBool("off-hardware", "")

	allUnset := detect.ROCmReadiness{
		HSAOverrideViable: unset, FirmwareDateOK: unset, KernelFloorOK: unset,
		RocminfoGfx1151: unset, ImagePolicyOK: unset,
	}
	allGood := detect.ROCmReadiness{
		HSAOverrideViable: good, FirmwareDateOK: good, KernelFloorOK: good,
		RocminfoGfx1151: good, ImagePolicyOK: good,
	}
	oneKnownBad := detect.ROCmReadiness{
		HSAOverrideViable: good, FirmwareDateOK: bad, KernelFloorOK: good,
		RocminfoGfx1151: good, ImagePolicyOK: good,
	}
	// A confidently-bad signal mixed with an unevaluable one must NOT be not-ready:
	// unknown wins over not-ready (no-false-green).
	badButAlsoUnknown := detect.ROCmReadiness{
		HSAOverrideViable: bad, FirmwareDateOK: unset, KernelFloorOK: good,
		RocminfoGfx1151: good, ImagePolicyOK: good,
	}

	cases := []struct {
		name string
		in   detect.ROCmReadiness
		want ROCmReadinessIndicator
	}{
		{"all-unset (off-hardware) → unknown", allUnset, ROCmUnknown},
		{"all-Known-good → ready", allGood, ROCmReady},
		{"one-Known-bad (rest Known) → not-ready", oneKnownBad, ROCmNotReady},
		{"any-unknown wins over not-ready → unknown", badButAlsoUnknown, ROCmUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := foldROCmReadiness(c.in); got != c.want {
				t.Errorf("foldROCmReadiness = %q, want %q", got, c.want)
			}
		})
	}
}

// TestRunPopulatesBackendAwareFields proves Run sources the active backend identity
// from the RESOLVED backend (never a literal), folds the readiness seam, and stamps
// the schema version. With the default vulkan config the backend/image come from
// inference.BackendFor("vulkan").
func TestRunPopulatesBackendAwareFields(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	want, err := inference.BackendFor("vulkan")
	if err != nil {
		t.Fatalf("resolve backend: %v", err)
	}
	r := Run(d)
	if r.Backend != want.Name() {
		t.Errorf("Report.Backend = %q, want %q (from resolved backend)", r.Backend, want.Name())
	}
	if r.Image != want.Image() {
		t.Errorf("Report.Image = %q, want %q (from resolved backend)", r.Image, want.Image())
	}
	if r.SchemaVersion != reportSchemaVersion {
		t.Errorf("Report.SchemaVersion = %d, want %d", r.SchemaVersion, reportSchemaVersion)
	}
	// The default stub leaves GenTokensPerSec/ROCmReadiness seams nil → typed-Unknown:
	// tok/s omitted (nil), readiness "unknown" (never a fabricated 0 / not-ready).
	if r.GenTokensPerSec != nil {
		t.Errorf("GenTokensPerSec = %v, want nil (no tok/s seam → omitted)", *r.GenTokensPerSec)
	}
	if r.ROCmReadiness != ROCmUnknown {
		t.Errorf("ROCmReadiness = %q, want %q (no readiness seam → unknown)", r.ROCmReadiness, ROCmUnknown)
	}
}

// rocmUnits renders the stack with the resolved ROCm backend so the report reflects
// a rocm-configured install (cfg.Backend="rocm").
func rocmUnits(t *testing.T) []orchestrate.Unit {
	t.Helper()
	backend, err := inference.BackendFor("rocm")
	if err != nil {
		t.Fatalf("resolve rocm backend: %v", err)
	}
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   backend,
		Cfg:       config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "rocm"},
		ModelFile: "qwen3.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	})
	if err != nil {
		t.Fatalf("render rocm: %v", err)
	}
	return units
}

// TestRunROCmResidencyKeysOnResolvedMarkers is the SC#1 correctness PROOF (DASH-06),
// exercisable off-hardware: on a rocm-configured install the offload/residency verdict
// must key on the RESOLVED backend's markers (backendROCm.ResidencyProof() →
// DeviceToken "ROCm0"), NOT a hardcoded Vulkan default. The fixture journal carries a
// ROCm0 buffer line and NO Vulkan0 line; a verdict that proves residency PASS confirms
// the markers came from the resolved ROCm backend. A Vulkan-default would not match the
// ROCm0 token and could not reach PASS — so this asserts the wiring is backend-correct.
// (The seam grep gate excludes _test.go, so the ROCm0 token may appear here.)
func TestRunROCmResidencyKeysOnResolvedMarkers(t *testing.T) {
	d := newDeps(t, rocmUnits(t))
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "rocm"}, nil
	}
	// A ROCm0 residency journal with NO Vulkan0 line: only a verdict keyed on the
	// resolved ROCm backend's ROCm0 marker can prove residency from this.
	d.JournalText = func(string) (string, bool) {
		return "load_tensors:      ROCm0 model buffer size = 21504.49 MiB\n", true
	}

	r := Run(d)
	if r.Err() != nil {
		t.Fatalf("Run err: %v", r.Err())
	}
	if r.Backend != "rocm" {
		t.Fatalf("Report.Backend = %q, want rocm (resolved backend, SC#1)", r.Backend)
	}

	var inf ServiceStatus
	for _, s := range r.Services {
		if s.Service == "villa-llama.service" {
			inf = s
		}
	}
	if !inf.OffloadApplies {
		t.Fatalf("inference row must assert offload on a rocm install")
	}
	if inf.Offload.Status != inference.StatusPass {
		t.Fatalf("rocm-config residency must PASS keying on the resolved ROCm0 markers "+
			"(a Vulkan default could not match ROCm0); got status=%v detail=%q",
			inf.Offload.Status, inf.Offload.Detail)
	}
}

// TestUsageOmittedWhenAbsent proves the typed-Unknown discipline for the cumulative
// usage field (D-09): when the ReadUsage seam is nil (default stub) OR returns nil
// (absent/empty store), Run leaves Report.Usage nil and the marshaled --json OMITS
// the "usage" key entirely — never a fabricated 0 total. Schema is still 2.
func TestUsageOmittedWhenAbsent(t *testing.T) {
	t.Run("nil seam → usage omitted", func(t *testing.T) {
		d := newDeps(t, loopbackUnits(t)) // newDeps sets no ReadUsage seam
		r := Run(d)
		if r.Usage != nil {
			t.Fatalf("nil ReadUsage seam must leave Usage nil, got %+v", r.Usage)
		}
		assertUsageKeyAbsent(t, r)
		if r.SchemaVersion != reportSchemaVersion {
			t.Errorf("SchemaVersion = %d, want %d", r.SchemaVersion, reportSchemaVersion)
		}
	})

	t.Run("seam returns nil (empty store) → usage omitted", func(t *testing.T) {
		d := newDeps(t, loopbackUnits(t))
		d.ReadUsage = func() *usage.UsageTotals { return nil }
		r := Run(d)
		if r.Usage != nil {
			t.Fatalf("a nil-returning ReadUsage seam must leave Usage nil, got %+v", r.Usage)
		}
		assertUsageKeyAbsent(t, r)
	})
}

// TestUsageSurfacedWhenPresent proves a populated read-only ReadUsage seam surfaces
// the cumulative totals on the Report and the --json carries the "usage" key plus
// schema_version 2 (D-09).
func TestUsageSurfacedWhenPresent(t *testing.T) {
	want := &usage.UsageTotals{
		SchemaVersion: 1,
		Models: map[string]usage.ModelUsage{
			"qwen3": {
				Model:     "qwen3",
				Prompt:    usage.CounterState{Cumulative: 1234},
				Predicted: usage.CounterState{Cumulative: 5678},
			},
		},
	}
	d := newDeps(t, loopbackUnits(t))
	d.ReadUsage = func() *usage.UsageTotals { return want }

	r := Run(d)
	if r.Usage == nil {
		t.Fatalf("populated ReadUsage seam must surface Usage on the Report")
	}
	if got := r.Usage.Models["qwen3"].Predicted.Cumulative; got != 5678 {
		t.Errorf("Usage qwen3 generated cumulative = %d, want 5678", got)
	}

	blob, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	s := string(blob)
	if !strings.Contains(s, `"usage"`) {
		t.Errorf("populated --json must carry the usage key; got:\n%s", s)
	}
	if !strings.Contains(s, `"schema_version":2`) {
		t.Errorf("--json must carry schema_version 2; got:\n%s", s)
	}
}

// assertUsageKeyAbsent marshals a Report and fails if the omitempty "usage" key leaked.
func assertUsageKeyAbsent(t *testing.T, r Report) {
	t.Helper()
	blob, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal report: %v", err)
	}
	if strings.Contains(string(blob), `"usage"`) {
		t.Errorf("absent usage store must OMIT the usage key (omitempty); got:\n%s", blob)
	}
}

// TestRunErrPropagates: a LoadConfig failure yields a FAIL Report carrying the error
// via Err() (the cmd layer maps that to exitBlocked).
func TestRunErrPropagates(t *testing.T) {
	d := newDeps(t, loopbackUnits(t))
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{}, os.ErrPermission
	}
	r := Run(d)
	if r.Err() == nil {
		t.Fatalf("Run must carry the LoadConfig error via Err()")
	}
	if r.Overall != inference.StatusFail.String() {
		t.Errorf("Overall on load error = %q, want FAIL", r.Overall)
	}
}
