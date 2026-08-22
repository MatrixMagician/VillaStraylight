package install

import (
	"errors"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// transaction_test.go covers install's transaction. This is the ticket's behaviour
// change, so the tests assert what the host looks like after a failure, not merely
// that a function returned.

// rollbackRecorder records the restore's effects in order, and can fail chosen steps.
type rollbackRecorder struct {
	calls []string

	stopErr   map[string]error
	startErr  map[string]error
	writeErr  map[string]error
	removeErr map[string]error
	saveErr   error
	rmCfgErr  error
	reloadErr error
}

func newRollbackRecorder() *rollbackRecorder {
	return &rollbackRecorder{
		stopErr:   map[string]error{},
		startErr:  map[string]error{},
		writeErr:  map[string]error{},
		removeErr: map[string]error{},
	}
}

func (r *rollbackRecorder) deps() RollbackDeps {
	return RollbackDeps{
		StopService: func(s string) error {
			r.calls = append(r.calls, "stop:"+s)
			return r.stopErr[s]
		},
		StartService: func(s string) error {
			r.calls = append(r.calls, "start:"+s)
			return r.startErr[s]
		},
		WriteUnit: func(name, _ string) error {
			r.calls = append(r.calls, "writeUnit:"+name)
			return r.writeErr[name]
		},
		RemoveUnit: func(name string) error {
			r.calls = append(r.calls, "removeUnit:"+name)
			return r.removeErr[name]
		},
		SaveConfig: func(config.VillaConfig) error {
			r.calls = append(r.calls, "saveConfig")
			return r.saveErr
		},
		RemoveConfig: func() error {
			r.calls = append(r.calls, "removeConfig")
			return r.rmCfgErr
		},
		DaemonReload: func() error {
			r.calls = append(r.calls, "reload")
			return r.reloadErr
		},
	}
}

func (r *rollbackRecorder) did(call string) bool {
	for _, c := range r.calls {
		if c == call {
			return true
		}
	}
	return false
}

func (r *rollbackRecorder) indexOf(call string) int {
	for i, c := range r.calls {
		if c == call {
			return i
		}
	}
	return -1
}

// TestCleanHostRollsBackToNothing is the decision ADR-0003 records. A failed first
// install must leave the host as it found it: services stopped, units removed, config
// gone. Leaving a half-installed stack would be indistinguishable from a healthy one
// to every ordinary check, and a later `villa up` or a reboot would bring up a stack
// that never passed its proof.
func TestCleanHostRollsBackToNothing(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	if !prior.FirstInstall {
		t.Fatal("a host with no config and no units is a first install")
	}

	r := newRollbackRecorder()
	m := Mutations{
		Started:    []string{"villa-llama.service", "villa-openwebui.service"},
		WroteUnits: []string{"villa-llama.container", "villa-openwebui.container"},
	}
	m.RecordConfigSave()

	res := Rollback(r.deps(), prior, m)

	if res.Incomplete {
		t.Fatalf("a clean rollback must not report incomplete: %v", res.Failures)
	}
	for _, want := range []string{
		"stop:villa-llama.service", "stop:villa-openwebui.service",
		"removeUnit:villa-llama.container", "removeUnit:villa-openwebui.container",
		"removeConfig",
	} {
		if !r.did(want) {
			t.Errorf("a first-install rollback must %s; calls = %v", want, r.calls)
		}
	}
	if r.did("saveConfig") {
		t.Error("a first-install rollback must REMOVE the config, not restore a prior one that never existed")
	}
}

// TestReinstallRestoresThePriorStack: a failed upgrade must leave the working
// install running, not a broken one.
func TestReinstallRestoresThePriorStack(t *testing.T) {
	priorCfg := config.VillaConfig{Model: "old-model", Backend: "vulkan"}
	prior := CapturePrior(priorCfg, true,
		map[string]string{"villa-llama.container": "[Container]\nImage=old\n"},
		map[string]bool{"villa-llama.service": true},
	)
	if prior.FirstInstall {
		t.Fatal("a host with a config and units is not a first install")
	}

	r := newRollbackRecorder()
	m := Mutations{
		Started:    []string{"villa-llama.service"},
		WroteUnits: []string{"villa-llama.container"},
	}
	m.RecordConfigSave()

	res := Rollback(r.deps(), prior, m)

	if res.Incomplete {
		t.Fatalf("a clean rollback must not report incomplete: %v", res.Failures)
	}
	if !r.did("writeUnit:villa-llama.container") {
		t.Errorf("a prior unit must be restored verbatim, not removed; calls = %v", r.calls)
	}
	if r.did("removeUnit:villa-llama.container") {
		t.Error("a unit that existed before must never be removed by rollback")
	}
	if !r.did("saveConfig") {
		t.Error("the prior config must be restored")
	}
	if r.did("removeConfig") {
		t.Error("a re-install must never delete the operator's config")
	}
	if !r.did("start:villa-llama.service") {
		t.Error("a service that was running before must be left running after rollback")
	}
	if r.did("stop:villa-llama.service") {
		t.Error("a service that was running before must not be stopped and left down")
	}
}

// TestRollbackOrdersStopsBeforeUnitRewrites: rewriting a unit under a service still
// running against it leaves the two disagreeing. Stop first, then restore.
func TestRollbackOrdersStopsBeforeUnitRewrites(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()
	m := Mutations{
		Started:    []string{"villa-llama.service"},
		WroteUnits: []string{"villa-llama.container"},
	}

	Rollback(r.deps(), prior, m)

	stopAt := r.indexOf("stop:villa-llama.service")
	unitAt := r.indexOf("removeUnit:villa-llama.container")
	if stopAt == -1 || unitAt == -1 {
		t.Fatalf("expected both a stop and a unit removal; calls = %v", r.calls)
	}
	if stopAt > unitAt {
		t.Errorf("stops must precede unit changes; calls = %v", r.calls)
	}
}

// TestRollbackReloadsAfterUnitChanges: the manager must be told the units changed, or
// it keeps serving the state install just undid.
func TestRollbackReloadsAfterUnitChanges(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()

	Rollback(r.deps(), prior, Mutations{WroteUnits: []string{"villa-llama.container"}})

	if !r.did("reload") {
		t.Errorf("a unit change must be followed by a manager reload; calls = %v", r.calls)
	}
	if r.indexOf("removeUnit:villa-llama.container") > r.indexOf("reload") {
		t.Errorf("the reload must follow the unit change; calls = %v", r.calls)
	}
}

// TestNoUnitChangeMeansNoReload: a rollback that touched no unit must not churn the
// manager.
func TestNoUnitChangeMeansNoReload(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()

	Rollback(r.deps(), prior, Mutations{Started: []string{"villa-llama.service"}})

	if r.did("reload") {
		t.Errorf("no unit changed, so nothing needed reloading; calls = %v", r.calls)
	}
}

// TestIncompleteRollbackIsReportedHonestly is the path the ticket names explicitly.
// A rollback step that itself failed must never be presented as a clean restoration:
// a wrong "restored" claim is worse than an honest "partially restored", because it
// tells the operator to stop looking.
func TestIncompleteRollbackIsReportedHonestly(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()
	r.removeErr["villa-llama.container"] = errors.New("permission denied")

	m := Mutations{
		Started:    []string{"villa-llama.service"},
		WroteUnits: []string{"villa-llama.container"},
	}
	m.RecordConfigSave()

	res := Rollback(r.deps(), prior, m)

	if !res.Incomplete {
		t.Fatal("a failed restore step must report the rollback as incomplete")
	}
	reason := res.Reason()
	if !strings.Contains(reason, "did not fully complete") {
		t.Errorf("the reason must not claim a clean restoration, got %q", reason)
	}
	if !strings.Contains(reason, "villa-llama.container") {
		t.Errorf("the reason must name what could not be undone, got %q", reason)
	}
	if !strings.Contains(reason, "indeterminate state") {
		t.Errorf("the reason must tell the operator the stack is indeterminate, got %q", reason)
	}
}

// TestRollbackContinuesPastAFailedStep: stopping at the first failure would leave
// more of the mutation in place than necessary. Every remaining step still runs, and
// every failure is recorded.
func TestRollbackContinuesPastAFailedStep(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()
	r.stopErr["villa-llama.service"] = errors.New("unit is masked")

	m := Mutations{
		Started:    []string{"villa-llama.service", "villa-openwebui.service"},
		WroteUnits: []string{"villa-llama.container"},
	}
	m.RecordConfigSave()

	res := Rollback(r.deps(), prior, m)

	if !res.Incomplete {
		t.Fatal("the failed stop must be reported")
	}
	if !r.did("stop:villa-openwebui.service") {
		t.Errorf("a failed step must not abort the rest of the restore; calls = %v", r.calls)
	}
	if !r.did("removeUnit:villa-llama.container") {
		t.Errorf("units must still be undone after a failed stop; calls = %v", r.calls)
	}
	if !r.did("removeConfig") {
		t.Errorf("the config must still be undone after a failed stop; calls = %v", r.calls)
	}
}

// TestEveryFailureIsNamed: an operator fixing a half-restored host needs to know
// each thing that could not be undone, not just the first.
func TestEveryFailureIsNamed(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()
	r.stopErr["villa-llama.service"] = errors.New("masked")
	r.removeErr["villa-llama.container"] = errors.New("read-only fs")
	r.rmCfgErr = errors.New("permission denied")

	m := Mutations{
		Started:    []string{"villa-llama.service"},
		WroteUnits: []string{"villa-llama.container"},
	}
	m.RecordConfigSave()

	res := Rollback(r.deps(), prior, m)

	if len(res.Failures) != 3 {
		t.Errorf("all three failures must be reported, got %d: %v", len(res.Failures), res.Failures)
	}
	reason := res.Reason()
	for _, want := range []string{"villa-llama.service", "villa-llama.container", "config"} {
		if !strings.Contains(reason, want) {
			t.Errorf("the reason must name %q, got %q", want, reason)
		}
	}
}

// TestNothingMutatedRollsBackCleanly: a refusal before any mutation has nothing to
// undo, and must not touch the host.
func TestNothingMutatedRollsBackCleanly(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{}, false, nil, nil)
	r := newRollbackRecorder()

	res := Rollback(r.deps(), prior, Mutations{})

	if res.Incomplete {
		t.Errorf("an empty rollback must be clean, got %v", res.Failures)
	}
	if len(r.calls) != 0 {
		t.Errorf("a refusal before any mutation must touch nothing; calls = %v", r.calls)
	}
	if res.Reason() != "rolled back to the prior state" {
		t.Errorf("Reason = %q", res.Reason())
	}
}

// TestPartialPriorStateIsNotACleanHost: a host with units but no config, or a config
// but no units, is an incomplete prior state rather than a clean host. Treating it as
// clean would delete the half that was there.
func TestPartialPriorStateIsNotACleanHost(t *testing.T) {
	cases := []struct {
		name      string
		hadConfig bool
		units     map[string]string
		wantFirst bool
	}{
		{"no config, no units", false, nil, true},
		{"config only", true, nil, false},
		{"units only", false, map[string]string{"villa-llama.container": "x"}, false},
		{"both", true, map[string]string{"villa-llama.container": "x"}, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p := CapturePrior(config.VillaConfig{}, tc.hadConfig, tc.units, nil)
			if p.FirstInstall != tc.wantFirst {
				t.Errorf("FirstInstall = %v, want %v", p.FirstInstall, tc.wantFirst)
			}
		})
	}
}

// TestModelWeightsAreNotCaptured: weights are large, expensive to re-acquire and
// inert on their own, so a failed install leaves them and a retry does not
// re-download tens of gigabytes. The capture has no field for them, which is what
// makes that guarantee structural rather than a promise.
func TestModelWeightsAreNotCaptured(t *testing.T) {
	prior := CapturePrior(config.VillaConfig{Model: "qwen3-30b-a3b"}, true, nil, nil)
	r := newRollbackRecorder()

	m := Mutations{Started: []string{"villa-llama.service"}}
	m.RecordConfigSave()
	Rollback(r.deps(), prior, m)

	for _, c := range r.calls {
		if strings.Contains(c, "model") || strings.Contains(c, "gguf") || strings.Contains(c, "weight") {
			t.Errorf("rollback touched model weights (%q); a retry must not re-download them", c)
		}
	}
}
