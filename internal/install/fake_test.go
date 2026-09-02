package install

import (
	"bytes"
	"context"
	"os"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// fake_test.go is the one test double for Deps: every flow test drives Run through
// it. It records the ORDER of every host-touching call so a test can assert the
// sequence, not merely the counts, and it controls every verdict the flow reads.

// The exit-code contract, restated here so a test reads as the operator sees it.
const (
	exitPass    = 0
	exitWarn    = 2
	exitBlocked = 1
)

type fakeDeps struct {
	*Deps

	writeCalls  int
	reloadCalls int
	startCalls  int
	lingerCalls int
	seboolCalls int
	pollCalls   int
	pullCalls   int
	saveCalls   int
	wizardCalls int
	startOrder  []string
	// callOrder records every effect, in order: ensureModel, start:<svc>,
	// dashWrite, enable:<svc>, stop:<svc>, removeUnit:<name>, removeConfig, …
	callOrder  []string
	downloaded bool
	savedCfg   config.VillaConfig

	dashWriteCalls  int
	dashEnableCalls int
	dashEnabled     []string
	dashBinaryPath  string
	// diskUnit is the on-disk dashboard unit; nil reads as absent (first install).
	diskUnit []byte

	persistedConfig   *config.VillaConfig
	configLoadErr     error
	activeState       string
	priorUnits        map[string]string
	priorConfigExists bool
	stopOrder         []string
	removedUnits      []string
	configRemoved     bool
	stopErr           error
	removeUnitErr     error

	memoryEnabled     bool
	embedPresent      bool
	embedEnsureCalls  int
	embedPresentCalls int
	memoryProofCalls  int
	memoryProofCfg    config.VillaConfig
	memoryProofStatus preflight.Status
	memoryProofDetail string

	webSearchEnabled      bool
	searxngSettingsCalls  int
	searxngSecretEnvCalls int
	searxngProofCalls     int
	searxngProofStatus    preflight.Status
	searxngProofDetail    string
	websafeSecretEnvCalls int

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
	renderedInput        orchestrate.RenderInput
	renderedInputSet     bool
	agentChecksCalls     int
	agentChecks          []preflight.CheckResult
}

// newFakeDeps builds the default world: a re-install over a running stack whose
// weights are present, every proof passing, memory/web-search/agent off, the flag
// path (no TTY). Tests override the fields or the seams they exercise.
func newFakeDeps(t *testing.T, units []orchestrate.Unit, plan orchestrate.Plan, checks []preflight.CheckResult) *fakeDeps {
	t.Helper()
	f := &fakeDeps{
		activeState:        "active",
		priorConfigExists:  true,
		priorUnits:         map[string]string{},
		downloaded:         true,
		embedPresent:       true,
		memoryProofStatus:  preflight.StatusPass,
		searxngProofStatus: preflight.StatusPass,
		coderPresent:       true,
		agentProofStatus:   preflight.StatusPass,
		agentCatOK:         true,
		// A single coder entry whose id matches the default pick's Coder.Model, so an
		// agent-on test resolves a shard without extra setup.
		agentCat: catalog.Catalog{Models: []catalog.Model{
			{ID: "qwen3-coder-30b-a3b", Role: "coder", Shards: []catalog.Shard{
				{Filename: "qwen3-coder-30b-a3b.gguf", SizeBytes: 4096},
			}},
		}},
	}
	d := &Deps{
		Probe: func() detect.HostProfile { return detect.HostProfile{} },
		Pick: func(detect.HostProfile, recommend.Overrides) recommend.Recommendation {
			return recommend.Recommendation{
				Model: "qwen2.5-0.5b", Quant: "Q4_K_M", ContextLen: 4096, Backend: "rocm",
				WeightBytes:  1 << 30,
				KVCacheBytes: 1 << 28, HeadroomBytes: 1 << 28, UsableEnvelopeBytes: 8 << 30,
				Fits:  true,
				Coder: recommend.CoderFit{Model: "qwen3-coder-30b-a3b", Quant: "Q4_K_M", AgentCtx: 65536, Fits: true, Residency: "swap"},
			}
		},
		ModelFile: func(recommend.Recommendation) (string, error) { return "qwen2.5-0.5b.gguf", nil },
		ModelsDir: func() string { return t.TempDir() },
		RunChecks: func(detect.HostProfile, preflight.ResourceReq) []preflight.CheckResult { return checks },
		Render: func(in orchestrate.RenderInput) ([]orchestrate.Unit, error) {
			f.renderedInput = in
			f.renderedInputSet = true
			return units, nil
		},
		Reconcile:     func([]orchestrate.Unit, string) (orchestrate.Plan, error) { return plan, nil },
		UnitDir:       func() (string, error) { return t.TempDir(), nil },
		ResidentUnits: func(config.VillaConfig) ([]orchestrate.ResidentUnit, error) { return nil, nil },
		HostVillaPath: func() string { return "/opt/villa/bin/villa" },
		CodingRender: func(config.VillaConfig) (string, *inference.CodingModeSpec, error) {
			return "qwen3-coder-30b-a3b.gguf", nil, nil
		},
		Username:    func() string { return "tester" },
		Endpoint:    func() string { return "http://127.0.0.1:8080" },
		Interactive: func() bool { return false },
		Consent:     func(string) bool { return false },
		// The flag path by default: stdoutIsTTY=false keeps the wizard gate off.
		StdoutIsTTY: func() bool { return false },
		Wizard: func(context.Context, WizardInput) (WizardResult, error) {
			f.wizardCalls++
			return WizardResult{}, nil
		},
	}
	d.ModelDownloaded = func(recommend.Recommendation) bool { return f.downloaded }
	d.EnsureModel = func(recommend.Recommendation) error {
		f.pullCalls++
		f.callOrder = append(f.callOrder, "ensureModel")
		return nil
	}
	d.SaveConfig = func(c config.VillaConfig) error { f.saveCalls++; f.savedCfg = c; return nil }
	d.WriteUnits = func(orchestrate.Plan, string) error { f.writeCalls++; return nil }
	d.DaemonReload = func() error { f.reloadCalls++; return nil }
	d.Start = func(service string) error {
		f.startCalls++
		f.startOrder = append(f.startOrder, service)
		f.callOrder = append(f.callOrder, "start:"+service)
		return nil
	}
	d.IsActive = func(string) (string, error) { return f.activeState, nil }
	d.Stop = func(service string) error {
		f.stopOrder = append(f.stopOrder, service)
		f.callOrder = append(f.callOrder, "stop:"+service)
		return f.stopErr
	}
	d.ReadUnit = func(_, name string) (string, bool) {
		text, ok := f.priorUnits[name]
		return text, ok
	}
	d.WriteUnit = func(_, name, _ string) error {
		f.callOrder = append(f.callOrder, "writeUnit:"+name)
		return nil
	}
	d.RemoveUnit = func(_, name string) error {
		f.removedUnits = append(f.removedUnits, name)
		f.callOrder = append(f.callOrder, "removeUnit:"+name)
		return f.removeUnitErr
	}
	d.ConfigExists = func() bool { return f.priorConfigExists }
	d.RemoveConfig = func() error {
		f.configRemoved = true
		f.callOrder = append(f.callOrder, "removeConfig")
		return nil
	}
	d.EnableLinger = func(string) error { f.lingerCalls++; return nil }
	d.Setsebool = func() error { f.seboolCalls++; return nil }
	d.UserUnitDir = func() (string, error) { return t.TempDir(), nil }
	d.WriteDashboardUnit = func(_ string, binaryPath string) error {
		f.dashWriteCalls++
		f.dashBinaryPath = binaryPath
		f.callOrder = append(f.callOrder, "dashWrite")
		return nil
	}
	d.ResolveBinaryPath = func() (string, error) { return "/opt/villa/bin/villa", nil }
	d.Enable = func(service string) error {
		f.dashEnableCalls++
		f.dashEnabled = append(f.dashEnabled, service)
		f.callOrder = append(f.callOrder, "enable:"+service)
		return nil
	}
	d.ReadDashboardUnit = func(string) ([]byte, error) {
		if f.diskUnit == nil {
			return nil, os.ErrNotExist
		}
		return f.diskUnit, nil
	}
	d.PollReady = func(context.Context, string) Proof {
		f.pollCalls++
		return Proof{Status: preflight.StatusPass, Detail: "ready"}
	}
	// The subsystem gates are PART of the persisted config, which is how production
	// reads them, so a test can never describe a world the flow could not observe.
	d.LoadConfig = func() (config.VillaConfig, error) {
		if f.configLoadErr != nil {
			return config.VillaConfig{}, f.configLoadErr
		}
		cfg := config.DefaultVillaConfig()
		if f.persistedConfig != nil {
			cfg = *f.persistedConfig
		}
		cfg.MemoryEnabled = f.memoryEnabled
		cfg.WebSearchEnabled = f.webSearchEnabled
		cfg.AgentEnabled = f.agentEnabled
		return cfg, nil
	}
	d.EmbedModelPresent = func(string) bool {
		f.embedPresentCalls++
		return f.embedPresent
	}
	d.EnsureEmbedModel = func(string) error {
		f.embedEnsureCalls++
		f.callOrder = append(f.callOrder, "ensureEmbedModel")
		return nil
	}
	d.ProveMemory = func(_ context.Context, cfg config.VillaConfig) Proof {
		f.memoryProofCalls++
		f.memoryProofCfg = cfg
		f.callOrder = append(f.callOrder, "memoryProof")
		return Proof{Status: f.memoryProofStatus, Detail: f.memoryProofDetail}
	}
	d.WriteSearxngSettings = func(string, string) error {
		f.searxngSettingsCalls++
		f.callOrder = append(f.callOrder, "writeSearxngSettings")
		return nil
	}
	d.WriteSearxngSecretEnv = func(string, string) error {
		f.searxngSecretEnvCalls++
		f.callOrder = append(f.callOrder, "writeSearxngSecretEnv")
		return nil
	}
	d.ProveSearch = func(context.Context) Proof {
		f.searxngProofCalls++
		f.callOrder = append(f.callOrder, "searxngProof")
		return Proof{Status: f.searxngProofStatus, Detail: f.searxngProofDetail}
	}
	d.WriteWebsafeSecretEnv = func(string, string) error {
		f.websafeSecretEnvCalls++
		f.callOrder = append(f.callOrder, "writeWebsafeSecretEnv")
		return nil
	}
	d.AgentCatalog = func() (catalog.Catalog, bool) { return f.agentCat, f.agentCatOK }
	d.CoderModelPresent = func(string, catalog.Shard) bool {
		f.coderPresentCalls++
		return f.coderPresent
	}
	d.EnsureCoderModel = func(string, catalog.Shard) error {
		f.coderEnsureCalls++
		f.callOrder = append(f.callOrder, "ensureCoderModel")
		return nil
	}
	d.InstallAgentBinary = func(context.Context) (string, error) {
		f.binaryInstallCalls++
		f.callOrder = append(f.callOrder, "installAgentBinary")
		return "/tmp/villa/bin/crush", nil
	}
	d.RenderCrushConfig = func(cfg config.VillaConfig) error {
		f.renderCrushCalls++
		f.renderedAgentEnabled = cfg.AgentEnabled
		f.callOrder = append(f.callOrder, "renderCrushConfig")
		return nil
	}
	d.ProveAgent = func(context.Context) Proof {
		f.agentProofCalls++
		f.callOrder = append(f.callOrder, "agentProof")
		return Proof{Status: f.agentProofStatus, Detail: f.agentProofDetail}
	}
	d.RunAgentChecks = func(detect.HostProfile, recommend.Recommendation) []preflight.CheckResult {
		f.agentChecksCalls++
		f.callOrder = append(f.callOrder, "runAgentChecks")
		return f.agentChecks
	}
	f.Deps = d
	return f
}

// run drives the flow with narration captured, returning the exit code and the
// two streams. Most tests want exactly this; runResult returns the Result too.
func (f *fakeDeps) run(opts Opts) (int, *bytes.Buffer, *bytes.Buffer) {
	res, out, errOut := f.runResult(opts)
	return res.Outcome.ExitCode(), out, errOut
}

func (f *fakeDeps) runResult(opts Opts) (Result, *bytes.Buffer, *bytes.Buffer) {
	var out, errOut bytes.Buffer
	d := *f.Deps
	d.Emit = func(l Line) {
		if l.Stderr {
			errOut.WriteString(l.Text)
			return
		}
		out.WriteString(l.Text)
	}
	return Run(context.Background(), d, opts), &out, &errOut
}

func passChecks() []preflight.CheckResult {
	return []preflight.CheckResult{
		{ID: "PRE-01", Tier: preflight.TierBlock, Status: preflight.StatusPass},
		{ID: "PRE-05", Tier: preflight.TierBlock, Status: preflight.StatusPass},
	}
}

// mustRenderDashboardUnit renders the dashboard unit through the SAME pure renderer
// the flow compares with, so a test can pre-seed diskUnit with bytes that exactly
// match what install would write.
func mustRenderDashboardUnit(t *testing.T, binPath string) []byte {
	t.Helper()
	body, err := orchestrate.RenderDashboardUnit(binPath)
	if err != nil {
		t.Fatalf("render: %v", err)
	}
	return []byte(body)
}

// The service names the flow starts, as the tests name them.
var (
	installServiceName   = DefaultUnits().Inference
	openWebUIServiceName = DefaultUnits().ChatUI
	qdrantServiceName    = DefaultUnits().Qdrant
	embedServiceName     = DefaultUnits().Embed
	searxngServiceName   = DefaultUnits().Searxng
	websafeServiceName   = DefaultUnits().Websafe
)
