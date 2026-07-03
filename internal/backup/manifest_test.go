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

// TestManifestV2MemoryEntryConsts asserts the Phase-23 optional-entry names are
// exactly qdrant-volume.tar / recall-state.json (D-05).
func TestManifestV2MemoryEntryConsts(t *testing.T) {
	if EntryQdrantVolume != "qdrant-volume.tar" {
		t.Fatalf("EntryQdrantVolume = %q, want qdrant-volume.tar", EntryQdrantVolume)
	}
	if EntryRecallState != "recall-state.json" {
		t.Fatalf("EntryRecallState = %q, want recall-state.json", EntryRecallState)
	}
}

// TestManifestSchemaVersionIsV4 asserts the manifest's own schema version is 4
// (the Phase-34 SURF-07 append-only bump: v4 adds the OPTIONAL settings.yml
// web-search provenance entry; old villas fail closed on a v4 backup, v3/v2/v1
// backups stay restorable because the gate is m.SchemaVersion <=
// backupSchemaVersion). The Phase-28 v3 crush.json entry stays present.
func TestManifestSchemaVersionIsV4(t *testing.T) {
	if backupSchemaVersion != 4 {
		t.Fatalf("backupSchemaVersion = %d, want 4 (Phase 34 web-search settings.yml entry)", backupSchemaVersion)
	}
	if EntryCrushConfig != "crush.json" {
		t.Fatalf("EntryCrushConfig = %q, want crush.json", EntryCrushConfig)
	}
	if EntrySearxngSettings != "searxng-settings.yml" {
		t.Fatalf("EntrySearxngSettings = %q, want searxng-settings.yml", EntrySearxngSettings)
	}
}

// TestExcludedAgentHasNoContentFields is the structural narrow-field / no-content
// security test for the Phase-28 ExcludedAgent identity record (cloned from
// TestExcludedModelHasNoContentFields): it must carry ONLY sha256 / version /
// pin sha256 — identity only, NEVER any prompt/response/content text (T-28-02-02).
// It asserts both the allow-set of Go field names AND a JSON-key denylist on a
// marshaled instance.
func TestExcludedAgentHasNoContentFields(t *testing.T) {
	allowed := map[string]bool{"SHA256": true, "Version": true, "PinSHA256": true}
	st := reflect.TypeOf(ExcludedAgent{})
	for i := 0; i < st.NumField(); i++ {
		name := st.Field(i).Name
		if !allowed[name] {
			t.Errorf("ExcludedAgent has unexpected field %q — identity only, no prompt/content", name)
		}
	}

	data, err := json.Marshal(ExcludedAgent{SHA256: "ab", Version: "v0.76.0", PinSHA256: "cd"})
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	denylist := []string{"prompt_text", "response", "content", "text", "messages"}
	js := strings.ToLower(string(data))
	for _, bad := range denylist {
		if strings.Contains(js, bad) {
			t.Errorf("ExcludedAgent JSON contains forbidden content key %q: %s", bad, data)
		}
	}
}

// TestManifestExcludedAgentThreadsAndOmits asserts BuildManifest threads the
// Phase-28 ExcludedAgent through (SURF-03/D-08) AND that an agent-off manifest
// (nil ExcludedAgent) OMITS the excluded_agent key entirely (omitempty — an
// agent-off backup never carries a fabricated agent claim, keeping the archive
// v2-layout-identical), and that ExcludedAgent stays tail-appended ABOVE
// schema_version (append-only).
func TestManifestExcludedAgentThreadsAndOmits(t *testing.T) {
	on := BuildManifest(ManifestInput{
		ExcludedAgent: &ExcludedAgent{SHA256: "aa", Version: "v0.76.0", PinSHA256: "bb"},
	})
	if on.ExcludedAgent == nil || on.ExcludedAgent.SHA256 != "aa" ||
		on.ExcludedAgent.Version != "v0.76.0" || on.ExcludedAgent.PinSHA256 != "bb" {
		t.Fatalf("BuildManifest did not thread ExcludedAgent: %+v", on.ExcludedAgent)
	}
	data, err := json.Marshal(on)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	s := string(data)
	if !strings.Contains(s, `"excluded_agent"`) {
		t.Fatalf("agent-on manifest JSON missing excluded_agent: %s", s)
	}
	// excluded_agent must precede schema_version (append-only: new field ABOVE it).
	if strings.Index(s, `"excluded_agent"`) > strings.Index(s, `"schema_version"`) {
		t.Fatalf("excluded_agent must appear BEFORE schema_version (append-only): %s", s)
	}

	off := BuildManifest(ManifestInput{})
	data, err = json.Marshal(off)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if strings.Contains(string(data), `"excluded_agent"`) {
		t.Fatalf("agent-off manifest JSON must OMIT excluded_agent (omitempty): %s", data)
	}
}

// TestManifestEmbeddingFieldsThreadAndOmit asserts BuildManifest threads the
// memory-on embedding_model/embedding_dim/recall_schema_version fields through
// (D-06/D-08) AND that a memory-off manifest OMITS all three keys entirely
// (omitempty — old/memory-off backups never carry a fabricated embedding claim,
// the typed-Unknown "not recorded" convention).
func TestManifestEmbeddingFieldsThreadAndOmit(t *testing.T) {
	on := BuildManifest(ManifestInput{
		EmbeddingModel:      "nomic-embed-text-v1.5",
		EmbeddingDim:        768,
		RecallSchemaVersion: 1,
	})
	if on.EmbeddingModel != "nomic-embed-text-v1.5" || on.EmbeddingDim != 768 || on.RecallSchemaVersion != 1 {
		t.Fatalf("BuildManifest did not thread embedding fields: %+v", on)
	}
	data, err := json.Marshal(on)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"embedding_model"`, `"embedding_dim"`, `"recall_schema_version"`} {
		if !strings.Contains(string(data), key) {
			t.Fatalf("memory-on manifest JSON missing %s: %s", key, data)
		}
	}

	off := BuildManifest(ManifestInput{})
	data, err = json.Marshal(off)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	for _, key := range []string{`"embedding_model"`, `"embedding_dim"`, `"recall_schema_version"`} {
		if strings.Contains(string(data), key) {
			t.Fatalf("memory-off manifest JSON must OMIT %s (omitempty): %s", key, data)
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
