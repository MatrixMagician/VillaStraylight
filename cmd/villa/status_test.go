package main

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// status_test.go drives the thin `villa status` cobra caller through a stubbed
// status.Deps: a frozen --json golden (the Phase-5 contract — must stay green with
// ZERO fixture edits after the internal/status extraction), the loopback/no-telemetry
// privacy assertion, the human-table N/A-offload rendering, and the exit-code mapping
// (all-PASS→0, any-WARN→2, any-FAIL→1; 503→loading→WARN not FAIL). The read-model unit
// asserts (Aggregate/ActiveStatus/parsePublishPort) live in internal/status.

// statusFixtureWeight matches the Vulkan0 residency fixture so the GTT floor clears.
const statusFixtureWeight = 21504 * 1024 * 1024

// loopbackUnits renders the real stack via orchestrate.Render so the golden + the
// privacy assertion reflect the actual generated PublishPort=127.0.0.1 mechanism.
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

// newStatusDeps builds a fully-stubbed status.Deps: a healthy residency journal, a
// matching /props, a GTT reading that clears the floor, and the rendered loopback
// stack. Knobs override the health probe / journal / props per test.
func newStatusDeps(t *testing.T, units []orchestrate.Unit) *status.Deps {
	t.Helper()
	drm := t.TempDir()
	if err := os.WriteFile(filepath.Join(drm, "mem_info_gtt_used"), []byte("23068672000\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	return &status.Deps{
		LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{Model: "qwen3", Quant: "Q4", Ctx: 131072, Backend: "vulkan"}, nil
		},
		ModelFile: func(config.VillaConfig) (string, error) { return "qwen3.gguf", nil },
		ModelsDir: func() string { return "/home/villa/.local/share/villa/models" },
		Render:    func(orchestrate.RenderInput) ([]orchestrate.Unit, error) { return units, nil },
		IsActive:  func(string) (string, error) { return "active", nil },
		Health:    func(string) status.HealthState { return status.HealthReady },
		JournalText: func(string) (string, bool) {
			return "load_tensors:      Vulkan0 model buffer size = 21504.49 MiB\n", true
		},
		Props: func(string) *inference.PropsInfo {
			return &inference.PropsInfo{ModelPath: "/models/qwen3.gguf", NCtx: 131072}
		},
		GTTUsed:     func() detect.Bytes { return detect.GTTUsedBytesForTest(drm) },
		WeightBytes: func(config.VillaConfig) uint64 { return statusFixtureWeight },
		Endpoint:    func() string { return "http://127.0.0.1:8080" },
		// Default owui probe: upstream /v1/models is non-empty → HealthReady. Tests
		// override this knob to exercise the empty-list / unreachable branches.
		OWUIHealth:  func(string) status.HealthState { return status.HealthReady },
		OWUIService: openWebUIServiceName,
		// Dashboard self-row (Plan 05-05 / D-04): a healthy /api/healthz by default;
		// tests override DashboardHealth for the wedged case.
		DashboardService: orchestrate.DashboardServiceName,
		DashboardAddr:    "http://127.0.0.1:8888",
		DashboardHealth:  func(string) status.HealthState { return status.HealthReady },
		// tok/s seam (D-03): default idle → nil (omitted, never a fabricated 0). Tests
		// override to exercise the generating case. ROCm-readiness seam (D-04): default
		// all-unset → folds to "unknown" (off-hardware honest default).
		GenTokensPerSec: func(string) *float64 { return nil },
		ROCmReadiness:   func() detect.ROCmReadiness { return detect.ROCmReadiness{} },
	}
}

// owuiRow finds the villa-openwebui.service row in a freshly-run report.
func owuiRow(t *testing.T, d *status.Deps) status.ServiceStatus {
	t.Helper()
	report := runStatusReport(t, d)
	for _, s := range report.Services {
		if s.Service == openWebUIServiceName {
			return s
		}
	}
	t.Fatalf("status report has no %s row; services=%v", openWebUIServiceName, report.Services)
	return status.ServiceStatus{}
}

// runStatusReport runs status in --json mode and decodes the Report so a test can
// assert on the structured per-service rows (not just the exit code).
func runStatusReport(t *testing.T, d *status.Deps) status.Report {
	t.Helper()
	cmd, out, _ := statusTestCmd()
	jsonOut = true
	defer func() { jsonOut = false }()
	runStatus(cmd, nil, d)
	var r status.Report
	if err := json.Unmarshal(out.Bytes(), &r); err != nil {
		t.Fatalf("decode status report: %v\n%s", err, out.String())
	}
	return r
}

// TestStatusOpenWebUIHealthProbe exercises the Open WebUI row health branch
// (D-12 / CHAT-01 SC#1): a non-empty upstream /v1/models → HealthReady → PASS; an
// empty list → HealthLoading → WARN (never PASS); a transport error/unreachable →
// typed-Unknown → WARN (never a false PASS, never an over-eager FAIL).
func TestStatusOpenWebUIHealthProbe(t *testing.T) {
	units := loopbackUnits(t)

	t.Run("non-empty /v1/models → owui Health ready (PASS)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.OWUIHealth = func(string) status.HealthState { return status.HealthReady }
		row := owuiRow(t, d)
		if row.Health != status.HealthReady {
			t.Fatalf("owui Health = %q, want %q", row.Health, status.HealthReady)
		}
		if status.HealthStatus(row.Health) != inference.StatusPass {
			t.Fatalf("non-empty model list must fold to PASS for owui health")
		}
	})

	t.Run("empty /v1/models → owui Health loading (WARN, never PASS)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.OWUIHealth = func(string) status.HealthState { return status.HealthLoading }
		row := owuiRow(t, d)
		if row.Health == status.HealthReady {
			t.Fatalf("empty model list must NOT be HealthReady (no false PASS)")
		}
		if status.HealthStatus(row.Health) != inference.StatusWarn {
			t.Fatalf("empty model list must fold to WARN, got %v", status.HealthStatus(row.Health))
		}
	})

	t.Run("unreachable owui → typed-Unknown → WARN (never false PASS, never over-eager FAIL)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.OWUIHealth = func(string) status.HealthState { return status.HealthUnknown }
		row := owuiRow(t, d)
		if row.Health == status.HealthReady {
			t.Fatalf("unreachable owui must NOT be HealthReady (no false PASS)")
		}
		if status.HealthStatus(row.Health) != inference.StatusWarn {
			t.Fatalf("unreachable owui must fold to WARN (typed-Unknown), not FAIL; got %v", status.HealthStatus(row.Health))
		}
	})
}

// TestStatusOpenWebUINoFalseOffloadPASS proves the owui row carries no inference
// offload PASS: it has no GPU offload, so its offload must not be applicable and must
// not bump the overall verdict to a spurious PASS (D-12).
func TestStatusOpenWebUINoFalseOffloadPASS(t *testing.T) {
	d := newStatusDeps(t, loopbackUnits(t))
	row := owuiRow(t, d)

	if row.OffloadApplies {
		t.Fatalf("owui row must mark offload N/A (OffloadApplies=false); it has no GPU offload")
	}
	if row.OffloadOK {
		t.Fatalf("owui row must NOT report offload_ok=true (no false offload PASS)")
	}
	if row.Offload.Status == inference.StatusPass {
		t.Fatalf("owui Offload.Status must not be a (false) PASS, got %v", row.Offload.Status)
	}

	report := runStatusReport(t, d)
	var infRow status.ServiceStatus
	for _, s := range report.Services {
		if s.Service == "villa-llama.service" {
			infRow = s
		}
	}
	if !infRow.OffloadApplies || infRow.Offload.Status != inference.StatusPass {
		t.Fatalf("inference row must keep its real offload PASS, got applies=%v status=%v",
			infRow.OffloadApplies, infRow.Offload.Status)
	}
}

// TestStatusOpenWebUIActiveFoldsToFail proves CR-02 is intact for the new row: a
// confidently-down owui unit (active=failed) drives the overall verdict to FAIL.
func TestStatusOpenWebUIActiveFoldsToFail(t *testing.T) {
	d := newStatusDeps(t, loopbackUnits(t))
	d.IsActive = func(svc string) (string, error) {
		if svc == openWebUIServiceName {
			return "failed", nil
		}
		return "active", nil
	}
	cmd, _, _ := statusTestCmd()
	if code := runStatus(cmd, nil, d); code != exitBlocked {
		t.Fatalf("a confidently-down owui unit must drive overall FAIL (CR-02), got exit %d", code)
	}
}

// floatPtr is a test helper for the typed-optional tok/s seam.
func floatPtr(v float64) *float64 { return &v }

// TestStatusTokensPerSecTypedOptional proves the live tok/s surfaces honestly (D-03):
// generating → a value rendered in the table AND labeled by the active backend; idle →
// omitted (seam returns nil); scrape-unavailable → omitted. NEVER a fabricated 0.
func TestStatusTokensPerSecTypedOptional(t *testing.T) {
	units := loopbackUnits(t)

	t.Run("generating → value rendered + labeled by backend", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.GenTokensPerSec = func(string) *float64 { return floatPtr(12.3) }
		report := runStatusReport(t, d)
		if report.GenTokensPerSec == nil {
			t.Fatalf("generating server must surface a tok/s reading (got nil)")
		}
		if *report.GenTokensPerSec != 12.3 {
			t.Errorf("GenTokensPerSec = %v, want 12.3", *report.GenTokensPerSec)
		}

		var buf bytes.Buffer
		renderStatusTable(&buf, report, false)
		got := buf.String()
		if !strings.Contains(got, "12.3") {
			t.Errorf("table must render the tok/s value; got:\n%s", got)
		}
		// Labeled by the active backend (vulkan in the fixture).
		if !strings.Contains(got, report.Backend) {
			t.Errorf("tok/s row must be labeled by the active backend %q; got:\n%s", report.Backend, got)
		}
	})

	t.Run("idle → omitted (never a fabricated 0)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.GenTokensPerSec = func(string) *float64 { return nil }
		report := runStatusReport(t, d)
		if report.GenTokensPerSec != nil {
			t.Fatalf("idle server must omit tok/s (typed-Unknown), got %v", *report.GenTokensPerSec)
		}

		var buf bytes.Buffer
		renderStatusTable(&buf, report, false)
		if strings.Contains(buf.String(), "tok/s") {
			t.Errorf("idle table must NOT render a tok/s row (never a fabricated 0); got:\n%s", buf.String())
		}
		// And the --json must not carry the key at all (omitempty).
		cmd, out, _ := statusTestCmd()
		jsonOut = true
		defer func() { jsonOut = false }()
		runStatus(cmd, nil, d)
		if strings.Contains(out.String(), "gen_tokens_per_sec") {
			t.Errorf("idle --json must omit gen_tokens_per_sec (omitempty); got:\n%s", out.String())
		}
	})

	t.Run("scrape unavailable → omitted", func(t *testing.T) {
		d := newStatusDeps(t, units)
		// An unavailable /metrics scrape is modeled the same as idle by the seam: nil.
		d.GenTokensPerSec = func(string) *float64 { return nil }
		report := runStatusReport(t, d)
		if report.GenTokensPerSec != nil {
			t.Fatalf("unavailable scrape must omit tok/s, got %v", *report.GenTokensPerSec)
		}
	})
}

func statusTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// TestStatusJSONGolden freezes the Report --json contract byte-for-byte. This is the
// JSON-neutral-move pass criterion (Pitfall 1): the golden MUST stay green with ZERO
// fixture edits after the internal/status extraction. Run with -update to regenerate.
func TestStatusJSONGolden(t *testing.T) {
	d := newStatusDeps(t, loopbackUnits(t))
	cmd, out, _ := statusTestCmd()

	jsonOut = true
	defer func() { jsonOut = false }()

	code := runStatus(cmd, nil, d)
	if code != exitPass {
		t.Fatalf("all-PASS status: exit = %d, want %d (out: %s)", code, exitPass, out.String())
	}

	golden := filepath.Join("testdata", "status.json.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, out.Bytes(), 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(out.Bytes(), want) {
		t.Errorf("status --json does not match golden.\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
	}
}

// TestStatusTableOffloadNAForNonGPUService proves the human status table renders the
// OFFLOAD column as "N/A" for a non-GPU service (OffloadApplies=false, e.g. Open WebUI)
// rather than leaking the underlying WARN-typed N/A Verdict, while a real GPU service
// still shows its offload verdict (D-12). The --json contract is unaffected.
func TestStatusTableOffloadNAForNonGPUService(t *testing.T) {
	report := status.Report{
		Services: []status.ServiceStatus{
			{
				Service:        "villa-llama.service",
				Active:         "active",
				Health:         status.HealthReady,
				Offload:        inference.Verdict{Status: inference.StatusPass, Detail: "offload proven"},
				OffloadApplies: true,
				OffloadOK:      true,
			},
			{
				Service:        "villa-openwebui.service",
				Active:         "active",
				Health:         status.HealthReady,
				Offload:        inference.Verdict{Status: inference.StatusWarn, Detail: "N/A — this service has no GPU offload"},
				OffloadApplies: false,
			},
		},
		LoopbackOnly: true,
		NoTelemetry:  "no telemetry; outbound = image/model pulls only",
		Overall:      "PASS",
	}

	var buf bytes.Buffer
	renderStatusTable(&buf, report, false)

	rowField := func(service string) string {
		t.Helper()
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.HasPrefix(line, service) {
				return line
			}
		}
		t.Fatalf("no table row for %s in:\n%s", service, buf.String())
		return ""
	}

	owui := rowField("villa-openwebui.service")
	if !strings.Contains(owui, "N/A") {
		t.Errorf("owui row OFFLOAD must render N/A (OffloadApplies=false); got: %q", owui)
	}
	if strings.Contains(owui, "WARN") {
		t.Errorf("owui row must NOT leak the WARN-typed N/A verdict into the OFFLOAD column; got: %q", owui)
	}

	llama := rowField("villa-llama.service")
	if !strings.Contains(llama, "PASS") {
		t.Errorf("inference row OFFLOAD must show its real verdict (PASS); got: %q", llama)
	}
	if strings.Contains(llama, "N/A") {
		t.Errorf("a real GPU-offload service must NOT render N/A; got: %q", llama)
	}
}

// TestStatusDashboardRow (Plan 05-05 / D-04): `villa status` emits a
// villa-dashboard.service row with its active-state + the /api/healthz-derived health
// and OffloadApplies=false (the human table renders its OFFLOAD as N/A, never a
// spurious offload verdict). A wedged/unreachable dashboard → a typed-down row that
// does not hang status.
func TestStatusDashboardRow(t *testing.T) {
	units := loopbackUnits(t)

	t.Run("healthy dashboard → row present, ready, offload N/A", func(t *testing.T) {
		d := newStatusDeps(t, units)
		report := runStatusReport(t, d)
		var row status.ServiceStatus
		found := false
		for _, s := range report.Services {
			if s.Service == orchestrate.DashboardServiceName {
				row, found = s, true
			}
		}
		if !found {
			t.Fatalf("status report has no %s row; services=%v", orchestrate.DashboardServiceName, report.Services)
		}
		if row.Health != status.HealthReady {
			t.Errorf("dashboard Health = %q, want ready", row.Health)
		}
		if row.OffloadApplies {
			t.Errorf("dashboard row must be offload N/A (OffloadApplies=false)")
		}

		// The human table renders the dashboard row with an N/A offload cell.
		var buf bytes.Buffer
		renderStatusTable(&buf, report, false)
		var dashLine string
		for _, line := range strings.Split(buf.String(), "\n") {
			if strings.HasPrefix(line, orchestrate.DashboardServiceName) {
				dashLine = line
			}
		}
		if dashLine == "" {
			t.Fatalf("no table row for %s in:\n%s", orchestrate.DashboardServiceName, buf.String())
		}
		if !strings.Contains(dashLine, "N/A") {
			t.Errorf("dashboard row OFFLOAD must render N/A; got %q", dashLine)
		}
	})

	t.Run("wedged dashboard → typed-down (never a confident PASS), status does not hang", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.DashboardHealth = func(string) status.HealthState { return status.HealthDown }
		report := runStatusReport(t, d)
		var row status.ServiceStatus
		for _, s := range report.Services {
			if s.Service == orchestrate.DashboardServiceName {
				row = s
			}
		}
		if row.Health != status.HealthDown {
			t.Fatalf("wedged dashboard Health = %q, want down", row.Health)
		}
		// A down dashboard health folds to FAIL → exit 1.
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("wedged dashboard exit = %d, want %d (down → FAIL)", code, exitBlocked)
		}
	})
}

// TestStatusPrivacyAssertion: the report carries the no-telemetry statement and a
// loopback-only=true assertion for the real (loopback) stack; a synthetic 0.0.0.0
// publish flips loopback-only to false AND the overall verdict to FAIL.
func TestStatusPrivacyAssertion(t *testing.T) {
	d := newStatusDeps(t, loopbackUnits(t))
	cmd, out, _ := statusTestCmd()
	if code := runStatus(cmd, nil, d); code != exitPass {
		t.Fatalf("loopback stack exit = %d, want %d", code, exitPass)
	}
	got := out.String()
	if !strings.Contains(got, "no telemetry; outbound = image/model pulls only") {
		t.Errorf("report must contain the no-telemetry statement, got:\n%s", got)
	}
	if !strings.Contains(got, "loopback-only") || !strings.Contains(got, "true") {
		t.Errorf("report must assert loopback-only=true, got:\n%s", got)
	}

	exposed := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\nPublishPort=0.0.0.0:8080:8080\nExec=llama-server --host 0.0.0.0\n"},
	}
	d2 := newStatusDeps(t, exposed)
	cmd2, out2, _ := statusTestCmd()
	code := runStatus(cmd2, nil, d2)
	if code != exitBlocked {
		t.Fatalf("exposed (0.0.0.0) publish exit = %d, want %d (FAIL); out:\n%s", code, exitBlocked, out2.String())
	}
	if !strings.Contains(out2.String(), "false") {
		t.Errorf("exposed publish must report loopback-only=false, got:\n%s", out2.String())
	}
}

// TestStatusExitCodes exercises the worst-wins exit mapping: all-PASS→0, a WARN
// signal→2, a FAIL signal→1, and that a 503 /health maps to loading (WARN), never a
// confident FAIL.
func TestStatusExitCodes(t *testing.T) {
	units := loopbackUnits(t)

	t.Run("all PASS → 0", func(t *testing.T) {
		d := newStatusDeps(t, units)
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitPass {
			t.Fatalf("exit = %d, want %d", code, exitPass)
		}
	})

	t.Run("503 health → loading → WARN (2), not FAIL", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.Health = func(string) status.HealthState { return status.HealthLoading }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitWarn {
			t.Fatalf("503 loading exit = %d, want %d (WARN, not a confident FAIL)", code, exitWarn)
		}
	})

	t.Run("empty journal → offload WARN → 2", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.JournalText = func(string) (string, bool) { return "", false }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitWarn {
			t.Fatalf("empty-journal exit = %d, want %d (offload unverifiable → WARN)", code, exitWarn)
		}
	})

	t.Run("CPU-only journal → offload FAIL → 1", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.JournalText = func(string) (string, bool) {
			return "load_tensors:   CPU_Mapped model buffer size = 21819.81 MiB\n", true
		}
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("CPU-only journal exit = %d, want %d (offload FAIL)", code, exitBlocked)
		}
	})

	t.Run("health down → FAIL → 1", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.Health = func(string) status.HealthState { return status.HealthDown }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("health-down exit = %d, want %d (FAIL)", code, exitBlocked)
		}
	})

	t.Run("failed unit → active FAIL → 1 (CR-02)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.IsActive = func(string) (string, error) { return "failed", nil }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("failed-unit exit = %d, want %d (active FAIL must fold into overall)", code, exitBlocked)
		}
	})

	t.Run("inactive unit → active FAIL → 1 (CR-02)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.IsActive = func(string) (string, error) { return "inactive", nil }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("inactive-unit exit = %d, want %d (active FAIL)", code, exitBlocked)
		}
	})

	t.Run("activating unit → active WARN → 2 (CR-02)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.IsActive = func(string) (string, error) { return "activating", nil }
		d.Health = func(string) status.HealthState { return status.HealthLoading }
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitWarn {
			t.Fatalf("activating-unit exit = %d, want %d (active WARN)", code, exitWarn)
		}
	})

	t.Run("is-active command error (empty+non-zero) → active FAIL → 1 (CR-02 tighten)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.IsActive = func(string) (string, error) {
			return "", orchestrate.ErrCommandFailed{Cmd: "systemctl --user is-active villa-llama.service"}
		}
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitBlocked {
			t.Fatalf("is-active-errored exit = %d, want %d (command error must FAIL)", code, exitBlocked)
		}
	})

	t.Run("is-active tool missing → unknown → WARN → 2 (never a false FAIL)", func(t *testing.T) {
		d := newStatusDeps(t, units)
		d.IsActive = func(string) (string, error) {
			return "", orchestrate.ErrToolNotFound{Tool: "systemctl"}
		}
		cmd, _, _ := statusTestCmd()
		if code := runStatus(cmd, nil, d); code != exitWarn {
			t.Fatalf("tool-missing exit = %d, want %d (can't-measure → WARN, not FAIL)", code, exitWarn)
		}
	})
}
