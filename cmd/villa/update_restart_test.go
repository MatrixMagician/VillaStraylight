package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/updateflow"
)

const (
	priorOpenWebUI = "[Container]\n# the pre-upgrade Open WebUI unit: villa.network only\n"
	priorQdrant    = "[Container]\n# qdrant on the prior pin\n"
	priorLlama     = "[Container]\n# villa-llama, stopped by the operator\n"
)

// memoryHost is the memory-on config the update tests run.
var memoryHost = config.VillaConfig{Model: "qwen3.5-0.8b", Quant: "Q4_K_M", Ctx: 4096, InferenceSecret: "s", MemoryEnabled: true}

// upgradedHost is a host running cfg whose unit dir holds what this binary renders,
// except three units an upgrade or a pin move rewrites: Open WebUI (running, as
// #347's first apply rewrites it onto villa-closed), Qdrant (running, memory's own)
// and villa-llama (changed, but stopped by the operator). Its systemd records every
// call and answers is-active from the running set.
func upgradedHost(t *testing.T, cfg config.VillaConfig) (updateflow.Deps, string, *[]string) {
	t.Helper()
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	running := map[string]bool{
		"villa-openwebui.service": true,
		"villa-qdrant.service":    true,
		"villa-embed.service":     true,
	}
	var calls []string
	sys := orchestrate.SystemdForTest(func(_ string, args ...string) (string, bool, bool) {
		calls = append(calls, strings.Join(args, " "))
		if len(args) == 3 && args[1] == "is-active" {
			if running[args[2]] {
				return "active\n", true, true
			}
			return "inactive\n", true, false
		}
		return "", true, true
	})
	stack := liveStackDeps()
	stack.DaemonReload = sys.DaemonReload
	stack.IsActive = sys.IsActive
	stack.Stop = sys.Stop

	dir, err := stack.UnitDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
	plan, err := stackapply.Plan(stack, cfg)
	if err != nil {
		t.Fatalf("render the fixture stack: %v", err)
	}
	for _, u := range plan.Changed {
		writeUnitFile(t, filepath.Join(dir, u.Name), u.Text)
	}
	writeUnitFile(t, filepath.Join(dir, "villa-openwebui.container"), priorOpenWebUI)
	writeUnitFile(t, filepath.Join(dir, "villa-qdrant.container"), priorQdrant)
	writeUnitFile(t, filepath.Join(dir, "villa-llama.container"), priorLlama)

	d := updateFlowDeps(sys, stack, cfg, nil)
	pass := func(context.Context, subsystem.Kind) updateflow.Proof {
		return updateflow.Proof{Status: updateflow.ProofPass}
	}
	d.ProveCurrent, d.ProveNew, d.ProveRestored = pass, pass, pass
	d.Pull = nil
	d.SnapshotData = func(context.Context, subsystem.Kind) (pinstate.DataSnapshot, error) {
		return pinstate.DataSnapshot{Volume: "villa-qdrant", Path: "/snapshot.tar"}, nil
	}
	d.RestoreData = func(context.Context, subsystem.Kind, pinstate.DataSnapshot) error { return nil }
	d.Commit = func(subsystem.Kind, map[string]string, pinstate.Previous) error { return nil }
	calls = nil
	return d, dir, &calls
}

func writeUnitFile(t *testing.T, path, text string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(text), 0o600); err != nil {
		t.Fatal(err)
	}
}

func memoryTarget(cfg config.VillaConfig) updateflow.Target {
	pins := map[string]string{}
	for _, r := range resolverFor(pinstate.State{}).For(subsystem.Memory, cfg) {
		pins[string(r.Component)] = r.Current.Ref
	}
	return updateflow.Target{Subsystem: subsystem.Memory, Pins: pins}
}

func countCalls(calls []string, call string) int {
	n := 0
	for _, c := range calls {
		if c == call {
			n++
		}
	}
	return n
}

// TestAnUpdateRestartsEveryChangedRunningUnit (#354): an update's stack apply
// writes every changed unit, not only its subsystem's. A memory update on a host
// whose Open WebUI unit the apply rewrites must restart Open WebUI too, or chat
// keeps running on the old network while memory moves to villa-closed, and the
// memory proof passes over the split. A changed unit whose service the operator
// stopped stays stopped (the Transact rule, ADR-0015).
func TestAnUpdateRestartsEveryChangedRunningUnit(t *testing.T) {
	d, dir, calls := upgradedHost(t, memoryHost)

	res := updateflow.Run(t.Context(), d, []updateflow.Target{memoryTarget(memoryHost)})
	if got := res.Subsystems[0]; got.Outcome != updateflow.Committed {
		t.Fatalf("outcome %s (step %q, err %v), want committed", got.Outcome, got.FailedStep, got.Err)
	}
	if n := countCalls(*calls, "--user restart villa-openwebui.service"); n != 1 {
		t.Errorf("the update restarted villa-openwebui.service %d times, want once: its unit was rewritten while it ran\n%s",
			n, strings.Join(*calls, "\n"))
	}
	for _, verb := range []string{"restart", "start"} {
		if n := countCalls(*calls, "--user "+verb+" villa-llama.service"); n != 0 {
			t.Errorf("the update ran %s villa-llama.service, a service the operator stopped", verb)
		}
	}
	if got := readUnitFile(t, filepath.Join(dir, "villa-openwebui.container")); got == priorOpenWebUI {
		t.Error("the apply did not rewrite villa-openwebui.container; the fixture does not exercise #354")
	}
}

// TestAFailedUpdateRestoresAndRestartsEveryUnitItChanged (#354): the rollback puts
// back every unit the apply rewrote, Open WebUI's included, and restarts what the
// update restarted, so a rolled-back update leaves chat and memory on the networks
// they were proven on. The stopped villa-llama gets its bytes back and stays stopped.
func TestAFailedUpdateRestoresAndRestartsEveryUnitItChanged(t *testing.T) {
	d, dir, calls := upgradedHost(t, memoryHost)
	d.ProveNew = func(context.Context, subsystem.Kind) updateflow.Proof {
		return updateflow.Proof{Status: updateflow.ProofFail, Detail: "memory did not answer"}
	}

	res := updateflow.Run(t.Context(), d, []updateflow.Target{memoryTarget(memoryHost)})
	got := res.Subsystems[0]
	if got.Outcome != updateflow.RolledBackFail || got.RollbackIncomplete {
		t.Fatalf("outcome %s incomplete=%v (err %v), want a clean rolled-back-fail", got.Outcome, got.RollbackIncomplete, got.Err)
	}
	for name, want := range map[string]string{
		"villa-openwebui.container": priorOpenWebUI,
		"villa-qdrant.container":    priorQdrant,
		"villa-llama.container":     priorLlama,
	} {
		if got := readUnitFile(t, filepath.Join(dir, name)); got != want {
			t.Errorf("after the rollback %s = %q, want its captured bytes %q", name, got, want)
		}
	}
	if n := countCalls(*calls, "--user restart villa-openwebui.service"); n != 2 {
		t.Errorf("villa-openwebui.service restarted %d times, want twice (the update, then the rollback)\n%s",
			n, strings.Join(*calls, "\n"))
	}
	for _, verb := range []string{"restart", "start"} {
		if n := countCalls(*calls, "--user "+verb+" villa-llama.service"); n != 0 {
			t.Errorf("the rollback ran %s villa-llama.service, a service the operator stopped", verb)
		}
	}
}

// TestAMemoryUpdateRestartsTheExtractorOnlyWhenItsGateIsOn: the services a memory
// update restarts whether or not their unit changed are the ones memory renders on
// this host (ADR-0033), so the extractor joins them with its gate and is never
// named without it.
func TestAMemoryUpdateRestartsTheExtractorOnlyWhenItsGateIsOn(t *testing.T) {
	for _, on := range []bool{false, true} {
		cfg := memoryHost
		cfg.Extractor = on
		d, _, calls := upgradedHost(t, cfg)

		updateflow.Run(t.Context(), d, []updateflow.Target{memoryTarget(cfg)})
		named := strings.Contains(strings.Join(*calls, "\n"), "villa-extract")
		restarted := countCalls(*calls, "--user restart villa-extract.service") == 1
		if on && !restarted {
			t.Errorf("with the gate on the update did not restart villa-extract.service once:\n%s", strings.Join(*calls, "\n"))
		}
		if !on && named {
			t.Errorf("with the gate off the update named villa-extract:\n%s", strings.Join(*calls, "\n"))
		}
	}
}

func readUnitFile(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}
