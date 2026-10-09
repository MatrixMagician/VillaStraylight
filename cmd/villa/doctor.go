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
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/doctor"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/memory"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/preflight"
	"github.com/MatrixMagician/VillaStraylight/internal/residency"
	"github.com/MatrixMagician/VillaStraylight/internal/stackapply"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
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
			// The config is loaded once, here, and doctor decides from it: which
			// subsystems to report on, drift, the sandbox network, tools mode (ADR-0017).
			cfg, err := config.LoadVilla()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "doctor: load config: %v\n", err)
				os.Exit(exitBlocked)
			}
			deps, err := liveDoctorDeps(cmdContext(cmd), cfg)
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "doctor: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runDoctor(cmd, args, cfg, deps))
			return nil
		},
	}
}

// runDoctor builds the Report from the injected core and renders it. It RETURNS the exit
// code (no os.Exit) so doctor_test.go drives it deterministically. All printing + exit
// mapping lives here; the worst-wins fold is doctor.Aggregate.
func runDoctor(cmd *cobra.Command, _ []string, cfg config.VillaConfig, deps doctor.Deps) int {
	report := doctor.Aggregate(cfg, deps)
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
//
// The FAULT: trailer below is the human table's epilogue, not part of the JSON document
// (issue #140): --json output must end at the document so a piped `jq` can parse it.
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
		if !asJSON {
			fmt.Fprintf(w, "\nFAULT: %d blocking finding(s) — the running install is not healthy. See the remediation(s) above.\n", blockFails)
		}
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
		if !asJSON {
			fmt.Fprintf(w, "\nFAULT: unrecognized overall verdict %q — treating the install as not healthy.\n", r.Overall)
		}
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
// directory-creation step — doctor never creates it (Pitfall 2). If the dir is absent,
// the core degrades it to a typed-Unknown WARN, so resolving the path is all this
// needs to do.
func unitDirReadOnly() (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(base, "containers", "systemd"), nil
}

// liveUnitDirExists reports whether the Quadlet dir is there, without creating it. An
// error means it could not be resolved or examined, which is not "absent".
func liveUnitDirExists() (bool, error) {
	dir, err := unitDirReadOnly()
	if err != nil {
		return false, err
	}
	if _, err := os.Stat(dir); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return false, nil
		}
		return false, err
	}
	return true, nil
}

// liveReadUnit reads one unit from the Quadlet dir; an absent unit satisfies
// errors.Is(err, fs.ErrNotExist).
func liveReadUnit(name string) ([]byte, error) {
	dir, err := unitDirReadOnly()
	if err != nil {
		return nil, err
	}
	return os.ReadFile(filepath.Join(dir, name)) //nolint:gosec // the unit dir is the fixed rootless Quadlet dir
}

// liveRenderUnits is the read-only plan every verb applies (ADR-0013), so the drift
// check renders coding mode exactly as `coding-mode enter` wrote it (#249). Only the
// binary mount differs: doctor renders with the path the installed unit records, and
// the core says which (#141).
func liveRenderUnits(cfg config.VillaConfig, hostVilla string) ([]orchestrate.Unit, error) {
	stack := liveStackDeps()
	stack.HostVillaPath = func() string { return hostVilla }
	return stackapply.Render(stack, cfg)
}

// liveCatalogGeometry binds the catalog-geometry seam (CAT-01): UNCONDITIONALLY — the
// catalog is not an optional subsystem, and an entry that no longer describes its file
// is wrong on every host. It is nil only when the catalog itself cannot be loaded,
// which is the core's no-finding case rather than a fabricated PASS. The open seam
// confines every filename to the models dir before opening it, because a shard
// filename from an external catalog is untrusted input.
func liveCatalogGeometry(cfg config.VillaConfig) func() []preflight.CheckResult {
	cat, _, err := catalog.Load(cmp.Or(modelCatalogPath, cfg.CatalogPath))
	if err != nil {
		return nil
	}
	dir := modelsDir()
	return func() []preflight.CheckResult {
		return preflight.RunCatalogGeometry(cat, func(filename string) (io.ReadCloser, error) {
			path := filepath.Join(dir, filename)
			if perr := pathsafe.Inside(path, dir); perr != nil {
				return nil, perr
			}
			return os.Open(path) //nolint:gosec // confined to the models dir immediately above
		})
	}
}

// liveDoctorDeps wires doctor.Deps to the real host for the config the run loaded. It
// binds reads, the four proofs that drive real workloads, and the preflight bindings,
// and nothing else: it decides nothing, so it binds every seam whatever the config
// says and doctor.Aggregate gates each on cfg (ADR-0017). It REUSES liveStatusDeps
// wholesale for the running-stack read-model (no re-wired HTTP/journald/GTT probes —
// RESEARCH A1) and never writes.
//
// ctx is the command's SIGINT/SIGTERM-cancelled context, captured by the proof
// seams below. Without it `villa doctor` could not be interrupted: the three
// residency proofs drive a live stack for up to residencyProofBudget /
// agentProofBudget (60-90s) each, and the agent tool-call probe adds another 90s,
// so a Ctrl-C landed on a command that kept running for minutes. Cancelling is
// safe by construction — doctor is read-only and mutates nothing, so an aborted
// run leaves no half-applied state, and the podman probe containers are named, so
// probeCurl force-removes one when its context is cancelled (#329).
func liveDoctorDeps(ctx context.Context, cfg config.VillaConfig) (doctor.Deps, error) {
	sd, err := liveStatusDeps()
	if err != nil {
		return doctor.Deps{}, err
	}
	// One doctor run takes ONE host reading (ADR-0016). The host-condition checks,
	// the status report and the three residency proofs' weight footprints all read
	// this memo, which probes on first use, so each reuses the reading the status
	// report is folded from instead of probing again (up to seven probes before).
	// doctor is one-shot, so the memo cannot go stale the way it would in the
	// long-lived dashboard, which is why only doctor wraps the seam.
	sd.Probe = sync.OnceValue(sd.Probe)
	// ...and ONE config: the status report is folded from the config doctor decides
	// from, not from a second load that could disagree with it.
	sd.LoadConfig = func() (config.VillaConfig, error) { return cfg, nil }
	return doctor.Deps{
		Probe:           sd.Probe,
		StatusReport:    func() status.Report { return status.Run(*sd) },
		IsActive:        sd.IsActive,
		RunMemoryChecks: preflight.RunMemory,
		ReadVerifyState: sd.ReadVerifyState,
		CatalogGeometry: liveCatalogGeometry(cfg),
		// SBX-01 reuses the exact same preflight.SandboxDeps PRE-09 uses
		// (liveSandboxDeps, preflight_sandbox.go) so `villa preflight` and `villa
		// doctor` can never observe the host through a different probe. It is built
		// on first use: it resolves the sandbox image pin, which a sandbox-off run
		// never needs.
		RunSandboxChecks: func(detect.HostProfile) []preflight.CheckResult {
			return preflight.RunSandbox(liveSandboxDeps())
		},
		// Each proof REUSES a shipped probe — never a re-rolled crush-run round-trip
		// or residency scrape — and is consumed opaquely as an inference.Verdict (no
		// backend marker literal in cmd/villa; TestSeamGrepGate walks this tree). They
		// are constructed here, not run: a drive only fires when doctor.Aggregate
		// invokes the seam for a subsystem that is on.
		ResidencyUnderLoad:       liveResidencyUnderLoad(ctx, cfg, sd),
		AgentToolCall:            liveAgentToolCallVerdict(ctx, cfg),
		AgentResidencyUnderLoad:  liveAgentResidencyUnderLoad(ctx, cfg, sd),
		SearchResidencyUnderLoad: liveSearchResidencyUnderLoad(ctx, cfg, sd),
		ImageResidency:           liveImageResidency(ctx, cfg, sd),
		UnitDirExists:            liveUnitDirExists,
		ReadUnit:                 liveReadUnit,
		RenderUnits:              liveRenderUnits,
		RunningVilla:             hostVillaPath,
		ContainerNetworks:        liveContainerNetworks,
		NetworkInternal:          liveNetworkInternal,
		// The agent drift reads reuse the code.go accessors (agentBinPath /
		// hashFileSHA256 / crushConfigPath) and agent.Render; no re-typed literal.
		AgentBinarySHA:  func() (string, bool, error) { return hashFileSHA256(agentBinPath()) },
		ReadCrushConfig: readCrushConfig,
		RenderCrushConfig: func(cfg config.VillaConfig) ([]byte, error) {
			reference, _, err := agent.Render(cfg, liveLSPProbes())
			return reference, err
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
// /props is read with cfg, the config the proof runs against, as the status fold
// reads it with the config its run loaded. The under-load proofs supply their own
// workload; PollHealth/Generate/GPUBusy are unused by that path and stay nil.
func residencyDepsFrom(sd *status.Deps, cfg config.VillaConfig) residency.Deps {
	return residency.Deps{
		Journal: sd.JournalText,
		GTTUsed: sd.GTTUsed,
		Props:   func() *inference.PropsInfo { return sd.Props(cfg) },
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
// evaluates. The weight footprint is computed from sd.Probe's reading, which
// liveDoctorDeps memoizes, so a proof reuses the host reading the status report took.
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
		WeightBytes: sd.WeightBytes(cfg, sd.Probe()),
		Markers:     backend.ResidencyProof(),
	}, nil
}

// The read-only precondition gate (doctor NEVER starts a service; an inactive
// unit degrades to a typed-Unknown WARN naming it, never a fabricated FAIL)
// lives in residency.ProveUnderLoad, stated once for every under-load proof.

// liveResidencyUnderLoad builds the live proof seam liveDoctorDeps always binds
// (doctor.Aggregate gates the call on subsystem.MemoryOn): a closure returning the chat-model residency Verdict sampled
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
//     config-resolved villa-embed /v1/embeddings over villa-closed via runClosedProbeCurlCode
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

	// (1) Memory-specific precondition — strictly read-only; the shared gate
	// (services active) and honesty mapping live in residency.ProveUnderLoad.
	if dec := memory.Decide(cfg); !dec.Enabled || !dec.Valid {
		return residency.Unevaluable(
			"could not evaluate "+subject+" — the memory stack is not enabled/valid in config",
			"fix the memory_* fields in config.toml (see `villa preflight`), then re-run `villa doctor`")
	}
	embedService := unitServiceName(orchestrate.EmbedContainerUnitName())

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

	// (3) Drive and sample via the shared proof shape. Embed requests are cheap and
	// uniform, so the load evidence is the WARMUP — the embedder has demonstrably
	// completed real requests — rather than a settle deadline no request that short
	// would survive; a PASS sampled under a faltering drive is degraded (Faltered),
	// because the embedder was not exercised.
	return residency.ProveUnderLoad(ctx, residencyDepsFrom(sd, cfg), residency.ProofSpec{
		Subject:  subject,
		IsActive: sd.IsActive,
		Services: []string{
			installServiceName,
			unitServiceName(orchestrate.QdrantContainerUnitName()),
			embedService,
		},
		ResolveTarget: func() (residency.Target, *inference.Verdict) {
			return residencyTargetFor(cfg, sd, subject)
		},
		Load: residency.Load{
			Drive: func(ctx context.Context) error {
				_, _, derr := runClosedProbeCurlCode(ctx, helperImage,
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
		},
		Unsampled: func(r residency.LoadResult) inference.Verdict {
			return residency.Unevaluable(
				fmt.Sprintf("could not evaluate %s — the embed drive could not complete (%d of %d requests finished before the budget)", subject, r.Completed, r.Rounds),
				fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", embedService))
		},
		Faltered: func(r residency.LoadResult) inference.Verdict {
			return residency.Unevaluable(
				fmt.Sprintf("could not evaluate %s — the embed drive could not complete (%d of %d requests failed)", subject, r.DriveErrs, r.Rounds),
				fmt.Sprintf("check `systemctl --user status %s` and `villa logs`, then re-run `villa doctor`", embedService))
		},
	})
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
// discipline for the search-stack drive: drive bounded sequential chat rounds and sample
// the served model's residency ONLY while a round is verifiably IN FLIGHT — never idle
// (which could mask a CPU-fallback-under-load false-green). The round is a plain chat
// completion sent while villa-searxng/villa-websafe are up (the precondition gate);
// it never queries either of them, so what it proves is the served model's residency
// with the search stack running beside it, not a search-augmented generation.
//
// searchResidencyMaxTokens is how far each round decodes. It must be long enough that
// a round is still decoding at searchResidencySettle, so the two decode rates bracket
// it: at the fastest rate (searchResidencyDecodeRateMax tok/s, measured 230 tok/s with
// ngram speculation on the dev host) max_tokens must still outlast the settle with
// margin, and at the slowest (searchResidencyDecodeRateMin tok/s, a dense model) every
// round the proof may drive must fit inside agentProofBudget. 16 tokens finished in
// ~0.38 s, before the settle, and the check was a permanent WARN (#286).
const (
	searchResidencyDriveRounds   = 3
	searchResidencySettle        = 750 * time.Millisecond
	searchResidencyMaxTokens     = 512
	searchResidencyDecodeRateMax = 250
	searchResidencyDecodeRateMin = 20
)

// searchResidencyDriveBody is the bounded chat-completion drive payload: a fixed
// searchResidencyMaxTokens completion that keeps villa-llama DECODING past the settle
// (so the residency sample observes the served model under real load) without an
// unbounded generation. ignore_eos is llama-server's non-OpenAI sampling field (documented
// in its server README beside the other /completion options, which /v1/chat/completions
// accepts): without it the model ends the reply at its own EOS and the round can finish
// before the settle. The model id is JSON-marshaled, never interpolated into a command
// string (the runResidencyUnderLoad precedent). stream=false keeps the round a single
// bounded request.
func searchResidencyDriveBody(model string) ([]byte, error) {
	return json.Marshal(map[string]any{
		"model": model,
		"messages": []map[string]string{
			{"role": "user", "content": "villa search-residency drive probe: write continuously."},
		},
		"max_tokens": searchResidencyMaxTokens,
		"ignore_eos": true,
		"stream":     false,
	})
}

// liveSearchResidencyUnderLoad builds the chat-model-residency-under-
// SEARCH-load seam: a closure returning the served model's residency Verdict sampled DURING
// a bounded chat drive (with villa-searxng/villa-websafe up). It mirrors
// liveAgentResidencyUnderLoad's drive→settle→sample-if-in-flight→join shape but drives a
// bounded chat completion (the cheapest honest drive that keeps villa-llama decoding past
// the settle) instead of the crush-run tool-call probe. It is constructed (not run) at
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
//  2. DRIVE + IN-FLIGHT SAMPLE: drive sequential bounded chat rounds (none touches searxng/websafe);
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

	// The bounded chat-completion drive payload (model id JSON-marshaled, never
	// interpolated). It is the cheapest honest drive that keeps villa-llama decoding
	// past the settle.
	body, err := searchResidencyDriveBody(cfg.Model)
	if err != nil {
		return residency.Unevaluable(
			fmt.Sprintf("could not evaluate %s — the chat drive body could not be built (%v)", subject, err),
			"re-run `villa doctor`")
	}
	// The round goes through the in-network inference client (ADR-0014): its curl
	// request carries the api key on stdin, never on a command line (#252).
	chat := inNetworkInferenceClient(cfg).CurlChatCompletions(body)
	helperImage := orchestrate.EmbedImage()

	// The shared proof shape carries the read-only gate (villa-llama AND the
	// web-search services active) and the unsampled honesty mapping; the settle
	// discipline samples only a round verifiably IN FLIGHT.
	return residency.ProveUnderLoad(ctx, residencyDepsFrom(sd, cfg), residency.ProofSpec{
		Subject:  subject,
		IsActive: sd.IsActive,
		Services: []string{
			installServiceName,
			unitServiceName(orchestrate.SearXNGContainerUnitName()),
			unitServiceName(orchestrate.WebsafeContainerUnitName()),
		},
		ResolveTarget: func() (residency.Target, *inference.Verdict) {
			return residencyTargetFor(cfg, sd, subject)
		},
		Load: residency.Load{
			Drive: func(ctx context.Context) error {
				// Drive-only: the FAIL signal here is residency, not the chat round.
				_, derr := runProbeCurlIn(ctx, helperImage, chat, "-sf")
				return derr
			},
			Rounds: searchResidencyDriveRounds,
			Settle: searchResidencySettle,
			Budget: agentProofBudget,
		},
		Unsampled: func(residency.LoadResult) inference.Verdict {
			return residency.Unevaluable(
				"could not evaluate "+subject+" — no chat round stayed in flight long enough to sample residency under load",
				"check `systemctl --user status "+installServiceName+"` and `villa logs`; ensure the stack (incl. villa-searxng/villa-websafe) is up (`villa up`), then re-run `villa doctor`")
		},
	})
}

// liveAgentToolCallVerdict builds the tool-call round-trip seam liveDoctorDeps
// always binds (doctor.Aggregate gates the call on subsystem.AgentOn): a closure that runs the REUSED liveAgentToolCallProbe
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

	// The shared proof shape carries the read-only gate (the served inference unit —
	// the coder under coding mode — must be active) and the unsampled honesty
	// mapping; the settle discipline samples only a round verifiably IN FLIGHT.
	return residency.ProveUnderLoad(ctx, residencyDepsFrom(sd, cfg), residency.ProofSpec{
		Subject:  subject,
		IsActive: sd.IsActive,
		Services: []string{installServiceName},
		ResolveTarget: func() (residency.Target, *inference.Verdict) {
			return residencyTargetFor(cfg, sd, subject)
		},
		Load: residency.Load{
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
		},
		Unsampled: func(residency.LoadResult) inference.Verdict {
			// No round stayed in flight long enough to sample, so the "under load"
			// precondition was never met. An idle-sampled verdict could mask exactly
			// the CPU-fallback-under-load this seam exists to catch.
			return residency.Unevaluable(
				"could not evaluate "+subject+" — no tool-call round-trip stayed in flight long enough to sample residency under load",
				"check `villa verify agent` and `villa logs` — the agent may be erroring or exiting before it loads the coder model; ensure the stack is up (`villa up`), then re-run `villa doctor`")
		},
	})
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
