// Package detect produces a read-only HostProfile describing the machine:
// CPU/arch, the AMD iGPU, Vulkan/ROCm backend availability, total RAM, and the
// real usable GTT/unified-memory envelope.
//
// Every probe degrades to a typed "Unknown" value (Known=false) on a missing
// tool or unparseable output — never a bare zero, never a panic (D-13/D-16).
// This file defines the typed Optional wrappers that are the spine of every
// HostProfile field.
package detect

// Optional is a detected value with provenance, or the typed absence of one.
//
// Known is the distinction the whole spine exists to preserve: "couldn't
// detect" (Known=false) is not the same answer as a legitimate zero, an empty
// string, or a confident false (Known=true with that value). --json consumers
// (the dashboard) and the recommender must never read an undetected envelope as
// an empty one, so no probe may collapse the two.
//
// The four concrete spellings below are aliases rather than distinct types, so
// every existing construction site and field declaration keeps compiling and the
// serialised form is unchanged.
type Optional[T any] struct {
	Value  T      `json:"value"`
	Known  bool   `json:"known"`
	Source string `json:"source,omitempty"` // provenance for -v (D-08), or reason on Unknown
	Raw    string `json:"-"`                // captured raw output on parse-fail (D-16); never serialized
}

// known builds a detected value with its provenance source.
func known[T any](v T, src string) Optional[T] {
	return Optional[T]{Value: v, Known: true, Source: src}
}

// unknown records an undetected value: reason explains why (surfaced in normal
// output), raw captures the offending probe output (surfaced under -v).
func unknown[T any](reason, raw string) Optional[T] {
	return Optional[T]{Known: false, Source: reason, Raw: raw}
}

// Bytes is an optional byte-count value with provenance.
type Bytes = Optional[uint64]

// Str is an optional string value with provenance.
type Str = Optional[string]

// Int is an optional integer value with provenance.
type Int = Optional[int]

// Bool is an optional boolean value with provenance.
//
// A Known=true Bool carries a real true/false answer; an Unknown Bool means the
// signal could not be evaluated (e.g. a tool was missing), which is distinct
// from a confidently-false answer.
type Bool = Optional[bool]

// KnownBytes wraps a successfully detected byte count with its provenance source.
func KnownBytes(v uint64, src string) Bytes { return known(v, src) }

// UnknownBytes records an undetected byte count: reason explains why (surfaced in
// normal output), raw captures the offending probe output (surfaced under -v).
func UnknownBytes(reason, raw string) Bytes { return unknown[uint64](reason, raw) }

// KnownStr wraps a successfully detected string with its provenance source.
func KnownStr(v, src string) Str { return known(v, src) }

// UnknownStr records an undetected string (reason + captured raw output).
func UnknownStr(reason, raw string) Str { return unknown[string](reason, raw) }

// KnownInt wraps a successfully detected integer with its provenance source.
func KnownInt(v int, src string) Int { return known(v, src) }

// UnknownInt records an undetected integer (reason + captured raw output).
func UnknownInt(reason, raw string) Int { return unknown[int](reason, raw) }

// KnownBool wraps a successfully evaluated boolean with its provenance source.
func KnownBool(v bool, src string) Bool { return known(v, src) }

// UnknownBool records an unevaluated boolean (reason + captured raw output).
func UnknownBool(reason, raw string) Bool { return unknown[bool](reason, raw) }
