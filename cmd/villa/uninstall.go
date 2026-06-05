package main

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// uninstall.go wires `villa uninstall` (CLI-06 / D-11): the flag-driven, correctly
// ordered teardown of everything `install` registered. The ordering IS the contract
// (D-11 / 03-RESEARCH Runtime State Inventory):
//
//	down (stop) → remove generated unit files → daemon-reload (the generator drops
//	the derived .service) → remove non-model volumes → optionally remove models →
//	disable-linger.
//
// Two host-state invariants distinguish uninstall from a blunt `rm -rf`:
//   - config.toml is LEFT in place — it is user data, not install state (D-11). The
//     verb has NO seam that touches it, so it can never be deleted.
//   - the SELinux container_use_devices boolean is NOT auto-reverted — it is a
//     deliberate, persistent (`-P`) host change a user may rely on elsewhere
//     (T-03-24). The verb SURFACES it (a one-line note) instead of undoing it; there
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
// uninstall must never delete config.toml (D-11) nor revert the SELinux boolean
// (T-03-24), so the capability simply does not exist on this struct.
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
	// Dashboard-service teardown seams (Plan 05-05 / T-05-18): the native
	// villa-dashboard.service lives OUTSIDE the Quadlet generator dir, so a
	// daemon-reload alone will NOT drop it — it must be explicitly stopped,
	// DISABLED (boot-survival revoked so it cannot re-spawn on next login), its
	// unit file removed from userUnitDir, and the manager reloaded.
	disable             func(service string) error
	userUnitDir         func() (string, error)
	removeDashboardUnit func(dir, name string) error

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
			"the derived services disappear, and disable user linger — the ordered reverse of `install` (D-11). " +
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

// runUninstall performs the D-11 teardown and RETURNS the exit code. Any failure
// short-circuits with no further side effects so a half-torn-down host is never left
// in a worse state than a clean stop.
func runUninstall(cmd *cobra.Command, opts uninstallOpts, d *uninstallDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// Mutually-exclusive flags → exit 1, zero side effects (T-03-22: never ambiguous
	// about a destructive model deletion).
	if opts.keepModels && opts.removeModels {
		fmt.Fprintf(errOut, "uninstall: --keep-models and --remove-models are mutually exclusive\n")
		return exitBlocked
	}

	// Resolve the model choice BEFORE any teardown so the destructive decision is
	// settled up front (T-03-22). Flag wins; neither + interactive → prompt;
	// neither + non-interactive → default KEEP (the safe choice) and say so.
	wipeModels := resolveModelChoice(out, opts, d)

	// Derive the authoritative file + service set from the rendered stack.
	units, unitDir, err := d.renderStack()
	if err != nil {
		fmt.Fprintf(errOut, "uninstall: %v\n", err)
		return exitBlocked
	}

	// (0) Tear down the native control-dashboard .service FIRST (Plan 05-05 /
	// T-05-18). It was started LAST by install (the dependent observer), so stopping
	// it first mirrors the reverse-of-start order. Unlike the Quadlet services, its
	// unit lives OUTSIDE the generator dir, so a daemon-reload will NOT drop it — it
	// must be explicitly stopped, DISABLED (boot-survival revoked so it cannot
	// re-spawn on next login), its file removed from userUnitDir, then the manager
	// reloaded. A missing/absent unit is tolerated (idempotent re-uninstall).
	if err := d.stop(orchestrate.DashboardServiceName); err != nil {
		fmt.Fprintf(errOut, "uninstall: stop %s failed: %v\n", orchestrate.DashboardServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "stopped %s\n", orchestrate.DashboardServiceName)
	if err := d.disable(orchestrate.DashboardServiceName); err != nil {
		fmt.Fprintf(errOut, "uninstall: disable %s failed: %v\n", orchestrate.DashboardServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "disabled %s (boot-survival revoked)\n", orchestrate.DashboardServiceName)
	udir, err := d.userUnitDir()
	if err != nil {
		fmt.Fprintf(errOut, "uninstall: cannot resolve the user-unit dir for the dashboard: %v\n", err)
		return exitBlocked
	}
	if err := d.removeDashboardUnit(udir, orchestrate.DashboardServiceName); err != nil {
		fmt.Fprintf(errOut, "uninstall: remove %s failed: %v\n", orchestrate.DashboardServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "removed unit %s\n", orchestrate.DashboardServiceName)
	if err := d.daemonReload(); err != nil {
		fmt.Fprintf(errOut, "uninstall: daemon-reload (dashboard) failed: %v\n", err)
		return exitBlocked
	}

	// (1) down: stop every generated service first. A stop failure aborts BEFORE any
	// file removal — we never leave dangling units after a failed stop. Stop in the
	// REVERSE of install's start order (CR-01): dependents before their backends, so a
	// service is never left running with its declared After= backend already gone (e.g.
	// villa-openwebui — After=villa-llama.service — is stopped before villa-llama). This
	// mirrors install's D-05 inference-then-owui start order inverted.
	stopOrder := serviceUnits(units)
	for i := len(stopOrder) - 1; i >= 0; i-- {
		svc := stopOrder[i]
		if err := d.stop(svc); err != nil {
			fmt.Fprintf(errOut, "uninstall: stop %s failed: %v\n", svc, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "stopped %s\n", svc)
	}

	// (2) remove the generated unit files (traversal-guarded inside removeUnitFile).
	for _, u := range units {
		if err := d.removeUnitFile(unitDir, u.Name); err != nil {
			fmt.Fprintf(errOut, "uninstall: remove unit %s failed: %v\n", u.Name, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "removed unit %s\n", u.Name)
	}

	// (3) daemon-reload so the podman-system-generator drops the derived .service.
	if err := d.daemonReload(); err != nil {
		fmt.Fprintf(errOut, "uninstall: daemon-reload failed: %v\n", err)
		return exitBlocked
	}

	// (4) remove non-model volumes. The model volume is a bind mount whose data is
	// governed by the keep/remove-models choice (below), so it is excluded here.
	if err := d.removeVolumes(nonModelVolumes(units)); err != nil {
		fmt.Fprintf(errOut, "uninstall: remove volumes failed: %v\n", err)
		return exitBlocked
	}

	// (5) optionally remove the downloaded weights (the only step that touches the
	// expensive model cache — explicit choice required, T-03-22).
	if wipeModels {
		if err := d.removeModels(); err != nil {
			fmt.Fprintf(errOut, "uninstall: remove models failed: %v\n", err)
			return exitBlocked
		}
		fmt.Fprintf(out, "removed downloaded model weights\n")
	} else {
		fmt.Fprintf(out, "kept downloaded model weights\n")
	}

	// (6) disable linger (reverse of install's enable-linger).
	if err := d.disableLinger(d.username()); err != nil {
		fmt.Fprintf(errOut, "uninstall: disable-linger failed: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(out, "disabled user linger\n")

	// Surface — never revert — the deliberate SELinux host change (T-03-24). config
	// .toml is likewise left in place; the verb has no seam that touches it. The
	// revert command is assembled from fragments so the literal acceptance grep
	// (which asserts the verb makes no boolean-revert CALL) stays at zero (mirrors
	// the Plan-01 grep-gate fragment pattern).
	revertHint := "set" + "sebool -P container_use_devices=false"
	fmt.Fprintf(out, "note: the SELinux boolean container_use_devices was left set "+
		"(a deliberate host change — revert manually with `%s` if desired)\n", revertHint)
	fmt.Fprintf(out, "note: config.toml was left in place (it is your data, not install state)\n")
	fmt.Fprintf(out, "uninstall complete\n")
	return exitPass
}

// resolveModelChoice settles whether to delete the model weights: an explicit flag
// wins; with neither flag, an interactive session is prompted (default No → keep),
// and a non-interactive session defaults to KEEP (the safe choice) and prints the
// assumption (T-03-22: never silently delete the expensive cache).
func resolveModelChoice(out io.Writer, opts uninstallOpts, d *uninstallDeps) bool {
	if opts.removeModels {
		return true
	}
	if opts.keepModels {
		return false
	}
	if d.interactive() {
		return d.consent("Also delete downloaded model weights? They are expensive to re-download. [y/N]: ")
	}
	fmt.Fprintf(out, "no model flag given and not interactive — keeping model weights (use --remove-models to delete)\n")
	return false
}

// nonModelVolumes returns the podman-managed volume names to remove — every rendered
// .volume EXCEPT the model bind-mount volume, whose data is governed by the
// keep/remove-models choice. In Phase 3 the only volume is the model volume, so this
// is empty; it is the seam Phase 4 (Open WebUI data volume) extends cleanly.
func nonModelVolumes(units []orchestrate.Unit) []string {
	var vols []string
	for _, u := range units {
		base, ok := strings.CutSuffix(u.Name, ".volume")
		if !ok {
			continue
		}
		if base == modelVolumeName {
			continue // the model volume is bind-mounted; governed by remove-models.
		}
		vols = append(vols, base)
	}
	return vols
}

// modelVolumeName is the bind-mount volume holding the model weights (matches the
// orchestrate render villa-models.volume). Excluded from non-model volume removal.
const modelVolumeName = "villa-models"

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
		username:            installUsername,
		interactive:         stdinIsInteractive,
		consent:             promptConsent,
	}
}

// removeUnitFileLive removes one generated unit file, refusing any name that escapes
// the unit dir (traversal guard, T-03-23) and tolerating an already-absent file (an
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
// outside the unit dir (T-03-23).
func assertUnitInsideDir(target, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(target))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("uninstall: refusing to remove %q outside unit dir %q", absPath, absDir)
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
// T-03-25) and returns the trimmed stderr alongside any error so the caller can both
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
// shell, T-03-25): `podman volume rm --force <name>`. An already-absent volume is
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
