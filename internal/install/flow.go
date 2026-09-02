package install

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"slices"
	"strconv"
	"strings"

	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// flow.go is the install flow itself: Run drives detect → recommend → gate →
// render → (dry-run) → pre-stage → capture → persist → write → start → prove, and
// returns the Result. ADR-0005 records the shape.
//
// The flow narrates through Deps.Emit rather than printing: every line it would
// have written to stdout or stderr is handed to the seam as it happens, so a
// model pull is still narrated before it runs while the core owns no writer.
// Every host effect is a Deps field; the command tier wires the live host and the
// tests wire a fake, so the invariants here (gates resolved once, refusal before
// mutation, rollback on a failure after it) are tested at this interface.

// Line is one narration line. Stderr routes it there; the rest go to stdout.
type Line struct {
	Stderr bool
	Text   string
}

// Proof is a readiness or proof verdict: PASS, WARN (a poll that could not
// confirm) or FAIL (a confident known-bad), with the operator-facing detail.
type Proof struct {
	Status preflight.Status
	Detail string
}

// WizardInput is what the guided wizard presents. The wizard is a pure
// COLLECTOR: it receives the already-computed profile, recommendation and checks,
// collects a model override plus per-item privileged consent, and returns; the
// single gate in Run consumes the consent, so a privileged fix runs at most once
// on either path.
type WizardInput struct {
	Profile detect.HostProfile
	Rec     recommend.Recommendation
	Checks  []preflight.CheckResult
	Backend inference.Backend
}

// WizardResult is what the wizard collected.
type WizardResult struct {
	// ModelOverride is a catalog id chosen from the alternatives, or empty to keep
	// the recommendation. It is re-validated through the same Pick seam.
	ModelOverride string
	// Consents maps a gap id to the operator's decision. A recorded decision is
	// honoured without re-prompting; an unrecorded id falls through to Consent.
	Consents map[string]bool
}

// Deps are the effects the flow performs and the answers it reads from the host.
// The command tier wires them to the real host in liveInstallDeps; tests wire a
// fake. NIL-SAFE fields say so; every other field is required.
type Deps struct {
	// Emit receives each narration line as it happens. NIL-SAFE: a nil seam
	// discards narration (the Result still carries the outcome).
	Emit func(Line)

	// LoadConfig returns the PERSISTED config, propagating a load error. Run seeds
	// from it and REFUSES on an error: installing from defaults would overwrite the
	// user's settings with seed values.
	LoadConfig func() (config.VillaConfig, error)
	Probe      func() detect.HostProfile
	// Pick recommends a fitting model. It takes Overrides so a wizard choice is
	// re-validated through the single polymorphism point.
	Pick      func(detect.HostProfile, recommend.Overrides) recommend.Recommendation
	ModelFile func(recommend.Recommendation) (string, error)
	ModelsDir func() string

	RunChecks func(detect.HostProfile, preflight.ResourceReq) []preflight.CheckResult
	// RunMemoryChecks appends the memory host-fitness gates when memory is on.
	// NIL-SAFE: nil appends nothing.
	RunMemoryChecks func(p detect.HostProfile, embeddingModel string) []preflight.CheckResult
	// RunAgentChecks appends the coding-agent gates when the agent is on.
	// NIL-SAFE: nil appends nothing.
	RunAgentChecks func(detect.HostProfile, recommend.Recommendation) []preflight.CheckResult

	// Interactive reports a TTY stdin; StdoutIsTTY a TTY stdout. Both must hold
	// for the wizard.
	Interactive func() bool
	StdoutIsTTY func() bool
	Consent     func(prompt string) bool
	Username    func() string
	Wizard      func(context.Context, WizardInput) (WizardResult, error)
	// Setsebool and EnableLinger are the consented privileged fixes, fixed-arg
	// execs behind the seam.
	Setsebool    func() error
	EnableLinger func(user string) error

	UnitDir func() (string, error)
	// ResidentUnits, HostVillaPath and CodingRender are the catalog→inference
	// translations the render input needs; they stay in the live wiring because
	// the pure renderer never imports the catalog.
	ResidentUnits func(config.VillaConfig) ([]orchestrate.ResidentUnit, error)
	HostVillaPath func() string
	CodingRender  func(config.VillaConfig) (modelFile string, spec *inference.CodingModeSpec, err error)
	Render        func(orchestrate.RenderInput) ([]orchestrate.Unit, error)
	Reconcile     func([]orchestrate.Unit, string) (orchestrate.Plan, error)

	ModelDownloaded    func(recommend.Recommendation) bool
	EnsureModel        func(recommend.Recommendation) error
	EmbedModelPresent  func(modelsDir string) bool
	EnsureEmbedModel   func(modelsDir string) error
	AgentCatalog       func() (catalog.Catalog, bool)
	CoderModelPresent  func(modelsDir string, sh catalog.Shard) bool
	EnsureCoderModel   func(modelsDir string, sh catalog.Shard) error
	InstallAgentBinary func(context.Context) (string, error)
	RenderCrushConfig  func(config.VillaConfig) error

	// Capture and rollback seams (ADR-0003). NIL-SAFE: a nil seam makes the
	// corresponding restore step a no-op, which Rollback reports as incomplete.
	ReadUnit     func(dir, name string) (string, bool)
	WriteUnit    func(dir, name, text string) error
	RemoveUnit   func(dir, name string) error
	IsActive     func(service string) (string, error)
	ConfigExists func() bool
	RemoveConfig func() error

	SaveConfig         func(config.VillaConfig) error
	UserUnitDir        func() (string, error)
	ResolveBinaryPath  func() (string, error)
	ReadDashboardUnit  func(dir string) ([]byte, error)
	WriteDashboardUnit func(dir, binaryPath string) error
	WriteUnits         func(orchestrate.Plan, string) error
	DaemonReload       func() error
	Enable             func(service string) error
	Start              func(service string) error
	Stop               func(service string) error

	WriteWebsafeSecretEnv func(name, text string) error
	WriteSearxngSettings  func(name, text string) error
	WriteSearxngSecretEnv func(name, text string) error

	Endpoint  func() string
	PollReady func(ctx context.Context, endpoint string) Proof
	// ReadRecallState feeds the read-only embedding-skew WARN. NIL-SAFE: nil is
	// silent.
	ReadRecallState func() (recall.State, error)
	ProveMemory     func(context.Context, config.VillaConfig) Proof
	ProveSearch     func(context.Context) Proof
	ProveAgent      func(context.Context) Proof
}

func (d Deps) emit(l Line) {
	if d.Emit != nil {
		d.Emit(l)
	}
}

// say narrates a stdout line; warn a stderr line.
func (d Deps) say(format string, args ...any) { d.emit(Line{Text: fmt.Sprintf(format, args...)}) }
func (d Deps) warn(format string, args ...any) {
	d.emit(Line{Stderr: true, Text: fmt.Sprintf(format, args...)})
}

// ChatURL and DashboardURL are the loopback URLs printed post-install. Loopback
// only, never a LAN address.
const (
	ChatURL      = "http://127.0.0.1:3000"
	DashboardURL = "http://127.0.0.1:8888"
)

// DefaultUnits names the services the flow starts, from the subsystem unit map so
// no service name is re-typed here.
func DefaultUnits() Units {
	_, inf := subsystem.Inference.Units()
	_, chat := subsystem.Chat.Units()
	_, mem := subsystem.Memory.Units()
	_, web := subsystem.WebSearch.Units()
	return Units{Inference: inf[0], ChatUI: chat[0], Qdrant: mem[0], Embed: mem[1], Searxng: web[0], Websafe: web[1]}
}

// Run executes the install flow end to end and returns its Result. It never
// prints and never exits; the exit code is Result.Outcome.ExitCode().
func Run(ctx context.Context, d Deps, opts Opts) Result {
	var res Result
	say, warn := d.say, d.warn
	// block records a pre-mutation stop: Blocked, nothing to roll back.
	block := func(format string, args ...any) Result {
		warn(format, args...)
		res.Block(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"))
		return res
	}
	units := DefaultUnits()

	// (0) Load the PERSISTED config FIRST and REFUSE if it cannot be read. An
	// absent config is not an error (typed defaults); an unreadable one is, because
	// installing from defaults would silently discard the user's settings.
	cfg, err := d.LoadConfig()
	if err != nil {
		warn("install: cannot read the persisted config: %v\n", err)
		return block("install: refusing to install from defaults — that would overwrite your persisted settings with seed values. Fix or remove config.toml, then re-run.\n")
	}

	// (1) Detect, (2) recommend. No fit is a refusal, never a -c 0 server.
	profile := d.Probe()
	rec := d.Pick(profile, recommend.Overrides{})
	if !RecommendationUsable(rec) {
		// The contracted empty-state copy, emitted before the wizard is evaluated so
		// both paths share the one emission point.
		return block("No catalog model fits the detected memory envelope (%s usable). Free memory or supply a larger-envelope host, then re-run villa install. (--no-tui shows the same result.)\n", GiBUsableEnvelope(profile.UsableEnvelopeBytes))
	}
	say("selected model %s (ctx %d, %s)\n", rec.Model, rec.ContextLen, GiB(rec.WeightBytes))

	// (3) Preflight against the concrete requirement; the gates resolved ONCE.
	fit := ResourceFit(rec)
	req := preflight.ResourceReq{MinDiskBytes: fit.MinDiskBytes, MinMemBytes: fit.MinMemBytes, DataDir: d.ModelsDir()}
	gates := ResolveGates(cfg, opts, rec)

	checks := d.RunChecks(profile, req)
	if gates.Memory && d.RunMemoryChecks != nil {
		checks = append(checks, d.RunMemoryChecks(profile, cfg.EmbeddingModel)...)
	}
	if GateAgentChecks(gates) && d.RunAgentChecks != nil {
		checks = append(checks, d.RunAgentChecks(profile, rec)...)
	}

	// (3b) The wizard collects; it never gates. --dry-run never enters it: the
	// wizard collects consent the gate would then EXECUTE, and a dry run has no
	// side effects for consent to apply to.
	var consents map[string]bool
	if d.Interactive() && !opts.JSON && !opts.NoTUI && d.StdoutIsTTY() && !opts.DryRun {
		backend, berr := inference.BackendFor(rec.Backend)
		if berr != nil {
			return block("install: resolve backend for wizard: %v — falling back to the flag path\n", berr)
		}
		w, werr := d.Wizard(ctx, WizardInput{Profile: profile, Rec: rec, Checks: checks, Backend: backend})
		if werr != nil {
			warn("Install cancelled — no changes were made. Re-run villa install, or villa install --no-tui for the flag-driven path.\n")
		}
		// A chosen override is re-validated through the SAME pick seam so the rec is
		// byte-identical to the flag path's. Checks are host-prep, not re-run.
		if w.ModelOverride != "" {
			rec = d.Pick(profile, recommend.Overrides{Model: w.ModelOverride})
			// The gates are a function of the recommendation too (the coder fit
			// decides coding mode), so a changed recommendation is the one case they
			// are resolved again.
			gates = ResolveGates(cfg, opts, rec)
		}
		consents = w.Consents
	}

	switch gate(d, checks, opts, consents) {
	case gateBlocked:
		res.Outcome = Blocked
		return res
	case gateForced:
		// The gap was bypassed, not satisfied: a clean bring-up still degrades.
		res.GateDegraded = true
	}

	// (4) Render from the assembled plan, then reconcile against disk.
	unitDir, err := d.UnitDir()
	if err != nil {
		return block("install: cannot resolve the Quadlet unit dir: %v\n", err)
	}
	plan := AssemblePlan(cfg, gates, rec, PersistedBackendChosen)
	cfg = plan.Config

	modelFile, err := d.ModelFile(rec)
	if err != nil {
		return block("install: resolve model file: %v\n", err)
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return block("install: resolve backend: %v\n", err)
	}
	resident, err := d.ResidentUnits(cfg)
	if err != nil {
		return block("install: resolve resident set: %v\n", err)
	}
	renderIn := orchestrate.RenderInput{
		Backend:       backend,
		Cfg:           cfg,
		ModelFile:     modelFile,
		ModelsDir:     d.ModelsDir(),
		HostVillaPath: d.HostVillaPath(),
		Resident:      resident,
	}
	if gates.CodingMode {
		// Serve the staged coder: the served -m and the descriptor derive from the
		// coder entry, through the same helpers `coding-mode enter` renders with.
		coderModelFile, spec, cerr := d.CodingRender(cfg)
		if cerr != nil {
			return block("install: %v\n", cerr)
		}
		renderIn.ModelFile = coderModelFile
		renderIn.CodingMode = spec
		renderIn.CoderAgentCtx = cfg.CoderAgentCtx
	}
	rendered, err := d.Render(renderIn)
	if err != nil {
		return block("install: render failed: %v\n", err)
	}
	unitPlan, err := d.Reconcile(rendered, unitDir)
	if err != nil {
		return block("install: reconcile failed: %v\n", err)
	}

	// (5) --dry-run: print the changed units and stop. Nothing is written, pulled
	// or persisted.
	if opts.DryRun {
		res.Outcome = DryRun
		if len(unitPlan.Changed) == 0 {
			say("dry-run: no changes — units already match config\n")
			return res
		}
		for _, u := range unitPlan.Changed {
			say("# %s\n%s\n", u.Name, u.Text)
		}
		say("dry-run: %d unit(s) would be written (nothing written, no model pulled, no config persisted)\n", len(unitPlan.Changed))
		return res
	}

	// (6) Ensure the weights are present BEFORE any start, on both the no-op and
	// write paths, pulling only when absent.
	if !d.ModelDownloaded(rec) {
		say("model %s not present — downloading...\n", rec.Model)
		if err := d.EnsureModel(rec); err != nil {
			return block("install: download model %s failed: %v\n", rec.Model, err)
		}
		say("model %s downloaded and verified\n", rec.Model)
	}
	if gates.Memory && !d.EmbedModelPresent(d.ModelsDir()) {
		say("embedding model %s not present — downloading...\n", NomicEmbedShard.Filename)
		if err := d.EnsureEmbedModel(d.ModelsDir()); err != nil {
			return block("install: pre-stage embedding model %s failed: %v\n", NomicEmbedShard.Filename, err)
		}
		say("embedding model %s downloaded and verified\n", NomicEmbedShard.Filename)
	}

	// (6c) Pre-stage the coding agent BEFORE persisting config and starting the
	// stack: notice, coder shard, coder weights, pinned binary, locked-down config.
	if gates.Agent {
		say("%s\n", AgentLicenseNotice)
		cat, ok := d.AgentCatalog()
		if !ok {
			return block("install: cannot load the model catalog to resolve the coder model — re-run `villa install --coding-agent` once the catalog is readable.\n")
		}
		// A SHARED-residency fit is a swap-only limitation, not a memory shortfall,
		// so it gets its own copy rather than the misdirecting "free memory" line.
		if rec.Coder.Residency == recommend.ResidencyShared {
			return block("install: the coding-agent addon currently requires a swap-residency coder fit, but this host only supports SHARED residency (the coder would ride the chat endpoint), which v1.4 does not yet serve as a dedicated agent — so the addon cannot be staged. This is a swap-only limitation, not a memory shortfall; freeing memory will not help. (The chat stack is unaffected.)\n")
		}
		sh, ok := CoderShardFor(rec, cat)
		if !ok {
			return block("install: no coder model fits the detected memory envelope, so the coding-agent addon cannot be staged — free memory or use a larger-envelope host, then re-run `villa install --coding-agent`. (The chat stack is unaffected.)\n")
		}
		if !d.CoderModelPresent(d.ModelsDir(), sh) {
			say("coder model %s not present — downloading...\n", sh.Filename)
			if err := d.EnsureCoderModel(d.ModelsDir(), sh); err != nil {
				return block("install: pre-stage coder model %s failed: %v\n", sh.Filename, err)
			}
			say("coder model %s downloaded and verified\n", sh.Filename)
		}
		binPath, err := d.InstallAgentBinary(ctx)
		if err != nil {
			return block("install: stage coding-agent binary failed: %v\n", err)
		}
		say("coding agent installed and verified at %s\n", binPath)
		if err := d.RenderCrushConfig(cfg); err != nil {
			return block("install: render coding-agent config failed: %v\n", err)
		}
		say("coding-agent config rendered (outbound tools disabled, loopback provider only)\n")
	}

	// (6z) CAPTURE before the first mutation (ADR-0003). Everything below that
	// changes the host is recorded, and every failure below routes through refuse.
	priorUnits := map[string]string{}
	for _, u := range slices.Concat(unitPlan.Changed, unitPlan.Unchanged) {
		if d.ReadUnit != nil {
			if text, ok := d.ReadUnit(unitDir, u.Name); ok {
				priorUnits[u.Name] = text
			}
		}
	}
	// The running set covers every service install may start, not only the
	// rendered plan, so a rollback never stops a service that was running before.
	priorRunning := map[string]bool{}
	if d.IsActive != nil {
		for _, svc := range []string{units.Inference, units.ChatUI, units.Qdrant, units.Embed, units.Searxng, units.Websafe, orchestrate.DashboardServiceName} {
			if state, aerr := d.IsActive(svc); aerr == nil && state == "active" {
				priorRunning[svc] = true
			}
		}
	}
	hadConfig := d.ConfigExists == nil || d.ConfigExists()
	prior := CapturePrior(cfg, hadConfig, priorUnits, priorRunning)
	var mutated Mutations

	// refuse rolls the host back to the captured state. An incomplete restore is
	// reported as such, never as a clean restoration.
	refuse := func(format string, args ...any) Result {
		warn(format, args...)
		rb := Rollback(RollbackDeps{
			StopService:  d.Stop,
			StartService: d.Start,
			WriteUnit: func(name, text string) error {
				if d.WriteUnit == nil {
					return fmt.Errorf("no unit-write seam wired")
				}
				return d.WriteUnit(unitDir, name, text)
			},
			RemoveUnit: func(name string) error {
				if d.RemoveUnit == nil {
					return fmt.Errorf("no unit-removal seam wired")
				}
				return d.RemoveUnit(unitDir, name)
			},
			SaveConfig:   d.SaveConfig,
			RemoveConfig: d.RemoveConfig,
			DaemonReload: d.DaemonReload,
		}, prior, mutated)
		warn("install: %s\n", rb.Reason())
		res.Refuse(strings.TrimSuffix(fmt.Sprintf(format, args...), "\n"), rb.Reason())
		return res
	}

	// (7) Persist config BEFORE any unit work: it is the source of truth the
	// lifecycle verbs render from. The save is the first mutation.
	if err := d.SaveConfig(cfg); err != nil {
		return block("install: persist config: %v\n", err)
	}
	mutated.RecordConfigSave()

	// (7b) Reconcile the native dashboard unit on BOTH paths, so a re-install with
	// unchanged containers still repairs it. Idempotent: a matching unit is a no-op.
	if err := reconcileDashboard(d, say, mutated.RecordStart); err != nil {
		return block("install: %v\n", err)
	}

	// (8) True no-op: reached only AFTER the weights, config and dashboard unit are
	// ensured, so the early return is safe.
	if len(unitPlan.Changed) == 0 {
		say("no changes — stack already matches config\n")
		postInstall(say, d.Endpoint(), Proof{Status: preflight.StatusPass, Detail: "unchanged"})
		res.Outcome = NoChange
		res.Finish()
		return res
	}

	// (9) Execute the sequence the core planned; the plan is the authority this
	// function is held to (AssertStartOrder below).
	seq := BuildSequence(gates, units, cfg.WebLoaderSecret == "")
	var performed []string
	start := func(svc string) error {
		if err := d.Start(svc); err != nil {
			return err
		}
		say("started %s\n", svc)
		performed = append(performed, svc)
		mutated.RecordStart(svc)
		return nil
	}

	if err := d.WriteUnits(unitPlan, unitDir); err != nil {
		return refuse("install: write units failed: %v\n", err)
	}
	for _, u := range unitPlan.Changed {
		mutated.RecordUnit(u.Name)
	}
	say("wrote %d unit(s) to %s\n", len(unitPlan.Changed), unitDir)
	if err := d.DaemonReload(); err != nil {
		return refuse("install: daemon-reload failed: %v\n", err)
	}
	if err := start(units.Inference); err != nil {
		return refuse("install: start %s failed: %v\n", units.Inference, err)
	}

	// (9a) The web-loader bearer is generated ONCE and its 0600 env file written
	// BEFORE the chat UI starts: the chat UI unit references it via EnvironmentFile
	// when web search is on. The value only ever lands in the 0600 file.
	if gates.WebSearch {
		if cfg.WebLoaderSecret == "" {
			secret, gerr := config.GenerateWebLoaderSecret()
			if gerr != nil {
				return block("install: generate web loader secret failed: %v\n", gerr)
			}
			cfg.WebLoaderSecret = secret
			if serr := d.SaveConfig(cfg); serr != nil {
				return block("install: persist web loader secret failed: %v\n", serr)
			}
		}
		envName, envText := orchestrate.RenderWebsafeSecretEnv(cfg.WebLoaderSecret)
		if werr := d.WriteWebsafeSecretEnv(envName, envText); werr != nil {
			return block("install: write websafe secret env failed: %v\n", werr)
		}
	}

	// The chat UI AFTER inference, so it comes up against a live backend.
	if err := start(units.ChatUI); err != nil {
		return refuse("install: start %s failed: %v\n", units.ChatUI, err)
	}

	// (9b) The memory stack: the vector store, then the embedder. Each start is
	// gated on its unit being in the written plan, never on the flag alone.
	if gates.Memory {
		if !UnitPresent(unitPlan, orchestrate.QdrantContainerUnitName()) ||
			!UnitPresent(unitPlan, orchestrate.EmbedContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: memory is enabled but the memory units (%s, %s) are absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\n",
				orchestrate.QdrantContainerUnitName(), orchestrate.EmbedContainerUnitName())
		}
		if err := start(units.Qdrant); err != nil {
			return refuse("install: start %s failed: %v\n", units.Qdrant, err)
		}
		if err := start(units.Embed); err != nil {
			return refuse("install: start %s failed: %v\n", units.Embed, err)
		}
	}

	// (9c) The web-search stack: the secret ONCE, then settings.yml and the 0600
	// secret env BEFORE the start so the container has both on first boot.
	if gates.WebSearch {
		if !UnitPresent(unitPlan, orchestrate.SearXNGContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: web search is enabled but the searxng unit (%s) is absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\\n",
				orchestrate.SearXNGContainerUnitName())
		}
		if cfg.SearxngSecret == "" {
			secret, gerr := config.GenerateSearxngSecret()
			if gerr != nil {
				return block("install: generate searxng secret failed: %v\n", gerr)
			}
			cfg.SearxngSecret = secret
			if serr := d.SaveConfig(cfg); serr != nil {
				return block("install: persist searxng secret failed: %v\n", serr)
			}
		}
		settingsName, settingsText, rerr := orchestrate.RenderSearxngSettings(cfg)
		if rerr != nil {
			return block("install: render searxng settings failed: %v\n", rerr)
		}
		if werr := d.WriteSearxngSettings(settingsName, settingsText); werr != nil {
			return block("install: write searxng settings failed: %v\n", werr)
		}
		envName, envText := orchestrate.RenderSearxngSecretEnv(cfg.SearxngSecret)
		if werr := d.WriteSearxngSecretEnv(envName, envText); werr != nil {
			return block("install: write searxng secret env failed: %v\n", werr)
		}
		if err := start(units.Searxng); err != nil {
			return refuse("install: start %s failed: %v\n", units.Searxng, err)
		}
		if !UnitPresent(unitPlan, orchestrate.WebsafeContainerUnitName()) {
			return refuse("install: INTERNAL ERROR: web search is enabled but the websafe unit (%s) is absent from the rendered plan — refusing to start a service systemd has never seen. This is a render/reconcile bug; please re-run `villa install`, and if it persists, file an issue.\\n",
				orchestrate.WebsafeContainerUnitName())
		}
		if err := start(units.Websafe); err != nil {
			return refuse("install: start %s failed: %v\n", units.Websafe, err)
		}
	}

	// (10) Readiness, then each opted-in subsystem's proof. A FAIL refuses, never
	// a silent skip.
	ready := d.PollReady(ctx, d.Endpoint())
	postInstall(say, d.Endpoint(), ready)

	if gates.Memory {
		warnRecallEmbeddingSkew(warn, cfg, d.ReadRecallState)
		proof := d.ProveMemory(ctx, cfg)
		if proof.Status == preflight.StatusFail {
			return refuse("install: memory stack not ready: %s\n", proof.Detail)
		}
		say("memory stack ready: %s\n", proof.Detail)
	}
	if gates.WebSearch {
		proof := d.ProveSearch(ctx)
		if proof.Status == preflight.StatusFail {
			return refuse("install: search service not ready: %s\n", proof.Detail)
		}
		say("search service ready: %s\n", proof.Detail)
	}
	if gates.Agent {
		proof := d.ProveAgent(ctx)
		if proof.Status == preflight.StatusFail {
			return refuse("install: coding agent not ready: %s\n", proof.Detail)
		}
		say("coding agent ready: %s\n", proof.Detail)
	}

	if err := AssertStartOrder(seq, performed); err != nil {
		return block("install: %v\n", err)
	}

	res.ReadinessWarn = ready.Status == preflight.StatusWarn
	res.Finish()
	return res
}

// reconcileDashboard brings up the native control-dashboard .service idempotently:
// it renders the expected unit through the PURE renderer, compares it with the
// on-disk unit, and only writes → reloads → enables → starts on a difference. An
// absent unit is the normal first-install state; any other read error is fatal.
// The binary path resolves fail-closed: never a fixed fallback path.
func reconcileDashboard(d Deps, say func(string, ...any), recordStart func(string)) error {
	udir, err := d.UserUnitDir()
	if err != nil {
		return fmt.Errorf("cannot resolve the user-unit dir for the dashboard: %w", err)
	}
	binPath, err := d.ResolveBinaryPath()
	if err != nil {
		return fmt.Errorf("cannot resolve the villa binary path for the dashboard unit: %w", err)
	}
	expected, err := orchestrate.RenderDashboardUnit(binPath)
	if err != nil {
		return fmt.Errorf("render dashboard unit failed: %w", err)
	}
	current, err := d.ReadDashboardUnit(udir)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			return fmt.Errorf("read dashboard unit failed: %w", err)
		}
		current = nil
	}
	if bytes.Equal(current, []byte(expected)) {
		say("dashboard unit already current\n")
		return nil
	}
	if err := d.WriteDashboardUnit(udir, binPath); err != nil {
		return fmt.Errorf("write dashboard unit failed: %w", err)
	}
	say("wrote %s to %s\n", orchestrate.DashboardServiceName, udir)
	if err := d.DaemonReload(); err != nil {
		return fmt.Errorf("daemon-reload (dashboard) failed: %w", err)
	}
	if err := d.Enable(orchestrate.DashboardServiceName); err != nil {
		return fmt.Errorf("enable %s failed: %w", orchestrate.DashboardServiceName, err)
	}
	if err := d.Start(orchestrate.DashboardServiceName); err != nil {
		return fmt.Errorf("start %s failed: %w", orchestrate.DashboardServiceName, err)
	}
	say("started %s (boot-survival enabled)\n", orchestrate.DashboardServiceName)
	recordStart(orchestrate.DashboardServiceName)
	return nil
}

// postInstall narrates the loopback endpoint, the readiness verdict and the two
// UI URLs. The endpoint comes from the backend seam, never retyped.
func postInstall(say func(string, ...any), endpoint string, ready Proof) {
	say("\ninference endpoint: %s\n", endpoint)
	switch ready.Status {
	case preflight.StatusPass:
		say("health: PASS — %s\n", ready.Detail)
	case preflight.StatusWarn:
		say("health: WARN — %s\n", ready.Detail)
	default:
		say("health: %s\n", ready.Detail)
	}
	say("chat (Open WebUI): %s\n", ChatURL)
	say("dashboard: %s\n", DashboardURL)
}

// warnRecallEmbeddingSkew is the read-only skew WARN: when the recall-state stamp
// records an embedding identity that CONFIDENTLY diverges from the configured one,
// warn with the remediation and do nothing else. A nil seam, an unreadable state or
// an empty stamp are all silent: no recorded truth means no alarm.
func warnRecallEmbeddingSkew(warn func(string, ...any), cfg config.VillaConfig, read func() (recall.State, error)) {
	if read == nil {
		return
	}
	st, err := read()
	if err != nil {
		return
	}
	if recall.EmbeddingSkew(st, cfg.EmbeddingModel, cfg.EmbeddingDim) != recall.SkewMismatch {
		return
	}
	warn("install: WARN: the recall index was built with %s (dim %d) but config now says %s (dim %d) — retrieval from the existing collection is corrupt until re-index; run `villa recall index --rebuild` to re-index, or revert embedding_model/embedding_dim in config.toml.\n",
		st.EmbeddingModel, st.EmbeddingDim, cfg.EmbeddingModel, cfg.EmbeddingDim)
}

// PersistedBackendChosen reports whether the persisted config carries a
// DELIBERATELY-chosen backend a re-install must preserve: any ROCm-family name or
// the explicit "vulkan" opt-out. Empty and unknown values fall through to the
// recommendation. Names only, never an image literal.
func PersistedBackendChosen(name string) bool {
	if name == "" {
		return false
	}
	return inference.IsROCmFamily(name) || name == "vulkan"
}

// CoderShardFor resolves the GGUF shard to pre-stage for the picked coder: the
// catalog entry whose ID is rec.Coder.Model, Shards[0]. It is resolved from the
// picked entry, never a literal, so the staged filename is single-source with the
// served -m path. False when no coder fits, the id is absent or it has no shards.
func CoderShardFor(rec recommend.Recommendation, cat catalog.Catalog) (catalog.Shard, bool) {
	if rec.Coder.Model == "" {
		return catalog.Shard{}, false
	}
	for _, m := range cat.Models {
		if m.ID == rec.Coder.Model && len(m.Shards) > 0 {
			return m.Shards[0], true
		}
	}
	return catalog.Shard{}, false
}

// NomicEmbedShard is the pinned nomic-embed-text-v1.5 Q8_0 GGUF pre-staged into the
// models dir when memory is on. Its Filename MUST equal
// orchestrate.EmbedGGUFFilename(), the name the embed unit serves, so the staged
// file and the served path can never drift.
var NomicEmbedShard = catalog.Shard{
	URL:       "https://huggingface.co/nomic-ai/nomic-embed-text-v1.5-GGUF/resolve/main/nomic-embed-text-v1.5.Q8_0.gguf",
	Filename:  "nomic-embed-text-v1.5.Q8_0.gguf",
	SHA256:    "3e24342164b3d94991ba9692fdc0dd08e3fd7362e0aacc396a9a5c54a544c3b7",
	SizeBytes: 146146432,
}

// AgentLicenseNotice is the FSL-1.1-MIT notice surfaced before the coding-agent
// binary is staged. Informational, never a click-through: the user already opted in.
const AgentLicenseNotice = "The coding agent (Crush, charmbracelet) is distributed under the Functional Source " +
	"License v1.1 (FSL-1.1-MIT): you may use, modify, and redistribute it for any purpose " +
	"except offering a competing commercial service; each version becomes MIT-licensed two " +
	"years after release. villa installs a pinned, checksum-verified release and renders its " +
	"config locally."

// GiB renders bytes as a GiB string with raw bytes, the recommend table's format.
func GiB(b uint64) string {
	return fmt.Sprintf("%.3f GiB (%d bytes)", float64(b)/(1<<30), b)
}

// GiBUsableEnvelope renders a typed-Unknown envelope for the empty-state copy:
// "unknown GiB" when unknown (never a fabricated 0), else the GiB figure with up
// to two decimals.
func GiBUsableEnvelope(b detect.Bytes) string {
	if !b.Known {
		return "unknown GiB"
	}
	g := float64(b.Value) / (1 << 30)
	return strconv.FormatFloat(g, 'g', -1, 64) + " GiB"
}
