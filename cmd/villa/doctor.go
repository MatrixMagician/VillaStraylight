package main

// doctor.go is the thin cobra caller for the read-only `villa doctor` health-diagnosis
// verb (DOCTOR-01/02/03): the running-install twin of `villa preflight`. The worst-wins
// decision logic — composing the preflight host-prep gate, the status read-model + its
// per-service offload Verdict, and an orchestrate.Reconcile config-vs-disk drift Plan —
// lives in the pure internal/doctor core (Plan 01). This file keeps ONLY: the cobra
// wiring + exit-code mapping (reusing the AUTHORITATIVE preflight constants), the human
// table renderer, and the live host wiring (liveDoctorDeps) that constructs doctor.Deps.
//
// doctor is strictly READ-ONLY (D-03): it mutates nothing. Note unitDirReadOnly — the
// quadletUnitDir twin that drops the directory-creation step — so a diagnosis never
// creates the Quadlet dir (Pitfall 2). There is no --force and no generation probe (D-07). No backend marker
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

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// newDoctor builds `villa doctor`: a read-only, one-shot health diagnosis of the RUNNING
// install. It composes the pure doctor core over live host seams and maps the worst-wins
// Report to an exit code mirroring `villa preflight`: 0 (healthy), 2 (warnings/drift), or
// 1 (a blocking fault — e.g. a confident CPU fallback). It mutates nothing (D-03): no
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
			deps, err := liveDoctorDeps()
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
// CRITICAL (D-04 / Pitfall 1 — the shipped preflight constants are AUTHORITATIVE, NOT the
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
	// stopped stack folds to WARN, never FAIL (D-08).
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
	default:
		return exitPass
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
// directory-creation step — doctor never creates it (Pitfall 2 / D-03). If the dir is absent, the drift read
// fails and the core degrades it to a typed-Unknown WARN (D-08), so resolving the path is
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
func liveDoctorDeps() (doctor.Deps, error) {
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
		// Surface a BackendFor error rather than swallowing it (WR-01): if a future
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
	// Memory seams (D-08/D-09, mirroring the rocmImageGate conditional shape):
	// bound ONLY when the persisted memory stack is opted in; all four stay
	// zero/nil when off so the memory-off doctor output is byte-identical
	// (mirror D-06). The service names come from the orchestrate accessors
	// (QdrantContainerUnitName/EmbedContainerUnitName) converted via the same
	// .container → .service derivation the status fold uses — never a typed
	// service-name literal here.
	var (
		memEnabled  bool
		memServices []string
		memChecks   func(detect.HostProfile) []preflight.CheckResult
		memProof    func() inference.Verdict
	)
	if cfg.MemoryEnabled {
		memEnabled = true
		memServices = []string{
			unitServiceName(orchestrate.QdrantContainerUnitName()),
			unitServiceName(orchestrate.EmbedContainerUnitName()),
		}
		embeddingModel := cfg.EmbeddingModel
		// D-08 composition over re-implementation: the memory host gate IS
		// preflight.RunMemory — doctor never re-rolls the disk/headroom logic.
		memChecks = func(p detect.HostProfile) []preflight.CheckResult {
			return preflight.RunMemory(p, preflight.MemoryGateInput{EmbeddingModel: embeddingModel})
		}
		memProof = liveResidencyUnderLoad(cfg, sd)
	}
	return doctor.Deps{
		Probe:              detect.Probe,
		LoadConfig:         config.LoadVilla,
		StatusReport:       func() status.Report { return status.Run(*sd) },
		Backend:            cfg.Backend,
		RunROCmImage:       rocmImageGate,
		MemoryEnabled:      memEnabled,
		MemoryServices:     memServices,
		RunMemoryChecks:    memChecks,
		ResidencyUnderLoad: memProof,
		// DriftPlan: render units from the persisted config, resolve the backend
		// fail-closed (D-02), and Reconcile against the READ-ONLY unit dir. It NEVER
		// writes. A read error (absent/unreadable unit dir) is returned verbatim so the
		// core degrades it to a typed-Unknown WARN (D-08) rather than swallowing it.
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
			units, err := orchestrate.Render(orchestrate.RenderInput{
				Backend:   backend,
				Cfg:       c,
				ModelFile: modelFile,
				ModelsDir: modelsDir(),
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
			// WARN ("units not yet written") instead (D-08 / WR-01). This stat is the
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

// Residency-under-embedding-load proof tuning (D-09, T-22-11 DoS bounds): a REAL but
// strictly bounded /v1/embeddings workload — N sequential multi-KiB requests, each with
// its own timeout, the whole proof under one parent budget, all probe containers
// transient (--rm, Pitfall 7).
const (
	// residencyDriveRequests is the bounded request count of the embed-load drive.
	residencyDriveRequests = 12
	// residencySampleAfter is how many drive requests must have completed before the
	// mid-drive residency sample fires (Pitfall 6: the sample must be DURING load,
	// not before the embedder has actually started working).
	residencySampleAfter = 2
	// residencyRequestTimeout bounds each individual embed-drive request.
	residencyRequestTimeout = 10 * time.Second
	// residencyProofBudget bounds the WHOLE proof (drive + sample + join).
	residencyProofBudget = 60 * time.Second
)

// residencyDriveText is the ~4 KiB embedding input each drive request carries — large
// enough that the embedder does real per-request work (a one-word probe would finish
// before the residency sample could observe load), small enough to stay well inside
// the embed context window. Repeats a fixed 45-byte phrase 96 times (~4.2 KiB).
func residencyDriveText() string {
	return strings.Repeat("villa residency-under-load drive probe text; ", 96)
}

// residencyUnevaluable builds the typed-Unknown WARN Verdict every unmet precondition
// and unevaluable drive degrades to (D-09/D-10): never a false-green PASS, never a
// FAIL fabricated from a signal that could not be evaluated.
func residencyUnevaluable(detail, remediation string) inference.Verdict {
	return inference.Verdict{
		Status:      inference.StatusWarn,
		Detail:      "could not evaluate residency under embedding load — " + detail,
		Remediation: remediation,
	}
}

// liveResidencyUnderLoad builds the D-09/D-10 live proof seam liveDoctorDeps binds when
// memory is enabled: a closure returning the chat-model residency Verdict sampled
// DURING a real embed-load drive. It is constructed (not run) at wiring time; the
// drive/sample only fire when doctor.Aggregate invokes the seam.
func liveResidencyUnderLoad(cfg config.VillaConfig, sd *status.Deps) func() inference.Verdict {
	return func() inference.Verdict { return runResidencyUnderLoad(cfg, sd) }
}

// runResidencyUnderLoad is the live under-load residency proof (D-09, the live half of
// MEM-DOC-residency; composed per 22-PATTERNS from liveMemoryProof's drive + the
// liveStatusDeps residency inputs — no analog exists for the interleaving):
//
//  1. D-10 PRECONDITION GATE (read-only — doctor NEVER starts a service): memory must
//     decide enabled+valid and villa-llama, villa-qdrant and villa-embed must all be
//     active. Any unmet precondition degrades to a typed-Unknown WARN naming the
//     precondition (never a FAIL fabricated from a stack that simply is not running).
//  2. DRIVE (T-22-09/T-22-11): a goroutine sends residencyDriveRequests sequential
//     POSTs to the config-resolved villa-embed /v1/embeddings over villa.network via
//     runProbeCurl (fixed-arg podman run --rm, helper image via orchestrate.EmbedImage(),
//     model id JSON-marshaled — never interpolated into a command string). Each request
//     is bounded by residencyRequestTimeout, the whole proof by residencyProofBudget.
//  3. SAMPLE MID-DRIVE (Pitfall 6): after residencySampleAfter completions and while
//     requests are still in flight, evaluate inference.RunningOffloadVerdict over the
//     EXACT liveStatusDeps input set — villa-llama's ResidencyJournal, the point-in-time
//     detect.GTTUsedBytes, liveWeightBytes(cfg), and BackendFor(cfg.Backend).ResidencyProof().
//  4. JOIN + HONESTY: the drive goroutine is drained before returning (no probe
//     container outlives the call). Drive errors alone degrade a PASS to WARN ("embed
//     drive could not complete") — the FAIL signal is the CHAT model's residency, not
//     the drive's success; a confident residency FAIL always stands.
func runResidencyUnderLoad(cfg config.VillaConfig, sd *status.Deps) inference.Verdict {
	// (1) D-10 precondition gate — strictly read-only, degrade to WARN.
	if dec := memory.Decide(cfg); !dec.Enabled || !dec.Valid {
		return residencyUnevaluable(
			"the memory stack is not enabled/valid in config",
			"fix the memory_* fields in config.toml (see `villa preflight`), then re-run `villa doctor`")
	}
	chatService := installServiceName
	for _, svc := range []string{
		chatService,
		unitServiceName(orchestrate.QdrantContainerUnitName()),
		unitServiceName(orchestrate.EmbedContainerUnitName()),
	} {
		state, err := sd.IsActive(svc)
		if err != nil || state != "active" {
			return residencyUnevaluable(
				fmt.Sprintf("%s is not active", svc),
				fmt.Sprintf("check `systemctl --user status %s`; run `villa up` if the stack is stopped, then re-run `villa doctor`", svc))
		}
	}
	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return residencyUnevaluable(
			fmt.Sprintf("the configured backend could not be resolved (%v)", err),
			"fix the backend field in config.toml (`villa backend set`), then re-run `villa doctor`")
	}

	// (2) The bounded embed-load drive. The body is JSON-marshaled (model id never
	// interpolated into a command string, T-22-09/T-19-10) and reused verbatim for
	// every request; the URL host:port composition mirrors liveMemoryProof.
	body, err := json.Marshal(map[string]any{
		"input":           residencyDriveText(),
		"model":           cfg.EmbeddingModel,
		"encoding_format": "float",
	})
	if err != nil {
		return residencyUnevaluable(
			fmt.Sprintf("the embed drive body could not be built (%v)", err),
			"re-run `villa doctor`")
	}
	url := fmt.Sprintf("http://%s:%d/v1/embeddings", cfg.EmbedAddr, cfg.EmbedPort)
	helperImage := orchestrate.EmbedImage()

	ctx, cancel := context.WithTimeout(context.Background(), residencyProofBudget)
	defer cancel()

	// completions carries one entry per attempted drive request; closing it is the
	// drive goroutine's exit signal, so draining it below JOINS the goroutine and
	// guarantees no --rm probe container outlives this call (T-22-11).
	completions := make(chan error, residencyDriveRequests)
	go func() {
		defer close(completions)
		for i := 0; i < residencyDriveRequests; i++ {
			if ctx.Err() != nil {
				return // parent budget exhausted — stop driving
			}
			reqCtx, reqCancel := context.WithTimeout(ctx, residencyRequestTimeout)
			_, derr := runProbeCurl(reqCtx, helperImage,
				"-sf", "-X", "POST", url,
				"-H", "Content-Type: application/json",
				"-d", string(body),
			)
			reqCancel()
			completions <- derr
		}
	}()

	// (3) Sample DURING load: the channel is buffered, so while this consumer handles
	// completion i the drive goroutine is already running request i+1 — the sample
	// below therefore reads the journal/GTT with an embed request in flight.
	var (
		completed, driveErrs int
		sampled              bool
		verdict              inference.Verdict
	)
	for derr := range completions {
		completed++
		if derr != nil {
			driveErrs++
		}
		if !sampled && completed >= residencySampleAfter && completed < residencyDriveRequests {
			journal, _ := sd.JournalText(chatService)
			verdict = inference.RunningOffloadVerdict(inference.RunningOffloadInput{
				JournalText:   journal,
				GTTUsedBytes:  detect.GTTUsedBytes(),
				WeightBytes:   liveWeightBytes(cfg),
				ConfigModel:   cfg.Model,
				ConfigContext: cfg.Ctx,
				Markers:       backend.ResidencyProof(),
			})
			sampled = true
		}
	}

	// (4) Join is complete (channel closed). Map the outcome honestly.
	if !sampled {
		return residencyUnevaluable(
			fmt.Sprintf("the embed drive could not complete (%d of %d requests finished before the budget)", completed, residencyDriveRequests),
			fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", unitServiceName(orchestrate.EmbedContainerUnitName())))
	}
	if driveErrs > 0 && verdict.Status == inference.StatusPass {
		// The workload did not fully exercise the embedder — a PASS sampled under a
		// faltering drive is not a proven PASS (no false-green); a confident FAIL or
		// an already-WARN verdict stands on its own.
		return residencyUnevaluable(
			fmt.Sprintf("the embed drive could not complete (%d of %d requests failed)", driveErrs, residencyDriveRequests),
			fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", unitServiceName(orchestrate.EmbedContainerUnitName())))
	}
	return verdict
}
