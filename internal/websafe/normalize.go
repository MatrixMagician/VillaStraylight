package websafe

// normalize.go is the GUARD-02 Unicode-security policy: it defangs the sanitized
// text by applying NFKC compatibility folding (fullwidth/compatibility variants ->
// canonical forms) and stripping the dangerous invisible/zero-width and bidirectional
// control runes (the "Trojan Source" class) BEFORE the text is classified or fenced,
// so obfuscated payloads cannot evade the heuristic rules.
//
// Scope is deliberately CONSERVATIVE for v1.5 (defang-not-destroy): NFKC + invisible +
// bidi-control neutralization only. Full Unicode TR39 confusables-skeleton folding is
// NOT done here — it is heavy and corrupts legitimate non-Latin content; it is a later
// refinement. Legitimate multilingual visible text survives this pass intact.
//
// HONESTY POSTURE: like the rest of the guard layer, this REDUCES and FLAGS; it does
// not confer immunity to prompt injection.

import (
	"unicode"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

// invisibleAndBidi matches the named dangerous runes — zero-width/invisible runes and
// the Trojan-Source bidirectional control characters — plus a catch-all for any other
// Unicode "Format" (Cf) rune. These are not legitimate visible content in fetched-page
// text, so removing the whole Cf category (incl. soft-hyphen) is acceptable defanging.
var invisibleAndBidi = runes.Predicate(func(r rune) bool {
	switch r {
	case 0x200B, 0x200C, 0x200D, 0xFEFF, 0x2060, 0x00AD, 0x180E, // zero-width / invisible
		0x202A, 0x202B, 0x202C, 0x202D, 0x202E, // bidi embeddings/overrides + PDF
		0x2066, 0x2067, 0x2068, 0x2069, // bidi isolates + PDI
		0x200E, 0x200F, 0x061C: // bidi marks (LRM/RLM/ALM)
		return true
	}
	return unicode.Is(unicode.Cf, r)
})

// normalizer folds NFKC then removes the invisible/bidi rune set in a single pass.
var normalizer = transform.Chain(norm.NFKC, runes.Remove(invisibleAndBidi))

// normalize applies the NFKC + invisible/bidi-strip pipeline to s.
//
// CR-02 invariant (websafe.go:163-214): on a transform error it falls back to
// NFKC-only of the raw input and NEVER returns "" for non-empty input — silently
// blackholing a real citation's content is the exact bug class the codebase fixed.
func normalize(s string) string {
	out, _, err := transform.String(normalizer, s)
	if err != nil {
		out = norm.NFKC.String(s) // never return "" on error — CR-02 anti-pattern
	}
	return out
}
