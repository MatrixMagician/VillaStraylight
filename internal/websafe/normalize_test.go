// normalize_test.go guards the Unicode-defang invariants: zero-width and
// bidirectional control runes are stripped, NFKC folds fullwidth variants to ASCII,
// legitimate non-Latin visible text survives (Pitfall 2 / defang-not-destroy), and a
// non-empty input never normalizes to "" (anti-pattern).
package websafe

import (
	"strings"
	"testing"
)

// TestNormalizeStripsInvisibleAndBidi asserts the dangerous invisible/zero-width and
// Trojan-Source bidirectional control runes are absent from the output.
func TestNormalizeStripsInvisibleAndBidi(t *testing.T) {
	dangerous := []rune{
		0x200B, // ZWSP
		0x202E, // RLO (Trojan Source)
		0xFEFF, // BOM / ZWNBSP
		0x2066, // LRI
		0x061C, // ALM
	}
	in := "ab" + string(dangerous) + "cd"
	got := normalize(in)
	for _, r := range dangerous {
		if strings.ContainsRune(got, r) {
			t.Errorf("normalize output still contains dangerous rune U+%04X", r)
		}
	}
	if got != "abcd" {
		t.Errorf("normalize(%q) = %q, want visible runes preserved as %q", in, got, "abcd")
	}
}

// TestNormalizeNFKCFolds asserts NFKC folds fullwidth compatibility variants to ASCII.
func TestNormalizeNFKCFolds(t *testing.T) {
	in := "ｉｇｎｏｒｅ" // fullwidth "ignore"
	if got := normalize(in); got != "ignore" {
		t.Errorf("normalize(fullwidth) = %q, want %q", got, "ignore")
	}
}

// TestNormalizePreservesNonLatin asserts legitimate non-Latin visible text survives
// (defang-not-destroy): the output is non-empty and retains its visible characters.
func TestNormalizePreservesNonLatin(t *testing.T) {
	cases := []string{
		"مرحبا بالعالم", // Arabic "hello world"
		"日本語のテキスト",      // Japanese
		"Привет мир",    // Cyrillic
	}
	for _, in := range cases {
		got := normalize(in)
		if got == "" {
			t.Errorf("normalize(%q) = \"\", want non-empty (defang-not-destroy)", in)
		}
		// Each input has no invisible/bidi runes, so visible content is unchanged
		// (NFKC is a no-op on already-canonical text).
		if got != in {
			t.Errorf("normalize(%q) = %q, want visible non-Latin text preserved", in, got)
		}
	}
}

// TestNormalizeNeverEmpty asserts a non-empty input never normalizes to "" — the
// content-swallow class. An all-invisible input is the strip-to-empty edge,
// which is acceptable (no visible content); a string with visible content must survive.
func TestNormalizeNeverEmpty(t *testing.T) {
	in := "citation text ​ with a zero-width space"
	if got := normalize(in); got == "" {
		t.Errorf("normalize(%q) = \"\", want non-empty (CR-02 invariant)", in)
	}
}
