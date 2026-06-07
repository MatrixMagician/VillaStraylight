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
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"
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

// Comparable reports whether two fingerprints describe the same measurement subject
// so a delta between them is meaningful. Two reports are comparable iff Model AND
// Quant AND Ctx AND HostGfxID all match. Backend is DELIBERATELY NOT a blocker —
// comparing the SAME model on different backends is the intended use (BENCH-04).
//
// An UNKNOWN host (HostGfxID == "") makes the pair NOT comparable even against an
// identically-empty one: we refuse rather than risk a false-equal that would present
// a misleading cross-host delta (the no-false-green posture). The returned slice
// names every differing field so the caller can explain the refusal.
func Comparable(a, b Fingerprint) (bool, []string) {
	var diff []string
	if a.Model != b.Model {
		diff = append(diff, "model")
	}
	if a.Quant != b.Quant {
		diff = append(diff, "quant")
	}
	if a.Ctx != b.Ctx {
		diff = append(diff, "ctx")
	}
	if a.HostGfxID == "" || b.HostGfxID == "" || a.HostGfxID != b.HostGfxID {
		diff = append(diff, "host")
	}
	return len(diff) == 0, diff
}

// CompareResult is the typed outcome of Compare. On a non-comparable pair Comparable
// is false, DifferingFields names why, and both deltas are ZERO (never fabricated).
// On a comparable pair the two deltas are computed per metric — pp and tg stay
// SEPARATE, there is no blended figure.
type CompareResult struct {
	Comparable           bool
	DifferingFields      []string
	DeltaPromptPerSec    float64
	DeltaPredictedPerSec float64
	A                    *SavedReport
	B                    *SavedReport
}

// measuredSide returns the side whose pp/tg figures Compare reads for a report: the
// Single band for a single-mode report, else the B (compared-against) side of an AB
// report. This is the primary measured side of each report.
func measuredSide(r SavedReport) SavedSide {
	if r.Single != nil {
		return *r.Single
	}
	if r.AB != nil {
		return r.AB.B
	}
	return SavedSide{}
}

// Compare computes the per-metric delta between two saved reports IFF their
// fingerprints are comparable. A non-comparable pair returns a result with
// Comparable=false, the differing fields, and ZERO deltas — never a fabricated
// number. pp and tg deltas are computed and returned SEPARATELY (B − A); there is
// deliberately no blended delta.
func Compare(a, b SavedReport) CompareResult {
	ok, diff := Comparable(a.Fingerprint, b.Fingerprint)
	res := CompareResult{Comparable: ok, DifferingFields: diff, A: &a, B: &b}
	if !ok {
		return res
	}
	sa := measuredSide(a)
	sb := measuredSide(b)
	res.DeltaPromptPerSec = sb.PromptPerSec - sa.PromptPerSec
	res.DeltaPredictedPerSec = sb.PredictedPerSec - sa.PredictedPerSec
	return res
}

// Deps is the injectable byte-I/O seam (cloned from bench.Deps's func-field shape).
// The pure core marshals/parses; these funcs do the actual host I/O so the package
// is fully testable off-hardware with a buffer-backed Deps.
type Deps struct {
	// AppendLine appends one marshaled JSONL line (terminated with '\n') to the store.
	AppendLine func(line []byte) error
	// ReadAll returns the whole store's bytes, or (nil, nil) when no store exists yet.
	ReadAll func() ([]byte, error)
	// Now supplies the capture timestamp stamped onto a report whose CapturedAt is empty.
	Now func() time.Time
}

// Append stamps the schema version, fills CapturedAt from d.Now if empty, marshals
// the report to one JSONL line, and appends it via the seam (append-only store).
func Append(d Deps, r SavedReport) error {
	r.SchemaVersion = savedReportSchemaVersion
	if r.CapturedAt == "" && d.Now != nil {
		r.CapturedAt = d.Now().UTC().Format(time.RFC3339)
	}
	line, err := Marshal(r)
	if err != nil {
		return err
	}
	if d.AppendLine == nil {
		return fmt.Errorf("benchstore: Append: nil AppendLine seam")
	}
	return d.AppendLine(line)
}

// scanBufferMax bounds the per-line scanner buffer so a pathological store line can
// never exhaust memory (T-14-03 DoS mitigation).
const scanBufferMax = 1 << 20 // 1 MiB per line

// Load reads the store via the seam and parses every JSONL line into a SavedReport.
// It fails CLOSED per line: a corrupt/unparseable line — OR one whose schema_version
// is not the supported savedReportSchemaVersion — is skipped (never a panic) and
// earlier records survive. An unknown/future schema is NEVER parsed as v1 (that would
// risk a misleading delta from mis-mapped fields). An absent store (ReadAll =>
// nil,nil) yields an empty slice and no error (mirrors config.LoadVilla returning
// defaults when absent).
func Load(d Deps) ([]SavedReport, error) {
	if d.ReadAll == nil {
		return nil, fmt.Errorf("benchstore: Load: nil ReadAll seam")
	}
	data, err := d.ReadAll()
	if err != nil {
		return nil, fmt.Errorf("benchstore: read store: %w", err)
	}
	if len(data) == 0 {
		return nil, nil
	}
	var out []SavedReport
	sc := bufio.NewScanner(bytes.NewReader(data))
	sc.Buffer(make([]byte, 0, 64*1024), scanBufferMax)
	for sc.Scan() {
		line := bytes.TrimSpace(sc.Bytes())
		if len(line) == 0 {
			continue
		}
		var r SavedReport
		if err := json.Unmarshal(line, &r); err != nil {
			continue // fail closed per line — skip and keep going
		}
		if r.SchemaVersion != savedReportSchemaVersion {
			// Unknown/future schema — skip (fail closed). The golden-frozen
			// schema_version contract gates migrations; a future v2 record (or a
			// hand-edited JSON-valid-but-semantically-wrong line) must NEVER be
			// parsed as v1 and fed into Compare, which could emit a misleading
			// pp/tg delta from mis-mapped fields. If forward-compat reads are ever
			// desired, gate on r.SchemaVersion <= savedReportSchemaVersion AND add
			// an explicit migration — never reinterpret an unknown version as v1.
			continue
		}
		out = append(out, r)
	}
	if err := sc.Err(); err != nil {
		return out, fmt.Errorf("benchstore: scan store: %w", err)
	}
	return out, nil
}

// benchReportsPath resolves the single append-only JSONL store:
// $XDG_DATA_HOME/villa/bench-reports.jsonl, falling back to
// ~/.local/share/villa/... then /var/tmp/villa/... (cloned from model.go:modelsDir).
// The cmd tier (Plan 02) calls this via the live Deps; it lives here so the resolver
// ships with the contract it serves.
func benchReportsPath() string {
	const file = "bench-reports.jsonl"
	if x := os.Getenv("XDG_DATA_HOME"); x != "" {
		return filepath.Join(x, "villa", file)
	}
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, ".local", "share", "villa", file)
	}
	return filepath.Join("/var/tmp", "villa", file)
}

// assertInsideDir verifies path resolves within dir, rejecting traversal escapes
// (T-14-01). This is a LOCAL copy of the config guard shape — internal/config's is
// unexported and importing config solely for it would widen this pure core's deps.
func assertInsideDir(path, dir string) error {
	absDir, err := filepath.Abs(filepath.Clean(dir))
	if err != nil {
		return err
	}
	absPath, err := filepath.Abs(filepath.Clean(path))
	if err != nil {
		return err
	}
	rel, err := filepath.Rel(absDir, absPath)
	if err != nil {
		return err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("benchstore: refusing to write %q outside store dir %q", absPath, absDir)
	}
	return nil
}
