package main

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// TestEveryRenderInputCarriesTheResidentSet guards the resident set against being
// silently dropped by a render site that forgets it. A RenderInput without Resident
// re-renders villa-openwebui.container with the single-endpoint env, so Open WebUI
// restarts pointing only at the primary while the resident containers keep running
// and keep holding their memory. Nothing errors, config still lists the slots, and
// the models just vanish from the chat UI. Reconcile never deletes an unplanned unit
// file, which is what makes the failure quiet instead of loud.
//
// This is a grep gate rather than a comment because the failure is an OMISSION, and
// no amount of prose on the struct prevents the next caller from leaving a field out.
func TestEveryRenderInputCarriesTheResidentSet(t *testing.T) {
	entries, err := os.ReadDir(".")
	if err != nil {
		t.Fatalf("read package dir: %v", err)
	}
	site := regexp.MustCompile(`orchestrate\.RenderInput\{`)
	for _, e := range entries {
		name := e.Name()
		if e.IsDir() || !strings.HasSuffix(name, ".go") || strings.HasSuffix(name, "_test.go") {
			continue
		}
		src, err := os.ReadFile(filepath.Clean(name))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		text := string(src)
		for _, loc := range site.FindAllStringIndex(text, -1) {
			body, ok := literalBody(text[loc[1]:])
			if !ok {
				t.Fatalf("%s: unbalanced braces after an orchestrate.RenderInput literal", name)
			}
			if !strings.Contains(body, "Resident:") {
				line := strings.Count(text[:loc[0]], "\n") + 1
				t.Errorf("%s:%d builds an orchestrate.RenderInput without Resident — the resident set would be dropped from the rendered stack and the models would disappear from the chat UI with no error", name, line)
			}
		}
	}
}

// literalBody returns the text of a composite literal whose opening brace has already
// been consumed, found by brace matching rather than by guessing a terminator: the
// literal may close as "\n\t}", "})" or "\n}" depending on whether it is a local, an
// argument or a package-level var, and a heuristic that misses one reports a parse
// failure instead of the omission the gate exists to catch.
func literalBody(s string) (string, bool) {
	depth := 1
	for i, r := range s {
		switch r {
		case '{':
			depth++
		case '}':
			depth--
			if depth == 0 {
				return s[:i], true
			}
		}
	}
	return "", false
}
