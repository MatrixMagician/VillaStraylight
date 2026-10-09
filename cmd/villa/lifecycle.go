package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
)

// lifecycle.go is the shared seam + helpers for the day-to-day lifecycle verbs
// (`up`/`down`/`restart`/`logs`), and home of liveStackDeps, the one live adapter
// every unit-writing verb applies the stack through (internal/stackapply,
// ADR-0013). Editing config.toml and re-running `up`/`restart` converges exactly
// the changed units. Every host-touching action is an injectable
// field on lifecycleDeps so lifecycle_test.go drives the whole flow with no live
// podman/systemd/journald host; runX RETURNS the exit code (0/2/1) — the cobra
// RunE wrapper calls os.Exit — mirroring runInstall/runModelPull.
//
// Service names flow into fixed-arg systemctl/journalctl calls only AFTER being
// validated against the known unit set: an unknown service is refused
// before any seam fires, so a CLI arg can never be shell-injected or target an
// arbitrary unit.

// lifecycleDeps are the injectable seams the lifecycle verbs drive. Defaults wire
// the real host (liveLifecycleDeps); lifecycle_test.go replaces them with stubs.
type lifecycleDeps struct {
	loadConfig func() (config.VillaConfig, error)
	// stack renders, writes and reloads the unit files (ADR-0013); the verbs only
	// choose which services to start or restart.
	stack stackapply.Deps

	start    func(service string) error
	stop     func(service string) error
	restart  func(service string) error
	isActive func(service string) (string, error)

	journalText   func(service string) (string, bool)
	followJournal func(service string) error
}

// liveStackDeps is the ONE live adapter every unit-writing verb applies the stack
// through (ADR-0013): the catalog, the pinned render (pinresolve), the Quadlet unit
// dir, the unit writer, systemd, the inference-secret writers (GHSA-qxg9,
// ADR-0011), and the crush.json key heal (ADR-0019).
func liveStackDeps() stackapply.Deps {
	sys := orchestrate.NewSystemd()
	return stackapply.Deps{
		Catalog: func() (catalog.Catalog, error) {
			cat, _, err := catalog.Load(modelCatalogPath)
			return cat, err
		},
		ModelsDir:               modelsDir,
		HostVillaPath:           hostVillaPath,
		Render:                  livePinnedRender,
		UnitDir:                 quadletUnitDir,
		Reconcile:               orchestrate.Reconcile,
		WriteUnits:              orchestrate.WriteUnits,
		RemoveUnits:             orchestrate.RemoveUnits,
		DaemonReload:            sys.DaemonReload,
		IsActive:                sys.IsActive,
		Stop:                    sys.Stop,
		SaveConfig:              config.SaveVilla,
		WriteInferenceSecretEnv: orchestrate.WriteInferenceSecretEnv,
		HealAgentConfig:         liveHealAgentConfig,
	}
}

// renderStack loads config, renders the units, and resolves the unit dir: the
// service-set source down, logs and uninstall validate against. It writes nothing.
func (d *lifecycleDeps) renderStack() (units []orchestrate.Unit, unitDir string, err error) {
	cfg, err := d.loadConfig()
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}
	dir, err := d.stack.UnitDir()
	if err != nil {
		return nil, "", fmt.Errorf("resolve unit dir: %w", err)
	}
	units, err = stackapply.Render(d.stack, cfg)
	if err != nil {
		return nil, "", err
	}
	return units, dir, nil
}

// uninstallUnits is the rendered stack plus the registry units on disk it no longer
// renders (ADR-0035), appended last so uninstall's reversed stop order stops them
// first: everything villa declared is torn down, gated on or not.
func (d *lifecycleDeps) uninstallUnits() ([]orchestrate.Unit, string, error) {
	units, dir, err := d.renderStack()
	if err != nil {
		return nil, "", err
	}
	plan, err := d.stack.Reconcile(units, dir)
	if err != nil {
		return nil, "", fmt.Errorf("reconcile: %w", err)
	}
	return append(units, plan.Removed...), dir, nil
}

// hostVillaPath returns the host filesystem path to the running villa binary, threaded into
// orchestrate.RenderInput so the villa-websafe unit bind-mounts THIS binary read-only and
// exec's `villa websafe-serve` inside the container (Phase-31 Area 1). It resolves
// os.Executable() (the canonical absolute path of the running binary). On the unlikely error
// it returns "" — harmless when web search is off (HostVillaPath is unused), and the
// install-flow gate (WebsafeContainerUnitName presence + an empty-path render) surfaces a
// web-search-on misconfiguration rather than silently shell-injecting. Never shell-interpolated.
func hostVillaPath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	return exe
}

// serviceUnits returns the systemd service names a rendered stack produces. Only
// .container units map to a service (Quadlet villa-llama.container →
// villa-llama.service); .network/.volume units are not services. This is the
// authoritative known-service set every verb validates an arg against.
func serviceUnits(units []orchestrate.Unit) []string {
	var svcs []string
	for _, u := range units {
		if name, ok := strings.CutSuffix(u.Name, ".container"); ok {
			svcs = append(svcs, name+".service")
		}
	}
	return svcs
}

// managedServices returns the FULL managed-service set for the lifecycle verbs: the
// .container-derived services (serviceUnits) PLUS the native control-dashboard
// service (Plan 05-05). serviceUnits only covers Quadlet.container units, so
// the dashboard — a native systemd --user .service with no .container — must be
// appended separately to make it a first-class up/down/restart target. The dashboard
// is appended LAST so a whole-stack `up` starts it after the containers and a
// whole-stack `down`/uninstall stop order can reverse cleanly.
func managedServices(units []orchestrate.Unit) []string {
	return append(serviceUnits(units), orchestrate.DashboardServiceName)
}

// resolveTargets validates the optional [service] arg against the known service
// set and returns the services to operate on: the named one (if valid) or the
// whole stack (when no arg). An unknown name is refused with a clear error BEFORE
// any seam fires (zero side effects) so a CLI arg cannot target an arbitrary unit.
func resolveTargets(errOut io.Writer, args []string, services []string) ([]string, bool) {
	if len(args) == 0 {
		return services, true
	}
	want := args[0]
	// Accept either the bare base name (villa-llama) or the full unit
	// (villa-llama.service) — normalize to the service form.
	if !strings.HasSuffix(want, ".service") {
		want += ".service"
	}
	for _, s := range services {
		if s == want {
			return []string{s}, true
		}
	}
	fmt.Fprintf(errOut, "unknown service %q — known services: %s\n", args[0], strings.Join(services, ", "))
	return nil, false
}

// applyStack applies cfg to the unit files and narrates the write and the removal:
// the apply up and restart share. It returns what the apply did so the caller decides
// between a true no-op and a (re)start. printDryRun handles --dry-run, which never
// reaches here.
func (d *lifecycleDeps) applyStack(out io.Writer, cfg config.VillaConfig) (stackapply.Applied, error) {
	applied, err := stackapply.Apply(d.stack, cfg)
	if len(applied.Changed) > 0 {
		fmt.Fprintf(out, "wrote %d changed unit(s)\n", len(applied.Changed))
	}
	if len(applied.Removed) > 0 {
		fmt.Fprintf(out, "removed %d unit(s) no longer rendered: %s\n", len(applied.Removed), unitNameList(applied.Removed))
	}
	return applied, err
}

// unitNameList joins unit names for a narration line.
func unitNameList(units []orchestrate.Unit) string {
	names := make([]string, 0, len(units))
	for _, u := range units {
		names = append(names, u.Name)
	}
	return strings.Join(names, ", ")
}

// printDryRun prints the changed unit text and the units it would remove (or a
// no-change note) and writes nothing — the shared --dry-run body for up.
func printDryRun(out io.Writer, plan orchestrate.Plan) int {
	if len(plan.Changed) == 0 && len(plan.Removed) == 0 {
		fmt.Fprintf(out, "dry-run: no changes — units already match config\n")
		return exitPass
	}
	for _, u := range plan.Changed {
		fmt.Fprintf(out, "# %s\n%s\n", u.Name, u.Text)
	}
	for _, u := range plan.Removed {
		fmt.Fprintf(out, "dry-run: %s would be stopped and removed\n", u.Name)
	}
	fmt.Fprintf(out, "dry-run: %d unit(s) would be written, %d removed (nothing written)\n", len(plan.Changed), len(plan.Removed))
	return exitPass
}

// liveLifecycleDeps wires lifecycleDeps to the real host: config.LoadVilla, the
// live stack adapter, and the systemd seam. It is replaced wholesale by stubs in
// lifecycle_test.go.
func liveLifecycleDeps() *lifecycleDeps {
	sys := orchestrate.NewSystemd()
	return &lifecycleDeps{
		loadConfig: config.LoadVilla,
		stack:      liveStackDeps(),
		start:      sys.Start,
		stop:       sys.Stop,
		restart:    sys.Restart,
		isActive:   sys.IsActive,

		journalText:   sys.JournalText,
		followJournal: followJournalLive,
	}
}

// followJournalLive streams a service's user journal with `journalctl --user -u
// <service> -f` as a FIXED-ARG exec (never a shell). The service name is
// validated by the caller against the known unit set before this is reached. The
// stream is bounded only by the user's interactive Ctrl-C (a follow is explicit),
// so it wires stdout/stderr straight through rather than buffering.
func followJournalLive(service string) error {
	if _, err := exec.LookPath("journalctl"); err != nil {
		return orchestrate.ErrToolNotFound{Tool: "journalctl"}
	}
	c := exec.Command("journalctl", "--user", "-u", service, "-f") // fixed args; no shell
	c.Stdout = os.Stdout
	c.Stderr = os.Stderr
	return c.Run()
}

// liveModelFile resolves the on-disk GGUF filename for the config'd model through
// the catalog (never as a path). It mirrors the install.go modelFile closure so
// the lifecycle verbs render the same Exec= model path install wrote. A catalog load
// failure or an unknown model id is a hard error — fabricating
// "<model>.gguf" would render a container whose -m points at a non-existent file
// that fails only at runtime after install reports success, so block here instead.
func liveModelFile(cfg config.VillaConfig) (string, error) {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return "", fmt.Errorf("load model catalog: %w", err)
	}
	m, ok := cat.FindByID(cfg.Model)
	if !ok {
		return "", fmt.Errorf("model %q is not in the catalog — cannot resolve its weight file", cfg.Model)
	}
	return m.PrimaryFile(), nil
}
