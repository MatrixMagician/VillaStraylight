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
// changed units. Every host-touching action is an injectable
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
	modelFile  func(config.VillaConfig) (string, error)
	modelsDir  func() string
	render     func(orchestrate.RenderInput) ([]orchestrate.Unit, error)
	reconcile  func([]orchestrate.Unit, string) (orchestrate.Plan, error)
	writeUnits func(orchestrate.Plan, string) error
	unitDir    func() (string, error)

	// saveConfig and writeInferenceSecretEnv back ensureInferenceSecret (GHSA-qxg9,
	// ADR-0011): the upgrade-migration path for an existing install whose
	// config.toml predates the inference bearer.
	saveConfig              func(config.VillaConfig) error
	writeInferenceSecretEnv func(name, text string) error

	daemonReload func() error
	start        func(service string) error
	stop         func(service string) error
	restart      func(service string) error
	isActive     func(service string) (string, error)

	journalText   func(service string) (string, bool)
	followJournal func(service string) error
}

// ensureInferenceSecretWith is the GHSA-qxg9 (ADR-0011) migration core, shared by
// every caller that needs it run through its OWN load/save/write-env seams: an
// existing install whose config.toml predates the inference bearer has neither
// the field nor the 0600 env file the rendered units now reference via
// EnvironmentFile=, so the first write after upgrading must self-heal it BEFORE
// any unit is touched. It reuses an existing secret verbatim (never rotates it)
// and always (re)writes the env file, self-healing a manually deleted one.
func ensureInferenceSecretWith(loadConfig func() (config.VillaConfig, error), saveConfig func(config.VillaConfig) error, writeInferenceSecretEnv func(name, text string) error) error {
	cfg, err := loadConfig()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}
	if cfg.InferenceSecret == "" {
		secret, gerr := config.GenerateInferenceSecret()
		if gerr != nil {
			return fmt.Errorf("generate inference secret: %w", gerr)
		}
		cfg.InferenceSecret = secret
		if serr := saveConfig(cfg); serr != nil {
			return fmt.Errorf("persist inference secret: %w", serr)
		}
	}
	name, text := orchestrate.RenderInferenceSecretEnv(cfg.InferenceSecret)
	if err := writeInferenceSecretEnv(name, text); err != nil {
		return fmt.Errorf("write inference secret env: %w", err)
	}
	return nil
}

// ensureInferenceSecret is the lifecycleDeps-injected half of the migration
// (up/restart, via applyReconcile — the seam they share): it drives
// ensureInferenceSecretWith through d's own loadConfig/saveConfig/
// writeInferenceSecretEnv fields so lifecycle_test.go can exercise it with
// stubs, with zero live host I/O.
func (d *lifecycleDeps) ensureInferenceSecret() error {
	return ensureInferenceSecretWith(d.loadConfig, d.saveConfig, d.writeInferenceSecretEnv)
}

// ensureInferenceSecretFile is the LIVE half of the same migration, driven
// through the real host seams (config.LoadVilla/SaveVilla,
// orchestrate.WriteInferenceSecretEnv) rather than lifecycleDeps' injected
// fields. liveWriteUnits calls this so every OTHER unit-writing verb — backend
// set, tools-mode, coding-mode, speculation set, model swap, a resident-model
// verb, `villa update`, `villa restore`, and the dashboard's model switch — gets
// the same self-heal lifecycleDeps.ensureInferenceSecret gives up/restart,
// without each of those verbs' own Deps struct needing its own copy of this
// logic (GHSA-qxg9, ADR-0011).
func ensureInferenceSecretFile() error {
	return ensureInferenceSecretWith(config.LoadVilla, config.SaveVilla, orchestrate.WriteInferenceSecretEnv)
}

// liveWriteUnits is the ONE live seam every unit-writing verb funnels its
// orchestrate.WriteUnits call through (GHSA-qxg9, ADR-0011), instead of calling
// orchestrate.WriteUnits directly: it self-heals the inference secret env file
// BEFORE any unit is touched, so a verb that runs first on an upgraded host
// whose config.toml predates the bearer does not write a unit whose
// EnvironmentFile= target does not exist yet. A no-op plan (nothing changed)
// skips it entirely — which is also what keeps a --dry-run caller (none of
// which ever reach here with a non-empty plan; they return before this seam)
// side-effect-free, and refuses before any unit is touched if the env file
// cannot be written.
func liveWriteUnits(plan orchestrate.Plan, unitDir string) error {
	if len(plan.Changed) > 0 {
		if err := ensureInferenceSecretFile(); err != nil {
			return fmt.Errorf("ensure inference secret: %w", err)
		}
	}
	return orchestrate.WriteUnits(plan, unitDir)
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
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return nil, "", fmt.Errorf("resolve backend: %w", err)
	}
	// Resident slots are threaded through so up/restart regenerate the SAME unit set
	// `villa model resident` wrote. Without them a reconcile drops every resident
	// endpoint from the chat UI's env and leaves the resident units unmanaged.
	resident, err := liveResidentUnits(cfg)
	if err != nil {
		return nil, "", fmt.Errorf("resolve resident models: %w", err)
	}
	units, err = d.render(orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     modelFile,
		ModelsDir:     d.modelsDir(),
		HostVillaPath: hostVillaPath(),
		Resident:      resident,
	})
	if err != nil {
		return nil, "", fmt.Errorf("render: %w", err)
	}
	return units, dir, nil
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

// applyReconcile writes the changed units and daemon-reloads (only when something
// changed). It returns whether anything changed so the caller can decide between a
// true no-op and a (re)start. printDryRun handles --dry-run separately.
//
// It is the ONE seam up and restart share (runUp/runRestart both call it), so the
// GHSA-qxg9 inference-secret migration lives HERE — before d.writeUnits — rather
// than as a separate call in each of their RunE bodies: neither can reach
// d.writeUnits without it running first, and a true no-op (nothing changed) skips
// both, which is also what keeps --dry-run (which never reaches a non-empty plan
// here) side-effect-free.
func (d *lifecycleDeps) applyReconcile(out io.Writer, plan orchestrate.Plan, unitDir string) (changed bool, err error) {
	if len(plan.Changed) == 0 {
		return false, nil
	}
	if err := d.ensureInferenceSecret(); err != nil {
		return false, fmt.Errorf("ensure inference secret: %w", err)
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
		render:     livePinnedRender,
		reconcile:  orchestrate.Reconcile,
		writeUnits: orchestrate.WriteUnits,
		unitDir:    quadletUnitDir,

		saveConfig:              config.SaveVilla,
		writeInferenceSecretEnv: orchestrate.WriteInferenceSecretEnv,

		daemonReload: sys.DaemonReload,
		start:        sys.Start,
		stop:         sys.Stop,
		restart:      sys.Restart,
		isActive:     sys.IsActive,

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
