package agent

import (
	"bytes"
	"encoding/json"
	"strings"
)

// drift.go is the PURE drift detector (AGENT-04). It is handed bytes/hashes
// plus a freshly-rendered reference and returns a REPORT ONLY — it has NO write
// and NO repair path of its own. The live filesystem reads + binary hash are
// injected via Deps.ReadConfig / Deps.HashBinary (Plan 02); this core does zero I/O.
//
// AGENT-04, as narrowed by ADR-0019: villa never overwrites the operator's edits to
// crush.json. A drift whose ONLY difference is providers.villa.api_key, the value
// villa alone writes, is reported as ConfigKeyOnly so the callers that write
// (agent.Run, and every stack apply through its HealAgentConfig seam) can heal it;
// every other config drift is surfaced and never written.
//
// Two drift signals, both surfaced with remediation:
//
// (a) binary drift — installed binary SHA-256 != policy binarySha256.
//	    Compares against the BINARY hash, NEVER the tarball checksum (Pitfall 6).
//	    When the policy binary hash is the UNPINNED sentinel (Plan 03 not yet run),
//	    it degrades to a typed-Unknown WARN (BinaryDriftUnknown), never a false FAIL.
//	    Never auto-corrected.
// (b) config drift — on-disk crush.json semantically != freshly-rendered.
//	    Parsed-semantic compare (canonicalize → bytes.Equal) so a whitespace-only
//	    re-save is NOT drift, while a semantic edit IS (Pitfall 4). Its key-only
//	    case (ConfigKeyOnly, KeyOnlyDrift) is the one a caller may heal.
//
// A DISTINCT third signal — config-ABSENT — is the FIRST-RUN render trigger: no
// on-disk crush.json yet. It is NOT drift (it parallels BinaryAbsent); the caller
// renders-then-launches. An absent config is NEVER compared against the rendered
// reference (that would mis-report a FALSE config drift at the first `villa code`).

// DriftInput is the pure input to DetectDrift: bytes/hashes + a freshly-rendered
// reference. No Deps, no io — the live reads are injected upstream (Plan 02).
type DriftInput struct {
	// BinaryPresent reports whether the villa-owned crush binary exists on disk.
	BinaryPresent bool
	// InstalledBinSHA is the SHA-256 of the installed binary (hex). Meaningful only
	// when BinaryPresent.
	InstalledBinSHA string
	// PolicyBinSHA is the pinned-policy binary SHA-256 (asset.BinarySHA256). When it
	// is the unpinned sentinel / empty, binary drift degrades to a typed-Unknown WARN.
	PolicyBinSHA string
	// ConfigPresent reports whether an on-disk crush.json exists. false → the
	// FIRST-RUN render trigger (ConfigAbsent), NOT drift.
	ConfigPresent bool
	// OnDiskConfig is the on-disk crush.json bytes (meaningful only when
	// ConfigPresent). An absent config's bytes are never compared.
	OnDiskConfig []byte
	// RenderedConfig is the freshly-rendered reference (from Render). The config
	// drift compare is OnDiskConfig vs this, parsed-semantically.
	RenderedConfig []byte
}

// DriftReport is the report-only outcome. Every non-clean signal carries a Reason
// (refuse-or-remediation); ConfigAbsent carries an INFORMATIONAL reason only (the
// caller auto-renders, it does not refuse). There is no field, and no method, that
// mutates anything (report only).
type DriftReport struct {
	// BinaryAbsent: the binary is not installed → Phase-27 install remediation.
	BinaryAbsent bool
	// BinaryDrift: installed binary SHA-256 != policy binarySha256 →
	// surface + remediation, never auto-correct.
	BinaryDrift bool
	// BinaryDriftUnknown: the policy binary hash is the unpinned sentinel/empty, so
	// binary drift could not be evaluated — a typed-Unknown WARN, NOT a false drift
	// (Pitfall 6). Distinct from a confident BinaryDrift=true.
	BinaryDriftUnknown bool
	// ConfigAbsent: no on-disk crush.json → the FIRST-RUN render trigger (DISTINCT
	// from ConfigDrift; parallels BinaryAbsent). Not a refusal.
	ConfigAbsent bool
	// ConfigDrift: on-disk crush.json present but semantically differs from the
	// rendered reference → surface + remediation; never written unless ConfigKeyOnly.
	ConfigDrift bool
	// ConfigKeyOnly: set together with ConfigDrift when the ONLY difference is
	// villa's own inference key (KeyOnlyDrift, ADR-0019). The operator edited
	// nothing, so a stack apply or `villa code` heals it; doctor names that heal.
	ConfigKeyOnly bool
	// Reason is the human refusal/remediation/informational explanation (empty when
	// every signal is clean).
	Reason string
}

// DetectDrift compares the installed binary + on-disk config against the pinned
// policy + freshly-rendered reference and returns a REPORT ONLY. It never
// writes, never repairs; a caller acts on ConfigKeyOnly (ADR-0019).
func DetectDrift(in DriftInput) DriftReport {
	var r DriftReport
	var reasons []string

	// (a) Binary presence + drift.
	if !in.BinaryPresent {
		r.BinaryAbsent = true
		reasons = append(reasons,
			"the villa-owned Crush binary is not installed — run the agent install (Phase 27 `villa install` addon) to place the pinned binary")
	} else if isUnpinnedBinaryHash(in.PolicyBinSHA) {
		// Pitfall 6 / Open-Q2: the binary hash is not yet pinned on-hardware (Plan 03).
		// Degrade to a typed-Unknown WARN rather than a false confident drift on a
		// freshly, correctly installed binary.
		r.BinaryDriftUnknown = true
		reasons = append(reasons,
			"binary drift cannot be confirmed — the policy binary checksum is not yet pinned (set on-hardware by 26-03); skipping the binary-drift gate")
	} else if !strings.EqualFold(in.InstalledBinSHA, in.PolicyBinSHA) {
		r.BinaryDrift = true
		reasons = append(reasons,
			"installed Crush binary checksum does not match the pinned policy — re-install the pinned binary; villa does not auto-correct a drifted binary")
	}

	// (b) Config presence vs drift.
	c := detectConfigDrift(in)
	r.ConfigAbsent, r.ConfigDrift, r.ConfigKeyOnly = c.ConfigAbsent, c.ConfigDrift, c.ConfigKeyOnly
	if c.Reason != "" {
		reasons = append(reasons, c.Reason)
	}

	r.Reason = strings.Join(reasons, "; ")
	return r
}

// detectConfigDrift is DetectDrift's config half, returned as a report carrying
// only the config fields. ABSENT is the first-run render trigger, NOT a drift: an
// absent config is never compared against the rendered reference, which would
// FALSE-positive at the first `villa code`.
func detectConfigDrift(in DriftInput) DriftReport {
	switch {
	case !in.ConfigPresent:
		return DriftReport{ConfigAbsent: true,
			Reason: "no crush.json found — villa will render it from your config.toml on first launch"}
	case semanticallyEqualConfig(in.OnDiskConfig, in.RenderedConfig):
		return DriftReport{}
	case KeyOnlyDrift(in.OnDiskConfig, in.RenderedConfig):
		return DriftReport{ConfigDrift: true, ConfigKeyOnly: true,
			Reason: "only villa's inference key in crush.json is stale — any stack apply (`villa up`) or `villa code` rewrites it (the old file is kept as crush.json.bak)"}
	}
	return DriftReport{ConfigDrift: true,
		Reason: "on-disk crush.json differs from what villa would render from config.toml — review your edits or re-render; villa surfaces drift but never overwrites your file automatically"}
}

// KeyOnlyDrift reports whether onDisk differs from rendered ONLY in
// providers.villa.api_key (ADR-0019): the two are not semantically equal, and
// putting rendered's key into the parsed on-disk document makes them so. An
// unparseable on-disk file, one without a villa provider, or any other difference,
// alone or beside a stale key, is false: that is the operator's edit, never villa's
// to overwrite (AGENT-04).
func KeyOnlyDrift(onDisk, rendered []byte) bool {
	if semanticallyEqualConfig(onDisk, rendered) {
		return false
	}
	doc, villa, ok := parseVillaProvider(onDisk)
	if !ok {
		return false
	}
	// A rendering without a villa provider leaves ref nil and sets the key to null,
	// which can never compare equal to it: no separate check is needed.
	_, ref, _ := parseVillaProvider(rendered)
	villa["api_key"] = ref["api_key"]
	patched, err := json.Marshal(doc)
	return err == nil && semanticallyEqualConfig(patched, rendered)
}

// parseVillaProvider decodes a crush.json document and returns it with its
// providers.villa object, which aliases into the document so an edit to it is an
// edit to the whole. ok is false when the bytes do not parse or there is no villa
// provider object.
func parseVillaProvider(b []byte) (doc any, villa map[string]any, ok bool) {
	if err := json.Unmarshal(b, &doc); err != nil {
		return nil, nil, false
	}
	root, _ := doc.(map[string]any)
	providers, _ := root["providers"].(map[string]any)
	villa, ok = providers[providerKey].(map[string]any)
	return doc, villa, ok
}

// isUnpinnedBinaryHash reports whether the policy binary hash is the unpinned
// sentinel or empty — i.e. the on-hardware pin (Plan 03) has not yet recorded the
// real extracted-binary SHA-256 (Pitfall 6).
func isUnpinnedBinaryHash(policyBinSHA string) bool {
	return policyBinSHA == "" || policyBinSHA == unpinnedBinarySentinel
}

// semanticallyEqualConfig compares two crush.json byte slices by PARSED-SEMANTIC
// content (canonicalize → bytes.Equal) so a whitespace-only re-save is NOT drift,
// while a semantic edit IS (Pitfall 4, Open-Q4 locked the parsed-semantic way).
func semanticallyEqualConfig(onDisk, rendered []byte) bool {
	return bytes.Equal(canonicalize(onDisk), canonicalize(rendered))
}
