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
