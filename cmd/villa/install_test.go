package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// fakeInstallDeps builds an installDeps wired entirely to stubs so runInstall is
// exercisable without a live GPU, podman, systemd, SELinux, or network host.
// Counters let each test assert exactly which host-touching seams fired
// (idempotency / consent / model-pull / config-persist).
type fakeInstallDeps struct {
	*installDeps
	writeCalls  int
	reloadCalls int
	startCalls  int
	lingerCalls int
	seboolCalls int
	pollCalls   int
	pullCalls   int
	saveCalls   int
	// wizardCalls counts invocations of the guided-wizard seam (Plan 03). The
	// dedicated wizard tests assert it is exactly 1 on a TTY (no --json/--no-tui)
	// and exactly 0 on every bypass path (--no-tui / --json / non-TTY). Defaulted
	// to a no-op stub in newFakeInstallDeps so existing flag-path tests (which keep
	// stdoutIsTTY=false) never enter the wizard branch and stay deterministic.
	wizardCalls int
	// startOrder records the service names passed to the start seam in invocation
	// order (D-05: inference strictly before owui). callOrder records cross-seam
	// ordering (e.g. ensureModel before the first start, MODEL-04).
	startOrder []string
	callOrder  []string
	// downloaded controls the modelDownloaded seam (default true = present, so
	// existing tests never trip the pull path). Set false to exercise auto-pull.
	downloaded bool
	// savedCfg captures the last config persisted via the saveConfig seam so tests
	// can assert install wrote the recommended model/quant/ctx/backend (F-2).
	savedCfg config.VillaConfig

	// Dashboard-service (Plan 05-05) seam counters: install renders+writes+enables+
	// starts the native villa-dashboard.service AFTER the container services.
	dashWriteCalls  int
	dashEnableCalls int
	dashEnabled     []string
	// dashBinaryPath captures the binary path install resolved and threaded into the
	// writeDashboardUnit seam (UAT Test 5 fix): a test asserts it is absolute and
	// points at an existing, executable file so the unit's ExecStart survives a reboot.
	dashBinaryPath string
	// diskUnit is the bytes the readDashboardUnit seam reports as the current on-disk
	// dashboard unit (05-08 idempotency compare). nil = unit absent (os.ErrNotExist →
	// treated as a diff → must write). Defaulted to nil in newFakeInstallDeps so the
	// rendered bytes always differ from disk and every existing dashboard test keeps
	// seeing a "differs → reconcile" outcome (dashWriteCalls==1 / a dashboard start).
	diskUnit []byte

	// Memory-stack (Phase-19) seam controls + counters. memoryEnabled drives the
	// loadedMemoryEnabled gate seam (default false → the memory path never fires, so
	// existing v1.2 install tests stay byte-for-byte unchanged). embedPresent drives the
	// embedModelPresent idempotency seam (default true → present, so the pre-stage pull
	// is skipped unless a test sets it false). The *Calls counters + memoryProofInput
	// capture let the memory tests assert exactly which memory seams fired.
	// persistedConfig, when non-nil, is returned by the loadedConfig seam so a test can
	// prove runInstall SEEDS cfg from the user's persisted config and preserves their
	// memory/dashboard/chat customizations through saveConfig (WR-02). nil → the seam
	// returns config.DefaultVillaConfig() (the old seed behavior, unchanged).
	persistedConfig   *config.VillaConfig
	memoryEnabled     bool
	embedPresent      bool
	embedEnsureCalls  int
	embedPresentCalls int
	memoryProofCalls  int
	memoryProofIn     memoryProofInput
	memoryProofStatus preflight.Status
	memoryProofDetail string

	// Web-search (SearXNG) seam controls + counters (v1.5 / Phase-29). webSearchEnabled
	// drives the loadedWebSearchEnabled gate seam (default false → the searxng path never
	// fires, so existing tests stay byte-for-byte unchanged). The *Calls counters +
	// searxngProofIn capture let the web-search tests assert exactly which seams fired
	// (settings + secret-env writes BEFORE the start, the proof input, the verdict folding).
	webSearchEnabled      bool
	searxngSettingsCalls  int
	searxngSecretEnvCalls int
	searxngProofCalls     int
	searxngProofIn        searxngProofInput
	searxngProofStatus    preflight.Status
	searxngProofDetail    string

	// Coding-agent (Crush) addon seam controls + counters (v1.4 / INSTALL-03). agentEnabled
	// drives the loadedAgentEnabled gate seam (default false → the agent path never fires,
	// so existing install tests stay unchanged). agentCat is the catalog the coderShardFor
	// resolution reads (default carries a single coder entry so the agent-on tests resolve a
	// shard). coderPresent drives the coderModelPresent idempotency seam (default true →
	// present, so the pre-stage pull is skipped unless a test sets it false). agentProofStatus
	// drives the readiness verdict. The *Calls counters let the agent tests assert exactly
	// which agent seams fired.
	agentEnabled         bool
	agentCat             catalog.Catalog
	agentCatOK           bool
	coderPresent         bool
	coderEnsureCalls     int
	coderPresentCalls    int
	binaryInstallCalls   int
	renderCrushCalls     int
	agentProofCalls      int
	agentProofStatus     preflight.Status
	agentProofDetail     string
	renderedAgentEnabled bool
	// renderedInput captures the orchestrate.RenderInput the render seam received so a
	// test can assert the coder-serving wiring (CR-01): on --coding-agent the captured
	// CodingMode must be non-nil and Cfg.CoderModel/CoderAgentCtx must equal rec.Coder,
	// while a chat-only install must capture CodingMode == nil + CoderModel == "" (off-path
	// byte-identical). renderedInputSet guards the zero-value (no render call yet).
	renderedInput    orchestrate.RenderInput
	renderedInputSet bool
	// agentChecksCalls counts invocations of the runAgentChecks preflight-fold seam
	// (INSTALL-04/D-09): a test asserts it is exactly 1 on an agent-on install and 0 on
	// an agent-off install (the agent-off gate is byte-identical). agentChecks is the
	// canned []CheckResult the seam returns (default empty → the gate stays a pass).
	agentChecksCalls int
	agentChecks      []preflight.CheckResult
}

func newFakeInstallDeps(t *testing.T, units []orchestrate.Unit, plan orchestrate.Plan, checks []preflight.CheckResult) *fakeInstallDeps {
	t.Helper()
	f := &fakeInstallDeps{
		downloaded: true, embedPresent: true, memoryProofStatus: preflight.StatusPass,
		searxngProofStatus: preflight.StatusPass,
		coderPresent:       true, agentProofStatus: preflight.StatusPass,
		// Default agent catalog carries a single coder entry whose id matches the default
		// pick's Coder.Model (set below) and a shard, so an agent-on test resolves a shard
		// without extra setup. agentCatOK true so the load seam succeeds by default.
		agentCatOK: true,
		// Use a REAL embedded-catalog coder id: codingModelFile/codingDescriptor (the
		// Phase-25 coder-serving helpers reused on the --coding-agent render path) read the
		// embedded catalog via modelCatalogPath, NOT this fake, so the served coder id must
		// be a genuine catalog entry for the default agent-on flow to render.
		agentCat: catalog.Catalog{Models: []catalog.CatalogModel{
			{ID: "qwen3-coder-30b-a3b", Role: "coder", Shards: []catalog.Shard{
				{Filename: "qwen3-coder-30b-a3b.gguf", SizeBytes: 4096},
			}},
		}},
	}
	d := &installDeps{
		probe: func() detect.HostProfile { return detect.HostProfile{} },
		pick: func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{
				Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan",
				WeightBytes:  1 << 30,
				KVCacheBytes: 1 << 28, HeadroomBytes: 1 << 28, UsableEnvelopeBytes: 8 << 30,
				Fits: true,
				// A fitting coder block so the agent-on tests resolve a shard from the
				// default agent catalog (id matches agentCat's coder entry).
				Coder: recommend.CoderFit{Model: "qwen3-coder-30b-a3b", Quant: "Q4_K_M", AgentCtx: 65536, Fits: true, Residency: "swap"},
			}
		},
		modelFile: func(recommend.Recommendation) (string, error) { return "qwen2.5-0.5b.gguf", nil },
		modelsDir: func() string { return t.TempDir() },
		runChecks: func(detect.HostProfile, preflight.ResourceReq) []preflight.CheckResult { return checks },
		render: func(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
			f.renderedInput = in
			f.renderedInputSet = true
			return units, nil
		},
		reconcile:   func([]orchestrate.Unit, string) (orchestrate.Plan, error) { return plan, nil },
		unitDir:     func() (string, error) { return t.TempDir(), nil },
		username:    func() string { return "tester" },
		endpoint:    func() string { return "http://127.0.0.1:8080" },
		interactive: func() bool { return false },
		consent:     func(string) bool { return false },
		// Default the wizard seams to the flag path: stdoutIsTTY=false forces the
		// useWizard gate off so existing flag-path tests (which only set interactive)
		// never enter the wizard branch. The dedicated wizard tests (Plan 03) override
		// these. wizard is a no-op canned result, never reached while stdoutIsTTY=false.
		stdoutIsTTY: func() bool { return false },
		wizard: func(context.Context, wizardInput) (wizardResult, error) {
			f.wizardCalls++
			return wizardResult{}, nil
		},
	}
	d.modelDownloaded = func(recommend.Recommendation) bool { return f.downloaded }
	d.ensureModel = func(recommend.Recommendation) error {
		f.pullCalls++
		f.callOrder = append(f.callOrder, "ensureModel")
		return nil
	}
	d.saveConfig = func(c config.VillaConfig) error { f.saveCalls++; f.savedCfg = c; return nil }
	d.writeUnits = func(orchestrate.Plan, string) error { f.writeCalls++; return nil }
	d.daemonReload = func() error { f.reloadCalls++; return nil }
	d.start = func(service string) error {
		f.startCalls++
		f.startOrder = append(f.startOrder, service)
		f.callOrder = append(f.callOrder, "start:"+service)
		return nil
	}
	d.isActive = func(string) (string, error) { return "active", nil }
	d.enableLinger = func(string) error { f.lingerCalls++; return nil }
	d.setsebool = func() error { f.seboolCalls++; return nil }
	// Dashboard-service seams (Plan 05-05): render/write the native unit, then
	// enable + start it. The write+enable record an ordered event so a test can
	// assert the dashboard comes up AFTER the container services.
	d.userUnitDir = func() (string, error) { return t.TempDir(), nil }
	d.writeDashboardUnit = func(_ string, binaryPath string) error {
		f.dashWriteCalls++
		f.dashBinaryPath = binaryPath
		f.callOrder = append(f.callOrder, "dashWrite")
		return nil
	}
	// Default the binary-path resolver to a fixed absolute path so existing tests that
	// only assert ordering/counts do not touch the host. Tests exercising the real
	// os.Executable resolution override this with resolveDashboardBinaryPath.
	d.resolveBinaryPath = func() (string, error) { return "/opt/villa/bin/villa", nil }
	d.enable = func(service string) error {
		f.dashEnableCalls++
		f.dashEnabled = append(f.dashEnabled, service)
		f.callOrder = append(f.callOrder, "enable:"+service)
		return nil
	}
	// readDashboardUnit reports the controllable diskUnit (nil → os.ErrNotExist, the
	// absent-unit first-install state). Defaulting diskUnit to nil makes the rendered
	// bytes always differ from disk, so the dashboard is reconciled (matching today's
	// behavior). Tests that want a true no-op pre-seed diskUnit with the rendered bytes.
	d.readDashboardUnit = func(string) ([]byte, error) {
		if f.diskUnit == nil {
			return nil, os.ErrNotExist
		}
		return f.diskUnit, nil
	}
	d.pollReady = func(context.Context, string) installReadiness {
		f.pollCalls++
		return installReadiness{status: preflight.StatusPass, detail: "ready"}
	}
	// Memory-stack seams (Phase-19). The gate seam reflects the controllable
	// memoryEnabled flag (default false → the memory path never fires, so existing
	// v1.2 tests are unchanged). The pre-stage/presence seams record an ordered event
	// so a test can assert the embed GGUF is staged BEFORE the embed service starts and
	// the Qdrant/embed start ordering (Pitfall 4). The proof seam returns the controllable
	// verdict and captures its input for assertion.
	// loadedConfig seeds runInstall's cfg from the persisted config (WR-02). Default to
	// the typed defaults (byte-for-byte the old DefaultVillaConfig() seed) so existing
	// tests are unchanged; persistedConfig lets a test inject a customized on-disk config
	// to prove install preserves it.
	d.loadedConfig = func() config.VillaConfig {
		if f.persistedConfig != nil {
			return *f.persistedConfig
		}
		return config.DefaultVillaConfig()
	}
	d.loadedMemoryEnabled = func() bool { return f.memoryEnabled }
	d.embedModelPresent = func(string) bool {
		f.embedPresentCalls++
		return f.embedPresent
	}
	d.ensureEmbedModel = func(string) error {
		f.embedEnsureCalls++
		f.callOrder = append(f.callOrder, "ensureEmbedModel")
		return nil
	}
	d.memoryProofFn = func(_ context.Context, in memoryProofInput) memoryProof {
		f.memoryProofCalls++
		f.memoryProofIn = in
		f.callOrder = append(f.callOrder, "memoryProof")
		return memoryProof{status: f.memoryProofStatus, detail: f.memoryProofDetail}
	}
	// Web-search (SearXNG) seams (v1.5 / Phase-29). The gate seam reflects the controllable
	// webSearchEnabled flag (default false → the searxng path never fires, so existing tests
	// are unchanged). The settings + secret-env writers record an ordered event so a test can
	// assert BOTH config files are written BEFORE the searxng start (Pitfall 3). The proof
	// seam returns the controllable verdict and captures its input for assertion.
	d.loadedWebSearchEnabled = func() bool { return f.webSearchEnabled }
	d.writeSearxngSettings = func(string, string) error {
		f.searxngSettingsCalls++
		f.callOrder = append(f.callOrder, "writeSearxngSettings")
		return nil
	}
	d.writeSearxngSecretEnv = func(string, string) error {
		f.searxngSecretEnvCalls++
		f.callOrder = append(f.callOrder, "writeSearxngSecretEnv")
		return nil
	}
	d.searxngProofFn = func(_ context.Context, in searxngProofInput) searxngProof {
		f.searxngProofCalls++
		f.searxngProofIn = in
		f.callOrder = append(f.callOrder, "searxngProof")
		return searxngProof{status: f.searxngProofStatus, detail: f.searxngProofDetail}
	}
	// Coding-agent (Crush) addon seams (v1.4 / INSTALL-03). The gate seam reflects the
	// controllable agentEnabled flag (default false → the agent path never fires, so existing
	// tests are unchanged). The pre-stage/install/render/proof seams record an ordered event
	// so a test can assert the binary + GGUF are staged and the config rendered BEFORE the
	// readiness proof, and the proof seam returns the controllable verdict.
	d.loadedAgentEnabled = func() bool { return f.agentEnabled }
	d.agentCatalog = func() (catalog.Catalog, bool) { return f.agentCat, f.agentCatOK }
	d.coderModelPresent = func(string, catalog.Shard) bool {
		f.coderPresentCalls++
		return f.coderPresent
	}
	d.ensureCoderModel = func(string, catalog.Shard) error {
		f.coderEnsureCalls++
		f.callOrder = append(f.callOrder, "ensureCoderModel")
		return nil
	}
	d.installAgentBinary = func(context.Context) (string, error) {
		f.binaryInstallCalls++
		f.callOrder = append(f.callOrder, "installAgentBinary")
		return "/tmp/villa/bin/crush", nil
	}
	d.renderCrushConfig = func(cfg config.VillaConfig) error {
		f.renderCrushCalls++
		f.renderedAgentEnabled = cfg.AgentEnabled
		f.callOrder = append(f.callOrder, "renderCrushConfig")
		return nil
	}
	d.agentProofFn = func(context.Context) agentProof {
		f.agentProofCalls++
		f.callOrder = append(f.callOrder, "agentProof")
		return agentProof{status: f.agentProofStatus, detail: f.agentProofDetail}
	}
	d.runAgentChecks = func(detect.HostProfile, recommend.Recommendation) []preflight.CheckResult {
		f.agentChecksCalls++
		f.callOrder = append(f.callOrder, "runAgentChecks")
		return f.agentChecks
	}
	f.installDeps = d
	return f
}

func installTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "install"}
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

func passChecks() []preflight.CheckResult {
	return []preflight.CheckResult{
		{ID: "PRE-01", Tier: preflight.TierBlock, Status: preflight.StatusPass},
		{ID: "PRE-05", Tier: preflight.TierBlock, Status: preflight.StatusPass},
	}
}

// mustRenderDashboardUnit renders the native dashboard unit body for binPath via the
// SAME pure renderer reconcileDashboardUnit uses for its idempotency compare, so a test
// can pre-seed the fake's on-disk unit (diskUnit) with bytes that EXACTLY match what
// install would write — proving the true-no-op path (matching unit → zero reconcile).
func mustRenderDashboardUnit(t *testing.T, binPath string) []byte {
	t.Helper()
	body, err := orchestrate.RenderDashboardUnit(binPath)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return []byte(body)
}

// TestInstallDryRunWritesNothing: --dry-run prints the rendered units and calls
// WriteUnits zero times, exiting 0 (ORCH success-criterion 1). It must also pull
// nothing and persist no config (a dry run has zero side effects, F-1/F-2).
func TestInstallDryRunWritesNothing(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\nImage=x\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.downloaded = false // even with a model absent, --dry-run must NOT pull

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{dryRun: true}, f.installDeps)
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
	f := newFakeInstallDeps(t, units, plan, passChecks())
	// Pre-seed the on-disk dashboard unit so it ALREADY matches the rendered bytes
	// (rendered from the SAME path the fake's resolveBinaryPath returns, "/opt/villa/bin/villa").
	// Then the dashboard reconcile is also a true no-op, so the reload==0 assertion below
	// holds: this now proves the FULL true-no-op — containers unchanged AND dashboard unit
	// current → zero writes, zero reloads, zero dashboard restarts (05-08).
	f.diskUnit = mustRenderDashboardUnit(t, "/opt/villa/bin/villa")

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("no-op exit = %d, want 0", code)
	}
	if f.writeCalls != 0 || f.reloadCalls != 0 {
		t.Errorf("no-op must not write/reload: write=%d reload=%d", f.writeCalls, f.reloadCalls)
	}
	if f.pullCalls != 0 {
		t.Errorf("no-op with a present model must not pull, pulled %d times", f.pullCalls)
	}
	// A matching on-disk dashboard unit is a true no-op too: zero dashboard writes,
	// zero enables, and no dashboard start.
	if f.dashWriteCalls != 0 {
		t.Errorf("true-no-op must not write the dashboard unit, wrote %d times", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 0 {
		t.Errorf("true-no-op must not enable the dashboard unit, enabled %d times", f.dashEnableCalls)
	}
	for _, svc := range f.startOrder {
		if svc == orchestrate.DashboardServiceName {
			t.Errorf("true-no-op must not (re)start the dashboard service, startOrder = %v", f.startOrder)
		}
	}
	if !strings.Contains(strings.ToLower(out.String()), "no changes") {
		t.Errorf("no-op should report no changes, got %q", out.String())
	}
}

// TestInstallAutoPullsAbsentModelThenStarts: on a model-absent host install pulls
// the recommended weights (ensureModel fires exactly once) BEFORE writing units
// and starting the service — the F-1 fix. With a present model it pulls zero times.
func TestInstallAutoPullsAbsentModelThenStarts(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// Model absent → install must pull, then write + start.
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.downloaded = false

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
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

	// Model present → install must NOT pull.
	f2 := newFakeInstallDeps(t, units, plan, passChecks())
	f2.downloaded = true
	cmd2, _, _ := installTestCmd()
	if code := runInstall(cmd2, installOpts{}, f2.installDeps); code != exitPass {
		t.Fatalf("present-model install exit = %d, want 0", code)
	}
	if f2.pullCalls != 0 {
		t.Errorf("a present model must not be re-pulled, pulled %d times", f2.pullCalls)
	}
}

// TestInstallStartsInferenceBeforeOpenWebUI: on the write path install starts
// villa-llama.service strictly BEFORE villa-openwebui.service (D-05 ordering) —
// Open WebUI must come up after inference so it can reach a live backend.
func TestInstallStartsInferenceBeforeOpenWebUI(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())

	cmd, _, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	// inference must start strictly before owui (D-05). Since 05-08 the dashboard is
	// reconciled BEFORE the container starts, so we make NO assertion about its position
	// here — only the inference→owui relative order on the (now 3-service) start list.
	llamaI, owuiI := -1, -1
	for i, svc := range f.startOrder {
		switch svc {
		case "villa-llama.service":
			llamaI = i
		case "villa-openwebui.service":
			owuiI = i
		}
	}
	if llamaI < 0 || owuiI < 0 || llamaI >= owuiI {
		t.Fatalf("start-call order = %v, want inference before owui (D-05)", f.startOrder)
	}
}

// TestInstallReconcilesDashboardForBootSurvival (Plan 05-05 / D-03/D-04, updated for
// 05-08): install renders+writes+enables+starts the native villa-dashboard.service for
// boot-survival. Since 05-08 the dashboard is reconciled BEFORE the container starts (the
// reconcile was hoisted above the no-op early return so it runs on both paths), so this no
// longer asserts "dashboard last" — only the boot-survival invariants that still matter:
// the unit is written once, enabled once for [Install] WantedBy=default.target, and the
// dashboard service is started exactly once.
func TestInstallReconcilesDashboardForBootSurvival(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}

	// The unit was written + enabled exactly once for boot-survival.
	if f.dashWriteCalls != 1 {
		t.Errorf("dashboard unit write calls = %d, want 1", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 1 || len(f.dashEnabled) != 1 || f.dashEnabled[0] != orchestrate.DashboardServiceName {
		t.Errorf("dashboard enable = %v (count %d), want one enable of %s", f.dashEnabled, f.dashEnableCalls, orchestrate.DashboardServiceName)
	}

	// The dashboard service is present in the start list exactly once (reconciled+started).
	dashStarts := 0
	for _, svc := range f.startOrder {
		if svc == orchestrate.DashboardServiceName {
			dashStarts++
		}
	}
	if dashStarts != 1 {
		t.Errorf("dashboard service started %d times, want exactly 1 (startOrder = %v)", dashStarts, f.startOrder)
	}
}

// TestInstallReconcilesDashboardUnitOnNoOpPath is the primary regression for the 05-08
// gap (UAT Test 5): a STALE on-disk dashboard unit (old ExecStart) plus an UNCHANGED
// container plan must STILL rewrite/enable/(re)start the dashboard — install must no
// longer return at the no-op early-return BEFORE reconciling the dashboard. It also
// asserts the two lifecycles are decoupled in the correct direction: the container units
// are NOT spuriously rewritten (f.writeCalls==0), and the rewrite carries the resolved
// (absolute) ExecStart, not the stale one.
func TestInstallReconcilesDashboardUnitOnNoOpPath(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Unchanged: units} // zero Changed = no-op CONTAINER path
	f := newFakeInstallDeps(t, units, plan, passChecks())
	// A STALE on-disk unit carrying the OLD fixed ExecStart — anything that differs from
	// the freshly rendered bytes forces a reconcile. (Sanity: it must not equal the
	// rendered bytes for the default resolver path.)
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

	cmd, _, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("no-op-container + stale-dashboard install exit = %d, want exitPass", code)
	}
	// The stale unit was rewritten + enabled, and the dashboard service was (re)started —
	// the no-op CONTAINER path STILL reconciled the dashboard (the gap fix).
	if f.dashWriteCalls != 1 {
		t.Errorf("stale dashboard unit must be rewritten exactly once, wrote %d times", f.dashWriteCalls)
	}
	if f.dashEnableCalls != 1 {
		t.Errorf("reconciled dashboard unit must be enabled exactly once, enabled %d times", f.dashEnableCalls)
	}
	dashStarts := 0
	for _, svc := range f.startOrder {
		if svc == orchestrate.DashboardServiceName {
			dashStarts++
		}
	}
	if dashStarts != 1 {
		t.Errorf("reconciled dashboard service must be (re)started exactly once, startOrder = %v", f.startOrder)
	}
	// Lifecycles decoupled in the correct direction: the container units were genuinely
	// unchanged → install must NOT have rewritten them just because the dashboard differed.
	if f.writeCalls != 0 {
		t.Errorf("the unchanged container plan must NOT be rewritten, writeCalls = %d", f.writeCalls)
	}
	// The rewrite carries the resolver's (absolute) path, not the stale ExecStart.
	if !filepath.IsAbs(f.dashBinaryPath) {
		t.Errorf("reconciled dashboard ExecStart must use the resolved absolute path, got %q", f.dashBinaryPath)
	}
	if f.dashBinaryPath != "/opt/villa/bin/villa" {
		t.Errorf("reconciled dashboard binary path = %q, want the resolver's path /opt/villa/bin/villa", f.dashBinaryPath)
	}
}

// TestInstallDashboardUnitTargetsResolvedBinary (UAT Test 5 fix): runInstall must
// resolve the running villa binary path and thread it into the writeDashboardUnit seam
// so the rendered ExecStart points at a real, executable file — NOT the old fixed
// ~/.local/bin/villa the install flow never populated (which caused 203/EXEC at boot).
// The default liveInstallDeps resolves os.Executable()→EvalSymlinks→Abs; here we assert
// the threaded path is absolute, then prove a unit rendered with that path's executable
// has an ExecStart that exists and carries an executable bit on disk.
func TestInstallDashboardUnitTargetsResolvedBinary(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())

	// Wire the real resolver into the fake so we exercise the production resolution
	// path (os.Executable→EvalSymlinks→Abs) rather than a hand-fed constant.
	f.installDeps.resolveBinaryPath = resolveDashboardBinaryPath

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}

	if f.dashWriteCalls != 1 {
		t.Fatalf("dashboard write calls = %d, want 1", f.dashWriteCalls)
	}
	if f.dashBinaryPath == "" {
		t.Fatal("install did not thread a binary path into writeDashboardUnit")
	}
	if !filepath.IsAbs(f.dashBinaryPath) {
		t.Fatalf("threaded binary path %q is not absolute", f.dashBinaryPath)
	}

	// The resolved path is the test binary itself (os.Executable in the test process):
	// assert it exists and has an executable bit, mirroring what systemd needs at boot.
	fi, err := os.Stat(f.dashBinaryPath)
	if err != nil {
		t.Fatalf("resolved binary path %q does not exist on disk: %v", f.dashBinaryPath, err)
	}
	if fi.Mode().Perm()&0o111 == 0 {
		t.Fatalf("resolved binary path %q is not executable (mode %v)", f.dashBinaryPath, fi.Mode())
	}

	// End-to-end: a unit rendered with the resolved path has an ExecStart referencing
	// it, and that ExecStart executable exists + is executable (the boot-survival bar).
	body, err := orchestrate.RenderDashboardUnit(f.dashBinaryPath)
	if err != nil {
		t.Fatalf("RenderDashboardUnit: %v", err)
	}
	if !strings.Contains(body, "ExecStart=") || !strings.Contains(body, f.dashBinaryPath) {
		t.Fatalf("rendered unit ExecStart does not reference the resolved path %q\n%s", f.dashBinaryPath, body)
	}
}

// TestInstallFailsClosedWhenBinaryUnresolvable (WR-03): when the binary-path resolver
// errors, install must FAIL (exitBlocked) and write NO dashboard unit — it must never
// fall back to a fixed path. This locks the documented "fail closed, no fixed-path
// fallback" contract on resolveBinaryPath so a regression that swallowed the error and
// proceeded with a bogus ExecStart would be caught.
func TestInstallFailsClosedWhenBinaryUnresolvable(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.installDeps.resolveBinaryPath = func() (string, error) {
		return "", errors.New("os.Executable: boom")
	}

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("install exit = %d, want exitBlocked (%d) when the binary path is unresolvable", code, exitBlocked)
	}
	if f.dashWriteCalls != 0 {
		t.Errorf("dashboard unit was written %d time(s) despite an unresolvable binary path; want 0 (no fixed-path fallback)", f.dashWriteCalls)
	}
	if !strings.Contains(errOut.String(), "resolve the villa binary path") {
		t.Errorf("expected a binary-path resolution error on stderr, got:\n%s", errOut.String())
	}
}

// TestInstallEnsuresModelBeforeAnyStart: ensureModel (when the model is absent)
// is invoked BEFORE any service start — Open WebUI must not come up before the
// model exists or the picker would be empty on first visit (MODEL-04 / F-1).
func TestInstallEnsuresModelBeforeAnyStart(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.downloaded = false // force the pull path

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
		t.Fatalf("absent-model install exit = %d, want 0", code)
	}
	// The first cross-seam event must be ensureModel; every start must follow it.
	if len(f.callOrder) == 0 || f.callOrder[0] != "ensureModel" {
		t.Fatalf("ensureModel must run before any start, call order = %v", f.callOrder)
	}
	for i, ev := range f.callOrder {
		if strings.HasPrefix(ev, "start:") && i == 0 {
			t.Fatalf("a service started before ensureModel, call order = %v", f.callOrder)
		}
	}
}

// TestInstallDryRunStartsNothing: under --dry-run neither service is started and
// ensureModel is not called (the dry-run zero-side-effect contract, regression
// guard for the owui-start wiring).
func TestInstallDryRunStartsNothing(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.downloaded = false

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{dryRun: true}, f.installDeps); code != exitPass {
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
// a hard BLOCK (exit 1) with a clear "start villa-openwebui.service failed"
// message — mirrors the inference start-failure path.
func TestInstallOpenWebUIStartFailureBlocks(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.installDeps.start = func(service string) error {
		f.startCalls++
		f.startOrder = append(f.startOrder, service)
		if service == "villa-openwebui.service" {
			return errors.New("unit not found")
		}
		return nil
	}

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("owui-start-failure exit = %d, want 1", code)
	}
	if !strings.Contains(errOut.String(), "start villa-openwebui.service failed") {
		t.Errorf("owui start failure must surface a clear error, got %q", errOut.String())
	}
}

// TestInstallPullFailureBlocks: an ensureModel failure (e.g. network/verify
// failure) is a hard BLOCK (exit 1) — install must NOT proceed to persist config,
// write units, or start a service that would crash-loop on a missing GGUF (F-1).
func TestInstallPullFailureBlocks(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.downloaded = false
	f.installDeps.ensureModel = func(recommend.Recommendation) error {
		f.pullCalls++
		return errors.New("sha256 mismatch")
	}

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("pull-failure exit = %d, want 1", code)
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
	f := newFakeInstallDeps(t, units, plan, passChecks())

	// Order guard: record the call order of saveConfig vs writeUnits.
	var order []string
	f.installDeps.saveConfig = func(c config.VillaConfig) error {
		f.saveCalls++
		f.savedCfg = c
		order = append(order, "save")
		return nil
	}
	f.installDeps.writeUnits = func(orchestrate.Plan, string) error {
		f.writeCalls++
		order = append(order, "write")
		return nil
	}

	cmd, _, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	if f.saveCalls != 1 {
		t.Errorf("install must persist config exactly once, saved %d times", f.saveCalls)
	}
	if len(order) != 2 || order[0] != "save" || order[1] != "write" {
		t.Errorf("config must be persisted BEFORE units are written, got order %v", order)
	}
	if f.savedCfg.Model != "qwen2.5-0.5b" || f.savedCfg.Quant != "Q4_K_M" ||
		f.savedCfg.Ctx != 4096 || f.savedCfg.Backend != "vulkan" {
		t.Errorf("persisted config must hold the recommended selection, got %+v", f.savedCfg)
	}
}

// TestInstallPersistedConfigIsReconcileNoOp: the config install persists is the
// SAME config a follow-up `up`/reconcile renders from — proving the acceptance
// criterion that a post-install lifecycle reconcile is a TRUE no-op (units match).
// install renders from cfg X and persists cfg X; since render is a pure function
// of cfg, a follow-up lifecycle render from the persisted cfg reproduces the exact
// units install reconciled, so the next reconcile reports zero Changed.
func TestInstallPersistedConfigIsReconcileNoOp(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\nExec=llama --ctx 4096\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())

	// Capture the exact cfg install rendered the units FROM.
	var renderedFrom config.VillaConfig
	f.installDeps.render = func(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
		renderedFrom = in.Cfg
		return units, nil
	}

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}

	// The no-op guarantee: install persisted the SAME cfg it rendered the on-disk
	// units from. A lifecycle verb (up/restart) renders from this persisted cfg, so
	// it reproduces the identical units → its reconcile is a true no-op (units match).
	if f.savedCfg != renderedFrom {
		t.Errorf("persisted cfg %+v must equal the cfg install rendered from %+v — otherwise a follow-up up/restart would diff and not be a no-op", f.savedCfg, renderedFrom)
	}
	if f.savedCfg.Model == "" {
		t.Fatalf("install must persist a non-empty model so up/restart can resolve it (F-2)")
	}
}

// TestInstallBlockWithoutConsentExits1: a BLOCK preflight gap (SELinux off) with
// consent declined and no --force exits 1 and prints the exact setsebool command.
func TestInstallBlockWithoutConsentExits1(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	checks := []preflight.CheckResult{
		{ID: "PRE-05", Name: "SELinux container_use_devices boolean", Tier: preflight.TierBlock,
			Status: preflight.StatusWarn, Detail: "container_use_devices is OFF",
			Remediation: "run `setsebool -P container_use_devices=true`."},
	}
	f := newFakeInstallDeps(t, units, plan, checks)
	f.installDeps.consent = func(string) bool { return false } // declined

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("BLOCK-without-consent exit = %d, want 1", code)
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
// 17-UI-SPEC.md Copywriting "BLOCK gap declined" line verbatim (Pillar 1), with
// <check name> = c.Name and <remediation> = c.Remediation substituted, while the
// 0/2/1 exit contract is unchanged: a declined BLOCK gap with no --force stays
// exitBlocked with zero host mutation; --force still degrades to exitWarn without
// emitting the decline copy as a hard block.
func TestWizardBlockDeclinedCopy(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}

	// declineWizard simulates the real collector returning a declined consent for the
	// privileged BLOCK gap (PRE-05), so the single gateInstall → resolveGap consumes
	// the threaded false WITHOUT re-prompting stdin.
	declineWizard := func(deps *installDeps) {
		deps.interactive = func() bool { return true }
		deps.stdoutIsTTY = func() bool { return true }
		deps.consent = func(prompt string) bool {
			t.Errorf("d.consent must NOT be re-invoked on the threaded wizard-decline path (%q)", prompt)
			return false
		}
		deps.wizard = func(context.Context, wizardInput) (wizardResult, error) {
			return wizardResult{consentDecisions: map[string]bool{"PRE-05": false}}, nil
		}
	}

	// The contracted line with the seloffCheck name + remediation substituted verbatim.
	const wantLine = "BLOCK: SELinux container_use_devices boolean. run `setsebool -P container_use_devices=true`.. " +
		"Run the suggested command, or re-run with --no-tui --force to override (auditable)."

	t.Run("declined-without-force-blocks-with-contracted-copy", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		declineWizard(f.installDeps)

		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code != exitBlocked {
			t.Fatalf("declined BLOCK gap (no --force) exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), wantLine) {
			t.Errorf("wizard-decline output missing the contracted BLOCK-gap-declined copy.\n got: %q\nwant substring: %q", errOut.String(), wantLine)
		}
		// Zero host mutation: the privileged seam never ran, nothing written/started/pulled.
		if f.seboolCalls != 0 {
			t.Errorf("declined gap must NOT run setsebool, ran %d times", f.seboolCalls)
		}
		if f.writeCalls != 0 || f.startCalls != 0 || f.pullCalls != 0 || f.saveCalls != 0 {
			t.Errorf("a declined BLOCK install must not mutate: write=%d start=%d pull=%d save=%d",
				f.writeCalls, f.startCalls, f.pullCalls, f.saveCalls)
		}
	})

	t.Run("force-override-degrades-to-warn", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
		declineWizard(f.installDeps)

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{force: true}, f.installDeps)
		// --force still degrades the unmet gap to WARN (it was bypassed, not satisfied)
		// — the decline copy is the declined-without-force state, not a hard block here.
		if code != exitWarn {
			t.Fatalf("declined BLOCK gap with --force exit = %d, want exitWarn (%d)", code, exitWarn)
		}
		// The gap was bypassed → install proceeded to write the units.
		if f.writeCalls != 1 {
			t.Errorf("--force override must proceed to write units once, wrote %d times", f.writeCalls)
		}
	})
}

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

// TestInstallConsentYesRunsSeamOncePerGap: with an interactive TTY and y consent,
// the SELinux (BLOCK) and linger (WARN) gaps each invoke their fixed-arg seam
// exactly once, and install proceeds to write/start.
func TestInstallConsentYesRunsSeamOncePerGap(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	checks := []preflight.CheckResult{seloffCheck(), lingeroffCheck()}
	f := newFakeInstallDeps(t, units, plan, checks)
	f.installDeps.interactive = func() bool { return true }
	f.installDeps.consent = func(string) bool { return true } // approve every gap

	cmd, _, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
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
// so scripts parsing stderr do not misread it as an error (WR-07). Declining it must
// not block the install.
func TestInstallWarnLingerOfferGoesToStdout(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	// Only the WARN-tier linger gap (no BLOCK gaps) so the run is not blocked.
	f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{lingeroffCheck()})
	f.installDeps.interactive = func() bool { return true }
	f.installDeps.consent = func(string) bool { return false } // decline the optional offer

	cmd, out, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	// Declining an optional WARN gap must not block; install proceeds (readiness may
	// still WARN, so accept PASS or WARN — just never BLOCK).
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

// TestInstallDryRunNeverRunsPrivilegedHostPrep (phase-22 WR-05): --dry-run on an
// interactive TTY with BLOCK (PRE-05) and WARN (PRE-03) gaps and a CONSENTING stub
// must execute ZERO privileged seams, never prompt, and never enter the wizard —
// '--dry-run ... writes nothing' is a zero-side-effect contract (ORCH SC#1), and
// before the fix a consenting interactive dry-run executed setsebool/enable-linger.
func TestInstallDryRunNeverRunsPrivilegedHostPrep(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck(), lingeroffCheck()})
	f.installDeps.interactive = func() bool { return true }
	f.installDeps.stdoutIsTTY = func() bool { return true } // wizard-eligible TTY
	consentCalls := 0
	f.installDeps.consent = func(string) bool { consentCalls++; return true } // would consent!

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{dryRun: true}, f.installDeps)
	// The unmet BLOCK gap cannot be consented under dry-run, so the run blocks —
	// the same honest outcome the non-interactive path already had.
	if code != exitBlocked {
		t.Fatalf("dry-run with an unmet BLOCK gap exit = %d, want exitBlocked (%d)", code, exitBlocked)
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
	f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.installDeps.interactive = func() bool { return true }
	f.installDeps.consent = func(string) bool { return false } // decline

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
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
	f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.installDeps.interactive = func() bool { return false }
	consentCalls := 0
	f.installDeps.consent = func(string) bool { consentCalls++; return true }

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
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
// (auditable), writing/starting and exiting 2 (warn, not clean 0).
func TestInstallForceOverridesBlock(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, []preflight.CheckResult{seloffCheck()})
	f.installDeps.interactive = func() bool { return false }

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{force: true}, f.installDeps)
	if code != exitWarn {
		t.Fatalf("--force exit = %d, want 2", code)
	}
	if f.writeCalls != 1 || f.startCalls != 3 {
		t.Errorf("--force should proceed and start all three services (llama, owui, dashboard): write=%d start=%d", f.writeCalls, f.startCalls)
	}
	if !strings.Contains(out.String(), "Overridden") {
		t.Errorf("--force must print an auditable override summary, got %q", out.String())
	}
}

// TestInstallPostInstallPrintsLoopbackEndpoint: a clean install prints the loopback
// inference endpoint, the REAL loopback chat URL (Open WebUI is now brought up by
// install, D-03), and notes the dashboard comes later — with no dead links.
func TestInstallPostInstallPrintsLoopbackEndpoint(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("clean install exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "http://127.0.0.1:8080") {
		t.Errorf("post-install must print the loopback inference endpoint, got %q", out.String())
	}
	if !strings.Contains(out.String(), "chat (Open WebUI): http://127.0.0.1:3000") {
		t.Errorf("post-install must print the real loopback chat URL, got %q", out.String())
	}
	// The old combined "chat ... arrive in later phases" note must be gone.
	if strings.Contains(out.String(), "chat (Open WebUI) and the control dashboard arrive in later phases") {
		t.Errorf("post-install must no longer use the old combined chat/dashboard-later note, got %q", out.String())
	}
}

// TestInstallNoOpPrintsChatURL: the true-no-op path (units already match config)
// also points the user at the live chat URL — a re-run still tells you where to
// chat (D-03 — both callers of printPostInstall emit the real chat URL).
func TestInstallNoOpPrintsChatURL(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Unchanged: units} // zero Changed = true no-op
	f := newFakeInstallDeps(t, units, plan, passChecks())

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("no-op install exit = %d, want 0", code)
	}
	if !strings.Contains(out.String(), "chat (Open WebUI): http://127.0.0.1:3000") {
		t.Errorf("no-op post-install must also print the real chat URL, got %q", out.String())
	}
}

// TestReadiness503ThenReady: a probe returning 503 then 200 resolves to ready
// (PASS) — 503 is keep-polling, NOT a confident down (Pitfall 2 / WR-07).
func TestReadiness503ThenReady(t *testing.T) {
	calls := 0
	probe := func() (int, error) {
		calls++
		if calls < 2 {
			return http.StatusServiceUnavailable, nil
		}
		return http.StatusOK, nil
	}
	r := pollReadiness(context.Background(), probe, time.Second, time.Millisecond)
	if r.status != preflight.StatusPass {
		t.Fatalf("503-then-200 status = %v, want PASS (detail=%q)", r.status, r.detail)
	}
	if calls < 2 {
		t.Errorf("poll should have retried past the 503, calls=%d", calls)
	}
}

// TestReadinessTimeoutWarns: a probe that never returns 200 yields a WARN (typed-
// Unknown) at the deadline, never a confident FAIL (WR-07).
func TestReadinessTimeoutWarns(t *testing.T) {
	probe := func() (int, error) { return http.StatusServiceUnavailable, nil }
	r := pollReadiness(context.Background(), probe, 5*time.Millisecond, time.Millisecond)
	if r.status != preflight.StatusWarn {
		t.Fatalf("timeout status = %v, want WARN (not FAIL)", r.status)
	}
	if r.status == preflight.StatusFail {
		t.Errorf("readiness timeout must never be a confident FAIL")
	}
}

// TestReadinessTransportErrorKeepsPollingThenWarns: a transport error is keep-
// polling (server may still be coming up), resolving to WARN at the deadline.
func TestReadinessTransportErrorWarns(t *testing.T) {
	probe := func() (int, error) { return 0, errors.New("connection refused") }
	r := pollReadiness(context.Background(), probe, 5*time.Millisecond, time.Millisecond)
	if r.status != preflight.StatusWarn {
		t.Fatalf("transport-error status = %v, want WARN", r.status)
	}
}

// TestReadinessCancelledContextAbortsBeforeProbe: a context cancelled before the
// loop starts is observed before any probe runs, returning a WARN immediately
// (WR-05 — the deadline/cancellation is checked before each probe, not only after).
func TestReadinessCancelledContextAbortsBeforeProbe(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel() // cancel up-front
	probed := false
	probe := func() (int, error) {
		probed = true
		return http.StatusOK, nil
	}
	r := pollReadiness(ctx, probe, time.Minute, time.Millisecond)
	if r.status != preflight.StatusWarn {
		t.Fatalf("cancelled-ctx status = %v, want WARN", r.status)
	}
	if probed {
		t.Errorf("a pre-cancelled context must abort before any probe runs")
	}
}

// TestInstallReadinessWarnYieldsExit2: when the readiness poll WARNs, install
// exits 2 (warn) and the post-install health summary reflects it.
func TestInstallReadinessWarnYieldsExit2(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.installDeps.pollReady = func(context.Context, string) installReadiness {
		return installReadiness{status: preflight.StatusWarn, detail: "server did not become ready before the timeout"}
	}

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitWarn {
		t.Fatalf("readiness-warn exit = %d, want 2", code)
	}
	if !strings.Contains(out.String(), "health: WARN") {
		t.Errorf("post-install should show health WARN, got %q", out.String())
	}
}

// TestInstallRegistered: the `install` verb is wired into the command tree.
func TestInstallRegistered(t *testing.T) {
	root := newRoot()
	install, _, err := root.Find([]string{"install"})
	if err != nil || install.Name() != "install" {
		t.Fatalf("`install` verb not registered: %v", err)
	}
}

// --- Phase-19 memory-stack install tests -------------------------------------

// memoryUnits returns a Changed plan so runInstall reaches the write→start path
// (the memory start + proof steps live after the unit write).
func memoryUnits() ([]orchestrate.Unit, orchestrate.Plan) {
	// A realistic memory-on plan: Render appends the memory .container units when
	// MemoryEnabled, so they must be present in the plan for the WR-04 start guard
	// (which gates the memory-service starts on the units actually being in the plan).
	units := []orchestrate.Unit{
		{Name: "villa-llama.container", Text: "[Container]\n"},
		{Name: orchestrate.QdrantContainerUnitName(), Text: "[Container]\n"},
		{Name: orchestrate.EmbedContainerUnitName(), Text: "[Container]\n"},
	}
	return units, orchestrate.Plan{Changed: units}
}

// TestInstallMemoryGateUsesPersistedConfig is the WARNING-1 end-to-end check (T-19-16):
// it drives runInstall through the loadedMemoryEnabled seam (the PERSISTED config gate),
// NOT a hand-built cfg with MemoryEnabled set — so it would catch a gate mistakenly bound
// to the always-false DefaultVillaConfig() seed. With the seam returning true, the
// pre-stage (ensureEmbedModel) AND the memory start (villa-qdrant + villa-embed) AND the
// proof seam all fire; with it returning false, NONE fire.
func TestInstallMemoryGateUsesPersistedConfig(t *testing.T) {
	t.Run("persisted memory_enabled=true fires every memory seam", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true // the PERSISTED gate is on
		f.embedPresent = false // force the pre-stage pull path

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code != exitPass {
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
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = false // the PERSISTED gate is off (the v1.2 default)
		f.embedPresent = false  // even absent, nothing should pre-stage

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code != exitPass {
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

// TestInstallPreservesPersistedMemoryConfig (WR-02): install must SEED cfg from the
// user's persisted config and override ONLY the recommendation-derived fields
// (Model/Quant/Ctx/Backend) + the MemoryEnabled gate — it must NOT reset the user's
// customized memory address/port/model/dim fields (or dashboard/chat fields) to seed
// defaults. A user who set a non-default embed_port / embedding_model in config.toml
// must keep them after `villa install`. This locks the write-side single-source-of-truth
// fix (the same class as WR-01 on the render side).
func TestInstallPreservesPersistedMemoryConfig(t *testing.T) {
	// A realistic memory-on plan (memory units present) so the WR-04 start guard passes;
	// this test is about the WR-02 config-preservation, not the start gate.
	units, plan := memoryUnits()
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.memoryEnabled = true

	// A persisted config carrying NON-default memory customizations + a non-default
	// chat port, as if the user hand-edited config.toml.
	persisted := config.DefaultVillaConfig()
	persisted.MemoryEnabled = true
	persisted.EmbedPort = 9090
	persisted.EmbeddingModel = "custom-embed-model"
	persisted.QdrantPort = 7333
	persisted.QdrantAddr = "villa-qdrant-custom"
	persisted.ChatPort = 4444
	f.persistedConfig = &persisted

	cmd, _, _ := installTestCmd()
	if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
		t.Fatalf("install exit = %d, want 0", code)
	}

	// The persisted memory + chat customizations must survive into the saved cfg —
	// install must not have reset them to the seed defaults (8080/nomic.../6333/3000).
	if f.savedCfg.EmbedPort != 9090 {
		t.Errorf("install reset persisted embed_port to %d, want 9090 preserved (WR-02)", f.savedCfg.EmbedPort)
	}
	if f.savedCfg.EmbeddingModel != "custom-embed-model" {
		t.Errorf("install reset persisted embedding_model to %q, want \"custom-embed-model\" preserved (WR-02)", f.savedCfg.EmbeddingModel)
	}
	if f.savedCfg.QdrantPort != 7333 {
		t.Errorf("install reset persisted qdrant_port to %d, want 7333 preserved (WR-02)", f.savedCfg.QdrantPort)
	}
	if f.savedCfg.QdrantAddr != "villa-qdrant-custom" {
		t.Errorf("install reset persisted qdrant_addr to %q, want preserved (WR-02)", f.savedCfg.QdrantAddr)
	}
	if f.savedCfg.ChatPort != 4444 {
		t.Errorf("install reset persisted chat_port to %d, want 4444 preserved (WR-02)", f.savedCfg.ChatPort)
	}
	// The recommendation-derived fields are still overridden from the rec.
	if f.savedCfg.Model != "qwen2.5-0.5b" || f.savedCfg.Backend != "vulkan" {
		t.Errorf("install must still override the recommendation-derived fields, got %+v", f.savedCfg)
	}
}

// TestInstallPreservesPersistedROCmBackend is the regression for the
// install-reverts-rocm-to-vulkan debug session: `villa install` (and `villa install
// --coding-agent`) must PRESERVE a persisted `backend=rocm` opt-in, not silently revert
// it to vulkan. recommend.Pick always returns the default (vulkan) backend — ROCm is
// strictly opt-in and never auto-recommended (REC-04) — so the cmd tier must guard the
// `cfg.Backend = rec.Backend` assignment (config is the single source of truth for the
// backend opt-in). The test proves BOTH halves: (a) the rendered villa-llama unit resolves
// to the ROCm backend (not vulkan-radv), and (b) the persisted cfg keeps backend=rocm; plus
// a re-install on an unchanged plan is a true no-op on the ROCm choice. The default path
// (persisted vulkan) must still take the recommendation — the guard only PRESERVES an
// already-chosen opt-in, it never auto-selects ROCm.
func TestInstallPreservesPersistedROCmBackend(t *testing.T) {
	persistedRocm := func() *config.VillaConfig {
		c := config.DefaultVillaConfig()
		c.Backend = "rocm"
		return &c
	}

	t.Run("flag path preserves a persisted rocm opt-in in render and saved config", func(t *testing.T) {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.persistedConfig = persistedRocm()

		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("install exit = %d, want 0", code)
		}
		// (a) The unit was rendered from the persisted ROCm backend — NOT reverted to the
		// vulkan-radv default. The render path resolves cfg.Backend via inference.BackendFor,
		// so the captured RenderInput.Backend.Name() is the proof the ROCm unit is rendered.
		if !f.renderedInputSet {
			t.Fatal("render seam was never invoked — cannot assert the rendered backend")
		}
		if got := f.renderedInput.Backend.Name(); got != "rocm" {
			t.Errorf("install rendered backend %q, want \"rocm\" preserved (config is the single source of truth; ROCm is opt-in and never auto-reverted)", got)
		}
		// (b) The persisted opt-in survives into the saved cfg — install must not rewrite
		// config.toml back to backend=vulkan.
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("install reverted persisted backend to %q, want \"rocm\" preserved", f.savedCfg.Backend)
		}
		// The recommendation-derived NON-backend fields are still overridden from the rec.
		if f.savedCfg.Model != "qwen2.5-0.5b" {
			t.Errorf("install must still override the recommendation-derived model, got %q", f.savedCfg.Model)
		}
	})

	t.Run("--coding-agent path preserves a persisted rocm opt-in (the reported repro)", func(t *testing.T) {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.persistedConfig = persistedRocm()

		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps); code != exitPass {
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
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Unchanged: units} // zero Changed = no-op container path
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.persistedConfig = persistedRocm()
		// Pre-seed the on-disk dashboard unit with the rendered bytes so the dashboard
		// reconcile is ALSO a no-op (otherwise the dashboard always reconciles on a nil
		// diskUnit, which is unrelated to the backend-preservation no-op under test).
		f.diskUnit = mustRenderDashboardUnit(t, "/opt/villa/bin/villa")

		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("re-install exit = %d, want 0", code)
		}
		// The unchanged plan must NOT rewrite or (re)start the llama unit — preserving the
		// persisted ROCm backend keeps the rendered units byte-identical to disk, so the
		// reconcile is a true no-op on the ROCm choice (plan.Changed == 0).
		if f.writeCalls != 0 {
			t.Errorf("re-install on an unchanged rocm config must NOT rewrite container units, writeCalls = %d", f.writeCalls)
		}
		if contains(f.startOrder, "villa-llama.service") {
			t.Errorf("re-install on an unchanged rocm config must NOT restart the llama service, startOrder = %v", f.startOrder)
		}
		// The persisted opt-in is still rocm after the no-op re-install.
		if f.savedCfg.Backend != "rocm" {
			t.Errorf("no-op re-install reverted persisted backend to %q, want \"rocm\" preserved", f.savedCfg.Backend)
		}
	})

	t.Run("a persisted vulkan default still takes the recommendation (guard preserves only opt-ins)", func(t *testing.T) {
		units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
		plan := orchestrate.Plan{Changed: units}
		f := newFakeInstallDeps(t, units, plan, passChecks())
		// Default persisted config: backend=vulkan. The recommendation (also vulkan) wins;
		// the guard must NOT pin a non-opt-in value or block the recommendation flow.
		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("install exit = %d, want 0", code)
		}
		if got := f.renderedInput.Backend.Name(); got != "vulkan" {
			t.Errorf("default-path install rendered backend %q, want \"vulkan\" from the recommendation", got)
		}
		if f.savedCfg.Backend != "vulkan" {
			t.Errorf("default-path install saved backend %q, want \"vulkan\" from the recommendation", f.savedCfg.Backend)
		}
	})
}

// TestInstallMemoryServices: gate=true pre-stages when absent and starts the memory
// services in order (Qdrant before embed — Pitfall 4); gate off / dry-run → none called.
func TestInstallMemoryServices(t *testing.T) {
	t.Run("absent embed model is pre-staged then services start in order", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = false

		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("exit = %d, want 0", code)
		}
		if f.embedEnsureCalls != 1 {
			t.Errorf("absent embed model must be pre-staged once, calls = %d", f.embedEnsureCalls)
		}
		// The pre-stage must precede the embed service start, which must follow Qdrant.
		ensureIdx := indexOf(f.callOrder, "ensureEmbedModel")
		qIdx := indexOf(f.callOrder, "start:"+qdrantServiceName)
		eIdx := indexOf(f.callOrder, "start:"+embedServiceName)
		if ensureIdx < 0 || qIdx < 0 || eIdx < 0 {
			t.Fatalf("missing expected events in callOrder = %v", f.callOrder)
		}
		if !(ensureIdx < eIdx && qIdx < eIdx) {
			t.Errorf("ordering wrong: ensure(%d) and qdrant(%d) must precede embed(%d); callOrder = %v", ensureIdx, qIdx, eIdx, f.callOrder)
		}
	})

	t.Run("present embed model is not re-pulled", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = true // already on disk

		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
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
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = false
		f.embedPresent = false

		cmd, _, _ := installTestCmd()
		_ = runInstall(cmd, installOpts{}, f.installDeps)
		if f.embedEnsureCalls != 0 || f.embedPresentCalls != 0 {
			t.Errorf("gate off must not touch the embed model (ensure=%d present=%d)", f.embedEnsureCalls, f.embedPresentCalls)
		}
		if contains(f.startOrder, qdrantServiceName) || contains(f.startOrder, embedServiceName) {
			t.Errorf("gate off must not start memory services, startOrder = %v", f.startOrder)
		}
	})

	t.Run("dry-run pre-stages and starts nothing", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		f.embedPresent = false

		cmd, _, _ := installTestCmd()
		_ = runInstall(cmd, installOpts{dryRun: true}, f.installDeps)
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

// TestInstallMemoryOnButUnitsAbsentFailsClosed (WR-04): when memory is enabled but the
// memory .container units are absent from the rendered plan (a hypothetical future
// render/reconcile bug that drops them), install must NOT attempt to start a service
// systemd has never seen. It must fail closed (exitBlocked) with a CLEAR internal-error
// remediation, never a bare "Unit not found", and never call start for the memory
// services. This locks the "start gate = unit present in the plan" invariant.
func TestInstallMemoryOnButUnitsAbsentFailsClosed(t *testing.T) {
	// A plan that has the inference unit (so the install proceeds to the start phase)
	// but is MISSING the memory units — the exact gap the WR-04 guard catches.
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "[Container]\n"}}
	plan := orchestrate.Plan{Changed: units}
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.memoryEnabled = true // memory ON, but the plan omits the memory units
	f.embedPresent = true

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("memory-on with absent memory units must fail closed, exit = %d, want exitBlocked (%d)", code, exitBlocked)
	}
	// A clear internal-error remediation, naming the absent units — not a bare systemd error.
	if !strings.Contains(errOut.String(), "INTERNAL ERROR") ||
		!strings.Contains(errOut.String(), orchestrate.QdrantContainerUnitName()) {
		t.Errorf("expected a clear internal-error message naming the absent memory units, got:\n%s", errOut.String())
	}
	// The memory services must NEVER have been started (the guard fires before start).
	if contains(f.startOrder, qdrantServiceName) || contains(f.startOrder, embedServiceName) {
		t.Errorf("memory services must not be started when their units are absent, startOrder = %v", f.startOrder)
	}
}

// TestEmbedGGUFFilenameSingleSource asserts the pre-stage Shard filename equals the
// orchestrate single-source accessor UNCONDITIONALLY (Pitfall 3) — the served `-m` path
// and the staged file can never drift. No literal fallback, no TODO branch.
func TestEmbedGGUFFilenameSingleSource(t *testing.T) {
	if nomicEmbedShard.Filename != orchestrate.EmbedGGUFFilename() {
		t.Fatalf("embed GGUF filename drift: nomicEmbedShard.Filename = %q, orchestrate.EmbedGGUFFilename() = %q",
			nomicEmbedShard.Filename, orchestrate.EmbedGGUFFilename())
	}
}

// TestNomicShardValues pins the verified integrity values (PRIV-04/D-07): a typo in the
// size or SHA256 would let an unverified GGUF through, so they are asserted here.
func TestNomicShardValues(t *testing.T) {
	if nomicEmbedShard.SizeBytes != 146146432 {
		t.Errorf("nomicEmbedShard.SizeBytes = %d, want 146146432", nomicEmbedShard.SizeBytes)
	}
	if nomicEmbedShard.SHA256 != "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7" {
		t.Errorf("nomicEmbedShard.SHA256 = %q, want 3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7", nomicEmbedShard.SHA256)
	}
}

// TestLiveEmbedModelPresentSizeGuard (IN-03): the embed-model presence check treats the
// GGUF as present only when its on-disk size matches nomicEmbedShard.SizeBytes — a
// truncated/tampered file is NOT trusted (returns false → re-pull + re-verify). A
// correctly-sized file is present; an absent file is not present.
func TestLiveEmbedModelPresentSizeGuard(t *testing.T) {
	t.Run("absent file is not present", func(t *testing.T) {
		dir := t.TempDir()
		if liveEmbedModelPresent(dir) {
			t.Error("an absent embed GGUF must not be reported present")
		}
	})

	t.Run("truncated file is not present (integrity guard)", func(t *testing.T) {
		dir := t.TempDir()
		// Write a too-short file at the expected path: present on disk but the wrong size.
		if err := os.WriteFile(embedModelPath(dir), []byte("not the real weight"), 0o600); err != nil {
			t.Fatalf("seed truncated file: %v", err)
		}
		if liveEmbedModelPresent(dir) {
			t.Error("a truncated/tampered embed GGUF must be treated as NOT present so it is re-pulled (IN-03)")
		}
	})

	t.Run("correctly-sized file is present", func(t *testing.T) {
		dir := t.TempDir()
		// A sparse file of exactly the expected size — present + correct size → present.
		f, err := os.Create(embedModelPath(dir))
		if err != nil {
			t.Fatalf("create: %v", err)
		}
		if err := f.Truncate(int64(nomicEmbedShard.SizeBytes)); err != nil {
			t.Fatalf("truncate to expected size: %v", err)
		}
		if err := f.Close(); err != nil {
			t.Fatalf("close: %v", err)
		}
		if !liveEmbedModelPresent(dir) {
			t.Error("a correctly-sized embed GGUF must be reported present (no needless re-pull)")
		}
	})
}

// --- Task 2: memory-stack readiness proof tests ------------------------------

// TestInstallMemoryProofPass: a PASS proof verdict leaves the exit code unaffected and
// prints the "memory stack ready" line.
func TestInstallMemoryProofPass(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.memoryProofStatus = preflight.StatusPass
	// A distinctive PASS detail so the test can prove install prints the verdict's OWN
	// detail (IN-02), not a re-typed "768-dim …" literal.
	f.memoryProofDetail = "768-dim embeddings + Qdrant writable"

	cmd, out, _ := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitPass {
		t.Fatalf("proof-pass exit = %d, want 0", code)
	}
	if f.memoryProofCalls != 1 {
		t.Errorf("proof must run once, calls = %d", f.memoryProofCalls)
	}
	if !strings.Contains(out.String(), "memory stack ready") {
		t.Errorf("a PASS proof must print the ready line, got %q", out.String())
	}
	// IN-02: the printed line carries the proof's OWN detail (single-sourced dim), not a
	// duplicated literal — so a dimension change in the verdict flows through here.
	if !strings.Contains(out.String(), "memory stack ready: 768-dim embeddings + Qdrant writable") {
		t.Errorf("a PASS proof must print the verdict's detail, got %q", out.String())
	}
	// The proof input must be resolved from the persisted config defaults (768-dim).
	if f.memoryProofIn.embeddingDim != 768 {
		t.Errorf("proof input embeddingDim = %d, want 768", f.memoryProofIn.embeddingDim)
	}
	if f.memoryProofIn.embedAddr != "villa-embed" || f.memoryProofIn.qdrantAddr != "villa-qdrant" {
		t.Errorf("proof input addrs = %q/%q, want villa-embed/villa-qdrant", f.memoryProofIn.embedAddr, f.memoryProofIn.qdrantAddr)
	}
}

// TestInstallMemoryProofFail: a FAIL proof verdict makes runInstall return exitBlocked
// and surface the remediation detail (refuse-with-remediation, never a silent skip).
func TestInstallMemoryProofFail(t *testing.T) {
	units, plan := memoryUnits()
	f := newFakeInstallDeps(t, units, plan, passChecks())
	f.memoryEnabled = true
	f.memoryProofStatus = preflight.StatusFail
	f.memoryProofDetail = "the embeddings endpoint did not answer"

	cmd, _, errOut := installTestCmd()
	code := runInstall(cmd, installOpts{}, f.installDeps)
	if code != exitBlocked {
		t.Fatalf("proof-fail exit = %d, want %d (refuse-with-remediation)", code, exitBlocked)
	}
	if !strings.Contains(errOut.String(), "the embeddings endpoint did not answer") {
		t.Errorf("a FAIL proof must surface the remediation detail, got %q", errOut.String())
	}
}

// TestInstallMemoryProofSkippedWhenOffOrDryRun: the proof is not invoked when memory is
// off or under --dry-run.
func TestInstallMemoryProofSkippedWhenOffOrDryRun(t *testing.T) {
	t.Run("gate off", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = false

		cmd, _, _ := installTestCmd()
		_ = runInstall(cmd, installOpts{}, f.installDeps)
		if f.memoryProofCalls != 0 {
			t.Errorf("gate off must not run the proof, calls = %d", f.memoryProofCalls)
		}
	})
	t.Run("dry-run", func(t *testing.T) {
		units, plan := memoryUnits()
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true

		cmd, _, _ := installTestCmd()
		_ = runInstall(cmd, installOpts{dryRun: true}, f.installDeps)
		if f.memoryProofCalls != 0 {
			t.Errorf("dry-run must not run the proof, calls = %d", f.memoryProofCalls)
		}
	})
}

// TestEvalMemoryProof table-drives the PURE proof core over the four outcomes.
func TestEvalMemoryProof(t *testing.T) {
	const wantDim = 768
	cases := []struct {
		name       string
		embedDim   int
		embedErr   error
		writable   bool
		qdrantErr  error
		wantStatus preflight.Status
	}{
		{"embed ok + qdrant writable", wantDim, nil, true, nil, preflight.StatusPass},
		{"wrong dim", 256, nil, true, nil, preflight.StatusFail},
		{"embed err", 0, errors.New("connection refused"), true, nil, preflight.StatusFail},
		{"qdrant not writable", wantDim, nil, false, nil, preflight.StatusFail},
		{"qdrant err", wantDim, nil, false, errors.New("readyz 503"), preflight.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			embedProbe := func() (int, error) { return tc.embedDim, tc.embedErr }
			qdrantProbe := func() (bool, error) { return tc.writable, tc.qdrantErr }
			got := evalMemoryProof(context.Background(), embedProbe, qdrantProbe, wantDim)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Errorf("a FAIL verdict must carry a remediation detail")
			}
		})
	}
}

// TestQdrantWritableProbeIdempotent (WR-03): a pre-existing villa-probe collection from
// an interrupted prior run must NOT make the writable proof FAIL. The probe issues a
// best-effort DELETE before the PUT-create, so a fake curl runner that fails a PUT against
// an existing collection (unless a DELETE preceded it) must yield writable=true, not an
// error. It also locks the no-leftover guarantee (a closing DELETE runs) and the clean-
// store case (no stale collection → still passes).
func TestQdrantWritableProbeIdempotent(t *testing.T) {
	const base = "http://villa-qdrant:6333"
	coll := base + "/collections/" + villaProbeCollection

	t.Run("stale leftover collection does not cause a FAIL", func(t *testing.T) {
		// exists models a leftover probe collection: a PUT-create while it exists is a
		// non-2xx (curl -sf error); only after a DELETE does the PUT succeed.
		exists := true
		var calls []string
		curl := func(args ...string) ([]byte, error) {
			// Reconstruct a coarse "METHOD path" signature from the fixed-arg call.
			method, path := "GET", args[len(args)-1]
			for i, a := range args {
				if a == "-X" && i+1 < len(args) {
					method = args[i+1]
				}
			}
			// The PUT/DELETE target is the coll arg (the token right after the method).
			for i, a := range args {
				if a == "-X" && i+2 < len(args) {
					path = args[i+2]
				}
			}
			calls = append(calls, method+" "+path)
			switch {
			case method == "GET" && path == base+"/readyz":
				return []byte("ok"), nil
			case method == "DELETE" && path == coll:
				exists = false
				return []byte("{}"), nil
			case method == "PUT" && path == coll:
				if exists {
					return nil, errors.New("409 Conflict: collection already exists")
				}
				return []byte("{}"), nil
			default:
				return []byte("{}"), nil
			}
		}

		writable, err := qdrantWritableProbe(curl, base, 768)
		if err != nil {
			t.Fatalf("a leftover probe collection must not FAIL the writable proof, got err: %v\ncalls: %v", err, calls)
		}
		if !writable {
			t.Fatalf("writable = false, want true (the create succeeded after the pre-DELETE)\ncalls: %v", calls)
		}
		// The pre-DELETE must precede the PUT (the idempotency ordering, WR-03).
		delIdx, putIdx := indexOf(calls, "DELETE "+coll), indexOf(calls, "PUT "+coll)
		if delIdx < 0 || putIdx < 0 || delIdx >= putIdx {
			t.Errorf("expected a DELETE before the PUT-create, got call order %v", calls)
		}
	})

	t.Run("clean store still proves writable", func(t *testing.T) {
		curl := func(args ...string) ([]byte, error) { return []byte("{}"), nil }
		writable, err := qdrantWritableProbe(curl, base, 768)
		if err != nil || !writable {
			t.Fatalf("clean-store probe must pass, got writable=%v err=%v", writable, err)
		}
	})

	t.Run("readyz failure FAILs", func(t *testing.T) {
		curl := func(args ...string) ([]byte, error) {
			if args[len(args)-1] == base+"/readyz" {
				return nil, errors.New("connection refused")
			}
			return []byte("{}"), nil
		}
		if _, err := qdrantWritableProbe(curl, base, 768); err == nil {
			t.Fatal("a /readyz failure must surface an error")
		}
	})
}

// indexOf returns the first index of v in s, or -1.
func indexOf(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}

// TestInstallMemoryGateRefusesUnfitHost is the CTRL-06 install half: an opted-in
// install whose memory host-fitness gate reports a confident shortage refuses-
// with-remediation (exitBlocked) BEFORE bringing up the memory stack — zero host
// mutation. With memory off the gate seam never fires (D-06: the memory-off
// install path is byte-identical).
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
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.memoryEnabled = true
		gateCalls := 0
		f.installDeps.runMemoryChecks = func(detect.HostProfile) []preflight.CheckResult {
			gateCalls++
			return []preflight.CheckResult{memDiskFail}
		}

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code != exitBlocked {
			t.Fatalf("unfit memory host exit = %d, want exitBlocked (%d)", code, exitBlocked)
		}
		if gateCalls != 1 {
			t.Errorf("memory gate must run exactly once, ran %d times", gateCalls)
		}
		// Refused BEFORE the stack comes up: nothing written, started, pulled, or
		// persisted — and the memory pre-stage/proof seams never fired.
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
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.installDeps.runMemoryChecks = func(detect.HostProfile) []preflight.CheckResult {
			t.Error("memory-off install must NOT run the memory host-fitness gate")
			return nil
		}

		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code == exitBlocked {
			t.Fatalf("memory-off install must not be blocked by the memory gate, exit = %d", code)
		}
	})
}

// TestEvalAgentProof asserts the coding-agent install-readiness verdict (D-05, T-27-05):
// PASS only on a REAL tool-call edit; FAIL on no-edit and on err. A health-200 is NEVER an
// input — the only signal is the planted read→edit round-trip result.
func TestEvalAgentProof(t *testing.T) {
	cases := []struct {
		name       string
		edited     bool
		err        error
		wantStatus preflight.Status
	}{
		{"real edit passes", true, nil, preflight.StatusPass},
		{"no edit fails (false-green guard)", false, nil, preflight.StatusFail},
		{"round-trip err fails", false, errors.New("crush run: exit 1"), preflight.StatusFail},
		{"err takes precedence over a stale edited=true", true, errors.New("timeout"), preflight.StatusFail},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			toolCall := func() (bool, error) { return tc.edited, tc.err }
			got := evalAgentProof(toolCall)
			if got.status != tc.wantStatus {
				t.Errorf("status = %v, want %v (detail %q)", got.status, tc.wantStatus, got.detail)
			}
			if tc.wantStatus == preflight.StatusFail && got.detail == "" {
				t.Error("a FAIL verdict must carry a remediation detail")
			}
		})
	}
}

// TestCoderShardSingleSource proves D-04: the staged coder shard and the served coder model
// resolve from ONE catalog entry (the recommend-picked id), not two independent literals. It
// resolves the shard via coderShardFor against the picked rec.Coder.Model, then asserts the
// resolved entry's id is exactly the served coder id — a single-entry derivation, so the
// staged filename and the served -m path can never drift.
func TestCoderShardSingleSource(t *testing.T) {
	cat := catalog.Catalog{Models: []catalog.CatalogModel{
		{ID: "qwen3-chat", Role: ""},
		{ID: "qwen3-coder-30b", Role: "coder", Shards: []catalog.Shard{
			{Filename: "qwen3-coder-30b.Q4.gguf", SizeBytes: 18_000_000_000},
		}},
	}}
	// The served coder id comes from the recommend pick (cfg.CoderModel is set from it at
	// coding-mode enter — the SAME id). coderShardFor must resolve THAT entry's shard.
	rec := recommend.Recommendation{Coder: recommend.CoderFit{Model: "qwen3-coder-30b"}}

	sh, ok := coderShardFor(rec, cat)
	if !ok {
		t.Fatal("coderShardFor did not resolve the picked coder entry's shard")
	}
	// Find the entry coderShardFor resolved from and assert it is the SAME entry whose id is
	// the served coder model — one entry drives both the staged filename and the served id.
	var servedEntry catalog.CatalogModel
	for _, m := range cat.Models {
		if m.ID == rec.Coder.Model {
			servedEntry = m
		}
	}
	if servedEntry.ID != rec.Coder.Model {
		t.Fatalf("served coder entry id = %q, want the picked id %q", servedEntry.ID, rec.Coder.Model)
	}
	if len(servedEntry.Shards) == 0 || sh.Filename != servedEntry.Shards[0].Filename {
		t.Errorf("staged shard %q does not derive from the served entry's Shards[0] %v — D-04 single-source break",
			sh.Filename, servedEntry.Shards)
	}
}

// TestInstallCodingAgentFlow asserts the --coding-agent gate block (D-01/D-03/D-05): with the
// flag set the FSL notice prints, the coder GGUF + binary are staged and the config rendered
// BEFORE the readiness proof, AgentEnabled is persisted, the render sees AgentEnabled=true,
// and a clean install passes. An agent-off install fires NONE of the agent seams.
func TestInstallCodingAgentFlow(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "x"}}
	plan := orchestrate.Plan{Changed: units}

	t.Run("--coding-agent stages, persists, and proves the agent", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.coderPresent = false // exercise the pre-stage pull path

		cmd, out, _ := installTestCmd()
		code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps)
		if code != exitPass {
			t.Fatalf("clean coding-agent install exit = %d, want %d", code, exitPass)
		}
		// FSL-1.1-MIT notice surfaced before staging.
		if !strings.Contains(out.String(), "FSL-1.1-MIT") {
			t.Errorf("install output must surface the FSL-1.1-MIT notice; got:\n%s", out.String())
		}
		// All agent seams fired exactly once.
		if f.coderEnsureCalls != 1 || f.binaryInstallCalls != 1 || f.renderCrushCalls != 1 || f.agentProofCalls != 1 {
			t.Errorf("agent seam counts: coderEnsure=%d binaryInstall=%d render=%d proof=%d, want 1/1/1/1",
				f.coderEnsureCalls, f.binaryInstallCalls, f.renderCrushCalls, f.agentProofCalls)
		}
		// The gate persisted AgentEnabled and the render saw it true (D-01).
		if !f.savedCfg.AgentEnabled {
			t.Error("--coding-agent must persist cfg.AgentEnabled = true (D-01)")
		}
		if !f.renderedAgentEnabled {
			t.Error("renderCrushConfig must receive cfg.AgentEnabled = true")
		}
		// Ordering: stage + render BEFORE the readiness proof (D-05).
		if idx(f.callOrder, "renderCrushConfig") > idx(f.callOrder, "agentProof") {
			t.Errorf("config must be rendered before the readiness proof; callOrder = %v", f.callOrder)
		}
		if idx(f.callOrder, "installAgentBinary") < 0 || idx(f.callOrder, "ensureCoderModel") < 0 {
			t.Errorf("binary install + coder pre-stage must both run; callOrder = %v", f.callOrder)
		}
	})

	t.Run("--coding-agent serves the coder (CR-01): RenderInput.CodingMode != nil + served id == rec.Coder.Model", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		// Use a REAL catalog coder id so codingModelFile/codingDescriptor (which read the
		// embedded catalog via modelCatalogPath, not the fake) resolve. rec.Coder carries a
		// real swap-residency coder fit with a non-zero agent ctx and quant.
		f.installDeps.pick = func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{
				Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan",
				WeightBytes: 1 << 30, KVCacheBytes: 1 << 28, HeadroomBytes: 1 << 28,
				UsableEnvelopeBytes: 8 << 30, Fits: true,
				Coder: recommend.CoderFit{
					Model: "qwen3-coder-30b-a3b", Quant: "Q4_K_M", AgentCtx: 65536,
					Fits: true, Residency: "swap",
				},
			}
		}
		// Catalog the coderShardFor pre-stage resolves against must carry the same id.
		f.agentCat = catalog.Catalog{Models: []catalog.CatalogModel{
			{ID: "qwen3-coder-30b-a3b", Role: "coder", Shards: []catalog.Shard{
				{Filename: "qwen3-coder-30b-a3b.gguf", SizeBytes: 4096},
			}},
		}}

		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps)
		if code != exitPass {
			t.Fatalf("coding-agent install exit = %d, want %d; stderr:\n%s", code, exitPass, errOut.String())
		}
		if !f.renderedInputSet {
			t.Fatal("render seam was never called")
		}
		// CR-01: the coder must be SERVED — a non-nil CodingMode descriptor + coder render inputs.
		if f.renderedInput.CodingMode == nil {
			t.Error("--coding-agent must thread a non-nil RenderInput.CodingMode (the coder is served)")
		}
		if f.renderedInput.Cfg.CoderModel != "qwen3-coder-30b-a3b" {
			t.Errorf("RenderInput.Cfg.CoderModel = %q, want rec.Coder.Model %q",
				f.renderedInput.Cfg.CoderModel, "qwen3-coder-30b-a3b")
		}
		if !f.renderedInput.Cfg.CodingMode {
			t.Error("RenderInput.Cfg.CodingMode must be true on --coding-agent")
		}
		if f.renderedInput.CoderAgentCtx != 65536 {
			t.Errorf("RenderInput.CoderAgentCtx = %d, want rec.Coder.AgentCtx %d",
				f.renderedInput.CoderAgentCtx, 65536)
		}
		// The persisted config carries the coder fields so a bare `villa up` re-renders the coder.
		if f.savedCfg.CoderModel != "qwen3-coder-30b-a3b" || !f.savedCfg.CodingMode ||
			f.savedCfg.CoderQuant != "Q4_K_M" || f.savedCfg.CoderAgentCtx != 65536 {
			t.Errorf("persisted cfg must carry coder fields: CoderModel=%q CodingMode=%v CoderQuant=%q CoderAgentCtx=%d",
				f.savedCfg.CoderModel, f.savedCfg.CodingMode, f.savedCfg.CoderQuant, f.savedCfg.CoderAgentCtx)
		}
	})

	t.Run("chat-only install is off-path byte-identical: RenderInput.CodingMode == nil + CoderModel empty", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		cmd, _, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
		if code != exitPass {
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
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.agentProofStatus = preflight.StatusFail
		f.agentProofDetail = "the coding agent ran but did not perform the tool-call edit"

		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps)
		if code != exitBlocked {
			t.Fatalf("an agent readiness FAIL must block, exit = %d want %d", code, exitBlocked)
		}
		if !strings.Contains(errOut.String(), "coding agent not ready") {
			t.Errorf("a readiness FAIL must refuse-with-remediation; got:\n%s", errOut.String())
		}
	})

	t.Run("shared-residency coder fit refuses with a swap-only message, NOT free-memory copy (WR-03)", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		// Catalog with no coder entry → coderShardFor would return false, but the shared-
		// residency branch refuses BEFORE reaching it.
		f.agentCat = catalog.Catalog{Models: []catalog.CatalogModel{{ID: "qwen3-chat"}}}
		// rec.Coder is a SHARED-residency fit (Residency "shared", empty Model). A coder DOES
		// fit conceptually (riding the chat endpoint) but v1.4 only serves a dedicated swap-
		// residency coder, so the addon refuses with the swap-only message — NOT the misleading
		// "free memory / use a larger host" copy. Coding-mode is NOT entered (cfg.CodingMode
		// stays false) and the addon refuses at the step-6c shared-residency gate.
		f.installDeps.pick = func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{
				Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "vulkan",
				WeightBytes: 1 << 30, KVCacheBytes: 1 << 28, HeadroomBytes: 1 << 28,
				UsableEnvelopeBytes: 8 << 30, Fits: true,
				Coder: recommend.CoderFit{Fits: false, Residency: recommend.ResidencyShared},
			}
		}

		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps)
		if code != exitBlocked {
			t.Fatalf("a shared-residency coder fit must block the addon, exit = %d want %d", code, exitBlocked)
		}
		if f.binaryInstallCalls != 0 || f.agentProofCalls != 0 {
			t.Errorf("no agent staging/proof after a coder-fit refusal: binaryInstall=%d proof=%d",
				f.binaryInstallCalls, f.agentProofCalls)
		}
		got := errOut.String()
		if !strings.Contains(got, "swap-residency coder fit") || !strings.Contains(got, "SHARED residency") {
			t.Errorf("a shared-residency refusal must explain the swap-only limitation; got:\n%s", got)
		}
		// WR-03 cardinal point: the operator must NOT be misdirected toward freeing memory.
		if strings.Contains(got, "free memory or use a larger-envelope host") {
			t.Errorf("a shared-residency refusal must NOT use the misleading no-fit free-memory copy; got:\n%s", got)
		}
	})

	t.Run("agent-off install fires NO agent seam (D-01 byte-identical)", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		// agentEnabled stays false (default); no --coding-agent flag.
		cmd, out, _ := installTestCmd()
		code := runInstall(cmd, installOpts{}, f.installDeps)
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
			t.Error("agent-off install must persist AgentEnabled = false (D-01)")
		}
	})
}

// TestInstallAgentPreflightFold asserts the INSTALL-04/D-09 preflight fold: the
// runAgentChecks seam is appended to the install gate ONLY when the addon is enabled
// (--coding-agent or persisted agent_enabled), an agent-off install never calls it (the
// gate stays byte-identical), and an agent BLOCK from the fold refuses the install
// through the SAME gateInstall (inheriting refuse-with-remediation).
func TestInstallAgentPreflightFold(t *testing.T) {
	units := []orchestrate.Unit{{Name: "villa-llama.container", Text: "x"}}
	plan := orchestrate.Plan{Changed: units}

	t.Run("--coding-agent folds the agent checks exactly once", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps); code != exitPass {
			t.Fatalf("clean agent install exit = %d, want %d", code, exitPass)
		}
		if f.agentChecksCalls != 1 {
			t.Errorf("runAgentChecks must fire exactly once when the addon is on; got %d", f.agentChecksCalls)
		}
	})

	t.Run("agent-off install never folds the agent checks (byte-identical)", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		cmd, _, _ := installTestCmd()
		if code := runInstall(cmd, installOpts{}, f.installDeps); code != exitPass {
			t.Fatalf("agent-off install exit = %d, want %d", code, exitPass)
		}
		if f.agentChecksCalls != 0 {
			t.Errorf("agent-off install must NOT fold the agent checks; got %d", f.agentChecksCalls)
		}
	})

	t.Run("an agent BLOCK from the fold refuses the install", func(t *testing.T) {
		f := newFakeInstallDeps(t, units, plan, passChecks())
		f.agentChecks = []preflight.CheckResult{{
			ID: agentEnvelopeCheckID, Name: "Coding-agent memory envelope",
			Tier: preflight.TierBlock, Status: preflight.StatusFail,
			Detail: "no coder model fits", Remediation: "free memory",
		}}
		cmd, _, errOut := installTestCmd()
		code := runInstall(cmd, installOpts{codingAgent: true}, f.installDeps)
		if code != exitBlocked {
			t.Fatalf("an agent BLOCK must refuse the install, exit = %d want %d", code, exitBlocked)
		}
		if f.binaryInstallCalls != 0 || f.agentProofCalls != 0 {
			t.Errorf("a blocked gate must not stage/prove the agent: binary=%d proof=%d",
				f.binaryInstallCalls, f.agentProofCalls)
		}
		if !strings.Contains(errOut.String(), "BLOCKED") {
			t.Errorf("an agent BLOCK must refuse-with-remediation via gateInstall; got:\n%s", errOut.String())
		}
	})
}

// idx returns the index of v in s, or -1 if absent (a small test helper for callOrder
// ordering assertions).
func idx(s []string, v string) int {
	for i, x := range s {
		if x == v {
			return i
		}
	}
	return -1
}
