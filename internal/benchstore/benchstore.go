// Package benchstore owns the on-disk saved-bench-report contract: the versioned
// SavedReport JSONL record (BENCH-03) and the comparability guard (BENCH-04).
//
// This is a PURE core. It never prints and never calls os.Exit; it returns typed
// values (SavedReport, CompareResult) and does its byte I/O ONLY through an
// injected Deps seam, mirroring the bench package. By deliberate constraint it
// imports NEITHER the inference seam NOR the detect probe (TestSeamGrepGate): host
// fingerprint facts arrive as plain strings captured at the cmd tier and passed in,
// exactly as the bench package takes backend markers only through its injected verdict.
//
// pp (prompt-processing) and tg (token-generation) figures are kept STRUCTURALLY
// SEPARATE end-to-end — there is no blended tok/s key anywhere in the record or the
// delta (the no-blended golden grep enforces it).
package benchstore

import (
	"encoding/json"
	"fmt"
	"os"
)

// savedReportSchemaVersion is the self-version of the on-disk SavedReport contract.
// It is bumped ONLY on an incompatible change; new fields are appended ABOVE
// SchemaVersion (the append-only discipline cloned from detect.hostProfileSchemaVersion).
// The record golden (testdata/record.golden) freezes this contract BEFORE any live
// writer exists, so the on-disk format can never silently drift into a migration.
const savedReportSchemaVersion = 1

// storeFileMode / storeDirMode are the owner-only modes the live append writer (Plan
// 02) enforces on the JSONL store and its dir (T-14-01/T-14-02 info-disclosure
// mitigation). They ship here with the path resolver so the contract owns them.
const (
	storeFileMode os.FileMode = 0o600
	storeDirMode  os.FileMode = 0o700
)

// SavedReport is ONE saved bench report — a single JSONL line in the store. The
// field ORDER is part of the byte-frozen contract (record.golden encodes it): new
// fields append ABOVE SchemaVersion, which MUST stay LAST (append-only discipline).
//
// It persists ONLY numeric timings + the reproducible spec (including the FIXED
// benchPrompt constant) + the host fingerprint — NEVER user prompt text or model
// response content (T-14-02).
type SavedReport struct {
	// CapturedAt is the RFC3339 capture timestamp (stamped from Deps.Now if empty).
	CapturedAt string `json:"captured_at"`
	// Mode is "single" or "ab".
	Mode string `json:"mode"`
	// Spec is the reproducible benchmark spec (the fixed prompt + run conditions).
	Spec SavedSpec `json:"spec"`
	// Single carries the single-backend band (nil for an ab report).
	Single *SavedSide `json:"single,omitempty"`
	// AB carries the two-sided comparison (nil for a single report).
	AB *SavedAB `json:"ab,omitempty"`
	// VoidExhausted records that fewer than the required resident runs were collected
	// — the band must NOT be presented as authoritative (mirrors bench.Result).
	VoidExhausted bool `json:"void_exhausted"`
	// Reason is the human explanation on a void-exhaustion WARN (empty on a clean run).
	Reason string `json:"reason,omitempty"`
	// Fingerprint is the comparability key (model/quant/ctx/host + backend).
	Fingerprint Fingerprint `json:"fingerprint"`
	// SchemaVersion is the contract self-version. It MUST stay the LAST field
	// (append-only discipline; new fields go above it).
	SchemaVersion int `json:"schema_version"`
}

// SavedSpec is the reproducible benchmark spec. Prompt carries the FIXED in-repo
// benchPrompt reproducibility constant (cmd/villa/bench.go), NOT user content — the
// saved record is a SUPERSET of `bench --json` (which omits the prompt key) so the
// run can be reproduced from the store.
type SavedSpec struct {
	Prompt   string  `json:"prompt"`
	Reps     int     `json:"reps"`
	Warmup   int     `json:"warmup"`
	NPredict int     `json:"n_predict"`
	Seed     int     `json:"seed"`
	Temp     float64 `json:"temp"`
	ABTarget string  `json:"ab_target,omitempty"`
}

// SavedSide is one backend's per-metric band: pp and tg as SEPARATE median+stddev
// figures plus the resident/void counts. JSON tags are byte-identical to
// cmd/villa benchSide so the on-disk numbers match `bench --json`.
type SavedSide struct {
	Backend         string  `json:"backend"`
	PromptPerSec    float64 `json:"prompt_per_sec"`
	PromptStddev    float64 `json:"prompt_per_sec_stddev"`
	PredictedPerSec float64 `json:"predicted_per_sec"`
	PredictedStddev float64 `json:"predicted_per_sec_stddev"`
	Kept            int     `json:"kept"`
	Void            int     `json:"void"`
}

// SavedAB is the two-sided comparison: each side's band + the per-metric deltas
// (B − A). pp and tg deltas are SEPARATE keys — never blended.
type SavedAB struct {
	From                 string    `json:"from"`
	To                   string    `json:"to"`
	A                    SavedSide `json:"a"`
	B                    SavedSide `json:"b"`
	DeltaPromptPerSec    float64   `json:"delta_prompt_per_sec"`
	DeltaPredictedPerSec float64   `json:"delta_predicted_per_sec"`
}

// Fingerprint is the comparability key. Two reports are Comparable iff Model AND
// Quant AND Ctx AND HostGfxID all match; Backend is DELIBERATELY allowed to differ
// (cross-backend compare is the intended use). Host facts arrive as plain strings
// from the cmd tier (detect.Probe() .Known-guarded) — an UNKNOWN host serializes to
// "" and makes the pair NOT comparable (no false-equal). KernelVersion is recorded
// but secondary (not a comparability blocker).
type Fingerprint struct {
	Model         string `json:"model"`
	Quant         string `json:"quant"`
	Ctx           int    `json:"ctx"`
	Backend       string `json:"backend"`
	HostGfxID     string `json:"host_gfx_id"`
	KernelVersion string `json:"kernel_version,omitempty"`
}

// NewSavedReport stamps the current schema version onto a report. The cmd tier may
// also build the struct directly so long as it sets SchemaVersion via Append (which
// stamps it). Prefer this constructor for clarity at call sites.
func NewSavedReport(r SavedReport) SavedReport {
	r.SchemaVersion = savedReportSchemaVersion
	return r
}

// Marshal renders ONE SavedReport as a single compact JSONL line terminated with
// '\n' (JSONL = one object per line, NOT indented). The record golden is this exact
// compact one-line form.
func Marshal(r SavedReport) ([]byte, error) {
	b, err := json.Marshal(r)
	if err != nil {
		return nil, fmt.Errorf("benchstore: marshal saved report: %w", err)
	}
	return append(b, '\n'), nil
}
