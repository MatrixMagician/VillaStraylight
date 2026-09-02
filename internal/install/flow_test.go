package install

import (
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// flow_test.go drives Run through the fake (fake_test.go) and asserts the flow's
// invariants at the Deps interface: refusal before mutation, the gates resolved
// once, --dry-run's zero side effects, the start order, and rollback on a failure
// after mutation began. The Result is the test surface; the narration is asserted
// only where its wording is a contract.

func seloffCheck() preflight.CheckResult {
	return preflight.CheckResult{
		ID: "PRE-05", Name: "SELinux container_use_devices boolean", Tier: preflight.TierBlock,
		Status: preflight.StatusWarn, Detail: "container_use_devices is OFF",
		Remediation: "run `setsebool -P container_use_devices=true`.",
	}
}

func lingeroffCheck() preflight.CheckResult {
	return preflight.CheckResult{
		ID: "PRE-03", Name: "User lingering enabled", Tier: preflight.TierWarn,
		Status: preflight.StatusWarn, Detail: "lingering is NOT enabled",
		Remediation: "loginctl enable-linger tester",
	}
}

// memoryUnits returns a Changed plan carrying the memory units, so a memory-on run
// reaches the write→start path and passes the unit-present start gate.
func memoryUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.QdrantContainerUnitName(), Text: "[Container]\n"},
		{Name: orchestrate.EmbedContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

// webUnits returns a Changed plan carrying the web-search units.
func webUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "x"},
		{Name: orchestrate.SearXNGContainerUnitName(), Text: "s"},
		{Name: orchestrate.WebsafeContainerUnitName(), Text: "w"},
	}
	return units, orchestrate.Plan{Changed: units}
}

func contains(ss []string, want string) bool { return idx(ss, want) >= 0 }

// idx returns the index of v in s, or -1 if absent.
func idx(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestInstallDryRunWritesNothing: --dry-run prints the rendered units and calls
// WriteUnits zero times, exiting 0 (ORCH success-criterion 1). It must also pull
// nothing and persist no config (a dry run has zero side effects, F-1/F-2).
func TestInstallDryRunWritesNothing(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\nImage=x\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.downloaded = false // even with a model absent, --dry-run must NOT pull

	code, out, _ := f.run(Opts{DryRun: true})
	if code != exitPass {
		t.Fatalf("--dry-run exit = %d, want 0", code)
	}
	if f.writeCalls != 0 {
		t.Errorf("--dry-run called WriteUnits %d times, want 0", f.writeCalls)
	}
	if f.reloadCalls != 0 || f.startCalls != 0 {
		t.Errorf("--dry-run touched systemd (reload=%d start=%d), want 0", f.reloadCalls, f.startCalls)
	}
	if f.pullCalls != 0 {
		t.Errorf("--dry-run must not pull the model, pulled %d times", f.pullCalls)
	}
	if f.saveCalls != 0 {
		t.Errorf("--dry-run must not persist config, saved %d times", f.saveCalls)
	}
	if !strings.Contains(out.String(), "[Container]") {
		t.Errorf("--dry-run must print rendered unit text, got %q", out.String())
	}
}

// TestInstallIdempotentNoOp: a second run with identical on-disk units (empty
// Changed) writes nothing, reloads nothing, starts nothing, and exits 0. With the
// model already present it also pulls nothing — but it STILL persists config so
// the source of truth is guaranteed even on the no-op path (F-2).
func TestInstallIdempotentNoOp(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Unchanged: units} // zero Changed = true no-op
	f := newFakeDeps(t, units, plan, passChecks())
	// Pre-seed the on-disk dashboard unit so it ALREADY matches the rendered bytes
	// (rendered from the SAME path the fake's ResolveBinaryPath returns), so the
	// dashboard reconcile is also a true no-op: zero writes, zero reloads, zero
	// dashboard restarts (05-08).
	f.diskUnit = mustRenderDashboardUnit(t, "/opt/villa/bin/villa")

	res, out, _ := f.runResult(Opts{})
	if res.Outcome != NoChange {
		t.Fatalf("no-op outcome = %q, want %q", res.Outcome, NoChange)
	}
	if f.writeCalls != 0 || f.reloadCalls != 0 {
		t.Errorf("no-op must not write/reload: write=%d reload=%d", f.writeCalls, f.reloadCalls)
	}
	if f.pullCalls != 0 {
		t.Errorf("no-op with a present model must not pull, pulled %d times", f.pullCalls)
	}
	if f.dashWriteCalls != 0 {
		t.Errorf("true-no-op must not write the dashboard unit, wrote %d times", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 0 {
		t.Errorf("true-no-op must not enable the dashboard unit, enabled %d times", f.dashEnableCalls)
	}
	if contains(f.startOrder, orchestrate.DashboardServiceName) {
		t.Errorf("true-no-op must not (re)start the dashboard service, startOrder = %v", f.startOrder)
	}
	if !strings.Contains(strings.ToLower(out.String()), "no changes") {
		t.Errorf("no-op should report no changes, got %q", out.String())
	}
}

// TestInstallAutoPullsAbsentModelThenStarts: on a model-absent host install pulls
// the recommended weights (EnsureModel fires exactly once) BEFORE writing units
// and starting the service — the F-1 fix. With a present model it pulls zero times.
func TestInstallAutoPullsAbsentModelThenStarts(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	f := newFakeDeps(t, units, plan, passChecks())
	f.downloaded = false

	code, out, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("absent-model install exit = %d, want 0", code)
	}
	if f.pullCalls != 1 {
		t.Errorf("absent model must be pulled exactly once, pulled %d times", f.pullCalls)
	}
	if f.writeCalls != 1 || f.startCalls != 3 {
		t.Errorf("install must still write+start all three services after the pull (llama, owui, dashboard): write=%d start=%d", f.writeCalls, f.startCalls)
	}
	if !strings.Contains(strings.ToLower(out.String()), "downloading") {
		t.Errorf("install should announce the download, got %q", out.String())
	}

	f2 := newFakeDeps(t, units, plan, passChecks())
	f2.downloaded = true
	if code, _, _ := f2.run(Opts{}); code != exitPass {
		t.Fatalf("present-model install exit = %d, want 0", code)
	}
	if f2.pullCalls != 0 {
		t.Errorf("a present model must not be re-pulled, pulled %d times", f2.pullCalls)
	}
}

// TestInstallStartsInferenceBeforeOpenWebUI: on the write path install starts
// villa-llama.service strictly BEFORE villa-openwebui.service — Open WebUI must
// come up after inference so it can reach a live backend.
func TestInstallStartsInferenceBeforeOpenWebUI(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())

	code, _, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	// The dashboard is reconciled BEFORE the container starts, so only the
	// inference→owui relative order is asserted.
	llamaI, owuiI := idx(f.startOrder, installServiceName), idx(f.startOrder, openWebUIServiceName)
	if llamaI < 0 || owuiI < 0 || llamaI >= owuiI {
		t.Fatalf("start-call order = %v, want inference before owui", f.startOrder)
	}
}

// TestInstallReconcilesDashboardForBootSurvival: install renders+writes+enables+
// starts the native villa-dashboard.service for boot-survival: the unit is written
// once, enabled once for [Install] WantedBy=default.target, and started exactly once.
func TestInstallReconcilesDashboardForBootSurvival(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	if f.dashWriteCalls != 1 {
		t.Errorf("dashboard unit write calls = %d, want 1", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 1 || len(f.dashEnabled) != 1 || f.dashEnabled[0] != orchestrate.DashboardServiceName {
		t.Errorf("dashboard enable = %v (count %d), want one enable of %s", f.dashEnabled, f.dashEnableCalls, orchestrate.DashboardServiceName)
	}
	if n := countOf(f.startOrder, orchestrate.DashboardServiceName); n != 1 {
		t.Errorf("dashboard service started %d times, want exactly 1 (startOrder = %v)", n, f.startOrder)
	}
}

func countOf(ss []string, want string) int {
	n := 0
	for _, s := range ss {
		if s == want {
			n++
		}
	}
	return n
}

// TestInstallReconcilesDashboardUnitOnNoOpPath is the primary regression for the
// 05-08 gap (UAT Test 5): a STALE on-disk dashboard unit (old ExecStart) plus an
// UNCHANGED container plan must STILL rewrite/enable/(re)start the dashboard — the
// flow must not return at the no-op early-return BEFORE reconciling the dashboard.
// The two lifecycles are decoupled in the correct direction: the container units are
// NOT spuriously rewritten, and the rewrite carries the resolved absolute ExecStart.
func TestInstallReconcilesDashboardUnitOnNoOpPath(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Unchanged: units} // zero Changed = no-op CONTAINER path
	f := newFakeDeps(t, units, plan, passChecks())
	f.diskUnit = []byte("[Unit]\n" +
		"Description=VillaStraylight control dashboard (read-only observer)\n" +
		"After=default.target\n\n" +
		"[Service]\n" +
		"Type=simple\n" +
		"ExecStart=%h/.local/bin/villa dashboard\n" +
		"Restart=on-failure\n\n" +
		"[Install]\n" +
		"WantedBy=default.target\n")
	if bytes.Equal(f.diskUnit, mustRenderDashboardUnit(t, "/opt/villa/bin/villa")) {
		t.Fatal("test setup error: the stale unit must differ from the rendered bytes")
	}

	code, _, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("no-op-container + stale-dashboard install exit = %d, want exitPass", code)
	}
	if f.dashWriteCalls != 1 {
		t.Errorf("stale dashboard unit must be rewritten exactly once, wrote %d times", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 1 {
		t.Errorf("reconciled dashboard unit must be enabled exactly once, enabled %d times", f.dashEnableCalls)
	}
	if n := countOf(f.startOrder, orchestrate.DashboardServiceName); n != 1 {
		t.Errorf("reconciled dashboard service must be (re)started exactly once, startOrder = %v", f.startOrder)
	}
	if f.writeCalls != 0 {
		t.Errorf("the unchanged container plan must NOT be rewritten, writeCalls = %d", f.writeCalls)
	}
	if !filepath.IsAbs(f.dashBinaryPath) {
		t.Errorf("reconciled dashboard ExecStart must use the resolved absolute path, got %q", f.dashBinaryPath)
	}
	if f.dashBinaryPath != "/opt/villa/bin/villa" {
		t.Errorf("reconciled dashboard binary path = %q, want the resolver's path /opt/villa/bin/villa", f.dashBinaryPath)
	}
}

// TestInstallDashboardUnitTargetsResolvedBinary (UAT Test 5 fix): the flow must
// take the binary path from the ResolveBinaryPath seam and thread it into the
// WriteDashboardUnit seam, so the rendered ExecStart points at the resolved binary —
// never a fixed ~/.local/bin/villa (which produced 203/EXEC at boot). The live
// resolver's own contract (absolute, executable) is tested in the command tier.
func TestInstallDashboardUnitTargetsResolvedBinary(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	const resolved = "/srv/villa/bin/villa"
	f.ResolveBinaryPath = func() (string, error) { return resolved, nil }

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}
	if f.dashWriteCalls != 1 {
		t.Fatalf("dashboard write calls = %d, want 1", f.dashWriteCalls)
	}
	if f.dashBinaryPath != resolved {
		t.Fatalf("threaded binary path = %q, want the resolver's %q", f.dashBinaryPath, resolved)
	}
	body, err := orchestrate.RenderDashboardUnit(f.dashBinaryPath)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	if !strings.Contains(body, "ExecStart=") || !strings.Contains(body, resolved) {
		t.Fatalf("rendered unit ExecStart does not reference the resolved path %q\n%s", resolved, body)
	}
}

// TestInstallFailsClosedWhenBinaryUnresolvable: when the binary-path resolver
// errors, install must FAIL and write NO dashboard unit — it must never fall back
// to a fixed path.
func TestInstallFailsClosedWhenBinaryUnresolvable(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.ResolveBinaryPath = func() (string, error) { return "", errors.New("os.Executable: boom") }

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Blocked {
		t.Fatalf("outcome = %q, want %q when the binary path is unresolvable", res.Outcome, Blocked)
	}
	if res.Outcome.ExitCode() != exitBlocked {
		t.Fatalf("exit = %d, want exitBlocked (%d)", res.Outcome.ExitCode(), exitBlocked)
	}
	if f.dashWriteCalls != 0 {
		t.Errorf("dashboard unit was written %d time(s) despite an unresolvable binary path; want 0 (no fixed-path fallback)", f.dashWriteCalls)
	}
	if !strings.Contains(errOut.String(), "resolve the villa binary path") {
		t.Errorf("expected a binary-path resolution error on stderr, got:\n%s", errOut.String())
	}
}

// TestInstallEnsuresModelBeforeAnyStart: EnsureModel (when the model is absent)
// is invoked BEFORE any service start — Open WebUI must not come up before the
// model exists or the picker would be empty on first visit (MODEL-04 / F-1).
func TestInstallEnsuresModelBeforeAnyStart(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.downloaded = false

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("absent-model install exit = %d, want 0", code)
	}
	if len(f.callOrder) == 0 || f.callOrder[0] != "ensureModel" {
		t.Fatalf("ensureModel must run before any start, call order = %v", f.callOrder)
	}
}

// TestInstallDryRunStartsNothing: under --dry-run no service is started and
// EnsureModel is not called (the dry-run zero-side-effect contract).
func TestInstallDryRunStartsNothing(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.downloaded = false

	if code, _, _ := f.run(Opts{DryRun: true}); code != exitPass {
		t.Fatalf("--dry-run exit = %d, want 0", code)
	}
	if len(f.startOrder) != 0 {
		t.Errorf("--dry-run must start no service, started %v", f.startOrder)
	}
	if f.pullCalls != 0 {
		t.Errorf("--dry-run must not ensureModel, pulled %d times", f.pullCalls)
	}
	if f.dashWriteCalls != 0 || f.dashEnableCalls != 0 {
		t.Errorf("--dry-run must not write/enable the dashboard unit, write=%d enable=%d", f.dashWriteCalls, f.dashEnableCalls)
	}
}

// TestInstallOpenWebUIStartFailureBlocks: a start failure for the owui service is
// a hard refusal (exit 1, rolled back) with a clear "start villa-openwebui.service
// failed" message — mirrors the inference start-failure path.
func TestInstallOpenWebUIStartFailureBlocks(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.Start = func(service string) error {
		f.startCalls++
		f.startOrder = append(f.startOrder, service)
		if service == openWebUIServiceName {
			return errors.New("unit not found")
		}
		return nil
	}

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Refused || res.Outcome.ExitCode() != exitBlocked {
		t.Fatalf("owui-start-failure outcome = %q (exit %d), want %q / 1", res.Outcome, res.Outcome.ExitCode(), Refused)
	}
	if !strings.Contains(errOut.String(), "start villa-openwebui.service failed") {
		t.Errorf("owui start failure must surface a clear error, got %q", errOut.String())
	}
}

// TestInstallPullFailureBlocks: an EnsureModel failure (e.g. network/verify
// failure) is a hard BLOCK (exit 1) — install must NOT proceed to persist config,
// write units, or start a service that would crash-loop on a missing GGUF (F-1).
func TestInstallPullFailureBlocks(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.downloaded = false
	f.EnsureModel = func(recommend.Recommendation) error {
		f.pullCalls++
		return errors.New("sha256 mismatch")
	}

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Blocked {
		t.Fatalf("pull-failure outcome = %q, want %q", res.Outcome, Blocked)
	}
	if f.saveCalls != 0 || f.writeCalls != 0 || f.startCalls != 0 {
		t.Errorf("a failed pull must not persist/write/start: save=%d write=%d start=%d", f.saveCalls, f.writeCalls, f.startCalls)
	}
	if !strings.Contains(errOut.String(), "download model") {
		t.Errorf("pull failure should surface a download error, got %q", errOut.String())
	}
}

// TestInstallPersistsConfigBeforeUnits: a clean install persists the recommended
// model/quant/ctx/backend to config.toml exactly once, BEFORE the units are
// written (the F-2 fix) — so the lifecycle verbs render from the same config and
// install-written units match config-rendered units (a true no-op follow-up).
func TestInstallPersistsConfigBeforeUnits(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())

	var order []string
	f.SaveConfig = func(c config.VillaConfig) error {
		f.saveCalls++
		f.savedCfg = c
		order = append(order, "save")
		return nil
	}
	f.WriteUnits = func(orchestrate.Plan, string) error {
		f.writeCalls++
		order = append(order, "write")
		return nil
	}

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	if f.saveCalls != 1 {
		t.Errorf("install must persist config exactly once, saved %d times", f.saveCalls)
	}
	if len(order) != 2 || order[0] != "save" || order[1] != "write" {
		t.Errorf("config must be persisted BEFORE units are written, got order %v", order)
	}
	if f.savedCfg.Model != "qwen2.5-0.5b" || f.savedCfg.Quant != "Q4_K_M" ||
		f.savedCfg.Ctx != 4096 || f.savedCfg.Backend != "rocm" {
		t.Errorf("persisted config must hold the recommended selection, got %+v", f.savedCfg)
	}
}

// TestInstallPersistedConfigIsReconcileNoOp: the config install persists is the
// SAME config a follow-up `up`/reconcile renders from — so a post-install lifecycle
// reconcile is a TRUE no-op (render is a pure function of cfg).
func TestInstallPersistedConfigIsReconcileNoOp(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\nExec=llama --ctx 4096\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())

	var renderedFrom config.VillaConfig
	f.Render = func(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
		renderedFrom = in.Cfg
		return units, nil
	}

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}
	if !reflect.DeepEqual(f.savedCfg, renderedFrom) {
		t.Errorf("persisted cfg %+v must equal the cfg install rendered from %+v — otherwise a follow-up up/restart would diff and not be a no-op", f.savedCfg, renderedFrom)
	}
	if f.savedCfg.Model == "" {
		t.Fatalf("install must persist a non-empty model so up/restart can resolve it (F-2)")
	}
}

// TestInstallBlockWithoutConsentExits1: a BLOCK preflight gap (SELinux off) with
// consent declined and no --force blocks and prints the exact setsebool command.
func TestInstallBlockWithoutConsentExits1(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.Consent = func(string) bool { return false } // declined

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Blocked || res.Outcome.ExitCode() != exitBlocked {
		t.Fatalf("BLOCK-without-consent outcome = %q, want %q (exit 1)", res.Outcome, Blocked)
	}
	if f.writeCalls != 0 || f.startCalls != 0 {
		t.Errorf("blocked install must not write/start: write=%d start=%d", f.writeCalls, f.startCalls)
	}
	if f.pullCalls != 0 || f.saveCalls != 0 {
		t.Errorf("a gate-blocked install must not pull/persist (gate precedes both): pull=%d save=%d", f.pullCalls, f.saveCalls)
	}
	if !strings.Contains(errOut.String(), "setsebool -P container_use_devices=true") {
		t.Errorf("blocked install must print the copy-paste setsebool command, got %q", errOut.String())
	}
}

// TestWizardBlockDeclinedCopy proves the wizard-decline path emits the EXACT
// contracted "BLOCK gap declined" line verbatim, with <check name> = c.Name and
// <remediation> = c.Remediation substituted, while the 0/2/1 exit contract is
// unchanged: a declined BLOCK gap with no --force stays Blocked with zero host
// mutation; --force still degrades to WARN without emitting the decline copy as a
// hard block.
func TestWizardBlockDeclinedCopy(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// declineWizard simulates the collector returning a declined consent for the
	// privileged BLOCK gap (PRE-05), so the single gate consumes the threaded false
	// WITHOUT re-prompting stdin.
	declineWizard := func(d *Deps) {
		d.Interactive = func() bool { return true }
		d.StdoutIsTTY = func() bool { return true }
		d.Consent = func(prompt string) bool {
			t.Errorf("Consent must NOT be re-invoked on the threaded wizard-decline path (%q)", prompt)
			return false
		}
		d.Wizard = func(context.Context, WizardInput) (WizardResult, error) {
			return WizardResult{Consents: map[string]bool{"PRE-05": false}}, nil
		}
	}

	const wantLine = "BLOCK: SELinux container_use_devices boolean. run `setsebool -P container_use_devices=true`.. " +
		"Run the suggested command, or re-run with --no-tui --force to override (auditable)."

	t.Run("declined-without-force-blocks-with-contracted-copy", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		declineWizard(f.Deps)

		res, _, errOut := f.runResult(Opts{})
		if res.Outcome != Blocked {
			t.Fatalf("declined BLOCK gap (no --force) outcome = %q, want %q", res.Outcome, Blocked)
		}
		if !strings.Contains(errOut.String(), wantLine) {
			t.Errorf("wizard-decline output missing the contracted BLOCK-gap-declined copy.\n got: %q\nwant substring: %q", errOut.String(), wantLine)
		}
		if f.seboolCalls != 0 {
			t.Errorf("declined gap must NOT run setsebool, ran %d times", f.seboolCalls)
		}
		if f.writeCalls != 0 || f.startCalls != 0 || f.pullCalls != 0 || f.saveCalls != 0 {
			t.Errorf("a declined BLOCK install must not mutate: write=%d start=%d pull=%d save=%d",
				f.writeCalls, f.startCalls, f.pullCalls, f.saveCalls)
		}
	})

	t.Run("force-override-degrades-to-warn", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		declineWizard(f.Deps)

		res, _, _ := f.runResult(Opts{Force: true})
		if res.Outcome != Degraded || res.Outcome.ExitCode() != exitWarn {
			t.Fatalf("declined BLOCK gap with --force outcome = %q, want %q (exit 2)", res.Outcome, Degraded)
		}
		if f.writeCalls != 1 {
			t.Errorf("--force override must proceed to write units once, wrote %d times", f.writeCalls)
		}
	})
}

// TestInstallConsentYesRunsSeamOncePerGap: with an interactive TTY and y consent,
// the SELinux (BLOCK) and linger (WARN) gaps each invoke their fixed-arg seam
// exactly once, and install proceeds to write/start.
func TestInstallConsentYesRunsSeamOncePerGap(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck(), lingeroffCheck()})
	f.Interactive = func() bool { return true }
	f.Consent = func(string) bool { return true }

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("consent-yes exit = %d, want 0", code)
	}
	if f.seboolCalls != 1 {
		t.Errorf("setsebool invoked %d times, want exactly 1", f.seboolCalls)
	}
	if f.lingerCalls != 1 {
		t.Errorf("enable-linger invoked %d times, want exactly 1", f.lingerCalls)
	}
	if f.writeCalls != 1 || f.startCalls != 3 {
		t.Errorf("consented install must write+start all three services (llama, owui, dashboard): write=%d start=%d", f.writeCalls, f.startCalls)
	}
}

// TestInstallWarnLingerOfferGoesToStdout: a WARN-tier linger (PRE-03) offer is a
// non-blocking, optional host-prep — its messaging must go to STDOUT, not stderr,
// so scripts parsing stderr do not misread it as an error. Declining it must
// not block the install.
func TestInstallWarnLingerOfferGoesToStdout(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{lingeroffCheck()})
	f.Interactive = func() bool { return true }
	f.Consent = func(string) bool { return false }

	code, out, errOut := f.run(Opts{})
	if code == exitBlocked {
		t.Fatalf("declining an optional WARN linger offer must not block, exit = %d", code)
	}
	if f.lingerCalls != 0 {
		t.Errorf("declined optional offer must not run enable-linger, ran %d times", f.lingerCalls)
	}
	if !strings.Contains(out.String(), "loginctl enable-linger tester") {
		t.Errorf("optional linger offer command must be printed to STDOUT, got stdout=%q", out.String())
	}
	if strings.Contains(errOut.String(), "loginctl enable-linger") {
		t.Errorf("optional WARN linger offer must NOT be written to stderr (reads as an error), got stderr=%q", errOut.String())
	}
	if strings.Contains(errOut.String(), "host-prep needed") {
		t.Errorf("the BLOCK-gap stderr wording must not be used for a WARN offer, got stderr=%q", errOut.String())
	}
}

// TestInstallDryRunNeverRunsPrivilegedHostPrep: --dry-run on an interactive TTY
// with BLOCK (PRE-05) and WARN (PRE-03) gaps and a CONSENTING stub must execute
// ZERO privileged seams, never prompt, and never enter the wizard — a dry run is a
// zero-side-effect contract.
func TestInstallDryRunNeverRunsPrivilegedHostPrep(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck(), lingeroffCheck()})
	f.Interactive = func() bool { return true }
	f.StdoutIsTTY = func() bool { return true }
	consentCalls := 0
	f.Consent = func(string) bool { consentCalls++; return true }

	res, _, errOut := f.runResult(Opts{DryRun: true})
	if res.Outcome != Blocked {
		t.Fatalf("dry-run with an unmet BLOCK gap outcome = %q, want %q", res.Outcome, Blocked)
	}
	if consentCalls != 0 {
		t.Errorf("--dry-run must never prompt for consent, prompted %d times", consentCalls)
	}
	if f.seboolCalls != 0 || f.lingerCalls != 0 {
		t.Errorf("--dry-run executed privileged host-prep (sebool=%d linger=%d), want 0/0 — zero-side-effect contract breach", f.seboolCalls, f.lingerCalls)
	}
	if f.wizardCalls != 0 {
		t.Errorf("--dry-run entered the wizard %d times, want 0 (consent collected there would be executable)", f.wizardCalls)
	}
	if f.writeCalls != 0 || f.startCalls != 0 || f.pullCalls != 0 || f.saveCalls != 0 {
		t.Errorf("--dry-run mutated state: write=%d start=%d pull=%d save=%d", f.writeCalls, f.startCalls, f.pullCalls, f.saveCalls)
	}
	if !strings.Contains(errOut.String(), "dry-run — run the command above") {
		t.Errorf("dry-run BLOCK gap should print the dry-run hint, got %q", errOut.String())
	}
}

// TestInstallConsentNoBlocksAndNeverRunsSeam: declining a BLOCK gap invokes the
// seam zero times, prints the command, and blocks (exit 1) unless --force.
func TestInstallConsentNoBlocksAndNeverRunsSeam(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.Interactive = func() bool { return true }
	f.Consent = func(string) bool { return false }

	code, _, errOut := f.run(Opts{})
	if code != exitBlocked {
		t.Fatalf("consent-no exit = %d, want 1", code)
	}
	if f.seboolCalls != 0 {
		t.Errorf("declined gap must not run setsebool, ran %d times", f.seboolCalls)
	}
	if !strings.Contains(errOut.String(), "setsebool -P container_use_devices=true") {
		t.Errorf("declined gap must print the command, got %q", errOut.String())
	}
}

// TestInstallNonInteractiveBlocksAndNeverPrompts: a non-interactive run never
// prompts, prints the command, and blocks the BLOCK gap.
func TestInstallNonInteractiveBlocksAndNeverPrompts(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.Interactive = func() bool { return false }
	consentCalls := 0
	f.Consent = func(string) bool { consentCalls++; return true }

	code, _, errOut := f.run(Opts{})
	if code != exitBlocked {
		t.Fatalf("non-interactive exit = %d, want 1", code)
	}
	if consentCalls != 0 {
		t.Errorf("non-interactive must never prompt, prompted %d times", consentCalls)
	}
	if f.seboolCalls != 0 {
		t.Errorf("non-interactive must not run setsebool, ran %d times", f.seboolCalls)
	}
	if !strings.Contains(errOut.String(), "non-interactive") {
		t.Errorf("non-interactive run should explain itself, got %q", errOut.String())
	}
}

// TestInstallForceOverridesBlock: --force lets an un-consented BLOCK gap proceed
// (auditable), writing/starting and exiting 2 (Degraded, not a clean 0).
func TestInstallForceOverridesBlock(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.Interactive = func() bool { return false }

	res, out, _ := f.runResult(Opts{Force: true})
	if res.Outcome != Degraded || res.Outcome.ExitCode() != exitWarn {
		t.Fatalf("--force outcome = %q (exit %d), want %q / 2", res.Outcome, res.Outcome.ExitCode(), Degraded)
	}
	if !res.GateDegraded {
		t.Error("--force must record GateDegraded: the gap was bypassed, not satisfied")
	}
	if f.writeCalls != 1 || f.startCalls != 3 {
		t.Errorf("--force should proceed and start all three services (llama, owui, dashboard): write=%d start=%d", f.writeCalls, f.startCalls)
	}
	if !strings.Contains(out.String(), "Overridden") {
		t.Errorf("--force must print an auditable override summary, got %q", out.String())
	}
}

// TestInstallPostInstallPrintsLoopbackEndpoint: a clean install prints the loopback
// inference endpoint and the REAL loopback chat URL — with no dead links.
func TestInstallPostInstallPrintsLoopbackEndpoint(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())

	code, out, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:8080") {
		t.Errorf("post-install must print the loopback inference endpoint, got %q", out.String())
	}
	if !strings.Contains(out.String(), "chat (Open WebUI): "+ChatURL) {
		t.Errorf("post-install must print the real loopback chat URL, got %q", out.String())
	}
	if strings.Contains(out.String(), "chat (Open WebUI) and the control dashboard arrive in later phases") {
		t.Errorf("post-install must no longer use the old combined chat/dashboard-later note, got %q", out.String())
	}
}

// TestInstallNoOpPrintsChatURL: the true-no-op path (units already match config)
// also points the user at the live chat URL — a re-run still tells you where to chat.
func TestInstallNoOpPrintsChatURL(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Unchanged: units}
	f := newFakeDeps(t, units, plan, passChecks())

	code, out, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("no-op install exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "chat (Open WebUI): "+ChatURL) {
		t.Errorf("no-op post-install must also print the real chat URL, got %q", out.String())
	}
}

// TestInstallReadinessWarnYieldsExit2: when the readiness poll WARNs, install
// exits 2 (Degraded) and the post-install health summary reflects it.
func TestInstallReadinessWarnYieldsExit2(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.PollReady = func(context.Context, string) Proof {
		return Proof{Status: preflight.StatusWarn, Detail: "server did not become ready before the timeout"}
	}

	res, out, _ := f.runResult(Opts{})
	if res.Outcome != Degraded || res.Outcome.ExitCode() != exitWarn {
		t.Fatalf("readiness-warn outcome = %q (exit %d), want %q / 2", res.Outcome, res.Outcome.ExitCode(), Degraded)
	}
	if !res.ReadinessWarn {
		t.Error("a readiness WARN must be recorded on the Result")
	}
	if !strings.Contains(out.String(), "health: WARN") {
		t.Errorf("post-install should show health WARN, got %q", out.String())
	}
}

// --- memory stack ------------------------------------------------------------

// TestInstallMemoryGateUsesPersistedConfig drives Run through the PERSISTED config
// gate, NOT a hand-built cfg with MemoryEnabled set — so it would catch a gate
// mistakenly bound to the always-false default seed. With memory on, the pre-stage
// AND the memory start AND the proof all fire; with it off, NONE fire.
func TestInstallMemoryGateUsesPersistedConfig(t *testing.T) {
	t.Run("persisted memory_enabled=true fires every memory seam", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("memory-on install exit = %d, want 0", code)
		}
		if f.embedEnsureCalls != 1 {
			t.Errorf("memory-on must pre-stage the embed GGUF once, ensureEmbedModel calls = %d", f.embedEnsureCalls)
		}
		if !contains(f.startOrder, qdrantServiceName) || !contains(f.startOrder, embedServiceName) {
			t.Errorf("memory-on must start villa-qdrant + villa-embed, startOrder = %v", f.startOrder)
		}
		if f.memoryProofCalls != 1 {
			t.Errorf("memory-on must run the readiness proof once, proof calls = %d", f.memoryProofCalls)
		}
	})

	t.Run("persisted memory_enabled=false fires no memory seam", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		f.embedPresent = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("memory-off install exit = %d, want 0", code)
		}
		if f.embedEnsureCalls != 0 {
			t.Errorf("memory-off must not pre-stage, ensureEmbedModel calls = %d", f.embedEnsureCalls)
		}
		if contains(f.startOrder, qdrantServiceName) || contains(f.startOrder, embedServiceName) {
			t.Errorf("memory-off must not start the memory services, startOrder = %v", f.startOrder)
		}
		if f.memoryProofCalls != 0 {
			t.Errorf("memory-off must not run the proof, proof calls = %d", f.memoryProofCalls)
		}
	})
}

// TestInstallPreservesPersistedMemoryConfig: install must SEED cfg from the user's
// persisted config and override ONLY the recommendation-derived fields + the gates —
// never reset customised memory/dashboard/chat fields to seed defaults.
func TestInstallPreservesPersistedMemoryConfig(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true

	persisted := config.DefaultVillaConfig()
	persisted.MemoryEnabled = true
	persisted.EmbeddingModel = "custom-embed-model"
	persisted.ChatPort = 4444
	f.persistedConfig = &persisted

	if code, _, _ := f.run(Opts{}); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}
	if f.savedCfg.EmbeddingModel != "custom-embed-model" {
		t.Errorf("install reset persisted embedding_model to %q, want \"custom-embed-model\" preserved", f.savedCfg.EmbeddingModel)
	}
	if f.savedCfg.ChatPort != 4444 {
		t.Errorf("install reset persisted chat_port to %d, want 4444 preserved", f.savedCfg.ChatPort)
	}
	if f.savedCfg.Model != "qwen2.5-0.5b" || f.savedCfg.Backend != "rocm" {
		t.Errorf("install must still override the recommendation-derived fields, got %+v", f.savedCfg)
	}
}

// TestInstallPreservesPersistedROCmBackend: install (and install --coding-agent)
// must PRESERVE whatever backend the config already carries, not silently revert it
// to the recommendation, in BOTH directions: a persisted `rocm` stays rocm and a
// persisted `vulkan` opt-out stays vulkan. A re-install on an unchanged plan is a
// true no-op on the choice, and an UNSET backend falls through to the recommendation.
func TestInstallPreservesPersistedROCmBackend(t *testing.T) {
	persistedRocm := func() *config.VillaConfig {
		c := config.DefaultVillaConfig()
		c.Backend = "rocm"
		return &c
	}
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}

	t.Run("flag path preserves a persisted rocm opt-in in render and saved config", func(t *testing.T) {
		f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
		f.persistedConfig = persistedRocm()

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("install exit = %d, want 0", code)
		}
		if !f.renderedInputSet {
			t.Fatal("render seam was never invoked — cannot assert the rendered backend")
		}
		if got := f.renderedInput.Backend.Name(); got != "rocm" {
			t.Errorf("install rendered backend %q, want \"rocm\" preserved (config is the single source of truth)", got)
		}
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("install reverted persisted backend to %q, want \"rocm\" preserved", f.savedCfg.Backend)
		}
		if f.savedCfg.Model != "qwen2.5-0.5b" {
			t.Errorf("install must still override the recommendation-derived model, got %q", f.savedCfg.Model)
		}
	})

	t.Run("--coding-agent path preserves a persisted rocm opt-in (the reported repro)", func(t *testing.T) {
		f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
		f.persistedConfig = persistedRocm()

		if code, _, _ := f.run(Opts{CodingAgent: true}); code != exitPass {
			t.Fatalf("install --coding-agent exit = %d, want 0", code)
		}
		if got := f.renderedInput.Backend.Name(); got != "rocm" {
			t.Errorf("install --coding-agent rendered backend %q, want \"rocm\" preserved", got)
		}
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("install --coding-agent reverted persisted backend to %q, want \"rocm\" preserved", f.savedCfg.Backend)
		}
	})

	t.Run("re-install on a persisted rocm config + unchanged plan is a true no-op", func(t *testing.T) {
		f := newFakeDeps(t, units, orchestrate.Plan{Unchanged: units}, passChecks())
		f.persistedConfig = persistedRocm()
		f.diskUnit = mustRenderDashboardUnit(t, "/opt/villa/bin/villa")

		res, _, _ := f.runResult(Opts{})
		if res.Outcome != NoChange {
			t.Fatalf("re-install outcome = %q, want %q", res.Outcome, NoChange)
		}
		if f.writeCalls != 0 {
			t.Errorf("re-install on an unchanged rocm config must NOT rewrite container units, writeCalls = %d", f.writeCalls)
		}
		if contains(f.startOrder, installServiceName) {
			t.Errorf("re-install on an unchanged rocm config must NOT restart the llama service, startOrder = %v", f.startOrder)
		}
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("no-op re-install reverted persisted backend to %q, want \"rocm\" preserved", f.savedCfg.Backend)
		}
	})

	t.Run("a persisted vulkan opt-out is preserved against the rocm recommendation", func(t *testing.T) {
		f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
		c := config.DefaultVillaConfig()
		c.Backend = "vulkan"
		f.persistedConfig = &c

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("install exit = %d, want 0", code)
		}
		if got := f.renderedInput.Backend.Name(); got != "vulkan" {
			t.Errorf("install rendered backend %q, want \"vulkan\" opt-out preserved", got)
		}
		if f.savedCfg.Backend != "vulkan" {
			t.Errorf("install reverted the persisted vulkan opt-out to %q, want \"vulkan\" preserved", f.savedCfg.Backend)
		}
	})

	t.Run("an UNSET persisted backend falls through to the recommendation", func(t *testing.T) {
		f := newFakeDeps(t, units, orchestrate.Plan{Changed: units}, passChecks())
		c := config.DefaultVillaConfig()
		c.Backend = ""
		f.persistedConfig = &c

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("install exit = %d, want 0", code)
		}
		if got := f.renderedInput.Backend.Name(); got != "rocm" {
			t.Errorf("unset-backend install rendered backend %q, want \"rocm\" from the recommendation", got)
		}
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("unset-backend install saved backend %q, want \"rocm\" from the recommendation", f.savedCfg.Backend)
		}
	})
}

// TestInstallMemoryServices: memory on pre-stages when absent and starts the memory
// services in order (Qdrant before embed); gate off / dry-run → none called.
func TestInstallMemoryServices(t *testing.T) {
	t.Run("absent embed model is pre-staged then services start in order", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.embedEnsureCalls != 1 {
			t.Errorf("absent embed model must be pre-staged once, calls = %d", f.embedEnsureCalls)
		}
		ensureIdx := idx(f.callOrder, "ensureEmbedModel")
		qIdx := idx(f.callOrder, "start:"+qdrantServiceName)
		eIdx := idx(f.callOrder, "start:"+embedServiceName)
		if ensureIdx < 0 || qIdx < 0 || eIdx < 0 {
			t.Fatalf("missing expected events in callOrder = %v", f.callOrder)
		}
		if ensureIdx >= eIdx || qIdx >= eIdx {
			t.Errorf("ordering wrong: ensure(%d) and qdrant(%d) must precede embed(%d); callOrder = %v", ensureIdx, qIdx, eIdx, f.callOrder)
		}
	})

	t.Run("present embed model is not re-pulled", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = true

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.embedEnsureCalls != 0 {
			t.Errorf("present embed model must not be re-pulled, calls = %d", f.embedEnsureCalls)
		}
		if !contains(f.startOrder, qdrantServiceName) || !contains(f.startOrder, embedServiceName) {
			t.Errorf("memory services must still start, startOrder = %v", f.startOrder)
		}
	})

	t.Run("gate off pre-stages and starts nothing", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		f.embedPresent = false

		f.run(Opts{})
		if f.embedEnsureCalls != 0 || f.embedPresentCalls != 0 {
			t.Errorf("gate off must not touch the embed model (ensure=%d present=%d)", f.embedEnsureCalls, f.embedPresentCalls)
		}
		if contains(f.startOrder, qdrantServiceName) || contains(f.startOrder, embedServiceName) {
			t.Errorf("gate off must not start memory services, startOrder = %v", f.startOrder)
		}
	})

	t.Run("dry-run pre-stages and starts nothing", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = false

		f.run(Opts{DryRun: true})
		if f.embedEnsureCalls != 0 {
			t.Errorf("dry-run must not pre-stage, ensureEmbedModel calls = %d", f.embedEnsureCalls)
		}
		if f.startCalls != 0 {
			t.Errorf("dry-run must not start anything, startCalls = %d", f.startCalls)
		}
		if f.memoryProofCalls != 0 {
			t.Errorf("dry-run must not run the proof, proof calls = %d", f.memoryProofCalls)
		}
	})
}

// TestInstallMemoryOnButUnitsAbsentFailsClosed: when memory is enabled but the
// memory .container units are absent from the rendered plan, install must NOT start
// a service systemd has never seen. It fails closed with a CLEAR internal-error
// remediation, never a bare "Unit not found", and never starts the memory services.
func TestInstallMemoryOnButUnitsAbsentFailsClosed(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.embedPresent = true

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Refused {
		t.Fatalf("memory-on with absent memory units must fail closed, outcome = %q, want %q", res.Outcome, Refused)
	}
	if !strings.Contains(errOut.String(), "INTERNAL ERROR") ||
		!strings.Contains(errOut.String(), orchestrate.QdrantContainerUnitName()) {
		t.Errorf("expected a clear internal-error message naming the absent memory units, got:\n%s", errOut.String())
	}
	if contains(f.startOrder, qdrantServiceName) || contains(f.startOrder, embedServiceName) {
		t.Errorf("memory services must not be started when their units are absent, startOrder = %v", f.startOrder)
	}
}

// TestInstallMemoryProofPass: a PASS proof verdict leaves the exit code unaffected,
// prints the "memory stack ready" line with the verdict's OWN detail, and the proof
// receives the persisted config (768-dim default).
func TestInstallMemoryProofPass(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.memoryProofStatus = preflight.StatusPass
	f.memoryProofDetail = "768-dim embeddings + Qdrant writable"

	code, out, _ := f.run(Opts{})
	if code != exitPass {
		t.Fatalf("proof-pass exit = %d, want 0", code)
	}
	if f.memoryProofCalls != 1 {
		t.Errorf("proof must run once, calls = %d", f.memoryProofCalls)
	}
	if !strings.Contains(out.String(), "memory stack ready: 768-dim embeddings + Qdrant writable") {
		t.Errorf("a PASS proof must print the verdict's detail, got %q", out.String())
	}
	if f.memoryProofCfg.EmbeddingDim != 768 {
		t.Errorf("proof cfg EmbeddingDim = %d, want 768", f.memoryProofCfg.EmbeddingDim)
	}
	if f.memoryProofCfg.EmbeddingModel == "" {
		t.Error("proof cfg must carry the configured embedding model")
	}
}

// TestInstallMemoryProofFail: a FAIL proof verdict refuses (exit 1, rolled back) and
// surfaces the remediation detail (refuse-with-remediation, never a silent skip).
func TestInstallMemoryProofFail(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.memoryProofStatus = preflight.StatusFail
	f.memoryProofDetail = "the embeddings endpoint did not answer"

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Refused || res.Outcome.ExitCode() != exitBlocked {
		t.Fatalf("proof-fail outcome = %q, want %q (exit 1)", res.Outcome, Refused)
	}
	if !strings.Contains(errOut.String(), "the embeddings endpoint did not answer") {
		t.Errorf("a FAIL proof must surface the remediation detail, got %q", errOut.String())
	}
}

// TestInstallMemoryProofSkippedWhenOffOrDryRun: the proof is not invoked when memory
// is off or under --dry-run.
func TestInstallMemoryProofSkippedWhenOffOrDryRun(t *testing.T) {
	t.Run("gate off", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		f.run(Opts{})
		if f.memoryProofCalls != 0 {
			t.Errorf("gate off must not run the proof, calls = %d", f.memoryProofCalls)
		}
	})
	t.Run("dry-run", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.run(Opts{DryRun: true})
		if f.memoryProofCalls != 0 {
			t.Errorf("dry-run must not run the proof, calls = %d", f.memoryProofCalls)
		}
	})
}

// TestInstallMemoryGateRefusesUnfitHost is the CTRL-06 install half: an opted-in
// install whose memory host-fitness gate reports a confident shortage refuses-with-
// remediation BEFORE bringing up the memory stack — zero host mutation. With memory
// off the gate seam never fires.
func TestInstallMemoryGateRefusesUnfitHost(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	memDiskFail := preflight.CheckResult{
		ID: "MEM-PRE-disk", Name: "Vector-index disk space",
		Tier: preflight.TierBlock, Status: preflight.StatusFail,
		Detail:      `free disk 0.50 GiB at "/volroot" < required floor 1.00 GiB for the vector index`,
		Remediation: `Free up disk under "/volroot" — the Qdrant vector index lives there and grows with indexed chats/documents; or disable memory_enabled in config.toml.`,
	}

	t.Run("memory-on unfit host refuses before any mutation", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		gateCalls := 0
		f.RunMemoryChecks = func(detect.HostProfile, string) []preflight.CheckResult {
			gateCalls++
			return []preflight.CheckResult{memDiskFail}
		}

		res, _, _ := f.runResult(Opts{})
		if res.Outcome != Blocked {
			t.Fatalf("unfit memory host outcome = %q, want %q", res.Outcome, Blocked)
		}
		if gateCalls != 1 {
			t.Errorf("memory gate must run exactly once, ran %d times", gateCalls)
		}
		if f.writeCalls != 0 || f.startCalls != 0 || f.pullCalls != 0 || f.saveCalls != 0 {
			t.Errorf("a blocked memory install must not mutate: write=%d start=%d pull=%d save=%d",
				f.writeCalls, f.startCalls, f.pullCalls, f.saveCalls)
		}
		if f.embedEnsureCalls != 0 || f.memoryProofCalls != 0 {
			t.Errorf("memory stack must not come up after a gate refusal: embedEnsure=%d proof=%d",
				f.embedEnsureCalls, f.memoryProofCalls)
		}
	})

	t.Run("memory-off install never invokes the gate", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.RunMemoryChecks = func(detect.HostProfile, string) []preflight.CheckResult {
			t.Error("memory-off install must NOT run the memory host-fitness gate")
			return nil
		}
		if code, _, _ := f.run(Opts{}); code == exitBlocked {
			t.Fatalf("memory-off install must not be blocked by the memory gate, exit = %d", code)
		}
	})
}

// --- web search + coding agent ---------------------------------------------------

// TestInstallWebSearchFlag: `villa install --web-search` overrides the persisted
// gate, persists cfg.WebSearchEnabled=true, and fires the searxng bring-up path even
// when the persisted web_search_enabled is false. Without the flag (persisted off),
// the searxng path never fires.
func TestInstallWebSearchFlag(t *testing.T) {
	units, plan := webUnits()

	t.Run("--web-search overrides the persisted-off gate, persists it, and brings up searxng", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		code, _, errOut := f.run(Opts{WebSearch: true})
		if code != exitPass {
			t.Fatalf("clean --web-search install exit = %d, want %d; stderr:\n%s", code, exitPass, errOut.String())
		}
		if !f.savedCfg.WebSearchEnabled {
			t.Error("--web-search must persist cfg.WebSearchEnabled = true")
		}
		if f.searxngSettingsCalls != 1 || f.searxngProofCalls != 1 {
			t.Errorf("web-search seams must fire once: searxngSettings=%d searxngProof=%d, want 1/1",
				f.searxngSettingsCalls, f.searxngProofCalls)
		}
	})

	t.Run("no --web-search with persisted-off gate: searxng path never fires", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.webSearchEnabled = false

		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("clean install exit = %d, want %d", code, exitPass)
		}
		if f.savedCfg.WebSearchEnabled {
			t.Error("without --web-search and persisted off, cfg.WebSearchEnabled must stay false")
		}
		if f.searxngSettingsCalls != 0 || f.searxngProofCalls != 0 {
			t.Errorf("web-search seams must NOT fire when off: searxngSettings=%d searxngProof=%d, want 0/0",
				f.searxngSettingsCalls, f.searxngProofCalls)
		}
	})
}

// TestInstallCodingAgentFlow asserts the --coding-agent block: with the flag set
// the FSL notice prints, the coder GGUF + binary are staged and the config rendered
// BEFORE the readiness proof, AgentEnabled is persisted, the coder is SERVED through
// the CodingRender seam, and a clean install passes. An agent-off install fires NONE
// of the agent seams.
func TestInstallCodingAgentFlow(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "x"}}
	plan := orchestrate.Plan{Changed: units}

	t.Run("--coding-agent stages, persists, and proves the agent", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.coderPresent = false

		code, out, _ := f.run(Opts{CodingAgent: true})
		if code != exitPass {
			t.Fatalf("clean coding-agent install exit = %d, want %d", code, exitPass)
		}
		if !strings.Contains(out.String(), "FSL-1.1-MIT") {
			t.Errorf("install output must surface the FSL-1.1-MIT notice; got:\n%s", out.String())
		}
		if f.coderEnsureCalls != 1 || f.binaryInstallCalls != 1 || f.renderCrushCalls != 1 || f.agentProofCalls != 1 {
			t.Errorf("agent seam counts: coderEnsure=%d binaryInstall=%d render=%d proof=%d, want 1/1/1/1",
				f.coderEnsureCalls, f.binaryInstallCalls, f.renderCrushCalls, f.agentProofCalls)
		}
		if !f.savedCfg.AgentEnabled {
			t.Error("--coding-agent must persist cfg.AgentEnabled = true")
		}
		if !f.renderedAgentEnabled {
			t.Error("RenderCrushConfig must receive cfg.AgentEnabled = true")
		}
		if idx(f.callOrder, "renderCrushConfig") > idx(f.callOrder, "agentProof") {
			t.Errorf("config must be rendered before the readiness proof; callOrder = %v", f.callOrder)
		}
		if idx(f.callOrder, "installAgentBinary") < 0 || idx(f.callOrder, "ensureCoderModel") < 0 {
			t.Errorf("binary install + coder pre-stage must both run; callOrder = %v", f.callOrder)
		}
	})

	t.Run("--coding-agent serves the coder: RenderInput.CodingMode != nil + served id == rec.Coder.Model", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		// The catalog→inference translation is the CodingRender seam; the flow must
		// thread its descriptor and -m file into the render input verbatim.
		f.CodingRender = func(config.VillaConfig) (string, *inference.CodingModeSpec, error) {
			return "qwen3-coder-30b-a3b.gguf", &inference.CodingModeSpec{}, nil
		}

		code, _, errOut := f.run(Opts{CodingAgent: true})
		if code != exitPass {
			t.Fatalf("coding-agent install exit = %d, want %d; stderr:\n%s", code, exitPass, errOut.String())
		}
		if !f.renderedInputSet {
			t.Fatal("render seam was never called")
		}
		if f.renderedInput.CodingMode == nil {
			t.Error("--coding-agent must thread a non-nil RenderInput.CodingMode (the coder is served)")
		}
		if f.renderedInput.ModelFile != "qwen3-coder-30b-a3b.gguf" {
			t.Errorf("RenderInput.ModelFile = %q, want the coder's file from CodingRender", f.renderedInput.ModelFile)
		}
		if f.renderedInput.Cfg.CoderModel != "qwen3-coder-30b-a3b" {
			t.Errorf("RenderInput.Cfg.CoderModel = %q, want rec.Coder.Model %q", f.renderedInput.Cfg.CoderModel, "qwen3-coder-30b-a3b")
		}
		if !f.renderedInput.Cfg.CodingMode {
			t.Error("RenderInput.Cfg.CodingMode must be true on --coding-agent")
		}
		if f.renderedInput.CoderAgentCtx != 65536 {
			t.Errorf("RenderInput.CoderAgentCtx = %d, want rec.Coder.AgentCtx %d", f.renderedInput.CoderAgentCtx, 65536)
		}
		if f.savedCfg.CoderModel != "qwen3-coder-30b-a3b" || !f.savedCfg.CodingMode ||
			f.savedCfg.CoderQuant != "Q4_K_M" || f.savedCfg.CoderAgentCtx != 65536 {
			t.Errorf("persisted cfg must carry coder fields: CoderModel=%q CodingMode=%v CoderQuant=%q CoderAgentCtx=%d",
				f.savedCfg.CoderModel, f.savedCfg.CodingMode, f.savedCfg.CoderQuant, f.savedCfg.CoderAgentCtx)
		}
	})

	t.Run("a coder render failure blocks before any mutation", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.CodingRender = func(config.VillaConfig) (string, *inference.CodingModeSpec, error) {
			return "", nil, errors.New("resolve coder model file: model \"x\" is not in the catalog")
		}

		res, _, errOut := f.runResult(Opts{CodingAgent: true})
		if res.Outcome != Blocked {
			t.Fatalf("outcome = %q, want %q", res.Outcome, Blocked)
		}
		if !strings.Contains(errOut.String(), "install: resolve coder model file:") {
			t.Errorf("the block must carry the seam's error, got %q", errOut.String())
		}
		if f.saveCalls != 0 || f.writeCalls != 0 || f.startCalls != 0 {
			t.Errorf("a render block must not mutate: save=%d write=%d start=%d", f.saveCalls, f.writeCalls, f.startCalls)
		}
	})

	t.Run("chat-only install is off-path byte-identical: RenderInput.CodingMode == nil + CoderModel empty", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("chat-only install exit = %d, want %d", code, exitPass)
		}
		if !f.renderedInputSet {
			t.Fatal("render seam was never called")
		}
		if f.renderedInput.CodingMode != nil {
			t.Error("chat-only install must keep RenderInput.CodingMode == nil (off-path byte-identical)")
		}
		if f.renderedInput.Cfg.CoderModel != "" || f.renderedInput.Cfg.CodingMode {
			t.Errorf("chat-only install must not set coder fields: CoderModel=%q CodingMode=%v",
				f.renderedInput.Cfg.CoderModel, f.renderedInput.Cfg.CodingMode)
		}
	})

	t.Run("readiness FAIL refuses-with-remediation, no false-green", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.agentProofStatus = preflight.StatusFail
		f.agentProofDetail = "the coding agent ran but did not perform the tool-call edit"

		res, _, errOut := f.runResult(Opts{CodingAgent: true})
		if res.Outcome != Refused {
			t.Fatalf("an agent readiness FAIL must refuse, outcome = %q want %q", res.Outcome, Refused)
		}
		if !strings.Contains(errOut.String(), "coding agent not ready") {
			t.Errorf("a readiness FAIL must refuse-with-remediation; got:\n%s", errOut.String())
		}
	})

	t.Run("shared-residency coder fit refuses with a swap-only message, NOT free-memory copy", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.agentCat = catalog.Catalog{Models: []catalog.Model{{ID: "qwen3-chat"}}}
		f.Pick = func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{
				Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "rocm",
				WeightBytes: 1 << 30, KVCacheBytes: 1 << 28, HeadroomBytes: 1 << 28,
				UsableEnvelopeBytes: 8 << 30, Fits: true,
				Coder: recommend.CoderFit{Fits: false, Residency: recommend.ResidencyShared},
			}
		}

		res, _, errOut := f.runResult(Opts{CodingAgent: true})
		if res.Outcome != Blocked {
			t.Fatalf("a shared-residency coder fit must block the addon, outcome = %q want %q", res.Outcome, Blocked)
		}
		if f.binaryInstallCalls != 0 || f.agentProofCalls != 0 {
			t.Errorf("no agent staging/proof after a coder-fit refusal: binaryInstall=%d proof=%d",
				f.binaryInstallCalls, f.agentProofCalls)
		}
		got := errOut.String()
		if !strings.Contains(got, "swap-residency coder fit") || !strings.Contains(got, "SHARED residency") {
			t.Errorf("a shared-residency refusal must explain the swap-only limitation; got:\n%s", got)
		}
		if strings.Contains(got, "free memory or use a larger-envelope host") {
			t.Errorf("a shared-residency refusal must NOT use the misleading no-fit free-memory copy; got:\n%s", got)
		}
	})

	t.Run("agent-off install fires NO agent seam (D-01 byte-identical)", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		code, out, _ := f.run(Opts{})
		if code != exitPass {
			t.Fatalf("agent-off install exit = %d, want %d", code, exitPass)
		}
		if f.coderPresentCalls != 0 || f.coderEnsureCalls != 0 || f.binaryInstallCalls != 0 ||
			f.renderCrushCalls != 0 || f.agentProofCalls != 0 {
			t.Errorf("agent-off install must fire NO agent seam: present=%d ensure=%d binary=%d render=%d proof=%d",
				f.coderPresentCalls, f.coderEnsureCalls, f.binaryInstallCalls, f.renderCrushCalls, f.agentProofCalls)
		}
		if strings.Contains(out.String(), "FSL-1.1-MIT") || strings.Contains(out.String(), "coding agent") {
			t.Errorf("agent-off install must not surface any coding-agent output; got:\n%s", out.String())
		}
		if f.savedCfg.AgentEnabled {
			t.Error("agent-off install must persist AgentEnabled = false")
		}
	})
}

// TestInstallAgentPreflightFold asserts the preflight fold: RunAgentChecks is
// appended to the gate ONLY when the addon is enabled, an agent-off install never
// calls it, and an agent BLOCK from the fold refuses through the SAME gate.
func TestInstallAgentPreflightFold(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "x"}}
	plan := orchestrate.Plan{Changed: units}

	t.Run("--coding-agent folds the agent checks exactly once", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		if code, _, _ := f.run(Opts{CodingAgent: true}); code != exitPass {
			t.Fatalf("clean agent install exit = %d, want %d", code, exitPass)
		}
		if f.agentChecksCalls != 1 {
			t.Errorf("RunAgentChecks must fire exactly once when the addon is on; got %d", f.agentChecksCalls)
		}
	})

	t.Run("agent-off install never folds the agent checks (byte-identical)", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		if code, _, _ := f.run(Opts{}); code != exitPass {
			t.Fatalf("agent-off install exit = %d, want %d", code, exitPass)
		}
		if f.agentChecksCalls != 0 {
			t.Errorf("agent-off install must NOT fold the agent checks; got %d", f.agentChecksCalls)
		}
	})

	t.Run("an agent BLOCK from the fold refuses the install", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.agentChecks = []preflight.CheckResult{{
			ID: "AGENT-PRE-envelope", Name: "Coding-agent memory envelope",
			Tier: preflight.TierBlock, Status: preflight.StatusFail,
			Detail: "no coder model fits", Remediation: "free memory",
		}}
		res, _, errOut := f.runResult(Opts{CodingAgent: true})
		if res.Outcome != Blocked {
			t.Fatalf("an agent BLOCK must refuse the install, outcome = %q want %q", res.Outcome, Blocked)
		}
		if f.binaryInstallCalls != 0 || f.agentProofCalls != 0 {
			t.Errorf("a blocked gate must not stage/prove the agent: binary=%d proof=%d",
				f.binaryInstallCalls, f.agentProofCalls)
		}
		if !strings.Contains(errOut.String(), "BLOCKED") {
			t.Errorf("an agent BLOCK must refuse-with-remediation via the gate; got:\n%s", errOut.String())
		}
	})
}

// --- refusal + rollback ----------------------------------------------------------

// TestInstallRefusesUnreadableConfig locks the fail-closed config-load seam: when
// config.toml cannot be READ, install refuses with remediation instead of quietly
// installing from seed defaults. An ABSENT config is NOT a load error, so this only
// fires on a present-but-unreadable config — exactly the case where defaulting would
// discard the user's persisted settings. Nothing may be rendered, written, started
// or persisted from input install failed to read.
func TestInstallRefusesUnreadableConfig(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeDeps(t, units, plan, passChecks())
	f.configLoadErr = errors.New(`config: parse "/home/u/.config/villa/config.toml": bare keys cannot contain '"'`)

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Blocked || res.Outcome.ExitCode() != exitBlocked {
		t.Fatalf("unreadable config: outcome = %q, want %q (exit 1)", res.Outcome, Blocked)
	}
	if f.writeCalls != 0 || f.saveCalls != 0 || f.startCalls != 0 || f.reloadCalls != 0 {
		t.Errorf("a refused install must not mutate the host: write=%d save=%d start=%d reload=%d",
			f.writeCalls, f.saveCalls, f.startCalls, f.reloadCalls)
	}
	if f.renderedInputSet {
		t.Error("nothing may be rendered from a config install failed to read")
	}
	msg := errOut.String()
	if !strings.Contains(msg, "cannot read the persisted config") {
		t.Errorf("refusal must name the cause, got %q", msg)
	}
	if !strings.Contains(msg, "config.toml") {
		t.Errorf("refusal must carry actionable remediation naming config.toml, got %q", msg)
	}
}

// TestInstallRollsBackOnAFailedProof is the behaviour change of ADR-0003, asserted
// where it matters: on the host, not on the return value. A stack that started four
// services and then failed its readiness proof is running-but-unproven, which is
// indistinguishable from a healthy one to every ordinary check.
func TestInstallRollsBackOnAFailedProof(t *testing.T) {
	units, plan := memoryUnits()

	t.Run("a failed memory proof stops what it started", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.memoryProofStatus = preflight.StatusFail
		f.memoryProofDetail = "embeddings returned 0 dimensions"
		f.activeState = "inactive"
		f.priorConfigExists = false

		res, _, errOut := f.runResult(Opts{})
		if res.Outcome != Refused {
			t.Fatalf("a failed proof must refuse, outcome = %q", res.Outcome)
		}
		if len(f.stopOrder) == 0 {
			t.Error("a failed proof must stop the services install started — a running-but-unproven stack is the false-green this prevents")
		}
		for _, started := range f.startOrder {
			if !contains(f.stopOrder, started) {
				t.Errorf("service %q was started and left running after a failed proof; stops = %v", started, f.stopOrder)
			}
		}
		if !strings.Contains(errOut.String(), "embeddings returned 0 dimensions") {
			t.Errorf("the refusal must carry the proof's detail, got %q", errOut.String())
		}
		if res.RollbackReason == "" {
			t.Error("a refusal after mutation must carry the rollback's reason")
		}
	})

	t.Run("a clean host is restored to nothing", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.memoryProofStatus = preflight.StatusFail
		f.activeState = "inactive"
		f.priorConfigExists = false
		f.priorUnits = map[string]string{}

		f.run(Opts{})
		if len(f.removedUnits) == 0 {
			t.Error("a failed first install must remove the units it wrote (ADR-0003)")
		}
		if !f.configRemoved {
			t.Error("a failed first install must remove the config it wrote — leaving one would make a later `villa up` try to bring up units that are gone")
		}
	})

	t.Run("a re-install restores the prior stack instead of removing it", func(t *testing.T) {
		f := newFakeDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.memoryProofStatus = preflight.StatusFail
		f.activeState = "active"
		f.priorConfigExists = true
		f.priorUnits = map[string]string{}
		for _, u := range units {
			f.priorUnits[u.Name] = "[Container]\nImage=prior\n"
		}

		f.run(Opts{})
		if contains(f.removedUnits, "villa-llama.container") {
			t.Error("a unit that existed before must be RESTORED, never removed — a failed upgrade must leave the working install")
		}
		// The restore goes through the WriteUnit seam with the captured prior text.
		if !contains(f.callOrder, "writeUnit:villa-llama.container") {
			t.Errorf("a prior unit must be restored through WriteUnit; callOrder = %v", f.callOrder)
		}
		if f.configRemoved {
			t.Error("a re-install must never delete the operator's config")
		}
		if len(f.stopOrder) > 0 {
			t.Errorf("services running before the install must not be left stopped; stops = %v", f.stopOrder)
		}
	})
}

// TestInstallReportsAnIncompleteRollbackHonestly: a rollback step that itself
// failed must never be presented as a clean restoration — a wrong "restored" claim
// tells the operator to stop looking.
func TestInstallReportsAnIncompleteRollbackHonestly(t *testing.T) {
	units, plan := memoryUnits()

	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.memoryProofStatus = preflight.StatusFail
	f.activeState = "inactive"
	f.priorConfigExists = false
	f.stopErr = errors.New("unit is masked")
	f.removeUnitErr = errors.New("read-only filesystem")

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Refused {
		t.Fatalf("outcome = %q, want %q", res.Outcome, Refused)
	}
	for _, want := range []string{"did not fully complete", "indeterminate state", "villa-llama"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr must carry %q (an incomplete rollback names what it could not undo), got %q", want, errOut.String())
		}
		if !strings.Contains(res.RollbackReason, want) {
			t.Errorf("Result.RollbackReason must carry %q, got %q", want, res.RollbackReason)
		}
	}
}

// --- the interface invariants (ADR-0005 acceptance) -------------------------------

// TestRunDryRunNeverMutates: --dry-run with a changed plan reaches DryRun having
// called NO mutating seam — not a pull, a config save, a unit write, a reload, a
// start, a privileged fix, or a dashboard write.
func TestRunDryRunNeverMutates(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, []preflight.CheckResult{lingeroffCheck()})
	f.memoryEnabled = true
	f.downloaded = false
	f.embedPresent = false

	res, _, _ := f.runResult(Opts{DryRun: true})
	if res.Outcome != DryRun {
		t.Fatalf("outcome = %q, want %q", res.Outcome, DryRun)
	}
	if f.pullCalls != 0 || f.embedEnsureCalls != 0 || f.saveCalls != 0 || f.writeCalls != 0 || f.reloadCalls != 0 ||
		f.startCalls != 0 || f.seboolCalls != 0 || f.lingerCalls != 0 || f.dashWriteCalls != 0 {
		t.Errorf("--dry-run mutated: pull=%d embed=%d save=%d write=%d reload=%d start=%d sebool=%d linger=%d dash=%d",
			f.pullCalls, f.embedEnsureCalls, f.saveCalls, f.writeCalls, f.reloadCalls, f.startCalls, f.seboolCalls, f.lingerCalls, f.dashWriteCalls)
	}
	if len(f.callOrder) != 0 {
		t.Errorf("--dry-run recorded host effects: %v", f.callOrder)
	}
}

// TestRunGatesResolvedOnce: the host-prep, memory and agent gates are each run
// EXACTLY once on a memory+agent-on install — a gate answered twice could answer
// differently, and a privileged fix could run twice.
func TestRunGatesResolvedOnce(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.agentEnabled = true
	checksCalls, memCalls := 0, 0
	f.RunChecks = func(detect.HostProfile, preflight.ResourceReq) []preflight.CheckResult {
		checksCalls++
		return passChecks()
	}
	f.RunMemoryChecks = func(detect.HostProfile, string) []preflight.CheckResult {
		memCalls++
		return nil
	}

	res, _, errOut := f.runResult(Opts{})
	if res.Outcome != Completed {
		t.Fatalf("outcome = %q, want %q; stderr:\n%s", res.Outcome, Completed, errOut.String())
	}
	if checksCalls != 1 || memCalls != 1 || f.agentChecksCalls != 1 {
		t.Errorf("gates must each resolve once: checks=%d memory=%d agent=%d", checksCalls, memCalls, f.agentChecksCalls)
	}
}
