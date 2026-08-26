package main

// model_resident.go wires `villa model resident` — ls / add / rm — the command tier
// over the resident set: several models held loaded at once, each on its own host
// loopback port, instead of the inference unit restarting to swap one for another.
//
// Nothing here decides anything the codebase already decides elsewhere. Whether a
// candidate MAY join is internal/residentset.Admit; what a candidate COSTS is
// recommend.Pick's fit math; what a slot's unit is called is
// orchestrate.ResidentUnitName. This file resolves inputs, sequences effects, and
// renders — it re-derives none of the three.
//
// add and rm are stack-mutating, so both run under the internal/install transaction
// (CapturePrior → Mutations → Rollback) the swap cores and install already share: the
// capture is taken before the config save, every later effect is recorded, and any
// failure restores the config AND the unit files verbatim. A restore that itself
// failed is reported as incomplete rather than as a clean rollback.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/openwebui"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/residentset"
)

// residentPortBase is the lowest host loopback port a resident slot may claim. The
// primary inference unit publishes below it, but allocResidentPort still excludes the
// primary's actual port so the two cannot collide if that ever changes.
const residentPortBase = 8081

// residentPortCeiling is the highest allocatable port. Exhausting the range is a
// refusal rather than an infinite scan.
const residentPortCeiling = 65535

// residentListSchemaVersion versions the `resident ls --json` contract. New keys are
// appended and this is bumped; no key is ever removed or retyped.
const residentListSchemaVersion = 1

// residentDeps is the injectable seam set the three resident verbs drive. Every host
// effect — config, catalog, memory probe, weight download, unit render/write, systemd
// — is a func field, so model_resident_test.go exercises all three off-hardware.
type residentDeps struct {
	loadConfig     func() (config.VillaConfig, error)
	saveConfig     func(config.VillaConfig) error
	resolveCatalog func(id string) (catalog.Model, bool)
	// fit returns the recommend fit math for m at ctx (ctx 0 = the catalog default).
	// It is the ONLY footprint estimator these verbs use.
	fit func(m catalog.Model, ctx int) recommend.Recommendation
	// primaryPort is the host port the primary inference unit publishes on.
	primaryPort  func() int
	isDownloaded func(m catalog.Model) bool
	pull         func(m catalog.Model) error
	renderUnits  func(cfg config.VillaConfig) ([]orchestrate.Unit, error)
	unitDir      func() (string, error)
	reconcile    func(units []orchestrate.Unit, dir string) (orchestrate.Plan, error)
	writeUnits   func(plan orchestrate.Plan, dir string) error
	readUnit     func(dir, name string) (string, bool)
	removeUnit   func(dir, name string) error
	daemonReload func() error
	start        func(service string) error
	stop         func(service string) error
	restart      func(service string) error
	isActive     func(service string) (string, error)
	// syncEndpoints reconciles Open WebUI's STORED connection list to want.
	// Rendering the env var is not enough on an existing install: Open WebUI reads
	// OPENAI_API_BASE_URLS from the environment only on first launch and serves it
	// from its database ever after, so without this call a slot is reachable on its
	// port and absent from the chat UI, with nothing reporting an error. nil means
	// the caller does not reconcile (tests, and any future non-chat consumer).
	syncEndpoints func(ctx context.Context, chatPort int, want []string) (openwebui.EndpointSync, error)
	// syncRetryDelay spaces the reconcile's retries. The reconcile runs immediately
	// after Open WebUI is restarted, and it refuses connections for about a second
	// while it comes back, so a single attempt reports a failure that is really just
	// a race. Zero in tests.
	syncRetryDelay time.Duration
}

// owuiSyncTimeout bounds the chat-UI reconcile. It runs right after a restart, so it
// must allow for Open WebUI still coming up, but never hang the command.
const owuiSyncTimeout = 30 * time.Second

// owuiSyncAttempts bounds the reconcile's retries so a genuinely broken chat UI (bad
// credentials, a crash loop) reports promptly instead of hanging out the whole timeout.
const owuiSyncAttempts = 15

// residentEntry is one row of `villa model resident ls`.
type residentEntry struct {
	Model   string `json:"model"`
	Quant   string `json:"quant"`
	Ctx     int    `json:"ctx"`
	Port    int    `json:"port"`
	Unit    string `json:"unit"`
	Primary bool   `json:"primary"`
	// Active is the systemd state verbatim ("active", "inactive", "failed"), or
	// "unknown" when it could not be read. An unevaluable state is never rendered
	// as inactive: that would be a confident negative the host never gave.
	Active string `json:"active"`
}

// residentListReport is the `resident ls --json` contract.
type residentListReport struct {
	Slots         []residentEntry `json:"slots"`
	SchemaVersion int             `json:"schema_version"`
}

// residentChange is what a mutating resident verb wants done, as data rather than as
// two near-identical effect sequences. add and rm differ only in which unit is started
// and which is orphaned, so both build one of these and hand it to a single applier.
type residentChange struct {
	// verb prefixes every message this change emits.
	verb string
	// prior is the config as it was; next is the config to persist.
	prior, next config.VillaConfig
	// startUnit is the .container filename whose service to start after the reload
	// (add). Empty for rm.
	startUnit string
	// orphan is the .container filename the change leaves unrendered, to be stopped
	// and deleted (rm). Empty for add.
	orphan string
}

// newModelResident builds the `villa model resident` noun and its three subcommands.
func newModelResident() *cobra.Command {
	resident := &cobra.Command{
		Use:   "resident",
		Short: "Hold several models loaded at once instead of restarting to swap",
		Long: "Manage the resident set: models kept loaded alongside the primary, each on its own " +
			"loopback port, so switching between them costs no cold load. Admission is decided against " +
			"the detected memory envelope — a candidate that does not fit is refused, never started.",
		Args: cobra.NoArgs,
	}
	resident.AddCommand(newModelResidentLs(), newModelResidentAdd(), newModelResidentRm())
	return resident
}

// newModelResidentLs builds `villa model resident ls`: read-only, --json supported.
func newModelResidentLs() *cobra.Command {
	var asJSON bool
	cmd := &cobra.Command{
		Use:   "ls",
		Short: "List the primary and every configured resident slot",
		Long: "Print the primary model and each configured resident slot with its quant, context, host " +
			"loopback port, systemd unit, and current unit state. Read-only: nothing is written or started.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, _ []string) error {
			os.Exit(runResidentLs(cmd, asJSON, liveResidentDeps(cmdContext(cmd))))
			return nil
		},
	}
	cmd.Flags().BoolVar(&asJSON, "json", false, "emit the resident set as JSON")
	return cmd
}

// newModelResidentAdd builds `villa model resident add <model-id>`.
func newModelResidentAdd() *cobra.Command {
	return &cobra.Command{
		Use:   "add <model-id>",
		Short: "Admit a model into the resident set: fit-guard, auto-pull, persist, start",
		Long: "Resolve <model-id> through the catalog, REFUSE it when the resident set plus the candidate " +
			"would not fit the usable memory envelope, allocate the lowest free loopback port, auto-pull its " +
			"weights if absent, persist the slot to config.toml BEFORE any unit work, then regenerate the " +
			"units and start the new one. Transactional: any failure after the first mutation is rolled back.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runResidentAdd(cmd, args[0], liveResidentDeps(cmdContext(cmd))))
			return nil
		},
	}
}

// newModelResidentRm builds `villa model resident rm <model-id>`.
func newModelResidentRm() *cobra.Command {
	return &cobra.Command{
		Use:   "rm <model-id>",
		Short: "Remove a resident slot, regenerate the units, and stop the orphaned one",
		Long: "Drop <model-id> from the resident set in config.toml, regenerate the units, then stop and " +
			"delete the unit the removed slot orphans. The primary model is not a resident slot and is " +
			"refused here — switch it with `villa model swap`. Transactional, like add.",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			os.Exit(runResidentRm(cmd, args[0], liveResidentDeps(cmdContext(cmd))))
			return nil
		},
	}
}

// runResidentLs renders the resident set and RETURNS the exit code (no os.Exit) so
// tests assert output and code together.
func runResidentLs(cmd *cobra.Command, asJSON bool, d *residentDeps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	cfg, err := d.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "model resident ls: load config: %v\n", err)
		return exitBlocked
	}

	entries := []residentEntry{{
		Model:   cfg.Model,
		Quant:   cfg.Quant,
		Ctx:     cfg.Ctx,
		Port:    d.primaryPort(),
		Unit:    installServiceName,
		Primary: true,
		Active:  d.activeState(installServiceName),
	}}
	for _, r := range cfg.Resident {
		name, nerr := orchestrate.ResidentUnitName(r.Model)
		if nerr != nil {
			fmt.Fprintf(errOut, "model resident ls: %v\n", nerr)
			return exitBlocked
		}
		service := name + ".service"
		entries = append(entries, residentEntry{
			Model:  r.Model,
			Quant:  r.Quant,
			Ctx:    r.Ctx,
			Port:   r.Port,
			Unit:   service,
			Active: d.activeState(service),
		})
	}

	if asJSON {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		if err := enc.Encode(residentListReport{Slots: entries, SchemaVersion: residentListSchemaVersion}); err != nil {
			fmt.Fprintf(errOut, "model resident ls: encode json: %v\n", err)
			return exitBlocked
		}
		return exitPass
	}

	fmt.Fprintf(out, "%-9s %-24s %-12s %9s %6s %-34s %s\n", "ROLE", "MODEL", "QUANT", "CTX", "PORT", "UNIT", "STATE")
	for _, e := range entries {
		role := "resident"
		if e.Primary {
			role = "primary"
		}
		fmt.Fprintf(out, "%-9s %-24s %-12s %9d %6d %-34s %s\n", role, e.Model, e.Quant, e.Ctx, e.Port, e.Unit, e.Active)
	}
	return exitPass
}

// runResidentAdd admits a candidate and RETURNS the exit code. Everything up to and
// including the admission decision is side-effect free, so an unknown id, an
// unresolvable slot, or a Refusal leaves config and units untouched.
func runResidentAdd(cmd *cobra.Command, id string, d *residentDeps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	cfg, err := d.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "model resident add: load config: %v\n", err)
		return exitBlocked
	}

	// Resolved THROUGH the catalog — an id is never a filesystem path.
	m, ok := d.resolveCatalog(id)
	if !ok {
		fmt.Fprintf(errOut, "model resident add: unknown model %q — run `villa model list` to see catalog names\n", id)
		return exitBlocked
	}

	rec := d.fit(m, 0)
	if rec.Model == "" {
		fmt.Fprintf(errOut, "model resident add: could not size %s against the detected memory envelope\n", m.ID)
		return exitBlocked
	}

	primaryPort := d.primaryPort()
	slots, err := d.residentSlots(cfg, primaryPort)
	if err != nil {
		fmt.Fprintf(errOut, "model resident add: %v\n", err)
		return exitBlocked
	}

	port := allocResidentPort(primaryPort, cfg.Resident)
	if port == 0 {
		fmt.Fprintf(errOut, "model resident add: no free host port at or above %d — remove a slot with `villa model resident rm`\n", residentPortBase)
		return exitBlocked
	}
	unitName, err := orchestrate.ResidentUnitName(m.ID)
	if err != nil {
		fmt.Fprintf(errOut, "model resident add: %v\n", err)
		return exitBlocked
	}

	candidate := residentset.Slot{
		Model: m.ID,
		Quant: m.Quant,
		Ctx:   rec.ContextLen,
		Port:  port,
		Unit:  unitName,
		Bytes: rec.WeightBytes + rec.KVCacheBytes,
	}
	// Eviction is not offered: dropping a slot the user configured is their call, made
	// explicitly with `rm`, never a side effect of an add. Headroom is carried by the
	// Policy rather than added to every slot's Bytes, because the whole set shares one
	// headroom reserve — counting it per slot would refuse sets that comfortably fit.
	plan, refusal := residentset.Admit(
		residentset.Set{Envelope: rec.UsableEnvelopeBytes, Slots: slots},
		candidate,
		residentset.Policy{HeadroomBytes: rec.HeadroomBytes},
	)
	if refusal.Reason != "" {
		fmt.Fprintf(errOut, "model resident add: refused %s (%s) — %s\n", m.ID, refusal.Reason, refusal.Remediation)
		return exitBlocked
	}
	if plan.NoOp {
		fmt.Fprintf(out, "%s is already resident — nothing to do\n", m.ID)
		return exitPass
	}

	if !d.isDownloaded(m) {
		fmt.Fprintf(out, "pulling %s (not yet downloaded)...\n", m.ID)
		if err := d.pull(m); err != nil {
			fmt.Fprintf(errOut, "model resident add: auto-pull %s failed: %v\n", m.ID, err)
			return exitBlocked
		}
	}

	next := cfg
	next.Resident = append(slices.Clone(cfg.Resident), config.ResidentModel{
		Model: candidate.Model,
		Quant: candidate.Quant,
		Ctx:   candidate.Ctx,
		Port:  candidate.Port,
	})

	code := d.applyResidentChange(out, errOut, residentChange{
		verb:      "model resident add",
		prior:     cfg,
		next:      next,
		startUnit: unitName + ".container",
	})
	if code == exitPass {
		fmt.Fprintf(out, "%s is resident on 127.0.0.1:%d (%s.service)\n", candidate.Model, candidate.Port, unitName)
	}
	return code
}

// runResidentRm drops a slot and RETURNS the exit code. The primary is refused before
// anything is read or written, with the command that actually changes it.
func runResidentRm(cmd *cobra.Command, id string, d *residentDeps) int {
	out, errOut := cmd.OutOrStdout(), cmd.ErrOrStderr()

	cfg, err := d.loadConfig()
	if err != nil {
		fmt.Fprintf(errOut, "model resident rm: load config: %v\n", err)
		return exitBlocked
	}

	if cfg.Model != "" && id == cfg.Model {
		fmt.Fprintf(errOut, "model resident rm: %s is the primary model, not a resident slot — change it with `villa model swap <name>`\n", id)
		return exitBlocked
	}

	next := cfg
	next.Resident = nil
	found := false
	for _, r := range cfg.Resident {
		if r.Model == id {
			found = true
			continue
		}
		next.Resident = append(next.Resident, r)
	}
	if !found {
		fmt.Fprintf(errOut, "model resident rm: %q is not a resident slot — run `villa model resident ls`\n", id)
		return exitBlocked
	}

	unitName, err := orchestrate.ResidentUnitName(id)
	if err != nil {
		fmt.Fprintf(errOut, "model resident rm: %v\n", err)
		return exitBlocked
	}

	code := d.applyResidentChange(out, errOut, residentChange{
		verb:   "model resident rm",
		prior:  cfg,
		next:   next,
		orphan: unitName + ".container",
	})
	if code == exitPass {
		fmt.Fprintf(out, "%s is no longer resident — config persisted and %s.service stopped\n", id, unitName)
	}
	return code
}

// applyResidentChange is the transactional half add and rm share.
//
// The capture is taken before the first mutation, the config is persisted BEFORE any
// unit work (config is the single source of truth and must never lag the running
// units), and every effect after the capture is recorded so install.Rollback can undo
// exactly what was done. A restore that itself failed is reported as incomplete: a
// wrong "rolled back" claim tells the operator to stop looking.
func (d *residentDeps) applyResidentChange(out, errOut io.Writer, ch residentChange) int {
	dir, err := d.unitDir()
	if err != nil {
		fmt.Fprintf(errOut, "%s: resolve unit dir: %v\n", ch.verb, err)
		return exitBlocked
	}
	units, err := d.renderUnits(ch.next)
	if err != nil {
		fmt.Fprintf(errOut, "%s: render units: %v\n", ch.verb, err)
		return exitBlocked
	}
	plan, err := d.reconcile(units, dir)
	if err != nil {
		fmt.Fprintf(errOut, "%s: reconcile units: %v\n", ch.verb, err)
		return exitBlocked
	}

	prior, mutated := d.capturePrior(ch, dir, plan), install.Mutations{}

	refuse := func(format string, args ...any) int {
		fmt.Fprintf(errOut, format, args...)
		res := install.Rollback(install.RollbackDeps{
			StopService: d.stop,
			// Restart, not start: a service that was running before is being brought
			// back against its RESTORED unit file, and `start` on an already-active
			// unit would leave the stale one running.
			StartService: d.restart,
			WriteUnit:    func(name, text string) error { return writeUnitText(dir, name, text) },
			RemoveUnit:   func(name string) error { return d.removeUnit(dir, name) },
			SaveConfig:   d.saveConfig,
			DaemonReload: d.daemonReload,
		}, prior, mutated)
		fmt.Fprintf(errOut, "%s: %s\n", ch.verb, res.Reason())
		return exitBlocked
	}

	if err := d.saveConfig(ch.next); err != nil {
		fmt.Fprintf(errOut, "%s: persist config: %v\n", ch.verb, err)
		return exitBlocked // the save IS the first mutation; there is nothing to undo
	}
	mutated.RecordConfigSave()

	if ch.orphan != "" {
		orphanService := unitServiceName(ch.orphan)
		// Recorded before the stop so a later failure restarts it even if the stop
		// itself half-succeeded.
		mutated.RecordStart(orphanService)
		if err := d.stop(orphanService); err != nil {
			return refuse("%s: stop %s failed: %v\n", ch.verb, orphanService, err)
		}
		if err := d.removeUnit(dir, ch.orphan); err != nil {
			return refuse("%s: remove unit %s failed: %v\n", ch.verb, ch.orphan, err)
		}
		mutated.RecordUnit(ch.orphan)
	}

	if len(plan.Changed) > 0 {
		if err := d.writeUnits(plan, dir); err != nil {
			return refuse("%s: write units failed: %v\n", ch.verb, err)
		}
		for _, u := range plan.Changed {
			mutated.RecordUnit(u.Name)
		}
		fmt.Fprintf(out, "wrote %d unit(s) to %s\n", len(plan.Changed), dir)
	}
	if err := d.daemonReload(); err != nil {
		return refuse("%s: daemon-reload failed: %v\n", ch.verb, err)
	}

	if ch.startUnit != "" {
		service := unitServiceName(ch.startUnit)
		mutated.RecordStart(service)
		if err := d.start(service); err != nil {
			return refuse("%s: start %s failed: %v\n", ch.verb, service, err)
		}
	}

	// The chat UI's connection env lists every resident endpoint, so its unit changes
	// whenever the set does. Without this restart the new slot is reachable on its port
	// but invisible in the chat UI, which is the whole point of admitting it.
	if unitsContain(plan.Changed, orchestrate.OpenWebUIContainerUnitName()) && prior.WasRunning(openWebUIServiceName) {
		mutated.RecordStart(openWebUIServiceName)
		if err := d.restart(openWebUIServiceName); err != nil {
			return refuse("%s: restart %s failed: %v\n", ch.verb, openWebUIServiceName, err)
		}
		d.reconcileChatEndpoints(out, errOut, ch.next)
	}

	return exitPass
}

// reconcileChatEndpoints points Open WebUI's STORED connection list at the resident
// set. It is deliberately NOT fatal and never triggers the rollback: by this point the
// units are written and every slot is serving, so the stack is correct and only the
// chat UI's own record of it lags. Tearing down a correct stack because a UI-side
// write failed would be the worse outcome, so the failure is reported with the command
// that repairs it instead.
func (d *residentDeps) reconcileChatEndpoints(out, errOut io.Writer, cfg config.VillaConfig) {
	if d.syncEndpoints == nil {
		return
	}
	want, err := residentEndpoints(cfg)
	if err != nil {
		fmt.Fprintf(errOut, "warning: could not compose the chat endpoint list: %v\n", err)
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), owuiSyncTimeout)
	defer cancel()
	res, err := d.syncEndpoints(ctx, cfg.ChatPort, want)
	for attempt := 1; err != nil && attempt < owuiSyncAttempts && ctx.Err() == nil; attempt++ {
		time.Sleep(d.syncRetryDelay)
		res, err = d.syncEndpoints(ctx, cfg.ChatPort, want)
	}
	if err != nil {
		fmt.Fprintf(errOut, "warning: the slot is serving but the chat UI still lists the old connections: %v\n", err)
		fmt.Fprintf(errOut, "         re-run this command once Open WebUI is reachable, or add the connection in its admin settings\n")
		return
	}
	if res.Wrote {
		fmt.Fprintf(out, "chat UI now lists %d inference connection(s)\n", len(res.Endpoints))
	}
}

// residentEndpoints is the ordered in-network endpoint list for cfg, primary FIRST.
// Every URL is composed by orchestrate, never re-typed here, so this list and the
// rendered Environment= line cannot drift.
func residentEndpoints(cfg config.VillaConfig) ([]string, error) {
	want := []string{orchestrate.LlamaInNetworkEndpoint()}
	for _, r := range cfg.Resident {
		url, err := orchestrate.ResidentInNetworkEndpoint(r.Model)
		if err != nil {
			return nil, err
		}
		want = append(want, url)
	}
	return want, nil
}

// capturePrior reads the pre-mutation state the rollback restores: the config, the
// text of every unit this change could touch, and which of the affected services were
// running. Model weights are deliberately not captured — the same reasoning install
// records in ADR-0003: they are large, inert on their own, and a retry should not
// re-download them.
func (d *residentDeps) capturePrior(ch residentChange, dir string, plan orchestrate.Plan) install.Prior {
	names := make([]string, 0, len(plan.Changed)+len(plan.Unchanged)+1)
	for _, u := range plan.Changed {
		names = append(names, u.Name)
	}
	for _, u := range plan.Unchanged {
		names = append(names, u.Name)
	}
	if ch.orphan != "" {
		names = append(names, ch.orphan)
	}

	priorUnits := map[string]string{}
	services := map[string]bool{installServiceName: true, openWebUIServiceName: true}
	for _, name := range names {
		if text, ok := d.readUnit(dir, name); ok {
			priorUnits[name] = text
		}
		if strings.HasSuffix(name, ".container") {
			services[unitServiceName(name)] = true
		}
	}

	priorRunning := map[string]bool{}
	for service := range services {
		if state, err := d.isActive(service); err == nil && state == "active" {
			priorRunning[service] = true
		}
	}
	// hadConfig is unconditionally true: both verbs refuse before this point unless
	// config.toml already names a primary model or a resident slot, and LoadVilla only
	// reports those from a file that exists. There is therefore no "config this command
	// created" case for the rollback to clean up.
	return install.CapturePrior(ch.prior, true, priorUnits, priorRunning)
}

// residentSlots is the residentset view of what is resident right now: the primary
// first, then every configured slot, each sized by the SAME recommend fit math the
// candidate is sized by. A slot whose model has left the catalog is a hard error —
// silently sizing it at zero would admit a candidate that cannot actually fit.
//
// Slot.Bytes carries weights + KV only. The headroom term is a property of the
// envelope, not of a model, so the caller passes it once as Policy.HeadroomBytes.
func (d *residentDeps) residentSlots(cfg config.VillaConfig, primaryPort int) ([]residentset.Slot, error) {
	if cfg.Model == "" {
		return nil, fmt.Errorf("no primary model is configured — run `villa recommend --save` or `villa install` first")
	}
	all := append([]config.ResidentModel{{Model: cfg.Model, Quant: cfg.Quant, Ctx: cfg.Ctx, Port: primaryPort}}, cfg.Resident...)

	slots := make([]residentset.Slot, 0, len(all))
	for i, r := range all {
		m, ok := d.resolveCatalog(r.Model)
		if !ok {
			return nil, fmt.Errorf("configured model %q is not in the catalog — cannot size the resident set", r.Model)
		}
		rec := d.fit(m, r.Ctx)
		if rec.Model == "" {
			return nil, fmt.Errorf("could not size configured model %q against the detected memory envelope", r.Model)
		}
		primary := i == 0
		unit := installServiceName
		if !primary {
			name, err := orchestrate.ResidentUnitName(r.Model)
			if err != nil {
				return nil, err
			}
			unit = name + ".service"
		}
		slots = append(slots, residentset.Slot{
			Model:   r.Model,
			Quant:   r.Quant,
			Ctx:     r.Ctx,
			Port:    r.Port,
			Unit:    unit,
			Primary: primary,
			Bytes:   rec.WeightBytes + rec.KVCacheBytes,
		})
	}
	return slots, nil
}

// activeState reports a service's systemd state verbatim, degrading an unreadable
// state to "unknown" rather than to a confident "inactive".
func (d *residentDeps) activeState(service string) string {
	state, err := d.isActive(service)
	if err != nil || state == "" {
		return "unknown"
	}
	return state
}

// allocResidentPort returns the lowest host port at or above residentPortBase that
// neither the primary nor any configured slot already claims, or 0 when the range is
// exhausted. Ports are explicit in config precisely so removing a middle slot does not
// renumber the ones after it, which is why this only ever fills the lowest gap.
func allocResidentPort(primaryPort int, slots []config.ResidentModel) int {
	taken := map[int]bool{primaryPort: true}
	for _, s := range slots {
		taken[s.Port] = true
	}
	for p := residentPortBase; p <= residentPortCeiling; p++ {
		if !taken[p] {
			return p
		}
	}
	return 0
}

// unitsContain reports whether a rendered unit slice holds the named unit file. It is
// deliberately narrower than install.UnitPresent, which also scans the unchanged half:
// the chat UI is restarted only when its unit ACTUALLY changed.
func unitsContain(units []orchestrate.Unit, name string) bool {
	return slices.ContainsFunc(units, func(u orchestrate.Unit) bool { return u.Name == name })
}

// liveResidentDeps wires the resident verbs to the real host, reusing the seams the
// rest of the CLI already uses: the catalog, recommend.Pick, the verified downloader
// behind pullFn, config.SaveVilla, the orchestrate render/reconcile/write core, and
// the systemd seam. Nothing here is new machinery.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the pull
// closure so Ctrl-C can interrupt the multi-GB transfer `resident add` may start.
// Cancelling mid-stream is safe: the partial ".part" file is kept and resumed.
func liveResidentDeps(ctx context.Context) *residentDeps {
	sys := orchestrate.NewSystemd()
	return &residentDeps{
		loadConfig: config.LoadVilla,
		saveConfig: config.SaveVilla,
		resolveCatalog: func(id string) (catalog.Model, bool) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return catalog.Model{}, false
			}
			return cat.FindByID(id)
		},
		fit: func(m catalog.Model, ctx int) recommend.Recommendation {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return recommend.Recommendation{}
			}
			// The override path re-validates the named model against the detected
			// envelope, threading the persisted memory/web-search reservations so a
			// resident slot is sized against the same shrunken envelope the primary was.
			return recommend.Pick(detect.Probe(), cat, recommend.Overrides{Model: m.ID, Ctx: ctx}, liveLoadedMemoryInputs(), liveLoadedWebSearchInputs())
		},
		primaryPort: inference.ServerPort,
		isDownloaded: func(m catalog.Model) bool {
			_, err := os.Stat(filepath.Join(modelsDir(), primaryModelFile(m)))
			return err == nil
		},
		pull: func(m catalog.Model) error {
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(ctx, m, dir)
		},
		renderUnits: liveRenderUnits,
		unitDir:     quadletUnitDir,
		reconcile:   orchestrate.Reconcile,
		writeUnits:  orchestrate.WriteUnits,
		readUnit: func(dir, name string) (string, bool) {
			// Containment is checked, not asserted: removeUnit directly below takes the
			// same dir and name and guards them, and a read that only claims safety in a
			// comment drifts from its sibling the moment either caller changes.
			path := filepath.Join(dir, name)
			if err := assertWithinDir(path, dir); err != nil {
				return "", false
			}
			b, err := os.ReadFile(path)
			if err != nil {
				return "", false
			}
			return string(b), true
		},
		removeUnit: func(dir, name string) error {
			if err := assertWithinDir(filepath.Join(dir, name), dir); err != nil {
				return err
			}
			err := os.Remove(filepath.Join(dir, name))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
			return nil
		},
		daemonReload:   sys.DaemonReload,
		start:          sys.Start,
		stop:           sys.Stop,
		restart:        sys.Restart,
		isActive:       sys.IsActive,
		syncRetryDelay: time.Second,
		syncEndpoints: func(ctx context.Context, chatPort int, want []string) (openwebui.EndpointSync, error) {
			c := liveOpenWebUIClient(owuiLoopbackBase(chatPort))
			token, err := liveOpenWebUISignIn(ctx, c)
			if err != nil {
				return openwebui.EndpointSync{}, err
			}
			return c.SyncEndpoints(ctx, token, want)
		},
	}
}

// liveRenderUnits renders the whole stack from cfg, including the resident slots. It
// is the resident verbs' render seam and the one place the resident catalog handoff is
// assembled for a full render.
func liveRenderUnits(cfg config.VillaConfig) ([]orchestrate.Unit, error) {
	modelFile, err := liveModelFile(cfg)
	if err != nil {
		return nil, err
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return nil, err
	}
	resident, err := liveResidentUnits(cfg)
	if err != nil {
		return nil, err
	}
	return livePinnedRender(orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     modelFile,
		ModelsDir:     modelsDir(),
		HostVillaPath: hostVillaPath(),
		Resident:      resident,
	})
}

// liveResidentUnits resolves every configured resident slot's catalog id to its GGUF
// filename — the handoff orchestrate.RenderInput requires so the pure renderer never
// imports the catalog. An unresolvable id is a hard error: a unit whose -m points at a
// fabricated filename fails only at container start, long after the command reported
// success.
func liveResidentUnits(cfg config.VillaConfig) ([]orchestrate.ResidentUnit, error) {
	if len(cfg.Resident) == 0 {
		return nil, nil
	}
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return nil, fmt.Errorf("load model catalog: %w", err)
	}
	units := make([]orchestrate.ResidentUnit, 0, len(cfg.Resident))
	for _, r := range cfg.Resident {
		m, ok := cat.FindByID(r.Model)
		if !ok {
			return nil, fmt.Errorf("resident model %q is not in the catalog — cannot resolve its weight file", r.Model)
		}
		units = append(units, orchestrate.ResidentUnit{
			Model:     r.Model,
			ModelFile: primaryModelFile(m),
			Ctx:       r.Ctx,
			Port:      r.Port,
		})
	}
	return units, nil
}
