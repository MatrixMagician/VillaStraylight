package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// lifecycle.go is the shared seam + helpers for the day-to-day lifecycle verbs
// (`up`/`down`/`restart`/`logs`). They reuse the Plan-01 orchestrate core
// (Render→Reconcile→WriteUnits→Systemd) and the Plan-02 install reconcile pattern,
// so editing config.toml and re-running `up`/`restart` converges exactly the
// changed units (D-06/D-07/CLI-05). Every host-touching action is an injectable
// field on lifecycleDeps so lifecycle_test.go drives the whole flow with no live
// podman/systemd/journald host; runX RETURNS the exit code (0/2/1) — the cobra
// RunE wrapper calls os.Exit — mirroring runInstall/runModelPull.
//
// Service names flow into fixed-arg systemctl/journalctl calls only AFTER being
// validated against the known unit set (T-03-11): an unknown service is refused
// before any seam fires, so a CLI arg can never be shell-injected or target an
// arbitrary unit.

// lifecycleDeps are the injectable seams the lifecycle verbs drive. Defaults wire
// the real host (liveLifecycleDeps); lifecycle_test.go replaces them with stubs.
type lifecycleDeps struct {
	loadConfig func() (config.VillaConfig, error)
	modelFile  func(config.VillaConfig) (string, error)
	modelsDir  func() string
	render     func(orchestrate.RenderInput) ([]orchestrate.Unit, error)
	reconcile  func([]orchestrate.Unit, string) (orchestrate.Plan, error)
	writeUnits func(orchestrate.Plan, string) error
	unitDir    func() (string, error)

	daemonReload func() error
	start        func(service string) error
	stop         func(service string) error
	restart      func(service string) error

	journalText   func(service string) (string, bool)
	followJournal func(service string) error
}

// renderStack loads config, renders the units, and resolves the unit dir. It is
// the shared front half of up/restart (and the service-set source for all verbs).
func (d *lifecycleDeps) renderStack() (units []orchestrate.Unit, unitDir string, err error) {
	cfg, err := d.loadConfig()
	if err != nil {
		return nil, "", fmt.Errorf("load config: %w", err)
	}
	dir, err := d.unitDir()
	if err != nil {
		return nil, "", fmt.Errorf("resolve unit dir: %w", err)
	}
	modelFile, err := d.modelFile(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("resolve model file: %w", err)
	}
	units, err = d.render(orchestrate.RenderInput{
		Backend:   inference.VulkanBackend(),
		Cfg:       cfg,
		ModelFile: modelFile,
		ModelsDir: d.modelsDir(),
	})
	if err != nil {
		return nil, "", fmt.Errorf("render: %w", err)
	}
	return units, dir, nil
}

// serviceUnits returns the systemd service names a rendered stack produces. Only
// .container units map to a service (Quadlet villa-llama.container →
// villa-llama.service); .network/.volume units are not services. This is the
// authoritative known-service set every verb validates an arg against (T-03-11).
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
// .service (Plan 05-05 / D-04). serviceUnits only covers Quadlet .container units, so
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

// applyReconcile writes the changed units and daemon-reloads (only when something
// changed). It returns whether anything changed so the caller can decide between a
// true no-op and a (re)start. printDryRun handles --dry-run separately.
func (d *lifecycleDeps) applyReconcile(out io.Writer, plan orchestrate.Plan, unitDir string) (changed bool, err error) {
	if len(plan.Changed) == 0 {
		return false, nil
	}
	if err := d.writeUnits(plan, unitDir); err != nil {
		return false, fmt.Errorf("write units: %w", err)
	}
	fmt.Fprintf(out, "wrote %d changed unit(s) to %s\n", len(plan.Changed), unitDir)
	if err := d.daemonReload(); err != nil {
		return false, fmt.Errorf("daemon-reload: %w", err)
	}
	return true, nil
}

// printDryRun prints the changed unit text (or a no-change note) and writes
// nothing — the shared --dry-run body for up.
func printDryRun(out io.Writer, plan orchestrate.Plan) int {
	if len(plan.Changed) == 0 {
		fmt.Fprintf(out, "dry-run: no changes — units already match config\n")
		return exitPass
	}
	for _, u := range plan.Changed {
		fmt.Fprintf(out, "# %s\n%s\n", u.Name, u.Text)
	}
	fmt.Fprintf(out, "dry-run: %d unit(s) would be written (nothing written)\n", len(plan.Changed))
	return exitPass
}

// liveLifecycleDeps wires lifecycleDeps to the real host: config.LoadVilla, the
// orchestrate render/reconcile/write + systemd seam, and the catalog-resolved
// model file. It is replaced wholesale by stubs in lifecycle_test.go.
func liveLifecycleDeps() *lifecycleDeps {
	sys := orchestrate.NewSystemd()
	return &lifecycleDeps{
		loadConfig: config.LoadVilla,
		modelFile:  liveModelFile,
		modelsDir:  modelsDir,
		render:     orchestrate.Render,
		reconcile:  orchestrate.Reconcile,
		writeUnits: orchestrate.WriteUnits,
		unitDir:    quadletUnitDir,

		daemonReload: sys.DaemonReload,
		start:        sys.Start,
		stop:         sys.Stop,
		restart:      sys.Restart,

		journalText:   sys.JournalText,
		followJournal: followJournalLive,
	}
}

// followJournalLive streams a service's user journal with `journalctl --user -u
// <service> -f` as a FIXED-ARG exec (never a shell, T-03-11). The service name is
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
// failure or an unknown model id is a hard error (WR-08) — fabricating
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
	return primaryModelFile(m), nil
}
