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

// silence unused import time when only Task-1 tests are present; Task 2 uses it.
var _ = time.Now
