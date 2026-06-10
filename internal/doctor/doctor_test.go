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
	"strings"
	"testing"

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
	// isolates the three STRUCTURALLY typed-Unknown WARNs the supersession targets —
	// ROCM-PRE-firmware/-hsa/-image (checks_rocm.go:66-67 hardcode firmware/hsa as
	// UnknownStr; RunROCm passes an empty image) — which are exactly the live-UAT WARNs.
	d.Probe = func() detect.HostProfile {
		return detect.HostProfile{
			IGPUGfxID:     detect.KnownStr("gfx1151", "test"),
			KernelVersion: detect.KnownStr("6.18.9", "test"),
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

// --- Phase 22-03: memory-stack fold (D-08/D-09) + offload down-rank (Pitfall 1) ---

// memoryServiceNames are the systemd .service names of the two memory-stack managed
// services as the status fold names them (Quadlet villa-qdrant.container →
// villa-qdrant.service). They are finding-ID/service-name strings, NOT backend marker
// literals, so TestSeamGrepGate stays green (the ID-string-not-marker precedent).
var memoryServiceNames = []string{"villa-qdrant.service", "villa-embed.service"}

// memoryOnStatusReport extends healthyStatusReport with the two memory services as the
// status fold REALLY reports them today (Pitfall 1, Phase-20 UAT): active + HealthReady
// but carrying a typed-Unknown offload WARN (their journals have no chat-model
// load_tensors line), with OffloadApplies=true — the status-side N/A fix is Phase 23.
func memoryOnStatusReport() status.Report {
	r := healthyStatusReport()
	for _, svc := range memoryServiceNames {
		r.Services = append(r.Services, status.ServiceStatus{
			Service: svc,
			Active:  "active",
			Health:  status.HealthReady,
			Offload: inference.Verdict{
				Status: inference.StatusWarn,
				Detail: "residency could not be confirmed from the journal (no load_tensors buffer line)",
			},
			OffloadApplies: true,
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
	d.MemoryEnabled = true
	d.MemoryServices = memoryServiceNames
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
// memory-off default — mirror D-06), Aggregate emits NO memory finding at all: no
// MEM-PRE-* checks, no MEM-DOC-residency (a nil proof seam NEVER PASSes by default).
// Together with every pre-existing test in this file passing unchanged, this is the
// memory-off byte-identical guard (D-08/D-09 nil/zero-safety).
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
// every other check — a confident MEM-PRE-headroom FAIL raises Overall to FAIL (D-08).
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
		t.Error("MEM-PRE-headroom FAIL has empty Remediation (D-11)")
	}
	if df, ok := findingByID(r, "MEM-PRE-disk"); !ok || df.Status != "PASS" {
		t.Errorf("expected a PASS MEM-PRE-disk finding alongside the FAIL; got %+v (found=%v)", df, ok)
	}
}

// TestResidencyUnderLoadFailBlocks: a confident StatusFail Verdict from the
// residency-under-embedding-load proof maps to a BLOCK-class FAIL MEM-DOC-residency
// finding with non-empty remediation, raising Overall to FAIL (D-09 — a confident CPU
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
		t.Error("MEM-DOC-residency FAIL has empty Remediation (D-11)")
	}
	if f.Name != "Chat-model residency under embedding load" {
		t.Errorf("MEM-DOC-residency Name = %q, want the D-09 contract name", f.Name)
	}
}

// TestResidencyUnderLoadWarnDegrades: an unevaluable proof (StatusWarn — stack down,
// scrape failed, drive could not complete) degrades to a typed-Unknown WARN-tier WARN
// with the upstream detail preserved and a non-empty fallback remediation — never a
// false-green PASS and never a blocking FAIL (D-09/D-10).
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
		t.Error("MEM-DOC-residency WARN has empty Remediation (D-11)")
	}
	if f.Detail == "" {
		t.Error("MEM-DOC-residency WARN dropped the upstream 'could not evaluate' detail")
	}
}

// TestHealthyMemoryOnOverallPass is the Pitfall 1 resolution (Research Open Question 3
// — down-rank-but-visible): on a perfectly healthy memory-on stack — all checks PASS,
// proof PASS, every health ready, chat offload proven — the typed-Unknown offload WARNs
// the status fold reports for villa-qdrant/villa-embed are DOWN-RANKED (visible but
// non-rank-raising), so Overall == PASS instead of today's spurious WARN.
func TestHealthyMemoryOnOverallPass(t *testing.T) {
	r := Aggregate(memoryDoctorDeps())
	if r.Overall != "PASS" {
		t.Fatalf("Overall = %q, want PASS (healthy memory-on stack; memory-service offload WARNs must be down-ranked)", r.Overall)
	}
	// Visibility preserved: the down-rank suppresses rank contribution, NOT the finding.
	for _, svc := range memoryServiceNames {
		f, ok := findingByID(r, "offload:"+svc)
		if !ok {
			t.Errorf("expected down-ranked offload finding for %q to remain VISIBLE; findings: %+v", svc, r.Findings)
			continue
		}
		if f.Status != "WARN" {
			t.Errorf("offload:%s status = %q, want WARN (down-rank must not rewrite the status)", svc, f.Status)
		}
	}
	if f, ok := findingByID(r, "MEM-DOC-residency"); !ok || f.Status != "PASS" {
		t.Errorf("expected a PASS MEM-DOC-residency finding; got %+v (found=%v)", f, ok)
	}
}

// TestMemoryOffloadFailNotSuppressed is the no-false-green half of the down-rank
// (DOCTOR-02): the predicate is the CONJUNCTION (offload:<svc> with svc in
// MemoryServices) AND (Status==WARN) — a CONFIDENT offload FAIL on the very same
// memory service is NEVER suppressed and still folds Overall to FAIL.
func TestMemoryOffloadFailNotSuppressed(t *testing.T) {
	d := memoryDoctorDeps()
	d.StatusReport = func() status.Report {
		r := memoryOnStatusReport()
		for i := range r.Services {
			if r.Services[i].Service == "villa-qdrant.service" {
				r.Services[i].Offload = inference.Verdict{
					Status:      inference.StatusFail,
					Detail:      "only a CPU model buffer was loaded — server fell back to CPU",
					Remediation: "check backend residency",
				}
			}
		}
		return r
	}

	r := Aggregate(d)
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident offload FAIL on a memory service must NEVER be down-ranked — DOCTOR-02)", r.Overall)
	}
	f, ok := findingByID(r, "offload:villa-qdrant.service")
	if !ok || f.Status != "FAIL" {
		t.Errorf("expected a visible FAIL offload finding for villa-qdrant.service; got %+v (found=%v)", f, ok)
	}
}

// TestErroredStatusReportDegradesToWarn (phase-22 CR-01): an ERRORED status read-model
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
		t.Fatalf("Overall = FAIL — an unevaluable status read-model must never fabricate a blocking fault (CR-01)")
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
		t.Error("stack WARN has empty Remediation (D-11)")
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
