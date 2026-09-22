package main

// inference_apikey_gate_test.go is the structural guard for W11 (GHSA-qxg9,
// GHSA-gvp9, ADR-0011): every cmd/villa construction of a client that reaches a
// bearer-gated llama-server endpoint must set the APIKey field, so a future
// caller cannot silently reintroduce the 401-by-omission this ticket closed.
//
// It walks every non-test .go file in cmd/villa, finds each composite literal
// of the three client-input shapes that carry a llama-server credential
// (llm.Options{...}, residency.Target{...}, inference.ValidateInput{...}), and
// asserts the literal's own body — the balanced-brace span from the opening `{`
// to its match — contains an APIKey field. A bare zero-value literal (`Target{}`,
// used only on an error-return path that names no live target) is exempt: it
// carries no fields at all, so there is nothing for it to omit.
//
// This is a text-shape gate, not a type-checker: it does not resolve idents or
// verify the field's VALUE is non-empty, only that the literal SITE mentions
// APIKey at all. That is deliberately the same strength as TestSeamGrepGate
// (internal/inference/seam_test.go), which this mirrors.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// inferenceClientLiteralPattern matches the START of a composite literal that
// carries a llama-server bearer field. Anchored on the exact three shapes this
// ticket wired (residency.Target, inference.ValidateInput, llm.Options) — adding
// a fourth such shape means adding it here too.
func inferenceClientLiteralPattern() *regexp.Regexp {
	return regexp.MustCompile(`(?:residency\.Target|inference\.ValidateInput|llm\.Options)\{`)
}

// TestInferenceClientsCarryAPIKey walks cmd/villa's non-test .go files and fails
// if any non-empty residency.Target{}/inference.ValidateInput{}/llm.Options{}
// composite literal omits an APIKey field.
func TestInferenceClientsCarryAPIKey(t *testing.T) {
	pattern := inferenceClientLiteralPattern()
	err := filepath.WalkDir(".", func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		src := string(data)
		for _, loc := range pattern.FindAllStringIndex(src, -1) {
			openBrace := loc[1] - 1 // index of the literal's opening '{'
			body, closeIdx, ok := balancedBraceBody(src, openBrace)
			if !ok {
				t.Fatalf("%s: unterminated composite literal starting at byte %d", path, loc[0])
			}
			if strings.TrimSpace(body) == "" {
				continue // a bare Target{} zero-value error-path return: nothing to omit
			}
			if !strings.Contains(body, "APIKey") {
				line := 1 + strings.Count(src[:loc[0]], "\n")
				t.Errorf("%s:%d: %q builds a llama-server client without an APIKey field (GHSA-qxg9, ADR-0011) — thread cfg.InferenceSecret through",
					path, line, strings.TrimSpace(src[loc[0]:closeIdx+1]))
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("walk cmd/villa: %v", err)
	}
}

// balancedBraceBody returns the text strictly between the '{' at openBrace and
// its matching '}', plus that closing index, by tracking brace depth (Go source
// has no unbalanced braces inside string/rune literals that this package emits
// in these specific composite-literal sites, so a naive scan is safe here).
func balancedBraceBody(src string, openBrace int) (body string, closeIdx int, ok bool) {
	depth := 0
	for i := openBrace; i < len(src); i++ {
		switch src[i] {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return src[openBrace+1 : i], i, true
			}
		}
	}
	return "", 0, false
}
