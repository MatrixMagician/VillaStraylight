// Package detect produces a read-only HostProfile describing the machine:
// CPU/arch, the AMD iGPU, Vulkan/ROCm backend availability, total RAM, and the
// real usable GTT/unified-memory envelope.
//
// Every probe degrades to a typed "Unknown" value (Known=false) on a missing
// tool or unparseable output — never a bare zero, never a panic (D-13/D-16).
// This file defines the typed Optional wrappers that are the spine of every
// HostProfile field.
package detect

// Bytes is an optional byte-count value with provenance.
//
// Known distinguishes "couldn't detect" (Known=false) from a legitimate zero
// (Known=true, Value=0), so --json consumers (the Phase 5 dashboard) and the
// recommender never mistake an undetected envelope for an empty one.
type Bytes struct {
	Value  uint64 `json:"value"`
	Known  bool   `json:"known"`
	Source string `json:"source,omitempty"` // provenance for -v (D-08), or reason on Unknown
	Raw    string `json:"-"`                // captured raw output on parse-fail (D-16); never serialized
}

// KnownBytes wraps a successfully detected byte count with its provenance source.
func KnownBytes(v uint64, src string) Bytes { return Bytes{Value: v, Known: true, Source: src} }

// UnknownBytes records an undetected byte count: reason explains why (surfaced in
// normal output), raw captures the offending probe output (surfaced under -v).
func UnknownBytes(reason, raw string) Bytes { return Bytes{Known: false, Source: reason, Raw: raw} }

// Str is an optional string value with provenance.
type Str struct {
	Value  string `json:"value"`
	Known  bool   `json:"known"`
	Source string `json:"source,omitempty"`
	Raw    string `json:"-"`
}

// KnownStr wraps a successfully detected string with its provenance source.
func KnownStr(v, src string) Str { return Str{Value: v, Known: true, Source: src} }

// UnknownStr records an undetected string (reason + captured raw output).
func UnknownStr(reason, raw string) Str { return Str{Known: false, Source: reason, Raw: raw} }

// Int is an optional integer value with provenance.
type Int struct {
	Value  int    `json:"value"`
	Known  bool   `json:"known"`
	Source string `json:"source,omitempty"`
	Raw    string `json:"-"`
}

// KnownInt wraps a successfully detected integer with its provenance source.
func KnownInt(v int, src string) Int { return Int{Value: v, Known: true, Source: src} }

// UnknownInt records an undetected integer (reason + captured raw output).
func UnknownInt(reason, raw string) Int { return Int{Known: false, Source: reason, Raw: raw} }

// Bool is an optional boolean value with provenance.
//
// A Known=true Bool carries a real true/false answer; an Unknown Bool means the
// signal could not be evaluated (e.g. a tool was missing), which is distinct
// from a confidently-false answer.
type Bool struct {
	Value  bool   `json:"value"`
	Known  bool   `json:"known"`
	Source string `json:"source,omitempty"`
	Raw    string `json:"-"`
}

// KnownBool wraps a successfully evaluated boolean with its provenance source.
func KnownBool(v bool, src string) Bool { return Bool{Value: v, Known: true, Source: src} }

// UnknownBool records an unevaluated boolean (reason + captured raw output).
func UnknownBool(reason, raw string) Bool { return Bool{Known: false, Source: reason, Raw: raw} }
