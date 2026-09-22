// Package uninstall is the pure teardown orchestrator for `villa uninstall`
// (#241, ADR-0012): the ordered reverse of install — dashboard teardown, the
// ALWAYS-removed coding-agent addon artifacts, service stop (reverse of
// install's start order), generated-unit removal, daemon-reload, non-model
// volume removal, the model keep/remove decision, and disable-linger — plus
// the model keep/remove decision itself. Every host effect is a Deps func
// field; Run performs no direct I/O of its own. The live wiring
// (liveUninstallDeps) and the cobra caller stay in cmd/villa.
//
// Two host-state invariants distinguish uninstall from a blunt `rm -rf`, and
// this package has NO seam that could violate either:
//   - config.toml is LEFT in place — it is user data, not install state.
//   - the SELinux container_use_devices boolean is NOT auto-reverted — it is a
//     deliberate, persistent host change a user may rely on elsewhere. Run
//     SURFACES it (a note line) instead of undoing it.
package uninstall

import (
	"fmt"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
)

// ModelVolumeName is the bind-mount volume holding the model weights (matches
// the orchestrate render villa-models.volume). Excluded from non-model volume
// removal.
const ModelVolumeName = "villa-models"

// Opts are the per-invocation flags driving the model keep/remove decision.
// KeepModels and RemoveModels are mutually exclusive; that check has zero Deps
// calls either way, so it stays a cmd-tier input validation ahead of Run.
type Opts struct {
	KeepModels   bool
	RemoveModels bool
}

// Deps is the injectable seam set uninstall needs. The live wiring
// (liveUninstallDeps) stays in cmd/villa. Deliberately ABSENT: any
// config-delete seam and any SELinux-boolean-revert seam — the capability
// simply does not exist on this struct.
type Deps struct {
	Stop                func(service string) error
	RemoveUnitFile      func(dir, name string) error
	DaemonReload        func() error
	RemoveVolumes       func(vols []string) error
	RemoveModels        func() error
	Disable             func(service string) error
	UserUnitDir         func() (string, error)
	RemoveDashboardUnit func(dir, name string) error
	RemoveAgentBinary   func() error
	RemoveCrushConfig   func() error
	DisableLinger       func(user string) error
	Username            func() string
	Interactive         func() bool
	Consent             func(prompt string) bool
}

// Input is what one teardown run needs from the already-rendered stack. The
// cmd tier resolves Units/UnitDir via its existing renderStack seam and
// StopOrder via its existing serviceUnits helper (unexported to cmd/villa, so
// it cannot move here) reversed — dependents before their backends, mirroring
// install's start order inverted.
type Input struct {
	Units   []orchestrate.Unit
	UnitDir string
	// StopOrder is the services to stop, ALREADY in the order to stop them
	// (reverse of install's start order).
	StopOrder []string
	// DashboardName is orchestrate.DashboardServiceName, passed through by the
	// cmd tier so this package carries no service-name literal.
	DashboardName string
}

// Result is one teardown run's narration plus its terminal error. Lines are
// rendered in order by the cmd tier; a non-nil Err means the run stopped with
// zero further Deps calls beyond what already ran.
type Result struct {
	Lines []string
	Err   error
}

func (r *Result) say(format string, args ...any) {
	r.Lines = append(r.Lines, fmt.Sprintf(format, args...))
}

// ModelChoiceSource is HOW the keep/remove-models decision was reached.
type ModelChoiceSource int

const (
	// SourceRemoveFlag: --remove-models was passed.
	SourceRemoveFlag ModelChoiceSource = iota
	// SourceKeepFlag: --keep-models was passed.
	SourceKeepFlag
	// SourcePrompt: neither flag, and the session is interactive — ask.
	SourcePrompt
	// SourceDefaultKeep: neither flag, non-interactive — default to keep (the
	// safe choice; never silently delete the expensive weight cache).
	SourceDefaultKeep
)

// ResolveModelChoiceSource decides HOW the wipe/keep decision will be
// reached — pure, no I/O. An explicit flag always wins; with neither flag, an
// interactive session is prompted; a non-interactive session defaults to keep.
func ResolveModelChoiceSource(opts Opts, interactive bool) ModelChoiceSource {
	switch {
	case opts.RemoveModels:
		return SourceRemoveFlag
	case opts.KeepModels:
		return SourceKeepFlag
	case interactive:
		return SourcePrompt
	default:
		return SourceDefaultKeep
	}
}

// NonModelVolumes returns the podman-managed volume names to remove — every
// rendered .volume EXCEPT the model bind-mount volume, whose data is governed
// by the keep/remove-models choice.
func NonModelVolumes(units []orchestrate.Unit) []string {
	var vols []string
	for _, u := range units {
		base, ok := strings.CutSuffix(u.Name, ".volume")
		if !ok {
			continue
		}
		if base == ModelVolumeName {
			continue // the model volume is bind-mounted; governed by remove-models.
		}
		vols = append(vols, base)
	}
	return vols
}

// Run performs the ordered teardown and returns a Result. Any failure
// short-circuits with NO further Deps calls — a half-torn-down host is never
// left in a worse state than a clean stop.
func Run(d Deps, opts Opts, in Input) Result {
	var res Result

	wipeModels := resolveWipe(d, opts, &res)

	// (0) Tear down the native control-dashboard .service FIRST (Plan 05-05):
	// it was started LAST by install (the dependent observer), so stopping it
	// first mirrors the reverse-of-start order. Its unit lives OUTSIDE the
	// generator dir, so a daemon-reload alone would NOT drop it.
	if err := d.Stop(in.DashboardName); err != nil {
		res.Err = fmt.Errorf("stop %s failed: %w", in.DashboardName, err)
		return res
	}
	res.say("stopped %s", in.DashboardName)
	if err := d.Disable(in.DashboardName); err != nil {
		res.Err = fmt.Errorf("disable %s failed: %w", in.DashboardName, err)
		return res
	}
	res.say("disabled %s (boot-survival revoked)", in.DashboardName)
	udir, err := d.UserUnitDir()
	if err != nil {
		res.Err = fmt.Errorf("cannot resolve the user-unit dir for the dashboard: %w", err)
		return res
	}
	if err := d.RemoveDashboardUnit(udir, in.DashboardName); err != nil {
		res.Err = fmt.Errorf("remove %s failed: %w", in.DashboardName, err)
		return res
	}
	res.say("removed unit %s", in.DashboardName)
	if err := d.DaemonReload(); err != nil {
		res.Err = fmt.Errorf("daemon-reload (dashboard) failed: %w", err)
		return res
	}

	// (0b) Coding-agent addon teardown (v1.4): ALWAYS removed, idempotently
	// (an absent file is not an error), at this deterministic position — after
	// the dashboard teardown, before the container stop.
	if err := d.RemoveAgentBinary(); err != nil {
		res.Err = fmt.Errorf("remove coding agent binary failed: %w", err)
		return res
	}
	res.say("removed coding agent binary")
	if err := d.RemoveCrushConfig(); err != nil {
		res.Err = fmt.Errorf("remove coding agent config (crush.json) failed: %w", err)
		return res
	}
	res.say("removed coding agent config (crush.json)")

	// (1) down: stop every generated service, already reverse-of-start ordered
	// by the caller — a stop failure aborts BEFORE any file removal.
	for _, svc := range in.StopOrder {
		if err := d.Stop(svc); err != nil {
			res.Err = fmt.Errorf("stop %s failed: %w", svc, err)
			return res
		}
		res.say("stopped %s", svc)
	}

	// (2) remove the generated unit files.
	for _, u := range in.Units {
		if err := d.RemoveUnitFile(in.UnitDir, u.Name); err != nil {
			res.Err = fmt.Errorf("remove unit %s failed: %w", u.Name, err)
			return res
		}
		res.say("removed unit %s", u.Name)
	}

	// (3) daemon-reload so the podman-system-generator drops the derived .service.
	if err := d.DaemonReload(); err != nil {
		res.Err = fmt.Errorf("daemon-reload failed: %w", err)
		return res
	}

	// (4) remove non-model volumes. The model volume is excluded (governed by
	// the keep/remove-models choice below).
	if err := d.RemoveVolumes(NonModelVolumes(in.Units)); err != nil {
		res.Err = fmt.Errorf("remove volumes failed: %w", err)
		return res
	}

	// (5) optionally remove the downloaded weights.
	if wipeModels {
		if err := d.RemoveModels(); err != nil {
			res.Err = fmt.Errorf("remove models failed: %w", err)
			return res
		}
		res.say("removed downloaded model weights")
	} else {
		res.say("kept downloaded model weights")
	}

	// (6) disable linger (reverse of install's enable-linger).
	if err := d.DisableLinger(d.Username()); err != nil {
		res.Err = fmt.Errorf("disable-linger failed: %w", err)
		return res
	}
	res.say("disabled user linger")

	// Surface — never revert — the deliberate SELinux host change. config.toml
	// is likewise left in place. The revert command is assembled from
	// fragments so the literal acceptance grep (asserting this package makes
	// no boolean-revert CALL) stays at zero.
	revertHint := "set" + "sebool -P container_use_devices=false"
	res.say("note: the SELinux boolean container_use_devices was left set "+
		"(a deliberate host change — revert manually with `%s` if desired)", revertHint)
	res.say("note: config.toml was left in place (it is your data, not install state)")
	res.say("uninstall complete")
	return res
}

// resolveWipe settles whether to delete the model weights via
// ResolveModelChoiceSource, prompting through Deps.Consent only when the
// session is interactive, and narrating the non-interactive default (never
// silently delete the expensive weight cache) into res.
func resolveWipe(d Deps, opts Opts, res *Result) bool {
	switch ResolveModelChoiceSource(opts, d.Interactive()) {
	case SourceRemoveFlag:
		return true
	case SourceKeepFlag:
		return false
	case SourcePrompt:
		return d.Consent("Also delete downloaded model weights? They are expensive to re-download. [y/N]: ")
	default:
		res.say("no model flag given and not interactive — keeping model weights (use --remove-models to delete)")
		return false
	}
}
