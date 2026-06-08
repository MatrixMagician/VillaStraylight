package backup

import (
	"encoding/json"
	"reflect"
	"strings"
	"testing"
)

// TestManifestJSONRoundTrip asserts a built Manifest survives a JSON
// marshal/unmarshal unchanged (the on-disk manifest.json contract, D-09).
func TestManifestJSONRoundTrip(t *testing.T) {
	in := ManifestInput{
		CreatedAt:           "2026-06-07T19:52:28Z",
		VillaVersion:        "v1.2.0",
		Host:                HostFingerprint{Arch: "amd64", IGPU: "gfx1151", Kernel: "6.18.4"},
		InferenceImage:      "image-inference@sha256:deadbeef",
		OpenWebUIImage:      "image-owui@sha256:cafe",
		ConfigSchemaVersion: 1,
		UsageSchemaVersion:  1,
		BenchSchemaVersion:  1,
		Entries: []EntryChecksum{
			{Name: EntryConfig, SHA256: "aa"},
			{Name: EntryBenchReports, SHA256: "bb"},
		},
		ExcludedModels: []ExcludedModel{
			{ID: "qwen", Quant: "UD-Q4_K_M", Ctx: "65536", Source: "catalog"},
		},
	}
	m := BuildManifest(in)
	if m.SchemaVersion != backupSchemaVersion {
		t.Fatalf("SchemaVersion = %d, want %d", m.SchemaVersion, backupSchemaVersion)
	}

	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	var got Manifest
	if err := json.Unmarshal(data, &got); err != nil {
		t.Fatalf("unmarshal: %v", err)
	}
	if !reflect.DeepEqual(m, got) {
		t.Fatalf("round-trip mismatch:\n got %+v\nwant %+v", got, m)
	}
}

// TestManifestSchemaVersionIsLastField asserts schema_version is the LAST field
// in the JSON document (append-only contract — new fields go ABOVE it, D-09;
// mirrors usage.UsageTotals). A raw-key-order scan catches an accidental
// reorder.
func TestManifestSchemaVersionIsLastField(t *testing.T) {
	m := BuildManifest(ManifestInput{
		Entries: []EntryChecksum{{Name: EntryManifest, SHA256: "x"}},
	})
	data, err := json.Marshal(m)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	idx := strings.Index(s, `"schema_version"`)
	if idx < 0 {
		t.Fatalf("schema_version key not present in %s", s)
	}
	// No other JSON key may follow schema_version: the remainder must be only the
	// value and the closing brace.
	rest := s[idx+len(`"schema_version"`):]
	if strings.Contains(rest, `":`) {
		t.Fatalf("a field appears AFTER schema_version (must be last): tail=%q", rest)
	}
}

// TestExcludedModelHasNoContentFields is the structural narrow-field / no-content
// security test (cloned from metrics.TestParseSlotsReadsOnlyNarrowFields and
// usage's no-content test): the ExcludedModel identity record must carry ONLY
// id/quant/ctx/source — never any prompt/response/content text (T-16-01c). It
// asserts both the allow-set of Go field names AND a JSON-key denylist on a
// marshaled instance.
func TestExcludedModelHasNoContentFields(t *testing.T) {
	allowed := map[string]bool{"ID": true, "Quant": true, "Ctx": true, "Source": true}
	st := reflect.TypeOf(ExcludedModel{})
	for i := 0; i < st.NumField(); i++ {
		name := st.Field(i).Name
		if !allowed[name] {
			t.Errorf("ExcludedModel has unexpected field %q — identity only, no prompt/content", name)
		}
	}

	data, err := json.Marshal(ExcludedModel{ID: "m", Quant: "q", Ctx: "c", Source: "s"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	denylist := []string{"prompt_text", "response", "content", "text", "messages"}
	js := strings.ToLower(string(data))
	for _, bad := range denylist {
		if strings.Contains(js, bad) {
			t.Errorf("ExcludedModel JSON contains forbidden content key %q: %s", bad, data)
		}
	}
}

// TestManifestBenchEntryIsSingle asserts the archive-entry naming uses ONE
// bench-reports.jsonl (the single append-only bench store), not plural bench
// files — the manifest carries exactly one bench checksum.
func TestManifestBenchEntryIsSingle(t *testing.T) {
	if EntryBenchReports != "bench-reports.jsonl" {
		t.Fatalf("EntryBenchReports = %q, want bench-reports.jsonl", EntryBenchReports)
	}
	// Building a manifest with one bench entry yields exactly one matching checksum.
	m := BuildManifest(ManifestInput{
		Entries: []EntryChecksum{
			{Name: EntryConfig, SHA256: "a"},
			{Name: EntryBenchReports, SHA256: "b"},
			{Name: EntryUsage, SHA256: "c"},
		},
	})
	n := 0
	for _, e := range m.Entries {
		if e.Name == EntryBenchReports {
			n++
		}
	}
	if n != 1 {
		t.Fatalf("bench-reports.jsonl entry count = %d, want exactly 1", n)
	}
}
