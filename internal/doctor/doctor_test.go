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
//   - TestDriftReadErrorDegrades   — an unreadable/absent unit dir yields a
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
	"io/fs"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// healthyStatusReport builds an all-PASS status.Report: one inference service with a
// proven offload (OffloadApplies=true, Offload.Status=StatusPass) over a HealthReady,
// loopback-only so status.Aggregate would itself be PASS.
func healthyStatusReport() status.Report {
	checkedToday := 0
	return status.Report{
		Updates: status.UpdatesInfo{State: status.UpdatesChecked, CheckedAt: "2026-09-11T12:26:35Z", AgeDays: &checkedToday},
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

// run is one doctor run's two inputs: the config it loaded and the seams it reads.
// Aggregate decides from the first and asks the second for raw reads, so a test sets
// cfg to switch a subsystem and the fake unit dir to stage the disk, and never binds a
// seam to gate anything (ADR-0017).
type run struct {
	Deps
	cfg config.VillaConfig
	// units is the fake Quadlet dir ReadUnit serves; a name missing from it is an absent unit.
	units map[string]string
	// dirMissing makes UnitDirExists answer false; dirErr makes it fail.
	dirMissing bool
	dirErr     error
}

func (r *run) aggregate() Report { return Aggregate(r.cfg, r.Deps) }

// render makes the units cfg calls for exactly these; the fake dir holds whatever
// r.units says, so a unit is drifted, unchanged or absent by what the test stages there.
func (r *run) render(us ...orchestrate.Unit) {
	r.RenderUnits = func(config.VillaConfig, string) ([]orchestrate.Unit, error) { return us, nil }
}

// inferenceUnitName is the Quadlet file villa-llama is rendered to.
func inferenceUnitName() string {
	units, _ := subsystem.Inference.Units()
	return units[0]
}

// servedUnit is an inference unit rendered without the tool-calling flag.
const servedUnit = "[Container]\nExec=llama-server -m x --port 8080\n"

// newDoctorDeps builds a fully-stubbed healthy-default run: the vulkan backend with every
// optional subsystem off, and seams that read a benign host. Each test copies it and
// overrides exactly one knob. Probe returns a benign typed-Unknown HostProfile
// (off-hardware honest default), StatusReport the all-PASS report above, and the fake
// unit dir holds the inference unit and renders no units, so there is no drift and
// tools mode matches (off).
func newDoctorDeps() *run {
	r := &run{
		cfg:   config.VillaConfig{Backend: "vulkan"},
		units: map[string]string{},
	}
	r.units[inferenceUnitName()] = servedUnit
	r.Deps = Deps{
		Probe:        func() detect.HostProfile { return detect.HostProfile{} },
		StatusReport: func() status.Report { return healthyStatusReport() },
		IsActive:     func(string) (string, error) { return "inactive", nil },
		ReadVerifyState: func() *verifystate.State {
			return &verifystate.State{}
		},
		UnitDirExists: func() (bool, error) { return !r.dirMissing && r.dirErr == nil, r.dirErr },
		ReadUnit: func(name string) ([]byte, error) {
			if text, ok := r.units[name]; ok {
				return []byte(text), nil
			}
			return nil, fs.ErrNotExist
		},
		RenderUnits:  func(config.VillaConfig, string) ([]orchestrate.Unit, error) { return nil, nil },
		RunningVilla: func() string { return "/usr/local/bin/villa" },
		// Every seam is wired, as liveDoctorDeps wires them, and the config decides
		// which ones Aggregate calls. The preflight bindings answer no checks and the
		// four proofs pass.
		RunMemoryChecks:          func(detect.HostProfile, preflight.MemoryGateInput) []preflight.CheckResult { return nil },
		RunSandboxChecks:         func(detect.HostProfile) []preflight.CheckResult { return nil },
		ResidencyUnderLoad:       passVerdict,
		AgentToolCall:            passVerdict,
		AgentResidencyUnderLoad:  passVerdict,
		SearchResidencyUnderLoad: passVerdict,
	}
	r.cleanAgentDrift()
	return r
}

// passVerdict is a proof that proves.
func passVerdict() inference.Verdict {
	return inference.Verdict{Status: inference.StatusPass, Detail: "resident under load"}
}

// rocmDoctorDeps builds a healthy-default run on the ROCm-family path:
// newDoctorDeps() with Backend="rocm" so Aggregate runs the ROCm host-prep gate
// (inference.IsROCmFamily("rocm")==true). The Probe stays the off-hardware
// typed-Unknown HostProfile (detect.HostProfile{}), so preflight.RunROCmForImage emits
// ROCM-PRE-firmware/-hsa as typed-Unknown WARN BY CONSTRUCTION (an unprobed firmware and
// HSA readiness are Unknown) — exactly the structural WARNs from the live UAT
// (13-UAT.md Test 1).
// The StatusReport keeps OffloadApplies=true + Offload.Status=StatusPass over a
// HealthReady — the PROVEN-residency precondition the supersession keys off.
func rocmDoctorDeps() *run {
	d := newDoctorDeps()
	d.cfg.Backend = "rocm"
	// Probe Known-good gfx1151 + a kernel at/above the policy floor so the two
	// Probe-DRIVEN ROCm host-prep checks (ROCM-PRE-gfx / ROCM-PRE-kernel) PASS. That
	// isolates the STRUCTURALLY typed-Unknown WARNs the supersession targets.
	// KFDAccess/RenderNodeAccess are also Known-good so the additive PRE-08 check
	// (issue #120) PASSes here too, rather than adding an un-superseded
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

	r := d.aggregate()
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

	r := d.aggregate()
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (offload StatusFail must dominate; supersession must NOT fire without proven residency)", r.Overall)
	}
}

// TestConfidentROCmFAILStillDominatesResidency is the CENTRAL no-false-green guard
// (DOCTOR-02) and proves the supersession keys on the (ID AND Status==WARN) CONJUNCTION,
// NOT ID-alone. Under PROVEN ROCm residency (Backend="rocm", OffloadApplies=true,
// Offload.Status==inference.StatusPass), a host whose probed linux-firmware is on the
// policy denylist yields a CONFIDENT FAIL on a SUPERSEDED ID — idROCmFirmware. A
// confident FAIL on one of the very IDs the supersession down-ranks at WARN must NEVER
// be swallowed → Overall=="FAIL". (A ROCM-PRE-gfx-style guard would NOT exercise this
// risk: gfx is not in the superseded set, so an ID-only match would never have
// swallowed it — the danger lives precisely on the superseded IDs, so the assertion
// lives there.) The denied stamp is the policy's own denylist entry, not a backend
// marker literal.
func TestConfidentROCmFAILStillDominatesResidency(t *testing.T) {
	d := rocmDoctorDeps() // proven residency: Backend=rocm, OffloadApplies, StatusPass
	probe := d.Probe
	d.Probe = func() detect.HostProfile {
		p := probe()
		p.FirmwareDate = detect.KnownStr("20251125", "test") // the denied linux-firmware build
		return p
	}

	r := d.aggregate()
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident FAIL on the superseded %s must NEVER be swallowed by residency-supersession — DOCTOR-02)", r.Overall, idROCmFirmware)
	}
	// The confident FAIL must still be present as a FAIL finding (not down-ranked).
	found := false
	for _, f := range r.Findings {
		if f.ID == idROCmFirmware && f.Status == "FAIL" {
			found = true
		}
	}
	if !found {
		t.Errorf("expected a FAIL finding on %s under proven residency; findings: %+v", idROCmFirmware, r.Findings)
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
	d.render(orchestrate.Unit{Name: inferenceUnitName(), Text: "x"})

	r := d.aggregate()
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

	r := d.aggregate()
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

// TestDriftWarn: a rendered unit that differs from the file on disk (and no offload
// FAIL) yields a drift WARN Finding and Report.Overall=="WARN" (DOCTOR-03).
func TestDriftWarn(t *testing.T) {
	d := newDoctorDeps()
	d.render(orchestrate.Unit{Name: inferenceUnitName(), Text: "drifted"})

	r := d.aggregate()
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

// TestDriftReadErrorDegrades: a unit dir that is missing (a never-installed host), cannot
// be examined, or cannot be rendered against must yield a typed-Unknown WARN Finding
// with remediation, never a panic, never drift and never a false PASS.
func TestDriftReadErrorDegrades(t *testing.T) {
	for _, tc := range []struct {
		name  string
		setup func(d *run)
	}{
		{"unit dir missing", func(d *run) { d.dirMissing = true }},
		{"unit dir unreadable", func(d *run) { d.dirErr = errors.New("stat unit dir: permission denied") }},
		{"render fails", func(d *run) {
			d.RenderUnits = func(config.VillaConfig, string) ([]orchestrate.Unit, error) {
				return nil, errors.New("render: unknown model")
			}
		}},
		{"a unit that cannot be read", func(d *run) {
			d.render(orchestrate.Unit{Name: "villa-openwebui.container", Text: "x"})
			read := d.ReadUnit
			d.ReadUnit = func(name string) ([]byte, error) {
				if name == "villa-openwebui.container" {
					return nil, errors.New("read: permission denied")
				}
				return read(name)
			}
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			// A unit that WOULD be drift if the dir were readable: the finding must not say so.
			d.render(orchestrate.Unit{Name: inferenceUnitName(), Text: "different"})
			tc.setup(d)

			r := d.aggregate()
			f, ok := findingByID(r, "drift")
			if !ok {
				t.Fatal("no drift finding")
			}
			if f.Status != "WARN" || f.Remediation == "" || f.Raw == "" {
				t.Errorf("drift = %s %q (remediation %q, raw %q), want a typed-Unknown WARN carrying its cause", f.Status, f.Detail, f.Remediation, f.Raw)
			}
			if strings.Contains(f.Detail, "no longer match") {
				t.Errorf("an unreadable unit dir was reported as drift: %q", f.Detail)
			}
			if r.Overall != "WARN" {
				t.Errorf("Overall = %q, want WARN", r.Overall)
			}
		})
	}
}

// TestDriftPlanTable is the unit drift plan's whole truth table against a fake unit dir:
// a rendered unit is unchanged when the file is byte-identical, drift when it differs and
// drift when it is absent (as orchestrate.Reconcile reads it), and the finding names
// every drifted unit.
func TestDriftPlanTable(t *testing.T) {
	llama := orchestrate.Unit{Name: inferenceUnitName(), Text: servedUnit}
	chat := orchestrate.Unit{Name: "villa-openwebui.container", Text: "[Container]\nImage=chat\n"}
	for _, tc := range []struct {
		name        string
		onDisk      map[string]string
		wantStatus  string
		wantChanged []string
	}{
		{"all match", map[string]string{llama.Name: llama.Text, chat.Name: chat.Text}, "PASS", nil},
		{"one edited", map[string]string{llama.Name: llama.Text, chat.Name: "[Container]\nImage=hand-edited\n"}, "WARN", []string{chat.Name}},
		{"one absent", map[string]string{llama.Name: llama.Text}, "WARN", []string{chat.Name}},
		{"every unit absent", map[string]string{}, "WARN", []string{llama.Name, chat.Name}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			d.units = tc.onDisk
			d.render(llama, chat)
			f, _ := findingByID(d.aggregate(), "drift")
			if f.Status != tc.wantStatus {
				t.Fatalf("drift = %s %q, want %s", f.Status, f.Detail, tc.wantStatus)
			}
			for _, name := range tc.wantChanged {
				if !strings.Contains(f.Detail, name) {
					t.Errorf("drift detail %q does not name %s", f.Detail, name)
				}
			}
			if tc.wantStatus == "PASS" && f.Remediation != "" {
				t.Errorf("a PASS drift carries remediation %q", f.Remediation)
			}
		})
	}
}

// TestDriftRendersWithTheInstalledBinaryPath pins the #141 precedence rule: the units are
// rendered with the villa path the INSTALLED villa-websafe unit mounts, and only when that
// unit is not installed with the running binary — so the same binary at another path is
// not drift.
func TestDriftRendersWithTheInstalledBinaryPath(t *testing.T) {
	websafe := orchestrate.WebsafeContainerUnitName()
	for _, tc := range []struct {
		name  string
		units map[string]string
		want  string
	}{
		{"websafe unit installed", map[string]string{websafe: websafeUnitMounting(t, "/opt/villa-elsewhere/villa")}, "/opt/villa-elsewhere/villa"},
		{"no websafe unit", map[string]string{}, "/usr/local/bin/villa"},
		{"websafe unit mounts no binary", map[string]string{websafe: "[Container]\nImage=x\n"}, "/usr/local/bin/villa"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			d.units = tc.units
			var got string
			d.RenderUnits = func(_ config.VillaConfig, hostVilla string) ([]orchestrate.Unit, error) {
				got = hostVilla
				return nil, nil
			}
			d.aggregate()
			if got != tc.want {
				t.Errorf("units rendered with the villa path %q, want %q", got, tc.want)
			}
		})
	}
}

// websafeUnitMounting renders the real villa-websafe unit for a villa binary at path, so
// the test reads the mount the way install wrote it.
func websafeUnitMounting(t *testing.T, path string) string {
	t.Helper()
	units, err := orchestrate.Render(orchestrate.RenderInput{
		Backend: inference.VulkanBackend(),
		Cfg: config.VillaConfig{
			Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
			WebSearchEnabled: true,
		},
		ModelFile:     "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir:     "/home/villa/.local/share/villa/models",
		HostVillaPath: path,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	for _, u := range units {
		if u.Name == orchestrate.WebsafeContainerUnitName() {
			return u.Text
		}
	}
	t.Fatal("fixture rendered no villa-websafe unit")
	return ""
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
func memoryDoctorDeps() *run {
	d := rocmDoctorDeps()
	d.cfg.MemoryEnabled = true
	d.cfg.EmbeddingModel = "test-embedder"
	d.StatusReport = func() status.Report { return memoryOnStatusReport() }
	d.RunMemoryChecks = func(detect.HostProfile, preflight.MemoryGateInput) []preflight.CheckResult {
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

// TestMemoryOffNoMemoryFindings: with memory off in the config (the
// memory-off default — mirror), Aggregate emits NO memory finding at all: no
// MEM-PRE-* checks, no MEM-DOC-residency (a proof never PASSes by default).
// Together with every pre-existing test in this file passing unchanged, this is the
// memory-off byte-identical guard.
func TestMemoryOffNoMemoryFindings(t *testing.T) {
	r := newDoctorDeps().aggregate()
	for _, id := range []string{"MEM-PRE-disk", "MEM-PRE-headroom", "MEM-DOC-residency"} {
		if hasFinding(r, id) {
			t.Errorf("memory-off Aggregate emitted finding %q (no PASS-by-default)", id)
		}
	}
	// NOTE: no Overall assertion here — the off-hardware vulkan fixture's host-prep
	// checks are typed-Unknown WARNs by construction (profile-dependent), so the
	// byte-identical memory-off guard is the absence of memory findings above PLUS
	// every pre-existing test in this file passing unchanged.
}

// TestCatalogGeometryNilSeamEmitsNothing: with the CatalogGeometry seam nil (the
// zero-value default), Aggregate emits no CAT-01 finding — a doctor that cannot
// read the catalog must not fabricate a verdict about it.
func TestCatalogGeometryNilSeamEmitsNothing(t *testing.T) {
	if r := newDoctorDeps().aggregate(); hasFinding(r, "CAT-01") {
		t.Error("a nil CatalogGeometry seam emitted a CAT-01 finding")
	}
}

// TestCatalogGeometryFoldedFailRaisesOverall: a CAT-01 FAIL from the seam folds
// through findingFromCheck like every other CheckResult and ranks worst-wins, so a
// catalog entry that no longer describes its file blocks a clean doctor verdict.
func TestCatalogGeometryFoldedFailRaisesOverall(t *testing.T) {
	d := rocmDoctorDeps()
	d.CatalogGeometry = func() []preflight.CheckResult {
		return []preflight.CheckResult{
			{ID: "CAT-01", Name: "catalog geometry: a", Tier: preflight.TierBlock,
				Status: preflight.StatusPass, Detail: "agrees", Provenance: "test"},
			{ID: "CAT-01", Name: "catalog geometry: b", Tier: preflight.TierBlock,
				Status: preflight.StatusFail, Detail: "catalog n_layers=48; header kv_layers=10",
				Remediation: "fix the b entry", Provenance: "test"},
		}
	}
	r := d.aggregate()
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident CAT-01 FAIL must rank worst-wins)", r.Overall)
	}
	n := 0
	for _, f := range r.Findings {
		if f.ID == "CAT-01" {
			n++
			if f.Status == "FAIL" && f.Remediation == "" {
				t.Error("a CAT-01 FAIL folded with an empty Remediation")
			}
		}
	}
	if n != 2 {
		t.Errorf("got %d CAT-01 findings, want 2 (one per checked entry)", n)
	}
}

// TestMemoryChecksFoldedFailRaisesOverall: a non-nil RunMemoryChecks seam has its
// CheckResults folded as findings via findingFromCheck and ranked worst-wins like
// every other check — a confident MEM-PRE-headroom FAIL raises Overall to FAIL.
func TestMemoryChecksFoldedFailRaisesOverall(t *testing.T) {
	d := memoryDoctorDeps()
	d.RunMemoryChecks = func(detect.HostProfile, preflight.MemoryGateInput) []preflight.CheckResult {
		return []preflight.CheckResult{
			{ID: "MEM-PRE-disk", Name: "Vector-index disk space", Tier: preflight.TierBlock,
				Status: preflight.StatusPass, Detail: "free disk ok", Provenance: "test"},
			{ID: "MEM-PRE-headroom", Name: "Embedder memory headroom", Tier: preflight.TierBlock,
				Status: preflight.StatusFail, Detail: "free memory below the embedding reservation",
				Remediation: "close memory-heavy processes", Provenance: "test"},
		}
	}

	r := d.aggregate()
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

	r := d.aggregate()
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

	r := d.aggregate()
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
	r := memoryDoctorDeps().aggregate()
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

	r := d.aggregate()
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

	r := d.aggregate()
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

	r := d.aggregate()
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
	r := newDoctorDeps().aggregate()
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
//   - ConfigKeyOnly → the same WARN, remediation naming the heal (ADR-0019).
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

	// ADR-0019: a key-only drift is still a read-only WARN, but its remediation names
	// the heal instead of asking the operator to review edits they never made.
	t.Run("config-drift-key-only", func(t *testing.T) {
		fs := agentDriftFindings(agent.DriftReport{ConfigDrift: true, ConfigKeyOnly: true, Reason: "stale key"})
		f, ok := hasID(fs, "agent-config-drift")
		if !ok || f.Status != statusWarn || f.Tier != tierWarn {
			t.Fatalf("ConfigKeyOnly → %+v, want WARN agent-config-drift", fs)
		}
		for _, want := range []string{"villa up", "villa code", "crush.json.bak"} {
			if !strings.Contains(f.Remediation, want) {
				t.Errorf("key-only remediation %q does not mention %q", f.Remediation, want)
			}
		}
		if strings.Contains(f.Remediation, "review your crush.json edits") {
			t.Errorf("key-only remediation %q asks the operator to review edits they never made", f.Remediation)
		}
		plain := agentDriftFindings(agent.DriftReport{ConfigDrift: true, Reason: "edited crush.json"})
		if p, _ := hasID(plain, "agent-config-drift"); strings.Contains(p.Remediation, "crush.json.bak") {
			t.Errorf("operator-edit remediation %q promises a heal villa will not perform", p.Remediation)
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

// cleanAgentDrift stages the raw reads of an installed, unedited agent: the pinned
// binary, and an on-disk crush.json equal to the rendered reference.
func (r *run) cleanAgentDrift() {
	pinned := agent.LoadCrushPolicy().Assets["linux/amd64"].BinarySHA256
	r.AgentBinarySHA = func() (string, bool, error) { return pinned, true, nil }
	r.RenderCrushConfig = func(config.VillaConfig) ([]byte, error) { return []byte(`{"a":1}`), nil }
	r.ReadCrushConfig = func() ([]byte, bool, error) { return []byte(`{"a":1}`), true, nil }
}

// agentDoctorDeps extends memoryDoctorDeps with the agent on and healthy: a PASS
// tool-call verdict, a PASS residency verdict, and reads that make a clean drift report.
func agentDoctorDeps() *run {
	d := memoryDoctorDeps()
	d.cfg.AgentEnabled = true
	d.AgentToolCall = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusPass, Detail: "tool-call round-trip completed"}
	}
	d.AgentResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusPass, Detail: "coder model resident under tool-call load"}
	}
	d.cleanAgentDrift()
	return d
}

// TestAgentOffNoAgentFindings: with the agent off in the config, Aggregate emits NO agent
// finding at all — never a PASS-by-default — and drives no agent proof. This is the
// agent-off byte-identical guard, and it holds by the config, not by an unbound seam.
func TestAgentOffNoAgentFindings(t *testing.T) {
	d := newDoctorDeps()
	d.AgentToolCall = func() inference.Verdict { t.Error("agent-off drove the tool-call proof"); return inference.Verdict{} }
	d.AgentResidencyUnderLoad = func() inference.Verdict {
		t.Error("agent-off drove the agent residency proof")
		return inference.Verdict{}
	}
	r := d.aggregate()
	for _, id := range []string{"agent-tool-call", "agent-residency", "agent-drift", "agent-binary-drift", "agent-config-drift"} {
		if hasFinding(r, id) {
			t.Errorf("agent-off Aggregate emitted finding %q (no PASS-by-default)", id)
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

	r := d.aggregate()
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

	r := d.aggregate()
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident agent residency FAIL must dominate a health-200)", r.Overall)
	}
	f, ok := findingByID(r, "agent-residency")
	if !ok || f.Status != statusFail || f.Tier != tierBlock {
		t.Errorf("agent-residency = %+v (found=%v), want a BLOCK-class FAIL", f, ok)
	}
}

// TestAgentDriftFoldedWarn: an installed binary whose hash is not the pinned one folds the
// drift findings worst-wins; the clean memory+agent stack drops to WARN (drift is a WARN,
// never auto-corrected).
func TestAgentDriftFoldedWarn(t *testing.T) {
	d := agentDoctorDeps()
	d.AgentBinarySHA = func() (string, bool, error) { return "0000not-the-pinned-hash", true, nil }

	r := d.aggregate()
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
	r := agentDoctorDeps().aggregate()
	if r.Overall != "PASS" {
		t.Fatalf("Overall = %q, want PASS (healthy agent-on stack, clean drift)", r.Overall)
	}
}

// --- issue #141: the websafe-binary finding (reportSchemaVersion 5→6) ---

// TestDoctorSchemaVersionIsEight: doctor's OWN --json contract self-version was
// bumped append-only 6→7 for the sandbox fold (SBX-01/SBX-02, issue #176) and
// 7→8 for the TMD-01 tools-mode drift finding (issue #173). The const is the single source of truth — Aggregate stamps it on every
// Report. INDEPENDENT of status's reportSchemaVersion (5).
func TestDoctorSchemaVersionIsEight(t *testing.T) {
	r := newDoctorDeps().aggregate()
	if r.SchemaVersion != 8 {
		t.Fatalf("Report.SchemaVersion = %d, want 8 (append-only bumps for the sandbox fold and TMD-01)", r.SchemaVersion)
	}
}

// --- issue #176: the sandbox fold (reportSchemaVersion 6→7) ---

// TestSandboxOffNoFindings proves the fold is gated on the config: with the workspace
// agent off Aggregate emits neither SBX finding and never calls the sandbox host gate.
func TestSandboxOffNoFindings(t *testing.T) {
	d := newDoctorDeps()
	d.RunSandboxChecks = func(detect.HostProfile) []preflight.CheckResult {
		t.Error("sandbox-off called the sandbox host gate")
		return nil
	}
	d.units[orchestrate.SandboxNetworkName()+".network"] = "[Network]\n"
	r := d.aggregate()
	for _, id := range []string{"PRE-09", "SBX-02"} {
		if hasFinding(r, id) {
			t.Errorf("sandbox-off Aggregate emitted finding %q (no PASS-by-default)", id)
		}
	}
}

// sandboxOnDeps is newDoctorDeps with the workspace agent on and a stack on disk that has
// the sandbox network and a proxy joined to it.
func sandboxOnDeps() *run {
	d := newDoctorDeps()
	d.cfg.WorkspaceAgent = true
	d.units[orchestrate.SandboxNetworkName()+".network"] = "[Network]\nInternal=true\n"
	d.units[orchestrate.InferproxyContainerUnitName()] = "[Container]\nNetwork=villa\nNetwork=" + orchestrate.SandboxNetworkName() + "\n"
	return d
}

// TestSandboxHostGatePASS proves SBX-01 folds a PRE-09 PASS through unchanged.
func TestSandboxHostGatePASS(t *testing.T) {
	d := sandboxOnDeps()
	d.RunSandboxChecks = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{{ID: "PRE-09", Name: "sandbox runtime", Tier: preflight.TierBlock, Status: preflight.StatusPass, Detail: "healthy"}}
	}
	r := d.aggregate()
	f, ok := findingByID(r, "PRE-09")
	if !ok {
		t.Fatal("PRE-09 finding missing")
	}
	if f.Status != "PASS" {
		t.Fatalf("PRE-09 finding Status = %q, want PASS", f.Status)
	}
}

// TestSandboxHostGateFAILRaisesOverall proves a confident PRE-09 FAIL folds
// worst-wins and blocks the report, mirroring TestMemoryChecksFoldedFailRaisesOverall.
func TestSandboxHostGateFAILRaisesOverall(t *testing.T) {
	d := sandboxOnDeps()
	d.RunSandboxChecks = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{{ID: "PRE-09", Name: "sandbox runtime", Tier: preflight.TierBlock, Status: preflight.StatusFail, Detail: "krun missing", Remediation: "install packages"}}
	}
	r := d.aggregate()
	if r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a confident PRE-09 FAIL must dominate)", r.Overall)
	}
}

// TestSandboxHostGateWARNDegrades proves an unevaluable PRE-09 (podman
// unreachable) folds to WARN, not FAIL.
func TestSandboxHostGateWARNDegrades(t *testing.T) {
	d := sandboxOnDeps()
	d.RunSandboxChecks = func(detect.HostProfile) []preflight.CheckResult {
		return []preflight.CheckResult{{ID: "PRE-09", Name: "sandbox runtime", Tier: preflight.TierBlock, Status: preflight.StatusWarn, Detail: "podman unreachable", Remediation: "check podman"}}
	}
	r := d.aggregate()
	if r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN", r.Overall)
	}
}

// TestSandboxNetworkFindingMatrix covers the three SBX-02 outcomes: present +
// joined → PASS; network unit missing → FAIL; unit present but the inference
// proxy unit not joined → FAIL. Every non-PASS carries a remediation.
func TestSandboxNetworkFindingMatrix(t *testing.T) {
	tests := []struct {
		name           string
		networkPresent bool
		proxyJoined    bool
		wantStatus     string
	}{
		{"present and joined", true, true, "PASS"},
		{"network missing", false, false, "FAIL"},
		{"present but not joined", true, false, "FAIL"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := sandboxNetworkFinding(tt.networkPresent, tt.proxyJoined)
			if f.Status != tt.wantStatus {
				t.Errorf("Status = %q, want %q (detail=%q)", f.Status, tt.wantStatus, f.Detail)
			}
			if f.Status != "PASS" && f.Remediation == "" {
				t.Error("non-PASS SBX-02 finding must carry a Remediation")
			}
			if f.ID != "SBX-02" || f.Tier != tierBlock {
				t.Errorf("ID/Tier = %q/%q, want SBX-02/BLOCK", f.ID, f.Tier)
			}
		})
	}
}

// TestSandboxNetworkScan is SBX-02's scan of the unit dir, joined or not: it needs the
// network unit on disk AND the villa-inferproxy unit carrying the sandbox network. The
// proxy, not villa-llama, is the joined unit since ADR-0011, so a stack whose llama unit
// carries the line but whose proxy does not is broken, and one where only the proxy
// carries it is healthy. A dir that cannot be resolved is a typed-Unknown WARN, never a
// fabricated FAIL.
func TestSandboxNetworkScan(t *testing.T) {
	network := orchestrate.SandboxNetworkName() + ".network"
	proxy := orchestrate.InferproxyContainerUnitName()
	join := "Network=" + orchestrate.SandboxNetworkName() + "\n"
	for _, tc := range []struct {
		name       string
		setup      func(d *run)
		wantStatus string
		wantDetail string
	}{
		{"joined", func(*run) {}, "PASS", "joined"},
		{"only the proxy carries the line", func(d *run) {
			d.units[inferenceUnitName()] = servedUnit // villa-llama never joins the sandbox network
		}, "PASS", "joined"},
		{"proxy not joined", func(d *run) { d.units[proxy] = "[Container]\nNetwork=villa\n" }, "FAIL", "not joined"},
		{"the line is on the wrong unit", func(d *run) {
			d.units[proxy] = "[Container]\nNetwork=villa\n"
			d.units[inferenceUnitName()] = "[Container]\n" + join
		}, "FAIL", "not joined"},
		{"proxy unit absent", func(d *run) { delete(d.units, proxy) }, "FAIL", "not joined"},
		{"network unit absent", func(d *run) { delete(d.units, network) }, "FAIL", "not on disk"},
		{"unit dir missing", func(d *run) { d.units = map[string]string{}; d.dirMissing = true }, "FAIL", "not on disk"},
		{"unit dir unresolvable", func(d *run) { d.dirErr = errors.New("no config dir") }, "WARN", "could not evaluate"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := sandboxOnDeps()
			tc.setup(d)
			f, ok := findingByID(d.aggregate(), "SBX-02")
			if !ok {
				t.Fatal("no SBX-02 finding")
			}
			if f.Status != tc.wantStatus || !strings.Contains(f.Detail, tc.wantDetail) {
				t.Errorf("SBX-02 = %s %q, want %s mentioning %q", f.Status, f.Detail, tc.wantStatus, tc.wantDetail)
			}
			if f.Status != "PASS" && f.Remediation == "" {
				t.Error("non-PASS SBX-02 carries no remediation")
			}
		})
	}
}

// TestSandboxNetworkFAILRaisesOverall proves a confident SBX-02 FAIL folds
// worst-wins and blocks the report, and that an unevaluable one only warns.
func TestSandboxNetworkFAILRaisesOverall(t *testing.T) {
	d := sandboxOnDeps()
	delete(d.units, orchestrate.SandboxNetworkName()+".network")
	if r := d.aggregate(); r.Overall != "FAIL" {
		t.Fatalf("Overall = %q, want FAIL (a missing sandbox network must dominate)", r.Overall)
	}
	d = sandboxOnDeps()
	d.dirErr = errors.New("read unit dir: not found")
	if r := d.aggregate(); r.Overall != "WARN" {
		t.Fatalf("Overall = %q, want WARN (unevaluable read, never a fabricated FAIL)", r.Overall)
	}
}

// recorded is a verify-search result stamped at the given age.
func recorded(verdict string, age time.Duration) *verifystate.State {
	return &verifystate.State{Verdict: verdict, CheckedAt: time.Now().Add(-age).UTC().Format(time.RFC3339)}
}

// webSearchOn is newDoctorDeps with web search on and a fresh verify PASS on record.
func webSearchOn() *run {
	d := newDoctorDeps()
	d.cfg.WebSearchEnabled = true
	d.ReadVerifyState = func() *verifystate.State { return recorded("PASS", time.Hour) }
	return d
}

// TestAggregateWebSearch proves the web-search fold is gated on the config: web off (the
// newDoctorDeps default) emits NO web-search finding and drives no search proof, however
// the last verify went (byte-identical, no PASS-by-default); web on emits the egress
// finding from the recorded verify result and the residency finding from its proof; and
// a fresh verify that did not pass fails doctor.
func TestAggregateWebSearch(t *testing.T) {
	t.Run("web-off-no-findings", func(t *testing.T) {
		d := newDoctorDeps()
		d.ReadVerifyState = func() *verifystate.State { return recorded("FAIL", time.Hour) }
		d.SearchResidencyUnderLoad = func() inference.Verdict {
			t.Error("web-off drove the search-residency proof")
			return inference.Verdict{}
		}
		r := d.aggregate()
		for _, id := range []string{"search-egress", "search-residency", "websafe-binary"} {
			if hasFinding(r, id) {
				t.Errorf("web-off Aggregate emitted finding %q (no PASS-by-default)", id)
			}
		}
	})
	t.Run("web-on-findings-present", func(t *testing.T) {
		r := webSearchOn().aggregate()
		if f, ok := findingByID(r, "search-egress"); !ok || f.Status != statusPass {
			t.Errorf("search-egress = %+v (found=%v), want a PASS from the recorded verify PASS", f, ok)
		}
		if _, ok := findingByID(r, "search-residency"); !ok {
			t.Errorf("search-residency finding missing with web search on")
		}
	})
	t.Run("failed-verify-fails-doctor", func(t *testing.T) {
		d := webSearchOn()
		d.ReadVerifyState = func() *verifystate.State { return recorded("FAIL", time.Hour) }
		if r := d.aggregate(); r.Overall != statusFail {
			t.Errorf("Overall = %q, want FAIL: the last verify search did not pass", r.Overall)
		}
	})
}

// TestSearchEgressFinding is the truth table for the egress mapper over the recorded
// verify result and whether the status report evaluated (#266): a fresh PASS → PASS, no
// remediation; a fresh non-PASS → a BLOCK-class FAIL naming the verdict; anything else
// (unreadable, absent, stale, future-dated, undated) → a typed-Unknown WARN that says
// which, never a PASS. The report's failure never changes the answer; it is named on
// the WARN.
func TestSearchEgressFinding(t *testing.T) {
	reportErr := errors.New("model not in catalog")
	cases := []struct {
		name       string
		st         *verifystate.State
		reportErr  error
		wantTier   string
		wantStatus string
		wantDetail []string
	}{
		{"fresh pass", recorded("PASS", time.Hour), nil, tierBlock, statusPass, []string{"PASS"}},
		{"fresh pass, report failed", recorded("PASS", time.Hour), reportErr, tierBlock, statusPass, []string{"PASS"}},
		{"fresh fail", recorded("FAIL", time.Hour), nil, tierBlock, statusFail, []string{"did not pass", "FAIL"}},
		{"fresh reject, report failed", recorded("REJECT", time.Hour), reportErr, tierBlock, statusFail, []string{"REJECT"}},
		{"stale pass", recorded("PASS", 72*time.Hour), nil, tierWarn, statusWarn, []string{"stale"}},
		{"future-dated pass", recorded("PASS", -72*time.Hour), nil, tierWarn, statusWarn, []string{"stale"}},
		{"nothing recorded", &verifystate.State{}, nil, tierWarn, statusWarn, []string{"no `villa verify search` result"}},
		{"unreadable", nil, nil, tierWarn, statusWarn, []string{"could not be read"}},
		{"stale, report failed", recorded("PASS", 72*time.Hour), reportErr, tierWarn, statusWarn, []string{"stale", "model not in catalog"}},
		{"unreadable, report failed", nil, reportErr, tierWarn, statusWarn, []string{"could not be read", "model not in catalog"}},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := searchEgressFinding(c.st, c.reportErr)
			if f.ID != "search-egress" {
				t.Errorf("ID = %q, want search-egress", f.ID)
			}
			if f.Tier != c.wantTier || f.Status != c.wantStatus {
				t.Errorf("(tier %s, status %s), want (%s, %s); detail %q", f.Tier, f.Status, c.wantTier, c.wantStatus, f.Detail)
			}
			for _, want := range c.wantDetail {
				if !strings.Contains(f.Detail, want) {
					t.Errorf("detail %q does not mention %q", f.Detail, want)
				}
			}
			if f.Status != statusPass && f.Remediation == "" {
				t.Errorf("non-PASS egress finding has empty Remediation")
			}
			if f.Status == statusPass && f.Remediation != "" {
				t.Errorf("PASS egress finding carries a Remediation %q", f.Remediation)
			}
		})
	}
}

// TestSearchEgressSurvivesAnErroredReport pins #266 at Aggregate: the egress finding does
// not depend on the status report evaluating, so a fresh failed verify is a FAIL even
// while the report is one typed-Unknown "stack" WARN.
func TestSearchEgressSurvivesAnErroredReport(t *testing.T) {
	d := webSearchOn()
	d.ReadVerifyState = func() *verifystate.State { return recorded("FAIL", time.Hour) }
	d.StatusReport = func() status.Report {
		sd, err := status.StubDeps(t.TempDir(), nil)
		if err != nil {
			t.Fatalf("StubDeps: %v", err)
		}
		sd.LoadConfig = func() (config.VillaConfig, error) { return config.VillaConfig{}, errors.New("config unreadable") }
		return status.Run(sd)
	}
	r := d.aggregate()
	if f, ok := findingByID(r, "stack"); !ok || f.Status != statusWarn {
		t.Fatalf("stack = %+v (found=%v), want the typed-Unknown WARN for the errored report", f, ok)
	}
	f, ok := findingByID(r, "search-egress")
	if !ok || f.Status != statusFail {
		t.Fatalf("search-egress = %+v (found=%v), want a FAIL despite the errored report", f, ok)
	}
	if r.Overall != statusFail {
		t.Errorf("Overall = %q, want FAIL", r.Overall)
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
		{"not-in-flight", inference.Verdict{Status: inference.StatusWarn, Detail: "no chat round stayed in flight long enough to sample"}, tierWarn, statusWarn, true},
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
	d := webSearchOn() // healthy HTTP-200 stack
	d.SearchResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "the chat model fell back to CPU under search load"}
	}
	r := d.aggregate()
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
	d := webSearchOn()
	d.ReadVerifyState = func() *verifystate.State { return recorded("PASS", 72*time.Hour) }
	d.SearchResidencyUnderLoad = func() inference.Verdict {
		return inference.Verdict{Status: inference.StatusFail, Detail: "fell back to CPU under search load"}
	}
	r := d.aggregate()
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

// TestDriftDetailNamesChangedUnits: the drift WARN Detail NAMES every unit in
// Plan.Changed, in plan order (issue #141). A drift verdict that names nothing leaves
// the operator with nothing to diff, which is what made a moved-binary false drift
// expensive to trace.
func TestDriftDetailNamesChangedUnits(t *testing.T) {
	d := newDoctorDeps()
	d.render(
		orchestrate.Unit{Name: "villa-llama.container", Text: "drifted"},
		orchestrate.Unit{Name: "villa-websafe.container", Text: "drifted"},
	)

	f, ok := findingByID(d.aggregate(), "drift")
	if !ok {
		t.Fatal("no drift finding in the report")
	}
	want := "on-disk Quadlet units no longer match the rendered-from-config units: villa-llama.container, villa-websafe.container"
	if f.Detail != want {
		t.Errorf("drift Detail = %q, want %q", f.Detail, want)
	}
}

// TestWebsafeBinaryFinding: a moved binary is its OWN WARN, not drift (issue #141).
// Doctor reads what the installed villa-websafe unit mounts and compares it with the
// running binary: equal paths PASS, different paths WARN with remediation, an absent
// unit emits NOTHING rather than a PASS-by-default, and web search off emits nothing at
// all even when the unit is on disk.
func TestWebsafeBinaryFinding(t *testing.T) {
	websafe := orchestrate.WebsafeContainerUnitName()
	t.Run("same-path-passes", func(t *testing.T) {
		d := webSearchOn()
		d.units[websafe] = websafeUnitMounting(t, "/usr/local/bin/villa")
		r := d.aggregate()
		f, ok := findingByID(r, "websafe-binary")
		if !ok {
			t.Fatal("no websafe-binary finding")
		}
		if f.Status != statusPass || f.Tier != tierWarn {
			t.Errorf("(status %s, tier %s), want (PASS, WARN)", f.Status, f.Tier)
		}
		if f.Detail != "villa-websafe mounts the running villa binary (/usr/local/bin/villa)" {
			t.Errorf("Detail = %q", f.Detail)
		}
	})

	t.Run("moved-binary-warns", func(t *testing.T) {
		d := webSearchOn()
		d.units[websafe] = websafeUnitMounting(t, "/home/villa/.local/bin/villa")
		r := d.aggregate()
		f, ok := findingByID(r, "websafe-binary")
		if !ok {
			t.Fatal("no websafe-binary finding")
		}
		if f.Status != statusWarn || f.Tier != tierWarn {
			t.Errorf("(status %s, tier %s), want (WARN, WARN)", f.Status, f.Tier)
		}
		if f.Detail != "the running villa (/usr/local/bin/villa) is not the binary villa-websafe mounts (/home/villa/.local/bin/villa)" {
			t.Errorf("Detail = %q", f.Detail)
		}
		if f.Remediation == "" {
			t.Error("a WARN websafe-binary finding must carry remediation")
		}
		if drift, _ := findingByID(r, "drift"); drift.Status != statusPass {
			t.Errorf("drift = %s, want PASS (a moved binary is its own finding, not drift)", drift.Status)
		}
	})

	t.Run("unit-absent-emits-nothing", func(t *testing.T) {
		if _, ok := findingByID(webSearchOn().aggregate(), "websafe-binary"); ok {
			t.Error("an uninstalled villa-websafe unit must emit no websafe-binary finding")
		}
	})

	t.Run("web-off-emits-nothing", func(t *testing.T) {
		d := newDoctorDeps()
		d.units[websafe] = websafeUnitMounting(t, "/home/villa/.local/bin/villa")
		if _, ok := findingByID(d.aggregate(), "websafe-binary"); ok {
			t.Error("web search off must emit no websafe-binary finding")
		}
	})
}

// TestSubsystemGating is the gating truth table: each optional subsystem's findings exist
// iff the config the run loaded turns it on, and its proof is driven then and only then.
// Every seam is wired in both rows (as liveDoctorDeps wires them), so nothing here is
// gated by an unbound seam.
func TestSubsystemGating(t *testing.T) {
	type subsystemCase struct {
		name    string
		turnOn  func(cfg *config.VillaConfig)
		proofs  func(d *run, drove *[]string)
		wantIDs []string
	}
	record := func(drove *[]string, name string) func() inference.Verdict {
		return func() inference.Verdict {
			*drove = append(*drove, name)
			return passVerdict()
		}
	}
	for _, tc := range []subsystemCase{
		{"memory", func(c *config.VillaConfig) { c.MemoryEnabled = true },
			func(d *run, drove *[]string) { d.ResidencyUnderLoad = record(drove, "embed") },
			[]string{"MEM-DOC-residency"}},
		{"agent", func(c *config.VillaConfig) { c.AgentEnabled = true },
			func(d *run, drove *[]string) {
				d.AgentToolCall = record(drove, "tool-call")
				d.AgentResidencyUnderLoad = record(drove, "agent-residency")
			},
			[]string{"agent-tool-call", "agent-residency", "agent-drift"}},
		{"web search", func(c *config.VillaConfig) { c.WebSearchEnabled = true },
			func(d *run, drove *[]string) { d.SearchResidencyUnderLoad = record(drove, "search") },
			[]string{"search-residency", "search-egress"}},
		{"sandbox", func(c *config.VillaConfig) { c.WorkspaceAgent = true },
			func(d *run, drove *[]string) {
				d.RunSandboxChecks = func(detect.HostProfile) []preflight.CheckResult {
					*drove = append(*drove, "sandbox")
					return nil
				}
			},
			[]string{"SBX-02"}},
	} {
		t.Run(tc.name+" off", func(t *testing.T) {
			d := newDoctorDeps()
			var drove []string
			tc.proofs(d, &drove)
			r := d.aggregate()
			for _, id := range tc.wantIDs {
				if hasFinding(r, id) {
					t.Errorf("%s is off but the report carries %s", tc.name, id)
				}
			}
			if len(drove) != 0 {
				t.Errorf("%s is off but its proof ran: %v", tc.name, drove)
			}
		})
		t.Run(tc.name+" on", func(t *testing.T) {
			d := newDoctorDeps()
			tc.turnOn(&d.cfg)
			var drove []string
			tc.proofs(d, &drove)
			r := d.aggregate()
			for _, id := range tc.wantIDs {
				if !hasFinding(r, id) {
					t.Errorf("%s is on but the report carries no %s", tc.name, id)
				}
			}
			if len(drove) == 0 {
				t.Errorf("%s is on but its proof never ran", tc.name)
			}
		})
	}
}

// TestMemoryGateInput pins what doctor hands the memory host gate: the configured
// embedding model, and EmbedderActive only when the embedder's service is verifiably
// active (an error or any other state keeps the strict pre-install semantics, so a
// probe failure cannot excuse a missing headroom reservation).
func TestMemoryGateInput(t *testing.T) {
	for _, tc := range []struct {
		name  string
		state string
		err   error
		want  bool
	}{
		{"active", "active", nil, true},
		{"inactive", "inactive", nil, false},
		{"unreadable", "", errors.New("systemctl missing"), false},
		{"errored but says active", "active", errors.New("exit 3"), false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := memoryDoctorDeps()
			var asked string
			d.IsActive = func(unit string) (string, error) { asked = unit; return tc.state, tc.err }
			var got preflight.MemoryGateInput
			d.RunMemoryChecks = func(_ detect.HostProfile, in preflight.MemoryGateInput) []preflight.CheckResult {
				got = in
				return nil
			}
			d.aggregate()
			if asked != "villa-embed.service" {
				t.Errorf("asked about %q, want the embedder service villa-embed.service", asked)
			}
			if got.EmbedderActive != tc.want || got.EmbeddingModel != "test-embedder" {
				t.Errorf("gate input = {model %q, active %v}, want {test-embedder, %v}", got.EmbeddingModel, got.EmbedderActive, tc.want)
			}
		})
	}
}

// TestToolsDriftReadsTheServedUnit is TMD-01 through the unit dir: the served unit must
// carry the tool-calling flag iff tools mode (or coding mode) is on. It is not
// subsystem-gated, so every row emits the finding. An unreadable unit or a backend that
// yields no token is a typed-Unknown WARN, never a match.
func TestToolsDriftReadsTheServedUnit(t *testing.T) {
	token, err := toolsFlagToken("vulkan")
	if err != nil {
		t.Fatalf("toolsFlagToken: %v", err)
	}
	withFlag := "[Container]\nExec=llama-server -m x " + token + " --port 8080\n"
	for _, tc := range []struct {
		name       string
		setup      func(d *run)
		wantStatus string
		wantDetail string
	}{
		{"off, unit without the flag", func(*run) {}, statusPass, "matches tools mode (off)"},
		{"tools mode on, unit with the flag", func(d *run) {
			d.cfg.ToolsMode = true
			d.units[inferenceUnitName()] = withFlag
		}, statusPass, "matches tools mode (on)"},
		{"coding mode counts as tools on", func(d *run) {
			d.cfg.CodingMode = true
			d.units[inferenceUnitName()] = withFlag
		}, statusPass, "matches tools mode (on)"},
		{"tools mode on, unit without the flag", func(d *run) { d.cfg.ToolsMode = true }, statusFail, "every tool call will fail"},
		{"tools mode off, unit with the flag", func(d *run) { d.units[inferenceUnitName()] = withFlag }, statusFail, "nobody asked for"},
		{"unit absent", func(d *run) { delete(d.units, inferenceUnitName()) }, statusWarn, "could not read"},
		{"unit dir missing", func(d *run) { d.units = map[string]string{} }, statusWarn, "could not read"},
		{"backend that renders no flag token", func(d *run) { d.cfg.Backend = "nvidia" }, statusWarn, "could not read"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			tc.setup(d)
			r := d.aggregate()
			f, ok := findingByID(r, "TMD-01")
			if !ok {
				t.Fatal("TMD-01 absent from the report")
			}
			if f.Status != tc.wantStatus || !strings.Contains(f.Detail, tc.wantDetail) {
				t.Errorf("TMD-01 = %s %q, want %s mentioning %q", f.Status, f.Detail, tc.wantStatus, tc.wantDetail)
			}
			if f.Status == statusFail && r.Overall != statusFail {
				t.Errorf("Overall = %q, want a confident tools-mode drift to fold FAIL", r.Overall)
			}
		})
	}
}

// TestToolsFlagTokenIsDerivedFromTheSeam guards that the token TMD-01 looks for comes
// from the inference seam rather than being typed here: it must be exactly the one
// argument ContainerArgs adds when RunSpec.Tools flips on, for every backend.
func TestToolsFlagTokenIsDerivedFromTheSeam(t *testing.T) {
	for _, name := range []string{"rocm", "rocm-6.4.4", "rocm-6.4.4-rocwmma", "vulkan"} {
		t.Run(name, func(t *testing.T) {
			token, err := toolsFlagToken(name)
			if err != nil {
				t.Fatalf("toolsFlagToken(%q): %v", name, err)
			}
			if !strings.HasPrefix(token, "--") {
				t.Errorf("token = %q, want a llama-server long flag", token)
			}
			b, err := inference.BackendFor(name)
			if err != nil {
				t.Fatalf("BackendFor(%q): %v", name, err)
			}
			for _, a := range b.ContainerArgs(inference.RunSpec{}) {
				if a == token {
					t.Fatalf("the tools-off args already carry %q, so it cannot identify tools mode", token)
				}
			}
		})
	}
}

// TestToolsFlagTokenRefusesAnUnknownBackend guards that an unresolvable backend is an
// error, never a token that would silently make every unit look drift-free.
func TestToolsFlagTokenRefusesAnUnknownBackend(t *testing.T) {
	if _, err := toolsFlagToken("nvidia"); err == nil {
		t.Error("an unknown backend returned a token, want an error")
	}
}

// TestUnitCarriesToolsFlag guards that the drift check reads the Exec line as
// arguments: a token in a comment, or one that is only a substring of another
// argument, is not a served flag.
func TestUnitCarriesToolsFlag(t *testing.T) {
	token, err := toolsFlagToken("rocm")
	if err != nil {
		t.Fatalf("toolsFlagToken: %v", err)
	}
	for _, tc := range []struct {
		name string
		unit string
		want bool
	}{
		{"served", "[Container]\nExec=llama-server -m x " + token + " --port 8080\n", true},
		{"absent", "[Container]\nExec=llama-server -m x --port 8080\n", false},
		{"only in a comment", "# " + token + "\n[Container]\nExec=llama-server -m x\n", false},
		{"substring of another argument", "[Container]\nExec=llama-server -m x " + token + "-extra\n", false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if got := unitCarriesToolsFlag([]byte(tc.unit), token); got != tc.want {
				t.Errorf("unitCarriesToolsFlag = %v, want %v", got, tc.want)
			}
		})
	}
}

// TestAgentDriftReads is the agent drift decision over raw reads: a clean install is one
// PASS; each read failure is a typed-Unknown WARN carrying its cause (never a fabricated
// drift or a PASS); a binary whose hash is not the pinned one, an absent binary and an
// edited crush.json are WARN-with-remediation; and a first run with no crush.json yet is
// no config drift.
func TestAgentDriftReads(t *testing.T) {
	pinned := agent.LoadCrushPolicy().Assets["linux/amd64"].BinarySHA256
	for _, tc := range []struct {
		name    string
		setup   func(d *run)
		wantIDs []string
		reason  string
	}{
		{"clean", func(*run) {}, []string{"agent-drift"}, ""},
		{"render fails", func(d *run) {
			d.RenderCrushConfig = func(config.VillaConfig) ([]byte, error) { return nil, errors.New("no lsp") }
		}, []string{"agent-binary-drift"}, "could not render the reference crush.json"},
		{"hash fails", func(d *run) {
			d.AgentBinarySHA = func() (string, bool, error) { return "", false, errors.New("permission denied") }
		}, []string{"agent-binary-drift"}, "could not hash the villa-owned Crush binary"},
		{"crush.json unreadable", func(d *run) {
			d.ReadCrushConfig = func() ([]byte, bool, error) { return nil, false, errors.New("io error") }
		}, []string{"agent-binary-drift"}, "could not read the on-disk crush.json"},
		{"binary absent", func(d *run) {
			d.AgentBinarySHA = func() (string, bool, error) { return "", false, nil }
		}, []string{"agent-binary-drift"}, "not installed"},
		{"binary hash differs", func(d *run) {
			d.AgentBinarySHA = func() (string, bool, error) { return pinned + "x", true, nil }
		}, []string{"agent-binary-drift"}, "does not match the pinned policy"},
		{"crush.json edited", func(d *run) {
			d.ReadCrushConfig = func() ([]byte, bool, error) { return []byte(`{"a":2}`), true, nil }
		}, []string{"agent-config-drift"}, "differs from what villa would render"},
		{"crush.json whitespace only", func(d *run) {
			d.ReadCrushConfig = func() ([]byte, bool, error) { return []byte("{ \"a\" : 1 }\n"), true, nil }
		}, []string{"agent-drift"}, ""},
		{"first run, no crush.json", func(d *run) {
			d.ReadCrushConfig = func() ([]byte, bool, error) { return nil, false, nil }
		}, []string{"agent-drift"}, ""},
	} {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			d.cfg.AgentEnabled = true
			tc.setup(d)
			r := d.aggregate()
			for _, id := range tc.wantIDs {
				f, ok := findingByID(r, id)
				if !ok {
					t.Fatalf("no %s finding; findings %+v", id, r.Findings)
				}
				if id != "agent-drift" && (f.Status != statusWarn || f.Remediation == "") {
					t.Errorf("%s = %s (remediation %q), want a WARN with remediation", id, f.Status, f.Remediation)
				}
				if tc.reason != "" && !strings.Contains(f.Detail, tc.reason) {
					t.Errorf("%s detail %q does not mention %q", id, f.Detail, tc.reason)
				}
			}
		})
	}
}

// TestUpdatesFindingReadsTheRecordedCheck is issue #216: doctor reports the last
// recorded update check the way status does, and never fetches one. Never checked
// and a month-old check are WARN with the on-command remediation; a recent one is
// PASS naming its age.
func TestUpdatesFindingReadsTheRecordedCheck(t *testing.T) {
	days := func(n int) *int { return &n }
	cases := []struct {
		name       string
		updates    status.UpdatesInfo
		wantStatus string
		wantDetail string
	}{
		{"never checked", status.UpdatesInfo{State: status.UpdatesNeverChecked}, "WARN", "villa has never checked for updates on this host"},
		{"today", status.UpdatesInfo{State: status.UpdatesChecked, CheckedAt: "2026-09-11T12:26:35Z", AgeDays: days(0)}, "PASS", "last checked today (2026-09-11T12:26:35Z)"},
		{"three days", status.UpdatesInfo{State: status.UpdatesChecked, CheckedAt: "2026-09-08T09:00:00Z", AgeDays: days(3)}, "PASS", "last checked 3 days ago (2026-09-08T09:00:00Z)"},
		{"stale", status.UpdatesInfo{State: status.UpdatesChecked, CheckedAt: "2026-08-01T09:00:00Z", AgeDays: days(41)}, "WARN", "last checked 41 days ago (2026-08-01T09:00:00Z); the recorded answer is no longer evidence about today"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			d := newDoctorDeps()
			rep := healthyStatusReport()
			rep.Updates = tc.updates
			d.StatusReport = func() status.Report { return rep }

			r := d.aggregate()

			var f *Finding
			for i := range r.Findings {
				if r.Findings[i].ID == "UPD-01" {
					f = &r.Findings[i]
				}
			}
			if f == nil {
				t.Fatalf("no UPD-01 finding in %+v", r.Findings)
			}
			if f.Status != tc.wantStatus || f.Detail != tc.wantDetail {
				t.Errorf("UPD-01 = %s %q, want %s %q", f.Status, f.Detail, tc.wantStatus, tc.wantDetail)
			}
			if tc.wantStatus == "WARN" && f.Remediation != "run `villa update --check`" {
				t.Errorf("WARN remediation = %q, want the on-command check", f.Remediation)
			}
			if tc.wantStatus == "PASS" && f.Remediation != "" {
				t.Errorf("PASS carries remediation %q", f.Remediation)
			}
		})
	}
}
