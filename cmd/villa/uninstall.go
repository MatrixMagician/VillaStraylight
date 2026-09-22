package main

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/uninstall"
)

// uninstall.go wires `villa uninstall`: the flag-driven, correctly
// ordered teardown of everything `install` registered. The ordering IS the contract
// (03-RESEARCH Runtime State Inventory):
//
//	down (stop) → remove generated unit files → daemon-reload (the generator drops
//	the derived .service) → remove non-model volumes → optionally remove models →
//	disable-linger.
//
// Two host-state invariants distinguish uninstall from a blunt `rm -rf`:
// - config.toml is LEFT in place — it is user data, not install state. The
//     verb has NO seam that touches it, so it can never be deleted.
//   - the SELinux container_use_devices boolean is NOT auto-reverted — it is a
//     deliberate, persistent (`-P`) host change a user may rely on elsewhere
// The verb SURFACES it (a one-line note) instead of undoing it; there
//     is deliberately no boolean-revert seam here.
//
// Like every Phase-3 verb, host-touching actions are injectable uninstallDeps fields
// so uninstall_test.go drives the whole flow (and asserts ordering) with no live
// podman/systemd/loginctl host; runUninstall RETURNS the exit code (the cobra RunE
// wrapper calls os.Exit), mirroring runInstall/runModelSwap.

// uninstallOpts are the per-invocation flags. keepModels and removeModels are
// mutually exclusive; neither set means "ask (interactive) or default-keep
// (non-interactive)".
type uninstallOpts struct {
	keepModels   bool
	removeModels bool
}

// uninstallDeps is the injectable seam set for `uninstall`. Defaults wire the real
// host (liveUninstallDeps); the test replaces them with order-recording stubs.
//
// Note the deliberate ABSENCE of any config-delete and any boolean-revert seam:
// uninstall must never delete config.toml nor revert the SELinux boolean
// so the capability simply does not exist on this struct.
type uninstallDeps struct {
	// renderStack yields the generated units (the authoritative file + service set
	// to tear down) and the unit dir they live in.
	renderStack func() ([]orchestrate.Unit, string, error)
	// stop stops one generated service (`systemctl --user stop`).
	stop func(service string) error
	// removeUnitFile removes one generated unit file from dir, traversal-guarded.
	removeUnitFile func(dir, name string) error
	// daemonReload re-reads units so the generator drops the now-absent .service.
	daemonReload func() error
	// removeVolumes removes the named non-model podman volumes (`podman volume rm`).
	// The model volume is a bind mount governed by keep/remove-models, so it is
	// never in this set.
	removeVolumes func(vols []string) error
	// removeModels deletes the downloaded GGUF weights (only on --remove-models).
	removeModels func() error
	// Dashboard-service teardown seams (Plan 05-05): the native
	// villa-dashboard.service lives OUTSIDE the Quadlet generator dir, so a
	// daemon-reload alone will NOT drop it — it must be explicitly stopped,
	// DISABLED (boot-survival revoked so it cannot re-spawn on next login), its
	// unit file removed from userUnitDir, and the manager reloaded.
	disable             func(service string) error
	userUnitDir         func() (string, error)
	removeDashboardUnit func(dir, name string) error

	// Coding-agent addon teardown seams (v1.4). Both are ALWAYS removed,
	// idempotently (an absent file is NOT an error — a re-uninstall, or an uninstall
	// after an agent-off install, succeeds):
	//   - removeAgentBinary removes the villa-owned crush binary at agentBinPath()
	//     ($XDG_DATA_HOME/villa/bin/crush), traversal-guarded inside agentBinDir().
	//   - removeCrushConfig removes the rendered crush.json at crushConfigPath()
	//     (~/.config/crush/crush.json), traversal-guarded inside its parent dir.
	// The staged coder GGUF is NOT a seam here — it lives in modelsDir() and is governed
	// by the existing keep/remove-models choice (default keep). config.toml is LEFT
	// (no seam touches it) — the deliberate-absence invariant above still holds.
	removeAgentBinary func() error
	removeCrushConfig func() error

	// disableLinger reverses install's enable-linger (`loginctl disable-linger`).
	disableLinger func(user string) error
	// username resolves the current user for disable-linger.
	username func() string
	// interactive reports whether stdin is a TTY (so prompting is meaningful).
	interactive func() bool
	// consent prompts y/N and returns the answer (opt-in).
	consent func(prompt string) bool
}

// newUninstall builds `villa uninstall`: ordered teardown with a flag-driven (or
// prompted) keep/remove-models choice; leaves config.toml and the SELinux boolean.
func newUninstall() *cobra.Command {
	var keep, remove bool
	cmd := &cobra.Command{
		Use:   "uninstall",
		Short: "Tear down the stack (units, non-model volumes, linger), keeping config.toml",
		Long: "Stop the stack, remove the generated Quadlet units and non-model volumes, daemon-reload so " +
			"the derived services disappear, and disable user linger — the ordered reverse of `install`. " +
			"config.toml is LEFT in place (it is your data, not install state) and the SELinux " +
			"container_use_devices boolean is NOT reverted (a deliberate host change — it is surfaced, not undone). " +
			"Use --remove-models to also delete downloaded weights, or --keep-models to keep them; with neither, " +
			"you are prompted on a terminal (and weights are kept by default when non-interactive).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			code := runUninstall(cmd, uninstallOpts{keepModels: keep, removeModels: remove}, liveUninstallDeps())
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&keep, "keep-models", false, "keep downloaded model weights (do not delete the models dir)")
	cmd.Flags().BoolVar(&remove, "remove-models", false, "also delete downloaded model weights")
	return cmd
}

// runUninstall performs the teardown and RETURNS the exit code. The ordering,
// the model keep/remove decision, and every other host-touching decision now
// live in the pure internal/uninstall core (#241, ADR-0012); this function is
// parse-input (the mutually-exclusive-flags check has zero Deps calls either
// way, so it stays here), resolve the rendered stack, call, and render.
func runUninstall(cmd *cobra.Command, opts uninstallOpts, d *uninstallDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Mutually-exclusive flags → exit 1, zero side effects (never ambiguous
	// about a destructive model deletion).
	if opts.keepModels && opts.removeModels {
		fmt.Fprintf(errOut, "uninstall: --keep-models and --remove-models are mutually exclusive\n")
		return exitBlocked
	}

	// Derive the authoritative file + service set from the rendered stack.
	units, unitDir, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "uninstall: %v\n", err)
		return exitBlocked
	}

	// Stop in the REVERSE of install's start order: dependents before their
	// backends, so a service is never left running with its declared After=
	// backend already gone (e.g. villa-openwebui — After=villa-llama.service —
	// is stopped before villa-llama). This mirrors install's
	// inference-then-owui start order inverted.
	startOrder := serviceUnits(units)
	stopOrder := make([]string, len(startOrder))
	for i, svc := range startOrder {
		stopOrder[len(startOrder)-1-i] = svc
	}

	res := uninstall.Run(d.toCore(), uninstall.Opts{KeepModels: opts.keepModels, RemoveModels: opts.removeModels}, uninstall.Input{
		Units:         units,
		UnitDir:       unitDir,
		StopOrder:     stopOrder,
		DashboardName: orchestrate.DashboardServiceName,
	})
	for _, line := range res.Lines {
		fmt.Fprintf(out, "%s\n", line)
	}
	if res.Err != nil {
		fmt.Fprintf(errOut, "uninstall: %v\n", res.Err)
		return exitBlocked
	}
	return exitPass
}

// toCore translates the cmd-tier uninstallDeps (kept for uninstall_test.go's
// existing lowercase-field construction) into internal/uninstall.Deps.
func (d *uninstallDeps) toCore() uninstall.Deps {
	return uninstall.Deps{
		Stop:                d.stop,
		RemoveUnitFile:      d.removeUnitFile,
		DaemonReload:        d.daemonReload,
		RemoveVolumes:       d.removeVolumes,
		RemoveModels:        d.removeModels,
		Disable:             d.disable,
		UserUnitDir:         d.userUnitDir,
		RemoveDashboardUnit: d.removeDashboardUnit,
		RemoveAgentBinary:   d.removeAgentBinary,
		RemoveCrushConfig:   d.removeCrushConfig,
		DisableLinger:       d.disableLinger,
		Username:            d.username,
		Interactive:         d.interactive,
		Consent:             d.consent,
	}
}

// nonModelVolumes delegates to the moved core decision (#241) — kept as a
// thin cmd-tier name because uninstall_test.go calls it directly.
func nonModelVolumes(units []orchestrate.Unit) []string {
	return uninstall.NonModelVolumes(units)
}

// modelVolumeName aliases the moved core constant — kept because
// uninstall_test.go references it directly.
const modelVolumeName = uninstall.ModelVolumeName

// liveUninstallDeps wires uninstall to the real host: the same orchestrate render +
// systemd seam install/up use, fixed-arg `podman volume rm`, and a traversal-guarded
// unit-file removal. There is intentionally no config-delete and no boolean-revert seam.
func liveUninstallDeps() *uninstallDeps {
	sys := orchestrate.NewSystemd()
	ld := liveLifecycleDeps()
	return &uninstallDeps{
		renderStack:    ld.renderStack,
		stop:           sys.Stop,
		removeUnitFile: removeUnitFileLive,
		daemonReload:   sys.DaemonReload,
		removeVolumes:  removeVolumesLive,
		removeModels:   removeModelsLive,
		disableLinger:  sys.DisableLinger,

		// Dashboard-service teardown seams (Plan 05-05): disable revokes boot-survival,
		// userUnitDir locates the native .service, and removeDashboardUnit reuses the
		// same traversal-guarded removal the Quadlet units use.
		disable:             sys.Disable,
		userUnitDir:         orchestrate.UserUnitDir,
		removeDashboardUnit: removeUnitFileLive,

		// Coding-agent addon teardown: reuse agentBinPath/crushConfigPath from
		// code.go (DRY — the same paths install_agent.go stages to) with a traversal-guarded
		// idempotent os.Remove. ALWAYS removed; an absent file is tolerated.
		removeAgentBinary: removeAgentBinaryLive,
		removeCrushConfig: removeCrushConfigLive,

		username:    installUsername,
		interactive: stdinIsInteractive,
		consent:     promptConsent,
	}
}

// removeAgentBinaryLive removes the villa-owned crush binary at agentBinPath()
// ($XDG_DATA_HOME/villa/bin/crush), confining the path inside agentBinDir() before
// removing (traversal guard, mirroring removeUnitFileLive/assertUnitInsideDir) and
// tolerating an already-absent file (idempotent re-uninstall).
func removeAgentBinaryLive() error {
	target := agentBinPath()
	if err := assertUnitInsideDir(target, agentBinDir()); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeCrushConfigLive removes the rendered crush.json at crushConfigPath()
// (~/.config/crush/crush.json), confining the path inside its parent dir before
// removing (traversal guard) and tolerating an already-absent file (idempotent).
func removeCrushConfigLive() error {
	target, err := crushConfigPath()
	if err != nil {
		return err
	}
	if err := assertUnitInsideDir(target, filepath.Dir(target)); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// removeUnitFileLive removes one generated unit file, refusing any name that escapes
// the unit dir (traversal guard) and tolerating an already-absent file (an
// idempotent re-uninstall is not an error).
func removeUnitFileLive(dir, name string) error {
	target := filepath.Join(dir, name)
	if err := assertUnitInsideDir(target, dir); err != nil {
		return err
	}
	if err := os.Remove(target); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

// assertUnitInsideDir verifies target resolves within dir (mirrors the orchestrate
// WriteUnits guard) so an attacker-influenced unit name can never delete a file
// outside the unit dir.
func assertUnitInsideDir(target, dir string) error {
	if err := pathsafe.Inside(target, dir); err != nil {
		return fmt.Errorf("uninstall: refusing to remove a path outside %q: %w", dir, err)
	}
	return nil
}

// volumeRmArgs builds the FIXED-ARG argv for removing one podman volume:
// `volume rm --force <name>`. --force lets a stopped-but-present volume remove
// cleanly. It deliberately emits NO not-found-tolerance flag: `podman volume rm`
// does not support one (only `rm`/`network rm`/`pod rm` do), and passing it made
// podman exit 125 on the first volume, aborting the rest of uninstall. Idempotent
// teardown is preserved instead by inspecting the not-found stderr in
// removeVolumesLive. This pure builder is the seam the argv regression test asserts
// against.
func volumeRmArgs(v string) []string {
	return []string{"volume", "rm", "--force", v}
}

// podmanVolumeRm runs `podman <args...>` with a FIXED-ARG exec (never a shell,
// and returns the trimmed stderr alongside any error so the caller can both
// diagnose a genuine failure AND recognise an already-absent volume. It is a
// package-level var so uninstall_test.go can swap in a fake runner and drive
// removeVolumesLive with no live podman.
var podmanVolumeRm = func(args []string) (stderr string, err error) {
	var buf bytes.Buffer
	cmd := exec.Command("podman", args...) // fixed args
	cmd.Stderr = &buf
	err = cmd.Run()
	return strings.TrimSpace(buf.String()), err
}

// removeVolumesLive removes each named podman volume with a FIXED-ARG exec (never a
// shell): `podman volume rm --force <name>`. An already-absent volume is
// tolerated WITHOUT any unsupported tolerance flag — when podman errors, the trimmed
// stderr is inspected and a not-found signal ("no such volume" / "no volume with
// name") is treated as success, preserving idempotent re-uninstall. Any other failure
// is wrapped WITH its trimmed stderr so the operator sees why (the old code swallowed
// stderr and surfaced only "exit status 125"). In Phase 3 the list is empty (only the
// model bind-mount volume exists); Phase 4's Open WebUI data volume flows through here.
func removeVolumesLive(vols []string) error {
	if len(vols) == 0 {
		return nil
	}
	if _, err := exec.LookPath("podman"); err != nil {
		return orchestrate.ErrToolNotFound{Tool: "podman"}
	}
	for _, v := range vols {
		stderr, err := podmanVolumeRm(volumeRmArgs(v))
		if err == nil {
			continue
		}
		// Tolerate an already-absent volume (idempotent teardown) by recognising the
		// not-found stderr rather than relying on a flag podman does not support.
		low := strings.ToLower(stderr)
		if strings.Contains(low, "no such volume") || strings.Contains(low, "no volume with name") {
			continue
		}
		return fmt.Errorf("podman volume rm %s: %w: %s", v, err, strings.TrimSpace(stderr))
	}
	return nil
}

// removeModelsLive deletes the downloaded GGUF weights by removing the models dir
// tree (the same dir `model pull`/`model swap` populate). config.toml lives under the
// XDG CONFIG dir, not here, so it is untouched.
func removeModelsLive() error {
	dir := modelsDir()
	if err := os.RemoveAll(dir); err != nil {
		return fmt.Errorf("remove models dir %q: %w", dir, err)
	}
	return nil
}
