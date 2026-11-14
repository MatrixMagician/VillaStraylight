package install

import (
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// flow_toolserver_test.go drives Run through `villa install --tool-server` (ADR-0042): the
// refusal when there is nothing to publish, the gate and tools mode it persists, the
// bearers and files written before the chat unit starts, the starts gated on the rendered
// units, and the health wait that refuses and rolls back on a failure.

var (
	toolsWebServiceName    = DefaultUnits().ToolsWeb
	toolsRecallServiceName = DefaultUnits().ToolsRecall
)

// toolUnits is a realistic plan for the stack the flags describe: Render appends each tool
// unit when its derived gate is on, and the start guard demands it be in the plan.
func toolUnits(web, recall bool) ([]orchestrate.Unit, orchestrate.Plan) {
	unit := func(name string) orchestrate.Unit { return orchestrate.Unit{Name: name, Text: "[Container]\n"} }
	units := []orchestrate.Unit{unit("villa-llama.container")}
	if recall {
		units = append(units, unit(orchestrate.QdrantContainerUnitName()), unit(orchestrate.EmbedContainerUnitName()),
			unit(orchestrate.RerankContainerUnitName()), unit(orchestrate.ExtractContainerUnitName()))
	}
	if web {
		units = append(units, unit(orchestrate.SearXNGContainerUnitName()), unit(orchestrate.WebsafeContainerUnitName()))
	}
	if web {
		units = append(units, unit(orchestrate.ToolsWebContainerUnitName()))
	}
	if recall {
		units = append(units, unit(orchestrate.ToolsRecallContainerUnitName()))
	}
	return units, orchestrate.Plan{Changed: units}
}

func toolFake(t *testing.T, web, recall bool) *fakeDeps {
	units, plan := toolUnits(web, recall)
	f := newFakeDeps(t, units, plan, passChecks())
	f.webSearchEnabled, f.memoryEnabled = web, recall
	return f
}

// TestInstallToolServerRefusesWithNothingToPublish: with neither web search nor memory on
// (or asked for in this run) the tool server has no unit to render, so install refuses
// before any mutation and names the remedy for each.
func TestInstallToolServerRefusesWithNothingToPublish(t *testing.T) {
	f := toolFake(t, false, false)
	code, _, errOut := f.run(Opts{ToolServer: true})
	if code != exitBlocked {
		t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
	}
	msg := errOut.String()
	for _, want := range []string{"--tool-server", "--web-search", "memory_enabled"} {
		if !strings.Contains(msg, want) {
			t.Errorf("the refusal does not name %q:\n%s", want, msg)
		}
	}
	if f.saveCalls != 0 || f.writeCalls != 0 || f.startCalls != 0 || len(f.toolEnvNames) != 0 || f.scopeWrites != 0 {
		t.Errorf("a refused run mutated the host: save %d write %d start %d env %v scope %d",
			f.saveCalls, f.writeCalls, f.startCalls, f.toolEnvNames, f.scopeWrites)
	}
}

// TestInstallToolServerWithWebSearchInTheSameRun: --web-search --tool-server resolves both
// gates together, so the pair is not refused.
func TestInstallToolServerWithWebSearchInTheSameRun(t *testing.T) {
	units, plan := toolUnits(true, false)
	f := newFakeDeps(t, units, plan, passChecks())
	code, _, errOut := f.run(Opts{WebSearch: true, ToolServer: true})
	if code != exitPass {
		t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	if !f.savedCfg.ToolServer || !f.savedCfg.WebSearchEnabled {
		t.Errorf("saved config = %+v, want both gates", f.savedCfg)
	}
}

// TestInstallToolServerPersistsTheGateAndRaisesToolsMode: a model served without tool
// calling cannot use a tool, so the install raises tools mode with the gate, generates
// one bearer per unit, and the render sees all of it.
func TestInstallToolServerPersistsTheGateAndRaisesToolsMode(t *testing.T) {
	f := toolFake(t, true, true)
	code, out, errOut := f.run(Opts{ToolServer: true})
	if code != exitPass {
		t.Fatalf("exit = %d, want exitPass; stderr = %q", code, errOut.String())
	}
	saved := f.savedCfg
	if !saved.ToolServer || !saved.ToolsMode {
		t.Errorf("saved config: tool_server=%v tools_mode=%v, want both", saved.ToolServer, saved.ToolsMode)
	}
	if len(saved.ToolWebSecret) != 64 || len(saved.ToolRecallSecret) != 64 || saved.ToolWebSecret == saved.ToolRecallSecret {
		t.Errorf("bearers = %q / %q, want two distinct 64-hex values", saved.ToolWebSecret, saved.ToolRecallSecret)
	}
	if !f.renderedCfg.ToolServer || !f.renderedCfg.ToolsMode || f.renderedCfg.ToolWebSecret != saved.ToolWebSecret {
		t.Errorf("the render did not see the persisted gate and bearers: %+v", f.renderedCfg)
	}
	if !strings.Contains(out.String(), "tool server ready") {
		t.Errorf("a passing health wait must be narrated; stdout = %q", out.String())
	}
}

// TestInstallToolServerReusesAnExistingBearer: a re-install keeps the bearers it finds,
// so the chat unit's registered key and the unit's key stay a pair.
func TestInstallToolServerReusesAnExistingBearer(t *testing.T) {
	f := toolFake(t, true, true)
	f.persistedConfig = &config.VillaConfig{ToolServer: true, ToolWebSecret: "kept-web", ToolRecallSecret: "kept-recall"}
	if code, _, errOut := f.run(Opts{}); code != exitPass {
		t.Fatalf("exit = %d; stderr = %q", code, errOut.String())
	}
	if f.savedCfg.ToolWebSecret != "kept-web" || f.savedCfg.ToolRecallSecret != "kept-recall" {
		t.Errorf("bearers rotated: %q / %q", f.savedCfg.ToolWebSecret, f.savedCfg.ToolRecallSecret)
	}
}

// TestInstallToolServerWritesItsFilesBeforeTheChatStarts: the chat unit names
// openwebui.env, so the files and the scope land before any unit is started, and the two
// tool units start after the services they depend on and before the image server.
func TestInstallToolServerWritesItsFilesBeforeTheChatStarts(t *testing.T) {
	f := toolFake(t, true, true)
	if code, _, errOut := f.run(Opts{ToolServer: true}); code != exitPass {
		t.Fatalf("exit = %d; stderr = %q", code, errOut.String())
	}
	for _, want := range []string{"web.env", "recall.env", "openwebui.env"} {
		if !slices.Contains(f.toolEnvNames, want) {
			t.Errorf("env files written = %v, missing %s", f.toolEnvNames, want)
		}
	}
	if f.scopeWrites != 1 {
		t.Errorf("scope written %d times, want 1", f.scopeWrites)
	}
	chatStart := slices.Index(f.callOrder, "start:"+openWebUIServiceName)
	for _, c := range []string{"writeToolEnv:web.env", "writeToolEnv:recall.env", "writeToolEnv:openwebui.env", "writeRecallScope"} {
		if i := slices.Index(f.callOrder, c); i < 0 || i > chatStart {
			t.Errorf("%s must precede the chat unit's start; callOrder = %v", c, f.callOrder)
		}
	}
	for _, svc := range []string{embedServiceName, searxngServiceName, websafeServiceName} {
		for _, tool := range []string{toolsWebServiceName, toolsRecallServiceName} {
			if slices.Index(f.startOrder, tool) < slices.Index(f.startOrder, svc) {
				t.Errorf("%s started before %s; startOrder = %v", tool, svc, f.startOrder)
			}
		}
	}
	if i, j := slices.Index(f.startOrder, toolsWebServiceName), slices.Index(f.startOrder, toolsRecallServiceName); i < 0 || j != i+1 {
		t.Errorf("tool units start web then recall, adjacent; startOrder = %v", f.startOrder)
	}
	if f.toolProofCalls != 1 {
		t.Errorf("health wait ran %d times, want once", f.toolProofCalls)
	}
}

// TestInstallToolServerStartsOnlyTheUnitsThatRender: web search alone renders the web unit
// and no scope; memory alone renders the recall unit and writes the scope.
func TestInstallToolServerStartsOnlyTheUnitsThatRender(t *testing.T) {
	web := toolFake(t, true, false)
	if code, _, errOut := web.run(Opts{ToolServer: true}); code != exitPass {
		t.Fatalf("web: exit = %d; stderr = %q", code, errOut.String())
	}
	if !slices.Contains(web.startOrder, toolsWebServiceName) || slices.Contains(web.startOrder, toolsRecallServiceName) {
		t.Errorf("web only: startOrder = %v", web.startOrder)
	}
	if web.scopeWrites != 0 || slices.Contains(web.toolEnvNames, "recall.env") {
		t.Errorf("web only wrote recall files: scope %d env %v", web.scopeWrites, web.toolEnvNames)
	}

	rec := toolFake(t, false, true)
	if code, _, errOut := rec.run(Opts{ToolServer: true}); code != exitPass {
		t.Fatalf("recall: exit = %d; stderr = %q", code, errOut.String())
	}
	if !slices.Contains(rec.startOrder, toolsRecallServiceName) || slices.Contains(rec.startOrder, toolsWebServiceName) {
		t.Errorf("recall only: startOrder = %v", rec.startOrder)
	}
	if rec.scopeWrites != 1 || slices.Contains(rec.toolEnvNames, "web.env") {
		t.Errorf("recall only: scope %d env %v", rec.scopeWrites, rec.toolEnvNames)
	}
}

// TestInstallToolServerStartIsGatedOnItsUnit: a plan with no tool unit fails closed
// instead of starting a service systemd has never seen, and the run rolls back.
func TestInstallToolServerStartIsGatedOnItsUnit(t *testing.T) {
	units, plan := toolUnits(true, false)
	plan.Changed = slices.DeleteFunc(slices.Clone(plan.Changed), func(u orchestrate.Unit) bool {
		return u.Name == orchestrate.ToolsWebContainerUnitName()
	})
	f := newFakeDeps(t, units, plan, passChecks())
	f.webSearchEnabled = true
	code, _, errOut := f.run(Opts{ToolServer: true})
	if code != exitBlocked {
		t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), orchestrate.ToolsWebContainerUnitName()) {
		t.Errorf("the refusal must name the absent unit; stderr = %q", errOut.String())
	}
	if slices.Contains(f.startOrder, toolsWebServiceName) {
		t.Error("a unit absent from the plan was started")
	}
}

// TestInstallToolServerHealthFailureRollsBack: a tool unit that never answers /healthz is a
// refusal that stops what this run started, never a green install.
func TestInstallToolServerHealthFailureRollsBack(t *testing.T) {
	f := toolFake(t, true, true)
	f.toolProofStatus = preflight.StatusFail
	f.toolProofDetail = "villa-tools-web did not answer /healthz"
	code, _, errOut := f.run(Opts{ToolServer: true})
	if code != exitBlocked {
		t.Fatalf("exit = %d, want exitBlocked; stderr = %q", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "tool server not ready") || !strings.Contains(errOut.String(), "/healthz") {
		t.Errorf("the refusal must say the tool server is not ready and carry the detail; stderr = %q", errOut.String())
	}
	if !slices.Contains(f.stopOrder, toolsWebServiceName) {
		t.Errorf("rollback must stop what this run started; stopOrder = %v", f.stopOrder)
	}
}

// TestInstallToolServerOffTouchesNothing: with the gate off a run writes no tool file,
// starts no tool unit and waits on no health.
func TestInstallToolServerOffTouchesNothing(t *testing.T) {
	f := toolFake(t, true, true)
	if code, _, errOut := f.run(Opts{}); code != exitPass {
		t.Fatalf("exit = %d; stderr = %q", code, errOut.String())
	}
	if len(f.toolEnvNames) != 0 || f.scopeWrites != 0 || f.toolProofCalls != 0 {
		t.Errorf("a gate-off install touched the tool server: env %v scope %d proof %d", f.toolEnvNames, f.scopeWrites, f.toolProofCalls)
	}
	for _, svc := range []string{toolsWebServiceName, toolsRecallServiceName} {
		if slices.Contains(f.startOrder, svc) {
			t.Errorf("%s started with the gate off", svc)
		}
	}
	if f.savedCfg.ToolWebSecret != "" || f.savedCfg.ToolRecallSecret != "" || f.savedCfg.ToolServer {
		t.Errorf("a gate-off install persisted tool fields: %+v", f.savedCfg)
	}
}

// TestToolServerGatesDeriveFromTheOthers: the two unit gates need what they publish.
func TestToolServerGatesDeriveFromTheOthers(t *testing.T) {
	for _, tc := range []struct {
		name        string
		cfg         config.VillaConfig
		opts        Opts
		web, recall bool
		refusal     bool
	}{
		{"nothing", config.VillaConfig{}, Opts{ToolServer: true}, false, false, true},
		{"web by flag", config.VillaConfig{}, Opts{ToolServer: true, WebSearch: true}, true, false, false},
		{"memory persisted", config.VillaConfig{MemoryEnabled: true}, Opts{ToolServer: true}, false, true, false},
		{"both", config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true}, Opts{ToolServer: true}, true, true, false},
		{"persisted gate", config.VillaConfig{ToolServer: true, WebSearchEnabled: true}, Opts{}, true, false, false},
		{"gate off", config.VillaConfig{MemoryEnabled: true, WebSearchEnabled: true}, Opts{}, false, false, false},
	} {
		g := ResolveGates(tc.cfg, tc.opts, recommend.Recommendation{})
		if g.ToolWeb() != tc.web || g.ToolRecall() != tc.recall {
			t.Errorf("%s: ToolWeb=%v ToolRecall=%v, want %v/%v", tc.name, g.ToolWeb(), g.ToolRecall(), tc.web, tc.recall)
		}
		if (ToolServerRefusal(g) != "") != tc.refusal {
			t.Errorf("%s: refusal = %q, want refusal=%v", tc.name, ToolServerRefusal(g), tc.refusal)
		}
	}
}

// TestPersistRaisesToolsModeForTheToolServerAndNeverLowersIt.
func TestPersistRaisesToolsModeForTheToolServerAndNeverLowersIt(t *testing.T) {
	on := ResolveGates(config.VillaConfig{WebSearchEnabled: true}, Opts{ToolServer: true}, recommend.Recommendation{}).Persist(config.VillaConfig{})
	if !on.ToolServer || !on.ToolsMode {
		t.Errorf("Persist = %+v, want tool_server and tools_mode", on)
	}
	off := ResolveGates(config.VillaConfig{}, Opts{}, recommend.Recommendation{}).Persist(config.VillaConfig{ToolsMode: true})
	if off.ToolServer || !off.ToolsMode {
		t.Errorf("a gate-off Persist must not lower tools mode an operator entered: %+v", off)
	}
}
