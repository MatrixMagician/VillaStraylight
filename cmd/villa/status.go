package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"net"
	"net/http"
	"os"
	"strconv"
	"text/tabwriter"
	"time"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/catalog"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/inprobe"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/pathsafe"
	"github.com/MatrixMagician/VillaStraylight/internal/pinstate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/recommend"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
	"github.com/MatrixMagician/VillaStraylight/internal/taskstore"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
	"github.com/MatrixMagician/VillaStraylight/internal/voice"
)

// status.go is the thin cobra caller for the offload-asserting `villa status` slice
// The read-model core — aggregation, the worst-wins fold,
// the publish-port privacy parse, and the frozen --json contract — was extracted to
// internal/status so the dashboard backend calls the SAME logic.
// This file keeps only: the cobra wiring + exit-code mapping, the human table
// renderer (CLI presentation), and the live host wiring (HTTP/journald/GTT probes)
// that constructs status.Deps. status_test.go drives runStatus through a stubbed
// status.Deps and freezes the --json contract byte-for-byte.

// statusHTTPTimeout bounds a single probe of a managed service. The inference
// unit's own reads are bounded by its client (ADR-0014).
const statusHTTPTimeout = 3 * time.Second

// newStatus builds `villa status`: aggregate unit + container + /health + offload
// Verdict into one table (or --json), assert the loopback/no-telemetry posture, and
// exit 0/2/1. The exit-code mapping lives entirely here (return-not-Exit verb body;
// cobra RunE calls os.Exit) mirroring runInference/runInstall.
func newStatus() *cobra.Command {
	return &cobra.Command{
		Use:   "status",
		Short: "Show aggregated health: unit + container + /health + GPU-offload proof, and the loopback/no-telemetry posture",
		Long: "Aggregate, for every service in the generated stack, the systemd unit active-state, the " +
			"container /health, and the running-server GPU-offload Verdict (residency proven from the " +
			"journald load_tensors Vulkan0 line, corroborated by a point-in-time GTT floor) into one " +
			"table. Asserts every published port binds loopback (none on 0.0.0.0) and that there is no " +
			"telemetry. Exits 0 (all PASS), 2 (any WARN), or 1 (any FAIL).",
		Args: cobra.NoArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			deps, err := liveStatusDeps()
			if err != nil {
				fmt.Fprintf(cmd.ErrOrStderr(), "status: %v\n", err)
				os.Exit(exitBlocked)
			}
			os.Exit(runStatus(cmd, args, deps))
			return nil
		},
	}
}

// runStatus builds the Report from the injected core and renders it. It RETURNS the
// exit code (no os.Exit) so status_test.go drives it deterministically. All printing
// + exit mapping lives here; the read-model is status.Run/status.Aggregate.
func runStatus(cmd *cobra.Command, _ []string, d *status.Deps) int {
	out := cmd.OutOrStdout()
	errOut := cmd.ErrOrStderr()

	report := status.Run(*d)
	if err := report.Err(); err != nil {
		fmt.Fprintf(errOut, "status: %v\n", err)
		return exitBlocked
	}

	if jsonOut {
		enc := json.NewEncoder(out)
		enc.SetIndent("", "  ")
		_ = enc.Encode(report)
	} else {
		renderStatusTable(out, report, verbose)
	}

	switch status.Aggregate(report) {
	case inference.StatusPass:
		return exitPass
	case inference.StatusWarn:
		return exitWarn
	default:
		return exitBlocked
	}
}

// renderStatusTable writes the aggregated report as an aligned human table: the
// overall verdict, each service's active/health/offload row, and the privacy
// posture (loopback-only + the no-telemetry statement). With -v it adds each
// offload Verdict's detail/provenance. This is CLI presentation (not the
// read-model), so it stays in cmd/villa.
func renderStatusTable(w io.Writer, r status.Report, withProvenance bool) {
	tw := tabwriter.NewWriter(w, 0, 4, 2, ' ', 0)
	fmt.Fprintf(tw, "overall\t%s\n", r.Overall)

	// Active-backend surface: backend name always; the digest-pinned image tag
	// is verbose-only to keep the default table compact (gated behind -v like the
	// offload provenance). The image string is a value from the resolved backend
	// (Report.Image), never a literal in this renderer.
	fmt.Fprintf(tw, "backend\t%s\n", r.Backend)
	fmt.Fprintf(tw, "speculation\t%s\n", r.Speculation)
	fmt.Fprintf(tw, "vision\t%s\n", yesNo(r.Vision))
	// Rendered only when on: an off stack is the unchanged default, and a line
	// saying so on every install would be noise (spec v1.11 §3.5).
	if r.Tools {
		fmt.Fprintf(tw, "mode\t%s\n", "tools")
	}
	// Last task: rendered only when present (sandbox off, or no task has ever
	// run, both leave r.LastTask nil) — the honest empty state is no line, not a
	// placeholder (spec v1.11 §10).
	if r.LastTask != nil {
		fmt.Fprintf(tw, "last task\t%s %s\n", r.LastTask.ID, r.LastTask.State)
	}
	if withProvenance {
		fmt.Fprintf(tw, "image\t%s\n", r.Image)
	}
	// Live tok/s: rendered ONLY when present — an idle/unavailable reading is
	// omitted (the seam returned nil), never a fabricated 0. Labeled by the active
	// backend so the user sees which backend produced the rate.
	if r.GenTokensPerSec != nil {
		fmt.Fprintf(tw, "gen tok/s\t%.1f (%s)\n", *r.GenTokensPerSec, r.Backend)
	}
	// ROCm-readiness tri-state: the folded indicator (ready/not-ready/unknown).
	fmt.Fprintf(tw, "rocm-readiness\t%s\n", r.ROCmReadiness)

	// Cumulative usage: rendered ONLY when present — an absent/empty store is
	// omitted (the read-only seam returned nil), never fabricated 0s. Prints the
	// per-model cumulative prompt/generated token totals.
	if r.Usage != nil {
		for _, m := range r.Usage.Models {
			fmt.Fprintf(tw, "usage %s\tprompt %d / generated %d (cumulative)\n",
				m.Model, m.Prompt.Cumulative, m.Predicted.Cumulative)
		}
	}

	// Coding-agent block: rendered ONLY when the agent is
	// enabled (r.Coding != nil — the section is gated on cfg.AgentEnabled in the
	// core). Each row degrades honestly: version/model/mode are omitted when their
	// cfg field is unset; pin is the tri-state ("match"/"mismatch"/"unknown");
	// residency is OMITTED when "" (typed-Unknown — never a guessed swap/shared);
	// per-model coder usage is selected from r.Usage.Models keyed on the coder model
	// id (honest empty state, never a fabricated 0); cache effectiveness shows the
	// pct + raw ratio ONLY when proven, else "unavailable" — never a fabricated 0%.
	if r.Coding != nil {
		c := r.Coding
		if c.Version != "" {
			fmt.Fprintf(tw, "agent version\t%s\n", c.Version)
		}
		fmt.Fprintf(tw, "agent pin\t%s\n", c.PinMatch)
		if c.Model != "" {
			fmt.Fprintf(tw, "agent model\t%s\n", c.Model)
		}
		if c.Mode != "" {
			fmt.Fprintf(tw, "agent mode\t%s\n", c.Mode)
		}
		// Residency is omitted when typed-Unknown (recomputed-from-envelope "")
		// never rendered as a guessed swap/shared.
		if c.Residency != "" {
			fmt.Fprintf(tw, "agent residency\t%s\n", c.Residency)
		}
		// Per-model coder usage: select the coder model's cumulative
		// totals out of the per-model usage store; honest empty state when absent.
		if r.Usage != nil && c.Model != "" {
			if m, ok := r.Usage.Models[c.Model]; ok {
				fmt.Fprintf(tw, "agent usage %s\tprompt %d / generated %d (cumulative)\n",
					m.Model, m.Prompt.Cumulative, m.Predicted.Cumulative)
			}
		}
		// Cache effectiveness: pct + raw ONLY when proven, else
		// "unavailable" — never a fabricated 0%.
		if c.CacheEffectivenessPct != nil {
			fmt.Fprintf(tw, "agent cache\t%.1f%% (%d/%d)\n", *c.CacheEffectivenessPct, c.CacheN, c.PromptN)
		} else {
			fmt.Fprintf(tw, "agent cache\tunavailable\n")
		}
	}

	fmt.Fprintf(tw, "\nSERVICE\tACTIVE\tHEALTH\tOFFLOAD\n")
	for _, s := range r.Services {
		// A service with no GPU offload (OffloadApplies=false, e.g. Open WebUI)
		// carries a typed N/A Verdict that is EXCLUDED from the worst-wins fold
		// Render it as "N/A" rather than leaking the underlying WARN-typed
		// verdict. The --json contract keeps the full Verdict + OffloadApplies.
		offloadCell := "N/A"
		if s.OffloadApplies {
			offloadCell = s.Offload.Status.String()
		}
		fmt.Fprintf(tw, "%s\t%s\t%s\t%s\n", s.Service, s.Active, s.Health, offloadCell)
	}
	fmt.Fprintf(tw, "\nloopback-only\t%s\n", strconv.FormatBool(r.LoopbackOnly))
	for _, p := range r.Ports {
		mark := "loopback"
		if !p.Loopback {
			mark = "EXPOSED (not loopback)"
		}
		fmt.Fprintf(tw, "  port %s\t%s:%s\n", p.ContainerPort, p.HostAddr, mark)
	}
	fmt.Fprintf(tw, "telemetry\t%s\n", r.NoTelemetry)
	// Update check: the LAST RECORDED one. Rendering it here triggers nothing —
	// status is polled by the dashboard, so a live check would be network access on
	// a UI refresh loop.
	//
	// Never-checked is its OWN line, not "0 available". A host that has never asked
	// and a host that asked and found nothing are different facts, and printing them
	// the same way is exactly the "you are up to date" misreading the update verb
	// exists to refuse. The AGE is always shown, even when fresh, because villa
	// traded automation away for honest staleness and this is where that shows up.
	fmt.Fprintf(tw, "updates\t%s\n", updatesLine(r.Updates))
	if withProvenance {
		for _, s := range r.Services {
			fmt.Fprintf(tw, "\n%s offload\t%s\n", s.Service, s.Offload.Detail)
			fmt.Fprintf(tw, "%s provenance\t%s\n", s.Service, s.Offload.Provenance)
			if s.Offload.Remediation != "" {
				fmt.Fprintf(tw, "%s remediation\t%s\n", s.Service, s.Offload.Remediation)
			}
		}
	}
	_ = tw.Flush()
}

// liveStatusDeps wires status.Deps to the real host: config.LoadVilla, the
// orchestrate render + systemd is-active/journald seam, the inference unit's /health,
// /props and /metrics reads through the authenticated inference client (ADR-0014),
// the live GTT reader, and the host probe every host-derived figure is computed
// from. It is replaced wholesale by stubs in status_test.go.
//
// Every seam is wired whatever the config says, and none captures it (ADR-0016):
// a seam that needs the config is handed the one status.Run loaded, and builds the
// inference client from it. The dashboard wires these once, at startup, so a seam
// that captured the config would answer every later poll with the agent gate and
// the api key of that moment (#253). The config is loaded here only to refuse, at
// wiring, a backend that cannot resolve.
func liveStatusDeps() (*status.Deps, error) {
	// Resolve the backend from config (fail-closed): an unknown backend string is an
	// error here, never a silent default.
	cfg, err := config.LoadVilla()
	if err != nil {
		return nil, fmt.Errorf("load config: %w", err)
	}
	if _, err := inference.BackendFor(cfg.Backend); err != nil {
		return nil, fmt.Errorf("resolve backend: %w", err)
	}
	sys := orchestrate.NewSystemd()
	return &status.Deps{
		LoadConfig:    config.LoadVilla,
		ModelFile:     liveModelFile,
		ResidentUnits: liveResidentUnits,
		ImageServe:    liveImageServe,
		ModelsDir:     modelsDir,
		Render:        livePinnedRender,
		// The run's one host reading: ROCm readiness, the weight footprint and the
		// agent's residency are all computed from it, never re-probed.
		Probe: detect.Probe,
		// READ-ONLY, and deliberately so: this surfaces the last recorded update
		// check and must never trigger a live one. status is polled by the
		// dashboard, so a fetch here would be network access on a UI refresh loop.
		ReadPinState: func() *pinstate.State {
			st, err := pinstate.Load(livePinStateDeps())
			if err != nil {
				return nil // unreadable ⇒ never-checked, never a fabricated count
			}
			return &st
		},
		IsActive: sys.IsActive,
		// ResidencyJournal (not JournalText) — the offload assert needs the CURRENT
		// invocation's startup, where the load_tensors residency line lives; the
		// whole-unit journal's oldest bytes are stale prior-start output (F-3).
		JournalText: sys.ResidencyJournal,
		Props: func(cfg config.VillaConfig) *inference.PropsInfo {
			return inferenceClient(cfg).Props(context.Background())
		},
		GTTUsed:     detect.GTTUsedBytes,
		WeightBytes: weightBytes,
		// The stack's services, as ONE list. Unit names are derived from the
		// orchestrate accessors via the same .container → .service derivation doctor
		// uses, never a typed service-name literal (the seam gate walks cmd/villa).
		//
		// Every managed service gets its OWN probe. Borrowing the inference
		// endpoint's health probe for a managed service was a real false-green: a
		// healthy chat model made a down vector store read as ready.
		Services: liveStatusServices(),
		// Live tok/s: the SAME /metrics + /slots reads the dashboard's Performance
		// panel uses — no new scraper. nil on a failed/absent /metrics scrape or an
		// idle server, so the figure is omitted (typed-Unknown), NEVER a fabricated 0.
		GenTokensPerSec: liveGenTokensPerSec,
		// Cumulative usage: READ-ONLY load of usage.json. The CLI is
		// one-shot and NEVER writes the store (the dashboard, Plan 04, is the sole
		// writer); nil on an absent/empty store so the figure is omitted.
		ReadUsage:       liveReadUsage,
		ReadRecallState: liveReadRecallState,
		ReadVerifyState: liveReadVerifyState,
		// Last task (spec v1.11 §10): READ-ONLY, over the same taskstore root
		// `villa work`/`villa task` use. The core consults this ONLY when the
		// workspace agent is enabled (subsystem.SandboxOn).
		ReadLastTask: func() *status.LastTaskInfo { return readLastTask(pathsafe.DataRoot()) },
		// Coding-agent seams: consulted ONLY when the config the run loaded has the
		// agent on, which Run decides. With it off the section is omitted from --json.
		AgentPinMatch:  liveAgentPinMatch,
		AgentResidency: liveAgentResidency,
		AgentCache:     liveAgentCache,
	}, nil
}

// readLastTask projects the taskstore's newest-by-id record into the status
// core's LastTaskInfo (spec v1.11 §10). internal/status must not import
// internal/taskstore, so the projection lives here rather than in the core. A
// store read error or an empty task list yields nil — typed-Unknown, never a
// fabricated record. Task ids are the time-ordered "20060102-150405-xxxx" string
// (taskstore.NewID); List already returns them in that order, but the newest is
// picked by an explicit max(id) so the ordering rule is stated, not assumed.
func readLastTask(root string) *status.LastTaskInfo {
	tasks, err := taskstore.New(root).List()
	if err != nil || len(tasks) == 0 {
		return nil
	}
	newest := tasks[0]
	for _, t := range tasks[1:] {
		if t.ID > newest.ID {
			newest = t
		}
	}
	return &status.LastTaskInfo{ID: newest.ID, State: string(newest.State), FinishedAt: newest.FinishedAt}
}

// liveAgentPinMatch is the tri-state policy-pin compare seam: it hashes the
// installed villa-owned Crush binary and compares it to the pinned policy
// BinarySHA256 via the SAME pure agent.DetectDrift gate `villa code` uses, mapping
// the report to "match" / "mismatch" / "unknown". Binary absent OR the policy hash
// not yet pinned → "unknown" (typed-Unknown WARN, never a false confident state).
// It consults ONLY the binary signal (drift's config branch is irrelevant to the
// pin), passing ConfigPresent=true with an empty rendered ref so no false config
// drift is computed. No backend marker literal touched (TestSeamGrepGate).
func liveAgentPinMatch() string {
	installedSHA, present, err := hashFileSHA256(agentBinPath())
	if err != nil {
		return status.PinUnknown // could not read the binary → typed-Unknown
	}
	policy := agent.LoadCrushPolicy()
	asset, ok := policy.Assets["linux/amd64"]
	if !ok {
		return status.PinUnknown
	}
	rep := agent.DetectDrift(agent.DriftInput{
		BinaryPresent:   present,
		InstalledBinSHA: installedSHA,
		PolicyBinSHA:    asset.BinarySHA256,
		// Config branch is irrelevant to the pin compare: mark present + identical
		// rendered ref so DetectDrift computes NO config drift (only the binary signal).
		ConfigPresent:  true,
		OnDiskConfig:   nil,
		RenderedConfig: nil,
	})
	switch {
	case rep.BinaryAbsent || rep.BinaryDriftUnknown:
		return status.PinUnknown // binary absent / hash not yet pinned → typed-Unknown
	case rep.BinaryDrift:
		return status.PinMismatch // confident mismatch
	default:
		return status.PinMatch // installed hash equals the pinned policy hash
	}
}

// liveAgentResidency is the DERIVED residency seam (SC1): it RECOMPUTES the
// coder fit's residency from the run's host profile, sharing weightBytes' shape
// (catalog.Load + recommend.Pick). It returns "" (typed-Unknown, the residency key
// is omitted) when the catalog load fails OR the envelope is unevaluable
// (host.UsableEnvelopeBytes.Known == false); otherwise the DERIVED
// recommend.Pick(...).Coder.Residency (the recommend.ResidencySwap/ResidencyShared
// CONSTANT — never a re-typed "swap"/"shared" literal). The residency value itself
// is NEVER read from cfg (VillaConfig has no residency field) and NEVER fabricated;
// cfg is consulted ONLY for the memory inputs (MemoryEnabled/EmbeddingModel) so the
// fit reflects the post-reservation envelope.
func liveAgentResidency(cfg config.VillaConfig, host detect.HostProfile) string {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return "" // catalog unavailable → typed-Unknown (omitted)
	}
	if !host.UsableEnvelopeBytes.Known {
		return "" // unevaluable envelope → typed-Unknown, NEVER a fabricated swap/shared
	}
	// thread the REAL memory inputs so the coder fit is computed
	// against the post-embedding-reservation envelope — matching every other live
	// caller (backend.go, dashboard.go, inference.go). A reservation-blind nil
	// would compute against the FULL un-reserved envelope and surface an
	// optimistically-wrong "shared" when the post-reservation reality is "swap" — a
	// fabricated-by-omission residency this seam's doc comment forbids.
	rec := recommend.Pick(host, cat, recommend.Overrides{},
		recommend.ReservationsFor(cfg))
	return rec.Coder.Residency
}

// liveAgentCache is the cache-effectiveness counter seam: it reads the cache pair
// through the inference client's SAME bounded /metrics scrape (no new HTTP request /
// endpoint literal), the client built from the run's config. It returns (cacheN,
// promptN, ok) with ok=true ONLY when BOTH counters are Known; an absent/unparseable
// scrape or either counter Unknown → ok=false so the surface degrades typed-Unknown
// (gray badge / "unavailable" — never a fabricated 0%). The Plan-03 ratio gate
// (promptN>0) lives in the status core's codingInfo populator.
func liveAgentCache(cfg config.VillaConfig) (uint64, uint64, bool) {
	sample, ok := inferenceClient(cfg).CacheCounters(context.Background())
	if !ok || !sample.CacheKnown || !sample.PromptKnown {
		return 0, 0, false // typed-Unknown, never a fabricated 0%
	}
	return sample.CacheN, sample.PromptN, true
}

// storeReader is the ReadAll seam every on-disk store is wired with: the file's
// bytes, or (nil, nil) when it does not exist yet. The absent case is not an error
// because every store's Load reads it as its own empty state (nothing indexed,
// never checked, no reports), and a real read error stays an error. Seven wirings
// used to spell this out, each in its own closure.
func storeReader(path string) func() ([]byte, error) {
	return func() ([]byte, error) {
		b, err := os.ReadFile(path)
		if errors.Is(err, fs.ErrNotExist) {
			return nil, nil
		}
		return b, err
	}
}

// liveReadUsage loads the cumulative-usage store READ-ONLY: it wires a
// usage.Deps whose ReadAll is storeReader over usage.Path() (so a not-yet-created
// store fails closed to empty in usage.Load) and supplies NO WriteAll seam — the
// CLI status path can never write usage.json (the dashboard, Plan 04, is the sole
// writer). It returns a *usage.Totals only when the store holds at least one model
// entry; an absent/empty/corrupt store yields nil so the Report omits the usage key
// (typed-Unknown, never a fabricated 0).
func liveReadUsage() *usage.Totals {
	totals, err := usage.Load(usage.Deps{ReadAll: storeReader(usage.Path())})
	if err != nil {
		return nil // unreadable store → typed-Unknown (omitted), never a fabricated 0
	}
	if len(totals.Models) == 0 {
		return nil // empty store ⇒ omit the usage key
	}
	return &totals
}

// --- Phase-23 memory-service health probes ---

// memoryHealthTTL bounds how often the memory-service health pair is re-probed.
// The probes are podman-run helper containers (runProbeCurl), so the dashboard's
// 2.5s status poll would otherwise spawn a container pair every poll (Pitfall 2).
// 15s is the bounded-staleness honesty tradeoff (OQ2): a stopped service shows
// within one TTL window while the poll churn is capped at one probe pair per
// window. FALLBACK: if the on-hardware proof (Plan 23-05) shows residual churn,
// raise this to 30s — a one-const change.
const memoryHealthTTL = 15 * time.Second

// memoryProbeTimeout bounds the WHOLE podman-run probe (container start + curl).
// The curl --max-time below mirrors statusHTTPTimeout for the HTTP leg; this
// parent context adds allowance for podman's own container startup.
const memoryProbeTimeout = 10 * time.Second

var memoryProbeExec = func(ctx context.Context, network, img string, args ...string) ([]byte, int, error) {
	return probeCurl(ctx, network, img, nil, args)
}

// statusProber is the in-network HTTP-code prober for the status rows on network:
// the typed-Unknown mapping doctrine lives in internal/inprobe (stated once, tested
// there); this binds it to the live exec seam, the orchestrate helper-image
// accessor (no re-typed literal), and the status timeouts.
func statusProber(network string) inprobe.Prober {
	return inprobe.Prober{
		Exec: func(ctx context.Context, img string, args ...string) ([]byte, int, error) {
			return memoryProbeExec(ctx, network, img, args...)
		},
		Image:   func() string { return orchestrate.EmbedImage() }, // probe helper, never a pin (spec §7.1)
		Timeout: memoryProbeTimeout,
		MaxTime: statusHTTPTimeout,
	}
}

// probeMemoryURL runs one bounded probe on villa-closed against url via the shared
// prober (curl writes ONLY the HTTP code; 200→ready, 503→loading — the
// inprobe.MapCoded doctrine).
func probeMemoryURL(url string) status.HealthState {
	return statusProber(closedNetwork).Coded(url)
}

// memoryHealthCache is the TTL-bounded pair cache (OQ2): one refresh probes BOTH
// services together so the dashboard poll spawns at most one probe pair per
// memoryHealthTTL window (the inprobe.PairCache discipline).
var memoryHealthCache = &inprobe.PairCache{TTL: memoryHealthTTL}

// memoryHealthSnapshot returns the cached (qdrant, embed) health pair,
// refreshing BOTH probes together when the TTL window has lapsed.
func memoryHealthSnapshot(qAddr string, qPort int, eAddr string, ePort int) (status.HealthState, status.HealthState) {
	return memoryHealthCache.Pair(func() (status.HealthState, status.HealthState) {
		return probeMemoryURL("http://" + net.JoinHostPort(qAddr, strconv.Itoa(qPort)) + "/readyz"),
			probeMemoryURL("http://" + net.JoinHostPort(eAddr, strconv.Itoa(ePort)) + "/health")
	})
}

// liveQdrantHealth probes the Qdrant /readyz endpoint in-network (TTL-cached
// pair refresh). The sibling embed target is the in-network constant, so one
// refresh covers both rows.
func liveQdrantHealth(addr string, port int) status.HealthState {
	q, _ := memoryHealthSnapshot(addr, port, config.EmbedAddr, config.EmbedPort)
	return q
}

// liveEmbedHealth probes the embed llama-server's /health endpoint
// in-network (TTL-cached pair refresh) with the 200→ready / 503→loading
// mapping of liveHealthProbe.
func liveEmbedHealth(addr string, port int) status.HealthState {
	_, e := memoryHealthSnapshot(config.QdrantAddr, config.QdrantPort, addr, port)
	return e
}

// rerankHealthCache bounds the reranker probe to one per memoryHealthTTL window,
// like the pair cache does for its two siblings.
var rerankHealthCache = &inprobe.Cache{TTL: memoryHealthTTL}

// liveRerankHealth probes the reranker llama-server's /health endpoint in-network
// with the same coded mapping as the embedder's.
func liveRerankHealth(addr string, port int) status.HealthState {
	return rerankHealthCache.Get(func() status.HealthState {
		return probeMemoryURL("http://" + net.JoinHostPort(addr, strconv.Itoa(port)) + "/health")
	})
}

// extractHealthCache bounds the extractor probe to one per memoryHealthTTL window,
// like the reranker's.
var extractHealthCache = &inprobe.Cache{TTL: memoryHealthTTL}

// liveExtractHealth probes the Tika server's /tika banner in-network with the same
// coded mapping as the reranker's (ADR-0033).
func liveExtractHealth(addr string, port int) status.HealthState {
	return extractHealthCache.Get(func() status.HealthState {
		return probeMemoryURL("http://" + net.JoinHostPort(addr, strconv.Itoa(port)) + "/tika")
	})
}

// liveReadRecallState loads recall-state.json READ-ONLY through the shared
// liveRecallStateLoad (recall.go): an absent store yields a pointer to the ZERO
// State — "no index yet" is a CONFIDENT empty, not nil (status renders "empty"); a
// corrupt/future-schema store fails closed to empty inside recall.Load (never a
// fabricated count); a read error other than NotExist yields nil so status renders
// "unknown" (typed-Unknown). It supplies NO WriteAll seam — the status path can
// never write the store.
func liveReadRecallState() *recall.State {
	st, err := liveRecallStateLoad()
	if err != nil {
		return nil // unreadable store → typed-Unknown ("unknown"), never fabricated
	}
	return &st
}

// Package-level web-search-health pair cache: mirrors the memory-health pair
// (the inprobe.PairCache discipline) so one refresh probes BOTH services
// together per memoryHealthTTL window.
var webSearchHealthCache = &inprobe.PairCache{TTL: memoryHealthTTL}

// webSearchHealthSnapshot returns the cached (searxng, websafe) health pair,
// refreshing BOTH probes together when the TTL window has lapsed. SearXNG exposes a
// standard /healthz (200→ready); the villa-websafe loader serves only its single
// POST /load route, so a liveness GET is mapped via inprobe.MapLiveness (any HTTP
// response = up; connect refused = down) — never the searxng 200-only mapping that
// would mis-read websafe's 401/400/405 as down.
func webSearchHealthSnapshot(sxAddr string, sxPort int, wsAddr string, wsPort int) (status.HealthState, status.HealthState) {
	return webSearchHealthCache.Pair(func() (status.HealthState, status.HealthState) {
		return statusProber(routedNetwork).Coded("http://" + net.JoinHostPort(sxAddr, strconv.Itoa(sxPort)) + "/healthz"),
			probeWebsafeURL("http://" + net.JoinHostPort(wsAddr, strconv.Itoa(wsPort)) + "/load")
	})
}

// liveSearxngHealth probes the SearXNG /healthz endpoint in-network (TTL-cached pair
// refresh). The sibling websafe target is the in-network constant, so one refresh
// covers both rows.
func liveSearxngHealth(addr string, port int) status.HealthState {
	sx, _ := webSearchHealthSnapshot(addr, port, config.WebsafeAddr, config.WebsafePort)
	return sx
}

// liveWebsafeHealth probes the villa-websafe loader in-network (TTL-cached pair
// refresh) via mapWebsafeProbe (any HTTP response = up; the loader has no dedicated
// health route, only POST /load).
func liveWebsafeHealth(addr string, port int) status.HealthState {
	_, ws := webSearchHealthSnapshot(config.SearxngAddr, config.SearxngPort, addr, port)
	return ws
}

// probeWebsafeURL runs one bounded in-network liveness probe against the villa-websafe
// loader. Because the loader exposes ONLY a POST /load route (no /healthz), a GET
// elicits a 401/400/405 — all of which prove the server is UP. inprobe.MapLiveness
// treats ANY HTTP code curl wrote as "ready" and only a curl-/podman-level failure as
// down/unknown — never the searxng 200-only mapping (which would false-negative every
// healthy websafe).
func probeWebsafeURL(url string) status.HealthState {
	return statusProber(routedNetwork).Liveness(url)
}

// inferproxyHealthCache bounds the villa-inferproxy probe to one per memoryHealthTTL
// window.
var inferproxyHealthCache = &inprobe.Cache{TTL: memoryHealthTTL}

// liveInferproxyHealth is a liveness probe of villa-inferproxy's root from
// villa.network. The root is off the proxy's allowlist, so the proxy answers 403
// itself: the probe proves the listener is up without reaching villa-llama.
func liveInferproxyHealth() status.HealthState {
	return inferproxyHealthCache.Get(func() status.HealthState {
		return statusProber(routedNetwork).Liveness("http://" + net.JoinHostPort(config.InferproxyAddr, strconv.Itoa(config.InferproxyPort)) + "/")
	})
}

// voiceHealthCache refreshes villa-stt and villa-tts together, the memory pair's
// discipline.
var voiceHealthCache = &inprobe.PairCache{TTL: memoryHealthTTL}

func voiceHealthSnapshot() (status.HealthState, status.HealthState) {
	return voiceHealthCache.Pair(func() (status.HealthState, status.HealthState) {
		return probeMemoryURL(voice.STT.HealthURL()), probeMemoryURL(voice.TTS.HealthURL())
	})
}

func liveSttHealth() status.HealthState {
	stt, _ := voiceHealthSnapshot()
	return stt
}

func liveTtsHealth() status.HealthState {
	_, tts := voiceHealthSnapshot()
	return tts
}

// liveReadVerifyState loads verify-search-state.json READ-ONLY,
// cloning liveReadRecallState's shape over verifystate.Load (fail-closed): an absent
// store yields a pointer to the ZERO State (the status core's freshness gate then reads
// it as a non-PASS/empty → "unknown", never green); a corrupt/future-schema store fails
// closed to empty inside verifystate.Load (never a fabricated PASS); a read error other
// than NotExist yields nil so the indicator stays "unknown" (typed-Unknown). It supplies
// NO WriteAll seam — the status path can never write the store.
func liveReadVerifyState() *verifystate.State {
	st, err := verifystate.Load(verifystate.Deps{ReadAll: storeReader(verifystate.Path())})
	if err != nil {
		return nil // unreadable store → typed-Unknown ("unknown"), never fabricated
	}
	return &st
}

// liveGenTokensPerSec reads the live token-generation throughput through the
// inference client's /metrics and /slots reads, the ones the dashboard's
// Performance panel uses, folded by metrics.IsGenerating; the client is built from
// the run's config. It returns nil — a typed-Unknown the Report omits — on a
// failed/absent /metrics scrape (404/401/transport) OR when the server is idle (the
// gauges are stale snapshots when !IsGenerating), so the surface NEVER shows a
// fabricated 0 tok/s. The reads inherit the client's time and body bounds (no new
// attack surface).
func liveGenTokensPerSec(cfg config.VillaConfig) *float64 {
	ctx := context.Background()
	inf := inferenceClient(cfg)
	snap, ok := inf.Perf(ctx)
	if !ok {
		return nil // /metrics 404 or transport error → typed-Unknown (omitted)
	}
	slots, _ := inf.Slots(ctx)
	if !metrics.IsGenerating(snap, slots) {
		return nil // idle: gauges are stale snapshots → omit, never a fabricated 0
	}
	v := snap.GenTokensPerSec
	return &v
}

// unansweredHealth is what each direct-HTTP health probe reports when its request
// got no answer: refused, reset, timed out, or refused by the inference client
// itself before sending (a key with a control character, ADR-0014). The three are
// decided here, side by side, because the choice differs by what was asked, and
// deliberately:
//
//   - the inference unit and the dashboard are asked on their OWN loopback port, so
//     no answer is a confident fact about that service: down, which fails status.
//   - the chat UI is asked THROUGH llama-server's model list (no WEBUI_AUTH
//     friction), so no answer is a fact about llama-server, not about Open WebUI: a
//     typed Unknown, which warns, never a down the chat UI did not earn.
//
// The in-network probes (vector store, embedder, metasearch, loader) map theirs in
// internal/inprobe, where a probe that could not run is Unknown and one that ran
// and found nothing listening is down.
var unansweredHealth = map[string]status.HealthState{
	installServiceName:               status.HealthDown,
	openWebUIServiceName:             status.HealthUnknown,
	orchestrate.DashboardServiceName: status.HealthDown,
}

// liveDashboardHealth maps a single bounded GET to the dashboard's own /api/healthz,
// on the loopback port of the run's config, to a status.HealthState: 200 → ready,
// any other code → loading (up but not confirmed), no answer → unansweredHealth (the
// wedged-dashboard case). The probe is bounded by statusHTTPTimeout + io.LimitReader
// so a wedged dashboard can NEVER hang `villa status` despite the benign
// self-recursion (Pitfall 6). Note the path is /api/healthz (the route Plan 02
// mounted under /api), not a top-level /healthz.
func liveDashboardHealth(cfg config.VillaConfig) status.HealthState {
	client := &http.Client{Timeout: statusHTTPTimeout}
	addr := "http://" + net.JoinHostPort(config.DashboardAddr, strconv.Itoa(cfg.DashboardPort))
	resp, err := client.Get(addr + "/api/healthz")
	if err != nil {
		return unansweredHealth[orchestrate.DashboardServiceName]
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 8<<10))
	if resp.StatusCode == http.StatusOK {
		return status.HealthReady
	}
	return status.HealthLoading
}

// liveHealthProbe maps a single /health GET through the inference client, built
// from the run's config, to a status.HealthState: 200→ready, 503→loading (up but
// not ready — Unknown, never down), any other code → down, no answer →
// unansweredHealth. The client bounds the time and the body.
func liveHealthProbe(cfg config.VillaConfig) status.HealthState {
	code, err := inferenceClient(cfg).Health(context.Background())
	if err != nil {
		return unansweredHealth[installServiceName]
	}
	switch code {
	case http.StatusOK:
		return status.HealthReady
	case http.StatusServiceUnavailable:
		return status.HealthLoading
	default:
		return status.HealthDown
	}
}

// liveOpenWebUIHealth maps the Open WebUI row's health to a status.HealthState by
// reading the UPSTREAM llama-server model list through the inference client built
// from the run's config (no WEBUI_AUTH friction, RESEARCH A3): a non-empty list →
// HealthReady (CHAT-01); an empty list / non-200 / unparseable body → HealthLoading
// (up but not ready → WARN, never a false PASS); no answer → unansweredHealth
// (typed-Unknown → WARN).
func liveOpenWebUIHealth(cfg config.VillaConfig) status.HealthState {
	n, reached := inferenceClient(cfg).Models(context.Background())
	if !reached {
		return unansweredHealth[openWebUIServiceName]
	}
	if n == 0 {
		return status.HealthLoading // up but not serving a model list yet → WARN (no false PASS)
	}
	return status.HealthReady // non-empty model list → Open WebUI reaches a model (PASS)
}

// weightBytes derives the configured model's expected weight footprint on the host
// it is given from the recommend fit math (the GTT-floor reference), PLUS the draft
// sidecar's own weight when the resolved pick carries one: the draft loads onto the
// device as a second model, so the band it is checked against must include it
// (ADR-0009). An undeterminable envelope yields 0 (the GTT floor then degrades to a
// typed-Unknown WARN, never a false PASS).
func weightBytes(cfg config.VillaConfig, host detect.HostProfile) uint64 {
	cat, _, err := catalog.Load(modelCatalogPath)
	if err != nil {
		return 0
	}
	// No reservations ON PURPOSE: this provably keeps status.json.golden
	// byte-identical — WeightBytes is envelope-independent for overrides (guarded
	// by TestPickOverrideWeightInvariance), so the frozen status path never sees
	// a reservation.
	rec := recommend.Pick(host, cat, recommend.Overrides{Model: cfg.Model, Speculation: cfg.Speculation}, nil)
	return rec.WeightBytes + rec.DraftBytes
}

// liveWeightBytes is weightBytes on a fresh host reading, for the callers outside a
// status run: backend.go's liveProve and bench.go's liveMeasure source their own
// residency bands through it, so folding the draft into weightBytes folds it in for
// every caller at once.
func liveWeightBytes(cfg config.VillaConfig) uint64 { return weightBytes(cfg, detect.Probe()) }

// liveStatusServices is the stack's services as ONE list: the inference service,
// the chat UI, the four optional-subsystem services, and the dashboard.
//
// This replaced seven same-shaped health probes and the six service names they
// belonged to, spread across the status seam struct. Adding a subsystem to status
// is now one entry here instead of a name, a probe and a report branch across three
// files.
//
// Unit names come from the orchestrate accessors via the same .container → .service
// derivation doctor uses, so no service-name literal is typed in the command tier.
//
// Every managed service carries its OWN probe. That is not stylistic: borrowing the
// inference endpoint's health probe for a managed service was a real false-green,
// where a healthy chat model made a down vector store read as ready.
func liveStatusServices() []status.Service {
	return []status.Service{
		{
			// The inference service is the ONLY one whose offload verdict folds into
			// the overall status: it is the only one running the model.
			Unit:  installServiceName,
			Kind:  status.Inference,
			Probe: liveHealthProbe,
		},
		{
			// The chat UI's health is reachability AND a non-empty upstream model
			// list. Reachability alone would report ready while it could not reach a
			// model at all.
			Unit:  openWebUIServiceName,
			Kind:  status.Managed,
			Probe: liveOpenWebUIHealth,
		},
		{
			Unit: unitServiceName(orchestrate.QdrantContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveQdrantHealth(config.QdrantAddr, config.QdrantPort)
			},
		},
		{
			Unit: unitServiceName(orchestrate.EmbedContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveEmbedHealth(config.EmbedAddr, config.EmbedPort)
			},
		},
		{
			Unit: unitServiceName(orchestrate.RerankContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveRerankHealth(config.RerankAddr, config.RerankPort)
			},
		},
		{
			Unit: unitServiceName(orchestrate.ExtractContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveExtractHealth(config.ExtractAddr, config.ExtractPort)
			},
		},
		{
			Unit: unitServiceName(orchestrate.SearXNGContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveSearxngHealth(config.SearxngAddr, config.SearxngPort)
			},
		},
		{
			Unit: unitServiceName(orchestrate.WebsafeContainerUnitName()),
			Kind: status.Managed,
			Probe: func(config.VillaConfig) status.HealthState {
				return liveWebsafeHealth(config.WebsafeAddr, config.WebsafePort)
			},
		},
		{
			Unit:  unitServiceName(orchestrate.InferproxyContainerUnitName()),
			Kind:  status.Managed,
			Probe: func(config.VillaConfig) status.HealthState { return liveInferproxyHealth() },
		},
		{
			Unit:  unitServiceName(orchestrate.STTContainerUnitName()),
			Kind:  status.Managed,
			Probe: func(config.VillaConfig) status.HealthState { return liveSttHealth() },
		},
		{
			Unit:  unitServiceName(orchestrate.TTSContainerUnitName()),
			Kind:  status.Managed,
			Probe: func(config.VillaConfig) status.HealthState { return liveTtsHealth() },
		},
		{
			// The image server's health is sd-server's cheap readiness GET; whether
			// its params sit on the GPU is doctor's IMG-DOC-residency, which needs a
			// generation a status run must not drive.
			Unit:  unitServiceName(orchestrate.ImageContainerUnitName()),
			Kind:  status.Managed,
			Probe: liveImageHealth,
		},
		{
			// The dashboard is a native systemd --user service, not a Quadlet
			// container, so it never appears in the rendered units and needs an
			// explicit row. Its probe is a bounded GET to its own /api/healthz, which
			// is a benign self-recursion the timeout keeps from hanging status.
			Unit:      orchestrate.DashboardServiceName,
			Kind:      status.Managed,
			AlwaysRow: true,
			Probe:     liveDashboardHealth,
		},
	}
}

// updatesLine renders the passive update-check surface.
//
// Three distinct readings, because they are three distinct facts:
//
//	never checked                        villa has never asked
//	last checked 3 days ago              villa asked recently
//	last checked 146 days ago — run …    villa asked, but long enough ago to matter
//
// The staleness nudge appears past updateStaleDays because at that point the recorded
// answer is not evidence about today. It is a nudge and not a warning: nothing is
// wrong with a host that has not checked, and treating staleness as a fault would
// pressure users toward the automatic checking villa deliberately does not do.
func updatesLine(u status.UpdatesInfo) string {
	if u.State != status.UpdatesChecked || u.AgeDays == nil {
		return "never checked — run `villa update --check`"
	}
	days := *u.AgeDays
	unit := "days"
	if days == 1 {
		unit = "day"
	}
	line := fmt.Sprintf("last checked %d %s ago", days, unit)
	if days == 0 {
		line = "last checked today"
	}
	if days >= status.UpdateStaleDays {
		line += " — run `villa update --check`"
	}
	return line
}
