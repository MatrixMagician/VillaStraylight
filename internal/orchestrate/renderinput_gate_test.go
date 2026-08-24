package orchestrate

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestEveryRenderInputCarriesTheResidentSet guards the resident set against being
// silently dropped by a caller that forgets the field. A RenderInput without Resident
// re-renders villa-openwebui.container with the single-endpoint env and omits every
// resident unit, so Open WebUI restarts pointing only at the primary while the
// resident containers keep running and keep holding their memory, and villa status
// reports a stack those units are absent from. Nothing errors: Reconcile never deletes
// an unplanned unit file, so the models simply disappear from the chat UI.
//
// This gate lives beside RenderInput rather than beside any one caller, and walks both
// trees, because the omission has already happened twice. Five sites in cmd/villa
// dropped it when the field was introduced, and internal/status dropped it again.
// It is a grep gate rather than a comment because the failure is an OMISSION, and no
// prose on the struct stops the next caller leaving a field out.
func TestEveryRenderInputCarriesTheResidentSet(t *testing.T) {
	for _, root := range []string{"../../internal", "../../cmd/villa"} {
		walkRenderInputSites(t, root)
	}
}

func walkRenderInputSites(t *testing.T, root string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() || !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go") {
			return nil
		}
		src, rerr := os.ReadFile(filepath.Clean(path))
		if rerr != nil {
			return rerr
		}
		text := string(src)
		// Qualified only: an unqualified RenderInput{ inside this package sits next to
		// the field declaration, and the loose form also matches the memory package's
		// identically-named RenderInput.
		const site = "orchestrate.RenderInput{"
		for i := 0; ; {
			at := strings.Index(text[i:], site)
			if at < 0 {
				return nil
			}
			at += i
			body, ok := literalBody(text[at+len(site):])
			if !ok {
				t.Errorf("%s: unbalanced braces after an orchestrate.RenderInput literal", path)
				return nil
			}
			if !strings.Contains(body, "Resident:") {
				t.Errorf("%s:%d builds an orchestrate.RenderInput without Resident — the resident set would be dropped from the rendered stack, vanishing from the chat UI and from villa status with no error",
					path, strings.Count(text[:at], "\n")+1)
			}
			i = at + len(site)
		}
	})
	if err != nil {
		t.Fatalf("walk %s: %v", root, err)
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
