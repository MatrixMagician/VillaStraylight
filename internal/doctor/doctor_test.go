// doctor_test.go drives the pure internal/doctor core through a fully-stubbed
// doctor.Deps (mirroring cmd/villa/status_test.go's newStatusDeps builder): a
// healthy-default Deps where every host seam returns a benign value, and each test
// overrides exactly ONE knob to exercise a single behavior. The core is off-hardware
// testable by construction — no host I/O ever runs here.
//
// Invariants guarded (DOCTOR-01/02/03):
//   - TestRemediationPresent       — every non-PASS Finding carries non-empty Remediation (D-11).
//   - TestOffloadFailDominatesHealth — a confident offload FAIL dominates a HealthReady,
//     yielding a BLOCK-class FAIL Finding and Report.Overall=="FAIL" (Pitfall 3: no
//     false-green over a health-200).
//   - TestDriftWarn                — a non-empty Plan.Changed yields a drift WARN Finding and
//     Report.Overall=="WARN" (DOCTOR-03).
//   - TestDriftReadErrorDegrades   — a DriftPlan read error (absent unit dir) yields a
//     typed-Unknown WARN Finding, never a panic (D-08).
//   - TestDownStackWarnsNotBlocks  — a confidently-down service (HealthDown) folds to a
//     WARN-tier WARN Finding and Report.Overall=="WARN", never a blocking FAIL (D-08 /
//     CR-01: a stopped stack is exit-2, not exit-1).
//
// NOTE: this file deliberately types NO backend marker literal (Vulkan0/ROCm0/image
// tags). Offload Verdicts are constructed opaquely via inference.Verdict only, so
// TestSeamGrepGate (which walks internal/) stays green.
package doctor

import (
	"errors"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// healthyStatusReport builds an all-PASS status.Report: one inference service with a
// proven offload (OffloadApplies=true, Offload.Status=StatusPass) over a HealthReady,
// loopback-only so status.Aggregate would itself be PASS.
func healthyStatusReport() status.Report {
	return status.Report{
		Services: []status.ServiceStatus{
			{
				Service:        "villa-llama.service",
				Active:         "active",
				Health:         status.HealthReady,
				Offload:        inference.Verdict{Status: inference.StatusPass, Detail: "offload proven"},
				OffloadApplies: true,
				OffloadOK:      true,
			},
		},
		LoopbackOnly: true,
		Overall:      inference.StatusPass.String(),
	}
}

// newDoctorDeps builds a fully-stubbed healthy-default doctor.Deps. Each test copies
// it and overrides exactly one knob. Probe returns a benign typed-Unknown HostProfile
// (off-hardware honest default), LoadConfig a vulkan default, StatusReport the all-PASS
// report above, and DriftPlan an empty Plan with nil error (no drift).
func newDoctorDeps() Deps {
	return Deps{
		Probe:      func() detect.HostProfile { return detect.HostProfile{} },
		LoadConfig: func() (config.VillaConfig, error) { return config.VillaConfig{Backend: "vulkan"}, nil },
		StatusReport: func() status.Report { return healthyStatusReport() },
		DriftPlan:    func() (orchestrate.Plan, error) { return orchestrate.Plan{}, nil },
		Backend:      "vulkan",
	}
}

// rocmDoctorDeps builds a healthy-default doctor.Deps on the ROCm-family path:
// newDoctorDeps() with Backend="rocm" so Aggregate runs the ROCm host-prep gate
// (inference.IsROCmFamily("rocm")==true). The Probe stays the off-hardware
// typed-Unknown HostProfile (detect.HostProfile{}), so preflight.RunROCm emits the
// three ROCM-PRE-firmware/-hsa/-image findings as typed-Unknown WARN BY CONSTRUCTION
// (checks_rocm.go:66-67 hardcode firmware/hsa as UnknownStr; RunROCm passes an empty
// requested image) — exactly the structural WARNs from the live UAT (13-UAT.md Test 1).
// The StatusReport keeps OffloadApplies=true + Offload.Status=StatusPass over a
// HealthReady — the PROVEN-residency precondition the supersession keys off.
func rocmDoctorDeps() Deps {
	d := newDoctorDeps()
	d.Backend = "rocm"
	d.LoadConfig = func() (config.VillaConfig, error) {
		return config.VillaConfig{Backend: "rocm"}, nil
	}
	return d
}

// hasFinding reports whether the report carries a finding with the given ID.
func hasFinding(r Report, id string) bool {
	for _, f := range r.Findings {
		if f.ID == id {
			return true
		}
	}
	return false
}

// TestROCmResidencySupersedesHostPrepWARN is the gap-closure / residency-supersession
// invariant (13-UAT.md Test 1; DOCTOR-01 "exit 0 = healthy" on the opt-in ROCm path).
// Probe-reachable branch: a PROVEN ROCm residency (Backend="rocm", OffloadApplies=true,
// Offload.Status==inference.StatusPass over a HealthReady) must DOWN-RANK the three
// typed-Unknown ROCm host-prep WARNs (ROCM-PRE-firmware/-hsa/-image) so they no longer
// force Overall=WARN. The findings stay VISIBLE in r.Findings (the supersession
// down-ranks; it does NOT delete), and none becomes a FAIL. Before the fix the
// typed-Unknown ROCm WARNs fold to "WARN" (the gap); after the fix Overall=="PASS".
func TestROCmResidencySupersedesHostPrepWARN(t *testing.T) {
	d := rocmDoctorDeps()

	r := Aggregate(d)
	if r.Overall != "PASS" {
		t.Fatalf("Overall = %q, want PASS", r.Overall)
	}
	// Visibility preserved: the supersession down-ranks, it does NOT delete the findings.
	for _, id := range []string{"ROCM-PRE-firmware", "ROCM-PRE-hsa", "ROCM-PRE-image"} {
		if !hasFinding(r, id) {
			t.Errorf("expected superseded host-prep finding %q to remain VISIBLE in Findings; findings: %+v", id, r.Findings)
		}
	}
	// No finding may be a FAIL under proven residency over typed-Unknown host-prep WARNs.
	for _, f := range r.Findings {
		if f.Status == "FAIL" {
			t.Errorf("unexpected FAIL finding %q (tier %s) under proven residency; findings: %+v", f.ID, f.Tier, r.Findings)
		}
	}
}

// TestROCmResidencyDoesNotFireOnStatusFail is the supersession-GATING guard
// (DOCTOR-02 / no-false-green): the supersession is gated on inference.StatusPass and
// MUST NOT fire when residency is NOT proven. Probe-reachable branch: a confident
// offload FAIL (Offload.Status==inference.StatusFail over a HealthReady) on the
// Backend="rocm" path must still dominate the health-200 → Overall=="FAIL", and the
// typed-Unknown ROCM-PRE-* WARNs are NOT downgraded (no proven residency). This is the
// gating half of the invariant — reachable from Task 1 with no Deps seam (StatusFail
// comes from the StatusReport, not the host-prep gate). It passes today and must keep
// passing after the fix (forward-guard against the supersession over-firing on a
// non-proven offload).
func TestROCmResidencyDoesNotFireOnStatusFail(t *testing.T) {
	d := rocmDoctorDeps()
	d.StatusReport = func() status.Report {
		r := healthyStatusReport() // HealthReady stays
		r.Services[0].Offload = inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      "offloaded 0/33 layers",
			Remediation: "check backend residency",
		}
		r.Services[0].OffloadOK = false
		return r
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (offload StatusFail must dominate; supersession must NOT fire without proven residency)", r.Overall)
	}
}

// nonPassFindings returns the findings whose Status is not "PASS".
func nonPassFindings(r Report) []Finding {
	var out []Finding
	for _, f := range r.Findings {
		if f.Status != "PASS" {
			out = append(out, f)
		}
	}
	return out
}

// TestRemediationPresent: a Report built from a Deps with BOTH a drift Plan AND an
// offload-FAIL service must have every non-PASS Finding carrying non-empty Remediation
// (DOCTOR-02 / D-11).
func TestRemediationPresent(t *testing.T) {
	d := newDoctorDeps()
	d.StatusReport = func() status.Report {
		r := healthyStatusReport()
		r.Services[0].Offload = inference.Verdict{Status: inference.StatusFail, Detail: "CPU fallback"}
		r.Services[0].OffloadOK = false
		return r
	}
	d.DriftPlan = func() (orchestrate.Plan, error) {
		return orchestrate.Plan{Changed: []orchestrate.Unit{{Name: "villa-llama.container", Text: "x"}}}, nil
	}

	r := Aggregate(d)
	bad := nonPassFindings(r)
	if len(bad) == 0 {
		t.Fatal("expected at least one non-PASS finding (offload FAIL + drift), got none")
	}
	for _, f := range bad {
		if f.Remediation == "" {
			t.Errorf("non-PASS finding %q (status %s) has empty Remediation", f.ID, f.Status)
		}
	}
}

// TestOffloadFailDominatesHealth: a status.Report whose inference ServiceStatus has
// OffloadApplies=true and Offload.Status==StatusFail, over Health==HealthReady, must
// yield a BLOCK-class FAIL Finding and Report.Overall=="FAIL" (Pitfall 3 — no
// false-green over a health-200).
func TestOffloadFailDominatesHealth(t *testing.T) {
	d := newDoctorDeps()
	d.StatusReport = func() status.Report {
		r := healthyStatusReport() // HealthReady stays
		r.Services[0].Offload = inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      "offloaded 0/33 layers",
			Remediation: "check backend residency",
		}
		r.Services[0].OffloadOK = false
		return r
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (offload FAIL must dominate HealthReady)", r.Overall)
	}
	found := false
	for _, f := range r.Findings {
		if f.Status == "FAIL" && f.Tier == "BLOCK" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a BLOCK-class FAIL finding for the offload FAIL; findings: %+v", r.Findings)
	}
}

// TestDriftWarn: a DriftPlan returning a Plan with a non-empty Changed slice (and no
// offload FAIL) yields a drift WARN Finding and Report.Overall=="WARN" (DOCTOR-03).
func TestDriftWarn(t *testing.T) {
	d := newDoctorDeps()
	d.DriftPlan = func() (orchestrate.Plan, error) {
		return orchestrate.Plan{Changed: []orchestrate.Unit{
			{Name: "villa-llama.container", Text: "drifted"},
		}}, nil
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (non-empty Plan.Changed = drift WARN)", r.Overall)
	}
	found := false
	for _, f := range r.Findings {
		if f.Status == "WARN" && f.Remediation != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a WARN drift finding with remediation; findings: %+v", r.Findings)
	}
}

// TestDriftReadErrorDegrades: a DriftPlan returning a read error (e.g. absent unit
// dir on a never-installed host) must yield a typed-Unknown WARN Finding with
// remediation, never a panic and never a false PASS (D-08).
func TestDriftReadErrorDegrades(t *testing.T) {
	d := newDoctorDeps()
	d.DriftPlan = func() (orchestrate.Plan, error) {
		return orchestrate.Plan{}, errors.New("open unit dir: no such file or directory")
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (drift read error degrades to typed-Unknown WARN)", r.Overall)
	}
	found := false
	for _, f := range r.Findings {
		if f.Status == "WARN" && f.Remediation != "" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a typed-Unknown WARN finding with remediation on a drift read error; findings: %+v", r.Findings)
	}
}

// TestDownStackWarnsNotBlocks: a confidently-down service (Health==HealthDown, no
// offload signal) must fold to a WARN-tier WARN health Finding and Report.Overall=="WARN"
// — NEVER a blocking FAIL. A stopped stack is an expected operational state (D-08): it
// maps to exit 2 (warning), not exit 1 (blocking fault), which is reserved for the silent-
// degradation faults (offload FAIL over a health-200, preflight BLOCK, loopback breach).
// Regression guard for CR-01 (phase-13 code review).
func TestDownStackWarnsNotBlocks(t *testing.T) {
	d := newDoctorDeps()
	d.StatusReport = func() status.Report {
		r := healthyStatusReport()
		r.Services[0].Active = "inactive"
		r.Services[0].Health = status.HealthDown
		// A down service proves no offload — the offload finding is not emitted.
		r.Services[0].Offload = inference.Verdict{}
		r.Services[0].OffloadApplies = false
		r.Services[0].OffloadOK = false
		return r
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (a down stack is a WARN, never a blocking FAIL — D-08/CR-01)", r.Overall)
	}
	// No finding may be a blocking-tier FAIL: FAIL ⟺ BLOCK-class invariant means a down
	// stack must not escalate doctor to the blocking exit tier.
	for _, f := range r.Findings {
		if f.Status == "FAIL" {
			t.Errorf("a down stack produced a FAIL finding %q (tier %s) — expected WARN, never FAIL", f.ID, f.Tier)
		}
	}
	// The down service must surface a WARN health finding with actionable remediation.
	found := false
	for _, f := range r.Findings {
		if f.ID == "health:villa-llama.service" {
			found = true
			if f.Status != "WARN" || f.Tier != tierWarn {
				t.Errorf("down health finding = (status %s, tier %s), want (WARN, %s)", f.Status, f.Tier, tierWarn)
			}
			if f.Remediation == "" {
				t.Error("down health finding has empty Remediation (D-11)")
			}
		}
	}
	if !found {
		t.Errorf("expected a health finding for the down service; findings: %+v", r.Findings)
	}
}
