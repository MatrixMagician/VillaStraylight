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
