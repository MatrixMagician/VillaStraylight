package benchstore

import (
	"bytes"
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// update is this package's OWN golden-update flag (do NOT reference cmd/villa's).
// Run `go test ./internal/benchstore/... -update` to (re)freeze record.golden.
var update = flag.Bool("update", false, "update golden files")

// deterministicReport builds a fully-populated single-mode SavedReport with a fixed
// CapturedAt so the record golden is byte-stable. VoidExhausted=true + Reason set so
// the residency-void state is exercised in the frozen contract.
func deterministicReport() SavedReport {
	return NewSavedReport(SavedReport{
		CapturedAt: "2026-06-07T12:00:00Z",
		Mode:       "single",
		Spec: SavedSpec{
			Prompt:   "Summarize the water cycle in three concise sentences.",
			Reps:     5,
			Warmup:   1,
			NPredict: 128,
			Seed:     42,
			Temp:     0.0,
		},
		Single: &SavedSide{
			Backend:         "vulkan",
			PromptPerSec:    512.50,
			PromptStddev:    3.10,
			PredictedPerSec: 48.25,
			PredictedStddev: 0.75,
			Kept:            5,
			Void:            0,
		},
		VoidExhausted: true,
		Reason:        "only 2 of 5 runs were resident",
		Fingerprint: Fingerprint{
			Model:         "llama-3.1-8b",
			Quant:         "Q4_K_M",
			Ctx:           8192,
			Backend:       "vulkan",
			HostGfxID:     "gfx1151",
			KernelVersion: "6.18.4",
		},
	})
}

// TestSchemaVersion locks the on-disk contract self-version at 1 (BENCH-03) and
// proves NewSavedReport stamps it, mirroring detect/profile_test.go.
func TestSchemaVersion(t *testing.T) {
	if savedReportSchemaVersion != 1 {
		t.Fatalf("savedReportSchemaVersion = %d, want 1", savedReportSchemaVersion)
	}
	r := NewSavedReport(SavedReport{})
	if r.SchemaVersion != savedReportSchemaVersion {
		t.Errorf("NewSavedReport stamped SchemaVersion = %d, want %d", r.SchemaVersion, savedReportSchemaVersion)
	}
}

// TestVoidRoundTrip proves a fully-populated report marshals then re-parses to an
// equal struct, with VoidExhausted=true and Reason preserved (BENCH-03 residency-void).
func TestVoidRoundTrip(t *testing.T) {
	want := deterministicReport()
	line, err := Marshal(want)
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	if line[len(line)-1] != '\n' {
		t.Errorf("Marshal output must end with newline (JSONL)")
	}
	var got SavedReport
	if err := json.Unmarshal(bytes.TrimRight(line, "\n"), &got); err != nil {
		t.Fatalf("Unmarshal: %v", err)
	}
	if !got.VoidExhausted {
		t.Errorf("VoidExhausted lost in round-trip")
	}
	if got.Reason != want.Reason {
		t.Errorf("Reason = %q, want %q", got.Reason, want.Reason)
	}
	if got.Single == nil || got.Single.PredictedPerSec != want.Single.PredictedPerSec {
		t.Errorf("Single band lost in round-trip: %+v", got.Single)
	}
	if got.Fingerprint != want.Fingerprint {
		t.Errorf("Fingerprint = %+v, want %+v", got.Fingerprint, want.Fingerprint)
	}
	if got.SchemaVersion != savedReportSchemaVersion {
		t.Errorf("SchemaVersion = %d, want %d", got.SchemaVersion, savedReportSchemaVersion)
	}
}

// TestNoBlendedKey asserts the marshaled record carries pp and tg as SEPARATE keys
// and NO blended throughput key — the milestone honesty invariant (BENCH-03). It
// checks both the live marshal and (where present) the frozen golden.
func TestNoBlendedKey(t *testing.T) {
	line, err := Marshal(deterministicReport())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	for _, want := range []string{"prompt_per_sec", "predicted_per_sec", "void_exhausted", "schema_version"} {
		if !bytes.Contains(line, []byte(want)) {
			t.Errorf("marshaled record missing required key %q", want)
		}
	}
	for _, blended := range []string{"tok_per_sec", "tokens_per_sec", "throughput"} {
		if bytes.Contains(line, []byte(blended)) {
			t.Errorf("marshaled record contains a blended key %q — pp and tg MUST stay SEPARATE", blended)
		}
	}
}

// TestSchemaVersionIsLastField proves schema_version is the LAST key emitted in the
// JSON object — the byte-frozen field order (append-only discipline).
func TestSchemaVersionIsLastField(t *testing.T) {
	line, err := Marshal(deterministicReport())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	s := string(bytes.TrimRight(line, "\n"))
	idx := bytes.LastIndex([]byte(s), []byte(`"schema_version"`))
	// every other top-level key must appear before schema_version's key position.
	for _, k := range []string{`"captured_at"`, `"mode"`, `"spec"`, `"single"`, `"void_exhausted"`, `"fingerprint"`} {
		if bytes.Index([]byte(s), []byte(k)) > idx {
			t.Errorf("key %s appears AFTER schema_version — schema_version must be last", k)
		}
	}
}

// TestRecordGolden freezes the on-disk schema-1 record format byte-for-byte BEFORE
// any live writer exists. Run with -update to create/refresh; without it, the
// deterministic record must match the frozen golden exactly.
func TestRecordGolden(t *testing.T) {
	line, err := Marshal(deterministicReport())
	if err != nil {
		t.Fatalf("Marshal: %v", err)
	}
	golden := filepath.Join("testdata", "record.golden")
	if *update {
		if err := os.MkdirAll("testdata", 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(golden, line, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("updated %s", golden)
		return
	}
	want, err := os.ReadFile(golden)
	if err != nil {
		t.Fatalf("read golden (run with -update to create): %v", err)
	}
	if !bytes.Equal(line, want) {
		t.Errorf("record does not match golden.\n--- got ---\n%s\n--- want ---\n%s", line, want)
	}
}

// --- Task 2: comparability guard, Deps seam, Append/Load, path helper ---

// fpBase is a fully-comparable fingerprint; tests mutate one field at a time.
func fpBase() Fingerprint {
	return Fingerprint{Model: "llama-3.1-8b", Quant: "Q4_K_M", Ctx: 8192, Backend: "vulkan", HostGfxID: "gfx1151"}
}

// TestComparableMatrix proves the guard: identical model+quant+ctx+host with a
// DIFFERENT backend is Comparable (cross-backend compare is the point); each single
// mismatch of model/quant/ctx/host yields Comparable==false naming the field.
func TestComparableMatrix(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(f *Fingerprint)
		wantOK   bool
		wantDiff string // expected field in the diff slice ("" => none)
	}{
		{"identical", func(f *Fingerprint) {}, true, ""},
		{"backend-differs-still-comparable", func(f *Fingerprint) { f.Backend = "rocm" }, true, ""},
		{"model-mismatch", func(f *Fingerprint) { f.Model = "other" }, false, "model"},
		{"quant-mismatch", func(f *Fingerprint) { f.Quant = "Q8_0" }, false, "quant"},
		{"ctx-mismatch", func(f *Fingerprint) { f.Ctx = 4096 }, false, "ctx"},
		{"host-mismatch", func(f *Fingerprint) { f.HostGfxID = "gfx1100" }, false, "host"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			a := fpBase()
			b := fpBase()
			tc.mutate(&b)
			ok, diff := Comparable(a, b)
			if ok != tc.wantOK {
				t.Fatalf("Comparable ok = %v, want %v (diff=%v)", ok, tc.wantOK, diff)
			}
			if tc.wantDiff != "" {
				found := false
				for _, d := range diff {
					if d == tc.wantDiff {
						found = true
					}
				}
				if !found {
					t.Errorf("diff = %v, want it to name %q", diff, tc.wantDiff)
				}
			}
		})
	}
}

// TestUnknownHost proves an UNKNOWN host fingerprint (HostGfxID=="") is NOT
// comparable to anything — even an identical-empty one — so no false-equal.
func TestUnknownHost(t *testing.T) {
	a := fpBase()
	a.HostGfxID = ""
	b := fpBase()
	b.HostGfxID = ""
	if ok, diff := Comparable(a, b); ok {
		t.Errorf("two unknown-host fingerprints must NOT be comparable, got ok=true diff=%v", diff)
	}
	c := fpBase() // known
	if ok, _ := Comparable(a, c); ok {
		t.Errorf("unknown vs known host must NOT be comparable")
	}
}

// TestCompareDelta proves Compare returns DeltaPP and DeltaTG as TWO separate
// figures for a comparable pair, and a non-comparable pair yields no numeric delta.
func TestCompareDelta(t *testing.T) {
	a := deterministicReport()
	a.Fingerprint.Backend = "vulkan"
	a.Single.Backend = "vulkan"
	a.Single.PromptPerSec = 500
	a.Single.PredictedPerSec = 40

	b := deterministicReport()
	b.Fingerprint.Backend = "rocm" // backend differs — still comparable
	b.Single.Backend = "rocm"
	b.Single.PromptPerSec = 560
	b.Single.PredictedPerSec = 35

	res := Compare(a, b)
	if !res.Comparable {
		t.Fatalf("expected comparable (only backend differs), got diff=%v", res.DifferingFields)
	}
	if res.DeltaPromptPerSec != 60 {
		t.Errorf("DeltaPromptPerSec = %v, want 60", res.DeltaPromptPerSec)
	}
	if res.DeltaPredictedPerSec != -5 {
		t.Errorf("DeltaPredictedPerSec = %v, want -5", res.DeltaPredictedPerSec)
	}

	// Non-comparable pair: differing model => no delta.
	c := deterministicReport()
	c.Fingerprint.Model = "other"
	bad := Compare(a, c)
	if bad.Comparable {
		t.Fatalf("expected not comparable on model mismatch")
	}
	if bad.DeltaPromptPerSec != 0 || bad.DeltaPredictedPerSec != 0 {
		t.Errorf("non-comparable pair must carry ZERO deltas, got pp=%v tg=%v", bad.DeltaPromptPerSec, bad.DeltaPredictedPerSec)
	}
	if len(bad.DifferingFields) == 0 {
		t.Errorf("non-comparable result must carry the differing fields")
	}
}

// bufDeps backs Deps with an in-memory buffer (no XDG touched) — the pure-core seam.
func bufDeps(buf *bytes.Buffer, now time.Time) Deps {
	return Deps{
		AppendLine: func(line []byte) error { _, err := buf.Write(line); return err },
		ReadAll:    func() ([]byte, error) { return buf.Bytes(), nil },
		Now:        func() time.Time { return now },
	}
}

// TestAppendGrowsViaSeam proves Append marshals one line per call through the seam,
// stamps the schema version + CapturedAt from Now, and the store grows append-only.
func TestAppendGrowsViaSeam(t *testing.T) {
	var buf bytes.Buffer
	fixed := time.Date(2026, 6, 7, 12, 0, 0, 0, time.UTC)
	d := bufDeps(&buf, fixed)

	r := deterministicReport()
	r.CapturedAt = "" // force the seam to stamp it
	r.SchemaVersion = 0
	if err := Append(d, r); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := Append(d, deterministicReport()); err != nil {
		t.Fatalf("Append 2: %v", err)
	}
	lines := bytes.Count(buf.Bytes(), []byte("\n"))
	if lines != 2 {
		t.Fatalf("store has %d lines after 2 Appends, want 2", lines)
	}
	// first record must have been stamped.
	loaded, err := Load(d)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if len(loaded) != 2 {
		t.Fatalf("Load returned %d records, want 2", len(loaded))
	}
	if loaded[0].SchemaVersion != savedReportSchemaVersion {
		t.Errorf("Append did not stamp SchemaVersion: %d", loaded[0].SchemaVersion)
	}
	if loaded[0].CapturedAt != fixed.Format(time.RFC3339) {
		t.Errorf("CapturedAt = %q, want %q", loaded[0].CapturedAt, fixed.Format(time.RFC3339))
	}
}

// TestLoadSkipsCorruptLine proves Load fails CLOSED per line: a corrupt JSONL line
// is skipped (no panic) and earlier valid records survive; an absent store yields an
// empty slice and no error.
func TestLoadSkipsCorruptLine(t *testing.T) {
	good, _ := Marshal(deterministicReport())
	var buf bytes.Buffer
	buf.Write(good)
	buf.WriteString("{ this is not valid json\n")
	buf.Write(good)

	d := Deps{ReadAll: func() ([]byte, error) { return buf.Bytes(), nil }}
	got, err := Load(d)
	if err != nil {
		t.Fatalf("Load must not error on a corrupt line: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("Load returned %d records, want 2 (corrupt line skipped)", len(got))
	}

	// absent store: ReadAll returns nil,nil => empty slice, no error.
	empty := Deps{ReadAll: func() ([]byte, error) { return nil, nil }}
	got2, err := Load(empty)
	if err != nil {
		t.Fatalf("Load on absent store errored: %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("absent store must yield empty slice, got %d", len(got2))
	}
}

// TestLoadSkipsUnknownSchemaVersion proves Load fails CLOSED on the schema_version
// contract: a JSON-valid line whose schema_version is a FUTURE value (2) or the
// UNSTAMPED zero value (0) is SKIPPED — never returned, never fed into Compare as if
// it were v1. Only a record stamped with the supported savedReportSchemaVersion (1)
// survives. This guards the very contract record.golden exists to protect: an unknown
// schema must not be reinterpreted as the current one (WR-01 fail-closed read path).
func TestLoadSkipsUnknownSchemaVersion(t *testing.T) {
	// A supported v1 record (deterministicReport stamps SchemaVersion=1 via the ctor).
	v1, _ := Marshal(deterministicReport())

	// A future-schema record: same struct shape but schema_version=2.
	future := deterministicReport()
	future.SchemaVersion = 2
	v2, _ := Marshal(future)

	// An unstamped record: schema_version=0 (e.g. a struct literal that never went
	// through Append/NewSavedReport). Must NOT be parsed as v1.
	unstamped := deterministicReport()
	unstamped.SchemaVersion = 0
	v0, _ := Marshal(unstamped)

	var buf bytes.Buffer
	buf.Write(v2) // unknown/future — skipped
	buf.Write(v1) // supported — kept
	buf.Write(v0) // unstamped/zero — skipped

	d := Deps{ReadAll: func() ([]byte, error) { return buf.Bytes(), nil }}
	got, err := Load(d)
	if err != nil {
		t.Fatalf("Load must not error on unknown-schema lines: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("Load returned %d records, want 1 (only the supported v1 record kept)", len(got))
	}
	if got[0].SchemaVersion != savedReportSchemaVersion {
		t.Errorf("surviving record SchemaVersion = %d, want %d", got[0].SchemaVersion, savedReportSchemaVersion)
	}
}

// TestBenchReportsPathXDG proves the path resolver honors XDG_DATA_HOME and that the
// traversal guard refuses a path resolving outside its dir.
func TestBenchReportsPathXDG(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_DATA_HOME", dir)
	got := benchReportsPath()
	want := filepath.Join(dir, "villa", "bench-reports.jsonl")
	if got != want {
		t.Errorf("benchReportsPath = %q, want %q", got, want)
	}
	storeDir := filepath.Dir(want)
	if err := assertInsideDir(want, storeDir); err != nil {
		t.Errorf("legit path rejected by guard: %v", err)
	}
	escape := filepath.Join(storeDir, "..", "..", "etc", "evil")
	if err := assertInsideDir(escape, storeDir); err == nil {
		t.Errorf("traversal guard failed to reject %q", escape)
	}
}
