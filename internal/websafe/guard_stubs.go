package websafe

// guard_stubs.go holds the Phase-32 injection-guard seam as IDENTITY pass-throughs.
//
// In Phase 31 the seam EXISTS but carries NO policy: fetched text flows through
// sanitize -> normalize -> fence -> classify unchanged. This makes the fetch path the
// real, sole producer of page_content (GUARD-01) while keeping the guard layer a clean,
// single insertion point. Phase 32 (GUARD-02/03/04) replaces each stub with its policy:
//
//   - sanitize  -> GUARD-02: HTML/markup sanitization (bluemonday StrictPolicy).
//   - normalize -> GUARD-02: Unicode normalization (strip bidi/zero-width tricks).
//   - fence     -> GUARD-03: nonced provenance fencing around untrusted content.
//   - classify  -> GUARD-04: heuristic prompt-injection classifier (reduces-and-flags).
//
// IMPORTANT (project posture): these stubs do NOT make content "injection-safe" — that
// claim is NEVER made in Phase 31. The two governing claims are kept distinct: outbound
// is bounded (proven later), injection-immunity is not asserted.

// sanitize is the Phase-32 GUARD-02 sanitization hook. Phase-31 identity pass-through.
func sanitize(s string) string { return s }

// normalize is the Phase-32 GUARD-02 Unicode-normalization hook. Phase-31 identity.
func normalize(s string) string { return s }

// fence is the Phase-32 GUARD-03 provenance-fencing hook. Phase-31 identity pass-through.
func fence(s string) string { return s }

// classify is the Phase-32 GUARD-04 prompt-injection classifier. It returns true when a
// detection fires; the Phase-31 stub never detects (returns false) — the seam is present,
// the policy is not.
func classify(s string) bool { _ = s; return false }
