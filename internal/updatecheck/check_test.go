// Package updatecheck tests cover the two things this verb is judged on: the
// rebuilt-versus-new-version distinction, and the Reject that must not read as
// "you are up to date".
//
// Everything here is pure. There is no network to fake because the report is
// derived from a config, a resolver and a verdict, all of them values.
package updatecheck

import (
	"encoding/json"
	"strings"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/manifest"
	"github.com/MatrixMagician/VillaStraylight/internal/manifestverify"
	"github.com/MatrixMagician/VillaStraylight/internal/pinresolve"
	"github.com/MatrixMagician/VillaStraylight/internal/pins"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
)

// fullConfig turns every addon on, so a report covers every subsystem row.
func fullConfig() config.VillaConfig {
	return config.VillaConfig{
		Backend:          "vulkan",
		MemoryEnabled:    true,
		WebSearchEnabled: true,
		AgentEnabled:     true,
	}
}

// acceptedVerdict wraps a document as if the verifier had accepted it.
func acceptedVerdict(mutate func(*manifest.Document)) manifestverify.Verdict {
	doc := manifest.FromTable(11, time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC))
	if mutate != nil {
		mutate(&doc)
	}
	return manifestverify.Verdict{Outcome: manifestverify.Accepted, Doc: doc}
}

// checkWith runs a check over a config and a verdict, with no recorded pin state.
func checkWith(cfg config.VillaConfig, v manifestverify.Verdict) Report {
	return Check(Input{
		Cfg:          cfg,
		Resolver:     pinresolve.New(pinstate.State{}),
		Verdict:      v,
		CheckedAt:    "2026-08-26T12:00:00Z",
		VillaVersion: "v1.7",
	})
}

// TestEverythingCurrentReportsNoUpdates: a manifest offering exactly the vetted
// pins means there is nothing to do, and every row must say so.
func TestEverythingCurrentReportsNoUpdates(t *testing.T) {
	r := checkWith(fullConfig(), acceptedVerdict(nil))

	if r.Result != ResultChecked {
		t.Fatalf("result = %q, want checked", r.Result)
	}
	if r.Summary == nil {
		t.Fatal("a completed check carries no summary")
	}
	if r.Summary.Updatable != 0 {
		t.Errorf("%d subsystems reported updatable against a manifest offering the vetted pins", r.Summary.Updatable)
	}
	for _, s := range r.Subsystems {
		for _, c := range s.Components {
			if c.Change != ChangeNone || c.Available != "" {
				t.Errorf("%s/%s reports change %q against its own vetted pin", s.Name, c.Name, c.Change)
			}
		}
	}
}

// TestARebuiltVersionTagIsNotANewVersion is the distinction the whole verb exists
// to preserve.
//
// `rocm-7.2.4` moving digest means THE SAME DECLARED VERSION WAS REBUILT upstream:
// the image villa validated on hardware is no longer the image that tag names,
// while nothing about "ROCm 7.2.4" changed. Reporting that as a new version would
// be false, and flattening both into "update available" is the dishonest shortcut.
func TestARebuiltVersionTagIsNotANewVersion(t *testing.T) {
	entry, _ := pins.Lookup(pins.BackendVulkan)
	_ = entry

	// Move the Qdrant digest while keeping its declared version.
	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			if d.Components[i].ID == string(pins.Qdrant) {
				name, _, _ := strings.Cut(d.Components[i].Ref, "@sha256:")
				d.Components[i].Ref = name + "@sha256:1111111111111111111111111111111111111111111111111111111111111111"
			}
		}
	})

	r := checkWith(fullConfig(), v)
	c := findComponent(t, r, string(pins.Qdrant))
	if c.Change != ChangeRebuilt {
		t.Errorf("a moved digest on an unchanged version tag reported %q, want rebuilt — the same declared version was rebuilt, not bumped", c.Change)
	}
	if c.PinShape != string(pins.VersionTag) {
		t.Errorf("pin_shape = %q; a consumer needs it to see WHY this is not a bump", c.PinShape)
	}
}

// TestAChangedVersionTagIsANewVersion: the other half of the distinction. When the
// declared version itself moves, it IS a bump.
func TestAChangedVersionTagIsANewVersion(t *testing.T) {
	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			if d.Components[i].ID == string(pins.Qdrant) {
				d.Components[i].Ref = "docker.io/qdrant/qdrant:v2.0.0-unprivileged@sha256:2222222222222222222222222222222222222222222222222222222222222222"
				d.Components[i].Version = "2.0.0"
			}
		}
	})

	r := checkWith(fullConfig(), v)
	if c := findComponent(t, r, string(pins.Qdrant)); c.Change != ChangeNewVersion {
		t.Errorf("a changed declared version reported %q, want new_version", c.Change)
	}
}

// TestARollingDigestIsAlwaysARebuild: a rolling tag names no version, so a moved
// digest is a rebuild by definition. Calling it a new version would invent a
// version that does not exist.
func TestARollingDigestIsAlwaysARebuild(t *testing.T) {
	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			if d.Components[i].ID == string(pins.OpenWebUI) {
				name, _, _ := strings.Cut(d.Components[i].Ref, "@sha256:")
				d.Components[i].Ref = name + "@sha256:3333333333333333333333333333333333333333333333333333333333333333"
			}
		}
	})

	r := checkWith(fullConfig(), v)
	c := findComponent(t, r, string(pins.OpenWebUI))
	if c.Change != ChangeRebuilt {
		t.Errorf("a moved rolling digest reported %q, want rebuilt", c.Change)
	}
}

// TestANewCrushReleaseIsANewVersion: a checksummed asset moving is a genuine
// release, never a rebuild.
func TestANewCrushReleaseIsANewVersion(t *testing.T) {
	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			if d.Components[i].ID == string(pins.Crush) {
				d.Components[i].Ref = "v0.99.0"
				d.Components[i].Version = "v0.99.0"
				d.Components[i].Checksum = "abc123"
			}
		}
	})

	r := checkWith(fullConfig(), v)
	if c := findComponent(t, r, string(pins.Crush)); c.Change != ChangeNewVersion {
		t.Errorf("a new Crush release reported %q, want new_version", c.Change)
	}
}

// TestTheRejectCarriesNoSummaryAndNoCheckedAt is the absent-is-not-zero discipline
// where it matters most.
//
// A script reading summary.updatable on a could-not-check must get NULL, never a 0
// that reads as "you are current". And CheckedAt must be empty: recording a time
// would imply villa checked, which is the exact claim it is refusing to make.
func TestTheRejectCarriesNoSummaryAndNoCheckedAt(t *testing.T) {
	verdict := manifestverify.Verdict{
		Outcome: manifestverify.Absent,
		Reason:  manifestverify.ReasonExpired,
		Message: "The pin manifest expired on 2026-05-19.",
	}
	r := Check(Input{
		Cfg:                 fullConfig(),
		Resolver:            pinresolve.New(pinstate.State{}),
		Verdict:             verdict,
		CheckedAt:           "2026-08-26T12:00:00Z",
		LastSuccessfulCheck: "2026-04-02T09:12:44Z",
		VillaVersion:        "v1.7",
	})

	if r.Result != ResultCouldNotCheck {
		t.Fatalf("result = %q, want could_not_check", r.Result)
	}
	if r.Summary != nil {
		t.Errorf("the Reject carries a summary of %+v; a script reading summary.updatable would get a number villa never learned", r.Summary)
	}
	if r.CheckedAt != "" {
		t.Errorf("checked_at = %q on a Reject; villa did not check, so recording a time implies it did", r.CheckedAt)
	}
	if r.LastSuccessfulCheck != "2026-04-02T09:12:44Z" {
		t.Errorf("last_successful_check = %q; staleness is the honest signal villa traded automation away for", r.LastSuccessfulCheck)
	}
	if len(r.Subsystems) != 0 {
		t.Error("the Reject carries subsystem rows; villa learned nothing about them")
	}
}

// TestTheRejectSerialisesSummaryAsNullNotZero pins the wire shape, because the
// Go-level nil is only half the property — a script sees the JSON.
func TestTheRejectSerialisesSummaryAsNullNotZero(t *testing.T) {
	r := Check(Input{
		Cfg:      fullConfig(),
		Resolver: pinresolve.New(pinstate.State{}),
		Verdict:  manifestverify.Verdict{Outcome: manifestverify.Absent, Reason: manifestverify.ReasonNotPublished},
	})
	blob, err := json.Marshal(r)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if !strings.Contains(string(blob), `"summary":null`) {
		t.Errorf("the Reject document does not carry a null summary:\n%s", blob)
	}
	if strings.Contains(string(blob), `"updatable":0`) {
		t.Errorf("the Reject document carries updatable:0, which a script reads as 'you are current':\n%s", blob)
	}
	if !strings.Contains(string(blob), `"result":"could_not_check"`) {
		t.Errorf("the Reject document does not carry the could_not_check result:\n%s", blob)
	}
}

// TestADisabledAddonIsSkippedWithItsReason: the working set is the INSTALLED
// FOOTPRINT, not the pin table. The row stays present so the decision is visible —
// omitting it would hide the fact that villa deliberately did not consider it.
func TestADisabledAddonIsSkippedWithItsReason(t *testing.T) {
	cfg := config.VillaConfig{Backend: "vulkan"} // every addon off
	r := checkWith(cfg, acceptedVerdict(nil))

	skipped := map[string]Subsystem{}
	for _, s := range r.Subsystems {
		if s.State == StateSkipped {
			skipped[s.Name] = s
		}
	}
	for _, name := range []string{"memory", "web search", "coding agent"} {
		s, ok := skipped[name]
		if !ok {
			t.Errorf("%s is not reported as skipped despite being disabled", name)
			continue
		}
		if s.SkipReason == "" {
			t.Errorf("%s is skipped with no stated reason; the decision would be invisible", name)
		}
	}
	// Inference and chat are always on, so they are never skipped.
	for _, s := range r.Subsystems {
		if (s.Name == "inference" || s.Name == "chat") && s.State == StateSkipped {
			t.Errorf("%s reported as skipped; it has no gate to be off", s.Name)
		}
	}
}

// TestOnlyTheActiveBackendIsARow: proving a non-active backend would require
// swapping to it, so four backends would mean four swaps and four residency proofs
// in one run — and freshening the vulkan landing spot unproven would degrade the
// very thing rollback depends on.
func TestOnlyTheActiveBackendIsARow(t *testing.T) {
	cfg := fullConfig() // vulkan
	r := checkWith(cfg, acceptedVerdict(nil))

	var inference Subsystem
	for _, s := range r.Subsystems {
		if s.Name == "inference" {
			inference = s
		}
	}
	if len(inference.Components) != 1 {
		t.Fatalf("inference has %d component rows, want exactly the active backend", len(inference.Components))
	}
	if inference.Components[0].Name != string(pins.BackendVulkan) {
		t.Errorf("the inference row is %q, want the active backend", inference.Components[0].Name)
	}

	// Switching the active backend switches the row.
	cfg.Backend = "rocm"
	r = checkWith(cfg, acceptedVerdict(nil))
	for _, s := range r.Subsystems {
		if s.Name == "inference" && s.Components[0].Name != string(pins.BackendROCm724) {
			t.Errorf("with backend=rocm the inference row is %q", s.Components[0].Name)
		}
	}
}

// TestDivergenceIsVisibleWithoutBeingAnUpdate: a host running an effective pin that
// differs from the vetted one, where the manifest offers exactly what it runs. The
// report must show both pins and report no update — the divergence is a fact about
// the host, not a thing to do.
func TestDivergenceIsVisibleWithoutBeingAnUpdate(t *testing.T) {
	running := "docker.io/qdrant/qdrant:v1.18.2-unprivileged@sha256:4444444444444444444444444444444444444444444444444444444444444444"
	state := pinstate.State{Pins: map[string]pinstate.Effective{string(pins.Qdrant): {Ref: running}}}

	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			if d.Components[i].ID == string(pins.Qdrant) {
				d.Components[i].Ref = running
			}
		}
	})

	r := Check(Input{
		Cfg:          fullConfig(),
		Resolver:     pinresolve.New(state),
		Verdict:      v,
		CheckedAt:    "2026-08-26T12:00:00Z",
		VillaVersion: "v1.7",
	})

	c := findComponent(t, r, string(pins.Qdrant))
	if c.Effective != running {
		t.Errorf("effective = %q, want what the host runs", c.Effective)
	}
	if c.Vetted == running {
		t.Error("the vetted pin was overwritten by the effective one; divergence would be invisible")
	}
	if c.Change != ChangeNone {
		t.Errorf("change = %q; the manifest offers exactly what this host runs, so there is nothing to do", c.Change)
	}
}

// TestVillaIsReportedButNeverApplied: self-replacement breaks the prove step (the
// binary proving the update is the binary being replaced) and the websafe
// bind-mount (the running binary is mounted into a container). AppliedByUpdate must
// be false whatever the manifest says.
func TestVillaIsReportedButNeverApplied(t *testing.T) {
	v := acceptedVerdict(func(d *manifest.Document) { d.VillaVersion = "v1.8" })
	r := checkWith(fullConfig(), v)

	if r.Villa == nil {
		t.Fatal("no villa row")
	}
	if r.Villa.Available != "v1.8" || r.Villa.Change != ChangeNewVersion {
		t.Errorf("villa row = %+v, want v1.8 reported as a new version", r.Villa)
	}
	if r.Villa.AppliedByUpdate {
		t.Error("the villa row claims this command applies it; villa does not replace itself")
	}
	// A newer villa is NOT a subsystem update: it must not inflate the count a
	// script branches on.
	if r.Summary.Updatable != 0 {
		t.Errorf("a newer villa counted as %d updatable subsystems", r.Summary.Updatable)
	}
}

// TestUpdatableIsInApplyOrder: `villa update` with no arguments and `--check` must
// never disagree about what would be updated, or in what order.
func TestUpdatableIsInApplyOrder(t *testing.T) {
	v := acceptedVerdict(func(d *manifest.Document) {
		for i := range d.Components {
			switch d.Components[i].ID {
			case string(pins.Crush):
				d.Components[i].Ref = "v0.99.0"
			case string(pins.OpenWebUI), string(pins.Qdrant):
				name, _, _ := strings.Cut(d.Components[i].Ref, "@sha256:")
				d.Components[i].Ref = name + "@sha256:5555555555555555555555555555555555555555555555555555555555555555"
			}
		}
	})

	got := Updatable(checkWith(fullConfig(), v))
	want := []string{"chat", "memory", "coding agent"}
	if len(got) != len(want) {
		t.Fatalf("Updatable = %v, want %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Errorf("Updatable[%d] = %q, want %q — the order is the apply sequence", i, got[i], want[i])
		}
	}
}

// TestSchemaVersionIsStamped: the --json contract carries its own version, like
// every other --json surface in this tree.
func TestSchemaVersionIsStamped(t *testing.T) {
	for _, r := range []Report{
		checkWith(fullConfig(), acceptedVerdict(nil)),
		checkWith(fullConfig(), manifestverify.Verdict{Outcome: manifestverify.Absent}),
	} {
		if r.SchemaVersion != SchemaVersion {
			t.Errorf("schema_version = %d, want %d", r.SchemaVersion, SchemaVersion)
		}
	}
}

// findComponent locates one component's row across the report.
func findComponent(t *testing.T, r Report, name string) Component {
	t.Helper()
	for _, s := range r.Subsystems {
		for _, c := range s.Components {
			if c.Name == name {
				return c
			}
		}
	}
	t.Fatalf("no row for component %q", name)
	return Component{}
}
