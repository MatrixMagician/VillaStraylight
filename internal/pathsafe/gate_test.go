package pathsafe_test

// gate_test.go is the contract half of the expand-migrate-contract sequence that
// moved path containment, the data-root chain and atomic writes into this package.
//
// The duplication it prevents did not arrive in a single commit. Thirteen copies of
// one containment predicate accumulated because each new package reimplemented what
// the previous one already had, and nothing failed when it did. This walks the tree
// and fails the build when a hand-rolled copy of any of the three reappears, so the
// next contributor is told about the shared helper rather than left to discover it.
//
// It lives in package pathsafe_test (not pathsafe) because it walks the whole repo
// including this package, and an external test package keeps it from being confused
// with the package's own unit tests.

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// reintroduction is one banned shape, the remedy to print when it reappears, and
// the files that are allowed to contain it.
//
// The remedy is not decoration. A gate that only says "you may not do this" makes
// the next contributor guess at the alternative; naming the helper is the whole
// difference between a gate that teaches and one that merely blocks.
type reintroduction struct {
	label   string
	pattern *regexp.Regexp
	remedy  string
	// allow lists repo-relative paths permitted to match: the shared package's own
	// definitions, and the few places with a documented reason to differ.
	allow []string
}

func bannedShapes() []reintroduction {
	return []reintroduction{
		{
			label: "hand-rolled containment predicate",
			// The shape every local copy had: measure with filepath.Rel, then decide
			// with IsLocal or by inspecting the ".." prefix by hand.
			pattern: regexp.MustCompile(`filepath\.IsLocal\(|strings\.HasPrefix\(\s*rel\s*,\s*"\.\."`),
			remedy:  `use pathsafe.Inside(path, root) instead of re-deriving the containment check`,
			allow: []string{
				"internal/pathsafe/pathsafe.go",
				// The tar extractor measures an ARCHIVE ENTRY NAME against a notional
				// extraction root rather than a real filesystem path, so it cannot use
				// Inside (which resolves against the filesystem). The check is
				// deliberately its own, and its comment says so.
				"internal/backup/tarutil.go",
			},
		},
		{
			label: "hand-rolled data-root fallback chain",
			// The shape: read $XDG_DATA_HOME, fall back to the home dir, then /var/tmp.
			pattern: regexp.MustCompile(`os\.Getenv\(\s*"XDG_DATA_HOME"\s*\)`),
			remedy:  `use pathsafe.DataRoot() instead of re-deriving the XDG fallback chain`,
			allow: []string{
				"internal/pathsafe/pathsafe.go",
			},
		},
		{
			label: "hand-rolled temp-plus-rename writer",
			// The shape: create a temp file, then rename it over the target. Matching
			// os.CreateTemp alone would over-fire on legitimate temp-file use, so this
			// anchors on the rename that makes it an atomic-write.
			pattern: regexp.MustCompile(`os\.CreateTemp\(`),
			remedy:  `use pathsafe.WriteFileAtomic(root, path, data, mode) instead of re-rolling temp+rename`,
			allow: []string{
				"internal/pathsafe/pathsafe.go",
				// The backup archive is STREAMED to its temp file (the volume export can
				// be many GiB), so it cannot go through a writer that takes the whole
				// payload as a []byte. It keeps the same temp+rename discipline by hand.
				"cmd/villa/backup.go",
			},
		},
	}
}

// TestNoReintroducedHelpers walks the first-party tree and fails on a
// reintroduced copy of any of the three shapes this package owns.
func TestNoReintroducedHelpers(t *testing.T) {
	repoRoot := filepath.Join("..", "..")
	shapes := bannedShapes()

	for _, dir := range []string{"internal", "cmd"} {
		root := filepath.Join(repoRoot, dir)
		err := filepath.Walk(root, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			if info.IsDir() || !strings.HasSuffix(path, ".go") {
				return nil
			}
			// Test files are exempt: a test may legitimately construct the shape it
			// is asserting about, and this file itself contains all three patterns as
			// regex literals.
			if strings.HasSuffix(path, "_test.go") {
				return nil
			}

			rel, relErr := filepath.Rel(repoRoot, path)
			if relErr != nil {
				return relErr
			}
			rel = filepath.ToSlash(rel)

			data, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			src := string(data)

			for _, shape := range shapes {
				if !shape.pattern.MatchString(src) || allowed(rel, shape.allow) {
					continue
				}
				t.Errorf("%s: reintroduced %s.\n\tRemedy: %s.", rel, shape.label, shape.remedy)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("walk %s: %v", dir, err)
		}
	}
}

// TestGateActuallyFires is the gate's own regression test: a gate that silently
// stopped matching would pass forever and protect nothing. Each pattern is checked
// against a sample of the shape it bans, so a well-meaning edit that neuters one
// fails here rather than going unnoticed until the duplication is back.
func TestGateActuallyFires(t *testing.T) {
	samples := map[string]string{
		"hand-rolled containment predicate":    "if !filepath.IsLocal(rel) { return errEscapes }",
		"hand-rolled data-root fallback chain": `if x := os.Getenv("XDG_DATA_HOME"); x != "" {`,
		"hand-rolled temp-plus-rename writer":  `tmp, err := os.CreateTemp(dir, "villa-*.tmp")`,
	}

	for _, shape := range bannedShapes() {
		sample, ok := samples[shape.label]
		if !ok {
			t.Errorf("no sample for banned shape %q — add one so the pattern stays exercised", shape.label)
			continue
		}
		if !shape.pattern.MatchString(sample) {
			t.Errorf("the %q pattern no longer matches the shape it bans:\n\tsample: %s", shape.label, sample)
		}
		if shape.remedy == "" {
			t.Errorf("banned shape %q has no remedy — the failure must name the shared helper", shape.label)
		}
	}
}

// allowed reports whether rel is on a shape's allowlist.
func allowed(rel string, allow []string) bool {
	for _, a := range allow {
		if rel == a {
			return true
		}
	}
	return false
}
