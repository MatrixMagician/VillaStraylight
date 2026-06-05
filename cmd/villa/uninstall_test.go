package main

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// uninstall_test.go drives runUninstall entirely through stubs so the D-11 ordering
// (down → rm units → daemon-reload → rm volumes → optionally rm models →
// disable-linger), the keep/remove-models choice, the config-toml-preserved
// invariant, and the SELinux-boolean-NOT-reverted invariant are all asserted with
// no live podman/systemd/loginctl host.

// fakeUninstallDeps records the ORDER of every host-touching call so the test can
// assert the D-11 sequence deterministically, plus counters for keep/remove paths.
type fakeUninstallDeps struct {
	*uninstallDeps
	order []string

	stopped       []string
	removedUnits  []string
	removedVols   []string
	removedModels bool
	lingerOff     bool
	reverts       int // setsebool revert calls — MUST stay 0

	// Dashboard-service teardown (Plan 05-05 / T-05-18): records the disable seam
	// calls and the dashboard unit file removal.
	dashDisabled    []string
	removedDashUnit []string
}

func newFakeUninstallDeps(t *testing.T, units []orchestrate.Unit, unitDir string) *fakeUninstallDeps {
	t.Helper()
	f := &fakeUninstallDeps{}
	d := &uninstallDeps{
		renderStack: func() ([]orchestrate.Unit, string, error) { return units, unitDir, nil },
		stop: func(svc string) error {
			f.order = append(f.order, "stop:"+svc)
			f.stopped = append(f.stopped, svc)
			return nil
		},
		removeUnitFile: func(dir, name string) error {
			f.order = append(f.order, "rmunit:"+name)
			f.removedUnits = append(f.removedUnits, name)
			return nil
		},
		daemonReload: func() error { f.order = append(f.order, "reload"); return nil },
		removeVolumes: func(vols []string) error {
			f.order = append(f.order, "rmvol")
			f.removedVols = append(f.removedVols, vols...)
			return nil
		},
		removeModels: func() error {
			f.order = append(f.order, "rmmodels")
			f.removedModels = true
			return nil
		},
		disableLinger: func(string) error { f.order = append(f.order, "linger"); f.lingerOff = true; return nil },
		username:      func() string { return "tester" },
		interactive:   func() bool { return false },
		consent:       func(string) bool { return false },
	}
	d.disable = func(svc string) error {
		f.order = append(f.order, "disable:"+svc)
		f.dashDisabled = append(f.dashDisabled, svc)
		return nil
	}
	d.userUnitDir = func() (string, error) { return unitDir, nil }
	d.removeDashboardUnit = func(_, name string) error {
		f.order = append(f.order, "rmdash:"+name)
		f.removedDashUnit = append(f.removedDashUnit, name)
		return nil
	}
	f.uninstallDeps = d
	return f
}

func uninstallTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	out := &bytes.Buffer{}
	errOut := &bytes.Buffer{}
	cmd := &cobra.Command{Use: "uninstall"}
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	return cmd, out, errOut
}

func sampleUnits() []orchestrate.Unit {
	return []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "x"},
		{Name: "villa.network", Text: "y"},
		{Name: "villa-models.volume", Text: "z"},
	}
}

// TestUninstallOrdering asserts the exact D-11 teardown sequence: stop the service,
// remove every generated unit file, daemon-reload, remove non-model volumes, then
// disable linger — in that order.
func TestUninstallOrdering(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, errOut := uninstallTestCmd()

	code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps)
	if code != exitPass {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", exitPass, code, errOut.String())
	}

	// The container teardown (D-11) sequence: the CONTAINER stop precedes the first
	// container unit removal; unit removals precede the container daemon-reload; that
	// reload precedes volume removal; volume removal precedes linger. The dashboard
	// teardown (stop/disable/remove + its own reload) runs FIRST and is asserted
	// separately by TestUninstallTearsDownDashboard, so we anchor on the CONTAINER
	// stop and use the LAST reload (the container one) to scope past the dashboard's.
	first := func(prefix string) int {
		for i, s := range f.order {
			if strings.HasPrefix(s, prefix) {
				return i
			}
		}
		return -1
	}
	last := func(s string) int {
		idx := -1
		for i, ev := range f.order {
			if ev == s {
				idx = i
			}
		}
		return idx
	}
	stopI := first("stop:villa-llama.service")
	rmunitI := first("rmunit:")
	reloadI := last("reload") // the container daemon-reload (after the dashboard's)
	rmvolI := first("rmvol")
	lingerI := first("linger")
	if !(stopI >= 0 && rmunitI > stopI && reloadI > rmunitI && rmvolI > reloadI && lingerI > rmvolI) {
		t.Fatalf("D-11 ordering violated: %v", f.order)
	}
}

// TestUninstallTearsDownDashboard (Plan 05-05 / T-05-18): uninstall stops, DISABLES
// (revokes boot-survival), and removes the native villa-dashboard.service unit file —
// and does so BEFORE the container teardown (it was started last by install). The
// .service lives outside the Quadlet generator dir, so it needs explicit removal: a
// daemon-reload alone would leave a stale enabled unit that re-spawns a listener.
func TestUninstallTearsDownDashboard(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, errOut := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", exitPass, code, errOut.String())
	}

	// The dashboard was stopped, disabled, and its unit removed.
	if len(f.dashDisabled) != 1 || f.dashDisabled[0] != orchestrate.DashboardServiceName {
		t.Fatalf("dashboard disable = %v, want one disable of %s", f.dashDisabled, orchestrate.DashboardServiceName)
	}
	if len(f.removedDashUnit) != 1 || f.removedDashUnit[0] != orchestrate.DashboardServiceName {
		t.Fatalf("dashboard unit removal = %v, want %s", f.removedDashUnit, orchestrate.DashboardServiceName)
	}

	idx := func(want string) int {
		for i, s := range f.order {
			if s == want {
				return i
			}
		}
		return -1
	}
	dashStopI := idx("stop:" + orchestrate.DashboardServiceName)
	dashDisableI := idx("disable:" + orchestrate.DashboardServiceName)
	dashRmI := idx("rmdash:" + orchestrate.DashboardServiceName)
	containerStopI := idx("stop:villa-llama.service")

	// stop → disable → remove the dashboard, and all before the container stop.
	if !(dashStopI >= 0 && dashDisableI > dashStopI && dashRmI > dashDisableI) {
		t.Fatalf("dashboard teardown order (stop→disable→remove) violated: %v", f.order)
	}
	if !(containerStopI > dashRmI) {
		t.Fatalf("dashboard must be torn down before the container services: %v", f.order)
	}
}

// TestUninstallKeepModels: --keep-models leaves the models dir untouched.
func TestUninstallKeepModels(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if f.removedModels {
		t.Fatalf("--keep-models must NOT remove models")
	}
}

// TestUninstallRemoveModels: --remove-models deletes the GGUFs AFTER the rest of the
// teardown (models removal is the last data-destroying step before linger).
func TestUninstallRemoveModels(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{removeModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if !f.removedModels {
		t.Fatalf("--remove-models must remove models")
	}
	// models removal happens after volume removal and before linger disable.
	pos := func(want string) int {
		for i, s := range f.order {
			if s == want {
				return i
			}
		}
		return -1
	}
	if !(pos("rmvol") < pos("rmmodels") && pos("rmmodels") < pos("linger")) {
		t.Fatalf("model removal ordering wrong: %v", f.order)
	}
}

// TestUninstallBothFlagsExit1: --keep-models and --remove-models are mutually
// exclusive.
func TestUninstallBothFlagsExit1(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, _ := uninstallTestCmd()

	code := runUninstall(cmd, uninstallOpts{keepModels: true, removeModels: true}, f.uninstallDeps)
	if code != exitBlocked {
		t.Fatalf("both flags must exit %d, got %d", exitBlocked, code)
	}
	// Zero side effects on the mutually-exclusive error.
	if len(f.order) != 0 {
		t.Fatalf("both-flags error must have zero side effects, got %v", f.order)
	}
}

// TestUninstallNonInteractiveDefaultKeep: neither flag + non-interactive defaults to
// keep-models (the safe choice) and does NOT remove models.
func TestUninstallNonInteractiveDefaultKeep(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	f.interactive = func() bool { return false }
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if f.removedModels {
		t.Fatalf("non-interactive default must KEEP models")
	}
}

// TestUninstallInteractivePromptRemove: neither flag + interactive + user consents →
// models removed.
func TestUninstallInteractivePromptRemove(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	f.interactive = func() bool { return true }
	f.consent = func(string) bool { return true } // user answers "yes, remove"
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if !f.removedModels {
		t.Fatalf("interactive consent to remove must remove models")
	}
}

// TestUninstallConfigPreserved: uninstall NEVER deletes config.toml. The deps set has
// no config-delete seam at all; this test asserts a real config.toml on disk survives
// a full run (the verb has no path that touches it).
func TestUninstallConfigPreserved(t *testing.T) {
	dir := t.TempDir()
	cfgPath := filepath.Join(dir, "config.toml")
	if err := os.WriteFile(cfgPath, []byte("model = \"x\"\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{removeModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if _, err := os.Stat(cfgPath); err != nil {
		t.Fatalf("config.toml must be preserved: %v", err)
	}
}

// TestUninstallSELinuxBooleanNotReverted: the deps struct has no setsebool-revert
// field and the source contains no setsebool call — verified by the seborevert
// counter staying 0 (the seam simply does not exist for the verb to call).
func TestUninstallSELinuxBooleanNotReverted(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	cmd, out, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("unexpected exit %d", code)
	}
	if f.reverts != 0 {
		t.Fatalf("SELinux boolean must NOT be reverted")
	}
	// The verb must SURFACE the deliberate host change rather than silently undo it.
	if !strings.Contains(out.String(), "container_use_devices") {
		t.Fatalf("uninstall should surface the left-set SELinux boolean; got: %s", out.String())
	}
}

// TestUninstallStopFailureShortCircuits: a stop failure aborts before removing units
// (no partial teardown that leaves dangling units after a failed stop).
func TestUninstallStopFailureShortCircuits(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, sampleUnits(), dir)
	f.stop = func(string) error { return errors.New("stop boom") }
	f.uninstallDeps.stop = f.stop
	cmd, _, _ := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitBlocked {
		t.Fatalf("stop failure must exit %d, got %d", exitBlocked, code)
	}
	if len(f.removedUnits) != 0 {
		t.Fatalf("stop failure must not remove any unit files")
	}
}

// renderedUninstallUnits renders the REAL stack (5 units incl. villa-openwebui.volume)
// so the uninstall teardown tests reflect the actual Phase-4 rendered volume set —
// not the hand-built sampleUnits() that predates the owui volume.
func renderedUninstallUnits(t *testing.T) []orchestrate.Unit {
	t.Helper()
	return loopbackUnits(t)
}

// TestNonModelVolumesIncludesOpenWebUI: once Render() emits villa-openwebui.volume,
// the reserved nonModelVolumes() seam returns exactly ["villa-openwebui"] and still
// excludes the bind-mounted villa-models volume (D-11). This is the verify-don't-fork
// assertion: nonModelVolumes was already generic, so no code change was required.
func TestNonModelVolumesIncludesOpenWebUI(t *testing.T) {
	units := renderedUninstallUnits(t)
	vols := nonModelVolumes(units)

	want := []string{"villa-openwebui"}
	if len(vols) != len(want) || vols[0] != want[0] {
		t.Fatalf("nonModelVolumes = %v, want %v (the owui volume must flow through; villa-models excluded)", vols, want)
	}
	for _, v := range vols {
		if v == modelVolumeName {
			t.Fatalf("nonModelVolumes must EXCLUDE the model bind-mount volume %q", modelVolumeName)
		}
	}
}

// TestUninstallRemovesOpenWebUIVolume: the teardown removeVolumes seam fires with the
// owui volume (the reserved Phase-3 no-op path now actually removes data), in the
// correct ordering relative to daemon-reload and linger (D-11 / CHAT-03).
func TestUninstallRemovesOpenWebUIVolume(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, renderedUninstallUnits(t), dir)
	cmd, _, errOut := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", exitPass, code, errOut.String())
	}

	// The owui volume must be the one passed to removeVolumes, and villa-models must
	// NOT be (it is governed by keep/remove-models).
	var sawOWUI, sawModels bool
	for _, v := range f.removedVols {
		switch v {
		case "villa-openwebui":
			sawOWUI = true
		case modelVolumeName:
			sawModels = true
		}
	}
	if !sawOWUI {
		t.Fatalf("uninstall must remove the villa-openwebui volume; removed=%v", f.removedVols)
	}
	if sawModels {
		t.Fatalf("uninstall must NOT remove the model bind-mount volume via removeVolumes; removed=%v", f.removedVols)
	}

	// Ordering: rmvol after reload, before linger (D-11).
	pos := func(want string) int {
		for i, s := range f.order {
			if s == want {
				return i
			}
		}
		return -1
	}
	if !(pos("reload") < pos("rmvol") && pos("rmvol") < pos("linger")) {
		t.Fatalf("owui volume removal ordering wrong: %v", f.order)
	}
}

// TestUninstallStopsOpenWebUIBeforeInference (CR-01): teardown must stop services in
// the REVERSE of install's start order — dependents before backends — so the owui
// container (After=villa-llama.service) is never left running with its declared backend
// already stopped. Symmetric to TestInstallStartsInferenceBeforeOpenWebUI.
func TestUninstallStopsOpenWebUIBeforeInference(t *testing.T) {
	dir := t.TempDir()
	f := newFakeUninstallDeps(t, renderedUninstallUnits(t), dir)
	cmd, _, errOut := uninstallTestCmd()

	if code := runUninstall(cmd, uninstallOpts{keepModels: true}, f.uninstallDeps); code != exitPass {
		t.Fatalf("expected exit %d, got %d (stderr: %s)", exitPass, code, errOut.String())
	}

	pos := func(svc string) int {
		for i, s := range f.stopped {
			if s == svc {
				return i
			}
		}
		return -1
	}
	owuiI, infI := pos("villa-openwebui.service"), pos("villa-llama.service")
	if owuiI < 0 || infI < 0 {
		t.Fatalf("both services must be stopped; stopped=%v", f.stopped)
	}
	if owuiI > infI {
		t.Fatalf("CR-01: owui must stop BEFORE inference (reverse dependency teardown); stopped=%v", f.stopped)
	}
}

// TestVolumeRmArgsHasNoIgnoreFlag: the pure arg-builder must emit
// `volume rm --force <name>` and NEVER the unsupported `--ignore` token (the bug
// that exited 125). --force must still be present so a stopped-but-present volume
// removes cleanly.
func TestVolumeRmArgsHasNoIgnoreFlag(t *testing.T) {
	args := volumeRmArgs("villa-openwebui")
	want := []string{"volume", "rm", "--force", "villa-openwebui"}
	if len(args) != len(want) {
		t.Fatalf("volumeRmArgs = %v, want %v", args, want)
	}
	for i := range want {
		if args[i] != want[i] {
			t.Fatalf("volumeRmArgs = %v, want %v", args, want)
		}
	}
	for _, a := range args {
		if a == "--ignore" {
			t.Fatalf("volumeRmArgs must NOT contain the unsupported --ignore flag: %v", args)
		}
	}
	var sawForce bool
	for _, a := range args {
		if a == "--force" {
			sawForce = true
		}
	}
	if !sawForce {
		t.Fatalf("volumeRmArgs must contain --force: %v", args)
	}
}

// TestRemoveVolumesLiveSuccess: a runner that succeeds removes the volume and
// returns nil (the happy path, no host required).
func TestRemoveVolumesLiveSuccess(t *testing.T) {
	var gotArgs []string
	orig := podmanVolumeRm
	t.Cleanup(func() { podmanVolumeRm = orig })
	podmanVolumeRm = func(args []string) (string, error) {
		gotArgs = args
		return "", nil
	}
	if err := removeVolumesLive([]string{"villa-openwebui"}); err != nil {
		t.Fatalf("removeVolumesLive success path returned err: %v", err)
	}
	if len(gotArgs) == 0 || gotArgs[0] != "volume" {
		t.Fatalf("podmanVolumeRm received unexpected args: %v", gotArgs)
	}
}

// TestRemoveVolumesLiveToleratesMissing: a not-found stderr (without --ignore) is
// treated as SUCCESS — idempotent re-uninstall preserved by stderr inspection.
func TestRemoveVolumesLiveToleratesMissing(t *testing.T) {
	orig := podmanVolumeRm
	t.Cleanup(func() { podmanVolumeRm = orig })
	podmanVolumeRm = func(args []string) (string, error) {
		return "Error: no such volume villa-openwebui", errors.New("exit status 1")
	}
	if err := removeVolumesLive([]string{"villa-openwebui"}); err != nil {
		t.Fatalf("removeVolumesLive must tolerate a not-found volume as success, got: %v", err)
	}
}

// TestRemoveVolumesLiveSurfacesStderr: a genuine (non-not-found) failure returns a
// non-nil error whose message CONTAINS the trimmed stderr, so it is diagnosable.
func TestRemoveVolumesLiveSurfacesStderr(t *testing.T) {
	orig := podmanVolumeRm
	t.Cleanup(func() { podmanVolumeRm = orig })
	podmanVolumeRm = func(args []string) (string, error) {
		return "Error: volume villa-openwebui is in use", errors.New("exit status 2")
	}
	err := removeVolumesLive([]string{"villa-openwebui"})
	if err == nil {
		t.Fatalf("removeVolumesLive must surface a genuine podman failure, got nil")
	}
	if !strings.Contains(err.Error(), "volume villa-openwebui is in use") {
		t.Fatalf("removeVolumesLive error must contain the trimmed stderr, got: %v", err)
	}
}

// TestUninstallRegistered: `villa uninstall` is wired into the root command tree.
func TestUninstallRegistered(t *testing.T) {
	root := newRoot()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "uninstall" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("uninstall verb not registered on root")
	}
}
