package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"strconv"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install.go wires the `villa install` lifecycle verb (CLI-01/07, ORCH-03,
// PRIV-01): the single command that turns Phase-2's manual `podman run` into a
// managed, idempotent, boot-survivable bring-up driven from config.toml.
//
// runInstall mirrors runInference's return-code-not-Exit discipline (the cobra
// RunE calls os.Exit; runInstall RETURNS 0/2/1 so tests drive it). It reuses the
// Plan-01 orchestrate core (Render→Reconcile→WriteUnits→Systemd) and the
// Phase-1/2 preflight/recommend seams. Every host-touching action — preflight
// probe, privileged setsebool/loginctl prep, model auto-pull, config persist,
// unit write, systemctl, readiness poll — is an injectable field on installDeps
// so install_test.go exercises the whole flow without a live GPU/podman/systemd/
// SELinux/network host.
//
// Privileged host-prep (D-04/D-05) is OFFERED per-step with the exact command
// shown and run only on explicit y; declined / --json / non-interactive falls
// back to printing the command and treats the gap as a BLOCK (overridable via the
// global --force). villa never silently runs a privileged command.
//
// Two install-completeness guarantees (Phase-03 UAT fixes F-1/F-2) live in the
// flow after the preflight gate and before the unit write/start, mirroring
// `model swap` (cmd/villa/model.go):
//   - F-1 ensureModel: the recommended GGUF must be present on disk BEFORE the
//     unit starts — pull-if-missing via the same download/catalog seam swap uses,
//     short-circuited when already present, skipped under --dry-run. Without it
//     llama-server crash-loops on a missing weight file ("just works" violated).
//   - F-2 saveConfig: the chosen model/quant/ctx/backend is persisted to
//     config.toml via the same 0600 traversal-guarded writer config set / swap
//     use, BEFORE the units are written, skipped under --dry-run. Without it the
//     lifecycle verbs (up/restart) render from an empty config and FAIL, and
//     install-written units never match config-rendered units (no true no-op).

// installOpts are the per-invocation flags for `villa install`. --force and --json
// are read from the global persistent flags (force/jsonOut); --dry-run is local.
type installOpts struct {
	// dryRun prints the rendered changed units and writes nothing (ORCH SC#1).
	dryRun bool
	// force overrides an un-consented BLOCK-tier preflight gap (auditable).
	force bool
	// json suppresses interactive consent (a --json run is non-interactive).
	json bool
	// noTUI opts out of the guided wizard to the flag-driven install path
	// (D-01/D-08). Bare `villa install` on a TTY launches the wizard; --no-tui (or
	// --json, or a non-TTY stdin/stdout) forces today's flag path verbatim. There is
	// NO --tui opt-in and NO `villa setup` subcommand — one progressively-enhanced verb.
	noTUI bool
}

// installReadiness is the readiness-poll verdict (Task 2): PASS once the service
// is active and /health returns 200, WARN when the poll could not confirm
// readiness (timeout / typed-Unknown — never a confident FAIL on a 503, WR-07).
type installReadiness struct {
	status preflight.Status
	detail string
}

// installDeps are the injectable seams runInstall drives. Defaults wire the real
// host (liveInstallDeps); install_test.go replaces them with stubs.
type installDeps struct {
	probe func() detect.HostProfile
	// pick recommends a fitting model. It takes recommend.Overrides so a wizard
	// model choice is re-validated through the SINGLE polymorphism point
	// (recommend.Pick) rather than a forked catalog re-derivation (D-02): the flag
	// path passes recommend.Overrides{} (today's behavior, byte-for-byte), the wizard
	// passes recommend.Overrides{Model: chosen}.
	pick       func(detect.HostProfile, recommend.Overrides) recommend.Recommendation
	modelFile  func(recommend.Recommendation) (string, error)
	modelsDir  func() string
	runChecks  func(detect.HostProfile, preflight.ResourceReq) []preflight.CheckResult
	render     func(orchestrate.RenderInput) ([]orchestrate.Unit, error)
	reconcile  func([]orchestrate.Unit, string) (orchestrate.Plan, error)
	writeUnits func(orchestrate.Plan, string) error
	unitDir    func() (string, error)

	// modelDownloaded reports whether the recommended model's weights are already
	// on disk (F-1). When true, ensureModel is NOT called — install never re-pulls
	// a present model (idempotency / strictly-local: no needless network).
	modelDownloaded func(recommend.Recommendation) bool
	// ensureModel auto-pulls the recommended model's verified weights into the
	// models dir (F-1). It reuses the same download/catalog seam `model swap` uses
	// and is invoked only when modelDownloaded reports false and not under --dry-run.
	ensureModel func(recommend.Recommendation) error
	// saveConfig persists the chosen model/quant/ctx/backend to config.toml (F-2)
	// via the same 0600 traversal-guarded writer config set / model swap use. It is
	// invoked BEFORE the units are written and skipped under --dry-run.
	saveConfig func(config.VillaConfig) error

	daemonReload func() error
	start        func(service string) error
	isActive     func(service string) (string, error)
	enableLinger func(user string) error
	setsebool    func() error

	// Dashboard-service seams (Plan 05-05 / D-03/D-04): the dashboard is a NATIVE
	// systemd --user .service (the villa binary running `villa dashboard`), NOT a
	// Quadlet .container — so it is rendered+written separately into userUnitDir
	// (~/.config/systemd/user, NOT the Quadlet dir — Pitfall 5), then enabled (for
	// boot-survival, [Install] WantedBy=default.target) and started AFTER the
	// container services. enable mirrors start (fixed-arg systemctl --user enable).
	userUnitDir func() (string, error)
	// writeDashboardUnit writes the native dashboard .service into dir with an
	// ExecStart pointed at binaryPath. binaryPath is resolved by the caller (impure
	// os.Executable resolution via resolveBinaryPath) and threaded in so the unit's
	// ExecStart targets the ACTUAL running villa binary — correct for both a dev build
	// (./villa from the repo) and an installed binary — instead of the old fixed
	// ~/.local/bin/villa the install flow never populated (UAT Test 5: 203/EXEC at boot).
	writeDashboardUnit func(dir, binaryPath string) error
	// readDashboardUnit reads the current on-disk dashboard unit (dir is the
	// userUnitDir; the file is orchestrate.DashboardServiceName) so reconcileDashboardUnit
	// can render-vs-disk compare and stay a true no-op when the unit already matches
	// (UAT Test 5 gap close, 05-08). It returns the existing unit bytes for that compare;
	// a not-exist read (os.IsNotExist) is reported as "no unit on disk" and treated as a
	// diff (must write), NOT a fatal error — an absent unit is the normal first-install
	// state. Any OTHER read error (present-but-unreadable) is fatal.
	readDashboardUnit func(dir string) ([]byte, error)
	// resolveBinaryPath returns the stable absolute path of the running villa binary
	// (os.Executable→EvalSymlinks→Abs). It is the single impure resolution seam; the
	// renderer stays pure. A fatal resolution error (os.Executable or filepath.Abs)
	// fails the install — it NEVER falls back to a fixed path like ~/.local/bin/villa
	// (the root cause of UAT Test 5). A non-fatal EvalSymlinks failure degrades to the
	// raw os.Executable path, which is still the running binary and still absolute.
	resolveBinaryPath func() (string, error)
	enable            func(service string) error

	username    func() string
	endpoint    func() string
	interactive func() bool
	consent     func(prompt string) bool
	pollReady   func(ctx context.Context, endpoint string) installReadiness

	// Memory-stack seams (Phase-19 / D-04/D-07, INFRA-02/PRIV-04). All gated on the
	// PERSISTED memory_enabled (loadedMemoryEnabled), skipped under --dry-run.
	//
	// loadedConfig returns the PERSISTED config.LoadVilla() (fail-soft to typed
	// defaults on a load error). runInstall SEEDS cfg from this instead of
	// DefaultVillaConfig(), then overrides ONLY the recommendation-derived fields
	// (Model/Quant/Ctx/Backend) and the MemoryEnabled gate — so a user's persisted
	// memory fields (qdrant_addr/port, embed_addr/port, embedding_model/dim) and the
	// dashboard/chat fields are PRESERVED through saveConfig, never silently reset to
	// seed defaults on every install (WR-02). LoadVilla self-heals zeroed dashboard/
	// chat fields, so seeding from it keeps the gap-test:1b loopback-default guarantee
	// while honoring any persisted customization.
	loadedConfig func() config.VillaConfig
	// loadedMemoryEnabled returns the AUTHORITATIVE gate value: the persisted
	// config.LoadVilla().MemoryEnabled (fail-soft to false on a load error). It is
	// threaded into runInstall to set cfg.MemoryEnabled, REPLACING the always-false
	// DefaultVillaConfig() seed as the memory gate source — so an opted-in user's memory
	// stack is never silently skipped (T-19-16). This is the one seam the whole memory
	// path keys off; binding it to the seed's hard-coded false would be the silent-failure
	// bug this plan exists to prevent.
	loadedMemoryEnabled func() bool
	// embedModelPresent reports whether the pre-staged embed GGUF is already on disk
	// (the ensureEmbedModel idempotency guard — a present file is never re-pulled).
	embedModelPresent func(modelsDir string) bool
	// ensureEmbedModel pre-stages the nomic embed GGUF into modelsDir via the verified
	// download path (D-07), invoked only when memory is on, not dry-run, and absent.
	ensureEmbedModel func(modelsDir string) error
	// memoryProofFn asserts the memory stack is healthy: an offline 768-dim
	// /v1/embeddings vector AND a Qdrant writable round-trip. A FAIL refuses-with-
	// remediation (exitBlocked), never a silent skip (D-09). Invoked only when memory
	// is on and not dry-run.
	memoryProofFn func(ctx context.Context, in memoryProofInput) memoryProof

	// stdoutIsTTY reports whether stdout is a real terminal — the stdout twin of
	// interactive() (which checks stdin). huh renders to stdout/stderr, so BOTH must
	// be a TTY for the styled wizard to make sense (D-01/D-08). The seam wraps the
	// stdoutIsTTY() helper from tui_theme.go so tests can inject a fake TTY result.
	stdoutIsTTY func() bool
	// wizard runs the guided huh 5-screen install wizard and RETURNS the collected
	// choices (a model override + per-item privileged consent) — it is a PURE
	// COLLECTOR (D-01/D-04): it presents the already-computed profile/rec/checks/
	// backend and NEVER executes a host fix. The single gateInstall in runInstall
	// consumes the collected consent. tests inject a fake returning a canned
	// wizardResult. NO internal/* core imports huh (D-11); the live impl is liveWizard
	// in install_wizard.go.
	wizard func(ctx context.Context, in wizardInput) (wizardResult, error)
}

// installServiceName is the systemd service the inference .container generates
// (Quadlet maps villa-llama.container → villa-llama.service).
const installServiceName = "villa-llama.service"

// openWebUIServiceName is the systemd service the Open WebUI .container generates
// (Quadlet maps villa-openwebui.container → villa-openwebui.service, the same
// .container→.service rule serviceUnits encodes). It is started AFTER inference
// (D-05) so the chat UI comes up against a live backend.
const openWebUIServiceName = "villa-openwebui.service"

// newInstall builds `villa install`: detect → recommend → preflight gate →
// consented host-prep → render → reconcile → write → daemon-reload → start →
// readiness poll, idempotent and --dry-run aware.
func newInstall() *cobra.Command {
	var dryRun bool
	var noTUI bool
	cmd := &cobra.Command{
		Use:   "install",
		Short: "Detect, recommend, gate, generate, and bring up the local inference stack",
		Long: "Run the full managed bring-up: detect the host, recommend a fitting model, gate on a " +
			"safe host (offering privileged host-prep with per-step consent), ensure the recommended model " +
			"is downloaded, persist the selection to config.toml, render rootless Podman Quadlet units from " +
			"config, write only what changed, daemon-reload, start, and poll readiness — then print the " +
			"loopback inference endpoint. Re-running with unchanged config is a true no-op. --dry-run prints " +
			"the rendered units and writes nothing (no pull, no config write). Strictly local.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveInstallDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "install: %v\n", err)
				os.Exit(exitBlocked)
			}
			code := runInstall(cmd, installOpts{dryRun: dryRun, force: force, json: jsonOut, noTUI: noTUI}, deps)
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered units without writing, pulling, or starting anything")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the guided wizard; use the flag-driven install path")
	return cmd
}

// runInstall executes the install flow and RETURNS the exit code (0 pass / 2 warn
// / 1 block) — it never calls os.Exit, so tests drive it. All printing + exit
// mapping live here; the orchestrate/preflight/recommend libraries stay pure.
func runInstall(cmd *cobra.Command, opts installOpts, d *installDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// (1) Detect the host.
	profile := d.probe()

	// (2) Recommend a concrete model. A refusal (empty Model / zero ctx / zero
	// weight) is a clear FAIL — never start llama-server with -c 0 / no fit.
	rec := d.pick(profile, recommend.Overrides{})
	if rec.Model == "" || rec.ContextLen <= 0 || rec.WeightBytes == 0 {
		// Emit the contracted empty-state copy (17-UI-SPEC.md:195) verbatim, with the
		// `<N> GiB` token substituted from the detected usable envelope. A typed-Unknown
		// envelope renders "unknown GiB usable" (never a fabricated 0). This branch fires
		// BEFORE the wizard is evaluated, so it covers both the flag and wizard paths from
		// one emission point — the parenthetical confirms the --no-tui path is identical.
		fmt.Fprintf(errOut, "No catalog model fits the detected memory envelope (%s usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)\n", gibUsableEnvelope(profile.UsableEnvelopeBytes))
		return exitBlocked
	}
	fmt.Fprintf(out, "selected model %s (ctx %d, %s)\n", rec.Model, rec.ContextLen, gib(rec.WeightBytes))

	// (3) Preflight gate with the concrete model's resource requirement. The
	// embedding reservation is included (Research Open Question 2 resolved YES —
	// more honest): the value flows from the pick so memory.Footprint stays the
	// single source; it is zero when memory is off, leaving the off-path gate
	// unchanged.
	req := preflight.ResourceReq{
		MinDiskBytes: rec.WeightBytes,
		MinMemBytes:  rec.WeightBytes + rec.KVCacheBytes + rec.HeadroomBytes + rec.EmbeddingReservationBytes,
		DataDir:      d.modelsDir(),
	}
	checks := d.runChecks(profile, req)

	// (3b) Guided wizard (D-01/D-08) — the PINNED composition. probe/pick/runChecks
	// (steps 1-3) have already run exactly once; the wizard RECEIVES their results,
	// COLLECTS a model override + per-item privileged consent, and RETURNS here. It
	// NEVER runs runGapFix/resolveGap/offerNonBlockingGap itself — the single
	// gateInstall below consumes the threaded consent, so both paths converge on one
	// gate execution (SC#1/SC#2; privileged fix at most once; D-04/D-06 preserved).
	// nil consentDecisions ⇒ flag path: gateInstall prompts via d.consent as today.
	var consentDecisions map[string]bool
	useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY()
	if useWizard {
		// Resolve the backend for the review screen via the single polymorphism point
		// (never a re-typed image literal). On an unknown backend, fall through to the
		// flag path rather than aborting the install.
		backend, berr := inference.BackendFor(rec.Backend)
		if berr != nil {
			fmt.Fprintf(errOut, "install: resolve backend for wizard: %v — falling back to the flag path\n", berr)
		} else {
			res, werr := d.wizard(cmd.Context(), wizardInput{
				profile:      profile,
				rec:          rec,
				alternatives: rec.Alternatives,
				checks:       checks,
				backend:      backend,
				colorEnabled: colorEnabled(),
			})
			if werr != nil {
				// Esc / Ctrl+C / Cancel → clean abort: no mutation, nothing written,
				// pulled, or persisted. Return a non-zero code (never os.Exit here).
				fmt.Fprintf(errOut, "Install cancelled — no changes were made. Re-run villa install, or villa install --no-tui for the flag-driven path.\n")
				return exitBlocked
			}
			// Re-validate a chosen model override through the SAME single pick seam
			// (the pinned override mechanism) so the resulting rec is byte-identical to
			// the flag path's `recommend --model <id>`. The wizard computes no fit; the
			// override is constrained to catalog ids surfaced in rec.Alternatives.
			// Preflight checks are host-prep (model-independent), so they are NOT re-run.
			if res.modelOverride != "" {
				rec = d.pick(profile, recommend.Overrides{Model: res.modelOverride})
			}
			consentDecisions = res.consentDecisions
		}
	}

	gateCode, ok := gateInstall(out, errOut, checks, opts, consentDecisions, d)
	if !ok {
		return gateCode
	}
	// A forced-override gate degrades the final verdict to WARN even on an
	// otherwise-clean bring-up: the host-prep gap was bypassed, not satisfied.
	gateDegraded := gateCode == exitWarn

	// (4) Render the units from config + backend, then reconcile against disk.
	unitDir, err := d.unitDir()
	if err != nil {
		fmt.Fprintf(errOut, "install: cannot resolve the Quadlet unit dir: %v\n", err)
		return exitBlocked
	}
	// Seed from the PERSISTED config (not DefaultVillaConfig()) so a user's customized
	// memory fields (qdrant_addr/port, embed_addr/port, embedding_model/dim) and the
	// dashboard/chat fields survive every install rather than being reset to seed
	// defaults (WR-02). loadedConfig() fails soft to typed defaults on a load error and
	// LoadVilla self-heals zeroed dashboard/chat fields, so this still guarantees the
	// loopback dashboard/chat defaults (8888/3000/127.0.0.1, gap test:1b) for a host
	// with no prior config. Only the recommendation-derived fields are overridden below.
	cfg := d.loadedConfig()
	cfg.Model = rec.Model
	cfg.Quant = rec.Quant
	cfg.Ctx = rec.ContextLen
	cfg.Backend = rec.Backend
	// AUTHORITATIVE memory gate (Phase-19 / T-19-16): the memory path keys off the
	// PERSISTED config.LoadVilla().MemoryEnabled (via the loadedMemoryEnabled seam). It is
	// the single gate value the pre-stage + start + proof steps read. (Seeding cfg from the
	// persisted config above already carries the persisted MemoryEnabled; this is an
	// explicit re-bind through the dedicated gate seam so the gate source stays a single,
	// testable seam regardless of how cfg was seeded.)
	cfg.MemoryEnabled = d.loadedMemoryEnabled()
	modelFile, err := d.modelFile(rec)
	if err != nil {
		fmt.Fprintf(errOut, "install: resolve model file: %v\n", err)
		return exitBlocked
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		fmt.Fprintf(errOut, "install: resolve backend: %v\n", err)
		return exitBlocked
	}
	units, err := d.render(orchestrate.RenderInput{
		Backend:   backend,
		Cfg:       cfg,
		ModelFile: modelFile,
		ModelsDir: d.modelsDir(),
	})
	if err != nil {
		fmt.Fprintf(errOut, "install: render failed: %v\n", err)
		return exitBlocked
	}
	plan, err := d.reconcile(units, unitDir)
	if err != nil {
		fmt.Fprintf(errOut, "install: reconcile failed: %v\n", err)
		return exitBlocked
	}

	// (5) --dry-run: print the changed unit text and stop — write nothing, pull
	// nothing, persist nothing (ORCH SC#1: a dry run has zero side effects).
	if opts.dryRun {
		if len(plan.Changed) == 0 {
			fmt.Fprintf(out, "dry-run: no changes — units already match config\n")
			return exitPass
		}
		for _, u := range plan.Changed {
			fmt.Fprintf(out, "# %s\n%s\n", u.Name, u.Text)
		}
		fmt.Fprintf(out, "dry-run: %d unit(s) would be written (nothing written, no model pulled, no config persisted)\n", len(plan.Changed))
		return exitPass
	}

	// (6) Ensure the recommended model is present BEFORE starting the unit (F-1).
	// Without the weights on disk llama-server crash-loops on the missing GGUF and
	// install reports WARN. Pull only when absent (idempotent, strictly-local: a
	// present model is never re-pulled). This runs on BOTH the no-op and write
	// paths so an existing-units-but-missing-weights host still self-heals.
	if !d.modelDownloaded(rec) {
		fmt.Fprintf(out, "model %s not present — downloading...\n", rec.Model)
		if err := d.ensureModel(rec); err != nil {
			fmt.Fprintf(errOut, "install: download model %s failed: %v\n", rec.Model, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "model %s downloaded and verified\n", rec.Model)
	}

	// (6b) Pre-stage the embedding GGUF into villa-models BEFORE starting villa-embed
	// (Phase-19 / D-07, PRIV-04). Gated on the PERSISTED memory_enabled (cfg.MemoryEnabled,
	// resolved via loadedMemoryEnabled above), skipped under --dry-run, and idempotent: a
	// present file is never re-pulled. This is the one-time install-time controlled pull
	// (the single sanctioned outbound window) so the embeddings runtime is ZERO-download.
	// download.PullModel verifies size + SHA256 before the atomic rename (T-19-06), so a
	// half-written/unverified GGUF is never trusted; a pull failure refuses-with-remediation.
	if cfg.MemoryEnabled && !opts.dryRun && !d.embedModelPresent(d.modelsDir()) {
		fmt.Fprintf(out, "embedding model %s not present — downloading...\n", nomicEmbedShard.Filename)
		if err := d.ensureEmbedModel(d.modelsDir()); err != nil {
			fmt.Fprintf(errOut, "install: pre-stage embedding model %s failed: %v\n", nomicEmbedShard.Filename, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "embedding model %s downloaded and verified\n", nomicEmbedShard.Filename)
	}

	// (7) Persist the chosen selection to config.toml BEFORE any unit work (F-2 /
	// D-09 spirit): config is the single source of truth, so install-written units
	// must derive from the same persisted config the lifecycle verbs render from —
	// otherwise post-install `villa up`/`restart` resolve an empty model and FAIL,
	// and a follow-up reconcile is never a true no-op.
	if err := d.saveConfig(cfg); err != nil {
		fmt.Fprintf(errOut, "install: persist config: %v\n", err)
		return exitBlocked
	}

	// (7b) Reconcile the native control-dashboard unit on BOTH the no-op and write
	// paths (UAT Test 5 / 05-08 gap close), mirroring the ensureModel + saveConfig
	// "runs on BOTH paths" contract above. The dashboard unit's lifecycle was wrongly
	// coupled to the container plan diff: install returned at the len(plan.Changed)==0
	// early return below BEFORE the old lower dashboard block ran, so a re-install on a
	// host with unchanged containers never landed the 05-06 ExecStart fix and the unit
	// stayed stale (203/EXEC at boot). Hoisting the reconcile ABOVE that early return
	// decouples the two lifecycles; reconcileDashboardUnit is itself idempotent (a
	// matching unit triggers zero writes/reloads/restarts), so this stays a true no-op
	// when nothing changed.
	if code := reconcileDashboardUnit(out, errOut, d); code != exitPass {
		return code
	}

	// (8) True no-op: nothing changed → no write, no reload, no restart. Note this
	// is reached only AFTER ensureModel + saveConfig + reconcileDashboardUnit, so a
	// re-run on a host whose units already match still guarantees the weights, config,
	// AND the boot-surviving dashboard unit are in place (the no-op return is safe).
	if len(plan.Changed) == 0 {
		fmt.Fprintf(out, "no changes — stack already matches config\n")
		printPostInstall(out, d.endpoint(), installReadiness{status: preflight.StatusPass, detail: "unchanged"})
		if gateDegraded {
			return exitWarn
		}
		return exitPass
	}

	// (9) Write only the changed units, daemon-reload, then (re)start the service.
	if err := d.writeUnits(plan, unitDir); err != nil {
		fmt.Fprintf(errOut, "install: write units failed: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(out, "wrote %d unit(s) to %s\n", len(plan.Changed), unitDir)
	if err := d.daemonReload(); err != nil {
		fmt.Fprintf(errOut, "install: daemon-reload failed: %v\n", err)
		return exitBlocked
	}
	if err := d.start(installServiceName); err != nil {
		fmt.Fprintf(errOut, "install: start %s failed: %v\n", installServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "started %s\n", installServiceName)
	// Start Open WebUI AFTER inference (D-05): the chat UI must come up against a
	// live backend, and the recommended model is already ensured present above
	// (step 6, MODEL-04) so the model picker is populated on first visit.
	if err := d.start(openWebUIServiceName); err != nil {
		fmt.Fprintf(errOut, "install: start %s failed: %v\n", openWebUIServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "started %s\n", openWebUIServiceName)

	// (9b) Start the memory stack AFTER inference + Open WebUI, gated on the PERSISTED
	// memory_enabled (Phase-19 / INFRA-02). Qdrant FIRST so the embedder/OWUI peers can
	// reach the vector store, then villa-embed (its GGUF is already pre-staged above —
	// Pitfall 4). Each start failure refuses-with-remediation (exitBlocked), mirroring the
	// inference/OWUI start handling. Skipped under --dry-run (the dry-run path returns far above).
	//
	// The start is gated on the memory .container units being PRESENT in the written plan
	// (plan.Changed ∪ plan.Unchanged), not solely on cfg.MemoryEnabled (WR-04). With memory
	// on, Render appends those units and reconcile diffs them in, so today they are always
	// present — but if a future change ever lets MemoryEnabled be true while the units are
	// filtered out of the plan (a swallowed partial render, a reconcile that drops them), we
	// must NOT `systemctl start villa-qdrant.service` for a unit systemd has never seen and
	// surface a raw "Unit not found". Instead fail closed with a clear INTERNAL-ERROR
	// remediation so the gate for STARTING a service is "its unit exists in the plan".
	if cfg.MemoryEnabled {
		if !planHasUnit(plan, orchestrate.QdrantContainerUnitName()) ||
			!planHasUnit(plan, orchestrate.EmbedContainerUnitName()) {
			fmt.Fprintf(errOut, "install: INTERNAL ERROR: memory is enabled but the memory units (%s, %s) are absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\n",
				orchestrate.QdrantContainerUnitName(), orchestrate.EmbedContainerUnitName())
			return exitBlocked
		}
		if err := d.start(qdrantServiceName); err != nil {
			fmt.Fprintf(errOut, "install: start %s failed: %v\n", qdrantServiceName, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "started %s\n", qdrantServiceName)
		if err := d.start(embedServiceName); err != nil {
			fmt.Fprintf(errOut, "install: start %s failed: %v\n", embedServiceName, err)
			return exitBlocked
		}
		fmt.Fprintf(out, "started %s\n", embedServiceName)
	}

	// (10) Poll readiness (503=keep-polling, timeout→WARN — Task 2 wiring).
	ready := d.pollReady(cmd.Context(), d.endpoint())
	printPostInstall(out, d.endpoint(), ready)

	// (10b) Memory-stack readiness proof (Phase-19 / D-09, SC#2/SC#3): an OFFLINE
	// 768-dim /v1/embeddings vector AND a Qdrant writable round-trip. Gated on the
	// PERSISTED memory_enabled (cfg.MemoryEnabled); skipped under --dry-run (that path
	// returned far above). A FAIL refuses-with-remediation (exitBlocked) — never a
	// silent skip / false-green (honesty-by-construction). A PASS prints a ready line
	// and folds into the existing PASS/WARN verdict.
	if cfg.MemoryEnabled {
		proof := d.memoryProofFn(cmd.Context(), memoryProofInput{
			embedAddr:    cfg.EmbedAddr,
			embedPort:    cfg.EmbedPort,
			embedModel:   cfg.EmbeddingModel,
			embeddingDim: cfg.EmbeddingDim,
			qdrantAddr:   cfg.QdrantAddr,
			qdrantPort:   cfg.QdrantPort,
		})
		if proof.status == preflight.StatusFail {
			fmt.Fprintf(errOut, "install: memory stack not ready: %s\n", proof.detail)
			return exitBlocked
		}
		// Print the proof's own detail (IN-02) rather than re-typing the "768-dim …"
		// figure as a literal — the dimension is single-sourced in the verdict
		// (evalMemoryProof, from cfg.EmbeddingDim), so a dim change can't leave this stale.
		fmt.Fprintf(out, "memory stack ready: %s\n", proof.detail)
	}

	if ready.status == preflight.StatusWarn || gateDegraded {
		return exitWarn
	}
	return exitPass
}

// planHasUnit reports whether a unit with the given name is present in the reconciled
// plan — in either Changed (must (re)write) or Unchanged (already on disk). The install
// flow uses it to gate the memory-service starts on the memory .container units actually
// being part of the written plan (WR-04), so a memory-on install never `systemctl start`s
// a unit systemd has never seen.
func planHasUnit(plan orchestrate.Plan, name string) bool {
	for _, u := range plan.Changed {
		if u.Name == name {
			return true
		}
	}
	for _, u := range plan.Unchanged {
		if u.Name == name {
			return true
		}
	}
	return false
}

// reconcileDashboardUnit brings up the native control-dashboard .service idempotently
// and returns an exit-code sentinel (exitPass on success — whether or not it had to
// write; exitBlocked on any hard failure). It runs on BOTH the no-op and write install
// paths (called before the len(plan.Changed)==0 early return in runInstall), so a
// re-install on a host with unchanged containers still repairs/updates the dashboard
// unit and keeps it boot-surviving (UAT Test 5 / 05-08 gap close).
//
// Idempotency: it renders the expected unit bytes via the PURE orchestrate.RenderDashboardUnit
// (so the compare can never drift from what WriteDashboardUnit writes), compares them to
// the on-disk unit read through the readDashboardUnit seam, and ONLY writes →
// daemon-reload → enable → (re)start when the bytes differ (or the unit is absent). When
// the on-disk unit already matches, it does nothing host-mutating — preserving the "true
// no-op" guarantee (no daemon-reload, no restart, exit code unperturbed).
//
// The running villa binary path is resolved fail-closed via resolveBinaryPath (no
// ~/.local/bin/villa fallback, WR-03) on this path too: an unresolvable binary fails the
// install closed rather than writing a unit that points at an attacker-plantable fixed path.
func reconcileDashboardUnit(out, errOut io.Writer, d *installDeps) int {
	// Resolve the user-unit dir (NOT the Quadlet dir — Pitfall 5).
	udir, err := d.userUnitDir()
	if err != nil {
		fmt.Fprintf(errOut, "install: cannot resolve the user-unit dir for the dashboard: %v\n", err)
		return exitBlocked
	}
	// Resolve the running villa binary's absolute, symlink-collapsed path for ExecStart
	// (UAT Test 5 fix). A resolution failure is fatal — we do NOT fall back to the old
	// fixed ~/.local/bin/villa, which is the exact path that produced 203/EXEC at boot
	// when the install flow never deployed the binary (WR-03, fail-closed).
	binPath, err := d.resolveBinaryPath()
	if err != nil {
		fmt.Fprintf(errOut, "install: cannot resolve the villa binary path for the dashboard unit: %v\n", err)
		return exitBlocked
	}
	// Compute the expected unit bytes via the PURE renderer so the idempotency compare
	// is exactly what WriteDashboardUnit would write (no drift).
	expected, err := orchestrate.RenderDashboardUnit(binPath)
	if err != nil {
		fmt.Fprintf(errOut, "install: render dashboard unit failed: %v\n", err)
		return exitBlocked
	}
	// Read the current on-disk unit. An absent unit (os.IsNotExist) is the normal
	// first-install state → treat as empty (must write). Any OTHER read error is a real
	// problem (a present-but-unreadable unit) → fatal.
	current, err := d.readDashboardUnit(udir)
	if err != nil {
		if !os.IsNotExist(err) {
			fmt.Fprintf(errOut, "install: read dashboard unit failed: %v\n", err)
			return exitBlocked
		}
		current = nil // absent → diff → must write
	}
	// Already current: do nothing host-mutating (no write/reload/enable/restart) — the
	// idempotent "true no-op" guarantee. A quiet non-mutating note is fine.
	if bytes.Equal(current, []byte(expected)) {
		fmt.Fprintf(out, "dashboard unit already current\n")
		return exitPass
	}
	// Differs (or absent): write the unit, daemon-reload so systemd sees it, enable it
	// for boot-survival ([Install] WantedBy=default.target), then (re)start it.
	if err := d.writeDashboardUnit(udir, binPath); err != nil {
		fmt.Fprintf(errOut, "install: write dashboard unit failed: %v\n", err)
		return exitBlocked
	}
	fmt.Fprintf(out, "wrote %s to %s\n", orchestrate.DashboardServiceName, udir)
	if err := d.daemonReload(); err != nil {
		fmt.Fprintf(errOut, "install: daemon-reload (dashboard) failed: %v\n", err)
		return exitBlocked
	}
	if err := d.enable(orchestrate.DashboardServiceName); err != nil {
		fmt.Fprintf(errOut, "install: enable %s failed: %v\n", orchestrate.DashboardServiceName, err)
		return exitBlocked
	}
	if err := d.start(orchestrate.DashboardServiceName); err != nil {
		fmt.Fprintf(errOut, "install: start %s failed: %v\n", orchestrate.DashboardServiceName, err)
		return exitBlocked
	}
	fmt.Fprintf(out, "started %s (boot-survival enabled)\n", orchestrate.DashboardServiceName)
	return exitPass
}

// gateInstall applies the preflight verdict to the install: WARN checks are
// printed; BLOCK gaps (FAIL or a WARN-downgraded BLOCK-tier with remediation) are
// OFFERED for consented host-prep (D-04). It returns (exitCode, proceed). proceed
// is false when a BLOCK gap is neither consented nor --force'd.
//
// consents threads a pre-collected decision map (gap-id → y/n) from the wizard
// path (D-04): a recorded decision is honored WITHOUT re-prompting stdin (huh
// already consumed it); an unrecorded id (or a nil map, the flag path) falls
// through to today's d.consent prompt byte-for-byte. gateInstall runs EXACTLY ONCE
// per install, so a privileged fix runs AT MOST ONCE regardless of path.
func gateInstall(out, errOut io.Writer, checks []preflight.CheckResult, opts installOpts, consents map[string]bool, d *installDeps) (int, bool) {
	var unmet []preflight.CheckResult
	for _, c := range checks {
		switch c.Status {
		case preflight.StatusPass:
			// nothing
		case preflight.StatusWarn:
			switch {
			case safeAutoFix(c.ID):
				// A non-privileged safe fix auto-runs with a visible notice and NO consent
				// (D-04/D-05) — but only when interactive and not --json (respect the
				// non-interactive guard). It never consumes a consents entry. With no
				// current safe fix (safeAutoFix is false for PRE-03/PRE-05) this branch is
				// a behavior no-op today; it is the forward-looking D-05 classifier.
				if opts.json || !d.interactive() {
					fmt.Fprintf(out, "warning: [%s] %s — %s\n", c.ID, c.Detail, c.Remediation)
					break
				}
				fmt.Fprintf(out, "auto-fixing [%s]: %s\n", c.ID, remediationCommand(c, d.username()))
				if err := runGapFix(c, d); err != nil {
					fmt.Fprintf(out, "  auto-fix failed: %v — run the command manually\n", err)
				} else {
					fmt.Fprintf(out, "  applied: %s\n", remediationCommand(c, d.username()))
				}
			case c.Tier == preflight.TierBlock:
				// A BLOCK-tier check that is not satisfied (off/unverifiable) is a gap
				// install must resolve via consent — not a clean pass.
				unmet = append(unmet, c)
			case hasAutomatedFix(c.ID):
				// A WARN-tier gap with an automated privileged fix (linger off, D-04):
				// OFFER the consented fix, but never block if declined — it is boot-
				// survival, not an immediate crash. Route this NON-blocking offer to
				// stdout (WR-07) so scripts parsing stderr do not misread a soft,
				// optional host-prep offer as an error. The BLOCK-gap path below keeps
				// its stderr wording.
				offerNonBlockingGap(out, c, opts, consents, d)
			default:
				fmt.Fprintf(out, "warning: [%s] %s — %s\n", c.ID, c.Detail, c.Remediation)
			}
		case preflight.StatusFail:
			unmet = append(unmet, c)
		}
	}

	if len(unmet) == 0 {
		return exitPass, true
	}

	// For each BLOCK gap: offer the exact privileged command on an interactive
	// TTY; run it only on explicit y. Decline / --json / non-interactive → print
	// the command and keep the gap as a block.
	var stillBlocked []preflight.CheckResult
	for _, c := range unmet {
		if resolveGap(out, errOut, c, opts, consents, d) {
			continue
		}
		stillBlocked = append(stillBlocked, c)
	}

	if len(stillBlocked) == 0 {
		return exitPass, true
	}

	if opts.force {
		fmt.Fprintf(out, "\nOverridden BLOCK gap(s) (--force): %d bypassed\n", len(stillBlocked))
		for _, c := range stillBlocked {
			fmt.Fprintf(out, "  - [%s] %s: %s\n", c.ID, c.Name, c.Detail)
		}
		fmt.Fprintf(out, "Proceeding despite unmet host-prep — you accepted the risk.\n")
		return exitWarn, true
	}

	fmt.Fprintf(errOut, "\nBLOCKED: %d host-prep step(s) unmet. Run the printed command(s) above, or re-run `villa install --force` to override (auditable).\n", len(stillBlocked))
	return exitBlocked, false
}

// resolveGap handles one BLOCK gap: it prints the exact remediation command, and
// — only on an interactive TTY, non-JSON, with an explicit y — runs the matching
// fixed-arg privileged seam (setsebool / enable-linger). It returns true when the
// gap was consented-and-run, false otherwise (caller treats false as a block).
func resolveGap(out, errOut io.Writer, c preflight.CheckResult, opts installOpts, consents map[string]bool, d *installDeps) bool {
	cmdStr := remediationCommand(c, d.username())
	fmt.Fprintf(errOut, "\nhost-prep needed: [%s] %s\n  command: %s\n", c.ID, c.Detail, cmdStr)

	// Wizard path: a pre-collected decision (huh already consumed stdin) is honored
	// WITHOUT re-prompting (D-04). A recorded `true` runs the same fixed-arg seam as
	// the consented stdin path; a recorded `false` is a decline (same return/messaging).
	if decision, recorded := consents[c.ID]; consents != nil && recorded {
		if !decision {
			// Emit the contracted BLOCK-gap-declined copy (17-UI-SPEC.md Copywriting)
			// verbatim, with <check name>=c.Name and <remediation>=blockRemediation(c).
			// The terse hint stays as the actionable next-step. Returning false keeps
			// the 0/2/1 exit contract (caller blocks unless --force).
			fmt.Fprintf(errOut, "BLOCK: %s. %s. Run the suggested command, or re-run with --no-tui --force to override (auditable).\n", c.Name, blockRemediation(c))
			fmt.Fprintf(errOut, "  declined — run the command above, then re-run `villa install`\n")
			return false
		}
		if err := runGapFix(c, d); err != nil {
			fmt.Fprintf(errOut, "  host-prep failed: %v — run the command manually, then re-run `villa install`\n", err)
			return false
		}
		fmt.Fprintf(out, "  applied: %s\n", cmdStr)
		return true
	}

	// Non-interactive / --json / no TTY → never prompt; print + block (D-04/D-05).
	if opts.json || !d.interactive() {
		fmt.Fprintf(errOut, "  (non-interactive — run the command above, then re-run `villa install`)\n")
		return false
	}

	if !d.consent(fmt.Sprintf("Run `%s` now? [y/N] ", cmdStr)) {
		fmt.Fprintf(errOut, "  declined — run the command above, then re-run `villa install`\n")
		return false
	}

	// Consented → run the matching fixed-arg seam (never a shell, T-03-08).
	if err := runGapFix(c, d); err != nil {
		fmt.Fprintf(errOut, "  host-prep failed: %v — run the command manually, then re-run `villa install`\n", err)
		return false
	}
	fmt.Fprintf(out, "  applied: %s\n", cmdStr)
	return true
}

// offerNonBlockingGap handles a WARN-tier gap with an automated privileged fix
// (e.g. PRE-03 linger): it OFFERS the consented fix but never blocks if declined —
// this is boot-survival, not an immediate crash. Unlike resolveGap (the BLOCK path,
// which writes to stderr), every message here goes to stdout (WR-07) so a soft,
// optional offer is never misread as an error by scripts parsing stderr. It returns
// whether the fix was consented-and-applied (informational; the caller never blocks
// on the result).
func offerNonBlockingGap(out io.Writer, c preflight.CheckResult, opts installOpts, consents map[string]bool, d *installDeps) bool {
	cmdStr := remediationCommand(c, d.username())
	fmt.Fprintf(out, "\noptional host-prep (boot survival): [%s] %s\n  command: %s\n", c.ID, c.Detail, cmdStr)

	// Wizard path: honor a pre-collected decision without re-prompting (D-04). A
	// recorded `true` runs the same fixed-arg seam; a recorded `false` is a skip.
	if decision, recorded := consents[c.ID]; consents != nil && recorded {
		if !decision {
			fmt.Fprintf(out, "  skipped — boot survival not enabled; install continues. Run the command above later if you want it.\n")
			return false
		}
		if err := runGapFix(c, d); err != nil {
			fmt.Fprintf(out, "  host-prep failed: %v — run the command manually if you want boot survival; install continues.\n", err)
			return false
		}
		fmt.Fprintf(out, "  applied: %s\n", cmdStr)
		return true
	}

	// Non-interactive / --json / no TTY → never prompt; just note it and continue.
	if opts.json || !d.interactive() {
		fmt.Fprintf(out, "  (optional — run the command above to enable boot survival; install continues regardless)\n")
		return false
	}

	if !d.consent(fmt.Sprintf("Run `%s` now? [y/N] ", cmdStr)) {
		fmt.Fprintf(out, "  skipped — boot survival not enabled; install continues. Run the command above later if you want it.\n")
		return false
	}

	// Consented → run the matching fixed-arg seam (never a shell, T-03-08).
	if err := runGapFix(c, d); err != nil {
		fmt.Fprintf(out, "  host-prep failed: %v — run the command manually if you want boot survival; install continues.\n", err)
		return false
	}
	fmt.Fprintf(out, "  applied: %s\n", cmdStr)
	return true
}

// runGapFix dispatches a consented gap to its fixed-arg privileged seam by check
// ID. PRE-05 → setsebool; PRE-03 (linger) → enable-linger. Unknown gaps cannot be
// auto-fixed (return an error so the caller blocks).
func runGapFix(c preflight.CheckResult, d *installDeps) error {
	switch c.ID {
	case "PRE-05":
		return d.setsebool()
	case "PRE-03":
		return d.enableLinger(d.username())
	default:
		return fmt.Errorf("no automated host-prep for %s", c.ID)
	}
}

// hasAutomatedFix reports whether a check ID has a consented privileged seam
// install can offer to run (D-04). Only these are offered; everything else is a
// printed remediation hint.
func hasAutomatedFix(id string) bool {
	switch id {
	case "PRE-05", "PRE-03":
		return true
	default:
		return false
	}
}

// safeAutoFix reports whether a check ID has a NON-privileged automated fix that
// may auto-run with a visible notice and NO consent (D-05). It returns false for
// both current fixes — [ASSUMED] PRE-05 (setsebool -P) and PRE-03 (loginctl
// enable-linger) are PRIVILEGED, so they stay consent-gated (D-04: villa never
// silently runs a privileged command; enable-linger stays privileged per the
// RESEARCH "Open Questions (RESOLVED)" interpretation 1). This is a forward-looking
// classifier — with no current safe fix it is a behavior no-op on the present check
// set; a FUTURE non-privileged fix returns true here to opt into auto-run.
func safeAutoFix(id string) bool {
	switch id {
	// No current non-privileged automated fix. PRE-03/PRE-05 are privileged → false.
	default:
		return false
	}
}

// remediationCommand returns the exact copy-paste command for a gap, preferring
// the well-known fixed commands (so the printed string matches the seam exactly)
// and falling back to the check's Remediation text.
func remediationCommand(c preflight.CheckResult, username string) string {
	switch c.ID {
	case "PRE-05":
		return "setsebool -P container_use_devices=true"
	case "PRE-03":
		return fmt.Sprintf("loginctl enable-linger %s", username)
	default:
		if c.Remediation != "" {
			return c.Remediation
		}
		return c.Detail
	}
}

// blockRemediation returns the <remediation> token for the contracted
// BLOCK-gap-declined copy (17-UI-SPEC.md Copywriting): the check's Remediation text
// when present, else the well-known fixed remediation command (remediation-forward
// fallback, mirroring remediationCommand). It re-resolves the username the same way
// resolveGap does — display only, the wizard never runs the command.
func blockRemediation(c preflight.CheckResult) string {
	if c.Remediation != "" {
		return c.Remediation
	}
	return remediationCommand(c, installUsername())
}

// gibUsableEnvelope renders a typed-Unknown usable-memory envelope as the
// "<N> GiB" figure the empty-state copy contract wants (17-UI-SPEC.md:195),
// emitting just the GiB number followed by " GiB" — e.g. "8 GiB". A typed-Unknown
// envelope (Known=false) renders "unknown GiB" so the no-fit sentence reads
// "(unknown GiB usable)" rather than a fabricated 0 (typed-Unknown never becomes a
// confident 0). A whole-GiB value renders without a fractional tail; a fractional
// value keeps up to two decimals (e.g. "7.5 GiB").
func gibUsableEnvelope(b detect.Bytes) string {
	if !b.Known {
		return "unknown GiB"
	}
	g := float64(b.Value) / (1 << 30)
	return strconv.FormatFloat(g, 'g', -1, 64) + " GiB"
}

// chatURL is the loopback chat (Open WebUI) URL printed post-install (D-03/D-04):
// the host side of the owui PublishPort (127.0.0.1:3000:8080, openWebUIPublishPort
// in internal/orchestrate). Loopback-only — never a LAN/0.0.0.0 address (PRIV-01).
const chatURL = "http://127.0.0.1:3000"

// dashboardURL is the loopback control-dashboard URL printed post-install. It is the
// config default (DashboardAddr 127.0.0.1 / DashboardPort 8888); the dashboard now
// comes up as a managed boot-surviving service in this install (Plan 05-05 / D-03),
// so there is no dead link. Loopback-only (PRIV-01).
const dashboardURL = "http://127.0.0.1:8888"

// printPostInstall prints the loopback inference endpoint + the readiness verdict,
// the real loopback chat URL (Open WebUI is brought up by this install, D-03), and
// the real loopback control-dashboard URL (the dashboard is now a managed
// boot-surviving service brought up by this install, Plan 05-05 / D-03 — no dead
// links). The endpoint is sourced from the backend seam (T-03-10), never retyped.
func printPostInstall(out io.Writer, endpoint string, ready installReadiness) {
	fmt.Fprintf(out, "\ninference endpoint: %s\n", endpoint)
	switch ready.status {
	case preflight.StatusPass:
		fmt.Fprintf(out, "health: PASS — %s\n", ready.detail)
	case preflight.StatusWarn:
		fmt.Fprintf(out, "health: WARN — %s\n", ready.detail)
	default:
		fmt.Fprintf(out, "health: %s\n", ready.detail)
	}
	fmt.Fprintf(out, "chat (Open WebUI): %s\n", chatURL)
	fmt.Fprintf(out, "dashboard: %s\n", dashboardURL)
}

// liveInstallDeps wires installDeps to the real host: detect.Probe, recommend.Pick
// against the loaded catalog, the orchestrate render/reconcile/write + systemd
// seam, the SELinux/linger privileged seams, the verified model downloader + the
// 0600 config writer (F-1/F-2, mirroring model swap), and the readiness poll
// (Task 2). It is replaced wholesale by stubs in install_test.go.
func liveInstallDeps() (*installDeps, error) {
	sys := orchestrate.NewSystemd()
	uname := installUsername()
	// Resolve the backend from config (fail-closed, D-02) for the post-install endpoint
	// line — derived from the resolved backend's container runner, never a literal. A
	// load failure or unknown-backend value blocks install rather than defaulting.
	cfg, err := config.LoadVilla()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return nil, fmt.Errorf("resolve backend: %w", err)
	}
	endpoint := inference.NewContainerRunner(backend, inference.RunSpec{}).Endpoint()

	// resolveCatalogModel maps a recommendation to its catalog entry — the single
	// place the model-id → catalog lookup happens for both the on-disk check and
	// the pull, so install never fabricates a weight path (WR-08, mirrors swap).
	resolveCatalogModel := func(rec recommend.Recommendation) (catalog.CatalogModel, bool) {
		cat, _, err := catalog.Load(modelCatalogPath)
		if err != nil {
			return catalog.CatalogModel{}, false
		}
		return cat.FindByID(rec.Model)
	}

	return &installDeps{
		probe: detect.Probe,
		pick: func(p detect.HostProfile, ov recommend.Overrides) recommend.Recommendation {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return recommend.Recommendation{}
			}
			// Thread the PERSISTED memory inputs (fail-soft) so an opted-in install
			// recommends against the shrunken envelope (D-01; Pitfall 3 — a
			// memory-blind install pick defeats CTRL-01).
			return recommend.Pick(p, cat, ov, liveLoadedMemoryInputs())
		},
		modelFile: func(rec recommend.Recommendation) (string, error) {
			// A catalog load failure or an unknown model id is a hard error (WR-08):
			// fabricating "<model>.gguf" would generate a container whose -m points at
			// a non-existent file that only fails at runtime after install reports
			// success. Block here so install surfaces the resolution failure instead.
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return "", fmt.Errorf("load model catalog: %w", err)
			}
			m, ok := cat.FindByID(rec.Model)
			if !ok {
				return "", fmt.Errorf("model %q is not in the catalog — cannot resolve its weight file", rec.Model)
			}
			return primaryModelFile(m), nil
		},
		modelsDir: modelsDir,
		modelDownloaded: func(rec recommend.Recommendation) bool {
			// An unresolvable model is treated as "not downloaded" so ensureModel runs
			// and surfaces the catalog error, rather than silently skipping the pull.
			m, ok := resolveCatalogModel(rec)
			if !ok {
				return false
			}
			path := filepath.Join(modelsDir(), primaryModelFile(m))
			_, err := os.Stat(path)
			return err == nil
		},
		ensureModel: func(rec recommend.Recommendation) error {
			// Reuse the exact verified/resumable downloader `model swap`/`model pull`
			// use (via the pullFn seam), into the same models dir — no new downloader.
			m, ok := resolveCatalogModel(rec)
			if !ok {
				return fmt.Errorf("model %q is not in the catalog — cannot download its weights", rec.Model)
			}
			dir := modelsDir()
			if mkErr := os.MkdirAll(dir, 0o700); mkErr != nil {
				return mkErr
			}
			return pullFn(context.Background(), m, dir)
		},
		saveConfig:   config.SaveVilla,
		runChecks:    preflight.RunWithResources,
		render:       orchestrate.Render,
		reconcile:    orchestrate.Reconcile,
		writeUnits:   orchestrate.WriteUnits,
		unitDir:      quadletUnitDir,
		daemonReload: sys.DaemonReload,
		start:        sys.Start,
		isActive:     sys.IsActive,
		enableLinger: sys.EnableLinger,
		setsebool:    liveSetsebool,

		// Dashboard-service seams (Plan 05-05): render+write the native unit into the
		// user-unit dir, then enable it for boot-survival via the systemd seam. The
		// binary path is resolved at install time (resolveDashboardBinaryPath) and
		// threaded into the renderer so ExecStart targets the running binary (UAT Test 5).
		userUnitDir:        orchestrate.UserUnitDir,
		writeDashboardUnit: orchestrate.WriteDashboardUnit,
		readDashboardUnit: func(dir string) ([]byte, error) {
			return os.ReadFile(filepath.Join(dir, orchestrate.DashboardServiceName))
		},
		resolveBinaryPath: resolveDashboardBinaryPath,
		enable:            sys.Enable,
		username:          func() string { return uname },
		endpoint:          func() string { return endpoint },
		interactive:       stdinIsInteractive,
		consent:           promptConsent,
		pollReady:         liveReadinessPoll,
		stdoutIsTTY:       stdoutIsTTY,
		wizard:            liveWizard,

		// Memory-stack seams (Phase-19). The gate keys off the PERSISTED config
		// (liveLoadedMemoryEnabled → config.LoadVilla().MemoryEnabled, fail-soft to false),
		// NOT the DefaultVillaConfig() seed (T-19-16). Pre-stage + presence reuse the same
		// verified download path and models dir as the chat-model ensureModel above (D-07).
		loadedConfig:        liveLoadedConfig,
		loadedMemoryEnabled: liveLoadedMemoryEnabled,
		embedModelPresent:   liveEmbedModelPresent,
		ensureEmbedModel:    liveEnsureEmbedModel,
		memoryProofFn:       liveMemoryProof,
	}, nil
}

// quadletUnitDir is the fixed rootless Quadlet generator directory
// (~/.config/containers/systemd), created if absent so the first install writes
// cleanly. It mirrors the XDG config discipline of internal/config.
func quadletUnitDir() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(base, "containers", "systemd")
	if mkErr := os.MkdirAll(dir, 0o755); mkErr != nil {
		return "", mkErr
	}
	return dir, nil
}

// resolveDashboardBinaryPath returns the stable absolute path of the running villa
// binary for the dashboard unit's ExecStart (UAT Test 5 fix). It resolves via
// os.Executable() (the kernel-reported path of the running process), then
// filepath.EvalSymlinks (collapse a symlinked launcher to the real binary so the unit
// survives the symlink being swapped) and filepath.Abs (defensive — guarantee an
// absolute token; systemd ExecStart must not be relative). This makes the dashboard
// service start correctly for BOTH a dev build (./villa from the repo) and an installed
// binary, with no file copying.
//
// Failure policy (matches the resolveBinaryPath seam doc): a fatal os.Executable or
// filepath.Abs error is RETURNED so the caller fails the install — it never falls back
// to the old fixed ~/.local/bin/villa. A non-fatal EvalSymlinks failure is tolerated by
// degrading to the raw os.Executable path (still the running binary, still absolute);
// this is graceful degradation to a dynamically-resolved path, NOT a fixed-path fallback.
func resolveDashboardBinaryPath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", fmt.Errorf("os.Executable: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(exe)
	if err != nil {
		// EvalSymlinks can fail (e.g. a deleted/renamed binary); degrade to the raw
		// os.Executable path rather than failing outright — it is still the running
		// binary and still absolute (NOT a fixed-path fallback).
		resolved = exe
	}
	abs, err := filepath.Abs(resolved)
	if err != nil {
		return "", fmt.Errorf("filepath.Abs(%q): %w", resolved, err)
	}
	return abs, nil
}

// installUsername resolves the current username for the loginctl enable-linger
// consent step, preferring os/user over $USER (matches preflight's liveLingerDeps).
func installUsername() string {
	if u, err := user.Current(); err == nil && u.Username != "" {
		return u.Username
	}
	return os.Getenv("USER")
}
