package install

import (
	"fmt"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// transaction.go gives install the capture-mutate-prove-rollback discipline the
// three swap cores already had.
//
// Install proves as thoroughly as they do — memory, search readiness and the coding
// agent all have proof seams — but a failure part-way through used to leave whatever
// it had already written and started in place. An install that renders units, starts
// four services and then fails its proof left a running-but-unproven stack: the
// false-green ADR-0001 exists to prevent, reached through the one door that was not
// guarded.
//
// ADR-0003 records the decision this file implements, including what "restore" means
// on a host that had no prior stack: it restores to NOTHING, because a half-installed
// stack is indistinguishable from a healthy one to every ordinary check, and a later
// `villa up` or a reboot would bring up a stack that never passed its proof.

// Prior is the state captured before install mutates anything.
//
// It holds the config, whether one existed at all, and which services were running.
// Model weights and container images are deliberately NOT captured: they are large,
// expensive to re-acquire and inert on their own, so a failed install leaves them in
// place and a retry does not re-download them. The backup path excludes them for the
// same reason.
type Prior struct {
	// Config is the config as it was before this install.
	Config config.VillaConfig
	// HadConfig reports whether a config existed at all. It is the difference
	// between a re-install, which restores the prior config, and a first install,
	// which removes the config it wrote.
	HadConfig bool
	// Units are the unit files present before this install, by name, with their
	// contents. A unit absent here that install wrote is removed on rollback.
	Units map[string]string
	// Running are the services that were active before this install.
	Running map[string]bool
	// FirstInstall reports that this host had no prior stack, so rollback restores
	// to nothing rather than to a previous state.
	FirstInstall bool
}

// WasRunning reports whether a service was active before this install.
func (p Prior) WasRunning(service string) bool { return p.Running[service] }

// HadUnit reports whether a unit file existed before this install.
func (p Prior) HadUnit(name string) (string, bool) {
	text, ok := p.Units[name]
	return text, ok
}

// CapturePrior builds the capture from the host readings the caller supplies.
//
// A host is a first install when it had no config AND no units: either alone is an
// incomplete prior state rather than a clean host, and treating it as clean would
// delete the half that was there.
func CapturePrior(cfg config.VillaConfig, hadConfig bool, units map[string]string, running map[string]bool) Prior {
	p := Prior{
		Config:    cfg,
		HadConfig: hadConfig,
		Units:     units,
		Running:   running,
	}
	if p.Units == nil {
		p.Units = map[string]string{}
	}
	if p.Running == nil {
		p.Running = map[string]bool{}
	}
	p.FirstInstall = !hadConfig && len(p.Units) == 0
	return p
}

// RollbackDeps are the effects a rollback performs. Each is a seam so the whole
// restore is driven from a test without a host.
type RollbackDeps struct {
	// StopService stops a service that this install started and that was not running
	// before.
	StopService func(service string) error
	// StartService restarts a service that was running before but that this install
	// stopped or replaced.
	StartService func(service string) error
	// WriteUnit restores a unit file's prior contents.
	WriteUnit func(name, text string) error
	// RemoveUnit deletes a unit file this install created where none existed.
	RemoveUnit func(name string) error
	// SaveConfig restores the prior config.
	SaveConfig func(config.VillaConfig) error
	// RemoveConfig deletes the config this install wrote on a clean host.
	RemoveConfig func() error
	// DaemonReload reloads the manager after unit files change.
	DaemonReload func() error
}

// Mutations is what this install actually changed, accumulated as it goes. Rollback
// undoes exactly this, which is what makes the restore verbatim rather than a guess
// at what a clean host looks like.
type Mutations struct {
	// Started are the services this install started, in order.
	Started []string
	// WroteUnits are the unit files this install wrote, by name.
	WroteUnits []string
	// SavedConfig reports whether this install persisted a config.
	SavedConfig bool
}

// RecordStart notes that a service was started.
func (m *Mutations) RecordStart(service string) { m.Started = append(m.Started, service) }

// RecordUnit notes that a unit file was written.
func (m *Mutations) RecordUnit(name string) { m.WroteUnits = append(m.WroteUnits, name) }

// RecordConfigSave notes that the config was persisted.
func (m *Mutations) RecordConfigSave() { m.SavedConfig = true }

// RollbackResult reports what the restore achieved.
//
// Incomplete is the honest signal that matters. A rollback step that itself failed
// must never be presented as a clean restoration: a wrong "restored" claim is worse
// than an honest "partially restored", because it tells the operator to stop looking.
type RollbackResult struct {
	// Incomplete is true when one or more restore steps failed.
	Incomplete bool
	// Failures describes each step that could not be undone.
	Failures []string
}

// Reason renders the operator-facing explanation, naming what could not be undone.
func (r RollbackResult) Reason() string {
	if !r.Incomplete {
		return "rolled back to the prior state"
	}
	msg := "rolled back, but the restore did not fully complete"
	for _, f := range r.Failures {
		msg += "; " + f
	}
	return msg + " — the stack is in an indeterminate state: run `villa status` and inspect the units"
}

// Rollback restores the host to the captured prior state.
//
// It is best-effort and continues past a failed step, because stopping at the first
// failure would leave more of the mutation in place than necessary. Every failure is
// recorded, and the result reports honestly that the restore was incomplete.
//
// The order is the reverse of the mutation: stop what was started, restore or remove
// the units, reload, then restore or remove the config. Stopping first means the
// units are not rewritten under services still running against them.
func Rollback(d RollbackDeps, prior Prior, m Mutations) RollbackResult {
	var res RollbackResult
	fail := func(format string, args ...any) {
		res.Incomplete = true
		res.Failures = append(res.Failures, fmt.Sprintf(format, args...))
	}

	// (1) Stop the services this install started that were not running before. A
	// service that WAS running before is left alone here and restarted below against
	// its restored unit.
	for i := len(m.Started) - 1; i >= 0; i-- {
		svc := m.Started[i]
		if prior.WasRunning(svc) {
			continue
		}
		if d.StopService == nil {
			continue
		}
		if err := d.StopService(svc); err != nil {
			fail("could not stop %s (%v)", svc, err)
		}
	}

	// (2) Restore the unit files. A unit that existed before is rewritten with its
	// prior contents; one this install created is removed.
	unitsChanged := false
	for _, name := range m.WroteUnits {
		text, had := prior.HadUnit(name)
		switch {
		case had && d.WriteUnit != nil:
			if err := d.WriteUnit(name, text); err != nil {
				fail("could not restore unit %s (%v)", name, err)
				continue
			}
			unitsChanged = true
		case !had && d.RemoveUnit != nil:
			if err := d.RemoveUnit(name); err != nil {
				fail("could not remove unit %s (%v)", name, err)
				continue
			}
			unitsChanged = true
		}
	}
	if unitsChanged && d.DaemonReload != nil {
		if err := d.DaemonReload(); err != nil {
			fail("could not reload the manager (%v)", err)
		}
	}

	// (3) Restart the services that were running before, so a failed re-install
	// leaves the working stack running rather than a stopped one.
	for _, svc := range m.Started {
		if !prior.WasRunning(svc) || d.StartService == nil {
			continue
		}
		if err := d.StartService(svc); err != nil {
			fail("could not restart %s (%v)", svc, err)
		}
	}

	// (4) Restore the config last, so nothing is rendered from it in between. On a
	// first install it is removed: leaving a config for a stack that does not exist
	// would make a later `villa up` try to bring up units that are gone.
	if m.SavedConfig {
		switch {
		case prior.HadConfig && d.SaveConfig != nil:
			if err := d.SaveConfig(prior.Config); err != nil {
				fail("could not restore config.toml (%v)", err)
			}
		case !prior.HadConfig && d.RemoveConfig != nil:
			if err := d.RemoveConfig(); err != nil {
				fail("could not remove the config this install wrote (%v)", err)
			}
		}
	}

	return res
}
