package websafe

import "testing"

// FuzzSanitizeNormalize feeds arbitrary bytes through the guard layer's
// pipeline in the order websafe.go's fetchOne actually runs it: sanitize
// (bluemonday's StrictPolicy over a fetched page's raw HTML) then normalize
// (NFKC fold + invisible/bidi strip). Both stages exist specifically because
// this input is adversarial -- a fetched web page under attacker control --
// so the invariant is that malformed or hostile markup can never panic the
// pipeline, regardless of how it mixes tags, entities, and Unicode control
// runes.
func FuzzSanitizeNormalize(f *testing.F) {
	// Built from rune codepoints rather than string escapes, so the zero-width,
	// BOM and bidi-override characters this seeds are testing for do not have
	// to live as raw bytes in this source file.
	zeroWidth := "a" + string(rune(0x200B)) + "b" + string(rune(0x200C)) + "c" + string(rune(0x200D)) + "d"
	bomAndBidi := string(rune(0xFEFF)) + "Ignore" + string(rune(0x202E)) + "previous"
	fullwidthIgnore := string([]rune{0xFF49, 0xFF47, 0xFF4E, 0xFF4F, 0xFF52, 0xFF45})

	seeds := []string{
		`<script>alert(1)</script>Hello &amp; goodbye<b>x</b>`,
		`if (a &lt; b)`,
		`<div></div>`,
		`<p>The quick brown fox.</p>`,
		zeroWidth,
		bomAndBidi,
		fullwidthIgnore,
		"",
		"<",
		"<<<<<<<<<<<<<<<<<<<<<<<",
		"&&&&&&&&&&&&&&&&&&&&&&&",
		"<!--x",
		"<svg><script>alert(1)</script></svg>",
	}
	for _, s := range seeds {
		f.Add(s)
	}

	f.Fuzz(func(t *testing.T, rawHTML string) {
		clean := sanitize(rawHTML)
		_ = normalize(clean)
	})
}
