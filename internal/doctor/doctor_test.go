// doctor_test.go drives the pure internal/doctor core through a fully-stubbed
// doctor.Deps (mirroring cmd/villa/status_test.go's newStatusDeps builder): a
// healthy-default Deps where every host seam returns a benign value, and each test
// overrides exactly ONE knob to exercise a single behavior. The core is off-hardware
// testable by construction — no host I/O ever runs here.
//
// Invariants guarded (DOCTOR-01/02/03):
// - TestRemediationPresent — every non-PASS Finding carries non-empty Remediation.
//   - TestOffloadFailDominatesHealth — a confident offload FAIL dominates a HealthReady,
//     yielding a BLOCK-class FAIL Finding and Report.Overall=="FAIL" (Pitfall 3: no
//     false-green over a health-200).
//   - TestDriftWarn                — a non-empty Plan.Changed yields a drift WARN Finding and
//     Report.Overall=="WARN" (DOCTOR-03).
//   - TestDriftReadErrorDegrades   — a DriftPlan read error (absent unit dir) yields a
//
// typed-Unknown WARN Finding, never a panic.
//   - TestDownStackWarnsNotBlocks  — a confidently-down service (HealthDown) folds to a
//
// WARN-tier WARN Finding and Report.Overall=="WARN", never a blocking FAIL (
// a stopped stack is exit-2, not exit-1).
//
// NOTE: this file deliberately types NO backend marker literal (Vulkan0/ROCm0/image
// tags). Offload Verdicts are constructed opaquely via inference.Verdict only, so
// TestSeamGrepGate (which walks internal/) stays green.
package doctor

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
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
		Probe:        func() detect.HostProfile { return detect.HostProfile{} },
		LoadConfig:   func() (config.VillaConfig, error) { return config.VillaConfig{Backend: "vulkan"}, nil },
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
	// Probe Known-good gfx1151 + a kernel at/above the policy floor so the two
	// Probe-DRIVEN ROCm host-prep checks (ROCM-PRE-gfx / ROCM-PRE-kernel) PASS. That
	// isolates the three STRUCTURALLY typed-Unknown WARNs the supersession targets
	// ROCM-PRE-firmware/-hsa/-image (checks_rocm.go:66-67 hardcode firmware/hsa as
	// UnknownStr; RunROCm passes an empty image) — which are exactly the live-UAT WARNs.
	// KFDAccess/RenderNodeAccess are also Known-good so the additive PRE-08 check
	// (issue #120) PASSes here too, rather than adding a fourth, un-superseded
	// typed-Unknown WARN that would falsely re-open the residency-supersession gap
	// this fixture exists to isolate.
	d.Probe = func() detect.HostProfile {
		return detect.HostProfile{
			IGPUGfxID:        detect.KnownStr("gfx1151", "test"),
			KernelVersion:    detect.KnownStr("6.18.9", "test"),
			KFDAccess:        detect.KnownBool(true, "/dev/kfd"),
			RenderNodeAccess: detect.KnownBool(true, "/dev/dri/renderD128"),
		}
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

// TestConfidentROCmFAILStillDominatesResidency is the CENTRAL no-false-green guard
// (DOCTOR-02) and proves the supersession keys on the (ID AND Status==WARN) CONJUNCTION,
// NOT ID-alone. Under PROVEN ROCm residency (Backend="rocm", OffloadApplies=true,
// Offload.Status==inference.StatusPass), inject the image-aware host-prep gate
// (RunROCmImage) returning a CONFIDENT FAIL on a SUPERSEDED ID — idROCmImage, a denied
// RUNNING image (reachable only via this Option-B seam; checks_rocm.go:66-67 make a
// firmware/hsa FAIL unreachable via Probe). A confident FAIL on one of the very IDs the
// supersession down-ranks at WARN must NEVER be swallowed → Overall=="FAIL". (A
// ROCM-PRE-gfx-style guard would NOT exercise this risk: gfx is not in the superseded
// set, so an ID-only match would never have swallowed it — the danger lives precisely on
// the superseded IDs, so the assertion lives there.) Type no backend marker literal: the
// stub uses the ROCM-PRE-* ID string + neutral detail.
func TestConfidentROCmFAILStillDominatesResidency(t *testing.T) {
	d := rocmDoctorDeps() // proven residency: Backend=rocm, OffloadApplies, StatusPass
	d.RunROCmImage = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{{
			ID:          idROCmImage,
			Name:        "ROCm image not denied",
			Tier:        preflight.TierBlock,
			Status:      preflight.StatusFail,
			Detail:      "requested image matches a denied build — ROCm bring-up refused",
			Remediation: "use the digest-pinned stable ROCm image",
			Provenance:  "requested image",
		}}
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident FAIL on the superseded %s must NEVER be swallowed by residency-supersession — DOCTOR-02)", r.Overall, idROCmImage)
	}
	// The confident FAIL must still be present as a FAIL finding (not down-ranked).
	found := false
	for _, f := range r.Findings {
		if f.ID == idROCmImage && f.Status == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a FAIL finding on %s under proven residency; findings: %+v", idROCmImage, r.Findings)
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
// (DOCTOR-02).
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
// remediation, never a panic and never a false PASS.
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

// --- Phase 22-03: memory-stack fold + offload down-rank (Pitfall 1) ---

// memoryServiceNames are the systemd .service names of the two memory-stack managed
// services as the status fold names them (Quadlet villa-qdrant.container →
// villa-qdrant.service). They are finding-ID/service-name strings, NOT backend marker
// literals, so TestSeamGrepGate stays green (the ID-string-not-marker precedent).
var memoryServiceNames = []string{"villa-qdrant.service", "villa-embed.service"}

// memoryOnStatusReport extends healthyStatusReport with the two memory services as the
// Phase-23 v3 status fold reports them (Plan 23-01): active, their OWN per-service
// health, and the N/A offload representation with OffloadApplies=false — the source
// classification fix that made doctor's old offload down-rank unreachable.
func memoryOnStatusReport() status.Report {
	r := healthyStatusReport()
	for _, svc := range memoryServiceNames {
		r.Services = append(r.Services, status.ServiceStatus{
			Service: svc,
			Active:  "active",
			Health:  status.HealthReady,
			// Phase-23 v3 classification (Plan 23-01): memory services are non-GPU
			// rows — their own per-service health, an N/A offload Verdict, and
			// OffloadApplies=false so doctor's offloadFinding gate never fires.
			Offload: inference.Verdict{
				Status:     inference.StatusWarn,
				Detail:     "N/A — this service has no GPU offload",
				Provenance: "not an inference service (no llama-server residency to assert)",
			},
			OffloadApplies: false,
			OffloadOK:      false,
		})
	}
	return r
}

// memoryDoctorDeps builds a healthy-default MEMORY-ON doctor.Deps: all four memory
// seams bound — PASS memory checks, a PASS residency-under-load proof, the memory
// service names, and the memory-on status report whose two memory services carry the
// typed-Unknown offload WARNs the down-rank targets. It is based on rocmDoctorDeps()
// because that is the ONLY off-hardware fixture where host-prep PASS (and therefore
// Overall=="PASS") is constructible: the vulkan path runs preflight.Run over the
// empty test HostProfile, which emits typed-Unknown WARNs by construction (the same
// PASS-reachability constraint TestROCmResidencySupersedesHostPrepWARN works under).
// The memory fold + down-rank predicate under test are backend-independent.
func memoryDoctorDeps() Deps {
	d := rocmDoctorDeps()
	d.StatusReport = func() status.Report { return memoryOnStatusReport() }
	d.RunMemoryChecks = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{
			{ID: "MEM-PRE-disk", Name: "Vector-index disk space", Tier: preflight.TierBlock,
				Status: preflight.StatusPass, Detail: "free disk ok", Provenance: "test"},
			{ID: "MEM-PRE-headroom", Name: "Embedder memory headroom", Tier: preflight.TierBlock,
				Status: preflight.StatusPass, Detail: "free memory ok", Provenance: "test"},
		}
	}
	d.ResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusPass, Detail: "chat model resident under embedding load"}
	}
	return d
}

// findingByID returns the first finding with the given ID, and whether it was found.
func findingByID(r Report, id string) (Finding, bool) {
	for _, f := range r.Findings {
		if f.ID == id {
			return f, true
		}
	}
	return Finding{}, false
}

// TestMemoryOffNoMemoryFindings: with every new memory Deps field nil/zero (the
// memory-off default — mirror), Aggregate emits NO memory finding at all: no
// MEM-PRE-* checks, no MEM-DOC-residency (a nil proof seam NEVER PASSes by default).
// Together with every pre-existing test in this file passing unchanged, this is the
// memory-off byte-identical guard (nil/zero-safety).
func TestMemoryOffNoMemoryFindings(t *testing.T) {
	r := Aggregate(newDoctorDeps())
	for _, id := range []string{"MEM-PRE-disk", "MEM-PRE-headroom", "MEM-DOC-residency"} {
		if hasFinding(r, id) {
			t.Errorf("memory-off Aggregate emitted finding %q — new Deps fields must be nil/zero-safe", id)
		}
	}
	// NOTE: no Overall assertion here — the off-hardware vulkan fixture's host-prep
	// checks are typed-Unknown WARNs by construction (profile-dependent), so the
	// byte-identical memory-off guard is the absence of memory findings above PLUS
	// every pre-existing test in this file passing unchanged.
}

// TestMemoryChecksFoldedFailRaisesOverall: a non-nil RunMemoryChecks seam has its
// CheckResults folded as findings via findingFromCheck and ranked worst-wins like
// every other check — a confident MEM-PRE-headroom FAIL raises Overall to FAIL.
func TestMemoryChecksFoldedFailRaisesOverall(t *testing.T) {
	d := memoryDoctorDeps()
	d.RunMemoryChecks = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{
			{ID: "MEM-PRE-disk", Name: "Vector-index disk space", Tier: preflight.TierBlock,
				Status: preflight.StatusPass, Detail: "free disk ok", Provenance: "test"},
			{ID: "MEM-PRE-headroom", Name: "Embedder memory headroom", Tier: preflight.TierBlock,
				Status: preflight.StatusFail, Detail: "free memory below the embedding reservation",
				Remediation: "close memory-heavy processes", Provenance: "test"},
		}
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident MEM-PRE-headroom FAIL must rank worst-wins)", r.Overall)
	}
	f, ok := findingByID(r, "MEM-PRE-headroom")
	if !ok {
		t.Fatalf("expected MEM-PRE-headroom finding; findings: %+v", r.Findings)
	}
	if f.Status != "FAIL" || f.Tier != "BLOCK" {
		t.Errorf("MEM-PRE-headroom = (status %s, tier %s), want (FAIL, BLOCK)", f.Status, f.Tier)
	}
	if f.Remediation == "" {
		t.Error("MEM-PRE-headroom FAIL has empty Remediation")
	}
	if df, ok := findingByID(r, "MEM-PRE-disk"); !ok || df.Status != "PASS" {
		t.Errorf("expected a PASS MEM-PRE-disk finding alongside the FAIL; got %+v (found=%v)", df, ok)
	}
}

// TestResidencyUnderLoadFailBlocks: a confident StatusFail Verdict from the
// residency-under-embedding-load proof maps to a BLOCK-class FAIL MEM-DOC-residency
// finding with non-empty remediation, raising Overall to FAIL (a confident CPU
// fallback under embedding load is the silent-degradation fault, never a false-green).
func TestResidencyUnderLoadFailBlocks(t *testing.T) {
	d := memoryDoctorDeps()
	d.ResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "only a CPU model buffer was loaded — server fell back to CPU"}
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (confident CPU fallback under embedding load)", r.Overall)
	}
	f, ok := findingByID(r, "MEM-DOC-residency")
	if !ok {
		t.Fatalf("expected MEM-DOC-residency finding; findings: %+v", r.Findings)
	}
	if f.Status != "FAIL" || f.Tier != "BLOCK" {
		t.Errorf("MEM-DOC-residency = (status %s, tier %s), want (FAIL, BLOCK)", f.Status, f.Tier)
	}
	if f.Remediation == "" {
		t.Error("MEM-DOC-residency FAIL has empty Remediation")
	}
	if f.Name != "Chat-model residency under embedding load" {
		t.Errorf("MEM-DOC-residency Name = %q, want the D-09 contract name", f.Name)
	}
}

// TestResidencyUnderLoadWarnDegrades: an unevaluable proof (StatusWarn — stack down,
// scrape failed, drive could not complete) degrades to a typed-Unknown WARN-tier WARN
// with the upstream detail preserved and a non-empty fallback remediation — never a
// false-green PASS and never a blocking FAIL.
func TestResidencyUnderLoadWarnDegrades(t *testing.T) {
	d := memoryDoctorDeps()
	d.ResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusWarn, Detail: "could not evaluate residency under embedding load — villa-embed.service is not active"}
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (unevaluable proof degrades, never PASS/FAIL)", r.Overall)
	}
	f, ok := findingByID(r, "MEM-DOC-residency")
	if !ok {
		t.Fatalf("expected MEM-DOC-residency finding; findings: %+v", r.Findings)
	}
	if f.Status != "WARN" || f.Tier != "WARN" {
		t.Errorf("MEM-DOC-residency = (status %s, tier %s), want (WARN, WARN)", f.Status, f.Tier)
	}
	if f.Remediation == "" {
		t.Error("MEM-DOC-residency WARN has empty Remediation")
	}
	if f.Detail == "" {
		t.Error("MEM-DOC-residency WARN dropped the upstream 'could not evaluate' detail")
	}
}

// TestHealthyMemoryOnOverallPass is the Pitfall 1 resolution, now solved at the SOURCE
// (Plan 23-01): the v3 status fold classifies villa-qdrant/villa-embed as non-GPU rows
// (OffloadApplies=false), so doctor's offloadFinding gate never creates an
// offload:<memory-svc> finding at all — no down-rank needed — and a perfectly healthy
// memory-on stack reaches Overall == PASS. Their HEALTH findings remain (the honest
// per-service signal the false-green fix introduced).
func TestHealthyMemoryOnOverallPass(t *testing.T) {
	r := Aggregate(memoryDoctorDeps())
	if r.Overall != "PASS" {
		t.Fatalf("Overall = %q, want PASS (healthy memory-on stack; non-GPU memory rows emit no offload finding)", r.Overall)
	}
	for _, svc := range memoryServiceNames {
		if f, ok := findingByID(r, "offload:"+svc); ok {
			t.Errorf("memory service %q must emit NO offload finding (OffloadApplies=false since the v3 fix); got %+v", svc, f)
		}
		if f, ok := findingByID(r, "health:"+svc); !ok || f.Status != "PASS" {
			t.Errorf("expected a PASS health finding for %q (per-service health is the honest signal); got %+v (found=%v)", svc, f, ok)
		}
	}
	if f, ok := findingByID(r, "MEM-DOC-residency"); !ok || f.Status != "PASS" {
		t.Errorf("expected a PASS MEM-DOC-residency finding; got %+v (found=%v)", f, ok)
	}
}

// TestMemoryServiceDownWarns is the negative control of the v3 reclassification: a
// stopped villa-embed surfaces through its HEALTH finding (down → WARN — a down
// stack is an expected operational state), never a false PASS and never an
// offload finding.
func TestMemoryServiceDownWarns(t *testing.T) {
	d := memoryDoctorDeps()
	d.StatusReport = func() status.Report {
		r := memoryOnStatusReport()
		for i := range r.Services {
			if r.Services[i].Service == "villa-embed.service" {
				r.Services[i].Active = "inactive"
				r.Services[i].Health = status.HealthDown
			}
		}
		return r
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (a down memory service degrades via its health finding)", r.Overall)
	}
	f, ok := findingByID(r, "health:villa-embed.service")
	if !ok || f.Status != "WARN" {
		t.Errorf("expected a WARN health finding for the stopped villa-embed.service; got %+v (found=%v)", f, ok)
	}
	if f, ok := findingByID(r, "offload:villa-embed.service"); ok {
		t.Errorf("a memory service must never emit an offload finding; got %+v", f)
	}
}

// TestErroredStatusReportDegradesToWarn (phase-22): an ERRORED status read-model
// (status.Run's zero-value Report with err set — reachable on any host whose config/
// model/backend/render fails, e.g. a never-installed box) must degrade to ONE
// typed-Unknown WARN "stack" finding — NEVER the fabricated confident loopback
// "privacy breach" BLOCK FAIL the zero-value LoopbackOnly=false would otherwise
// produce. The errored Report is built through the REAL status.Run error path (the
// err field is unexported), so the fixture is exactly what doctor sees live.
func TestErroredStatusReportDegradesToWarn(t *testing.T) {
	d := newDoctorDeps()
	d.StatusReport = func() status.Report {
		return status.Run(status.Deps{LoadConfig: func() (config.VillaConfig, error) {
			return config.VillaConfig{}, errors.New(`model "ghost" not found in catalog`)
		}})
	}

	r := Aggregate(d)
	if r.Overall == "FAIL" {
		t.Fatalf("Overall = FAIL — an unevaluable status read-model must never fabricate a blocking fault")
	}
	if hasFinding(r, "loopback") {
		t.Error("errored read-model fabricated a loopback finding from the zero-value LoopbackOnly=false")
	}
	f, ok := findingByID(r, "stack")
	if !ok {
		t.Fatalf("expected a typed-Unknown 'stack' WARN finding for the errored read-model; findings: %+v", r.Findings)
	}
	if f.Status != "WARN" || f.Tier != tierWarn {
		t.Errorf("stack finding = (status %s, tier %s), want (WARN, %s)", f.Status, f.Tier, tierWarn)
	}
	if f.Remediation == "" {
		t.Error("stack WARN has empty Remediation")
	}
	if !strings.Contains(f.Detail, "not found in catalog") {
		t.Errorf("stack WARN detail %q must carry the real status.Run error cause", f.Detail)
	}
	// No service-derived finding can exist — the errored report has no Services.
	for _, found := range r.Findings {
		if strings.HasPrefix(found.ID, "health:") || strings.HasPrefix(found.ID, "offload:") {
			t.Errorf("errored read-model produced a service finding %q from a zero-value report", found.ID)
		}
	}
}

// TestDownStackWarnsNotBlocks: a confidently-down service (Health==HealthDown, no
// offload signal) must fold to a WARN-tier WARN health Finding and Report.Overall=="WARN"
// — NEVER a blocking FAIL. A stopped stack is an expected operational state: it
// maps to exit 2 (warning), not exit 1 (blocking fault), which is reserved for the silent-
// degradation faults (offload FAIL over a health-200, preflight BLOCK, loopback breach).
// Regression guard for (phase-13 code review).
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
		t.Fatalf("Overall = %q, want WARN (a down stack is a WARN, never a blocking FAIL)", r.Overall)
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
				t.Error("down health finding has empty Remediation")
			}
		}
	}
	if !found {
		t.Errorf("expected a health finding for the down service; findings: %+v", r.Findings)
	}
}

// --- Phase 28-01: coding-agent fold ---

// TestDoctorSchemaVersionAgentFold: the doctor --json contract self-version reached 2 when
// the agent findings were folded in (Phase 28-01). Phase 34-04 bumped it append-only 2→3
// (the web-search fold), and issue #120 bumped it again 3→4 (the PRE-08 fold, asserted by
// TestDoctorSchemaVersionIsFour); the const is the single source of truth — Aggregate
// stamps it on every Report. This test now tracks the CURRENT version so it cannot
// silently desync from the bump.
func TestDoctorSchemaVersionAgentFold(t *testing.T) {
	r := Aggregate(newDoctorDeps())
	if r.SchemaVersion != reportSchemaVersion {
		t.Fatalf("Report.SchemaVersion = %d, want %d (the const is the single source of truth)", r.SchemaVersion, reportSchemaVersion)
	}
}

// TestAgentToolCallFindingSwitch is the offload-FAIL-dominates truth table for the
// tool-call round-trip mapper (clone of residencyUnderLoadFinding): Pass → BLOCK/PASS,
// Fail → BLOCK/FAIL+remediation, Warn → WARN/WARN+remediation (typed-Unknown, never a
// false-green PASS).
func TestAgentToolCallFindingSwitch(t *testing.T) {
	cases := []struct {
		name       string
		in         inference.Verdict
		wantTier   string
		wantStatus string
		wantRemed  bool
	}{
		{"pass", inference.Verdict{Status: inference.StatusPass, Detail: "round-trip completed"}, tierBlock, statusPass, false},
		{"fail", inference.Verdict{Status: inference.StatusFail, Detail: "round-trip did not complete"}, tierBlock, statusFail, true},
		{"warn", inference.Verdict{Status: inference.StatusWarn, Detail: "could not evaluate"}, tierWarn, statusWarn, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := agentToolCallFinding(c.in)
			if f.ID != "agent-tool-call" {
				t.Errorf("ID = %q, want agent-tool-call", f.ID)
			}
			if f.Tier != c.wantTier || f.Status != c.wantStatus {
				t.Errorf("(tier %s, status %s), want (%s, %s)", f.Tier, f.Status, c.wantTier, c.wantStatus)
			}
			if c.wantRemed && f.Remediation == "" {
				t.Errorf("non-PASS finding has empty Remediation")
			}
			if !c.wantRemed && f.Status == statusPass && f.Remediation != "" {
				t.Errorf("PASS finding carries a Remediation %q", f.Remediation)
			}
		})
	}
}

// TestAgentResidencyFindingSwitch is the same offload-FAIL-dominates truth table for the
// agent under-load residency mapper (honesty dominance): a confident StatusFail is a
// BLOCK-class FAIL that dominates a healthy-looking HTTP-200; an unevaluable signal → WARN.
func TestAgentResidencyFindingSwitch(t *testing.T) {
	cases := []struct {
		name       string
		in         inference.Verdict
		wantTier   string
		wantStatus string
		wantRemed  bool
	}{
		{"pass", inference.Verdict{Status: inference.StatusPass, Detail: "coder resident under load"}, tierBlock, statusPass, false},
		{"fail", inference.Verdict{Status: inference.StatusFail, Detail: "coder fell back to CPU under load"}, tierBlock, statusFail, true},
		{"warn", inference.Verdict{Status: inference.StatusWarn, Detail: "could not evaluate"}, tierWarn, statusWarn, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := agentResidencyFinding(c.in)
			if f.ID != "agent-residency" {
				t.Errorf("ID = %q, want agent-residency", f.ID)
			}
			if f.Tier != c.wantTier || f.Status != c.wantStatus {
				t.Errorf("(tier %s, status %s), want (%s, %s)", f.Tier, f.Status, c.wantTier, c.wantStatus)
			}
			if c.wantRemed && f.Remediation == "" {
				t.Errorf("non-PASS finding has empty Remediation")
			}
		})
	}
}

// TestAgentDriftFindingsMatrix is the drift-report → findings mapping matrix:
//   - all clean → exactly ONE PASS finding.
//   - BinaryDriftUnknown → typed-Unknown WARN (agent-binary-drift).
//   - BinaryDrift → WARN + re-install remediation (agent-binary-drift).
//   - BinaryAbsent → WARN + Phase-27 install remediation (agent-binary-drift).
//   - ConfigDrift → WARN + review/re-render remediation (agent-config-drift).
//   - ConfigAbsent ALONE → NO finding (first-run trigger, not drift).
func TestAgentDriftFindingsMatrix(t *testing.T) {
	hasID := func(fs []Finding, id string) (Finding, bool) {
		for _, f := range fs {
			if f.ID == id {
				return f, true
			}
		}
		return Finding{}, false
	}

	t.Run("clean", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{})
		if len(fs) != 1 {
			t.Fatalf("clean drift → %d findings, want exactly 1 PASS; %+v", len(fs), fs)
		}
		if fs[0].Status != statusPass {
			t.Errorf("clean drift finding status = %s, want PASS", fs[0].Status)
		}
	})

	t.Run("binary-drift-unknown", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{BinaryDriftUnknown: true, Reason: "not yet pinned"})
		f, ok := hasID(fs, "agent-binary-drift")
		if !ok || f.Status != statusWarn || f.Tier != tierWarn {
			t.Fatalf("BinaryDriftUnknown → %+v, want a typed-Unknown WARN agent-binary-drift", fs)
		}
	})

	t.Run("binary-drift", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{BinaryDrift: true, Reason: "checksum mismatch"})
		f, ok := hasID(fs, "agent-binary-drift")
		if !ok || f.Status != statusWarn || f.Remediation == "" {
			t.Fatalf("BinaryDrift → %+v, want WARN agent-binary-drift with remediation", fs)
		}
	})

	t.Run("binary-absent", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{BinaryAbsent: true, Reason: "not installed"})
		f, ok := hasID(fs, "agent-binary-drift")
		if !ok || f.Status != statusWarn || f.Remediation == "" {
			t.Fatalf("BinaryAbsent → %+v, want WARN agent-binary-drift with install remediation", fs)
		}
	})

	t.Run("config-drift", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{ConfigDrift: true, Reason: "edited crush.json"})
		f, ok := hasID(fs, "agent-config-drift")
		if !ok || f.Status != statusWarn || f.Remediation == "" {
			t.Fatalf("ConfigDrift → %+v, want WARN agent-config-drift with remediation", fs)
		}
	})

	t.Run("config-absent-alone-no-finding", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{ConfigAbsent: true, Reason: "first run"})
		if _, ok := hasID(fs, "agent-config-drift"); ok {
			t.Errorf("ConfigAbsent alone must emit NO config-drift finding (first-run trigger); %+v", fs)
		}
		// Binary is present + matched + config absent → no binary drift either, and the
		// config-absent path is not drift, so there is no PASS-by-default for config.
		if _, ok := hasID(fs, "agent-config-drift"); ok {
			t.Errorf("unexpected config finding on the first-run absent path; %+v", fs)
		}
	})
}

// agentDoctorDeps extends memoryDoctorDeps with all three agent seams bound to healthy
// defaults: a PASS tool-call verdict, a PASS residency verdict, and a clean drift report.
func agentDoctorDeps() Deps {
	d := memoryDoctorDeps()
	d.AgentToolCall = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusPass, Detail: "tool-call round-trip completed"}
	}
	d.AgentResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusPass, Detail: "coder model resident under tool-call load"}
	}
	d.AgentDrift = func() agent.DriftReport { return agent.DriftReport{} }
	return d
}

// TestAgentOffNoAgentFindings: with every agent Deps seam nil (the agent-off default),
// Aggregate emits NO agent finding at all — never a PASS-by-default. This is the
// agent-off byte-identical guard.
func TestAgentOffNoAgentFindings(t *testing.T) {
	r := Aggregate(newDoctorDeps())
	for _, id := range []string{"agent-tool-call", "agent-residency", "agent-binary-drift", "agent-config-drift"} {
		if hasFinding(r, id) {
			t.Errorf("agent-off Aggregate emitted finding %q — agent Deps seams must be nil-safe (no PASS-by-default)", id)
		}
	}
}

// TestAgentToolCallFoldedFailRaisesOverall: a confident StatusFail tool-call verdict folds
// worst-wins to Overall==FAIL (an agent FAIL dominates a healthy-looking stack).
func TestAgentToolCallFoldedFailRaisesOverall(t *testing.T) {
	d := agentDoctorDeps()
	d.AgentToolCall = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "the agent tool-call round-trip failed"}
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident agent tool-call FAIL must dominate)", r.Overall)
	}
	f, ok := findingByID(r, "agent-tool-call")
	if !ok || f.Status != statusFail || f.Tier != tierBlock {
		t.Errorf("agent-tool-call = %+v (found=%v), want a BLOCK-class FAIL", f, ok)
	}
}

// TestAgentResidencyFoldedFailRaisesOverall: a confident StatusFail agent residency verdict
// (the coder fell back to CPU under tool-call load) folds worst-wins to Overall==FAIL.
func TestAgentResidencyFoldedFailRaisesOverall(t *testing.T) {
	d := agentDoctorDeps()
	d.AgentResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "the coder model fell back to CPU under load"}
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident agent residency FAIL must dominate a health-200)", r.Overall)
	}
	f, ok := findingByID(r, "agent-residency")
	if !ok || f.Status != statusFail || f.Tier != tierBlock {
		t.Errorf("agent-residency = %+v (found=%v), want a BLOCK-class FAIL", f, ok)
	}
}

// TestAgentDriftFoldedWarn: a non-nil AgentDrift seam reporting BinaryDrift folds the
// drift findings worst-wins; the clean memory+agent stack drops to WARN (drift is a WARN,
// never auto-corrected).
func TestAgentDriftFoldedWarn(t *testing.T) {
	d := agentDoctorDeps()
	d.AgentDrift = func() agent.DriftReport {
		return agent.DriftReport{BinaryDrift: true, Reason: "installed Crush binary checksum does not match the pinned policy"}
	}

	r := Aggregate(d)
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (binary drift folds to WARN, surfaced not auto-corrected)", r.Overall)
	}
	f, ok := findingByID(r, "agent-binary-drift")
	if !ok || f.Status != statusWarn || f.Remediation == "" {
		t.Errorf("agent-binary-drift = %+v (found=%v), want a WARN with remediation", f, ok)
	}
}

// TestAgentCleanDriftPasses: a clean drift report (binary present+matched, config
// present+matched) emits a single PASS agent-drift finding and does not raise Overall.
func TestAgentCleanDriftPasses(t *testing.T) {
	r := Aggregate(agentDoctorDeps())
	if r.Overall != "PASS" {
		t.Fatalf("Overall = %q, want PASS (healthy agent-on stack, clean drift)", r.Overall)
	}
}

// --- issue #120: PRE-08 compute device access fold (reportSchemaVersion 3→4) ---

// TestDoctorSchemaVersionIsFour: doctor's OWN --json contract self-version was bumped
// append-only 3→4 when PRE-08 (compute device access) was folded in. The const is the
// single source of truth — Aggregate stamps it on every Report. INDEPENDENT of status's
// reportSchemaVersion (5).
func TestDoctorSchemaVersionIsFour(t *testing.T) {
	r := Aggregate(newDoctorDeps())
	if r.SchemaVersion != 4 {
		t.Fatalf("Report.SchemaVersion = %d, want 4 (append-only bump for the PRE-08 fold)", r.SchemaVersion)
	}
}

// TestAggregateWebSearch proves the nil-safe fold: with both web-search seams nil (web
// off — the newDoctorDeps default) Aggregate emits NO web-search finding (byte-identical
// except the schema bump, which is covered separately); with the seams bound the egress
// and residency findings are present.
func TestAggregateWebSearch(t *testing.T) {
	t.Run("web-off-no-findings", func(t *testing.T) {
		r := Aggregate(newDoctorDeps())
		for _, id := range []string{"search-egress", "search-residency"} {
			if hasFinding(r, id) {
				t.Errorf("web-off Aggregate emitted finding %q — web-search Deps seams must be nil-safe (no PASS-by-default)", id)
			}
		}
	})
	t.Run("web-on-findings-present", func(t *testing.T) {
		d := newDoctorDeps()
		d.SearchEgressProof = func() inference.Verdict {
			return inference.Verdict{Status: inference.StatusPass, Detail: "outbound bounded: a recent verify search PASS"}
		}
		d.SearchResidencyUnderLoad = func() inference.Verdict {
			return inference.Verdict{Status: inference.StatusPass, Detail: "chat model resident under search load"}
		}
		r := Aggregate(d)
		if _, ok := findingByID(r, "search-egress"); !ok {
			t.Errorf("search-egress finding missing with SearchEgressProof bound")
		}
		if _, ok := findingByID(r, "search-residency"); !ok {
			t.Errorf("search-residency finding missing with SearchResidencyUnderLoad bound")
		}
	})
}

// TestSearchEgressFinding is the tri-state truth table for the egress-proof mapper
// a cached verify PASS+fresh → ready (PASS, no remediation); a real recent
// non-PASS → degraded-with-reason (FAIL/WARN + remediation); a stale/absent cache →
// typed-Unknown WARN + remediation. The Verdict is consumed opaquely (the cmd-tier
// closure does the freshness mapping), so the switch mirrors offloadFinding's grammar.
func TestSearchEgressFinding(t *testing.T) {
	cases := []struct {
		name       string
		in         inference.Verdict
		wantStatus string
		wantRemed  bool
	}{
		{"ready", inference.Verdict{Status: inference.StatusPass, Detail: "outbound bounded"}, statusPass, false},
		{"degraded", inference.Verdict{Status: inference.StatusFail, Detail: "a recent verify search FAILed"}, statusFail, true},
		{"unknown", inference.Verdict{Status: inference.StatusWarn, Detail: "no fresh verify search result"}, statusWarn, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := searchEgressFinding(c.in)
			if f.ID != "search-egress" {
				t.Errorf("ID = %q, want search-egress", f.ID)
			}
			if f.Status != c.wantStatus {
				t.Errorf("Status = %q, want %q", f.Status, c.wantStatus)
			}
			if c.wantRemed && f.Remediation == "" {
				t.Errorf("non-PASS egress finding has empty Remediation")
			}
			if !c.wantRemed && f.Remediation != "" {
				t.Errorf("PASS egress finding carries a Remediation %q", f.Remediation)
			}
		})
	}
}

// TestSearchResidencyFinding is the offload-FAIL-dominates truth table for the
// residency-under-search-load mapper: a confident CPU-fallback Verdict is a
// BLOCK-class FAIL that DOMINATES a healthy-looking HTTP-200 (never an idle-sampled
// false-green); a not-in-flight / unevaluable Verdict → typed-Unknown WARN.
func TestSearchResidencyFinding(t *testing.T) {
	cases := []struct {
		name       string
		in         inference.Verdict
		wantTier   string
		wantStatus string
		wantRemed  bool
	}{
		{"pass", inference.Verdict{Status: inference.StatusPass, Detail: "chat model resident under search load"}, tierBlock, statusPass, false},
		{"cpu-fallback", inference.Verdict{Status: inference.StatusFail, Detail: "only a CPU model buffer was loaded — server fell back to CPU under search load"}, tierBlock, statusFail, true},
		{"not-in-flight", inference.Verdict{Status: inference.StatusWarn, Detail: "no search-augmented round stayed in flight long enough to sample"}, tierWarn, statusWarn, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := searchResidencyFinding(c.in)
			if f.ID != "search-residency" {
				t.Errorf("ID = %q, want search-residency", f.ID)
			}
			if f.Tier != c.wantTier || f.Status != c.wantStatus {
				t.Errorf("(tier %s, status %s), want (%s, %s)", f.Tier, f.Status, c.wantTier, c.wantStatus)
			}
			if c.wantRemed && f.Remediation == "" {
				t.Errorf("non-PASS residency finding has empty Remediation")
			}
		})
	}
}

// TestSearchResidencyFoldedFailDominatesHealth proves the no-false-green invariant at the
// Aggregate level: a confident CPU-fallback search-residency Verdict folds worst-wins to
// Overall==FAIL even over an all-healthy HTTP-200 stack.
func TestSearchResidencyFoldedFailDominatesHealth(t *testing.T) {
	d := newDoctorDeps() // healthy HTTP-200 stack
	d.SearchResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "the chat model fell back to CPU under search load"}
	}
	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident CPU-fallback under search load must dominate a health-200)", r.Overall)
	}
	f, ok := findingByID(r, "search-residency")
	if !ok || f.Status != statusFail || f.Tier != tierBlock {
		t.Errorf("search-residency = %+v (found=%v), want a BLOCK-class FAIL not masked by HTTP-200", f, ok)
	}
}

// TestWebSearchFindingsHaveRemediation asserts EVERY non-PASS web-search finding carries a
// non-empty Remediation, across the egress and residency mappers' non-PASS branches.
func TestWebSearchFindingsHaveRemediation(t *testing.T) {
	d := newDoctorDeps()
	d.SearchEgressProof = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusWarn, Detail: "no fresh verify search result"}
	}
	d.SearchResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "fell back to CPU under search load"}
	}
	r := Aggregate(d)
	for _, id := range []string{"search-egress", "search-residency"} {
		f, ok := findingByID(r, id)
		if !ok {
			t.Fatalf("finding %q missing", id)
		}
		if f.Status != statusPass && f.Remediation == "" {
			t.Errorf("non-PASS web-search finding %q has empty Remediation", id)
		}
	}
}
