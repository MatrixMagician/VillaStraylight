// Package status is the extracted `villa status` read-model core: the
// JSON-neutral aggregation that turns the injected host seams into a frozen
// StatusReport (here exported as Report) and the worst-wins overall Verdict.
//
// It was moved VERBATIM out of cmd/villa/status.go (Pitfall 1: JSON-neutral move)
// so the Phase-5 dashboard backend can call the SAME logic the CLI does, not a
// fork. Every json:"..." tag and field order is preserved byte-for-byte; the
// byte-frozen `status --json` golden in cmd/villa stays green with zero edits.
//
// All host-touching actions are injected via Deps so both the cobra caller
// (cmd/villa) and the dashboard handler can drive it with their own live wiring;
// internal/status itself stays free of http/journald/systemd coupling.
package status

import (
	"errors"
	"strings"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/agent"
	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/subsystem"
	"github.com/MatrixMagician/VillaStraylight/internal/usage"
	"github.com/MatrixMagician/VillaStraylight/internal/verifystate"
)

// noTelemetryStatement is the assertion the report always carries.
const noTelemetryStatement = "no telemetry; outbound = image/model pulls only"

// activeErrored is the synthetic active-state set when `systemctl is-active` ran but
// errored with no parseable state (orchestrate.ErrCommandFailed). It is distinct from
// the empty "" (clean-but-silent → WARN) and "unknown" (cannot measure → WARN) tokens
// so an indeterminate-but-bad unit drives FAIL (tighten).
const activeErrored = "errored"

// HealthState is the mapped container-health outcome: 200→ready, 503→loading
// (Unknown, NOT down), transport error→down.
type HealthState string

const (
	HealthReady   HealthState = "ready"   // /health 200
	HealthLoading HealthState = "loading" // /health 503 — up but still loading (Unknown)
	HealthDown    HealthState = "down"    // transport error / unreachable
	HealthUnknown HealthState = "unknown" // could not probe (no endpoint)
)

// ServiceStatus is the per-service aggregate row in the report.
type ServiceStatus struct {
	Service string      `json:"service"`
	Active  string      `json:"active"` // systemctl is-active state
	Health  HealthState `json:"health"` // mapped /health (or owui /v1/models reachability)

	// Offload is the running-server GPU-offload Verdict. It is only meaningful for the
	// inference service; for non-GPU managed services (Open WebUI) it is an N/A
	// representation that does NOT fold into the overall verdict (OffloadApplies=false).
	Offload inference.Verdict `json:"offload"`
	// OffloadApplies marks whether the offload Verdict is a real GPU-offload assertion
	// (inference) versus an N/A placeholder (a non-GPU service). Aggregate folds
	// the offload Status ONLY when this is true, so a non-GPU service can never record a
	// spurious offload PASS nor a false offload FAIL.
	OffloadApplies bool `json:"offload_applies"`
	// OffloadOK is a convenience: a proven offload PASS. Always false when offload is
	// N/A (a non-GPU service is never "offload OK").
	OffloadOK bool `json:"offload_ok"`
}

// naOffloadVerdict is the typed-Unknown / N-A offload representation for a non-GPU
// managed service (Open WebUI). It is deliberately a WARN-typed Verdict (uncertainty,
// never a false PASS) but is EXCLUDED from the worst-wins fold via OffloadApplies=false,
// so it neither bumps the overall verdict to a spurious PASS nor FAILs a service that
// legitimately has no GPU offload.
func naOffloadVerdict() inference.Verdict {
	return inference.Verdict{
		Status:     inference.StatusWarn,
		Detail:     "N/A — this service has no GPU offload",
		Provenance: "not an inference service (no llama-server residency to assert)",
	}
}

// PortBinding is one published port and its host bind address (privacy posture).
type PortBinding struct {
	HostAddr      string `json:"host_addr"`
	ContainerPort string `json:"container_port"`
	Loopback      bool   `json:"loopback"`
}

// Report is the aggregated `--json` contract (the Phase-5 dashboard
// struct). Its shape is frozen by a golden test, like HostProfile/Recommendation.
// (Moved verbatim from cmd/villa/status.go StatusReport — the Go type name changed
// only; every json tag and field order is preserved byte-for-byte, Pitfall 1.)
type Report struct {
	Services []ServiceStatus `json:"services"`

	// Privacy posture, sourced from the generated PublishPort.
	Ports        []PortBinding `json:"ports"`
	LoopbackOnly bool          `json:"loopback_only"`
	NoTelemetry  string        `json:"no_telemetry"`

	// Overall is the aggregated PASS/WARN/FAIL across every service offload + health.
	Overall string `json:"overall"`

	// Backend is the active inference backend's short identifier (e.g. "vulkan"),
	// and Image its digest-pinned container image — sourced from the RESOLVED
	// inference.Backend (BackendFor(cfg.Backend)), never a literal. This is
	// the single authoritative active-backend surface both `villa status` and the
	// dashboard /api/status read. Appended at the tail — nothing above moved.
	Backend string `json:"backend"`
	Image   string `json:"image"`

	// GenTokensPerSec is the live token-generation throughput
	// (llamacpp:predicted_tokens_seconds) for the ACTIVE backend, populated ONLY
	// while the server is generating (metrics.IsGenerating). It is *float64 +
	// omitempty so an idle snapshot or a failed/absent /metrics scrape omits it
	// entirely — a typed-Unknown, NEVER a fabricated 0.0 (no-false-green).
	GenTokensPerSec *float64 `json:"gen_tokens_per_sec,omitempty"`

	// ROCmReadiness is the tri-state indicator folded (consumed, never recomputed)
	// from the detect rocm_readiness sub-tree: any unevaluable signal yields
	// "unknown" — never a fabricated "not-ready" (no-false-green).
	ROCmReadiness ROCmReadinessIndicator `json:"rocm_readiness"`

	// Model is the active configured model identity (cfg.Model) — the key the
	// dashboard uses to select the CURRENT model's cumulative usage row out of the
	// per-model Usage store. Sourced from the SAME cfg that sources
	// Backend/Image; it is the single authoritative active-model surface both
	// `villa status` and the dashboard /api/status read. It is omitempty so an unset
	// model omits the key entirely — a typed-Unknown, never a fabricated identity.
	// Tail-appended above SchemaVersion (append-only; nothing above moved). Part of
	// the same Phase-15 v2 contract delta as Usage — no further schema bump.
	Model string `json:"model,omitempty"`

	// Usage is the cumulative per-model token totals read (read-only) from the usage
	// store (usage.json) — the Phase-15 surface. It is a
	// *usage.UsageTotals + omitempty so an absent/empty store OMITS the key entirely:
	// a typed-Unknown, NEVER a fabricated 0 total. The CLI populates it via a
	// read-only ReadUsage seam (usage.Load only — the CLI never writes the store);
	// the dashboard (Plan 04) reads the SAME field through handleStatus, no new endpoint
	// Tail-appended above SchemaVersion (append-only; nothing above moved).
	Usage *usage.UsageTotals `json:"usage,omitempty"`

	// Memory is the v1.3 memory-stack summary: the active
	// embedding identity (from cfg, the single source of truth), the typed
	// recall-index summary, and the embedding-skew indicator (set ONLY on a
	// confident mismatch). It is a *MemoryInfo + omitempty (Pitfall 10:
	// a non-pointer struct with omitempty still serializes) so a memory-OFF
	// install OMITS the key entirely — with memory off the v3 contract differs
	// from v2 ONLY in schema_version. Tail-appended above SchemaVersion
	// (append-only; nothing above moved). Part of the v3 bump.
	Memory *MemoryInfo `json:"memory,omitempty"`

	// Coding is the v1.4 coding-agent summary (Phase-28..): the active
	// agent identity (version/model/mode from cfg, the single source of truth),
	// the tri-state policy-pin match, the DERIVED residency mode (recomputed at
	// status time from the live memory envelope — NEVER persisted), and the
	// cache-effectiveness ratio inputs. Like Memory it is a *CodingInfo +
	// omitempty so an agent-OFF install OMITS the key entirely — with the agent
	// off the v4 contract differs from v3 ONLY in schema_version (hidden-
	// until-data). Tail-appended above SchemaVersion (append-only; nothing above
	// moved). Part of the v4 bump — the phase's ONLY contract change.
	Coding *CodingInfo `json:"coding,omitempty"`

	// WebSearch is the v1.5 web-search summary section of the Report (Phase-34
	// the enabled state plus the outbound-bounded indicator DERIVED from
	// the cached `villa verify search` result (verifystate.State) with a freshness
	// gate — NEVER from cfg.WebSearchEnabled. Like Memory/Coding it is a
	// *WebSearchInfo + omitempty so a web-search-OFF install OMITS the key entirely
	// with web search off the v5 contract differs from v4 ONLY in schema_version
	// (the agent-off hidden-until-data precedent). The villa-searxng / villa-websafe
	// health rows surface as Services entries (dedicated in-network seams), NOT as
	// WebSearchInfo sub-fields. Tail-appended above SchemaVersion (append-only;
	// nothing above moved). Part of the v5 bump — the phase's ONLY contract change.
	WebSearch *WebSearchInfo `json:"web_search,omitempty"`

	// SchemaVersion is the Report contract self-version. It MUST stay the
	// LAST tagged field (append-only; new tagged fields go above it, the unexported
	// err stays after it and never serializes).
	SchemaVersion int `json:"schema_version"`

	// err is the unexported load/render error carried out of Run (read via Err()).
	// It has no json tag and is unexported, so encoding/json never serializes it
	// the frozen --json contract is unchanged (Pitfall 1).
	err error
}

// reportSchemaVersion is the Report contract self-version. Version 1 carried the
// Phase-10 backend-aware tail-append fields (Backend, Image, GenTokensPerSec,
// ROCmReadiness). Version 2 (Phase-15) tail-appends the cumulative usage
// field (Usage) above SchemaVersion. Version 3 (Phase-23)
// reclassifies the memory-service rows as non-GPU rows with their OWN
// per-service health (the false-green fix) and tail-appends the Memory section
// (*MemoryInfo, omitted when memory is off). Version 4 (Phase-28)
// tail-appends the coding-agent section (*CodingInfo, omitted when the agent is
// off) ABOVE SchemaVersion — the v1.4 milestone's SINGLE contract evolution;
// with the agent off the v4 output differs from v3 ONLY in schema_version. Version
// 5 (Phase-34) tail-appends the web-search section (*WebSearchInfo,
// omitted when web search is off) ABOVE SchemaVersion — the v1.5 milestone's SINGLE
// contract evolution; with web search off the v5 output differs from v4 ONLY in
// schema_version. It is itself a tail-appended additive marker; bumped on
// any additive change to the Report --json contract.
const reportSchemaVersion = 5

// VerifyFreshnessWindow bounds how recent a persisted `villa verify search` PASS must
// be to surface as the green "bounded" outbound indicator (Open Q3). A PASS
// older than this window degrades to "unknown" ("unavailable"), NEVER green: a security
// property must be re-proven, not trusted indefinitely from a stale cache.
// Defined ONCE (exported) in the status core so `villa status --json`, the dashboard,
// AND `villa doctor` all inherit the SAME freshness gate — no forked literal to drift.
const VerifyFreshnessWindow = 24 * time.Hour

// MemoryInfo is the v1.3 memory-stack summary section of the Report (Phase-23
// EmbeddingModel/EmbeddingDim are the ACTIVE configured identity
// (cfg.EmbeddingModel/EmbeddingDim — config is the single source of truth);
// RecallState is the typed recall-index summary ("unknown" — store unreadable
// or no seam; "empty" — confidently nothing indexed yet; "indexed" — a clean
// complete run; "incomplete" — a run started but never completed). The
// count/timestamp fields are populated ONLY for a complete run (omitempty drops
// them otherwise — never a fabricated count). EmbeddingSkew is set ONLY
// on a confident mismatch; a match or an unevaluated comparison leaves
// it empty/omitted — never a green "ok" for an unevaluated state.
type MemoryInfo struct {
	EmbeddingModel       string `json:"embedding_model"`
	EmbeddingDim         int    `json:"embedding_dim"`
	RecallState          string `json:"recall_state"`
	IndexedChats         int    `json:"indexed_chats,omitempty"`
	LastIndexStartedAt   string `json:"last_index_started_at,omitempty"`
	LastIndexCompletedAt string `json:"last_index_completed_at,omitempty"`
	EmbeddingSkew        string `json:"embedding_skew,omitempty"`
}

// CodingInfo is the v1.4 coding-agent summary section of the Report (Phase-28
// .). It clones the MemoryInfo sidecar idiom (snake_case + omitempty
// tails) and is built ONLY when the agent is enabled (cfg.AgentEnabled), so its
// Enabled field is always true. The field set is LOCKED:
//
//   - Version/Model/Mode are the ACTIVE configured identity from cfg (the single
//     source of truth) — omitempty drops an unset field (typed-Unknown, never a
//     fabricated identity).
//   - PinMatch is a TRI-STATE string ("match"/"mismatch"/"unknown") — NOT a bare
//     bool: the policy/binary comparison degrades to "unknown" when it cannot be
//     made (mirrors ROCmReadinessIndicator + the Memory skew idiom). The UI-SPEC
//     pin badge maps it directly. It is NOT omitempty — the tri-state always
//     surfaces so an unevaluable pin is honestly "unknown", never a silent absence.
//   - Residency ("swap"/"shared") is DERIVED (recommend.CoderFit.Residency,
//     recommend.ResidencySwap/ResidencyShared) and RECOMPUTED at status time from
//     the live memory envelope via the AgentResidency seam — NEVER read from cfg
//     (VillaConfig has no residency field) and NEVER fabricated: an unevaluable
//     envelope leaves it "" (omitted by omitempty), never a guessed swap/shared.
//   - CacheEffectivenessPct is *float64 + omitempty (nil-on-Unknown, mirroring
//     GenTokensPerSec): set ONLY when both cache_n/prompt_n are Known AND
//     prompt_n>0 — otherwise nil so the surface shows a gray Unknown badge, never
//     a fabricated 0%. CacheN/PromptN are the raw ratio inputs (omitted when
//     Unknown).
type CodingInfo struct {
	Enabled               bool     `json:"enabled"`
	Version               string   `json:"version,omitempty"`
	PinMatch              string   `json:"pin_match"`
	Model                 string   `json:"model,omitempty"`
	Mode                  string   `json:"mode,omitempty"`
	Residency             string   `json:"residency,omitempty"`
	CacheEffectivenessPct *float64 `json:"cache_effectiveness_pct,omitempty"`
	CacheN                uint64   `json:"cache_n,omitempty"`
	PromptN               uint64   `json:"prompt_n,omitempty"`
}

// WebSearchInfo is the v1.5 web-search summary section of the Report (Phase-34
// It clones the MemoryInfo/CodingInfo sidecar idiom (snake_case +
// omitempty tails) and is built ONLY when web search is enabled
// (cfg.WebSearchEnabled), so its Enabled field is always true. The field set is
// deliberately MINIMAL (the documented accepted scope limit):
//
//   - Enabled is always true (the section is built ONLY when cfg.WebSearchEnabled).
//   - OutboundBounded is a TRI-STATE string ("bounded"/"not-bounded"/"unknown")
//     DERIVED from the cached `villa verify search` result (verifystate.State) with
//     a freshness gate — green ONLY for a real RECENT PASS; a real recent non-PASS
//     verdict → "not-bounded"; a stale PASS, an absent/corrupt store, or a nil seam
//     → "unknown" (typed-Unknown "unavailable"). It is NEVER derived from
//
// cfg.WebSearchEnabled (the no-false-green spoof guard). It is NOT
//
//	  omitempty: the tri-state always surfaces so an unevaluable bound is honestly
//	  "unknown", never a silent absence.
//	- VerifyCheckedAt is the RFC3339 timestamp of the cached verify result, carried
//	  ONLY when a non-stale result exists (omitempty drops it for a stale/absent
//	  result) — never a fabricated "never".
//
// SOURCE-GAP fields (guard strip/flag counters, last_query_at, outbound-visibility
// last_query/last_fetched[]) have NO host-side persisted source today and are
// OMITTED — surfacing them would require a query/URL log that conflicts with the
// "ephemeral content excluded by design" posture. Building a
// counter/query-log pipeline is NEW behavior and OUT OF SCOPE for this surfacing
// phase. The villa-searxng / villa-websafe health rows surface as Services entries
// (their own in-network seams), NOT as WebSearchInfo sub-fields.
type WebSearchInfo struct {
	Enabled         bool   `json:"enabled"`
	OutboundBounded string `json:"outbound_bounded"`
	VerifyCheckedAt string `json:"verify_checked_at,omitempty"`
}

// Outbound-bounded tri-state tokens. "bounded" is reserved for a real
// RECENT verify-search PASS; "not-bounded" is a real recent non-PASS verdict;
// "unknown" is the typed-Unknown default (stale/absent/nil) — NEVER green by default,
// NEVER inferred from cfg.WebSearchEnabled.
const (
	OutboundBounded    = "bounded"
	OutboundNotBounded = "not-bounded"
	OutboundUnknown    = "unknown"
)

// Pin-match tri-state tokens. The comparison degrades to PinUnknown when
// the policy/binary hashes cannot be compared — never a fabricated confident
// match/mismatch (the typed-Unknown convention; the UI-SPEC pin badge maps these
// directly).
const (
	PinMatch    = "match"
	PinMismatch = "mismatch"
	PinUnknown  = "unknown"
)

// ROCmReadinessIndicator is the tri-state surfaced from the detect rocm_readiness
// sub-tree. It is a string enum so the --json contract is stable and the dashboard
// badge maps it directly.
type ROCmReadinessIndicator string

const (
	// ROCmReady means every readiness signal is Known-good.
	ROCmReady ROCmReadinessIndicator = "ready"
	// ROCmNotReady means at least one signal is Known-BAD and all others are Known
	// (a confidently-detected blocker — never inferred from an unevaluable signal).
	ROCmNotReady ROCmReadinessIndicator = "not-ready"
	// ROCmUnknown means at least one signal is unevaluable (off-hardware default).
	// Unknown wins over not-ready (no-false-green).
	ROCmUnknown ROCmReadinessIndicator = "unknown"
)

// foldROCmReadiness reads (never recomputes) the detect rocm_readiness sub-tree and
// folds it worst-wins with UNKNOWN winning over NOT-READY (no-false-green):
// a single unevaluable (Known=false) signal makes the whole indicator "unknown", so
// off-hardware (most fields unset) the honest answer is "unknown", and a
// confidently-bad signal only yields "not-ready" when every other signal is Known.
// It is pure — it reads the passed struct only, performing no I/O and no re-probe.
// Because any !Known short-circuits to "unknown", fold order is irrelevant to
// correctness: unknown can never be masked by a later not-ready.
func foldROCmReadiness(r detect.ROCmReadiness) ROCmReadinessIndicator {
	bools := []detect.Bool{
		r.HSAOverrideViable, r.FirmwareDateOK, r.KernelFloorOK,
		r.RocminfoGfx1151, r.ImagePolicyOK,
	}
	sawBad := false
	for _, b := range bools {
		if !b.Known {
			return ROCmUnknown // any unevaluable signal → unknown (never not-ready)
		}
		if !b.Value {
			sawBad = true
		}
	}
	if sawBad {
		return ROCmNotReady
	}
	return ROCmReady
}

// Deps are the injectable seams Run drives. Defaults wire the real host
// (cmd/villa liveStatusDeps); status_test.go and the dashboard replace them with
// stubs / their own live wiring.
type Deps struct {
	LoadConfig func() (config.VillaConfig, error)
	ModelFile  func(config.VillaConfig) (string, error)
	// ResidentUnits resolves each configured resident slot's catalog model id to
	// its GGUF filename. It is a seam for the same reason ModelFile is: internal/status
	// must not import internal/catalog to turn a model id into a weight file. Run
	// fails closed if cfg.Resident is non-empty and this is nil, because a silently
	// residentless render under-reports the stack.
	ResidentUnits func(config.VillaConfig) ([]orchestrate.ResidentUnit, error)
	ModelsDir     func() string
	Render        func(orchestrate.RenderInput) ([]orchestrate.Unit, error)

	IsActive    func(service string) (string, error)
	JournalText func(service string) (string, bool)
	Props       func(endpoint string) *inference.PropsInfo
	GTTUsed     func() detect.Bytes
	WeightBytes func(config.VillaConfig) uint64
	Endpoint    func() string

	// GenTokensPerSec is the live token-generation tok/s seam, wired in
	// cmd/villa liveStatusDeps to reuse metrics.ScrapeMetrics. It returns nil on an
	// idle server or a failed/absent /metrics scrape so Run omits the figure
	// (typed-Unknown, never a fabricated 0). internal/status stays free of HTTP
	// coupling; status_test.go stubs it like the other seams. A nil seam is treated
	// as "no reading" (Run guards it).
	GenTokensPerSec func(endpoint string) *float64
	// ROCmReadiness is the detect rocm_readiness probe seam, wired in
	// liveStatusDeps to detect.Probe().ROCmReadiness. internal/status folds the
	// returned sub-tree via foldROCmReadiness; a nil seam leaves the indicator
	// "unknown" (no false-green). status_test.go stubs it to drive the fold.
	ROCmReadiness func() detect.ROCmReadiness

	// ReadUsage is the READ-ONLY cumulative-usage seam, wired in
	// liveStatusDeps to a usage.Load over usage.UsagePath(). It returns the loaded
	// *usage.UsageTotals, or nil when the store is absent/empty so Run OMITS the Usage
	// field (typed-Unknown, never a fabricated 0). It MUST never write usage.json — the
	// CLI is one-shot and read-only; the dashboard (Plan 04) is the sole writer.
	// internal/status stays free of filesystem coupling; status_test.go stubs it. A nil
	// seam is treated as "no reading" (Run guards it, leaving Usage nil).
	ReadUsage func() *usage.UsageTotals

	// Services is the stack's services: which units exist, how each is probed, and
	// whether its offload counts. It replaces seven same-shaped health probes and
	// the six service names they belonged to.
	//
	// The names are Deps values, not literals here, so internal/status never
	// re-types a unit name (which would also create a cycle back to package main).
	// Required input is visible in the type: a service with no Probe reports
	// Unknown, rather than a nil field silently producing a row that looks answered.
	Services []Service

	// ReadRecallState is the READ-ONLY recall-state.json seam, wired in
	// liveStatusDeps over recall.Load (fail-closed). It returns a pointer to the
	// loaded (possibly zero/empty) State, or nil when the store is unreadable
	// Run then reports RecallState "unknown" with no fabricated counts. A nil
	// seam is treated the same (Run guards it).
	ReadRecallState func() *recall.State

	// --- Web-search seams. All nil-safe: a nil seam degrades to
	// typed-Unknown (no fabricated value), mirroring the qdrant/embed + ReadRecallState
	// contract. The service-row seams are consulted ONLY when the matching service unit
	// is rendered; ReadVerifyState is consulted ONLY when web search is enabled (Run
	// gates on cfg.WebSearchEnabled).

	// ReadVerifyState is the READ-ONLY cached verify-search-result seam,
	// wired in liveStatusDeps over verifystate.Load (fail-closed). It returns a pointer
	// to the loaded State, or nil when the store is absent/unreadable — the
	// outbound-bounded indicator then reads "unknown" (typed-Unknown), NEVER a
	// fabricated PASS. It MUST never write the store. A nil seam is treated the same
	// (Run/webSearchInfo guards it). The "bounded" verdict ALSO requires the cached
	// PASS to be FRESH (within VerifyFreshnessWindow) — derived ONLY here, never from
	// cfg.WebSearchEnabled.
	ReadVerifyState func() *verifystate.State

	// --- Coding-agent seams (Phase-28..). All nil-safe: a nil seam
	// degrades to typed-Unknown (no fabricated value), mirroring the
	// ReadUsage/ReadRecallState contract. They are consulted ONLY when the agent
	// is enabled (Run gates on cfg.AgentEnabled).

	// AgentPinMatch is the tri-state policy-pin compare seam: the installed
	// villa-owned Crush binary SHA-256 vs the pinned policy hash → "match" /
	// "mismatch" / "unknown" (the unevaluable case: binary absent, or the policy
	// hash not yet pinned). A nil seam → "unknown". internal/status stays free of
	// filesystem/hash coupling; status_test.go stubs it.
	AgentPinMatch func() string

	// AgentResidency is the DERIVED residency seam (SC1): it RECOMPUTES the
	// coder fit's residency from the live memory envelope
	// (recommend.Pick(...).Coder.Residency → recommend.ResidencySwap/ResidencyShared)
	// and returns "" (typed-Unknown) when the envelope is unevaluable. It is NEVER
	// read from cfg (VillaConfig has no residency field) and NEVER fabricated. A nil
	// seam → "" (the residency key is omitted), never a guessed swap/shared.
	AgentResidency func() string

	// AgentCache is the cache-effectiveness counter seam: it reuses the
	// Plan-02 metrics.ScrapeCacheCounters primitive over the live /metrics scrape.
	// It returns (cacheN, promptN, ok) where ok is false on an absent/unparseable
	// scrape — Run then leaves the ratio nil + counts omitted (typed-Unknown, never
	// a fabricated 0%). A nil seam is treated as ok=false (Run guards it).
	AgentCache func() (cacheN uint64, promptN uint64, ok bool)
}

// Errored returns the synthetic active-state token Run records when `systemctl
// is-active` ran but errored with no parseable state (tighten). Exposed so
// the cmd-layer table/test code can reference the same constant.
func Errored() string { return activeErrored }

// serviceUnits returns the systemd service names a rendered stack produces. Only
// .container units map to a service (Quadlet villa-llama.container →
// villa-llama.service); .network/.volume units are not services. Moved here (pure)
// so Run no longer depends on the cmd-layer helper.
func serviceUnits(units []orchestrate.Unit) []string {
	var svcs []string
	for _, u := range units {
		if name, ok := strings.CutSuffix(u.Name, ".container"); ok {
			svcs = append(svcs, name+".service")
		}
	}
	return svcs
}

// Run builds the Report from the injected seams (the body of the old runStatus,
// minus printing/exit). It performs no I/O of its own; every host touch is a Deps
// seam. The result is the frozen --json contract the CLI encodes and the dashboard
// serializes.
func Run(d Deps) Report {
	cfg, err := d.LoadConfig()
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	modelFile, err := d.ModelFile(cfg)
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	backend, err := inference.BackendFor(cfg.Backend)
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	var resident []orchestrate.ResidentUnit
	if d.ResidentUnits != nil {
		resident, err = d.ResidentUnits(cfg)
		if err != nil {
			return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
		}
	} else if len(cfg.Resident) > 0 {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: errors.New("status: the ResidentUnits seam is unwired while the config declares resident slots; the report would under-report the stack by omitting every resident service and its published port")}
	}

	units, err := d.Render(orchestrate.RenderInput{
		Backend:   backend,
		Cfg:       cfg,
		ModelFile: modelFile,
		ModelsDir: d.ModelsDir(),
		Resident:  resident,
	})
	if err != nil {
		return Report{Overall: inference.StatusFail.String(), NoTelemetry: noTelemetryStatement, err: err}
	}

	endpoint := d.Endpoint()
	report := Report{
		NoTelemetry: noTelemetryStatement,
		Ports:       publishedPorts(units),
	}
	report.LoopbackOnly = allLoopback(report.Ports)

	// Active-backend identity from the ALREADY-RESOLVED backend — the same
	// accessors backendShowEntry uses, never a literal. residency correctness is
	// wired below at the RunningOffloadVerdict call (Markers: backend.ResidencyProof());
	// this only surfaces the visible identity.
	report.Backend = backend.Name()
	report.Image = backend.Image()
	// Active model identity: the dashboard keys per-model cumulative usage
	// on this. Sourced from the same cfg as Backend/Image; omitempty omits it when unset
	// (typed-Unknown, never a fabricated identity).
	report.Model = cfg.Model
	report.SchemaVersion = reportSchemaVersion
	// Live tok/s: typed-optional via the seam — nil on idle/unavailable so it
	// serializes as omitted, never a fabricated 0. Guard a nil seam defensively.
	if d.GenTokensPerSec != nil {
		report.GenTokensPerSec = d.GenTokensPerSec(endpoint)
	}
	// ROCm-readiness tri-state: fold the detect sub-tree from the seam. A nil
	// seam leaves the indicator "unknown" (no false-green).
	report.ROCmReadiness = ROCmUnknown
	if d.ROCmReadiness != nil {
		report.ROCmReadiness = foldROCmReadiness(d.ROCmReadiness())
	}
	// Cumulative usage: read-only via the seam. A nil seam OR a nil result
	// (absent/empty store) leaves report.Usage nil so it serializes as omitted
	// typed-Unknown, never a fabricated 0. The seam never writes usage.json.
	if d.ReadUsage != nil {
		report.Usage = d.ReadUsage()
	}
	// Memory section: populated ONLY when the memory stack is
	// enabled — a memory-off report carries Memory == nil so the omitempty key
	// is absent and the v3 contract differs from v2 only in schema_version
	// The embedding identity comes from cfg (single source of truth);
	// the recall summary degrades typed-Unknown: a nil seam or an unreadable
	// store ⇒ "unknown" with NO fabricated counts/timestamps.
	if subsystem.MemoryOn(cfg) {
		report.Memory = memoryInfo(cfg, d.ReadRecallState)
	}
	// Coding-agent section: populated ONLY when the agent is
	// enabled — an agent-off report carries Coding == nil so the omitempty key is
	// absent and the v4 contract differs from v3 only in schema_version.
	// The identity (version/model/mode) comes from cfg (single source of truth);
	// pin/residency/cache degrade typed-Unknown via their seams: a nil seam or an
	// unevaluable signal yields "unknown" pin / omitted residency / omitted cache
	// NEVER a fabricated match/swap/0%.
	if subsystem.AgentOn(cfg) {
		report.Coding = codingInfo(cfg, d.AgentPinMatch, d.AgentResidency, d.AgentCache)
	}
	// Web-search section: populated ONLY when web search is
	// enabled — a web-search-off report carries WebSearch == nil so the omitempty
	// key is absent and the v5 contract differs from v4 only in schema_version
	// (the agent-off precedent). The outbound-bounded indicator degrades
	// typed-Unknown via the cached verify seam: a nil seam, an absent store, OR a
	// stale PASS yields "unknown" — NEVER a fabricated PASS, NEVER derived from
	// cfg.WebSearchEnabled.
	if subsystem.WebSearchOn(cfg) {
		report.WebSearch = webSearchInfo(d.ReadVerifyState)
	}

	weight := d.WeightBytes(cfg)

	// activeState resolves one unit's systemd active-state, keeping the three
	// outcomes distinct: a parseable state, a systemctl that RAN but errored with no
	// state (indeterminate-but-bad, which must drive FAIL rather than a soft WARN),
	// and a state that could not be measured at all (e.g. systemctl absent), which
	// must never become a false FAIL.
	activeState := func(unit string) string {
		active, aerr := d.IsActive(unit)
		switch {
		case aerr == nil:
			return active
		case errors.As(aerr, &orchestrate.ErrCommandFailed{}):
			return activeErrored
		default:
			return "unknown"
		}
	}

	// row builds one service row. The kind decides the offload treatment, which is
	// the ONE thing that genuinely differs between services: only the inference
	// service runs the model, so only its verdict folds into the overall status.
	// Every managed service carries the N/A representation, excluded from the fold.
	row := func(svc Service) ServiceStatus {
		ss := ServiceStatus{Service: svc.Unit, Active: activeState(svc.Unit)}
		if svc.Kind != Inference {
			ss.Health = svc.health()
			ss.Offload = naOffloadVerdict()
			ss.OffloadApplies = false
			ss.OffloadOK = false
			return ss
		}

		ss.Health = svc.health()
		journal, _ := d.JournalText(svc.Unit)
		ss.Offload = inference.RunningOffloadVerdict(inference.RunningOffloadInput{
			JournalText:   journal,
			Props:         d.Props(endpoint),
			GTTUsedBytes:  d.GTTUsed(),
			WeightBytes:   weight,
			ConfigModel:   modelFile,
			ConfigContext: cfg.Ctx,
			Markers:       backend.ResidencyProof(),
			// GPUBusyPercent left Unknown (the busy fold is skipped): the decode-time
			// read belongs to the residency proof, which drives its own workload.
		})
		ss.OffloadApplies = true
		ss.OffloadOK = ss.Offload.Status == inference.StatusPass
		return ss
	}

	// Rendered rows, in unit order.
	for _, unit := range serviceUnits(units) {
		svc, ok := findService(d.Services, unit)
		if !ok {
			// A rendered unit with no configured service still gets a row, probed by
			// nothing: reporting Unknown is honest, and omitting the row would hide a
			// running service from the operator.
			svc = Service{Unit: unit, Kind: Managed}
		}
		report.Services = append(report.Services, row(svc))
	}

	// Always-row services, AFTER the rendered ones. The dashboard is a managed,
	// observable member of the stack but a native systemd service rather than a
	// Quadlet container, so it never appears in the rendered units.
	for _, svc := range d.Services {
		if !svc.AlwaysRow {
			continue
		}
		report.Services = append(report.Services, row(svc))
	}

	report.Overall = Aggregate(report).String()
	return report
}

// memoryInfo assembles the Report's Memory section from the configured embedding
// identity and the recall-state read seam. RecallState mapping: nil seam
// or nil return (unreadable store) → "unknown"; a state with no index run
// recorded → "empty" (a confident "nothing indexed yet", distinct from unknown);
// a clean complete run (recall.CompleteRun — the single predicate, never
// re-rolled) → "indexed" with the chat count and timestamps verbatim; a run that
// started but never completed → "incomplete". EmbeddingSkew is set ONLY on a
// confident recall.SkewMismatch — match and unknown leave the field
// empty/omitted, never a green "ok" for an unevaluated comparison.
func memoryInfo(cfg config.VillaConfig, readState func() *recall.State) *MemoryInfo {
	mi := &MemoryInfo{
		EmbeddingModel: cfg.EmbeddingModel,
		EmbeddingDim:   cfg.EmbeddingDim,
		RecallState:    "unknown",
	}
	if readState == nil {
		return mi
	}
	st := readState()
	if st == nil {
		return mi // unreadable store → typed-Unknown, no fabricated counts
	}
	switch {
	case st.LastIndexStartedAt == "" && st.LastIndexCompletedAt == "":
		mi.RecallState = "empty"
	case recall.CompleteRun(*st):
		mi.RecallState = "indexed"
		mi.IndexedChats = len(st.Chats)
		mi.LastIndexStartedAt = st.LastIndexStartedAt
		mi.LastIndexCompletedAt = st.LastIndexCompletedAt
	default:
		mi.RecallState = "incomplete"
	}
	if recall.EmbeddingSkew(*st, cfg.EmbeddingModel, cfg.EmbeddingDim) == recall.SkewMismatch {
		mi.EmbeddingSkew = "mismatch"
	}
	return mi
}

// codingInfo assembles the Report's Coding section from the configured agent
// identity and the three nil-safe agent seams (Phase-28..). It clones
// memoryInfo's typed-Unknown discipline:
//
//   - Enabled is always true (the section is built ONLY when cfg.AgentEnabled).
//   - Version is the pinned Crush policy version (agent.LoadCrushPolicy() — a pure
//     embedded-bytes read, no host I/O) — omitted by omitempty if empty.
//   - Model is cfg.CoderModel; Mode is "coding" when cfg.CodingMode is on, else ""
//     (omitted) — both from cfg, the single source of truth (NEVER fabricated).
//   - PinMatch is the tri-state from the pin seam: a nil seam OR an empty return
//     degrades to PinUnknown — never a fabricated confident match/mismatch.
//   - Residency is the DERIVED value from the residency seam (recomputed from the
//     live envelope): "" when the seam is nil or the envelope is unevaluable
//     (omitted by omitempty) — NEVER a guessed swap/shared, NEVER read from cfg.
//   - Cache: the pct is set ONLY when the cache seam reports ok AND both counts are
//     usable AND promptN>0; otherwise the pct stays nil and the counts are omitted
//     — never a fabricated 0%.
func codingInfo(
	cfg config.VillaConfig,
	pinMatch func() string,
	residency func() string,
	cache func() (uint64, uint64, bool),
) *CodingInfo {
	ci := &CodingInfo{
		Enabled:  true,
		Version:  agent.LoadCrushPolicy().Version,
		Model:    cfg.CoderModel,
		PinMatch: PinUnknown,
	}
	if subsystem.CodingModeOn(cfg) {
		ci.Mode = "coding"
	}
	// Tri-state pin compare: a nil seam or an empty return is typed-Unknown.
	if pinMatch != nil {
		if pm := pinMatch(); pm != "" {
			ci.PinMatch = pm
		}
	}
	// Derived residency: recomputed from the live envelope by the seam. "" stays ""
	// (omitted) — never a fabricated swap/shared.
	if residency != nil {
		ci.Residency = residency()
	}
	// Cache effectiveness: the pct is shown ONLY when the scrape is usable, promptN>0
	// AND the sample is internally consistent (cacheN<=promptN). Otherwise pct
	// stays nil + counts omitted (gray Unknown badge / "unavailable" — never a
	// fabricated 0%). cacheN and promptN are scraped from DISTINCT llama.cpp _total
	// series independently, so a counter skew (one reset but not the other, or a build
	// where cache_n semantics differ) can yield cacheN>promptN — an impossible >100%
	// ratio. Treating that as an inconsistent sample and degrading to the gray
	// Unknown badge keeps the surface honest; fixing it HERE in the core means both
	// --json and the dashboard inherit it (no dashboard-only clamp).
	if cache != nil {
		if cacheN, promptN, ok := cache(); ok && promptN > 0 && cacheN <= promptN {
			ci.CacheN = cacheN
			ci.PromptN = promptN
			pct := (float64(cacheN) / float64(promptN)) * 100.0
			ci.CacheEffectivenessPct = &pct
		}
	}
	return ci
}

// webSearchInfo assembles the Report's WebSearch section. Enabled
// is always true (the section is built ONLY when cfg.WebSearchEnabled). The
// outbound-bounded indicator is the load-bearing honesty property: it is
// DERIVED from the cached `villa verify search` result with a FRESHNESS gate and is
// NEVER inferred from cfg.WebSearchEnabled.
//
//   - default "unknown" (typed-Unknown — never green by default);
//   - "bounded" ONLY when the cached State has Verdict=="PASS" AND CheckedAt parses
//     and is within VerifyFreshnessWindow (a real RECENT proof PASS);
//   - "not-bounded" when the cached State carries a real recent non-PASS verdict
//     (FAIL/REJECT) within the freshness window;
//   - "unknown" when the seam is nil, the store is absent, the timestamp is
//     unparseable, OR the result is stale (older than the window) — a security
//     property must be re-proven, never trusted indefinitely from a stale cache.
//
// VerifyCheckedAt is carried ONLY for a non-stale result (omitempty drops it
// otherwise) — never a fabricated timestamp. Source-gap fields (guard counters,
// last_query_at, outbound-visibility) are OMITTED: no host-side source exists and
// building one is out of scope.
func webSearchInfo(readVerify func() *verifystate.State) *WebSearchInfo {
	wi := &WebSearchInfo{
		Enabled:         true,
		OutboundBounded: OutboundUnknown, // typed-Unknown default — NEVER green by default
	}
	if readVerify == nil {
		return wi // no seam → typed-Unknown ("unavailable"), never green
	}
	st := readVerify()
	if st == nil {
		return wi // absent/unreadable store → typed-Unknown, never a fabricated PASS
	}
	checked, err := time.Parse(time.RFC3339, st.CheckedAt)
	if err != nil {
		return wi // unparseable timestamp → cannot assert freshness → "unknown"
	}
	if age := time.Since(checked); age < 0 || age > VerifyFreshnessWindow {
		// Stale OR future-dated result — a PASS this old (or stamped in the future by a
		// skewed/forged clock) must be re-proven, NEVER read as bounded. A negative age
		// is never > window, so the lower-bound clamp is required to keep the no-false-green
		// invariant. VerifyCheckedAt stays omitted (no fabricated "current" timestamp).
		return wi
	}
	// Fresh, evaluable result: surface its timestamp and map the verdict.
	wi.VerifyCheckedAt = st.CheckedAt
	if st.Verdict == "PASS" {
		wi.OutboundBounded = OutboundBounded // green: a real RECENT verify PASS
	} else {
		wi.OutboundBounded = OutboundNotBounded // amber: a real recent non-PASS verdict
	}
	return wi
}

// Err exposes the load/render error Run encountered, if any. Run returns a Report
// with Overall=FAIL on a config/model/render error; the cmd-layer caller checks
// Err to surface the precise message and map to exitBlocked.
func (r Report) Err() error { return r.err }

// Aggregate folds every service's offload Verdict, mapped health, and the
// loopback posture into the worst-wins overall status: any FAIL → FAIL; else any
// WARN → WARN; else PASS. A non-loopback bind (breach) is a FAIL.
func Aggregate(r Report) inference.Status {
	worst := inference.StatusPass
	bump := func(s inference.Status) {
		if s > worst {
			worst = s
		}
	}
	if !r.LoopbackOnly {
		bump(inference.StatusFail)
	}
	for _, s := range r.Services {
		// Only a real GPU-offload assertion folds into the verdict. A non-GPU service
		// (Open WebUI) carries an N/A offload (OffloadApplies=false) that must neither
		// bump to a spurious PASS nor FAIL a service that legitimately has no offload.
		if s.OffloadApplies {
			bump(s.Offload.Status)
		}
		bump(HealthStatus(s.Health))
		bump(ActiveStatus(s.Active))
	}
	return worst
}

// ActiveStatus maps a systemctl is-active state to the PASS/WARN/FAIL vocabulary so
// a genuinely down unit drives the overall verdict to FAIL. A clean "active"
// is PASS; transient/unknown/empty states are WARN; every terminal-bad state
// (failed, inactive, deactivating) is FAIL.
func ActiveStatus(a string) inference.Status {
	switch a {
	case "active":
		return inference.StatusPass
	case "activating", "reloading", "unknown", "":
		return inference.StatusWarn
	case activeErrored:
		return inference.StatusFail
	default: // failed, inactive, deactivating
		return inference.StatusFail
	}
}

// HealthStatus maps a mapped health state to the PASS/WARN/FAIL vocabulary: ready →
// PASS, loading/unknown → WARN (up-but-not-confirmed, never a confident FAIL
// down → FAIL.
func HealthStatus(h HealthState) inference.Status {
	switch h {
	case HealthReady:
		return inference.StatusPass
	case HealthDown:
		return inference.StatusFail
	default: // loading / unknown
		return inference.StatusWarn
	}
}

// publishedPorts parses the generated container unit(s) for PublishPort= lines (the
// generator-enforced privacy mechanism) and records each host bind address.
// It deliberately reads ONLY PublishPort= lines — never the Exec= line.
func publishedPorts(units []orchestrate.Unit) []PortBinding {
	var ports []PortBinding
	for _, u := range units {
		for _, line := range strings.Split(u.Text, "\n") {
			line = strings.TrimSpace(line)
			val, ok := strings.CutPrefix(line, "PublishPort=")
			if !ok {
				continue
			}
			ports = append(ports, parsePublishPort(val))
		}
	}
	return ports
}

// parsePublishPort splits a PublishPort value (ADDR:HOSTPORT:CONTAINERPORT, or
// HOSTPORT:CONTAINERPORT with an implicit all-interfaces bind) into a PortBinding.
// A value with no explicit host address is treated as a NON-loopback bind. A
// bracketed IPv6 host address ([::1]:HOSTPORT:CONTAINERPORT) is handled explicitly
// so a `::1` loopback bind is not misread as exposed by a naive colon split.
func parsePublishPort(val string) PortBinding {
	if strings.HasPrefix(val, "[") {
		if end := strings.Index(val, "]"); end > 0 {
			addr := val[1:end]
			rest := strings.TrimPrefix(val[end+1:], ":")
			parts := strings.Split(rest, ":")
			containerPort := ""
			if len(parts) >= 2 {
				containerPort = parts[len(parts)-1]
			}
			return PortBinding{HostAddr: addr, ContainerPort: containerPort, Loopback: isLoopbackAddr(addr)}
		}
		// Malformed bracket — treat conservatively as non-loopback.
		return PortBinding{HostAddr: val, ContainerPort: "", Loopback: false}
	}

	parts := strings.Split(val, ":")
	switch len(parts) {
	case 3:
		// ADDR:HOSTPORT:CONTAINERPORT
		addr := parts[0]
		return PortBinding{HostAddr: addr, ContainerPort: parts[2], Loopback: isLoopbackAddr(addr)}
	case 2:
		// HOSTPORT:CONTAINERPORT — no explicit address ⇒ all-interfaces (not loopback).
		return PortBinding{HostAddr: "0.0.0.0", ContainerPort: parts[1], Loopback: false}
	default:
		return PortBinding{HostAddr: val, ContainerPort: "", Loopback: false}
	}
}

// isLoopbackAddr reports whether a host bind address is the IPv4/IPv6 loopback.
func isLoopbackAddr(addr string) bool {
	return addr == "127.0.0.1" || addr == "::1" || addr == "localhost"
}

// allLoopback reports whether every published port binds loopback. An
// empty port set is vacuously loopback-only (nothing exposed).
func allLoopback(ports []PortBinding) bool {
	for _, p := range ports {
		if !p.Loopback {
			return false
		}
	}
	return true
}
