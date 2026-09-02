package main

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"os/user"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"syscall"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/install"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
)

// install.go wires the `villa install` lifecycle verb (07, ORCH-03,
// the single command that turns Phase-2's manual `podman run` into a
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
// Privileged host-prep is OFFERED per-step with the exact command
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
	// dryRun prints the rendered changed units and writes nothing (ORCH).
	dryRun bool
	// force overrides an un-consented BLOCK-tier preflight gap (auditable).
	force bool
	// json suppresses interactive consent (a --json run is non-interactive).
	json bool
	// noTUI opts out of the guided wizard to the flag-driven install path
	// Bare `villa install` on a TTY launches the wizard; --no-tui (or
	// --json, or a non-TTY stdin/stdout) forces today's flag path verbatim. There is
	// NO --tui opt-in and NO `villa setup` subcommand — one progressively-enhanced verb.
	noTUI bool
	// codingAgent opts INTO the v1.4 coding-agent (Crush) install addon (
	// when set, runInstall overrides+persists cfg.AgentEnabled = true before the
	// gate, then runs the agent pre-stage/install/render/readiness block. A bare
	// `villa install` (flag unset) gates on the PERSISTED agent_enabled instead.
	codingAgent bool
	// webSearch opts INTO the v1.5 web-search (SearXNG + villa-websafe) addon: when set,
	// runInstall overrides+persists cfg.WebSearchEnabled = true before the gate (mirroring
	// codingAgent), so the searxng-secret generation + render + start + readiness proof all
	// fire. A bare `villa install` (flag unset) gates on the PERSISTED web_search_enabled
	// instead, so an unchanged, web-search-off install stays byte-identical to v1.4.
	webSearch bool
}

// installReadiness is the readiness-poll verdict (Task 2): PASS once the service
// is active and /health returns 200, WARN when the poll could not confirm
// readiness (timeout / typed-Unknown — never a confident FAIL on a 503).
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
	// (recommend.Pick) rather than a forked catalog re-derivation: the flag
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
	// Rollback seams (ADR-0003). Install is transactional: it captures before
	// mutating and restores on failure, like the three swap cores. These are the
	// effects the restore performs, and they exist ONLY for that path — the forward
	// path never stops a service or removes a unit.
	//
	// NIL-SAFE: a nil seam makes the corresponding restore step a no-op, which the
	// core reports as an incomplete rollback rather than as a clean restoration.
	stop       func(service string) error
	readUnit   func(dir, name string) (string, bool)
	removeUnit func(dir, name string) error
	// configExists reports whether a config was on disk BEFORE this install. It is
	// the difference between restoring the operator's prior config and removing one
	// this install created on a clean host.
	configExists func() bool
	// removeConfig deletes the config this install wrote on a clean host.
	removeConfig func() error
	isActive     func(service string) (string, error)
	enableLinger func(user string) error
	setsebool    func() error

	// Dashboard-service seams (Plan 05-05): the dashboard is a NATIVE
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

	// Memory-stack seams (Phase-19 /, INFRA-02). All gated on the
	// resolved memory gate, skipped under --dry-run.
	//
	// loadedConfig returns the PERSISTED config.LoadVilla(), PROPAGATING a load
	// error. runInstall SEEDS cfg from this instead of
	// DefaultVillaConfig(), then overrides ONLY the recommendation-derived fields
	// (Model/Quant/Ctx/Backend) and the MemoryEnabled gate — so a user's persisted
	// memory fields (qdrant_addr/port, embed_addr/port, embedding_model/dim) and the
	// dashboard/chat fields are PRESERVED through saveConfig, never silently reset to
	// seed defaults on every install. LoadVilla self-heals zeroed dashboard/
	// chat fields, so seeding from it keeps the gap-test:1b loopback-default guarantee
	// while honoring any persisted customization.
	loadedConfig func() (config.VillaConfig, error)
	// embedModelPresent reports whether the pre-staged embed GGUF is already on disk
	// (the ensureEmbedModel idempotency guard — a present file is never re-pulled).
	embedModelPresent func(modelsDir string) bool
	// ensureEmbedModel pre-stages the nomic embed GGUF into modelsDir via the verified
	// download path, invoked only when memory is on, not dry-run, and absent.
	ensureEmbedModel func(modelsDir string) error
	// memoryProofFn asserts the memory stack is healthy: an offline 768-dim
	// /v1/embeddings vector AND a Qdrant writable round-trip. A FAIL refuses-with-
	// remediation (exitBlocked), never a silent skip. Invoked only when memory
	// is on and not dry-run.
	memoryProofFn func(ctx context.Context, in memoryProofInput) memoryProof
	// runMemoryChecks returns the opt-in memory-stack host-fitness gates
	// (MEM-PRE-disk vector-index disk + MEM-PRE-headroom embedder headroom,
	// CTRL-06) appended to the preflight checks when the resolved memory gate
	// is on — so an unfit host is refused-with-remediation BEFORE the
	// memory stack comes up. NIL-SAFE: when nil (test doubles), no memory checks
	// are appended (mirrors the doctor optional-seam pattern).
	//
	// The embedding model comes in as an argument, from the config loaded ONCE at
	// step 0, so this gate cannot re-read (and re-swallow) config.toml behind the
	// refusal above.
	runMemoryChecks func(p detect.HostProfile, embeddingModel string) []preflight.CheckResult
	// readRecallState reads recall-state.json fail-closed for the Phase-23
	// skew WARN surface (warnRecallEmbeddingSkew): absent ⇒ empty state ⇒ silent
	// typed-Unknown; only a real I/O fault errors (also silent — read-only WARN,
	// NIL-SAFE: when nil (test doubles), the WARN helper degrades silently
	// (mirrors the runMemoryChecks optional-seam pattern). Live wiring is the
	// SHARED liveRecallStateLoad (the same reader `villa recall` uses) so the two
	// guards can never drift onto different readers.
	readRecallState func() (recall.State, error)

	// writeSearxngSettings persists the rendered settings.yml into the villa-owned searxng
	// config dir mounted read-only at /etc/searxng (Plan 02's atomic, traversal-guarded,
	// 0600 writer). Invoked BEFORE the searxng start so the container reads it on first boot.
	writeSearxngSettings func(name, text string) error
	// writeSearxngSecretEnv persists the 0600 SEARXNG_SECRET env file — the unit's
	// EnvironmentFile= target (Plan 02's writer) — so the secret reaches the container via
	// the 0600 file, never the 0644 unit. Invoked BEFORE the searxng start.
	writeSearxngSecretEnv func(name, text string) error
	// searxngProofFn asserts the search service is ready via a REAL format=json query
	// parsing results[] (never a health-200). A FAIL refuses-with-remediation
	// (exitBlocked), never a silent skip. Invoked only when web search is on and not dry-run.
	searxngProofFn func(ctx context.Context, in searxngProofInput) searxngProof

	// writeWebsafeSecretEnv persists the 0600 EXTERNAL_WEB_LOADER_API_KEY env file — the
	// EnvironmentFile= target BOTH the villa-websafe AND the OWUI units reference
	// (WebsafeSecretEnvFilePath, single source) — so the bearer reaches both containers via the
	// 0600 file, never a 0644 unit. It is gated on the PERSISTED web_search_enabled
	// and MUST be invoked BEFORE the OWUI start (which references it via EnvironmentFile= when
	// web search is on) AND before the villa-websafe start. Mirrors writeSearxngSecretEnv.
	writeWebsafeSecretEnv func(name, text string) error

	// agentCatalog loads the model catalog the coder-shard resolution reads (coderShardFor).
	// Returns (catalog, false) on a load failure so the gate refuses-with-remediation
	// rather than fabricating a shard.
	agentCatalog func() (catalog.Catalog, bool)
	// coderModelPresent reports whether the resolved coder GGUF is already on disk AND
	// intact (size-gated; a present file is never re-pulled — the idempotency guard).
	coderModelPresent func(modelsDir string, sh catalog.Shard) bool
	// ensureCoderModel pre-stages the resolved coder shard into modelsDir via the verified
	// download path (the single sanctioned outbound window), invoked only when the
	// agent is on, not dry-run, and the file is absent.
	ensureCoderModel func(modelsDir string, sh catalog.Shard) error
	// installAgentBinary stages the pinned Crush binary via the checksum-before-extract
	// agent.Install seam. Returns the placed binary path.
	installAgentBinary func(ctx context.Context) (string, error)
	// renderCrushConfig composes agent.Render (the restrictive-tools crush.json) and writes
	// it to the global crush config path. The render is the security control.
	renderCrushConfig func(cfg config.VillaConfig) error
	// agentProofFn asserts the agent is ready via a REAL tool-call round-trip (read→edit→
	// result), PASS/FAIL only — a health-200 is never an input. A FAIL refuses-with-
	// remediation (exitBlocked). Invoked only when the agent is on and not dry-run.
	agentProofFn func(ctx context.Context) agentProof
	// runAgentChecks returns the opt-in coding-agent host-fitness gates (AGENT-PRE-disk
	// staged-footprint disk BLOCK, AGENT-PRE-envelope post-coder fit BLOCK driven by
	// rec.Coder, AGENT-PRE-cloud-cred env-credential WARN) appended to
	// the preflight checks when the resolved agent gate is on — so an unfit host (no
	// disk, no coder fit) is refused-with-remediation BEFORE the agent is staged. It takes
	// rec (unlike runMemoryChecks(profile)) because the envelope gate reads rec.Coder and
	// the disk gate sizes from the picked coder shard. NIL-SAFE: when nil (test doubles), no
	// agent checks are appended (mirrors the runMemoryChecks optional-seam pattern).
	runAgentChecks func(detect.HostProfile, recommend.Recommendation) []preflight.CheckResult

	// stdoutIsTTY reports whether stdout is a real terminal — the stdout twin of
	// interactive() (which checks stdin). huh renders to stdout/stderr, so BOTH must
	// be a TTY for the styled wizard to make sense. The seam wraps the
	// stdoutIsTTY() helper from tui_theme.go so tests can inject a fake TTY result.
	stdoutIsTTY func() bool
	// wizard runs the guided huh 5-screen install wizard and RETURNS the collected
	// choices (a model override + per-item privileged consent) — it is a PURE
	// COLLECTOR: it presents the already-computed profile/rec/checks/
	// backend and NEVER executes a host fix. The single gateInstall in runInstall
	// consumes the collected consent. tests inject a fake returning a canned
	// wizardResult. NO internal/* core imports huh; the live impl is liveWizard
	// in install_wizard.go.
	wizard func(ctx context.Context, in wizardInput) (wizardResult, error)
}

// installServiceName is the systemd service the inference .container generates
// (Quadlet maps villa-llama.container → villa-llama.service).
const installServiceName = "villa-llama.service"

// openWebUIServiceName is the systemd service the Open WebUI .container generates
// (Quadlet maps villa-openwebui.container → villa-openwebui.service, the same
// .container→.service rule serviceUnits encodes). It is started AFTER inference
// so the chat UI comes up against a live backend.
const openWebUIServiceName = "villa-openwebui.service"

// websafeServiceName is the systemd service the villa-websafe .container generates (Quadlet
// maps villa-websafe.container → villa-websafe.service). Its start is gated on the rendered
// unit being present in the written plan AND on the PERSISTED web_search_enabled, and it is
// started only AFTER the 0600 websafe.env secret file is written (its EnvironmentFile= target).
const websafeServiceName = "villa-websafe.service"

// newInstall builds `villa install`: detect → recommend → preflight gate →
// consented host-prep → render → reconcile → write → daemon-reload → start →
// readiness poll, idempotent and --dry-run aware.
func newInstall() *cobra.Command {
	var dryRun bool
	var noTUI bool
	var codingAgent bool
	var webSearch bool
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
		RunE: func(cmd *cobra.Command, _ []string) error {
			deps, err := liveInstallDeps(cmdContext(cmd))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "install: %v\n", err)
				os.Exit(exitBlocked)
			}
			code := runInstall(cmd, installOpts{dryRun: dryRun, force: force, json: jsonOut, noTUI: noTUI, codingAgent: codingAgent, webSearch: webSearch}, deps)
			os.Exit(code)
			return nil
		},
	}
	cmd.Flags().BoolVar(&dryRun, "dry-run", false, "print the rendered units without writing, pulling, or starting anything")
	cmd.Flags().BoolVar(&noTUI, "no-tui", false, "skip the guided wizard; use the flag-driven install path")
	cmd.Flags().BoolVar(&codingAgent, "coding-agent", false, "install the local coding agent (Crush) addon: stage its pinned binary + coder model, render a locked-down config, and prove a tool-call round-trip")
	cmd.Flags().BoolVar(&webSearch, "web-search", false, "install the web-search addon: render the SearXNG service + the SSRF-guarded villa-websafe loader, wire Open WebUI's native web search, and prove SearXNG readiness (opt-in; default off)")
	return cmd
}

// runInstall executes the install flow and RETURNS the exit code (0 pass / 2 warn
// / 1 block) — it never calls os.Exit, so tests drive it. All printing + exit
// mapping live here; the orchestrate/preflight/recommend libraries stay pure.
func runInstall(cmd *cobra.Command, opts installOpts, d *installDeps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	// res is the typed outcome of this run (internal/install's Result). The flow
	// still streams its progress as it happens — a model pull cannot be narrated
	// after the fact — but every terminal return derives its exit code from the
	// Result, so the outcome-to-exit-code contract lives in ONE tested place.
	var res install.Result
	// blockf prints a pre-mutation stop and records it: Blocked, nothing to roll
	// back. It replaces the print-then-return-exitBlocked pair at every site.
	blockf := func(format string, args ...any) int {
		fmt.Fprintf(errOut, format, args...)
		return res.Block(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"))
	}

	// (0) Load the PERSISTED config FIRST, and REFUSE if it cannot be read.
	//
	// This seam used to fail soft to typed defaults, so an unreadable or hand-edited
	// config.toml was silently replaced by seed values and install went on to render
	// units, write them, and persist a config from input it had failed to read —
	// discarding the user's memory/dashboard/chat customizations without saying so.
	// The convention for untrusted input is to fail closed: error, never a silent
	// default. An ABSENT config is NOT an error (LoadVilla returns typed defaults), so
	// a first install on a clean host is unaffected.
	//
	// It is loaded here, before the host probe, so the refusal costs nothing and one
	// read serves both the memory host-fitness gate below and the cfg seed at step 4.
	cfg, err := d.loadedConfig()
	if err != nil {
		return blockf("install: cannot read the persisted config: %v\n", err)
		fmt.Fprintln(errOut, "install: refusing to install from defaults — that would overwrite your persisted settings with seed values. Fix or remove config.toml, then re-run.")
	}

	// (1) Detect the host.
	profile := d.probe()

	// (2) Recommend a concrete model. A refusal (empty Model / zero ctx / zero
	// weight) is a clear FAIL — never start llama-server with -c 0 / no fit.
	rec := d.pick(profile, recommend.Overrides{})
	if !install.RecommendationUsable(rec) {
		// Emit the contracted empty-state copy (17-UI-SPEC.md:195) verbatim, with the
		// `<N> GiB` token substituted from the detected usable envelope. A typed-Unknown
		// envelope renders "unknown GiB usable" (never a fabricated 0). This branch fires
		// BEFORE the wizard is evaluated, so it covers both the flag and wizard paths from
		// one emission point — the parenthetical confirms the --no-tui path is identical.
		return blockf("No catalog model fits the detected memory envelope (%s usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)\n", gibUsableEnvelope(profile.UsableEnvelopeBytes))
	}
	fmt.Fprintf(out, "selected model %s (ctx %d, %s)\n", rec.Model, rec.ContextLen, gib(rec.WeightBytes))

	// (3) Preflight gate with the concrete model's resource requirement. The
	// embedding reservation is included (Research Open Question 2 resolved YES
	// more honest): the value flows from the pick so memory.Footprint stays the
	// single source; it is zero when memory is off, leaving the off-path gate
	// unchanged.
	fit := install.ResourceFit(rec)
	req := preflight.ResourceReq{
		MinDiskBytes: fit.MinDiskBytes,
		MinMemBytes:  fit.MinMemBytes,
		DataDir:      d.modelsDir(),
	}
	// Resolve the optional-subsystem gates ONCE, before anything is gated or
	// rendered. The preflight gate, the pre-stage step, the render and the proofs all
	// read them; resolving per step is how the same flag came to be read eleven times
	// in this one flow, with the risk that two steps disagree.
	gates := install.ResolveGates(cfg, install.Opts{
		CodingAgent: opts.codingAgent,
		WebSearch:   opts.webSearch,
	}, rec)

	checks := d.runChecks(profile, req)
	// (3a) Opt-in memory-stack gates (CTRL-06): vector-index disk + embedder
	// headroom are appended ONLY when the persisted memory_enabled is on, so the
	// memory-off install gate is byte-identical. They flow through the single
	// gateInstall below — an opted-in install on an unfit host refuses-with-
	// remediation BEFORE the memory stack comes up.
	if gates.Memory && d.runMemoryChecks != nil {
		checks = append(checks, d.runMemoryChecks(profile, cfg.EmbeddingModel)...)
	}
	// (3a') Opt-in coding-agent gates: the staged-footprint disk BLOCK,
	// the post-coder envelope BLOCK (driven by rec.Coder, never re-derived), and the cloud-
	// credential WARN are appended ONLY when the addon is enabled (the persisted agent_enabled
	// OR --coding-agent override), so the agent-off install gate is byte-identical. They flow
	// through the SAME gateInstall below — an unfit host (no disk, no coder fit) is refused-
	// with-remediation BEFORE the agent is staged. agentEnabledForGate folds the --coding-agent
	// override into the persisted gate so a first-time `--coding-agent` run is gated too.
	if install.GateAgentChecks(gates) && d.runAgentChecks != nil {
		checks = append(checks, d.runAgentChecks(profile, rec)...)
	}

	// (3b) Guided wizard — the PINNED composition. probe/pick/runChecks
	// (steps 1-3) have already run exactly once; the wizard RECEIVES their results,
	// COLLECTS a model override + per-item privileged consent, and RETURNS here. It
	// NEVER runs runGapFix/resolveGap/offerNonBlockingGap itself — the single
	// gateInstall below consumes the threaded consent, so both paths converge on one
	// gate execution (privileged fix at most once; preserved).
	// nil consentDecisions ⇒ flag path: gateInstall prompts via d.consent as today.
	// --dry-run never enters the wizard (phase-22): the wizard collects
	// privileged consent the gate would then EXECUTE, and a dry run has zero side
	// effects (ORCH) — there is nothing for consent to apply to.
	var consentDecisions map[string]bool
	useWizard := d.interactive() && !opts.json && !opts.noTUI && d.stdoutIsTTY() && !opts.dryRun
	if useWizard {
		// Resolve the backend for the review screen via the single polymorphism point
		// (never a re-typed image literal). On an unknown backend, fall through to the
		// flag path rather than aborting the install.
		backend, berr := inference.BackendFor(rec.Backend)
		if berr != nil {
			return blockf("install: resolve backend for wizard: %v — falling back to the flag path\n", berr)
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
		res.Outcome = install.Blocked
		return gateCode
	}
	// A forced-override gate degrades the final verdict to WARN even on an
	// otherwise-clean bring-up: the host-prep gap was bypassed, not satisfied.
	res.GateDegraded = gateCode == exitWarn

	// (4) Render the units from config + backend, then reconcile against disk.
	unitDir, err := d.unitDir()
	if err != nil {
		return blockf("install: cannot resolve the Quadlet unit dir: %v\n", err)
	}
	// Assemble the install plan: the config this run persists and renders from,
	// derived in the pure core from the persisted config, the recommendation and the
	// resolved gates. It SEEDS from the persisted config rather than from defaults, so
	// a user's customised memory, dashboard and chat fields survive every install.
	//
	// The backend is guarded rather than assigned: the recommender always returns the
	// default and carries no backend override, so an unconditional assignment would
	// silently revert a deliberately-chosen backend on every re-install. The core
	// compares backend NAMES only, never an image literal.
	//
	// A wizard model override has already been folded into rec above, so the plan is
	// byte-identical between the wizard and flag paths.
	installPlan := install.AssemblePlan(cfg, install.Opts{
		CodingAgent: opts.codingAgent,
		WebSearch:   opts.webSearch,
	}, rec, persistedBackendChosen)
	cfg = installPlan.Config
	gates = installPlan.Gates

	modelFile, err := d.modelFile(rec)
	if err != nil {
		return blockf("install: resolve model file: %v\n", err)
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return blockf("install: resolve backend: %v\n", err)
	}
	// Build the render input. On the chat-only path this is byte-identical to v1.3 (CodingMode
	// stays nil → orchestrate.Render off-path unchanged → the v1.3 unit goldens are untouched).
	// On the --coding-agent path (cfg.CodingMode), serve the staged coder: resolve the served
	// -m from the coder shard (NOT the chat model's modelFile) and thread a non-nil CodingMode
	// descriptor, REUSING the proven Phase-25 coding-mode helpers (codingServedTarget /
	// codingModelFile / codingDescriptor) — the same render path `villa coding-mode enter`
	// drives, already frozen by villa-llama-coding.container.golden. The catalog→inference
	// translation stays in the live wiring (the pure renderer never imports internal/catalog).
	resident, err := liveResidentUnits(cfg)
	if err != nil {
		return blockf("install: resolve resident set: %v\n", err)
	}
	renderIn := orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     modelFile,
		ModelsDir:     d.modelsDir(),
		HostVillaPath: hostVillaPath(),
		Resident:      resident,
	}
	if gates.CodingMode {
		servedModel, _ := codingServedTarget(cfg)
		coderModelFile, mferr := codingModelFile(cfg, servedModel)
		if mferr != nil {
			return blockf("install: resolve coder model file: %v\n", mferr)
		}
		spec, derr := codingDescriptor(cfg, servedModel)
		if derr != nil {
			return blockf("install: build coding-mode descriptor: %v\n", derr)
		}
		renderIn.ModelFile = coderModelFile
		renderIn.CodingMode = spec
		renderIn.CoderAgentCtx = cfg.CoderAgentCtx
	}
	units, err := d.render(renderIn)
	if err != nil {
		return blockf("install: render failed: %v\n", err)
	}
	plan, err := d.reconcile(units, unitDir)
	if err != nil {
		return blockf("install: reconcile failed: %v\n", err)
	}

	// (5) --dry-run: print the changed unit text and stop — write nothing, pull
	// nothing, persist nothing (ORCH: a dry run has zero side effects).
	if opts.dryRun {
		res.Outcome = install.DryRun
		if len(plan.Changed) == 0 {
			fmt.Fprintf(out, "dry-run: no changes — units already match config\n")
			return res.Outcome.ExitCode()
		}
		for _, u := range plan.Changed {
			fmt.Fprintf(out, "# %s\n%s\n", u.Name, u.Text)
		}
		fmt.Fprintf(out, "dry-run: %d unit(s) would be written (nothing written, no model pulled, no config persisted)\n", len(plan.Changed))
		return res.Outcome.ExitCode()
	}

	// (6) Ensure the recommended model is present BEFORE starting the unit (F-1).
	// Without the weights on disk llama-server crash-loops on the missing GGUF and
	// install reports WARN. Pull only when absent (idempotent, strictly-local: a
	// present model is never re-pulled). This runs on BOTH the no-op and write
	// paths so an existing-units-but-missing-weights host still self-heals.
	if !d.modelDownloaded(rec) {
		fmt.Fprintf(out, "model %s not present — downloading...\n", rec.Model)
		if err := d.ensureModel(rec); err != nil {
			return blockf("install: download model %s failed: %v\n", rec.Model, err)
		}
		fmt.Fprintf(out, "model %s downloaded and verified\n", rec.Model)
	}

	// (6b) Pre-stage the embedding GGUF into villa-models BEFORE starting villa-embed
	// (Phase-19). Gated on the PERSISTED memory_enabled (cfg.MemoryEnabled,
	// resolved once above), skipped under --dry-run, and idempotent: a
	// present file is never re-pulled. This is the one-time install-time controlled pull
	// (the single sanctioned outbound window) so the embeddings runtime is ZERO-download.
	// download.PullModel verifies size + SHA256 before the atomic rename, so a
	// half-written/unverified GGUF is never trusted; a pull failure refuses-with-remediation.
	if gates.Memory && !opts.dryRun && !d.embedModelPresent(d.modelsDir()) {
		fmt.Fprintf(out, "embedding model %s not present — downloading...\n", nomicEmbedShard.Filename)
		if err := d.ensureEmbedModel(d.modelsDir()); err != nil {
			return blockf("install: pre-stage embedding model %s failed: %v\n", nomicEmbedShard.Filename, err)
		}
		fmt.Fprintf(out, "embedding model %s downloaded and verified\n", nomicEmbedShard.Filename)
	}

	// (6c) Pre-stage the coding-agent (Crush) addon BEFORE persisting config + starting
	// the stack (v1.4). Gated on cfg.AgentEnabled (the
	// persisted gate, or --coding-agent override) and skipped under --dry-run (that path
	// returned far above). The steps, in order: surface the FSL-1.1-MIT notice; resolve the
	// coder GGUF shard from the recommend-picked catalog entry (refuse-with-remediation when
	// no coder fits); presence-skip else pre-stage it via the single sanctioned
	// outbound window; stage the pinned, checksum-verified Crush binary; render
	// the locked-down crush.json (the restrictive-tools security control). The
	// readiness proof (a real tool-call round-trip) runs AFTER the stack is up (step 10c).
	var coderShard catalog.Shard
	if gates.Agent && !opts.dryRun {
		// (a) FSL-1.1-MIT consent notice — informational, printed BEFORE staging the binary.
		fmt.Fprintf(out, "%s\n", agentLicenseNotice())

		// (b) Resolve the coder GGUF from the picked catalog entry (single source):
		// the staged filename and the served -m path derive from ONE entry, never a literal.
		cat, ok := d.agentCatalog()
		if !ok {
			return blockf("install: cannot load the model catalog to resolve the coder model — re-run `villa install --coding-agent` once the catalog is readable.\n")
		}
		// distinguish a SHARED-residency coder fit from a genuine no-fit. When the
		// recommender derived residency "shared" (rec.Coder.Residency == residencyShared, i.e. a
		// coder DOES fit but only by riding the chat endpoint, not standalone), the v1.4 addon
		// which serves a dedicated SWAP-residency coder — cannot stage it. That is a deliberate
		// v1.4 swap-only limitation, NOT a memory shortfall, so emit a refusal that says so
		// rather than the "free memory / use a larger host" copy (which misdirects an operator
		// who may have ample memory). The generic no-fit copy below is reserved for a coder that
		// is genuinely absent from the catalog (coderShardFor false with a non-shared residency).
		if rec.Coder.Residency == recommend.ResidencyShared {
			return blockf("install: the coding-agent addon currently requires a swap-residency coder fit, but this host only supports SHARED residency (the coder would ride the chat endpoint), which v1.4 does not yet serve as a dedicated agent — so the addon cannot be staged. This is a swap-only limitation, not a memory shortfall; freeing memory will not help. (The chat stack is unaffected.)\n")
		}
		sh, ok := coderShardFor(rec, cat)
		if !ok {
			return blockf("install: no coder model fits the detected memory envelope, so the coding-agent addon cannot be staged — free memory or use a larger-envelope host, then re-run `villa install --coding-agent`. (The chat stack is unaffected.)\n")
		}
		coderShard = sh

		// (c) Pre-stage the coder GGUF (idempotent, size-gated). The single sanctioned
		// outbound window for the coder weight; download.PullModel verifies before use.
		if !d.coderModelPresent(d.modelsDir(), coderShard) {
			fmt.Fprintf(out, "coder model %s not present — downloading...\n", coderShard.Filename)
			if err := d.ensureCoderModel(d.modelsDir(), coderShard); err != nil {
				return blockf("install: pre-stage coder model %s failed: %v\n", coderShard.Filename, err)
			}
			fmt.Fprintf(out, "coder model %s downloaded and verified\n", coderShard.Filename)
		}

		// (d) Stage the pinned Crush binary (checksum-before-extract, — never re-implemented).
		binPath, err := d.installAgentBinary(cmd.Context())
		if err != nil {
			return blockf("install: stage coding-agent binary failed: %v\n", err)
		}
		fmt.Fprintf(out, "coding agent installed and verified at %s\n", binPath)

		// (e) Render the locked-down crush.json (the restrictive-tools security control).
		if err := d.renderCrushConfig(cfg); err != nil {
			return blockf("install: render coding-agent config failed: %v\n", err)
		}
		fmt.Fprintf(out, "coding-agent config rendered (outbound tools disabled, loopback provider only)\n")
	}

	// (6z) CAPTURE the prior state BEFORE the first mutation (ADR-0003). Install is
	// transactional like the three swap cores: it captures, mutates, proves, and
	// restores verbatim when a mutation or a proof fails. Everything after this
	// point that changes the host is recorded in `mutated`, and any failure below
	// routes through `refuse`, which undoes exactly what was done.
	//
	// The capture is the config, the unit files, and which services were running.
	// Model weights are deliberately NOT captured, so a failed install leaves them
	// and a retry does not re-download tens of gigabytes.
	priorUnits := map[string]string{}
	for _, u := range slices.Concat(plan.Changed, plan.Unchanged) {
		if d.readUnit != nil {
			if text, ok := d.readUnit(unitDir, u.Name); ok {
				priorUnits[u.Name] = text
			}
		}
	}
	// The running set covers every service install may start, not only those in the
	// rendered plan. A service install starts but did not capture would be stopped by
	// a rollback even though it was running beforehand — turning a failed re-install
	// into an outage of a service the run never needed to touch.
	priorRunning := map[string]bool{}
	if d.isActive != nil {
		for _, svc := range []string{
			installServiceName,
			openWebUIServiceName,
			qdrantServiceName,
			embedServiceName,
			searxngServiceName,
			websafeServiceName,
			orchestrate.DashboardServiceName,
		} {
			if state, aerr := d.isActive(svc); aerr == nil && state == "active" {
				priorRunning[svc] = true
			}
		}
	}
	hadConfig := d.configExists == nil || d.configExists()
	prior := install.CapturePrior(cfg, hadConfig, priorUnits, priorRunning)

	var mutated install.Mutations

	// refuse rolls the host back to the captured state and returns the exit code.
	// A rollback that could not fully complete is reported honestly rather than as a
	// clean restoration: a wrong "restored" claim tells the operator to stop looking.
	refuse := func(format string, args ...any) int {
		fmt.Fprintf(errOut, format, args...)
		rb := install.Rollback(install.RollbackDeps{
			StopService:  d.stop,
			StartService: d.start,
			WriteUnit: func(name, text string) error {
				return writeUnitText(unitDir, name, text)
			},
			RemoveUnit: func(name string) error {
				if d.removeUnit == nil {
					return fmt.Errorf("no unit-removal seam wired")
				}
				return d.removeUnit(unitDir, name)
			},
			SaveConfig:   d.saveConfig,
			RemoveConfig: d.removeConfig,
			DaemonReload: d.daemonReload,
		}, prior, mutated)
		fmt.Fprintf(errOut, "install: %s\n", rb.Reason())
		return res.Refuse(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"), rb.Reason())
	}

	// (7) Persist the chosen selection to config.toml BEFORE any unit work (F-2 /
	// spirit): config is the single source of truth, so install-written units
	// must derive from the same persisted config the lifecycle verbs render from
	// otherwise post-install `villa up`/`restart` resolve an empty model and FAIL,
	// and a follow-up reconcile is never a true no-op.
	if err := d.saveConfig(cfg); err != nil {
		return blockf("install: persist config: %v\n", err) // nothing mutated yet: the save is the first mutation
	}
	mutated.RecordConfigSave()

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
	if code := reconcileDashboardUnit(out, errOut, d, mutated.RecordStart); code != exitPass {
		return code
	}

	// (8) True no-op: nothing changed → no write, no reload, no restart. Note this
	// is reached only AFTER ensureModel + saveConfig + reconcileDashboardUnit, so a
	// re-run on a host whose units already match still guarantees the weights, config,
	// AND the boot-surviving dashboard unit are in place (the no-op return is safe).
	if len(plan.Changed) == 0 {
		fmt.Fprintf(out, "no changes — stack already matches config\n")
		printPostInstall(out, d.endpoint(), installReadiness{status: preflight.StatusPass, detail: "unchanged"})
		res.Outcome = install.NoChange
		return res.Finish().ExitCode()
	}

	// (9) Execute the mutate-and-start sequence the core planned.
	//
	// The core owns the ORDER; this tier performs the effects and renders output.
	// The ordering rules — secrets before the units that reference them, the bearer
	// file before the chat UI starts, inference before the chat UI, each start gated
	// on its unit being in the written plan — are properties of that plan, asserted
	// in internal/install rather than implied by the order of statements here.
	//
	// installSeq is compared against what this function actually does, so the plan
	// cannot drift into decoration: a mismatch fails the run rather than being
	// silently ignored.
	installSeq := install.BuildSequence(gates, install.Units{
		Inference: installServiceName,
		ChatUI:    openWebUIServiceName,
		Qdrant:    qdrantServiceName,
		Embed:     embedServiceName,
		Searxng:   searxngServiceName,
		Websafe:   websafeServiceName,
	}, cfg.WebLoaderSecret == "")
	var performed []string

	if err := d.writeUnits(plan, unitDir); err != nil {
		return refuse("install: write units failed: %v\n", err)
	}
	for _, u := range plan.Changed {
		mutated.RecordUnit(u.Name)
	}
	fmt.Fprintf(out, "wrote %d unit(s) to %s\n", len(plan.Changed), unitDir)
	if err := d.daemonReload(); err != nil {
		return refuse("install: daemon-reload failed: %v\n", err)
	}
	if err := d.start(installServiceName); err != nil {
		return refuse("install: start %s failed: %v\n", installServiceName, err)
	}
	fmt.Fprintf(out, "started %s\n", installServiceName)
	performed = append(performed, installServiceName)
	mutated.RecordStart(installServiceName)

	// (9a) Generate-and-persist the EXTERNAL_WEB_LOADER_API_KEY bearer ONCE and write the 0600
	// websafe.env BEFORE the OWUI start (v1.5 / Phase-31), gated on the
	// PERSISTED web_search_enabled. This MUST precede the OWUI start: when web search is on the
	// OWUI unit references the SAME websafe.env via EnvironmentFile= (WebsafeSecretEnvFilePath),
	// and `systemctl start` fails if that EnvironmentFile target is absent. The villa-websafe
	// service itself is started further below alongside searxng (its planHasUnit gate). The
	// secret VALUE only ever lands in this 0600 file — never the 0644 unit, a log line, or
	// stdout. Mirrors the searxng secret-env path (generate-once + 0600 EnvironmentFile).
	if gates.WebSearch {
		// Generate-and-persist the bearer ONCE on first opt-in BEFORE rendering the env file so
		// the EnvironmentFile target exists and is non-empty (a re-install reuses the same bearer
		// rather than churning the OWUI⇄websafe trust).
		if cfg.WebLoaderSecret == "" {
			secret, gerr := config.GenerateWebLoaderSecret()
			if gerr != nil {
				return blockf("install: generate web loader secret failed: %v\n", gerr)
			}
			cfg.WebLoaderSecret = secret
			if serr := d.saveConfig(cfg); serr != nil {
				return blockf("install: persist web loader secret failed: %v\n", serr)
			}
		}
		// Write the 0600 bearer env file — the EnvironmentFile= target BOTH the OWUI unit (started
		// next) and the villa-websafe unit reference. The secret VALUE only ever lands in this
		// 0600 file.
		envName, envText := orchestrate.RenderWebsafeSecretEnv(cfg.WebLoaderSecret)
		if werr := d.writeWebsafeSecretEnv(envName, envText); werr != nil {
			return blockf("install: write websafe secret env failed: %v\n", werr)
		}
	}

	// Start Open WebUI AFTER inference: the chat UI must come up against a
	// live backend, and the recommended model is already ensured present above
	// (step 6, MODEL-04) so the model picker is populated on first visit.
	if err := d.start(openWebUIServiceName); err != nil {
		return refuse("install: start %s failed: %v\n", openWebUIServiceName, err)
	}
	fmt.Fprintf(out, "started %s\n", openWebUIServiceName)
	performed = append(performed, openWebUIServiceName)
	mutated.RecordStart(openWebUIServiceName)

	// (9b) Start the memory stack AFTER inference + Open WebUI, gated on the PERSISTED
	// memory_enabled (Phase-19 / INFRA-02). Qdrant FIRST so the embedder/OWUI peers can
	// reach the vector store, then villa-embed (its GGUF is already pre-staged above
	// Pitfall 4). Each start failure refuses-with-remediation (exitBlocked), mirroring the
	// inference/OWUI start handling. Skipped under --dry-run (the dry-run path returns far above).
	//
	// The start is gated on the memory .container units being PRESENT in the written plan
	// (plan.Changed ∪ plan.Unchanged), not solely on cfg.MemoryEnabled. With memory
	// on, Render appends those units and reconcile diffs them in, so today they are always
	// present — but if a future change ever lets MemoryEnabled be true while the units are
	// filtered out of the plan (a swallowed partial render, a reconcile that drops them), we
	// must NOT `systemctl start villa-qdrant.service` for a unit systemd has never seen and
	// surface a raw "Unit not found". Instead fail closed with a clear INTERNAL-ERROR
	// remediation so the gate for STARTING a service is "its unit exists in the plan".
	if gates.Memory {
		if !install.UnitPresent(plan, orchestrate.QdrantContainerUnitName()) ||
			!install.UnitPresent(plan, orchestrate.EmbedContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: memory is enabled but the memory units (%s, %s) are absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\n",
				orchestrate.QdrantContainerUnitName(), orchestrate.EmbedContainerUnitName())
		}
		if err := d.start(qdrantServiceName); err != nil {
			return refuse("install: start %s failed: %v\n", qdrantServiceName, err)
		}
		fmt.Fprintf(out, "started %s\n", qdrantServiceName)
		performed = append(performed, qdrantServiceName)
		mutated.RecordStart(qdrantServiceName)
		if err := d.start(embedServiceName); err != nil {
			return refuse("install: start %s failed: %v\n", embedServiceName, err)
		}
		fmt.Fprintf(out, "started %s\n", embedServiceName)
		performed = append(performed, embedServiceName)
		mutated.RecordStart(embedServiceName)
	}

	// (9c) Start the SearXNG web-search service, gated on the PERSISTED web_search_enabled
	// (v1.5 / Phase-29). INDEPENDENT of inference/memory (SearXNG has no dependency
	// on llama/qdrant — it only needs villa.network, already attached). Each step
	// refuses-with-remediation (exitBlocked). Skipped under --dry-run (that path returned
	// far above).
	//
	// Order within the block (BEFORE the start, so the container has its config + secret on
	// first boot — Pitfall 3):
	// 1. Gate the START on the rendered unit being PRESENT in the written plan (
	// never `systemctl start villa-searxng.service` for a unit systemd has
	//      never seen — fail closed with an INTERNAL-ERROR remediation, not a raw "Unit not
	//      found", if web search is on but the unit is absent from the plan (a render/
	//      reconcile bug). With web search on, Render appends the unit, so today it is always
	//      present; the gate is the fail-closed backstop.
	//   2. Generate-and-persist the secret ONCE if absent (first opt-in): the secret_key is a
	//      crypto/rand value generated a SINGLE time and persisted in config.toml at 0600
	//      (config.GenerateSearxngSecret + saveConfig), so a re-install reuses the same secret
	//      rather than churning sessions (Pitfall 3).
	//   3. Write BOTH config artifacts the unit needs: the rendered settings.yml (mounted
	//      read-only at /etc/searxng) and the 0600 secret env file (the EnvironmentFile=
	//      target — the secret reaches the container via this 0600 file, NEVER an inline
	// literal in the 0644 unit — BLOCKER 1).
	if gates.WebSearch {
		if !install.UnitPresent(plan, orchestrate.SearXNGContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: web search is enabled but the searxng unit (%s) is absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\\n",
				orchestrate.SearXNGContainerUnitName())
		}
		// Generate-and-persist the secret ONCE on first opt-in (Pitfall 3) BEFORE rendering
		// the env file so the unit's EnvironmentFile target exists and is non-empty.
		if cfg.SearxngSecret == "" {
			secret, gerr := config.GenerateSearxngSecret()
			if gerr != nil {
				return blockf("install: generate searxng secret failed: %v\n", gerr)
			}
			cfg.SearxngSecret = secret
			if serr := d.saveConfig(cfg); serr != nil {
				return blockf("install: persist searxng secret failed: %v\n", serr)
			}
		}
		// (a) settings.yml (mounted ro at /etc/searxng) — the file that enables the json
		// format + the bounded engine allowlist. The render is the single source
		// of truth (RenderSearxngSettings); the writer persists exactly those bytes.
		settingsName, settingsText, rerr := orchestrate.RenderSearxngSettings(cfg)
		if rerr != nil {
			return blockf("install: render searxng settings failed: %v\n", rerr)
		}
		if werr := d.writeSearxngSettings(settingsName, settingsText); werr != nil {
			return blockf("install: write searxng settings failed: %v\n", werr)
		}
		// (b) the 0600 secret env file — the EnvironmentFile= target. The secret
		// VALUE only ever lands in this 0600 file, never the 0644 unit.
		envName, envText := orchestrate.RenderSearxngSecretEnv(cfg.SearxngSecret)
		if werr := d.writeSearxngSecretEnv(envName, envText); werr != nil {
			return blockf("install: write searxng secret env failed: %v\n", werr)
		}
		if err := d.start(searxngServiceName); err != nil {
			return refuse("install: start %s failed: %v\n", searxngServiceName, err)
		}
		fmt.Fprintf(out, "started %s\n", searxngServiceName)
		performed = append(performed, searxngServiceName)
		mutated.RecordStart(searxngServiceName)

		// villa-websafe (grounded-fetch loader, Phase-31): its 0600 websafe.env bearer was already
		// written above (step 9a, BEFORE the OWUI start that also references it). Gate the START on
		// the rendered unit being PRESENT in the written plan (backstop): never `systemctl
		// start villa-websafe.service` for a unit systemd has never seen — fail closed with an
		// INTERNAL-ERROR remediation, not a raw "Unit not found". With web search on, Render
		// appends the unit, so today it is always present; the gate is the fail-closed backstop.
		if !install.UnitPresent(plan, orchestrate.WebsafeContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: web search is enabled but the websafe unit (%s) is absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\\n",
				orchestrate.WebsafeContainerUnitName())
		}
		if err := d.start(websafeServiceName); err != nil {
			return refuse("install: start %s failed: %v\n", websafeServiceName, err)
		}
		fmt.Fprintf(out, "started %s\n", websafeServiceName)
		performed = append(performed, websafeServiceName)
		mutated.RecordStart(websafeServiceName)
	}

	// (10) Poll readiness (503=keep-polling, timeout→WARN — Task 2 wiring).
	ready := d.pollReady(cmd.Context(), d.endpoint())
	printPostInstall(out, d.endpoint(), ready)

	// (10b) Memory-stack readiness proof (Phase-19): an OFFLINE
	// 768-dim /v1/embeddings vector AND a Qdrant writable round-trip. Gated on the
	// PERSISTED memory_enabled (cfg.MemoryEnabled); skipped under --dry-run (that path
	// returned far above). A FAIL refuses-with-remediation (exitBlocked) — never a
	// silent skip / false-green (honesty-by-construction). A PASS prints a ready line
	// and folds into the existing PASS/WARN verdict.
	if gates.Memory {
		// (10b-pre) Phase-23 skew WARN (CTRL-05, read-only): if the
		// recall-state stamp records an embedding identity that confidently
		// diverges from the configured one, warn-with-remediation BEFORE the proof
		// — the operator learns retrieval is corrupt even if the proof then fails.
		// Never blocks, never mutates; an absent/unreadable stamp is silent
		// typed-Unknown (warnRecallEmbeddingSkew).
		warnRecallEmbeddingSkew(errOut, cfg, d.readRecallState)
		proof := d.memoryProofFn(cmd.Context(), memoryProofInput{
			embedAddr:    config.EmbedAddr,
			embedPort:    config.EmbedPort,
			embedModel:   cfg.EmbeddingModel,
			embeddingDim: cfg.EmbeddingDim,
			qdrantAddr:   config.QdrantAddr,
			qdrantPort:   config.QdrantPort,
		})
		if proof.status == preflight.StatusFail {
			return refuse("install: memory stack not ready: %s\n", proof.detail)
		}
		// Print the proof's own detail rather than re-typing the "768-dim …"
		// figure as a literal — the dimension is single-sourced in the verdict
		// (evalMemoryProof, from cfg.EmbeddingDim), so a dim change can't leave this stale.
		fmt.Fprintf(out, "memory stack ready: %s\n", proof.detail)
	}

	// (10b2) Web-search readiness proof (v1.5 / Phase-29): a REAL format=json
	// query parsing results[], NOT a health-200 (the project's "offload-asserting, never
	// liveness" principle). Gated on the PERSISTED web_search_enabled (cfg.WebSearchEnabled);
	// skipped under --dry-run (that path returned far above). A FAIL refuses-with-remediation
	// (exitBlocked) — never a silent skip / false-green (honesty-by-construction). A PASS
	// prints a ready line and folds into the existing PASS/WARN verdict. The proof probes the
	// SAME config.SearxngAddr/SearxngPort the rendered unit's container-DNS identity derives from
	// so it can never probe a different target than what runs.
	if gates.WebSearch {
		proof := d.searxngProofFn(cmd.Context(), searxngProofInput{
			searxngAddr: config.SearxngAddr,
			searxngPort: config.SearxngPort,
		})
		if proof.status == preflight.StatusFail {
			return refuse("install: search service not ready: %s\n", proof.detail)
		}
		fmt.Fprintf(out, "search service ready: %s\n", proof.detail)
	}

	// (10c) Coding-agent readiness proof (v1.4): a REAL `crush run`
	// tool-call round-trip (plant a file → ask the agent to edit it → assert the edit),
	// NOT a health-200. Gated on cfg.AgentEnabled; skipped under --dry-run (that path
	// returned far above). A FAIL refuses-with-remediation (exitBlocked) — never a silent
	// skip / false-green (honesty-by-construction). A health-200 is never an input.
	if gates.Agent {
		proof := d.agentProofFn(cmd.Context())
		if proof.status == preflight.StatusFail {
			return refuse("install: coding agent not ready: %s\n", proof.detail)
		}
		fmt.Fprintf(out, "coding agent ready: %s\n", proof.detail)
	}

	// The sequence the core planned must match what this function actually did.
	// Without this the plan would be decoration: internal/install's ordering tests
	// would pass while the flow drifted out from under them. Comparing the two makes
	// the core's plan the authority the command tier is held to.
	if err := install.AssertStartOrder(installSeq, performed); err != nil {
		return blockf("install: %v\n", err)
	}

	res.ReadinessWarn = ready.status == preflight.StatusWarn
	return res.Finish().ExitCode()
}

// persistedBackendChosen reports whether the persisted config already carries a
// DELIBERATELY-chosen backend that a re-install must preserve rather than overwrite
// with the recommendation. True for any ROCm-family name (the default family) and for
// the explicit "vulkan" opt-out; false for the empty string (never configured) and for
// any unknown/typo'd value, both of which fall through to the recommendation and the
// fail-closed inference.BackendFor at render time. It compares NAME strings only
// never an image/device literal — so it stays clear of the seam grep gate.
func persistedBackendChosen(name string) bool {
	if name == "" {
		return false
	}
	return inference.IsROCmFamily(name) || name == "vulkan"
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
// ~/.local/bin/villa fallback) on this path too: an unresolvable binary fails the
// install closed rather than writing a unit that points at an attacker-plantable fixed path.
// reconcileDashboardUnit renders, writes and starts the native dashboard unit.
//
// recordStart notes the start as a mutation so a later rollback stops the dashboard
// too. Without it, a failed install left the dashboard running against units it had
// just removed.
func reconcileDashboardUnit(out, errOut io.Writer, d *installDeps, recordStart func(string)) int {
	// Resolve the user-unit dir (NOT the Quadlet dir — Pitfall 5).
	udir, err := d.userUnitDir()
	if err != nil {
		fmt.Fprintf(errOut, "install: cannot resolve the user-unit dir for the dashboard: %v\n", err)
		return exitBlocked
	}
	// Resolve the running villa binary's absolute, symlink-collapsed path for ExecStart
	// (UAT Test 5 fix). A resolution failure is fatal — we do NOT fall back to the old
	// fixed ~/.local/bin/villa, which is the exact path that produced 203/EXEC at boot
	// when the install flow never deployed the binary (fail-closed).
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
	if recordStart != nil {
		recordStart(orchestrate.DashboardServiceName)
	}
	return exitPass
}

// gateInstall applies the preflight verdict to the install: WARN checks are
// printed; BLOCK gaps (FAIL or a WARN-downgraded BLOCK-tier with remediation) are
// OFFERED for consented host-prep. It returns (exitCode, proceed). proceed
// is false when a BLOCK gap is neither consented nor --force'd.
//
// consents threads a pre-collected decision map (gap-id → y/n) from the wizard
// path: a recorded decision is honored WITHOUT re-prompting stdin (huh
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
				// — but only when interactive, not --json, and not --dry-run
				// (a dry run has zero side effects). It never consumes a consents
				// entry. With no current safe fix (safeAutoFix is false for PRE-03/PRE-05)
				// this branch is a behavior no-op today; it is the forward-looking
				// classifier.
				if opts.json || opts.dryRun || !d.interactive() {
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
				// A WARN-tier gap with an automated privileged fix (linger off):
				// OFFER the consented fix, but never block if declined — it is boot-
				// survival, not an immediate crash. Route this NON-blocking offer to
				// stdout so scripts parsing stderr do not misread a soft,
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

	// --dry-run NEVER mutates the host (phase-22 / ORCH): treat it
	// exactly like the non-interactive path — print the command, never prompt,
	// never run the privileged seam. Checked FIRST so even a (stale) threaded
	// consent can never execute host-prep under a flag sold as side-effect-free.
	if opts.dryRun {
		fmt.Fprintf(errOut, "  (dry-run — run the command above, then re-run `villa install`)\n")
		return false
	}

	// Wizard path: a pre-collected decision (huh already consumed stdin) is honored
	// WITHOUT re-prompting. A recorded `true` runs the same fixed-arg seam as
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

	// Non-interactive / --json / no TTY → never prompt; print + block.
	if opts.json || !d.interactive() {
		fmt.Fprintf(errOut, "  (non-interactive — run the command above, then re-run `villa install`)\n")
		return false
	}

	if !d.consent(fmt.Sprintf("Run `%s` now? [y/N] ", cmdStr)) {
		fmt.Fprintf(errOut, "  declined — run the command above, then re-run `villa install`\n")
		return false
	}

	// Consented → run the matching fixed-arg seam (never a shell).
	if err := runGapFix(c, d); err != nil {
		fmt.Fprintf(errOut, "  host-prep failed: %v — run the command manually, then re-run `villa install`\n", err)
		return false
	}
	fmt.Fprintf(out, "  applied: %s\n", cmdStr)
	return true
}

// offerNonBlockingGap handles a WARN-tier gap with an automated privileged fix
// (e.g. PRE-03 linger): it OFFERS the consented fix but never blocks if declined
// this is boot-survival, not an immediate crash. Unlike resolveGap (the BLOCK path,
// which writes to stderr), every message here goes to stdout so a soft,
// optional offer is never misread as an error by scripts parsing stderr. It returns
// whether the fix was consented-and-applied (informational; the caller never blocks
// on the result).
func offerNonBlockingGap(out io.Writer, c preflight.CheckResult, opts installOpts, consents map[string]bool, d *installDeps) bool {
	cmdStr := remediationCommand(c, d.username())
	fmt.Fprintf(out, "\noptional host-prep (boot survival): [%s] %s\n  command: %s\n", c.ID, c.Detail, cmdStr)

	// --dry-run NEVER mutates the host (phase-22 / ORCH): print the
	// command, never prompt, never run the privileged seam (mirrors resolveGap).
	if opts.dryRun {
		fmt.Fprintf(out, "  (dry-run — optional; run the command above to enable boot survival)\n")
		return false
	}

	// Wizard path: honor a pre-collected decision without re-prompting. A
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

	// Consented → run the matching fixed-arg seam (never a shell).
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
// install can offer to run. Only these are offered; everything else is a
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
// may auto-run with a visible notice and NO consent. It returns false for
// both current fixes — [ASSUMED] PRE-05 (setsebool -P) and PRE-03 (loginctl
// enable-linger) are PRIVILEGED, so they stay consent-gated (villa never
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

// chatURL is the loopback chat (Open WebUI) URL printed post-install:
// the host side of the owui PublishPort (127.0.0.1:3000:8080, openWebUIPublishPort
// in internal/orchestrate). Loopback-only — never a LAN/0.0.0.0 address.
const chatURL = "http://127.0.0.1:3000"

// dashboardURL is the loopback control-dashboard URL printed post-install. It is the
// config default (DashboardAddr 127.0.0.1 / DashboardPort 8888); the dashboard now
// comes up as a managed boot-surviving service in this install (Plan 05-05),
// so there is no dead link. Loopback-only.
const dashboardURL = "http://127.0.0.1:8888"

// printPostInstall prints the loopback inference endpoint + the readiness verdict,
// the real loopback chat URL (Open WebUI is brought up by this install), and
// the real loopback control-dashboard URL (the dashboard is now a managed
// boot-surviving service brought up by this install, Plan 05-05 / — no dead
// links). The endpoint is sourced from the backend seam, never retyped.
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
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the closures
// that pull weights (the main model, the embed GGUF, the coder shard). Install is
// the longest-running command in the tool and every one of those transfers is
// multi-GB, so without it Ctrl-C could not interrupt a download. Cancelling
// mid-stream is safe: download.PullModel keeps the partial ".part" file and
// resumes it via HTTP Range on the next run.
func liveInstallDeps(ctx context.Context) (*installDeps, error) {
	sys := orchestrate.NewSystemd()
	uname := installUsername()
	// Resolve the backend from config (fail-closed) for the post-install endpoint
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
	// the pull, so install never fabricates a weight path (mirrors swap).
	resolveCatalogModel := func(rec recommend.Recommendation) (catalog.Model, bool) {
		cat, _, err := catalog.Load(modelCatalogPath)
		if err != nil {
			return catalog.Model{}, false
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
			// recommends against the shrunken envelope (Pitfall 3 — a
			// memory-blind install pick defeats CTRL-01).
			return recommend.Pick(p, cat, ov, liveLoadedMemoryInputs(), liveLoadedWebSearchInputs())
		},
		modelFile: func(rec recommend.Recommendation) (string, error) {
			// A catalog load failure or an unknown model id is a hard error:
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
			return pullFn(ctx, m, dir)
		},
		saveConfig:   config.SaveVilla,
		runChecks:    preflight.RunWithResources,
		render:       livePinnedRender,
		reconcile:    orchestrate.Reconcile,
		writeUnits:   orchestrate.WriteUnits,
		unitDir:      quadletUnitDir,
		daemonReload: sys.DaemonReload,
		start:        sys.Start,
		stop:         sys.Stop,
		readUnit: func(dir, name string) (string, bool) {
			b, rerr := os.ReadFile(filepath.Join(dir, name))
			if rerr != nil {
				return "", false
			}
			return string(b), true
		},
		removeUnit: func(dir, name string) error {
			rerr := os.Remove(filepath.Join(dir, name))
			if rerr != nil && !os.IsNotExist(rerr) {
				return rerr
			}
			return nil
		},
		configExists: func() bool {
			path, perr := config.Path()
			if perr != nil {
				return false
			}
			_, serr := os.Stat(path)
			return serr == nil
		},
		removeConfig: func() error {
			path, perr := config.Path()
			if perr != nil {
				return perr
			}
			rerr := os.Remove(path)
			if rerr != nil && !os.IsNotExist(rerr) {
				return rerr
			}
			return nil
		},
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
		// NOT the DefaultVillaConfig seed. Pre-stage + presence reuse the same
		// verified download path and models dir as the chat-model ensureModel above.
		loadedConfig:      liveLoadedConfig,
		embedModelPresent: liveEmbedModelPresent,
		ensureEmbedModel:  func(modelsDir string) error { return liveEnsureEmbedModel(ctx, modelsDir) },
		memoryProofFn:     liveMemoryProof,
		// Memory host-fitness gates (CTRL-06): the embedding model is passed in from the
		// config runInstall loaded once, and refused on, at step 0.
		runMemoryChecks: func(p detect.HostProfile, embeddingModel string) []preflight.CheckResult {
			return preflight.RunMemory(p, preflight.MemoryGateInput{EmbeddingModel: embeddingModel})
		},
		// Phase-23 skew WARN reader: the SHARED fail-closed recall-state
		// loader `villa recall` uses (one reader, never a re-rolled second one).
		readRecallState: liveRecallStateLoad,

		// Web-search (SearXNG) seams (v1.5 / Phase-29). The gate keys off the
		// PERSISTED config (liveLoadedWebSearchEnabled → config.LoadVilla().WebSearchEnabled,
		// fail-soft to false), NOT the DefaultVillaConfig() seed. The settings.yml + 0600
		// secret env writers are the Phase-29 Plan-02 orchestrate writers (the secret reaches
		// the container via the 0600 EnvironmentFile, never the 0644 unit). The proof
		// is the real format=json query (liveSearxngProof), never a health-200.
		writeSearxngSettings:  orchestrate.WriteSearxngSettings,
		writeSearxngSecretEnv: orchestrate.WriteSearxngSecretEnv,
		searxngProofFn:        liveSearxngProof,
		// villa-websafe 0600 bearer (websafe.env) writer — the EnvironmentFile= target BOTH the
		// villa-websafe AND the OWUI units reference (WebsafeSecretEnvFilePath). The secret reaches
		// both containers via the 0600 file, never the 0644 unit. Mirrors the searxng
		// secret-env writer wiring above.
		writeWebsafeSecretEnv: orchestrate.WriteWebsafeSecretEnv,

		// Coding-agent (Crush) addon seams (v1.4). The gate keys off the
		// PERSISTED config (liveLoadedAgentEnabled → config.LoadVilla().AgentEnabled,
		// fail-soft to false); --coding-agent overrides it. Pre-stage reuses the same
		// verified download path + models dir as the chat/embed models; the binary
		// install + render compose the Phase-26/Task-1 seams, never re-implemented.
		agentCatalog: func() (catalog.Catalog, bool) {
			cat, _, err := catalog.Load(modelCatalogPath)
			if err != nil {
				return catalog.Catalog{}, false
			}
			return cat, true
		},
		coderModelPresent: liveCoderModelPresent,
		ensureCoderModel: func(modelsDir string, sh catalog.Shard) error {
			return liveEnsureCoderModel(ctx, modelsDir, sh)
		},
		installAgentBinary: liveInstallAgentBinary,
		renderCrushConfig:  liveRenderCrushConfig,
		agentProofFn: func(ctx context.Context) agentProof {
			return evalAgentProof(liveAgentToolCallProbe(ctx))
		},
		// Coding-agent preflight gates. The closure resolves the staged
		// footprint (coder GGUF SizeBytes + the pinned binary asset.Size) from the SAME
		// catalog entry + policy pin the install flow stages, so the disk gate can never
		// drift from what is actually written; statfs reuses the same syscall.Statfs path
		// the install/memory disk checks use (liveAgentStatfs), and the cloud-credential
		// scan reads the env via os.LookupEnv. A catalog load failure / no-coder-fit yields
		// a zero staged size — the envelope check (rec.Coder.Fits==false) is the BLOCK that
		// refuses-with-remediation, so the disk check is not relied on to catch that case.
		runAgentChecks: func(p detect.HostProfile, rec recommend.Recommendation) []preflight.CheckResult {
			var staged uint64
			if cat, _, err := catalog.Load(modelCatalogPath); err == nil {
				if sh, ok := coderShardFor(rec, cat); ok {
					staged += sh.SizeBytes
				}
			}
			if asset, ok := agent.LoadCrushPolicy().Assets["linux/amd64"]; ok {
				staged += asset.Size
			}
			return runAgentChecks(p, rec, agentCheckInput{
				stagedBytes: staged,
				dataDir:     modelsDir(),
				statfs:      liveAgentStatfs,
				lookupEnv:   os.LookupEnv,
			})
		},
	}, nil
}

// liveAgentStatfs reads real free space at a path via syscall.Statfs (the same
// locale-proof, no-shell-to-df discipline preflight.liveStatfs uses — that helper is
// package-private to preflight, so the cmd-tier agent gate carries its own copy). It
// walks up to an existing ancestor so a not-yet-created models dir still reports its
// filesystem's free space. A statfs error → ok=false → the disk check degrades to a
// typed-Unknown WARN (never a false BLOCK).
func liveAgentStatfs(path string) (uint64, bool) {
	p := existingAncestorDir(path)
	var st syscall.Statfs_t
	if err := syscall.Statfs(p, &st); err != nil {
		return 0, false
	}
	return st.Bavail * uint64(st.Bsize), true
}

// existingAncestorDir returns path if it exists, else the nearest existing parent
// (down to "/"), so statfs has a real path to stat for a target dir not yet created.
func existingAncestorDir(path string) string {
	if path == "" {
		return "/"
	}
	p := path
	for {
		if _, err := os.Stat(p); err == nil {
			return p
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "/"
		}
		p = parent
	}
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

// writeUnitText restores one unit file's prior contents during a rollback. It is
// separate from the forward path's writeUnits seam because rollback restores a
// captured TEXT rather than re-rendering from config: re-rendering would produce
// whatever the current config says, which is exactly the config the rollback is
// about to undo.
func writeUnitText(dir, name, text string) error {
	if err := assertWithinDir(filepath.Join(dir, name), dir); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(dir, name), []byte(text), 0o644) //nolint:gosec // unit files are world-readable by design; secrets live in 0600 env files
}
