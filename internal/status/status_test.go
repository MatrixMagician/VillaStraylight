package status

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
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
