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
// status.Report's reportSchemaVersion and of the usage/bench store schema
// versions. It is NOT byte-frozen by any golden test (this phase ships no --json
// contract). Bump only on an incompatible manifest.json change; CompareSkew
// fail-closes on a manifest whose schema_version is unreadable or NEWER than
// this (D-08).
const backupSchemaVersion = 1

// Archive entry names (the deterministic outer-tar layout, D-03). manifest.json
// is FIRST so a reader parses the manifest before validating the rest. The bench
// store is a SINGLE append-only bench-reports.jsonl — exactly ONE entry / ONE
// checksum, never plural bench files.
const (
	EntryManifest        = "manifest.json"
	EntryConfig          = "config.toml"
	EntryOpenWebUIVolume = "openwebui-volume.tar"
	EntryUsage           = "usage.json"
	EntryBenchReports    = "bench-reports.jsonl"
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
