package main

// doctor.go is the thin cobra caller for the read-only `villa doctor` health-diagnosis
// verb (DOCTOR-01/02/03): the running-install twin of `villa preflight`. The worst-wins
// decision logic — composing the preflight host-prep gate, the status read-model + its
// per-service offload Verdict, and an orchestrate.Reconcile config-vs-disk drift Plan
// lives in the pure internal/doctor core (Plan 01). This file keeps ONLY: the cobra
// wiring + exit-code mapping (reusing the AUTHORITATIVE preflight constants), the human
// table renderer, and the live host wiring (liveDoctorDeps) that constructs doctor.Deps.
//
// doctor is strictly READ-ONLY: it mutates nothing. Note unitDirReadOnly — the
// quadletUnitDir twin that drops the directory-creation step — so a diagnosis never
// creates the Quadlet dir (Pitfall 2). There is no --force and no generation probe. No backend marker
// literal appears here (TestSeamGrepGate walks cmd/villa); ROCm is routed only via the
// core's inference.IsROCmFamily and resolved via inference.BackendFor.

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
)

// newDoctor builds `villa doctor`: a read-only, one-shot health diagnosis of the RUNNING
// install. It composes the pure doctor core over live host seams and maps the worst-wins
// Report to an exit code mirroring `villa preflight`: 0 (healthy), 2 (warnings/drift), or
// 1 (a blocking fault — e.g. a confident CPU fallback). It mutates nothing: no
// --force, no unit-dir creation, no generation probe. The exit-code mapping lives ENTIRELY
// here (return-not-Exit verb body; cobra RunE calls os.Exit).
func newDoctor() *cobra.Command {
	return &cobra.Command{
		Use:   "doctor",
		Short: "Diagnose the health of the running install: host conditions + service health + GPU-offload proof + config-vs-disk drift",
		Long: "Run a read-only, one-shot health diagnosis of the RUNNING stack: re-check the host-prep " +
			"conditions, fold each service's /health and running GPU-offload Verdict (residency proven, " +
			"never a false-green over a health-200), and detect config-vs-disk Quadlet drift. Every " +
			"non-healthy finding carries an actionable remediation. Exits 0 (healthy), 2 (warnings or " +
			"drift), or 1 (a blocking fault such as a confident CPU fallback). Mutates nothing — no " +
			"unit files are written or created.",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveDoctorDeps(cmdContext(cmd))
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "doctor: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runDoctor(cmd, args, deps))
			return nil
		},
	}
}

// runDoctor builds the Report from the injected core and renders it. It RETURNS the exit
// code (no os.Exit) so doctor_test.go drives it deterministically. All printing + exit
// mapping lives here; the worst-wins fold is doctor.Aggregate.
func runDoctor(cmd *cobra.Command, _ []string, deps doctor.Deps) int {
	report := doctor.Aggregate(deps)
	return renderDoctor(cmd.OutOrStdout(), report, jsonOut, verbose)
}

// renderDoctor writes the report and RETURNS the exit code (it does not call os.Exit) so
// tests can assert both the rendered output and the mapped code without spawning a
// subprocess. It mirrors renderPreflight EXACTLY and is the single place that interprets
// the doctor findings as exit codes.
//
// CRITICAL (Pitfall 1 — the shipped preflight constants are AUTHORITATIVE, NOT the
// inverted ROADMAP prose): a confident BLOCK-class FAIL → exitBlocked (=1); any WARN /
// drift / typed-Unknown → exitWarn (=2); all healthy → exitPass (=0). Do NOT invert.
func renderDoctor(w io.Writer, r doctor.Report, asJSON, withProvenance bool) int {
	if asJSON {
		enc := json.NewEncoder(w)
		enc.SetIndent("", "  ")
		_ = enc.Encode(r)
	} else {
		renderDoctorTable(w, r, withProvenance)
	}

	// The core's worst-wins fold (doctor.Aggregate) is the SINGLE source of truth for the
	// verdict: r.Overall is mapped here to the AUTHORITATIVE preflight exit constants so the
	// exit code can never diverge from the JSON `overall` field. By the core's FAIL ⟺
	// BLOCK-class invariant, an Overall of FAIL means at least one blocking-tier FAIL is
	// present (a confident offload FAIL, a preflight BLOCK, or a loopback breach); a down/
	// stopped stack folds to WARN, never FAIL.
	switch r.Overall {
	case "FAIL":
		var blockFails int
		for _, f := range r.Findings {
			if f.Status == "FAIL" {
				blockFails++
			}
		}
		fmt.Fprintf(w, "\nFAULT: %d blocking finding(s) — the running install is not healthy. See the remediation(s) above.\n", blockFails)
		return exitBlocked
	case "WARN":
		return exitWarn
	case "PASS":
		return exitPass
	default:
		// FAIL CLOSED (phase-22, mirroring renderInference): an unrecognized
		// Overall (a future Aggregate bug, a hand-built Report, a JSON-roundtripped
		// fixture) can NEVER map to "healthy" — for a health verdict the only safe
		// default is the blocking tier.
		fmt.Fprintf(w, "\nFAULT: unrecognized overall verdict %q — treating the install as not healthy.\n", r.Overall)
		return exitBlocked
	}
}

// renderDoctorTable writes the findings as an aligned human table (mirroring
// renderPreflightTable): the overall verdict, then one row per finding
// (ID/Tier/Status/Detail), appending " — Remediation" to the detail cell on any non-PASS
// finding. With provenance, a trailing column shows which composed core produced it.
func renderDoctorTable(w io.Writer, r doctor.Report, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "overall\t%s\n\n", r.Overall)
	for _, f := range r.Findings {
		detail := f.Detail
		if f.Status != "PASS" && f.Remediation != "" {
			detail = detail + " — " + f.Remediation
		}
		if withProvenance {
			prov := f.Provenance
			if f.Raw != "" {
				prov = prov + " | raw: " + f.Raw
			}
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\t(%s)\n", f.ID, f.Tier, f.Status, detail, prov)
		} else {
			fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", f.ID, f.Tier, f.Status, detail)
		}
	}
	_ = tw.Flush()
}

// unitDirReadOnly is the READ-ONLY twin of quadletUnitDir: the same fixed rootless
// Quadlet generator directory (~/.config/containers/systemd) but without the
// directory-creation step — doctor never creates it (Pitfall 2). If the dir is absent, the drift read
// fails and the core degrades it to a typed-Unknown WARN, so resolving the path is
// all this needs to do.
func unitDirReadOnly() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "containers", "systemd"), nil
}

// liveDoctorDeps wires doctor.Deps to the real host. It REUSES liveStatusDeps wholesale
// for the running-stack read-model (no re-wired HTTP/journald/GTT probes — RESEARCH A1)
// and constructs a DriftPlan closure that renders units from config and Reconciles them
// against the on-disk unit dir, returning the Plan WITHOUT ever writing (no WriteUnits).
// It is replaced wholesale by stubbed doctor.Report fixtures in doctor_test.go.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the proof
// seams below. Without it `villa doctor` could not be interrupted: the three
// residency proofs drive a live stack for up to residencyProofBudget /
// agentProofBudget (60-90s) each, and the agent tool-call probe adds another 90s,
// so a Ctrl-C landed on a command that kept running for minutes. Cancelling is
// safe by construction — doctor is read-only and mutates nothing, so an aborted
// run leaves no half-applied state, and the podman probe containers are
// exec.CommandContext children that die with the context rather than outliving it.
func liveDoctorDeps(ctx context.Context) (doctor.Deps, error) {
	sd, err := liveStatusDeps()
	if err != nil {
		return doctor.Deps{}, err
	}
	cfg, err := config.LoadVilla()
	if err != nil {
		return doctor.Deps{}, fmt.Errorf("load config: %w", err)
	}
	// Option B (image thread-through): on a ROCm-family backend, resolve the RUNNING
	// ROCm image via the inference seam and bind the image-aware host-prep gate so a
	// denied running image is a confident FAIL (refuse-with-remediation) rather than the
	// un-evaluated "no image requested" WARN. The image is obtained ONLY through
	// inference.BackendFor(...).Image() — no image literal appears in cmd/villa, so the
	// cmd-tier TestSeamGrepGate walk stays green. For non-ROCm backends rocmImageGate
	// stays nil and Aggregate uses preflight.Run/RunROCm exactly as before.
	var rocmImageGate func(detect.HostProfile) []preflight.CheckResult
	if inference.IsROCmFamily(cfg.Backend) {
		// Surface a BackendFor error rather than swallowing it: if a future
		// ROCm digest is added to IsROCmFamily but missed in BackendFor, a silent nil
		// gate would downgrade the image-aware denied-image FAIL to the un-evaluated
		// "no image requested" WARN — a false-green the residency-supersession could
		// then swallow. Fail closed instead, mirroring the DriftPlan BackendFor path.
		b, berr := inference.BackendFor(cfg.Backend)
		if berr != nil {
			return doctor.Deps{}, fmt.Errorf("resolve ROCm backend image: %w", berr)
		}
		image := b.Image()
		rocmImageGate = func(p detect.HostProfile) []preflight.CheckResult {
			return preflight.RunROCmForImage(p, image)
		}
	}
	// Memory seams (mirroring the rocmImageGate conditional shape):
	// bound ONLY when the persisted memory stack is opted in; both stay nil when
	// off so the memory-off doctor output is byte-identical (mirror). The
	// embed service name comes from the orchestrate accessor converted via the
	// same .container → .service derivation the status fold uses — never a typed
	// service-name literal here. (The old MemoryEnabled/MemoryServices Deps
	// wiring was removed with the doctor offload down-rank — Plan 23-01: memory
	// rows are OffloadApplies=false at the status source, so no offload finding
	// exists to down-rank.)
	var (
		memChecks func(detect.HostProfile) []preflight.CheckResult
		memProof  func() inference.Verdict
	)
	if subsystem.MemoryOn(cfg) {
		embeddingModel := cfg.EmbeddingModel
		embedService := unitServiceName(orchestrate.EmbedContainerUnitName())
		// composition over re-implementation: the memory host gate IS
		// preflight.RunMemory — doctor never re-rolls the disk/headroom logic.
		// EmbedderActive (phase-22): doctor runs MEM-PRE-headroom against a
		// possibly-RUNNING stack, where the embedder's own consumption is already
		// subtracted from MemAvailable — without this flag the check would demand
		// a SECOND reservation on top of the resident one and fabricate a blocking
		// fault on a healthy memory-tight host. The active-state read is the same
		// read-only IsActive seam the status fold uses; any error or non-active
		// state keeps the strict pre-install semantics (false).
		memChecks = func(p detect.HostProfile) []preflight.CheckResult {
			embedActive := false
			if state, aerr := sd.IsActive(embedService); aerr == nil && state == "active" {
				embedActive = true
			}
			return preflight.RunMemory(p, preflight.MemoryGateInput{
				EmbeddingModel: embeddingModel,
				EmbedderActive: embedActive,
			})
		}
		memProof = liveResidencyUnderLoad(ctx, cfg, sd)
	}
	// Coding-agent seams (mirroring the cfg.MemoryEnabled
	// conditional above): bound ONLY when the persisted agent_enabled is true; all
	// three stay nil when the agent is off so the agent-off doctor output is
	// byte-identical (mirror). Each seam REUSES a Phase-27 probe — never a
	// re-rolled crush-run round-trip or residency scrape — and consumes the resulting
	// inference.Verdict opaquely (no backend marker literal in cmd/villa;
	// TestSeamGrepGate walks this tree).
	var (
		agentToolCall  func() inference.Verdict
		agentResidency func() inference.Verdict
		agentDrift     func() agent.DriftReport
	)
	if subsystem.AgentOn(cfg) {
		agentToolCall = liveAgentToolCallVerdict(ctx, cfg)
		agentResidency = liveAgentResidencyUnderLoad(ctx, cfg, sd)
		agentDrift = liveAgentDrift(cfg)
	}
	// Web-search seams (mirroring the cfg.AgentEnabled conditional
	// above): bound ONLY when the persisted web_search_enabled is true; both stay nil when
	// web search is off so the web-off doctor output is byte-identical (except the schema
	// bump). SearchEgressProof reads the CACHED `villa verify search` result (never a config
	// bool); SearchResidencyUnderLoad samples residency under a bounded search-augmented chat
	// drive. Both consume/produce inference.Verdict opaquely (no backend marker literal in
	// cmd/villa; TestSeamGrepGate walks this tree). Guard health is a documented omission.
	var (
		searchEgress    func() inference.Verdict
		searchResidency func() inference.Verdict
	)
	if subsystem.WebSearchOn(cfg) {
		searchEgress = liveSearchEgressProof()
		searchResidency = liveSearchResidencyUnderLoad(ctx, cfg, sd)
	}
	return doctor.Deps{
		Probe:                    detect.Probe,
		LoadConfig:               config.LoadVilla,
		StatusReport:             func() status.Report { return status.Run(*sd) },
		Backend:                  cfg.Backend,
		RunROCmImage:             rocmImageGate,
		RunMemoryChecks:          memChecks,
		ResidencyUnderLoad:       memProof,
		AgentToolCall:            agentToolCall,
		AgentResidencyUnderLoad:  agentResidency,
		AgentDrift:               agentDrift,
		SearchEgressProof:        searchEgress,
		SearchResidencyUnderLoad: searchResidency,
		// DriftPlan: render units from the persisted config, resolve the backend
		// fail-closed, and Reconcile against the READ-ONLY unit dir. It NEVER
		// writes. A read error (absent/unreadable unit dir) is returned verbatim so the
		// core degrades it to a typed-Unknown WARN rather than swallowing it.
		DriftPlan: func() (orchestrate.Plan, error) {
			c, err := config.LoadVilla()
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("load config: %w", err)
			}
			backend, err := inference.BackendFor(c.Backend)
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve backend: %w", err)
			}
			modelFile, err := liveModelFile(c)
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve model file: %w", err)
			}
			resident, err := liveResidentUnits(c)
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve resident set: %w", err)
			}
			units, err := livePinnedRender(orchestrate.RenderInput{
				Backend:       backend,
				Cfg:           c,
				ModelFile:     modelFile,
				ModelsDir:     modelsDir(),
				HostVillaPath: hostVillaPath(),
				Resident:      resident,
			})
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("render units: %w", err)
			}
			dir, err := unitDirReadOnly()
			if err != nil {
				return orchestrate.Plan{}, fmt.Errorf("resolve unit dir: %w", err)
			}
			// An absent unit dir means the stack was never installed — NOT drift.
			// Reconcile would otherwise treat every rendered unit as Changed (absent
			// file ⇒ Changed) and the core would misreport "units no longer match".
			// Return a read error so the core degrades it to the honest typed-Unknown
			// WARN ("units not yet written") instead. This stat is the
			// only filesystem touch and is strictly read-only.
			if _, statErr := os.Stat(dir); statErr != nil {
				return orchestrate.Plan{}, fmt.Errorf("read unit dir %q: %w", dir, statErr)
			}
			return orchestrate.Reconcile(units, dir)
		},
	}, nil
}

// unitServiceName converts a Quadlet .container unit filename to its generated systemd
// .service name (villa-qdrant.container → villa-qdrant.service) — the same derivation
// the status fold (status.serviceUnits) and the lifecycle verbs use, so the doctor
// down-rank predicate keys on exactly the names the status rows carry.
func unitServiceName(containerUnit string) string {
	return strings.TrimSuffix(containerUnit, ".container") + ".service"
}

// Residency-under-embedding-load proof tuning (DoS bounds): a REAL but
// strictly bounded /v1/embeddings workload — N sequential multi-KiB requests, each with
// its own timeout, the whole proof under one parent budget, all probe containers
// transient (--rm, Pitfall 7).
const (
	// residencyDriveRequests is the bounded request count of the embed-load drive.
	residencyDriveRequests = 12
	// residencySampleAfter is how many drive requests must have completed before the
	// mid-drive residency sample fires (Pitfall 6: the sample must be DURING load,
	// not before the embedder has actually started working). The sample itself is
	// then taken while the NEXT request is verifiably IN FLIGHT (launched async and
	// joined after sampling) — never in the idle gap between two sequential
	// requests (phase-22).
	residencySampleAfter = 2
	// residencyRequestTimeout bounds each individual embed-drive request.
	residencyRequestTimeout = 10 * time.Second
	// residencyProofBudget bounds the WHOLE proof (drive + sample + join).
	residencyProofBudget = 60 * time.Second
)

// residencyDriveText is the ~2 KiB embedding input each drive request carries — large
// enough that the embedder does real per-request work (a one-word probe would finish
// before the residency sample could observe load), small enough to fit the embed
// server's PHYSICAL batch (llama-server default -ub 512 tokens): a pooled embedding
// input must fit in ONE ubatch, so anything above 512 tokens is a hard HTTP 500
// ("input is too large to process"), not a context-window question (the 8192 ctx is
// NOT the binding limit — measured on the live gfx1151 box, 22-04). Repeats a fixed
// 45-byte phrase 44 times (~2.0 KiB ≈ 442 tokens, ~14% margin under the 512 floor).
func residencyDriveText() string {
	return strings.Repeat("villa residency-under-load drive probe text; ", 44)
}

// residencyDepsFrom binds the residency drive protocol's seams to the SAME status
// seams the status fold reads, so no doctor proof can drift onto a different reader.
// The under-load proofs supply their own workload; PollHealth/Generate/GPUBusy are
// unused by that path and stay nil.
func residencyDepsFrom(sd *status.Deps) residency.Deps {
	return residency.Deps{
		Journal: sd.JournalText,
		GTTUsed: sd.GTTUsed,
		Props:   func() *inference.PropsInfo { return sd.Props(sd.Endpoint()) },
		Fold:    inference.RunningOffloadVerdict,
	}
}

// residencyTargetFor resolves WHAT a doctor proof is proving: the served model file,
// the served ctx, the weight footprint and the backend's markers. Every resolution
// failure is a typed-Unknown WARN carrying the caller's wording, never a FAIL
// fabricated from a signal that could not be evaluated.
//
// The model FILE, not the catalog id, is what the /props and journal identity checks
// compare against; passing the id would make the drift overlay misfire the moment it
// evaluates.
func residencyTargetFor(cfg config.VillaConfig, sd *status.Deps, subject string) (residency.Target, *inference.Verdict) {
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		v := residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the configured backend could not be resolved (%v)", subject, err),
			"fix the backend field in config.toml (`villa backend set`), then re-run `villa doctor`")
		return residency.Target{}, &v
	}
	modelFile, err := sd.ModelFile(cfg)
	if err != nil {
		v := residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the served model could not be resolved (%v)", subject, err),
			"fix the model field in config.toml (`villa model swap`), then re-run `villa doctor`")
		return residency.Target{}, &v
	}
	return residency.Target{
		Service:     installServiceName,
		ModelFile:   modelFile,
		ContextLen:  cfg.Ctx,
		WeightBytes: sd.WeightBytes(cfg),
		Markers:     backend.ResidencyProof(),
	}, nil
}

// requireActive is doctor's read-only precondition gate: doctor NEVER starts a
// service, so an inactive unit degrades to a typed-Unknown WARN naming it, never a
// FAIL fabricated from a stack that simply is not running.
func requireActive(sd *status.Deps, subject string, services ...string) *inference.Verdict {
	for _, svc := range services {
		if state, err := sd.IsActive(svc); err != nil || state != "active" {
			v := residency.Unevaluable(
				fmt.Sprintf("could not evaluate %s — %s is not active", subject, svc),
				fmt.Sprintf("check `systemctl --user status %s`; run `villa up` if the stack is stopped, then re-run `villa doctor`", svc))
			return &v
		}
	}
	return nil
}

// liveResidencyUnderLoad builds the live proof seam liveDoctorDeps binds when
// memory is enabled: a closure returning the chat-model residency Verdict sampled
// DURING a real embed-load drive. It is constructed (not run) at wiring time; the
// drive/sample only fire when doctor.Aggregate invokes the seam.
func liveResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) func() inference.Verdict {
	return func() inference.Verdict { return runResidencyUnderLoad(ctx, cfg, sd) }
}

// runResidencyUnderLoad is the live under-load residency proof (the live half of
// MEM-DOC-residency; composed per 22-PATTERNS from liveMemoryProof's drive + the
// liveStatusDeps residency inputs — no analog exists for the interleaving):
//
//  1. PRECONDITION GATE (read-only — doctor NEVER starts a service): memory must
//     decide enabled+valid and villa-llama, villa-qdrant and villa-embed must all be
//     active. Any unmet precondition degrades to a typed-Unknown WARN naming the
//     precondition (never a FAIL fabricated from a stack that simply is not running).
//  2. DRIVE: residencyDriveRequests sequential POSTs to the
//     config-resolved villa-embed /v1/embeddings over villa.network via runProbeCurl
//     (fixed-arg podman run --rm, helper image via orchestrate.EmbedImage(), model id
//     JSON-marshaled — never interpolated into a command string). Each request is
//     bounded by residencyRequestTimeout, the whole proof by residencyProofBudget.
//  3. SAMPLE MID-DRIVE (Pitfall 6, phase-22): after residencySampleAfter
//     completions (the embedder has demonstrably done real work), the NEXT request is
//     launched asynchronously and the sample is taken while that request is verifiably
//     IN FLIGHT — never gated on a completion count alone, which could fire in the
//     idle gap between two sequential requests. The sample evaluates
//     inference.RunningOffloadVerdict over the EXACT liveStatusDeps input set
//
// (phase-22) — every signal through the same sd seams the status fold
//
//	   uses (JournalText, Props, GTTUsed, WeightBytes), keyed on the
//	   catalog-resolved GGUF filename (sd.ModelFile, mirroring liveProve), with
//	   markers from BackendFor(cfg.Backend).ResidencyProof().
//	4. JOIN + HONESTY: the sampled in-flight request is always awaited before the loop
//	   continues (no probe container outlives the call). Drive errors alone degrade a
//	   PASS to WARN ("embed drive could not complete") — the FAIL signal is the CHAT
//	   model's residency, not the drive's success; a confident residency FAIL always
//	   stands.
func runResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) inference.Verdict {
	const subject = "residency under embedding load"

	// (1) Precondition gate — strictly read-only; doctor never starts a service.
	if dec := memory.Decide(cfg); !dec.Enabled || !dec.Valid {
		return residency.Unevaluable(
			"could not evaluate "+subject+" — the memory stack is not enabled/valid in config",
			"fix the memory_* fields in config.toml (see `villa preflight`), then re-run `villa doctor`")
	}
	embedService := unitServiceName(orchestrate.EmbedContainerUnitName())
	if v := requireActive(sd, subject,
		installServiceName,
		unitServiceName(orchestrate.QdrantContainerUnitName()),
		embedService,
	); v != nil {
		return *v
	}
	target, warn := residencyTargetFor(cfg, sd, subject)
	if warn != nil {
		return *warn
	}

	// (2) The bounded embed-load drive. The body is JSON-marshaled (the model id is
	// never interpolated into a command string) and reused verbatim for every request.
	body, err := json.Marshal(map[string]any{
		"input":           residencyDriveText(),
		"model":           cfg.EmbeddingModel,
		"encoding_format": "float",
	})
	if err != nil {
		return residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the embed drive body could not be built (%v)", subject, err),
			"re-run `villa doctor`")
	}
	url := fmt.Sprintf("http://%s:%d/v1/embeddings", config.EmbedAddr, config.EmbedPort)
	helperImage := orchestrate.EmbedImage()

	// (3) Drive and sample. Embed requests are cheap and uniform, so the load evidence
	// is the WARMUP — the embedder has demonstrably completed real requests — rather
	// than a settle deadline no request that short would survive.
	res := residency.UnderLoad(ctx, residencyDepsFrom(sd), target, residency.Load{
		Drive: func(ctx context.Context) error {
			_, derr := runProbeCurl(ctx, helperImage,
				"-sf", "-X", "POST", url,
				"-H", "Content-Type: application/json",
				"-d", string(body),
			)
			return derr
		},
		Rounds:         residencyDriveRequests,
		Warmup:         residencySampleAfter,
		RoundTimeout:   residencyRequestTimeout,
		Budget:         residencyProofBudget,
		DriveAllRounds: true,
	})

	// (4) Map the outcome honestly. A drive that never reached the sample, and a PASS
	// sampled under a faltering drive, are both unevaluable: the embedder was not
	// exercised, so the PASS was not proven. A confident FAIL stands on its own.
	if !res.Sampled {
		return residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the embed drive could not complete (%d of %d requests finished before the budget)", subject, res.Completed, res.Rounds),
			fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", embedService))
	}
	if res.DriveFaltered() {
		return residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the embed drive could not complete (%d of %d requests failed)", subject, res.DriveErrs, res.Rounds),
			fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", embedService))
	}
	return res.Verdict
}

// agentProofBudget bounds the WHOLE coding-agent tool-call round-trip (read→edit `crush
// run`) the doctor tool-call + residency seams drive. It mirrors residencyProofBudget: a
// timeout → err → a typed-Unknown WARN, never a hang masquerading as a PASS.
const agentProofBudget = 90 * time.Second

const (
	// agentResidencyDriveRounds bounds how many sequential tool-call round-trips the
	// residency-under-load proof will drive while trying to catch one verifiably IN
	// FLIGHT. The memory analog drives cheap embed requests; an agent
	// round-trip is heavyweight, so a small bound under agentProofBudget suffices.
	agentResidencyDriveRounds = 3
	// agentResidencySettle is how long the proof waits after launching a tool-call
	// round before checking it is still in flight, then sampling. Long enough
	// that a real coder round-trip has demonstrably started loading the model, short
	// enough to stay well inside agentProofBudget. A round that has already COMPLETED
	// by this point was too fast to have been sampled under load — that round is
	// skipped and the next one is driven (or the proof degrades to a typed-Unknown
	// WARN), never sampled idle (which could mask a CPU-fallback-under-load false-green).
	agentResidencySettle = 750 * time.Millisecond
)

// --- Phase 34-04: live search-residency proof + egress-proof seam ---

// searchResidencyDriveRounds / searchResidencySettle clone the agent-residency in-flight
// discipline for the search-load drive: drive bounded sequential search-augmented
// chat rounds and sample the served model's residency ONLY while a round is verifiably IN
// FLIGHT — never idle (which could mask a CPU-fallback-under-search-load false-green,
// The drive is the cheapest honest one that keeps villa-llama decoding under load
// (a bounded chat completion) while villa-searxng/villa-websafe are up (Open Q2 resolution).
const (
	searchResidencyDriveRounds = 3
	searchResidencySettle      = 750 * time.Millisecond
)

// searchVerifyFreshnessWindow is the SINGLE freshness gate a cached `villa verify search`
// PASS must satisfy to read as a CURRENT outbound-bounded proof — sourced from the exported
// status.VerifyFreshnessWindow (not a forked literal) so the doctor egress finding and the
// status `outbound_bounded` indicator can never drift apart; a security property is NEVER
// trusted indefinitely from a stale cache.
const searchVerifyFreshnessWindow = status.VerifyFreshnessWindow

// searchResidencyDriveBody is the bounded chat-completion drive payload: a small, fixed
// max_tokens completion that keeps villa-llama DECODING (so the residency sample observes
// the served model under real load) without an unbounded generation. The model id is
// JSON-marshaled, never interpolated into a command string (the runResidencyUnderLoad
// precedent). stream=false keeps the round a single bounded request.
func searchResidencyDriveBody(model string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "villa search-residency drive probe: reply with a single short sentence."},
		},
		"max_tokens": 16,
		"stream":     false,
	})
}

// liveSearchEgressProof builds the egress-proof seam liveDoctorDeps binds
// when web search is enabled: a closure that reads the CACHED `villa verify search` result
// (verifystate.Load, fail-closed) and maps it to a tri-state inference.Verdict consumed
// opaquely by the doctor core. The mapping mirrors the status core's webSearchInfo exactly
// (the SAME 24h freshness gate, NEVER cfg.WebSearchEnabled): a fresh cached PASS → StatusPass
// (ready); a real recent non-PASS (FAIL/REJECT within the window) → StatusFail
// (degraded-with-reason); a nil/absent/corrupt store, an unparseable timestamp, or a stale
// result (>24h) → StatusWarn (typed-Unknown — re-prove, never trust a stale cache). It is
// constructed (not run) at wiring time; the read only fires when doctor.Aggregate invokes it.
func liveSearchEgressProof() func() inference.Verdict {
	return func() inference.Verdict {
		st := liveReadVerifyState() // fail-closed: absent → zero State, unreadable → nil
		if st == nil {
			return inference.Verdict{
				Status:      inference.StatusWarn,
				Detail:      "no fresh verified outbound-bounded result (the cached verify-search store is unavailable)",
				Remediation: "run `villa verify search` to prove outbound is bounded, then re-run `villa doctor`",
			}
		}
		checked, perr := time.Parse(time.RFC3339, st.CheckedAt)
		if perr != nil || time.Since(checked) < 0 || time.Since(checked) > searchVerifyFreshnessWindow {
			// Unparseable, future-dated, or stale → the property must be re-proven, NEVER read
			// as bounded and NEVER inferred from cfg.WebSearchEnabled. A future
			// CheckedAt yields a negative age (never > window), so the lower-bound clamp is
			// required to keep the no-false-green invariant.
			return inference.Verdict{
				Status:      inference.StatusWarn,
				Detail:      "no fresh verified outbound-bounded result (the last `villa verify search` is stale or absent)",
				Remediation: "run `villa verify search` to re-prove outbound is bounded, then re-run `villa doctor`",
			}
		}
		if st.Verdict == "PASS" {
			return inference.Verdict{
				Status: inference.StatusPass,
				Detail: fmt.Sprintf("outbound bounded: a recent `villa verify search` PASS (checked %s)", st.CheckedAt),
			}
		}
		// A real RECENT non-PASS verdict (FAIL/REJECT) — confidently NOT bounded.
		return inference.Verdict{
			Status:      inference.StatusFail,
			Detail:      fmt.Sprintf("the last `villa verify search` did not pass (verdict %q, checked %s)", st.Verdict, st.CheckedAt),
			Remediation: "re-run `villa verify search` and check `villa logs` — outbound is not proven bounded",
		}
	}
}

// liveSearchResidencyUnderLoad builds the chat-model-residency-under-
// SEARCH-load seam: a closure returning the served model's residency Verdict sampled DURING
// a bounded search-augmented chat drive (with villa-searxng/villa-websafe up). It mirrors
// liveAgentResidencyUnderLoad's drive→settle→sample-if-in-flight→join shape but drives a
// bounded chat completion (the cheapest honest drive that keeps villa-llama decoding under
// search load) instead of the crush-run tool-call probe. It is constructed (not run) at
// wiring time; the drive/sample only fire when doctor.Aggregate invokes the seam.
func liveSearchResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) func() inference.Verdict {
	return func() inference.Verdict { return runSearchResidencyUnderLoad(ctx, cfg, sd) }
}

// runSearchResidencyUnderLoad is the live under-SEARCH-load residency proof for the served
// model, a verbatim-in-shape clone of runAgentResidencyUnderLoad with ONLY
// the drive swapped and the precondition gate extended. Strictly READ-ONLY (doctor never
// starts a service):
//
//  1. PRECONDITION GATE: the served inference unit (villa-llama) AND villa-searxng AND
//     villa-websafe must all be active; the backend + served model must resolve. Each unmet
//     precondition → agentUnevaluable typed-Unknown WARN (NOT a FAIL fabricated from a stack
//     that is not running). Unit names come from orchestrate.*ContainerUnitName() via
//     unitServiceName — never a typed service-name literal (TestSeamGrepGate).
//  2. DRIVE + IN-FLIGHT SAMPLE: drive sequential bounded search-augmented chat rounds;
//     for each, launch async, wait searchResidencySettle, then sample ONLY IF the round is
//     still in flight (a round that finished too fast to load the model under observation is
//     joined and the next driven). Sample inference.RunningOffloadVerdict over the EXACT
//     liveStatusDeps input set (JournalText/Props/GTTUsed/WeightBytes/ConfigModel/
//     ConfigContext/Markers: backend.ResidencyProof()), keyed on the served GGUF filename.
//  3. JOIN + HONESTY: every sampled round is JOINED so no probe outlives the call; if no round
//     can be caught in flight within the bounded rounds / budget, degrade to a typed-Unknown
//     WARN (never an idle-sampled verdict). A confident CPU fallback under search load → FAIL.
func runSearchResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) inference.Verdict {
	const subject = "residency under search load"

	// The served unit AND the web-search services must all be active (read-only gate).
	if v := requireActive(sd, subject,
		installServiceName,
		unitServiceName(orchestrate.SearXNGContainerUnitName()),
		unitServiceName(orchestrate.WebsafeContainerUnitName()),
	); v != nil {
		return *v
	}
	target, warn := residencyTargetFor(cfg, sd, subject)
	if warn != nil {
		return *warn
	}

	// The bounded chat-completion drive payload (model id JSON-marshaled, never
	// interpolated). It is the cheapest honest drive that keeps villa-llama decoding
	// under search load.
	body, err := searchResidencyDriveBody(cfg.Model)
	if err != nil {
		return residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the chat drive body could not be built (%v)", subject, err),
			"re-run `villa doctor`")
	}
	url := orchestrate.LlamaInNetworkEndpoint() + "/chat/completions"
	helperImage := orchestrate.EmbedImage()

	res := residency.UnderLoad(ctx, residencyDepsFrom(sd), target, residency.Load{
		Drive: func(ctx context.Context) error {
			// Drive-only: the FAIL signal here is residency, not the chat round.
			_, derr := runProbeCurl(ctx, helperImage,
				"-sf", "-X", "POST", url,
				"-H", "Content-Type: application/json",
				"-d", string(body),
			)
			return derr
		},
		Rounds: searchResidencyDriveRounds,
		Settle: searchResidencySettle,
		Budget: agentProofBudget,
	})

	if !res.Sampled {
		return residency.Unevaluable(
			"could not evaluate "+subject+" — no search-augmented chat round stayed in flight long enough to sample residency under load",
			"check `systemctl --user status "+installServiceName+"` and `villa logs`; ensure the stack (incl. villa-searxng/villa-websafe) is up (`villa up`), then re-run `villa doctor`")
	}
	return res.Verdict
}

// liveAgentToolCallVerdict builds the tool-call round-trip seam liveDoctorDeps
// binds when the agent is enabled: a closure that runs the REUSED liveAgentToolCallProbe
// (DEFINED at install_agent.go; the SAME read→edit `crush run` driver verify_agent.go
// wires as agentTaskFn — never re-rolled here) and maps the outcome to an
// inference.Verdict consumed opaquely by the doctor core. A completed round-trip →
// StatusPass; not-completed → StatusFail; a probe error (binary absent, timeout, non-zero
// exit) → StatusFail (a confident failure to drive the agent is a real fault, not an
// unevaluable signal — the agent IS enabled). It is constructed (not run) at wiring time;
// the drive only fires when doctor.Aggregate invokes the seam.
func liveAgentToolCallVerdict(parent context.Context, _ config.VillaConfig) func() inference.Verdict {
	return func() inference.Verdict {
		ctx, cancel := context.WithTimeout(parent, agentProofBudget)
		defer cancel()
		completed, err := liveAgentToolCallProbe(ctx)()
		if err != nil {
			return inference.Verdict{
				Status:      inference.StatusFail,
				Detail:      fmt.Sprintf("the agent tool-call round-trip failed to run: %v", err),
				Remediation: "ensure the agent is installed (`villa install --coding-agent`) and the stack is up (`villa up`), then re-run `villa doctor`; check `villa verify agent` and `villa logs`",
			}
		}
		if !completed {
			return inference.Verdict{
				Status:      inference.StatusFail,
				Detail:      "the agent ran but did not complete the read→edit tool-call round-trip (the probe file was not edited as instructed)",
				Remediation: "check `villa verify agent` and `villa logs` — the coder model may not be serving tool-calls correctly",
			}
		}
		return inference.Verdict{
			Status: inference.StatusPass,
			Detail: "the agent completed a real read→edit tool-call round-trip over the local endpoint",
		}
	}
}

// liveAgentResidencyUnderLoad builds the coder-residency-under-load seam: a
// closure returning the CODER model's residency Verdict sampled DURING a real tool-call
// drive. It mirrors liveResidencyUnderLoad's drive→sample→join shape but drives the REUSED
// crush-run tool-call probe (install_agent.go) instead of the embed workload, and samples
// inference.RunningOffloadVerdict over the EXACT liveStatusDeps input set — keyed on the
// SERVED coder model file (sd.ModelFile resolves cfg.Model, which IS the coder under coding
// mode, per distinct served model). Every unmet precondition / unevaluable drive
// degrades to a typed-Unknown WARN; a confident CPU fallback of the coder under load is the
// silent-degradation FAIL this seam exists to catch (consumed opaquely by the core).
func liveAgentResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) func() inference.Verdict {
	return func() inference.Verdict { return runAgentResidencyUnderLoad(ctx, cfg, sd) }
}

// runAgentResidencyUnderLoad is the live under-tool-call-load residency proof for the
// served coder model. Strictly READ-ONLY (doctor never starts a
// service): villa-llama must be active; the backend + served model must resolve; otherwise
// it degrades to a typed-Unknown WARN. It drives sequential REUSED tool-call rounds and
// samples the coder model's GTT/journal residency ONLY while a round-trip is verifiably IN
// FLIGHT (a round that completes before the settle deadline is too fast to have
// loaded the model under observation, so it is joined and the next round driven — never
// sampled idle, which could mask a CPU-fallback-under-load false-green). Every sampled
// round is JOINED so no agent process outlives the call; if no round can be caught in
// flight within the bounded rounds / budget, it degrades to a typed-Unknown WARN.
func runAgentResidencyUnderLoad(ctx context.Context, cfg config.VillaConfig, sd *status.Deps) inference.Verdict {
	const subject = "coder residency under tool-call load"

	// The served inference unit (the coder under coding mode) must be active.
	if v := requireActive(sd, subject, installServiceName); v != nil {
		return *v
	}
	target, warn := residencyTargetFor(cfg, sd, subject)
	if warn != nil {
		return *warn
	}

	res := residency.UnderLoad(ctx, residencyDepsFrom(sd), target, residency.Load{
		Drive: func(ctx context.Context) error {
			// The REUSED read→edit `crush run` probe (install_agent.go), never
			// re-rolled here. Drive-only: the FAIL signal is residency, not the
			// round-trip, which liveAgentToolCallVerdict reports separately.
			_, derr := liveAgentToolCallProbe(ctx)()
			return derr
		},
		Rounds: agentResidencyDriveRounds,
		Settle: agentResidencySettle,
		Budget: agentProofBudget,
	})

	if !res.Sampled {
		// No round stayed in flight long enough to sample, so the "under load"
		// precondition was never met. An idle-sampled verdict could mask exactly the
		// CPU-fallback-under-load this seam exists to catch.
		return residency.Unevaluable(
			"could not evaluate "+subject+" — no tool-call round-trip stayed in flight long enough to sample residency under load",
			"check `villa verify agent` and `villa logs` — the agent may be erroring or exiting before it loads the coder model; ensure the stack is up (`villa up`), then re-run `villa doctor`")
	}
	return res.Verdict
}

// liveAgentDrift builds the drift seam: a closure that assembles the pure
// agent.DetectDrift inputs from the live host (the installed-binary SHA + on-disk
// crush.json + a freshly-rendered reference + the pinned policy binary hash) and returns
// the report-only DriftReport. It REUSES the code.go accessors (agentBinPath /
// hashFileSHA256 / crushConfigPath) and agent.Render / agent.LoadCrushPolicy — no
// re-typed image/marker literal. Any read error degrades to a typed-Unknown WARN report
// (BinaryDriftUnknown) rather than a fabricated drift — doctor never FAILs on a signal it
// could not evaluate. It is constructed (not run) at wiring time.
func liveAgentDrift(cfg config.VillaConfig) func() agent.DriftReport {
	return func() agent.DriftReport {
		reference, _, rerr := agent.Render(cfg, liveLSPProbes())
		if rerr != nil {
			return agent.DriftReport{
				BinaryDriftUnknown: true,
				Reason:             fmt.Sprintf("could not render the reference crush.json to check drift: %v", rerr),
			}
		}
		binSHA, binPresent, herr := hashFileSHA256(agentBinPath())
		if herr != nil {
			return agent.DriftReport{
				BinaryDriftUnknown: true,
				Reason:             fmt.Sprintf("could not hash the villa-owned Crush binary to check drift: %v", herr),
			}
		}
		onDisk, configPresent, cerr := readCrushConfig()
		if cerr != nil {
			return agent.DriftReport{
				BinaryDriftUnknown: true,
				Reason:             fmt.Sprintf("could not read the on-disk crush.json to check drift: %v", cerr),
			}
		}
		var policyBinSHA string
		if asset, ok := agent.LoadCrushPolicy().Assets["linux/amd64"]; ok {
			policyBinSHA = asset.BinarySHA256
		}
		return agent.DetectDrift(agent.DriftInput{
			BinaryPresent:   binPresent,
			InstalledBinSHA: binSHA,
			PolicyBinSHA:    policyBinSHA,
			ConfigPresent:   configPresent,
			OnDiskConfig:    onDisk,
			RenderedConfig:  reference,
		})
	}
}

// readCrushConfig reads ~/.config/crush/crush.json for the drift compare. A not-exist read
// maps to (nil, false, nil) — the FIRST-RUN trigger (ConfigPresent=false), distinct from a
// real read error. Mirrors the liveAgentDeps.ReadConfig seam (code.go).
func readCrushConfig() ([]byte, bool, error) {
	path, err := crushConfigPath()
	if err != nil {
		return nil, false, err
	}
	b, err := os.ReadFile(path) //nolint:gosec // path is the XDG-resolved crush config, not user input
	if os.IsNotExist(err) {
		return nil, false, nil
	}
	if err != nil {
		return nil, false, err
	}
	return b, true, nil
}
