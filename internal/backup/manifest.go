package backup

import (
	"encoding/json"
	"fmt"
)

// manifest.go defines the self-describing backup Manifest (D-09): a
// schema-versioned JSON document recording the villa version, host fingerprint,
// both image digests (seam-sourced, never re-typed — D-10), the store schema
// versions, per-entry SHA-256 checksums, and the IDENTITIES of the excluded
// model weights (for re-pull; the weights themselves are excluded — BAK-01). The
// manifest is COUNTS/IDENTITY-ONLY by construction: no prompt/response/content
// text ever enters it (asserted structurally in manifest_test.go), mirroring the
// usage/metrics narrow-field discipline.

// backupSchemaVersion is the manifest's OWN self-version (D-09), INDEPENDENT of
// status.Report's reportSchemaVersion and of the usage/bench/recall store schema
// versions. It is NOT byte-frozen by any golden test (this phase ships no --json
// contract). Bump only on an incompatible manifest.json change; CompareSkew
// fail-closes on a manifest whose schema_version is unreadable or NEWER than
// this (D-08).
//
// Version history:
//   - v1 (Phase 16): the original five-entry archive (manifest/config/owui
//     volume/usage/bench) + version/digest/host/store-schema fields.
//   - v2 (Phase 23, D-04 doctrine): adds the OPTIONAL memory entries
//     (qdrant-volume.tar / recall-state.json) and the embedding_model /
//     embedding_dim / recall_schema_version manifest fields. The version
//     reflects the CONTRACT: an old villa fails closed on a v2 backup (it
//     cannot honor the memory entries); v1 backups STAY restorable because
//     the gate is m.SchemaVersion <= backupSchemaVersion.
//   - v3 (Phase 28, SURF-03/D-08): adds the OPTIONAL coding-agent coverage —
//     the rendered crush.json archive entry and the ExcludedAgent identity
//     record (sha256 + version + pin sha256) of the EXCLUDED agent binary
//     (bytes never archived, exactly like model weights — BAK-01). Same D-04
//     contract: an old villa fails closed on a v3 backup; v2/v1 backups stay
//     restorable (the gate is m.SchemaVersion <= backupSchemaVersion). An
//     agent-OFF backup records NO agent entry / NO ExcludedAgent and stays
//     byte-/layout-identical to a v2 archive (the bump only widens the contract
//     for an agent-ON backup).
//   - v4 (Phase 34, SURF-07): adds the OPTIONAL web-search coverage — the
//     RENDERED SearXNG settings.yml provenance archive entry (EntrySearxngSettings).
//     The settings.yml holds the rendered SEARXNG_SECRET, so restore re-writes it
//     0600-preserving (never widens the mode). The WebSearchEnabled GATE itself is
//     ALREADY archived via config.toml (web_search_enabled → EntryConfig), so this
//     bump adds the settings.yml provenance ONLY. Fetched EPHEMERAL web content is
//     EXCLUDED by design — no query/URL log entry is ever added (SURF-07). Same D-04
//     contract: an old villa fails closed on a v4 backup; v3/v2/v1 backups stay
//     restorable (the gate is m.SchemaVersion <= backupSchemaVersion). A
//     web-search-OFF backup records NO settings.yml entry and stays
//     byte-/layout-identical to a v3 archive (the bump only widens the contract
//     for a web-search-ON backup).
const backupSchemaVersion = 4

// Archive entry names (the deterministic outer-tar layout, D-03). manifest.json
// is FIRST so a reader parses the manifest before validating the rest. The bench
// store is a SINGLE append-only bench-reports.jsonl — exactly ONE entry / ONE
// checksum, never plural bench files. The two Phase-23 memory entries
// (qdrant-volume.tar / recall-state.json) are OPTIONAL: present only in a
// memory-on backup (D-05/D-06).
const (
	EntryManifest        = "manifest.json"
	EntryConfig          = "config.toml"
	EntryOpenWebUIVolume = "openwebui-volume.tar"
	EntryUsage           = "usage.json"
	EntryBenchReports    = "bench-reports.jsonl"
	EntryQdrantVolume    = "qdrant-volume.tar"
	EntryRecallState     = "recall-state.json"
	// EntryCrushConfig is the OPTIONAL Phase-28 coding-agent entry (SURF-03/D-08):
	// the RENDERED crush.json (the villa-authored agent config — kill switches +
	// loopback provider, no cloud credentials by construction, AGENT-02). Present
	// ONLY in an agent-on backup; checksummed and verified exactly like every other
	// archive member. The agent BINARY is NEVER an archive entry — only its identity
	// is recorded (ExcludedAgent), mirroring the excluded model weights (BAK-01).
	EntryCrushConfig = "crush.json"
	// EntrySearxngSettings is the OPTIONAL Phase-34 web-search entry (SURF-07): the
	// RENDERED SearXNG settings.yml provenance (the villa-authored search config —
	// the bounded engine keep_only allowlist + render-time decisions). It holds the
	// rendered SEARXNG_SECRET, so restore re-writes it 0600-preserving (never widens
	// the mode — T-34-05). Present ONLY in a web-search-on backup; checksummed and
	// SHA-256-verified exactly like every other archive member (T-34-07). Fetched
	// EPHEMERAL web content is NEVER archived — no query/URL log entry exists by
	// design (SURF-07, T-34-06). The WebSearchEnabled gate itself rides in config.toml
	// (web_search_enabled to EntryConfig), so this is the settings.yml provenance only.
	EntrySearxngSettings = "searxng-settings.yml"
)

// EntryChecksum is one archive member's name and its lowercase-hex SHA-256
// (D-09). Identity only — no content.
type EntryChecksum struct {
	Name   string `json:"name"`
	SHA256 string `json:"sha256"`
}

// ExcludedModel is the IDENTITY of a model weight that the backup deliberately
// excludes (BAK-01), recorded so restore can report it for re-pull. It carries
// catalog id / quant / ctx / source ONLY — NEVER any prompt/response/content
// text (asserted by the reflect-over-allow-set + JSON-denylist test, cloned from
// metrics.TestParseSlotsReadsOnlyNarrowFields).
type ExcludedModel struct {
	ID     string `json:"id"`
	Quant  string `json:"quant"`
	Ctx    string `json:"ctx"`
	Source string `json:"source"`
}

// ExcludedAgent is the IDENTITY of the coding-agent (Crush) binary that the
// backup deliberately EXCLUDES (SURF-03/D-08), recorded so restore can re-stage
// it (re-download the pinned release) and refuse-with-remediation on identity
// drift — exactly the ExcludedModel model-weights pattern (BAK-01). It carries
// the on-disk binary's SHA-256, the pinned policy version, and the policy's
// pinned binary SHA-256 ONLY — NEVER any prompt/response/content text (asserted
// by the reflect-over-allow-set + JSON-denylist test, cloned from
// TestExcludedModelHasNoContentFields). The binary BYTES are never archived.
type ExcludedAgent struct {
	// SHA256 is the lowercase-hex SHA-256 of the on-disk villa-owned crush binary
	// at backup time (identity for the restore re-stage verify).
	SHA256 string `json:"sha256"`
	// Version is the pinned Crush release recorded for re-stage (e.g. "v0.76.0"),
	// sourced from the embedded crush-policy.json by the cmd tier.
	Version string `json:"version"`
	// PinSHA256 is the policy-pinned EXTRACTED-binary SHA-256 (CrushAsset
	// BinarySHA256) — the expected identity restore re-stages against. A drift
	// between SHA256 and PinSHA256 is surfaced for the operator; "" means the pin
	// was not yet recorded on-hardware (the typed-Unknown sentinel — never a false
	// confident drift).
	PinSHA256 string `json:"pin_sha256"`
}

// HostFingerprint is the plain-string host identity recorded for skew compare
// (D-09). Plain strings on purpose: the pure core does NOT import internal/detect
// — the cmd layer flattens its HostProfile into these fields when building the
// ManifestInput (keeps backup free of detect, SeamGrepGate-clean).
type HostFingerprint struct {
	Arch   string `json:"arch"`
	IGPU   string `json:"igpu"`
	Kernel string `json:"kernel"`
}

// Manifest is the self-describing backup document (D-09). Fields are APPEND-ONLY
// with SchemaVersion as the LAST field (mirrors usage.UsageTotals /
// benchstore) — new fields append ABOVE it; bump backupSchemaVersion on a
// breaking change.
type Manifest struct {
	// CreatedAt is the backup timestamp (RFC3339).
	CreatedAt string `json:"created_at"`
	// VillaVersion is the build-stamped villa binary version (cmd/villa version.go;
	// D-09/OQ1).
	VillaVersion string `json:"villa_version"`
	// Host is the host fingerprint (arch / iGPU / kernel) for skew compare.
	Host HostFingerprint `json:"host"`
	// InferenceImage is the digest-pinned inference backend image, sourced from
	// inference.BackendFor(cfg.Backend).Image() by the caller — NEVER a literal here
	// (D-10).
	InferenceImage string `json:"inference_image"`
	// OpenWebUIImage is the digest-pinned Open WebUI image, sourced from
	// orchestrate.OpenWebUIImage() by the caller — NEVER a literal here (D-10).
	OpenWebUIImage string `json:"openwebui_image"`
	// ConfigSchemaVersion / UsageSchemaVersion / BenchSchemaVersion are the store
	// schema versions (D-09). Plain ints populated from ManifestInput; the REAL
	// usage/bench values are sourced by the cmd tier via the Plan-02 accessors
	// (usage.SchemaVersion() / benchstore.SavedReportSchemaVersion()) — this pure
	// core only carries the fields.
	ConfigSchemaVersion int `json:"config_schema_version"`
	UsageSchemaVersion  int `json:"usage_schema_version"`
	BenchSchemaVersion  int `json:"bench_schema_version"`
	// Entries are the per-archive-member SHA-256 checksums (one per entry; the
	// single bench-reports.jsonl carries exactly one).
	Entries []EntryChecksum `json:"entries"`
	// ExcludedModels are the identities of the excluded model weights (for re-pull,
	// BAK-01). Identity only.
	ExcludedModels []ExcludedModel `json:"excluded_models,omitempty"`
	// EmbeddingModel / EmbeddingDim record the embedding model id and its
	// LOAD-BEARING vector dimension at backup time (Phase 23, D-06/D-08), sourced
	// from config (the single source of truth). Recorded ONLY on a memory-on
	// backup; omitted otherwise so an old/memory-off backup never carries a
	// fabricated embedding claim (the "not recorded" typed-Unknown convention —
	// CompareSkew raises NO alarm on an empty model).
	EmbeddingModel string `json:"embedding_model,omitempty"`
	EmbeddingDim   int    `json:"embedding_dim,omitempty"`
	// RecallSchemaVersion is the recall store's own schema version at backup time
	// (recall.SchemaVersion(), accessor-sourced — Phase 23). Zero means "not
	// recorded" (memory-off / pre-v2 backup) and never blocks.
	RecallSchemaVersion int `json:"recall_schema_version,omitempty"`
	// ExcludedAgent is the IDENTITY of the EXCLUDED coding-agent binary (Phase 28,
	// SURF-03/D-08), recorded ONLY on an agent-on backup so restore can re-stage it
	// and fail-closed on identity drift — the binary bytes are NEVER archived
	// (clone of ExcludedModels weights exclusion, BAK-01). A *ExcludedAgent +
	// omitempty: an agent-off backup OMITS the key entirely, so the archive stays
	// byte-/layout-identical to a v2 manifest (never a fabricated agent claim).
	ExcludedAgent *ExcludedAgent `json:"excluded_agent,omitempty"`
	// SchemaVersion is the manifest's own self-version. APPEND-ONLY: this stays the
	// LAST field; new fields go ABOVE it (D-09).
	SchemaVersion int `json:"schema_version"`
}

// ManifestInput is the plain-data assembly input for BuildManifest — pure data,
// no I/O. The caller (cmd tier) gathers the version, host facts, the two
// seam-sourced digests, the three store schema versions, the computed entry
// checksums, and the excluded-model identities, then BuildManifest stamps the
// schema_version and timestamp into a Manifest.
type ManifestInput struct {
	CreatedAt           string
	VillaVersion        string
	Host                HostFingerprint
	InferenceImage      string
	OpenWebUIImage      string
	ConfigSchemaVersion int
	UsageSchemaVersion  int
	BenchSchemaVersion  int
	Entries             []EntryChecksum
	ExcludedModels      []ExcludedModel
	// EmbeddingModel / EmbeddingDim / RecallSchemaVersion are the Phase-23
	// memory-stack fields (D-06/D-08): config-sourced model id + dimension and the
	// accessor-sourced recall store schema. Zero values mean "not recorded"
	// (memory-off backup) and are omitted from the marshaled manifest.
	EmbeddingModel      string
	EmbeddingDim        int
	RecallSchemaVersion int
	// ExcludedAgent is the Phase-28 coding-agent identity record (SURF-03/D-08):
	// the cmd tier sets it ONLY on an agent-on backup (nil otherwise, so the
	// manifest omits the key and the archive stays v2-layout-identical). Identity
	// only — the binary bytes are never archived.
	ExcludedAgent *ExcludedAgent
}

// BuildManifest is the pure assembly of a Manifest from plain-data input. It
// stamps the manifest's OWN backupSchemaVersion (never trusts a caller-supplied
// value for it) and copies the input fields through. No host I/O.
func BuildManifest(in ManifestInput) Manifest {
	return Manifest{
		CreatedAt:           in.CreatedAt,
		VillaVersion:        in.VillaVersion,
		Host:                in.Host,
		InferenceImage:      in.InferenceImage,
		OpenWebUIImage:      in.OpenWebUIImage,
		ConfigSchemaVersion: in.ConfigSchemaVersion,
		UsageSchemaVersion:  in.UsageSchemaVersion,
		BenchSchemaVersion:  in.BenchSchemaVersion,
		Entries:             in.Entries,
		ExcludedModels:      in.ExcludedModels,
		EmbeddingModel:      in.EmbeddingModel,
		EmbeddingDim:        in.EmbeddingDim,
		RecallSchemaVersion: in.RecallSchemaVersion,
		ExcludedAgent:       in.ExcludedAgent,
		SchemaVersion:       backupSchemaVersion,
	}
}

// marshalManifest serializes a Manifest to indented JSON for the manifest.json
// archive entry. Indented for human readability (this phase ships human-readable
// output only — D-13); the manifest is NOT byte-frozen by any golden test.
func marshalManifest(m Manifest) ([]byte, error) {
	b, err := json.MarshalIndent(m, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("backup: marshal manifest: %w", err)
	}
	return b, nil
}

// parseManifest deserializes the manifest.json archive entry back into a Manifest
// (the inverse of marshalManifest), used by the restore read+verify pass (D-08).
// A malformed manifest is a real error — restore turns it into a fail-closed BLOCK.
func parseManifest(data []byte) (Manifest, error) {
	var m Manifest
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("backup: parse manifest: %w", err)
	}
	return m, nil
}
