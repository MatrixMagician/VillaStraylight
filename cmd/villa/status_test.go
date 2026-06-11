package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
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

// memoryStatusCfg is the memory-ON config fixture for the v3 memory golden:
// the standard qwen3/vulkan install plus the typed memory defaults (sourced
// from config.DefaultVillaConfig — never re-typed literals).
func memoryStatusCfg() config.VillaConfig {
	cfg := config.DefaultVillaConfig()
	cfg.Model = "qwen3"
	cfg.Quant = "Q4"
	cfg.Ctx = 131072
	cfg.MemoryEnabled = true
	return cfg
}

// memoryLoopbackUnits renders the REAL memory-on stack so the frozen fixture
// walks genuine villa-qdrant/villa-embed container units (Pitfall 8 coherence:
// cfg.MemoryEnabled=true paired with render output containing the memory units).
func memoryLoopbackUnits(t *testing.T) []orchestrate.Unit {
	t.Helper()
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       memoryStatusCfg(),
		ModelFile: "qwen3.gguf",
		ModelsDir: "/home/villa/.local/share/villa/models",
	})
	if err != nil {
		t.Fatalf("render memory-on: %v", err)
	}
	return units
}

// newMemoryStatusDeps builds the memory-ON stubbed deps for the v3 golden: the
// base healthy stubs plus the memory-on config/units, per-service health seam
// stubs, the orchestrate-derived memory service names, and a complete-run
// recall state (deterministic timestamps so the golden is byte-stable).
func newMemoryStatusDeps(t *testing.T) *status.Deps {
	t.Helper()
	d := newStatusDeps(t, memoryLoopbackUnits(t))
	d.LoadConfig = func() (config.VillaConfig, error) { return memoryStatusCfg(), nil }
	d.QdrantService = unitServiceName(orchestrate.QdrantContainerUnitName())
	d.EmbedService = unitServiceName(orchestrate.EmbedContainerUnitName())
	d.QdrantHealth = func(string, int) status.HealthState { return status.HealthReady }
	d.EmbedHealth = func(string, int) status.HealthState { return status.HealthReady }
	d.ReadRecallState = func() *recall.State {
		return &recall.State{
			EmbeddingModel:       "nomic-embed-text-v1.5",
			EmbeddingDim:         768,
			LastIndexStartedAt:   "2026-06-09T10:00:00Z",
			LastIndexCompletedAt: "2026-06-09T10:05:00Z",
			Chats: map[string]recall.ChatState{
				"chat-1": {UserID: "u1", OWUIUpdatedAt: 1, FileID: "f1", IndexedAt: "2026-06-09T10:01:00Z"},
				"chat-2": {UserID: "u1", OWUIUpdatedAt: 2, FileID: "f2", IndexedAt: "2026-06-09T10:02:00Z"},
			},
		}
	}
	return d
}

// TestStatusJSONGoldenMemoryOn freezes the MEMORY-ON v3 --json contract
// byte-for-byte (D-04, Plan 23-01 — the milestone's single contract evolution):
// memory rows with per-service health + N/A offload, the memory section with
// the indexed recall summary, schema_version 3. Run with -update to regenerate.
func TestStatusJSONGoldenMemoryOn(t *testing.T) {
	d := newMemoryStatusDeps(t)
	cmd, out, _ := statusTestCmd()

	jsonOut = true
	defer func() { jsonOut = false }()

	code := runStatus(cmd, nil, d)
	if code != exitPass {
		t.Fatalf("healthy memory-on status: exit = %d, want %d (out: %s)", code, exitPass, out.String())
	}

	golden := filepath.Join("testdata", "status-memory.json.golden")
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
		t.Errorf("memory-on status --json does not match golden.\n--- got ---\n%s\n--- want ---\n%s", out.String(), want)
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

// --- Phase-23 memory-service probe seams (Plan 23-01 Task 2). All hermetic:
// the podman invocation is routed through the injectable memoryProbeExec seam;
// no live podman/network is touched. ---

// resetMemoryHealthCache clears the TTL cache so each test starts cold.
func resetMemoryHealthCache() {
	memoryHealthMu.Lock()
	memoryHealthAt = time.Time{}
	memoryHealthMu.Unlock()
}

// swapMemoryProbeExec installs a fake probe runner and restores it (plus a cold
// cache) on cleanup. Tests also isolate XDG_CONFIG_HOME so the sibling-target
// config read resolves typed defaults, never the developer's real config.
func swapMemoryProbeExec(t *testing.T, fake func(ctx context.Context, helperImage string, curlArgs ...string) ([]byte, int, error)) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	orig := memoryProbeExec
	memoryProbeExec = fake
	resetMemoryHealthCache()
	t.Cleanup(func() {
		memoryProbeExec = orig
		resetMemoryHealthCache()
	})
}

// probeURLOf extracts the target URL (the last curl arg) from a probe invocation.
func probeURLOf(curlArgs []string) string {
	if len(curlArgs) == 0 {
		return ""
	}
	return curlArgs[len(curlArgs)-1]
}

// TestLiveQdrantHealth proves the typed-Unknown probe mapping (T-23-03 fixed-arg
// probe, behavior table): an HTTP code written by curl maps 200→ready,
// 503→loading, other→down; a curl-level connect failure inside villa.network
// (exit < 125, no HTTP code) is a CONFIDENT down; a podman-level failure (exit
// 125/126/127 or podman absent) is typed-Unknown — never a fabricated confident
// state from an unevaluable probe. Also pins the /readyz path and the
// orchestrate-accessor helper image (no re-typed literals).
func TestLiveQdrantHealth(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		err  error
		want status.HealthState
	}{
		{"200 → ready", "200", 0, nil, status.HealthReady},
		{"503 → loading", "503", 0, nil, status.HealthLoading},
		{"404 → down", "404", 0, nil, status.HealthDown},
		{"curl connect failure (exit 7) → down", "", 7, errors.New("exit status 7"), status.HealthDown},
		{"podman generic failure (exit 125) → unknown", "", 125, errors.New("exit status 125"), status.HealthUnknown},
		{"podman command not found (exit 127) → unknown", "", 127, errors.New("exit status 127"), status.HealthUnknown},
		{"podman absent (could not start) → unknown", "", -1, errors.New("exec: podman: not found"), status.HealthUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotImage, gotURL string
			swapMemoryProbeExec(t, func(_ context.Context, helperImage string, curlArgs ...string) ([]byte, int, error) {
				gotImage = helperImage
				if strings.HasSuffix(probeURLOf(curlArgs), "/readyz") {
					gotURL = probeURLOf(curlArgs)
					return []byte(c.out), c.code, c.err
				}
				// Sibling embed probe in the shared refresh: healthy.
				return []byte("200"), 0, nil
			})
			if got := liveQdrantHealth("villa-qdrant", 6333); got != c.want {
				t.Errorf("liveQdrantHealth = %q, want %q", got, c.want)
			}
			if gotURL != "http://villa-qdrant:6333/readyz" {
				t.Errorf("qdrant probe URL = %q, want http://villa-qdrant:6333/readyz", gotURL)
			}
			if gotImage != orchestrate.EmbedImage() {
				t.Errorf("helper image = %q, want orchestrate.EmbedImage()", gotImage)
			}
		})
	}
}

// TestLiveEmbedHealth proves the embed probe targets /health on the passed
// addr:port and maps the llama-server 200/503 codes per liveHealthProbe's
// discipline (200→ready, 503→loading — WR-07, never down while loading).
func TestLiveEmbedHealth(t *testing.T) {
	cases := []struct {
		name string
		out  string
		code int
		err  error
		want status.HealthState
	}{
		{"200 → ready", "200", 0, nil, status.HealthReady},
		{"503 (model loading) → loading", "503", 0, nil, status.HealthLoading},
		{"curl connect failure → down (false-green fix: stopped embed is DOWN)", "", 7, errors.New("exit status 7"), status.HealthDown},
		{"podman-level failure → unknown", "", 126, errors.New("exit status 126"), status.HealthUnknown},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var gotURL string
			swapMemoryProbeExec(t, func(_ context.Context, _ string, curlArgs ...string) ([]byte, int, error) {
				if strings.HasSuffix(probeURLOf(curlArgs), "/health") {
					gotURL = probeURLOf(curlArgs)
					return []byte(c.out), c.code, c.err
				}
				// Sibling qdrant probe in the shared refresh: healthy.
				return []byte("200"), 0, nil
			})
			if got := liveEmbedHealth("villa-embed", 8080); got != c.want {
				t.Errorf("liveEmbedHealth = %q, want %q", got, c.want)
			}
			if gotURL != "http://villa-embed:8080/health" {
				t.Errorf("embed probe URL = %q, want http://villa-embed:8080/health", gotURL)
			}
		})
	}
}

// TestMemoryHealthTTL (OQ2 / T-23-04, Pitfall 2): one refresh probes BOTH
// services together (exactly one podman run per service), and every further
// call within the TTL window is served from the cache — podman is NOT executed
// again. This bounds the dashboard's 2.5s-poll churn to one probe pair per
// memoryHealthTTL.
func TestMemoryHealthTTL(t *testing.T) {
	runs := 0
	swapMemoryProbeExec(t, func(_ context.Context, _ string, curlArgs ...string) ([]byte, int, error) {
		runs++
		return []byte("200"), 0, nil
	})

	if got := liveQdrantHealth("villa-qdrant", 6333); got != status.HealthReady {
		t.Fatalf("first qdrant call = %q, want ready", got)
	}
	if runs != 2 {
		t.Fatalf("first refresh must probe BOTH services together: runs = %d, want 2", runs)
	}
	// All subsequent calls inside the TTL window are cache hits.
	_ = liveEmbedHealth("villa-embed", 8080)
	_ = liveQdrantHealth("villa-qdrant", 6333)
	_ = liveEmbedHealth("villa-embed", 8080)
	if runs != 2 {
		t.Errorf("calls within the TTL window must not re-execute podman: runs = %d, want 2", runs)
	}
}

// TestLiveReadRecallState proves the recall-state read seam's typed-Unknown
// discipline (D-02): an absent state file is a CONFIDENT empty ("no index yet"
// — pointer to the zero State, NOT nil); a valid store loads verbatim; a
// corrupt store fails closed to empty via recall.Load (never a fabricated
// count); a read error other than NotExist yields nil (status renders
// "unknown").
func TestLiveReadRecallState(t *testing.T) {
	t.Run("absent file → empty state (confident empty, not nil)", func(t *testing.T) {
		t.Setenv("XDG_DATA_HOME", t.TempDir())
		st := liveReadRecallState()
		if st == nil {
			t.Fatalf("absent store must yield a pointer to the zero State (confident empty), got nil")
		}
		if len(st.Chats) != 0 || st.LastIndexStartedAt != "" {
			t.Errorf("absent store must yield the zero State, got %+v", st)
		}
	})

	t.Run("valid store → loaded verbatim", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dir)
		blob := `{"schema_version":1,"knowledge_id":"kb1","knowledge_name":"villa-recall","embedding_model":"nomic-embed-text-v1.5","embedding_dim":768,"last_index_started_at":"2026-06-09T10:00:00Z","last_index_completed_at":"2026-06-09T10:05:00Z","chats":{"c1":{"user_id":"u1","owui_updated_at":1,"file_id":"f1","indexed_at":"2026-06-09T10:01:00Z"}}}`
		if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "villa", "recall-state.json"), []byte(blob), 0o600); err != nil {
			t.Fatal(err)
		}
		st := liveReadRecallState()
		if st == nil {
			t.Fatalf("valid store must load, got nil")
		}
		if st.EmbeddingModel != "nomic-embed-text-v1.5" || len(st.Chats) != 1 {
			t.Errorf("loaded state = %+v, want fixture values", st)
		}
	})

	t.Run("corrupt store → fail-closed empty (recall.Load semantics)", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dir)
		if err := os.MkdirAll(filepath.Join(dir, "villa"), 0o700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, "villa", "recall-state.json"), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
		st := liveReadRecallState()
		if st == nil {
			t.Fatalf("corrupt store fails CLOSED to empty in recall.Load, got nil")
		}
		if len(st.Chats) != 0 {
			t.Errorf("corrupt store must never fabricate counts, got %+v", st)
		}
	})

	t.Run("read error other than NotExist → nil (typed-Unknown)", func(t *testing.T) {
		dir := t.TempDir()
		t.Setenv("XDG_DATA_HOME", dir)
		// Make the state path a DIRECTORY so os.ReadFile errors with EISDIR.
		if err := os.MkdirAll(filepath.Join(dir, "villa", "recall-state.json"), 0o700); err != nil {
			t.Fatal(err)
		}
		if st := liveReadRecallState(); st != nil {
			t.Errorf("an unreadable store must yield nil (status renders unknown), got %+v", st)
		}
	})
}
