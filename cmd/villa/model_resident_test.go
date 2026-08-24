package main

// model_resident_test.go guards the promises `villa model resident` makes: that a
// refusal costs nothing, that admission is decided by internal/residentset rather than
// re-derived here, that ports are allocated into the lowest free gap, and that a
// failure after the first mutation is undone — and reported honestly when the undo
// itself fails.
//
// Every test drives the verbs through a fake residentDeps, so none of them needs a
// GPU, podman, systemd or a network.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// residentRecorder captures every effect a fake residentDeps performs, so a test can
// assert not just the exit code but that nothing was written on a refusal.
type residentRecorder struct {
	saved     []config.VillaConfig
	written   [][]string
	removed   []string
	started   []string
	stopped   []string
	restarted []string
	pulled    []string
	reloads   int
}

// mutated reports whether any host-visible effect happened at all — the assertion
// behind every "zero side effects" promise below.
func (r *residentRecorder) mutated() bool {
	return len(r.saved) > 0 || len(r.written) > 0 || len(r.removed) > 0 ||
		len(r.started) > 0 || len(r.stopped) > 0 || len(r.restarted) > 0 ||
		len(r.pulled) > 0 || r.reloads > 0
}

// residentFixture is the tunable fake host a test builds its deps from.
type residentFixture struct {
	cfg    config.VillaConfig
	models map[string]catalog.CatalogModel
	fits   map[string]recommend.Recommendation
	// diskUnits is the unit-file text already on disk, keyed by unit filename.
	diskUnits map[string]string
	// rendered is what renderUnits returns for any config.
	rendered []orchestrate.Unit
	// changed names the rendered units the reconcile reports as needing a write.
	changed []string
	// active names the services isActive reports as running.
	active map[string]bool

	// Failure injection.
	saveErr    error
	writeErr   error
	startErr   error
	rollbackWr error
}

const (
	testPrimary   = "primary-model"
	testCandidate = "candidate-model"
)

// newResidentFixture builds a host with one configured primary, a two-entry catalog,
// and a generous envelope — the baseline every test narrows from.
func newResidentFixture() *residentFixture {
	const gib = uint64(1) << 30
	f := &residentFixture{
		cfg: config.VillaConfig{Model: testPrimary, Quant: "Q4", Ctx: 8192, Backend: "rocm"},
		models: map[string]catalog.CatalogModel{
			testPrimary:   {ID: testPrimary, Quant: "Q4", DefaultCtx: 8192},
			testCandidate: {ID: testCandidate, Quant: "Q8", DefaultCtx: 4096},
			"third-model": {ID: "third-model", Quant: "Q8", DefaultCtx: 4096},
		},
		fits: map[string]recommend.Recommendation{
			testPrimary:   {Model: testPrimary, Quant: "Q4", ContextLen: 8192, WeightBytes: 8 * gib, KVCacheBytes: gib, HeadroomBytes: 2 * gib, UsableEnvelopeBytes: 64 * gib, Fits: true},
			testCandidate: {Model: testCandidate, Quant: "Q8", ContextLen: 4096, WeightBytes: 4 * gib, KVCacheBytes: gib, HeadroomBytes: 2 * gib, UsableEnvelopeBytes: 64 * gib, Fits: true},
			"third-model": {Model: "third-model", Quant: "Q8", ContextLen: 4096, WeightBytes: 4 * gib, KVCacheBytes: gib, HeadroomBytes: 2 * gib, UsableEnvelopeBytes: 64 * gib, Fits: true},
		},
		diskUnits: map[string]string{orchestrate.OpenWebUIContainerUnitName(): "old-owui"},
		rendered: []orchestrate.Unit{
			{Name: "villa-llama.container", Text: "llama"},
			{Name: orchestrate.OpenWebUIContainerUnitName(), Text: "new-owui"},
		},
		changed: []string{orchestrate.OpenWebUIContainerUnitName()},
		active:  map[string]bool{installServiceName: true, openWebUIServiceName: true},
	}
	return f
}

// deps wires the fixture into a residentDeps plus the recorder that watches it.
func (f *residentFixture) deps() (*residentDeps, *residentRecorder) {
	rec := &residentRecorder{}
	d := &residentDeps{
		loadConfig: func() (config.VillaConfig, error) { return f.cfg, nil },
		saveConfig: func(c config.VillaConfig) error {
			if f.saveErr != nil {
				return f.saveErr
			}
			rec.saved = append(rec.saved, c)
			return nil
		},
		resolveCatalog: func(id string) (catalog.CatalogModel, bool) {
			m, ok := f.models[id]
			return m, ok
		},
		fit: func(m catalog.CatalogModel, _ int) recommend.Recommendation {
			return f.fits[m.ID]
		},
		primaryPort:  func() int { return 8080 },
		isDownloaded: func(catalog.CatalogModel) bool { return true },
		pull: func(m catalog.CatalogModel) error {
			rec.pulled = append(rec.pulled, m.ID)
			return nil
		},
		renderUnits: func(config.VillaConfig) ([]orchestrate.Unit, error) { return f.rendered, nil },
		unitDir:     func() (string, error) { return "/unit/dir", nil },
		reconcile: func(units []orchestrate.Unit, _ string) (orchestrate.Plan, error) {
			var plan orchestrate.Plan
			for _, u := range units {
				if contains(f.changed, u.Name) {
					plan.Changed = append(plan.Changed, u)
					continue
				}
				plan.Unchanged = append(plan.Unchanged, u)
			}
			return plan, nil
		},
		writeUnits: func(plan orchestrate.Plan, _ string) error {
			if f.writeErr != nil {
				return f.writeErr
			}
			var names []string
			for _, u := range plan.Changed {
				names = append(names, u.Name)
			}
			rec.written = append(rec.written, names)
			return nil
		},
		readUnit: func(_, name string) (string, bool) {
			text, ok := f.diskUnits[name]
			return text, ok
		},
		removeUnit: func(_, name string) error {
			rec.removed = append(rec.removed, name)
			return nil
		},
		daemonReload: func() error {
			rec.reloads++
			return nil
		},
		start: func(service string) error {
			if f.startErr != nil {
				return f.startErr
			}
			rec.started = append(rec.started, service)
			return nil
		},
		stop: func(service string) error {
			rec.stopped = append(rec.stopped, service)
			return nil
		},
		restart: func(service string) error {
			rec.restarted = append(rec.restarted, service)
			return nil
		},
		isActive: func(service string) (string, error) {
			if f.active[service] {
				return "active", nil
			}
			return "inactive", nil
		},
	}
	// A rollback restores unit text through writeUnitText, which is a real filesystem
	// call. The tests that exercise rollback point the unit dir at a temp dir so that
	// write lands somewhere harmless, or inject rollbackWr to make it fail.
	if f.rollbackWr != nil {
		d.removeUnit = func(_, _ string) error { return f.rollbackWr }
	}
	return d, rec
}

// newResidentCmd returns a cobra command with captured output buffers.
func newResidentCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "resident"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// TestResidentAddUnknownModelRefusedWithNoSideEffects: an id that is not in the
// catalog is refused, is never interpreted as a filesystem path, and leaves the config
// and the units untouched.
func TestResidentAddUnknownModelRefusedWithNoSideEffects(t *testing.T) {
	for _, id := range []string{"no-such-model", "../../etc/passwd", ""} {
		f := newResidentFixture()
		d, rec := f.deps()
		cmd, _, errOut := newResidentCmd()

		if code := runResidentAdd(cmd, id, d); code != exitBlocked {
			t.Errorf("id %q: exit = %d, want %d", id, code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "unknown model") {
			t.Errorf("id %q: stderr = %q, want an unknown-model refusal", id, errOut.String())
		}
		if rec.mutated() {
			t.Errorf("id %q: an unknown model mutated the host: %+v", id, rec)
		}
	}
}

// TestResidentAddNonFittingCandidateRefusedAndNothingWritten: a candidate the
// admission core refuses is surfaced with its Remediation verbatim, and no config,
// unit, download or systemd effect happens.
func TestResidentAddNonFittingCandidateRefusedAndNothingWritten(t *testing.T) {
	const gib = uint64(1) << 30
	f := newResidentFixture()
	// Larger than the whole envelope: residentset refuses with exceeds_envelope.
	f.fits[testCandidate] = recommend.Recommendation{
		Model: testCandidate, Quant: "Q8", ContextLen: 4096,
		WeightBytes: 200 * gib, KVCacheBytes: gib, HeadroomBytes: 2 * gib,
		UsableEnvelopeBytes: 64 * gib,
	}
	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "exceeds_envelope") {
		t.Errorf("stderr = %q, want the residentset refusal reason", errOut.String())
	}
	if !strings.Contains(errOut.String(), "does not fit the memory envelope") {
		t.Errorf("stderr = %q, want the refusal Remediation surfaced verbatim", errOut.String())
	}
	if rec.mutated() {
		t.Errorf("a refused candidate mutated the host: %+v", rec)
	}
}

// TestResidentAddPersistsConfigBeforeUnitsAndStartsTheSlot: the happy path persists
// the slot with its allocated port, writes the regenerated units, reloads, starts the
// new service, and restarts the chat UI whose endpoint env changed.
func TestResidentAddPersistsConfigBeforeUnitsAndStartsTheSlot(t *testing.T) {
	f := newResidentFixture()
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	f.rendered = append(f.rendered, orchestrate.Unit{Name: unitName + ".container", Text: "resident"})
	f.changed = append(f.changed, unitName+".container")

	d, rec := f.deps()
	cmd, out, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d (stderr %q), want %d", code, errOut.String(), exitPass)
	}
	if len(rec.saved) != 1 {
		t.Fatalf("saved %d configs, want exactly 1", len(rec.saved))
	}
	got := rec.saved[0].Resident
	if len(got) != 1 || got[0].Model != testCandidate || got[0].Port != residentPortBase || got[0].Ctx != 4096 {
		t.Errorf("persisted slot = %+v, want %s on port %d at ctx 4096", got, testCandidate, residentPortBase)
	}
	if len(rec.written) != 1 {
		t.Errorf("wrote units %d times, want 1", len(rec.written))
	}
	if !contains(rec.started, unitName+".service") {
		t.Errorf("started = %v, want the new resident service", rec.started)
	}
	if !contains(rec.restarted, openWebUIServiceName) {
		t.Errorf("restarted = %v, want the chat UI whose endpoint env changed", rec.restarted)
	}
	if !strings.Contains(out.String(), "is resident on 127.0.0.1:8081") {
		t.Errorf("stdout = %q, want the slot's endpoint reported", out.String())
	}
}

// TestResidentAddIsANoOpWhenAlreadyResident: re-adding a configured slot is reported
// as already resident and changes nothing (residentset.Admit's NoOp verdict).
func TestResidentAddIsANoOpWhenAlreadyResident(t *testing.T) {
	f := newResidentFixture()
	f.cfg.Resident = []config.ResidentModel{{Model: testCandidate, Quant: "Q8", Ctx: 4096, Port: 8081}}
	d, rec := f.deps()
	cmd, out, _ := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if !strings.Contains(out.String(), "already resident") {
		t.Errorf("stdout = %q, want an already-resident no-op", out.String())
	}
	if rec.mutated() {
		t.Errorf("a no-op add mutated the host: %+v", rec)
	}
}

// TestAllocResidentPortSkipsTakenPorts: allocation returns the lowest free port at or
// above the base, skipping the primary's port and every configured slot's, and filling
// the lowest gap a removal left behind.
func TestAllocResidentPortSkipsTakenPorts(t *testing.T) {
	slot := func(ports ...int) []config.ResidentModel {
		var out []config.ResidentModel
		for _, p := range ports {
			out = append(out, config.ResidentModel{Port: p})
		}
		return out
	}
	tests := []struct {
		name        string
		primaryPort int
		slots       []config.ResidentModel
		want        int
	}{
		{"empty set takes the base", 8080, nil, 8081},
		{"skips a contiguous run", 8080, slot(8081, 8082, 8083), 8084},
		{"fills the lowest gap", 8080, slot(8081, 8083), 8082},
		{"skips the primary when it sits in range", 8081, slot(8082), 8083},
		{"ignores order", 8080, slot(8083, 8081, 8082), 8084},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			if got := allocResidentPort(tc.primaryPort, tc.slots); got != tc.want {
				t.Errorf("allocResidentPort = %d, want %d", got, tc.want)
			}
		})
	}
}

// TestResidentRmRefusesThePrimary: the primary is not a resident slot, and removing it
// is refused with the command that actually changes it, before anything is written.
func TestResidentRmRefusesThePrimary(t *testing.T) {
	f := newResidentFixture()
	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentRm(cmd, testPrimary, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "villa model swap") {
		t.Errorf("stderr = %q, want the refusal to point at `villa model swap`", errOut.String())
	}
	if rec.mutated() {
		t.Errorf("refusing to remove the primary mutated the host: %+v", rec)
	}
}

// TestResidentRmStopsAndDeletesTheOrphanedUnit: removing a slot persists the shortened
// config, stops the orphaned service, and deletes the unit file the regenerated set no
// longer contains.
func TestResidentRmStopsAndDeletesTheOrphanedUnit(t *testing.T) {
	f := newResidentFixture()
	f.cfg.Resident = []config.ResidentModel{
		{Model: testCandidate, Quant: "Q8", Ctx: 4096, Port: 8081},
		{Model: "third-model", Quant: "Q8", Ctx: 4096, Port: 8082},
	}
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	f.diskUnits[unitName+".container"] = "resident"

	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentRm(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d (stderr %q), want %d", code, errOut.String(), exitPass)
	}
	if len(rec.saved) != 1 || len(rec.saved[0].Resident) != 1 || rec.saved[0].Resident[0].Model != "third-model" {
		t.Fatalf("persisted resident set = %+v, want only third-model", rec.saved)
	}
	if !contains(rec.stopped, unitName+".service") {
		t.Errorf("stopped = %v, want the orphaned service", rec.stopped)
	}
	if !contains(rec.removed, unitName+".container") {
		t.Errorf("removed = %v, want the orphaned unit file", rec.removed)
	}
}

// TestResidentRmRefusesAnUnconfiguredSlot: an id that is neither the primary nor a
// configured slot is refused with nothing written.
func TestResidentRmRefusesAnUnconfiguredSlot(t *testing.T) {
	f := newResidentFixture()
	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentRm(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "is not a resident slot") {
		t.Errorf("stderr = %q, want a not-a-slot refusal", errOut.String())
	}
	if rec.mutated() {
		t.Errorf("refusing an unconfigured slot mutated the host: %+v", rec)
	}
}

// TestResidentAddRollsConfigBackWhenAStepAfterTheSaveFails: a failure after the config
// has been persisted restores the prior config, so config.toml never records a slot
// the units do not have.
func TestResidentAddRollsConfigBackWhenAStepAfterTheSaveFails(t *testing.T) {
	f := newResidentFixture()
	f.writeErr = errors.New("disk full")
	d, rec := f.deps()
	// The unit dir must be a real directory: the rollback restores captured unit text
	// through writeUnitText, which writes for real.
	dir := t.TempDir()
	d.unitDir = func() (string, error) { return dir, nil }
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if len(rec.saved) != 2 {
		t.Fatalf("saved %d configs, want 2 (the new one, then the restored prior)", len(rec.saved))
	}
	if len(rec.saved[1].Resident) != 0 {
		t.Errorf("restored config = %+v, want the prior config with no resident slot", rec.saved[1])
	}
	if !strings.Contains(errOut.String(), "rolled back to the prior state") {
		t.Errorf("stderr = %q, want a clean-rollback report", errOut.String())
	}
}

// TestResidentAddReportsAnIncompleteRollbackHonestly: when a restore step itself fails,
// the operator is told the stack is in an indeterminate state rather than that it was
// cleanly rolled back.
func TestResidentAddReportsAnIncompleteRollbackHonestly(t *testing.T) {
	f := newResidentFixture()
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	// The resident unit is written by this run and absent from disk beforehand, so the
	// rollback must REMOVE it — and the removal is what fails.
	f.rendered = append(f.rendered, orchestrate.Unit{Name: unitName + ".container", Text: "resident"})
	f.changed = append(f.changed, unitName+".container")
	f.startErr = errors.New("unit failed to start")
	f.rollbackWr = errors.New("permission denied")

	d, _ := f.deps()
	dir := t.TempDir()
	d.unitDir = func() (string, error) { return dir, nil }
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	got := errOut.String()
	if !strings.Contains(got, "did not fully complete") {
		t.Errorf("stderr = %q, want the rollback reported as incomplete", got)
	}
	if !strings.Contains(got, "indeterminate state") {
		t.Errorf("stderr = %q, want the operator warned the stack is indeterminate", got)
	}
	if strings.Contains(got, "rolled back to the prior state") {
		t.Errorf("stderr = %q, must never claim a clean rollback after a failed restore", got)
	}
}

// TestResidentAddDoesNotRollBackWhenTheConfigSaveItselfFails: the save is the FIRST
// mutation, so a failure there leaves nothing to undo and must not report a rollback
// the run never performed.
func TestResidentAddDoesNotRollBackWhenTheConfigSaveItselfFails(t *testing.T) {
	f := newResidentFixture()
	f.saveErr = errors.New("read-only filesystem")
	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "persist config") {
		t.Errorf("stderr = %q, want the persist failure surfaced", errOut.String())
	}
	if strings.Contains(errOut.String(), "rolled back") {
		t.Errorf("stderr = %q, must not claim a rollback when nothing was mutated", errOut.String())
	}
	if rec.mutated() {
		t.Errorf("a failed config save mutated the host: %+v", rec)
	}
}

// TestResidentLsListsThePrimaryAndEverySlot: ls is read-only and reports the primary
// plus each configured slot with its port, unit and live state.
func TestResidentLsListsThePrimaryAndEverySlot(t *testing.T) {
	f := newResidentFixture()
	f.cfg.Resident = []config.ResidentModel{{Model: testCandidate, Quant: "Q8", Ctx: 4096, Port: 8081}}
	d, rec := f.deps()
	cmd, out, _ := newResidentCmd()

	if code := runResidentLs(cmd, false, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	text := out.String()
	for _, want := range []string{"primary", testPrimary, installServiceName, "resident", testCandidate, "8081", "active"} {
		if !strings.Contains(text, want) {
			t.Errorf("ls output missing %q:\n%s", want, text)
		}
	}
	if rec.mutated() {
		t.Errorf("ls mutated the host: %+v", rec)
	}
}

// TestResidentLsJSONCarriesEverySlotAndItsSchema: --json emits one entry per slot with
// the primary first, and stamps the contract's schema version.
func TestResidentLsJSONCarriesEverySlotAndItsSchema(t *testing.T) {
	f := newResidentFixture()
	f.cfg.Resident = []config.ResidentModel{{Model: testCandidate, Quant: "Q8", Ctx: 4096, Port: 8081}}
	f.active = map[string]bool{installServiceName: true}
	d, _ := f.deps()
	cmd, out, _ := newResidentCmd()

	if code := runResidentLs(cmd, true, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	var got residentListReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode --json: %v (%s)", err, out.String())
	}
	if got.SchemaVersion != residentListSchemaVersion {
		t.Errorf("schema_version = %d, want %d", got.SchemaVersion, residentListSchemaVersion)
	}
	if len(got.Slots) != 2 {
		t.Fatalf("slots = %+v, want the primary plus one resident", got.Slots)
	}
	if !got.Slots[0].Primary || got.Slots[0].Port != 8080 {
		t.Errorf("slot[0] = %+v, want the primary on 8080", got.Slots[0])
	}
	if got.Slots[1].Primary || got.Slots[1].Port != 8081 || got.Slots[1].Active != "inactive" {
		t.Errorf("slot[1] = %+v, want the resident on 8081 reported inactive", got.Slots[1])
	}
}

// TestResidentLsReportsAnUnreadableStateAsUnknown: a state the host could not evaluate
// degrades to "unknown", never to a confident "inactive".
func TestResidentLsReportsAnUnreadableStateAsUnknown(t *testing.T) {
	f := newResidentFixture()
	d, _ := f.deps()
	d.isActive = func(string) (string, error) { return "", errors.New("systemctl missing") }
	cmd, out, _ := newResidentCmd()

	if code := runResidentLs(cmd, true, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	var got residentListReport
	if err := json.Unmarshal(out.Bytes(), &got); err != nil {
		t.Fatalf("decode --json: %v", err)
	}
	if got.Slots[0].Active != "unknown" {
		t.Errorf("active = %q, want \"unknown\" for an unevaluable state", got.Slots[0].Active)
	}
}

// TestResidentSlotsFailsClosedOnAModelThatLeftTheCatalog: a configured slot whose model
// is no longer in the catalog blocks the add rather than being silently sized at zero,
// which would admit a candidate that cannot actually fit.
func TestResidentSlotsFailsClosedOnAModelThatLeftTheCatalog(t *testing.T) {
	f := newResidentFixture()
	f.cfg.Resident = []config.ResidentModel{{Model: "gone-from-catalog", Quant: "Q8", Ctx: 4096, Port: 8081}}
	d, rec := f.deps()
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitBlocked {
		t.Fatalf("exit = %d, want %d", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "is not in the catalog") {
		t.Errorf("stderr = %q, want a fail-closed catalog error", errOut.String())
	}
	if rec.mutated() {
		t.Errorf("a fail-closed sizing error mutated the host: %+v", rec)
	}
}

// TestModelResidentRegistered: the three verbs are reachable under `villa model`.
func TestModelResidentRegistered(t *testing.T) {
	var resident *cobra.Command
	for _, c := range newModel().Commands() {
		if c.Name() == "resident" {
			resident = c
		}
	}
	if resident == nil {
		t.Fatal("`villa model resident` is not registered under `villa model`")
	}
	want := map[string]bool{"ls": false, "add": false, "rm": false}
	for _, c := range resident.Commands() {
		if _, ok := want[c.Name()]; ok {
			want[c.Name()] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Errorf("`villa model resident %s` is not registered", name)
		}
	}
}

// TestResidentAddReconcilesChatEndpointsPrimaryFirst guards the ordering Open WebUI's
// stored connection list is written in. The primary must lead, because the chat UI
// presents the first connection as the default and a resident slot silently becoming
// the default is a behaviour change nobody asked for.
func TestResidentAddReconcilesChatEndpointsPrimaryFirst(t *testing.T) {
	f := newResidentFixture()
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	f.rendered = append(f.rendered, orchestrate.Unit{Name: unitName + ".container", Text: "resident"})
	f.changed = append(f.changed, unitName+".container")

	d, _ := f.deps()
	var got []string
	d.syncEndpoints = func(_ context.Context, _ int, want []string) (openwebui.EndpointSync, error) {
		got = want
		return openwebui.EndpointSync{Wrote: true, Endpoints: want}, nil
	}
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d (stderr %q), want %d", code, errOut.String(), exitPass)
	}
	if len(got) != 2 {
		t.Fatalf("reconciled %d endpoints, want the primary plus the new slot: %v", len(got), got)
	}
	if got[0] != orchestrate.LlamaInNetworkEndpoint() {
		t.Errorf("endpoint[0] = %q, want the primary to lead", got[0])
	}
	wantSlot, err := orchestrate.ResidentInNetworkEndpoint(testCandidate)
	if err != nil {
		t.Fatalf("ResidentInNetworkEndpoint: %v", err)
	}
	if got[1] != wantSlot {
		t.Errorf("endpoint[1] = %q, want %q", got[1], wantSlot)
	}
}

// TestResidentAddSurvivesAChatEndpointReconcileFailure guards the deliberate decision
// that the chat-UI reconcile is not part of the transaction. By the time it runs the
// units are written and the slot is serving, so the stack is correct and only the chat
// UI's own record of it lags. Rolling back would destroy a working slot to fix a
// cosmetic lag, so the command succeeds, keeps the config, and says what to re-run.
func TestResidentAddSurvivesAChatEndpointReconcileFailure(t *testing.T) {
	f := newResidentFixture()
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	f.rendered = append(f.rendered, orchestrate.Unit{Name: unitName + ".container", Text: "resident"})
	f.changed = append(f.changed, unitName+".container")

	d, rec := f.deps()
	d.syncEndpoints = func(_ context.Context, _ int, _ []string) (openwebui.EndpointSync, error) {
		return openwebui.EndpointSync{}, errors.New("chat UI unreachable")
	}
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d, want %d — a chat-UI lag must not fail a correct stack mutation", code, exitPass)
	}
	if len(rec.saved) != 1 || len(rec.saved[0].Resident) != 1 {
		t.Errorf("config was rolled back by a non-transactional step: %+v", rec.saved)
	}
	if !contains(rec.started, unitName+".service") {
		t.Errorf("started = %v, want the slot to still be running", rec.started)
	}
	if !strings.Contains(errOut.String(), "chat UI") {
		t.Errorf("stderr = %q, want it to name the chat UI lag and the repair", errOut.String())
	}
}

// TestResidentAddRetriesTheChatReconcileThroughTheRestartRace guards against reporting
// a failure that is really a race. The reconcile runs immediately after Open WebUI is
// restarted, and Open WebUI refuses connections for about a second while it comes back,
// so a single attempt reports "chat UI unreachable" on a perfectly healthy stack.
func TestResidentAddRetriesTheChatReconcileThroughTheRestartRace(t *testing.T) {
	f := newResidentFixture()
	unitName, err := orchestrate.ResidentUnitName(testCandidate)
	if err != nil {
		t.Fatalf("ResidentUnitName: %v", err)
	}
	f.rendered = append(f.rendered, orchestrate.Unit{Name: unitName + ".container", Text: "resident"})
	f.changed = append(f.changed, unitName+".container")

	d, _ := f.deps()
	d.syncRetryDelay = 0
	calls := 0
	d.syncEndpoints = func(_ context.Context, _ int, want []string) (openwebui.EndpointSync, error) {
		calls++
		if calls < 3 {
			return openwebui.EndpointSync{}, errors.New("connection reset while restarting")
		}
		return openwebui.EndpointSync{Wrote: true, Endpoints: want}, nil
	}
	cmd, _, errOut := newResidentCmd()

	if code := runResidentAdd(cmd, testCandidate, d); code != exitPass {
		t.Fatalf("exit = %d, want %d", code, exitPass)
	}
	if calls != 3 {
		t.Errorf("syncEndpoints called %d times, want it retried until it succeeded", calls)
	}
	if strings.Contains(errOut.String(), "chat UI still lists") {
		t.Errorf("warned about a failure it recovered from: %q", errOut.String())
	}
}
