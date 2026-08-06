package agent

import (
	"bytes"
	"strings"
)

// drift.go is the PURE drift detector (AGENT-04). It is handed bytes/hashes
// plus a freshly-rendered reference and returns a REPORT ONLY — it has NO write,
// NO repair, and NO auto-correct path anywhere (present-but-differs is
// surfaced, never silently rewritten). The live filesystem reads + binary hash
// are injected via Deps.ReadConfig / Deps.HashBinary (Plan 02); this core does
// zero I/O.
//
// Two drift signals, both surfaced with remediation, never auto-corrected:
//
// (a) binary drift — installed binary SHA-256 != policy binarySha256.
//	    Compares against the BINARY hash, NEVER the tarball checksum (Pitfall 6).
//	    When the policy binary hash is the UNPINNED sentinel (Plan 03 not yet run),
//	    it degrades to a typed-Unknown WARN (BinaryDriftUnknown), never a false FAIL.
// (b) config drift — on-disk crush.json semantically != freshly-rendered.
//	    Parsed-semantic compare (canonicalize → bytes.Equal) so a whitespace-only
//	    re-save is NOT drift, while a semantic edit IS (Pitfall 4).
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
	// rendered reference → surface + remediation, never auto-correct.
	ConfigDrift bool
	// Reason is the human refusal/remediation/informational explanation (empty when
	// every signal is clean).
	Reason string
}

// DetectDrift compares the installed binary + on-disk config against the pinned
// policy + freshly-rendered reference and returns a REPORT ONLY. It never
// writes, never repairs.
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

	// (b) Config presence vs drift. ABSENT is the first-run render trigger, NOT a
	// drift — STOP the config branch here (never compare an absent config against the
	// rendered reference, which would FALSE-positive at the first `villa code`).
	if !in.ConfigPresent {
		r.ConfigAbsent = true
		reasons = append(reasons,
			"no crush.json found — villa will render it from your config.toml on first launch")
	} else if !semanticallyEqualConfig(in.OnDiskConfig, in.RenderedConfig) {
		r.ConfigDrift = true
		reasons = append(reasons,
			"on-disk crush.json differs from what villa would render from config.toml — review your edits or re-render; villa surfaces drift but never overwrites your file automatically")
	}

	r.Reason = strings.Join(reasons, "; ")
	return r
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
