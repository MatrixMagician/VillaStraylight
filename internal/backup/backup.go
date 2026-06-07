package backup

// backup.go holds the PURE skew comparison (BAK-03 / D-08): it compares a backup
// Manifest against the CURRENT install and classifies each difference as either a
// WARN-and-confirm finding (legitimate skew that does NOT block — e.g. a newer
// villa restoring an older backup) or a fail-closed BLOCK (corruption /
// incompatible-future schema that cannot be safely applied). No host I/O — the
// caller supplies the current-install facts as plain data (CurrentInstall) and
// the recomputed checksum verdict as a flag.

import "fmt"

// CurrentInstall is the plain-data snapshot of the running install that a backup
// Manifest is compared against (BAK-03). The cmd tier gathers these: the current
// villa version (build-stamped), the current inference + OWUI image digests
// (seam-sourced via inference.BackendFor(...).Image() / orchestrate.OpenWebUIImage()
// — never re-typed), the current host fingerprint (from detect), the current
// config/usage/bench store schema versions (usage/bench via the Plan-02
// accessors), and ChecksumFailed (set true when archive verify failed — a
// fail-closed BLOCK trigger).
type CurrentInstall struct {
	VillaVersion        string
	InferenceImage      string
	OpenWebUIImage      string
	Host                HostFingerprint
	ConfigSchemaVersion int
	UsageSchemaVersion  int
	BenchSchemaVersion  int
	// ChecksumFailed is set by the caller when a per-entry SHA-256 verify failed
	// (archive corruption) — CompareSkew turns it into a fail-closed BLOCK (D-08).
	ChecksumFailed bool
}

// SkewWarning is one WARN-and-confirm finding: the field that differs, a
// human-readable detail, and named remediation text (D-08). It does NOT block —
// the caller prints it and requires explicit y/N confirmation (--yes bypass).
type SkewWarning struct {
	Field       string
	Detail      string
	Remediation string
}

// SkewVerdict is the classified outcome of CompareSkew. Block (with BlockReason)
// is a fail-closed refusal with zero side effects; Warnings are surfaced and
// require confirmation but do NOT block. A fully-matching manifest yields neither.
type SkewVerdict struct {
	Block       bool
	BlockReason string
	Warnings    []SkewWarning
}

// CompareSkew classifies the difference between a backup Manifest m and the
// current install cur (pure; BAK-03 / D-08), per the RESEARCH §Skew Detection
// table:
//
//	BLOCK (fail-closed, no apply):
//	  - cur.ChecksumFailed (archive corruption)
//	  - m.SchemaVersion unreadable (<= 0) or NEWER than backupSchemaVersion
//	    (incompatible-future manifest)
//	  - any store schema version (config/usage/bench) in the manifest NEWER than
//	    the current value (future schema can't be safely applied — mirrors
//	    usage.Load's fail-closed-on-future)
//
//	WARN-and-confirm (legitimate skew, does NOT block):
//	  - villa version mismatch
//	  - inference / OWUI image digest mismatch (re-pull remediation)
//	  - host fingerprint mismatch (cross-host caveat)
//	  - any store schema version OLDER in the manifest than current
//
// A fully-matching manifest returns the zero SkewVerdict (no Block, no Warnings).
func CompareSkew(m Manifest, cur CurrentInstall) SkewVerdict {
	var v SkewVerdict

	// --- fail-closed BLOCK checks (D-08) ------------------------------------
	if cur.ChecksumFailed {
		v.Block = true
		v.BlockReason = "archive integrity check failed (SHA-256 mismatch) — refusing to restore a corrupt backup"
		return v
	}
	if m.SchemaVersion <= 0 || m.SchemaVersion > backupSchemaVersion {
		v.Block = true
		v.BlockReason = fmt.Sprintf(
			"manifest schema_version %d is unreadable or newer than this villa supports (%d) — cannot safely restore an incompatible manifest",
			m.SchemaVersion, backupSchemaVersion)
		return v
	}
	if blocked, reason := blockOnNewerStore("config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}
	if blocked, reason := blockOnNewerStore("usage", m.UsageSchemaVersion, cur.UsageSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}
	if blocked, reason := blockOnNewerStore("bench", m.BenchSchemaVersion, cur.BenchSchemaVersion); blocked {
		v.Block, v.BlockReason = true, reason
		return v
	}

	// --- WARN-and-confirm findings (legitimate skew) ------------------------
	if m.VillaVersion != cur.VillaVersion {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "villa_version",
			Detail:      fmt.Sprintf("backup was made by villa %q; this is villa %q", m.VillaVersion, cur.VillaVersion),
			Remediation: "version skew is usually fine; confirm to proceed, or rebuild/reinstall the matching villa version if a behaviour change is suspected",
		})
	}
	if m.InferenceImage != cur.InferenceImage {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "inference_image",
			Detail:      fmt.Sprintf("backup inference image %q differs from current %q", m.InferenceImage, cur.InferenceImage),
			Remediation: "after restore, re-pull the inference image/model weights with `villa model pull <id>` if inference fails to start",
		})
	}
	if m.OpenWebUIImage != cur.OpenWebUIImage {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "openwebui_image",
			Detail:      fmt.Sprintf("backup Open WebUI image %q differs from current %q", m.OpenWebUIImage, cur.OpenWebUIImage),
			Remediation: "the restored Open WebUI data volume was produced by a different image; confirm to proceed (Open WebUI migrates its DB forward on start)",
		})
	}
	if m.Host != cur.Host {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       "host",
			Detail:      fmt.Sprintf("backup host %+v differs from current %+v", m.Host, cur.Host),
			Remediation: "backed up on a different host — if Open WebUI cannot read its data after restore, run `podman unshare chown -R $(id -u):$(id -g) <mountpoint>` and ensure the :Z relabel",
		})
	}
	warnOnOlderStore(&v, "config", m.ConfigSchemaVersion, cur.ConfigSchemaVersion)
	warnOnOlderStore(&v, "usage", m.UsageSchemaVersion, cur.UsageSchemaVersion)
	warnOnOlderStore(&v, "bench", m.BenchSchemaVersion, cur.BenchSchemaVersion)

	return v
}

// blockOnNewerStore reports a fail-closed BLOCK when the manifest's store schema
// version is NEWER than the current value — a future schema this villa cannot
// safely apply (mirrors usage.Load's fail-closed-on-future). A zero/absent
// manifest value (<= 0) is treated as "not recorded" and does NOT block.
func blockOnNewerStore(name string, manifestVer, currentVer int) (bool, string) {
	if manifestVer > 0 && manifestVer > currentVer {
		return true, fmt.Sprintf(
			"%s store schema_version %d in the backup is newer than this villa supports (%d) — a future schema cannot be safely applied",
			name, manifestVer, currentVer)
	}
	return false, ""
}

// warnOnOlderStore appends a WARN when the manifest's store schema version is
// OLDER than current (a legitimate older backup; the store migrates forward).
func warnOnOlderStore(v *SkewVerdict, name string, manifestVer, currentVer int) {
	if manifestVer > 0 && manifestVer < currentVer {
		v.Warnings = append(v.Warnings, SkewWarning{
			Field:       name + "_schema_version",
			Detail:      fmt.Sprintf("%s store schema_version %d in the backup is older than current %d", name, manifestVer, currentVer),
			Remediation: "older store schema; confirm to proceed — the restored store will be read/migrated forward by the current villa",
		})
	}
}
