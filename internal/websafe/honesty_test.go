package websafe

// honesty_test.go enforces the GUARD-04 honesty discipline (T-32-09): the guard layer must
// never describe itself dishonestly. It clones the directory-walking, regexp content-ban
// structure of internal/inference/seam_test.go (TestSeamGrepGate) and applies it to the
// websafe package's own source.
//
// Two invariants are guarded:
//
//   - TestNoInjectionSafeCopy: no source file in this package may contain the dishonest
//     phrasings "injection-safe", "immune", or "blocks injection" (case-insensitive). The
//     honest posture is "reduces and flags, does not eliminate" (see doc.go).
//   - TestMarkdownImageResidualDocumented: doc.go must document the browser-side
//     markdown-image zero-click exfil channel as a KNOWN, accepted residual — NOT closed.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// bannedCopyPattern matches the dishonest claims forbidden anywhere in the websafe package
// source (case-insensitive). "injection-safe"/"blocks injection" are the false "we stopped
// it" claims; the standalone-word "immune" boundary avoids matching benign words like
// "immunity" while still catching "immune to injection". This is the single source of the
// ban list; honesty_test.go is excluded from the walk so it does not self-trip on it.
func bannedCopyPattern() *regexp.Regexp {
	return regexp.MustCompile(`(?i)injection-safe|\bimmune\b|blocks injection`)
}

// TestNoInjectionSafeCopy walks every .go file under the websafe package directory and
// fails if any non-test file contains a banned dishonest phrasing. The walk skips _test.go
// files (this file legitimately carries the ban list, and corpus-driven test fixtures /
// behavior-naming in tests are not operator-facing copy) — mirroring seam_test.go's
// _test.go skip. It asserts at least one .go file was scanned so a broken walk cannot pass
// vacuously.
func TestNoInjectionSafeCopy(t *testing.T) {
	ban := bannedCopyPattern()
	scanned := 0

	err := filepath.Walk(".", func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			return nil
		}
		if !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, readErr := os.ReadFile(path)
		if readErr != nil {
			return readErr
		}
		scanned++
		if loc := ban.FindString(string(data)); loc != "" {
			t.Errorf("%s contains dishonest copy %q — the honest posture is "+
				"\"reduces and flags, does not eliminate\" (see doc.go)", filepath.ToSlash(path), loc)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk websafe package: %v", err)
	}
	if scanned == 0 {
		t.Fatalf("scanned 0 non-test .go files — the grep-ban walk is broken")
	}
}

// TestMarkdownImageResidualDocumented asserts doc.go documents the browser-side
// markdown-image zero-click exfil channel as a known residual (T-32-08, accept/documented).
// The residual must be present and framed as NOT closed; if doc.go ever drops or softens
// this note the honesty contract regresses, so this test pins it.
func TestMarkdownImageResidualDocumented(t *testing.T) {
	data, err := os.ReadFile("doc.go")
	if err != nil {
		t.Fatalf("read doc.go: %v", err)
	}
	src := string(data)

	markdownImage := regexp.MustCompile(`(?i)markdown.image`)
	if !markdownImage.MatchString(src) {
		t.Fatalf("doc.go does not document the markdown-image residual exfil channel")
	}
	residual := regexp.MustCompile(`(?i)residual`)
	if !residual.MatchString(src) {
		t.Fatalf("doc.go mentions the markdown-image channel but does not frame it as a residual")
	}
	notClosed := regexp.MustCompile(`(?i)not\s+(closed|claimed closed)|NOT\s+closed`)
	if !notClosed.MatchString(src) {
		t.Fatalf("doc.go does not state the markdown-image residual is NOT closed")
	}
}
