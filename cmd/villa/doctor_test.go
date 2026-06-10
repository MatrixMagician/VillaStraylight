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

	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
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
		SchemaVersion: 1,
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
		SchemaVersion: 1,
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
// golden MUST carry "schema_version": 1. doctor never extends status.Report's golden.
func TestDoctorJSON(t *testing.T) {
	var buf bytes.Buffer
	renderDoctor(&buf, healthyReport(), true, false)
	if !bytes.Contains(buf.Bytes(), []byte(`"schema_version": 1`)) {
		t.Errorf("--json output must carry schema_version 1, got:\n%s", buf.String())
	}
	assertGolden(t, "doctor.json.golden", buf.Bytes())
}

// memoryHealthyReport is the healthy MEMORY-ON shape (Phase 22-03, Pitfall 1 resolved):
// memory checks PASS, the under-load residency proof PASS, and the two memory-service
// offload WARNs DOWN-RANKED but still VISIBLE — Overall=="PASS" → exit 0. The fixture
// mirrors what doctor.Aggregate emits on a healthy memory-on stack; findings are data,
// not schema, so SchemaVersion stays 1.
func memoryHealthyReport() doctor.Report {
	return doctor.Report{
		Findings: []doctor.Finding{
			{ID: "PRE-01", Name: "Vulkan ICD + iGPU enumeration", Tier: "BLOCK", Status: "PASS", Detail: "RADV ICD present; 2 /dev/dri node(s)", Provenance: "icd; /dev/dri"},
			{ID: "MEM-PRE-disk", Name: "Vector-index disk space", Tier: "BLOCK", Status: "PASS", Detail: "free disk 469.22 GiB ≥ 1.00 GiB at the podman volume root", Provenance: "syscall.Statfs"},
			{ID: "MEM-PRE-headroom", Name: "Embedder memory headroom", Tier: "BLOCK", Status: "PASS", Detail: "free memory 76.67 GiB ≥ embedding reservation 0.50 GiB", Provenance: "/proc/meminfo MemAvailable"},
			{ID: "health:villa-llama.service", Name: "villa-llama.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-llama.service", Name: "villa-llama.service GPU offload", Tier: "BLOCK", Status: "PASS", Detail: "residency proven on Vulkan; GTT floor corroborated", Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)"},
			{ID: "health:villa-qdrant.service", Name: "villa-qdrant.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-qdrant.service", Name: "villa-qdrant.service GPU offload", Tier: "WARN", Status: "WARN", Detail: "residency could not be confirmed from the journal (no load_tensors buffer line)", Remediation: "offload could not be verified — ensure the stack is running, then re-run `villa doctor`", Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)"},
			{ID: "health:villa-embed.service", Name: "villa-embed.service health", Tier: "WARN", Status: "PASS", Detail: "/health is ready (200)", Provenance: "status.Report.Services[].Health"},
			{ID: "offload:villa-embed.service", Name: "villa-embed.service GPU offload", Tier: "WARN", Status: "WARN", Detail: "residency could not be confirmed from the journal (no load_tensors buffer line)", Remediation: "offload could not be verified — ensure the stack is running, then re-run `villa doctor`", Provenance: "status.Report.Services[].Offload (inference.RunningOffloadVerdict)"},
			{ID: "MEM-DOC-residency", Name: "Chat-model residency under embedding load", Tier: "BLOCK", Status: "PASS", Detail: "chat-model device buffer 21504.49 MiB resident on the iGPU; GTT-used floor corroborated mid-drive", Provenance: "embed-load drive + inference.RunningOffloadVerdict"},
			{ID: "drift", Name: "Config-vs-disk drift", Tier: "WARN", Status: "PASS", Detail: "on-disk units match the rendered-from-config units", Provenance: "orchestrate.Reconcile (empty Plan.Changed)"},
		},
		Overall:       "PASS",
		SchemaVersion: 1,
	}
}

// memoryResidencyFailReport flips the under-load proof to a confident CPU-fallback
// FAIL (D-09): MEM-DOC-residency becomes a BLOCK-class FAIL with remediation and
// Overall=="FAIL" → exitBlocked. The down-ranked memory offload WARNs stay visible.
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

// TestDoctorMemoryRender freezes the ADDITIVE memory-on render shapes (Phase 22-03):
// a healthy memory-on report (down-ranked offload WARNs visible, Overall PASS → exit
// 0) and a confident under-load residency FAIL (Overall FAIL → exitBlocked). NEW
// goldens only — the existing doctor goldens are untouched (memory-off byte-identical).
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
	if !bytes.Contains(buf.Bytes(), []byte(`"schema_version": 1`)) {
		t.Errorf("--json output must carry schema_version 1, got:\n%s", buf.String())
	}
	assertGolden(t, "doctor-memory.json.golden", buf.Bytes())
}

// TestLiveDoctorDepsWiresMemorySeams asserts liveDoctorDeps binds the four memory
// seams ONLY when the persisted memory_enabled is true (D-08/D-09, mirror D-06):
// memory off (absent config) → all four zero/nil so the memory-off doctor output is
// byte-identical; memory on → all four bound, with MemoryServices derived from the
// orchestrate accessors (.container → .service), never typed literals. It inspects
// only the constructed Deps fields — it never invokes the live host probes.
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
			if d.MemoryEnabled != tc.wantBound {
				t.Errorf("MemoryEnabled = %v, want %v", d.MemoryEnabled, tc.wantBound)
			}
			if got := d.RunMemoryChecks != nil; got != tc.wantBound {
				t.Errorf("RunMemoryChecks non-nil = %v, want %v", got, tc.wantBound)
			}
			if got := d.ResidencyUnderLoad != nil; got != tc.wantBound {
				t.Errorf("ResidencyUnderLoad non-nil = %v, want %v", got, tc.wantBound)
			}
			if !tc.wantBound {
				if len(d.MemoryServices) != 0 {
					t.Errorf("MemoryServices = %v, want empty on the memory-off path", d.MemoryServices)
				}
				return
			}
			// The .service names must derive from the orchestrate accessors via the
			// .container → .service conversion the status fold uses.
			want := []string{
				unitServiceName(orchestrate.QdrantContainerUnitName()),
				unitServiceName(orchestrate.EmbedContainerUnitName()),
			}
			if len(d.MemoryServices) != len(want) {
				t.Fatalf("MemoryServices = %v, want %v", d.MemoryServices, want)
			}
			for i := range want {
				if d.MemoryServices[i] != want[i] {
					t.Errorf("MemoryServices[%d] = %q, want %q", i, d.MemoryServices[i], want[i])
				}
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
