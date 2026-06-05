package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
	"github.com/MatrixMagician/VillaStraylight/internal/modelswap"
	"github.com/MatrixMagician/VillaStraylight/internal/status"
)

// handleStatus folds the SHARED internal/status read-model and serializes the frozen
// Report (RESEARCH Pattern 2 / DASH-01). It calls status.Run(s.statusDeps) — the SAME
// core villa status uses — and never re-implements the worst-wins aggregation; the
// json tags are the frozen --json contract, so a dashboard consumer sees byte-identical
// Report shape to `villa status --json`.
func (s *Server) handleStatus(w http.ResponseWriter, _ *http.Request) {
	report := status.Run(s.statusDeps)
	writeJSON(w, http.StatusOK, report)
}

// healthzResponse is the tiny self-reachability body the D-04 status row probes
// (Plan 05 GETs /healthz to record the dashboard's own liveness).
type healthzResponse struct {
	OK bool `json:"ok"`
}

// handleHealthz returns 200 + {"ok":true} — the dashboard's own reachability signal
// (D-04). It touches no host state; reaching this handler is itself the liveness proof.
func (s *Server) handleHealthz(w http.ResponseWriter, _ *http.Request) {
	writeJSON(w, http.StatusOK, healthzResponse{OK: true})
}

// metricsView is the Performance panel JSON read-model (DASH-02). It carries the
// gen/prompt tok/s, the A5 prompt-eval latency, the active-slot count, and the two
// honesty flags: Idle (the gauges are stale snapshots → render "Idle — no active
// generation.") and Available (the scrape failed → render "unavailable", never zeros).
// LatencyMS is a *float64 so an unavailable/idle view omits it rather than encoding 0.
type metricsView struct {
	// GenTokensPerSec / PromptTokensPerSec are the last-window throughput gauges; they
	// are only a LIVE rate when Idle==false (Pitfall 3).
	GenTokensPerSec    float64 `json:"gen_tokens_per_sec"`
	PromptTokensPerSec float64 `json:"prompt_tokens_per_sec"`
	// LatencyMS is the A5 latency definition: prompt-eval milliseconds-per-token =
	// 1000 / prompt_tokens_seconds (captioned "prompt-eval latency" in the UI). nil
	// when unavailable or unmeasurable (prompt tok/s == 0).
	LatencyMS *float64 `json:"latency_ms,omitempty"`
	// ActiveSlots is count(is_processing) from /slots; only meaningful when SlotsKnown.
	ActiveSlots int `json:"active_slots"`
	// SlotsKnown is false when the /slots scrape failed (independent of /metrics). When
	// false the active-slot count is not a real reading and ActivityKnown may be false.
	SlotsKnown bool `json:"slots_known"`
	// Idle is true ONLY when activity is confidently known to be idle (ActivityKnown &&
	// not generating) — the UI shows "Idle — no active generation." It is never a
	// confident claim when ActivityKnown is false (WR-01).
	Idle bool `json:"idle"`
	// ActivityKnown reports whether the idle/generating state is a real measurement.
	// It is true when /slots was read (slot processing is authoritative) OR when
	// requests_processing>0 definitively says generating. When /slots failed AND
	// requests_processing==0, the snapshot cannot distinguish "idle" from "generating
	// between requests", so this is false and the UI renders activity as Unknown rather
	// than a fabricated "Idle" (WR-01 / D-10/D-11).
	ActivityKnown bool `json:"activity_known"`
	// Available is false on a 404/transport-error scrape — the UI shows "unavailable"
	// and NEVER the zero-valued fields as if they were real (D-11).
	Available bool `json:"available"`
}

// handleMetrics folds the metrics collector (/metrics + /slots) into the Performance
// read-model (DASH-02). A failed /metrics scrape marks the panel unavailable with NO
// fabricated zeros (D-11); a successful-but-not-generating snapshot sets Idle so the UI
// renders "Idle — no active generation." (Pitfall 3 / D-10) rather than a stale rate.
func (s *Server) handleMetrics(w http.ResponseWriter, _ *http.Request) {
	snap, ok := s.scrapeMetrics()
	if !ok {
		// /metrics 404 (--metrics absent) or transport error → typed-Unknown panel.
		writeJSON(w, http.StatusOK, metricsView{Available: false})
		return
	}
	// /slots is independent; its absence only drops the slot signal, not the whole panel.
	// Carry the availability bool so a failed /slots scrape degrades the activity state to
	// Unknown rather than a confident "Idle" (WR-01).
	slots, slotsOK := s.scrapeSlots()

	generating := metrics.IsGenerating(snap, slots)
	// Activity is a real measurement only when /slots was read (slot processing is
	// authoritative) OR when requests_processing>0 definitively says generating. With
	// /slots unavailable AND requests_processing==0 the snapshot cannot tell idle from
	// "between requests mid-generation", so we report ActivityKnown=false (WR-01).
	activityKnown := slotsOK || generating
	view := metricsView{
		GenTokensPerSec:    snap.GenTokensPerSec,
		PromptTokensPerSec: snap.PromptTokensPerSec,
		ActiveSlots:        metrics.ActiveSlots(slots),
		SlotsKnown:         slotsOK,
		Idle:               activityKnown && !generating,
		ActivityKnown:      activityKnown,
		Available:          true,
	}
	// A5 latency: prompt-eval ms/token derived from the available prompt-throughput
	// gauge (no dedicated latency metric exists). Only meaningful when measurable.
	if snap.PromptTokensPerSec > 0 {
		ms := 1000.0 / snap.PromptTokensPerSec
		view.LatencyMS = &ms
	}
	writeJSON(w, http.StatusOK, view)
}

// gpuView is the GPU & Memory panel JSON read-model (DASH-03), MEMORY-FIRST: the
// unified-memory used-vs-envelope headline is always the lead, and the iGPU busy% is a
// best-effort overlay that degrades to BusyAvailable=false ("Unavailable" badge) when
// the sysfs reader returns typed-Unknown (D-06) — never a fabricated number. Each
// memory figure carries its own Known flag so an undetected envelope renders honestly
// rather than as 0 bytes.
type gpuView struct {
	MemUsedBytes     uint64 `json:"mem_used_bytes"`
	MemUsedKnown     bool   `json:"mem_used_known"`
	MemEnvelopeBytes uint64 `json:"mem_envelope_bytes"`
	MemEnvelopeKnown bool   `json:"mem_envelope_known"`
	// BusyPercent is the iGPU utilization 0..100; only meaningful when BusyAvailable.
	BusyPercent int `json:"busy_percent"`
	// BusyAvailable is false when gpu_busy_percent is missing/garbage (D-06) — the UI
	// shows the gray "Unavailable" badge + "GPU utilization isn't reliably reported on
	// this hardware." rather than a confident wrong number.
	BusyAvailable bool `json:"busy_available"`
}

// handleGPU folds the existing memory readers (GTT-used headline + usable envelope) and
// the best-effort detect.GPUBusyPercent into the memory-first GPU read-model (DASH-03).
// Busy% degrades to a typed-Unknown "unavailable" overlay; the memory headline is the
// lead and carries per-figure Known flags so an undetected value never renders as 0.
func (s *Server) handleGPU(w http.ResponseWriter, _ *http.Request) {
	used := s.memUsed()
	env := s.memEnvelope()
	busy := s.gpuBusy()

	view := gpuView{
		MemUsedBytes:     used.Value,
		MemUsedKnown:     used.Known,
		MemEnvelopeBytes: env.Value,
		MemEnvelopeKnown: env.Known,
		BusyPercent:      busy.Value,
		BusyAvailable:    busy.Known,
	}
	writeJSON(w, http.StatusOK, view)
}

// ModelView is one row of the Models panel read-model (DASH-04): a catalog entry marked
// loaded / on-disk / catalog-only plus a Fits flag from the SHARED modelswap fit seam so
// the UI can disable the Switch button on a non-fitting target (D-08), and a FitDetail
// string the confirm dialog shows (the fit-verdict line). The shape mirrors
// cmd/villa/model.go modelListEntry ({ID,Quant,Loaded}) extended with the on-disk/fit
// fields; the values are computed in the live Models seam (cmd/villa), never here.
type ModelView struct {
	ID    string `json:"id"`
	Quant string `json:"quant"`
	// Loaded is true for the model the inference unit was generated with (== cfg.Model).
	Loaded bool `json:"loaded"`
	// OnDisk is true when the weights are already downloaded (no pull needed to switch).
	OnDisk bool `json:"on_disk"`
	// Fits is the SHARED fit-seam verdict; false → the UI renders Switch disabled
	// ("Won't fit") so the dashboard never fires a swap the core would reject (D-08).
	Fits bool `json:"fits"`
	// FitDetail is the human fit-verdict line the confirm dialog shows (headroom at the
	// configured context when fitting; the won't-fit reason otherwise).
	FitDetail string `json:"fit_detail"`
}

// handleModels serves the full catalog marked loaded/on-disk/catalog-only with a per-row
// fit flag (DASH-04), folding the injected Models seam — it does NOT re-implement the
// catalog/config read or the fit-math (those live in the shared cmd/villa wiring reusing
// runModelList's shape + recommend.Pick). An unavailable seam (catalog load failed)
// degrades to an empty list so the UI shows the "No models in catalog" empty state rather
// than a 500. The list is always a non-nil slice so it serializes as [] not null.
func (s *Server) handleModels(w http.ResponseWriter, _ *http.Request) {
	models, ok := s.listModels()
	if !ok || models == nil {
		models = []ModelView{}
	}
	writeJSON(w, http.StatusOK, models)
}

// switchRequest is the narrow POST /api/models/switch body: only the catalog model id.
// modelswap.Run resolves it THROUGH the catalog and refuses unknown/non-fitting ids
// before any side effect (Security V5) — the handler never treats it as a path.
type switchRequest struct {
	Model string `json:"model"`
}

// switchResponse is the typed modelswap.Result mapped to the dashboard JSON contract.
// The UI branches on switched/no_op/refused to drive the Switching…→ready transition and
// to surface the refusal reason; it carries no swap logic of its own.
type switchResponse struct {
	Switched bool `json:"switched"`
	// NoOp is true when config was persisted but the units were already up to date
	// (no restart needed, WR-06) — the UI treats it as a successful switch.
	NoOp bool `json:"no_op"`
	// Refused is true on a clean policy rejection (unknown id or won't-fit) — the UI
	// shows reason without entering the Switching… state.
	Refused bool `json:"refused"`
	// Unknown distinguishes an unresolved id from a won't-fit refusal.
	Unknown bool `json:"unknown,omitempty"`
	// Pulled is true when the weights were auto-downloaded during the swap.
	Pulled bool `json:"pulled,omitempty"`
	// From / To are the previous and new model ids.
	From string `json:"from,omitempty"`
	To   string `json:"to,omitempty"`
	// Reason is the refusal/error explanation (empty on success).
	Reason string `json:"reason,omitempty"`
}

// handleSwitch is the ONE sanctioned dashboard mutation (DASH-04). It decodes the narrow
// {model} body and calls modelswap.Run(s.swapDeps, body.Model) VERBATIM — the SAME guarded
// path `villa model swap` uses — then maps the typed Result to HTTP. It performs NO swap
// logic itself (no resolve/fit/pull/save/restart): modelswap.Run resolves the id THROUGH
// the catalog and refuses unknown/non-fitting targets before any side effect (Security
// V5 / T-05-12). The same-origin + JSON-content-type middleware (Plan 02) already gated
// this non-GET request, so a cross-origin POST never reaches here (T-05-11).
func (s *Server) handleSwitch(w http.ResponseWriter, r *http.Request) {
	// Serialize swaps: chi serves handlers concurrently, and modelswap.Run is a
	// non-atomic read-modify-write of the config↔units source of truth (CR-02). A second
	// switch arriving while one is in flight is refused with 409 Conflict rather than
	// allowed to interleave — matching the UI's single in-flight `switching` model.
	if !s.swapMu.TryLock() {
		writeJSON(w, http.StatusConflict, switchResponse{
			Refused: true,
			Reason:  "a model switch is already in progress",
		})
		return
	}
	defer s.swapMu.Unlock()

	var body switchRequest
	dec := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<16))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&body); err != nil || body.Model == "" {
		writeJSON(w, http.StatusBadRequest, switchResponse{
			Refused: true,
			Reason:  "invalid request: a JSON body {\"model\":\"<id>\"} is required",
		})
		return
	}

	// The verbatim shared core (resolve→fit→pull→save→regenerate→restart). The handler
	// adds nothing — it only decodes, folds modelswap.Run, and serializes the Result.
	res := modelswap.Run(s.swapDeps, body.Model)

	resp := switchResponse{
		Switched: res.Switched,
		NoOp:     res.NoOp,
		Refused:  res.Refused,
		Unknown:  res.Unknown,
		Pulled:   res.Pulled,
		From:     res.FromModel,
		To:       res.ToModel,
		Reason:   res.Reason,
	}

	switch {
	case res.Refused && res.Unknown:
		// Unresolved id (resolve-through-catalog guard) — no side effect.
		resp.Reason = "unknown model"
		writeJSON(w, http.StatusNotFound, resp)
	case res.Refused:
		// Won't fit the envelope — refused before any side effect (D-08).
		writeJSON(w, http.StatusUnprocessableEntity, resp)
	case res.Err != nil:
		// A step failed (pull/save/reconcile/restart) — surface it as a server error with
		// the FailedStep in the reason.
		resp.Reason = res.FailedStep + ": " + res.Err.Error()
		writeJSON(w, http.StatusInternalServerError, resp)
	default:
		// Switched or NoOp — a successful switch the UI drives to ready via polling.
		writeJSON(w, http.StatusOK, resp)
	}
}

// writeJSON sets the application/json content type and encodes v. On an encode error
// it has already written the status header, so it can only log via the response state;
// the read-model values here always marshal cleanly.
func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}
