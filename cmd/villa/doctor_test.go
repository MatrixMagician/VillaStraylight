package main

// doctor_test.go drives the cmd-tier doctor verb deterministically off-hardware: it
// builds doctor.Report fixtures directly (no live host) and asserts the worst-wins exit
// mapping + the frozen --json contract.
//
// CRITICAL (D-04 / Pitfall 1): the exit table asserts exitBlocked (=1) for a residency
// FAIL and exitWarn (=2) for a drift WARN — mirroring the AUTHORITATIVE preflight
// constants, NOT the inverted ROADMAP prose. The shared `update` flag is declared in
// detect_test.go; assertGolden lives in preflight_test.go.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// healthyReport is an all-PASS fixture (Overall PASS → exit 0).
func healthyReport() doctor.Report {
	return doctor.Report{
		Findings: []doctor.Finding{
			{ID: "PRE-01", Name: "Vulkan ICD + iGPU enumeration", Tier: "BLOCK", Status: "PASS", Detail: "RADV ICD present; 2 /dev/dri node(s)", Provenance: "icd; /dev/dri"},
			{ID: "health:villa-llama", Name: "villa-llama health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-llama", Name: "villa-llama GPU offload", Tier: "BLOCK", Status: "PASS", Detail: "residency proven on Vulkan; GTT floor corroborated", Provenance: "status.Report.Services[].Offload"},
			{ID: "drift", Name: "Config-vs-disk drift", Tier: "WARN", Status: "PASS", Detail: "on-disk units match the rendered-from-config units", Provenance: "orchestrate.Reconcile (empty Plan.Changed)"},
		},
		Overall:       "PASS",
		SchemaVersion: 3,
	}
}

// driftReport adds a config-vs-disk drift WARN (Overall WARN → exit 2).
func driftReport() doctor.Report {
	r := healthyReport()
	r.Findings[3] = doctor.Finding{
		ID:          "drift",
		Name:        "Config-vs-disk drift",
		Tier:        "WARN",
		Status:      "WARN",
		Detail:      "on-disk Quadlet units no longer match the rendered-from-config units",
		Remediation: "re-run `villa install` to reconcile config-vs-disk drift",
		Provenance:  "orchestrate.Reconcile (non-empty Plan.Changed)",
	}
	r.Overall = "WARN"
	return r
}

// offloadFailReport adds a confident residency FAIL — a BLOCK-class fault that dominates
// a HealthReady (no false-green over a health-200; Overall FAIL → exit 1).
func offloadFailReport() doctor.Report {
	r := healthyReport()
	r.Findings[2] = doctor.Finding{
		ID:          "offload:villa-llama",
		Name:        "villa-llama GPU offload",
		Tier:        "BLOCK",
		Status:      "FAIL",
		Detail:      "no residency line — the model is running on CPU (silent fallback)",
		Remediation: "GPU offload is not happening — check the backend (`villa backend set`) and `villa logs`",
		Provenance:  "status.Report.Services[].Offload",
	}
	r.Overall = "FAIL"
	return r
}

// rocmSupersededReport is the POST-supersession shape of a fully-healthy opt-in ROCm
// install (13-UAT.md Test 1 / DOCTOR-01): proven ROCm residency (offload PASS), health
// 200, drift PASS, and the typed-Unknown ROCm host-prep advisories
// (ROCM-PRE-firmware/-hsa) still VISIBLE as WARN findings — but down-ranked by the
// residency-supersession so Overall=="PASS" → exit 0. It proves exit 0 with the
// host-prep advisories still shown. Types no backend marker literal (ROCM-PRE-* IDs +
// neutral detail strings).
func rocmSupersededReport() doctor.Report {
	return doctor.Report{
		Findings: []doctor.Finding{
			{ID: "ROCM-PRE-gfx", Name: "ROCm iGPU is gfx1151", Tier: "BLOCK", Status: "PASS", Detail: "iGPU is gfx1151", Provenance: "rocminfo"},
			{ID: "ROCM-PRE-kernel", Name: "ROCm kernel floor", Tier: "BLOCK", Status: "PASS", Detail: "kernel 6.18.9 meets the 6.18.4 floor", Provenance: "/proc/sys/kernel/osrelease"},
			{ID: "ROCM-PRE-firmware", Name: "ROCm linux-firmware not denied", Tier: "BLOCK", Status: "WARN", Detail: "firmware version not probed; ensure recent and avoid the denied build", Remediation: "install a recent linux-firmware and avoid the known-bad build", Provenance: "rocm-policy.json (firmwareDeny)"},
			{ID: "ROCM-PRE-hsa", Name: "ROCm HSA override set", Tier: "BLOCK", Status: "WARN", Detail: "could not verify HSA_OVERRIDE_GFX_VERSION", Remediation: "set the HSA override for the ROCm runtime on gfx1151", Provenance: "rocm-policy.json"},
			{ID: "health:villa-llama", Name: "villa-llama health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-llama", Name: "villa-llama GPU offload", Tier: "BLOCK", Status: "PASS", Detail: "residency proven on the running ROCm backend; GTT floor corroborated", Provenance: "status.Report.Services[].Offload"},
			{ID: "drift", Name: "Config-vs-disk drift", Tier: "WARN", Status: "PASS", Detail: "on-disk units match the rendered-from-config units", Provenance: "orchestrate.Reconcile (empty Plan.Changed)"},
		},
		Overall:       "PASS",
		SchemaVersion: 3,
	}
}

// TestDoctorExitCodes is the load-bearing exit contract (DOCTOR-01 / Pitfall 1): a
// healthy report → exitPass (0), a drift WARN → exitWarn (2), a residency FAIL →
// exitBlocked (1), and a residency-superseded ROCm report → exitPass (0) with the
// ROCM-PRE-* WARN advisories still visible. The FAIL/WARN codes mirror the authoritative
// preflight constants and MUST NOT be inverted.
func TestDoctorExitCodes(t *testing.T) {
	tests := []struct {
		name     string
		report   doctor.Report
		wantCode int
		golden   string
	}{
		{"healthy", healthyReport(), exitPass, "doctor-pass.golden"},
		{"warn", driftReport(), exitWarn, "doctor-warn.golden"},
		{"fail", offloadFailReport(), exitBlocked, ""},
		{"rocm-superseded", rocmSupersededReport(), exitPass, "doctor-rocm-superseded.golden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := renderDoctor(&buf, tc.report, false, false)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			if tc.golden != "" {
				assertGolden(t, tc.golden, buf.Bytes())
			}
		})
	}
}

// TestDoctorUnknownOverallFailsClosed (phase-22 WR-04): an unrecognized/empty Overall
// (a future Aggregate bug, a hand-built Report, a JSON-roundtripped fixture) must map
// to exitBlocked — for a health-verdict command, defaulting to "healthy" is the wrong
// defensive direction (mirrors renderInference's fail-closed default).
func TestDoctorUnknownOverallFailsClosed(t *testing.T) {
	for _, overall := range []string{"", "bogus", "pass"} {
		var buf bytes.Buffer
		code := renderDoctor(&buf, doctor.Report{Overall: overall, SchemaVersion: 1}, false, false)
		if code != exitBlocked {
			t.Errorf("Overall=%q mapped to exit %d, want %d (unknown verdict is never healthy)", overall, code, exitBlocked)
		}
		if !bytes.Contains(buf.Bytes(), []byte("unrecognized overall verdict")) {
			t.Errorf("Overall=%q output should explain the fail-closed mapping, got:\n%s", overall, buf.String())
		}
	}
}

// TestDoctorJSON freezes doctor's OWN --json contract (D-02/D-09) byte-for-byte. The
// golden MUST carry "schema_version": 3 (the web-search-fold bump, SURF-06). doctor never extends status.Report's golden.
func TestDoctorJSON(t *testing.T) {
	var buf bytes.Buffer
	renderDoctor(&buf, healthyReport(), true, false)
	if !bytes.Contains(buf.Bytes(), []byte(`"schema_version": 3`)) {
		t.Errorf("--json output must carry schema_version 3, got:\n%s", buf.String())
	}
	assertGolden(t, "doctor.json.golden", buf.Bytes())
}

// memoryHealthyReport is the healthy MEMORY-ON shape (Phase-23 v3 classification,
// Plan 23-01): memory checks PASS, the under-load residency proof PASS, and the two
// memory services carrying ONLY their per-service health findings — no offload
// findings exist for them (the status fold reports OffloadApplies=false non-GPU
// rows, so doctor's offloadFinding gate never fires) — Overall=="PASS" → exit 0.
// The fixture mirrors what doctor.Aggregate emits on a healthy memory-on stack;
// findings are data, not schema, so SchemaVersion stays 1.
func memoryHealthyReport() doctor.Report {
	return doctor.Report{
		Findings: []doctor.Finding{
			{ID: "PRE-01", Name: "Vulkan ICD + iGPU enumeration", Tier: "BLOCK", Status: "PASS", Detail: "RADV ICD present; 2 /dev/dri node(s)", Provenance: "icd; /dev/dri"},
			{ID: "MEM-PRE-disk", Name: "Vector-index disk space", Tier: "BLOCK", Status: "PASS", Detail: "free disk 469.22 GiB ≥ 1.00 GiB at the podman volume root", Provenance: "syscall.Statfs"},
			{ID: "MEM-PRE-headroom", Name: "Embedder memory headroom", Tier: "BLOCK", Status: "PASS", Detail: "free memory 76.67 GiB ≥ embedding reservation 0.50 GiB", Provenance: "/proc/meminfo MemAvailable"},
			{ID: "health:villa-llama.service", Name: "villa-llama.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-llama.service", Name: "villa-llama.service GPU offload", Tier: "BLOCK", Status: "PASS", Detail: "residency proven on Vulkan; GTT floor corroborated", Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)"},
			{ID: "health:villa-qdrant.service", Name: "villa-qdrant.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "health:villa-embed.service", Name: "villa-embed.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "MEM-DOC-residency", Name: "Chat-model residency under embedding load", Tier: "BLOCK", Status: "PASS", Detail: "chat-model device buffer 21504.49 MiB resident on the iGPU; GTT-used floor corroborated mid-drive", Provenance: "embed-load drive + inference.RunningOffloadVerdict"},
			{ID: "drift", Name: "Config-vs-disk drift", Tier: "WARN", Status: "PASS", Detail: "on-disk units match the rendered-from-config units", Provenance: "orchestrate.Reconcile (empty Plan.Changed)"},
		},
		Overall:       "PASS",
		SchemaVersion: 3,
	}
}

// memoryResidencyFailReport flips the under-load proof to a confident CPU-fallback
// FAIL (D-09): MEM-DOC-residency becomes a BLOCK-class FAIL with remediation and
// Overall=="FAIL" → exitBlocked.
func memoryResidencyFailReport() doctor.Report {
	r := memoryHealthyReport()
	for i := range r.Findings {
		if r.Findings[i].ID == "MEM-DOC-residency" {
			r.Findings[i].Status = "FAIL"
			r.Findings[i].Detail = "only a CPU model buffer was loaded — the chat model fell back to CPU under embedding load"
			r.Findings[i].Remediation = "the chat model fell back to CPU under embedding load — check the backend (`villa backend set`) and `villa logs`"
		}
	}
	r.Overall = "FAIL"
	return r
}

// TestDoctorMemoryRender freezes the memory-on render shapes (re-frozen in Plan 23-01
// with the v3 classification: memory services emit health findings only, no offload
// findings): a healthy memory-on report (Overall PASS → exit 0) and a confident
// under-load residency FAIL (Overall FAIL → exitBlocked). The memory-off doctor
// goldens are untouched (memory-off byte-identical).
func TestDoctorMemoryRender(t *testing.T) {
	tests := []struct {
		name     string
		report   doctor.Report
		wantCode int
		golden   string
	}{
		{"memory-healthy", memoryHealthyReport(), exitPass, "doctor-memory-pass.golden"},
		{"memory-residency-fail", memoryResidencyFailReport(), exitBlocked, "doctor-memory-residency-fail.golden"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var buf bytes.Buffer
			code := renderDoctor(&buf, tc.report, false, false)
			if code != tc.wantCode {
				t.Errorf("exit code = %d, want %d", code, tc.wantCode)
			}
			assertGolden(t, tc.golden, buf.Bytes())
		})
	}
}

// TestDoctorMemoryJSON freezes the ADDITIVE memory-on --json shape: the memory
// findings flow through the unchanged doctor Report contract (findings are data, not
// schema — schema_version stays 1). NEW golden; doctor.json.golden is untouched.
func TestDoctorMemoryJSON(t *testing.T) {
	var buf bytes.Buffer
	renderDoctor(&buf, memoryHealthyReport(), true, false)
	if !bytes.Contains(buf.Bytes(), []byte(`"schema_version": 3`)) {
		t.Errorf("--json output must carry schema_version 3, got:\n%s", buf.String())
	}
	assertGolden(t, "doctor-memory.json.golden", buf.Bytes())
}

// agentHealthyReport is the healthy AGENT-ON shape (Phase 28-01, SURF-02): the
// memory-on healthy report PLUS the three additive agent findings — a PASS tool-call
// round-trip, a PASS coder residency under tool-call load, and a clean drift PASS.
// Overall=="PASS" → exit 0. Findings are data, not schema; the contract bump is the
// schema_version 1→2 carried by EVERY doctor report (additive agent fold).
func agentHealthyReport() doctor.Report {
	r := memoryHealthyReport()
	r.Findings = append(r.Findings,
		doctor.Finding{ID: "agent-tool-call", Name: "Coding-agent tool-call round-trip", Tier: "BLOCK", Status: "PASS", Detail: "the agent completed a real read→edit tool-call round-trip over the local endpoint", Provenance: "crush-run tool-call round-trip (liveAgentToolCallProbe)"},
		doctor.Finding{ID: "agent-residency", Name: "Coder-model residency under tool-call load", Tier: "BLOCK", Status: "PASS", Detail: "coder-model device buffer resident on the iGPU; GTT-used floor corroborated mid-drive", Provenance: "tool-call drive + inference.RunningOffloadVerdict"},
		doctor.Finding{ID: "agent-drift", Name: "Coding-agent drift", Tier: "WARN", Status: "PASS", Detail: "the installed Crush binary and on-disk crush.json match the pinned policy and rendered reference", Provenance: "agent.DetectDrift (clean)"},
	)
	return r
}

// agentToolCallFailReport flips the agent tool-call round-trip to a confident FAIL — a
// BLOCK-class fault that DOMINATES a healthy-looking HTTP-200 (D-07), Overall=="FAIL" →
// exitBlocked.
func agentToolCallFailReport() doctor.Report {
	r := agentHealthyReport()
	for i := range r.Findings {
		if r.Findings[i].ID == "agent-tool-call" {
			r.Findings[i].Status = "FAIL"
			r.Findings[i].Detail = "the agent ran but did not complete the read→edit tool-call round-trip (the probe file was not edited as instructed)"
			r.Findings[i].Remediation = "check `villa verify agent` and `villa logs` — the coder model may not be serving tool-calls correctly"
		}
	}
	r.Overall = "FAIL"
	return r
}

// TestDoctorAgentRender freezes the agent-on render shapes: a healthy agent-on report
// (Overall PASS → exit 0, golden doctor-agent.json.golden) and a confident tool-call FAIL
// (Overall FAIL → exitBlocked). It proves the additive agent findings render and that an
// agent FAIL maps to the blocking exit tier (D-07 honesty dominance).
func TestDoctorAgentRender(t *testing.T) {
	var buf bytes.Buffer
	code := renderDoctor(&buf, agentHealthyReport(), true, false)
	if code != exitPass {
		t.Errorf("agent-healthy exit code = %d, want %d", code, exitPass)
	}
	if !bytes.Contains(buf.Bytes(), []byte(`"schema_version": 3`)) {
		t.Errorf("--json output must carry schema_version 3 (the web-search-fold bump), got:\n%s", buf.String())
	}
	assertGolden(t, "doctor-agent.json.golden", buf.Bytes())

	var failBuf bytes.Buffer
	if code := renderDoctor(&failBuf, agentToolCallFailReport(), false, false); code != exitBlocked {
		t.Errorf("agent tool-call FAIL exit code = %d, want %d (a confident agent FAIL must block — D-07)", code, exitBlocked)
	}
}

// TestLiveDoctorDepsWiresAgentSeams asserts liveDoctorDeps binds the three agent seams
// ONLY when the persisted agent_enabled is true (SURF-02, mirroring the memory-seam
// wiring): agent off (absent config) → all three nil so the agent-off doctor output is
// byte-identical; agent on → all three bound. It inspects only the constructed Deps
// fields — it never invokes the live host probes.
func TestLiveDoctorDepsWiresAgentSeams(t *testing.T) {
	cases := []struct {
		name      string
		agentOn   bool
		wantBound bool
	}{
		{"agent-off-default", false, false},
		{"agent-on", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgBase := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgBase)
			if tc.agentOn {
				dir := filepath.Join(cfgBase, "villa")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("mkdir config dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("agent_enabled = true\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			d, err := liveDoctorDeps()
			if err != nil {
				t.Fatalf("liveDoctorDeps() error = %v", err)
			}
			if got := d.AgentToolCall != nil; got != tc.wantBound {
				t.Errorf("AgentToolCall non-nil = %v, want %v", got, tc.wantBound)
			}
			if got := d.AgentResidencyUnderLoad != nil; got != tc.wantBound {
				t.Errorf("AgentResidencyUnderLoad non-nil = %v, want %v", got, tc.wantBound)
			}
			if got := d.AgentDrift != nil; got != tc.wantBound {
				t.Errorf("AgentDrift non-nil = %v, want %v", got, tc.wantBound)
			}
		})
	}
}

// TestLiveDoctorDepsWiresMemorySeams asserts liveDoctorDeps binds the memory seams
// ONLY when the persisted memory_enabled is true (D-08/D-09, mirror D-06): memory
// off (absent config) → both nil so the memory-off doctor output is byte-identical;
// memory on → both bound. (The old MemoryEnabled/MemoryServices wiring assertions
// were removed with the doctor offload down-rank — Plan 23-01.) It inspects only
// the constructed Deps fields — it never invokes the live host probes.
func TestLiveDoctorDepsWiresMemorySeams(t *testing.T) {
	cases := []struct {
		name      string
		memoryOn  bool
		wantBound bool
	}{
		{"memory-off-default", false, false},
		{"memory-on", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgBase := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgBase)
			if tc.memoryOn {
				dir := filepath.Join(cfgBase, "villa")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("mkdir config dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("memory_enabled = true\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			d, err := liveDoctorDeps()
			if err != nil {
				t.Fatalf("liveDoctorDeps() error = %v", err)
			}
			if got := d.RunMemoryChecks != nil; got != tc.wantBound {
				t.Errorf("RunMemoryChecks non-nil = %v, want %v", got, tc.wantBound)
			}
			if got := d.ResidencyUnderLoad != nil; got != tc.wantBound {
				t.Errorf("ResidencyUnderLoad non-nil = %v, want %v", got, tc.wantBound)
			}
		})
	}
}

// TestLiveDoctorDepsWiresRunROCmImage closes the silently-nil hole in the Option-B
// image thread-through: liveDoctorDeps() must populate the RunROCmImage seam NON-NIL on
// a ROCm-family backend (so a denied running image is a confident FAIL via
// preflight.RunROCmForImage, never the un-evaluated WARN) and leave it NIL for vulkan
// (the nil-fallback path Aggregate handles by calling preflight.Run). It inspects only
// the constructed Deps func-field for nil-ness — it never invokes the live host probes.
// The config backend is driven deterministically via XDG_CONFIG_HOME so the test is
// off-hardware. (The newDoctorDeps() test double leaves RunROCmImage nil ON PURPOSE; that
// intended nil-fallback path is covered by the internal/doctor tests.)
func TestLiveDoctorDepsWiresRunROCmImage(t *testing.T) {
	cases := []struct {
		name       string
		backend    string // "" → write no config file (default vulkan)
		wantNonNil bool
	}{
		{"vulkan-default", "", false},
		{"rocm", "rocm", true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgBase := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgBase)
			if tc.backend != "" {
				dir := filepath.Join(cfgBase, "villa")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("mkdir config dir: %v", err)
				}
				body := "backend = \"" + tc.backend + "\"\n"
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte(body), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			d, err := liveDoctorDeps()
			if err != nil {
				t.Fatalf("liveDoctorDeps() error = %v", err)
			}
			got := d.RunROCmImage != nil
			if got != tc.wantNonNil {
				t.Errorf("RunROCmImage non-nil = %v, want %v (backend %q)", got, tc.wantNonNil, tc.backend)
			}
		})
	}
}

// --- Phase 34-04: live web-search residency + egress-proof seams (SURF-06) ---

// TestRunSearchResidencyUnderLoadPreconditionGate exercises the read-only precondition
// gate of the live search-residency proof OFF-HARDWARE (SURF-06, T-34-13): a stub
// status.Deps whose IsActive reports the served inference unit as NOT active must yield a
// typed-Unknown WARN (never a FAIL fabricated from a stack that simply is not running, and
// never an idle-sampled false-green). doctor NEVER starts a service. This is the
// off-hardware-reachable half of the drive seam; the full drive→sample→join is exercised
// on the live Strix Halo box (plan verification).
func TestRunSearchResidencyUnderLoadPreconditionGate(t *testing.T) {
	sd := &status.Deps{
		// Served inference unit not active → the gate must short-circuit to typed-Unknown
		// BEFORE any podman drive runs (off-hardware safe).
		IsActive: func(string) (string, error) { return "inactive", nil },
	}
	v := runSearchResidencyUnderLoad(config.VillaConfig{Backend: "vulkan", WebSearchEnabled: true}, sd)
	if v.Status != inference.StatusWarn {
		t.Fatalf("Status = %v, want StatusWarn (typed-Unknown when the served unit is not active — never a fabricated FAIL)", v.Status)
	}
	if v.Remediation == "" {
		t.Errorf("typed-Unknown verdict must carry a Remediation (D-11)")
	}
}

// TestLiveDoctorDepsWiresWebSearchSeams asserts liveDoctorDeps binds the two web-search
// seams ONLY when the persisted web_search_enabled is true (SURF-06, mirroring the memory/
// agent-seam wiring): web off (absent config) → both nil so the web-off doctor output is
// byte-identical (except the schema bump); web on → both bound. It inspects only the
// constructed Deps func-fields — it never invokes the live host probes.
func TestLiveDoctorDepsWiresWebSearchSeams(t *testing.T) {
	cases := []struct {
		name      string
		webOn     bool
		wantBound bool
	}{
		{"web-off-default", false, false},
		{"web-on", true, true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			cfgBase := t.TempDir()
			t.Setenv("XDG_CONFIG_HOME", cfgBase)
			if tc.webOn {
				dir := filepath.Join(cfgBase, "villa")
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatalf("mkdir config dir: %v", err)
				}
				if err := os.WriteFile(filepath.Join(dir, "config.toml"), []byte("web_search_enabled = true\n"), 0o600); err != nil {
					t.Fatalf("write config: %v", err)
				}
			}

			d, err := liveDoctorDeps()
			if err != nil {
				t.Fatalf("liveDoctorDeps() error = %v", err)
			}
			if got := d.SearchEgressProof != nil; got != tc.wantBound {
				t.Errorf("SearchEgressProof non-nil = %v, want %v", got, tc.wantBound)
			}
			if got := d.SearchResidencyUnderLoad != nil; got != tc.wantBound {
				t.Errorf("SearchResidencyUnderLoad non-nil = %v, want %v", got, tc.wantBound)
			}
		})
	}
}
