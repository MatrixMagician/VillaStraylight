package uninstall

// uninstall_test.go table-tests the pure decisions Run and
// ResolveModelChoiceSource hold: the model keep/remove source (flag wins,
// interactive prompts, non-interactive defaults to keep) and the ordered
// teardown itself against a fake Deps that records call order (#241,
// ADR-0012). cmd/villa/uninstall_test.go continues to guard the SAME
// invariants at the cobra-caller boundary; these tests guard them at the core.

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// TestResolveModelChoiceSource locks: an explicit flag always wins over the
// interactive/non-interactive default, and with neither flag the session's
// interactivity decides prompt vs. default-keep.
func TestResolveModelChoiceSource(t *testing.T) {
	tests := []struct {
		name        string
		opts        Opts
		interactive bool
		want        ModelChoiceSource
	}{
		{"remove flag wins even when interactive", Opts{RemoveModels: true}, true, SourceRemoveFlag},
		{"keep flag wins even when interactive", Opts{KeepModels: true}, true, SourceKeepFlag},
		{"neither flag, interactive, prompts", Opts{}, true, SourcePrompt},
		{"neither flag, non-interactive, defaults to keep", Opts{}, false, SourceDefaultKeep},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := ResolveModelChoiceSource(tt.opts, tt.interactive); got != tt.want {
				t.Fatalf("ResolveModelChoiceSource(%+v, %v) = %v, want %v", tt.opts, tt.interactive, got, tt.want)
			}
		})
	}
}

// TestNonModelVolumes locks: every rendered .volume unit is included EXCEPT
// the model bind-mount, whose data is governed by the keep/remove-models
// choice rather than blanket volume removal.
func TestNonModelVolumes(t *testing.T) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container"},
		{Name: "villa.network"},
		{Name: "villa-models.volume"},
		{Name: "villa-openwebui.volume"},
	}
	got := NonModelVolumes(units)
	if len(got) != 1 || got[0] != "villa-openwebui" {
		t.Fatalf("NonModelVolumes = %v, want [villa-openwebui]", got)
	}
}

// fakeDeps records the ordered seam calls so Run's teardown sequence and its
// short-circuit-on-failure behavior are asserted deterministically.
type fakeDeps struct {
	order []string

	stopErr                error
	disableErr             error
	userUnitDirErr         error
	removeDashboardUnitErr error
	daemonReloadErr        error
	removeAgentBinaryErr   error
	removeCrushConfigErr   error
	removeUnitFileErr      error
	removeVolumesErr       error
	removeModelsErr        error
	disableLingerErr       error

	interactive bool
	consent     bool
}

func (f *fakeDeps) deps() Deps {
	return Deps{
		Stop: func(svc string) error {
			f.order = append(f.order, "stop:"+svc)
			return f.stopErr
		},
		RemoveUnitFile: func(dir, name string) error {
			f.order = append(f.order, "rmunit:"+name)
			return f.removeUnitFileErr
		},
		DaemonReload: func() error {
			f.order = append(f.order, "reload")
			return f.daemonReloadErr
		},
		RemoveVolumes: func(vols []string) error {
			f.order = append(f.order, "rmvol")
			return f.removeVolumesErr
		},
		RemoveModels: func() error {
			f.order = append(f.order, "rmmodels")
			return f.removeModelsErr
		},
		Disable: func(svc string) error {
			f.order = append(f.order, "disable:"+svc)
			return f.disableErr
		},
		UserUnitDir: func() (string, error) {
			return "/unitdir", f.userUnitDirErr
		},
		RemoveDashboardUnit: func(dir, name string) error {
			f.order = append(f.order, "rmdash:"+name)
			return f.removeDashboardUnitErr
		},
		RemoveAgentBinary: func() error {
			f.order = append(f.order, "rmagentbin")
			return f.removeAgentBinaryErr
		},
		RemoveCrushConfig: func() error {
			f.order = append(f.order, "rmcrushcfg")
			return f.removeCrushConfigErr
		},
		DisableLinger: func(string) error {
			f.order = append(f.order, "linger")
			return f.disableLingerErr
		},
		Username:    func() string { return "tester" },
		Interactive: func() bool { return f.interactive },
		Consent:     func(string) bool { return f.consent },
	}
}

func sampleInput() Input {
	return Input{
		Units:         []orchestrate.Unit{{Name: "villa-llama.container"}, {Name: "villa.network"}, {Name: "villa-models.volume"}},
		UnitDir:       "/units",
		StopOrder:     []string{"villa-openwebui.service", "villa-llama.service"},
		DashboardName: "villa-dashboard.service",
	}
}

// TestRunOrdering locks the full teardown sequence: dashboard (stop, disable,
// remove, reload) → agent addon (bin, config) → services in StopOrder → unit
// files → reload → volumes → models (per the choice) → linger.
func TestRunOrdering(t *testing.T) {
	f := &fakeDeps{}
	res := Run(f.deps(), Opts{KeepModels: true}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	want := []string{
		"stop:villa-dashboard.service",
		"disable:villa-dashboard.service",
		"rmdash:villa-dashboard.service",
		"reload",
		"rmagentbin",
		"rmcrushcfg",
		"stop:villa-openwebui.service",
		"stop:villa-llama.service",
		"rmunit:villa-llama.container",
		"rmunit:villa.network",
		"rmunit:villa-models.volume",
		"reload",
		"rmvol",
		"linger",
	}
	if len(f.order) != len(want) {
		t.Fatalf("order = %v, want %v", f.order, want)
	}
	for i := range want {
		if f.order[i] != want[i] {
			t.Fatalf("order[%d] = %q, want %q; full order = %v", i, f.order[i], want[i], f.order)
		}
	}
}

// TestRunKeepModelsNeverCallsRemoveModels locks: --keep-models must not fire
// RemoveModels, and the run still reports success.
func TestRunKeepModelsNeverCallsRemoveModels(t *testing.T) {
	f := &fakeDeps{}
	res := Run(f.deps(), Opts{KeepModels: true}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	for _, c := range f.order {
		if c == "rmmodels" {
			t.Fatalf("--keep-models must not remove models; order = %v", f.order)
		}
	}
}

// TestRunRemoveModelsFiresAfterVolumesBeforeLinger locks the model-removal
// position: after volume removal, before disable-linger.
func TestRunRemoveModelsFiresAfterVolumesBeforeLinger(t *testing.T) {
	f := &fakeDeps{}
	res := Run(f.deps(), Opts{RemoveModels: true}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	pos := func(want string) int {
		for i, c := range f.order {
			if c == want {
				return i
			}
		}
		return -1
	}
	if pos("rmvol") >= pos("rmmodels") || pos("rmmodels") >= pos("linger") {
		t.Fatalf("rmmodels out of position: %v", f.order)
	}
}

// TestRunStopFailureShortCircuits locks: a stop failure aborts before any
// unit-file removal — no partial teardown that leaves dangling units.
func TestRunStopFailureShortCircuits(t *testing.T) {
	f := &fakeDeps{stopErr: errors.New("boom")}
	res := Run(f.deps(), Opts{KeepModels: true}, sampleInput())
	if res.Err == nil {
		t.Fatalf("expected a stop failure to be reported")
	}
	for _, c := range f.order {
		if len(c) >= 7 && c[:7] == "rmunit:" {
			t.Fatalf("a stop failure must not remove any unit files: %v", f.order)
		}
	}
}

// TestRunAgentRemovalErrorNamesTheArtifact locks: a genuine removal error
// (not absence) aborts with a message naming the artifact, mirroring the
// other host-touching steps' short-circuit discipline.
func TestRunAgentRemovalErrorNamesTheArtifact(t *testing.T) {
	f := &fakeDeps{removeAgentBinaryErr: errors.New("permission denied")}
	res := Run(f.deps(), Opts{KeepModels: true}, sampleInput())
	if res.Err == nil {
		t.Fatalf("expected the agent-binary removal error to be reported")
	}
	if got := res.Err.Error(); !strings.Contains(got, "coding agent") {
		t.Fatalf("error must name the artifact; got %q", got)
	}
}

// TestRunNonInteractiveDefaultKeep locks: neither flag + non-interactive
// keeps the models AND narrates the default into Result.Lines.
func TestRunNonInteractiveDefaultKeep(t *testing.T) {
	f := &fakeDeps{interactive: false}
	res := Run(f.deps(), Opts{}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	for _, c := range f.order {
		if c == "rmmodels" {
			t.Fatalf("non-interactive default must KEEP models")
		}
	}
	var sawDefaultLine bool
	for _, l := range res.Lines {
		if strings.Contains(l, "not interactive") {
			sawDefaultLine = true
		}
	}
	if !sawDefaultLine {
		t.Fatalf("must narrate the non-interactive default; lines = %v", res.Lines)
	}
}

// TestRunInteractivePromptRemove locks: neither flag + interactive + consent
// removes the models.
func TestRunInteractivePromptRemove(t *testing.T) {
	f := &fakeDeps{interactive: true, consent: true}
	res := Run(f.deps(), Opts{}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	var removed bool
	for _, c := range f.order {
		if c == "rmmodels" {
			removed = true
		}
	}
	if !removed {
		t.Fatalf("interactive consent to remove must remove models; order = %v", f.order)
	}
}

// TestRunSurfacesSELinuxNoteNeverReverts locks the two host-state invariants:
// the SELinux boolean is surfaced (not reverted — no Deps field even exists
// for that), and config.toml is never touched (no Deps field for that either).
func TestRunSurfacesSELinuxNoteNeverReverts(t *testing.T) {
	f := &fakeDeps{}
	res := Run(f.deps(), Opts{KeepModels: true}, sampleInput())
	if res.Err != nil {
		t.Fatalf("unexpected error: %v", res.Err)
	}
	var sawNote bool
	for _, l := range res.Lines {
		if strings.Contains(l, "container_use_devices") {
			sawNote = true
		}
	}
	if !sawNote {
		t.Fatalf("must surface the left-set SELinux boolean; lines = %v", res.Lines)
	}
}
