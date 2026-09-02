package main

// docs_gate_test.go is the DOCUMENTATION grep-gate: a structural test that fails
// the build when a markdown file in this repo makes a statement the tree has
// stopped supporting. It is the same shape as internal/inference/seam_test.go's
// TestSeamGrepGate — walk the tree, match a small set of patterns, fail with the
// file and the reason — applied to docs rather than to backend literals.
//
// WHY IT EXISTS. An audit of every doc in this repo found two failure modes, both
// of which had survived for several milestones because nothing could see them:
//
//	(a) a claim that was TRUE ONCE and silently stopped being true. `make lint`
//	    was documented as falling back to `go vet` in FOUR files, months after that
//	    fallback was deleted for being unable to fail; two docs said no CI existed
//	    while .github/workflows/ci.yml ran on every push.
//	(b) a REFERENCE that no longer resolves — a path that moved, a `make` target
//	    that was renamed, a golden-fixture glob matching zero files.
//
// Both are decidable from the tree, which is the whole reason they belong in a
// test rather than in a reviewer's memory.
//
// WHAT IT DELIBERATELY DOES NOT DO — read this before extending it. The gate
// catches DEAD references and RETIRED claims. It cannot catch an enumeration that
// has gone incomplete, a description that is merely wrong, or prose that is stale
// but self-consistent: "the suite has 380 test functions" when there are 1110 is
// invisible here, and so is a package list naming 24 of 34. Those need a human or
// an agent reading the doc against the code. A gate that appears to check
// freshness but silently passes most staleness would be worse than none — so the
// honest scope is stated here, in the failure messages, and in docs/TESTING.md,
// and the fix for the classes it cannot see is to stop writing enumerations into
// prose (see the pointer-at-the-authority pattern the docs now use).

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// repoRoot is where the walks start: this test lives in cmd/villa.
const repoRoot = "../.."

// markdownFiles returns every tracked .md file in the repo, as paths relative to
// repoRoot. Vendor-ish and generated trees are skipped so the gate only ever
// judges prose this project actually maintains.
func markdownFiles(t *testing.T) []string {
	t.Helper()
	var out []string
	skipDir := map[string]bool{".git": true, "node_modules": true, "testdata": true}
	err := filepath.Walk(repoRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() {
			if skipDir[info.Name()] {
				return filepath.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(path, ".md") {
			return nil
		}
		rel, relErr := filepath.Rel(repoRoot, path)
		if relErr != nil {
			return relErr
		}
		out = append(out, filepath.ToSlash(rel))
		return nil
	})
	if err != nil {
		t.Fatalf("walk repo for markdown: %v", err)
	}
	if len(out) == 0 {
		t.Fatal("found no markdown files — the walk is broken, not the docs (a gate that cannot fail proves nothing)")
	}
	return out
}

// mdLink matches a markdown inline link target: the (...) half of [text](target).
var mdLink = regexp.MustCompile(`\]\(([^)\s]+)(?:\s+"[^"]*")?\)`)

// TestDocsLinkedPathsExist guards the promise that a repo-relative link in the
// docs resolves. A link that 404s on disk is a reader sent nowhere, and unlike a
// stale sentence it is decidable, so it should never reach main. External URLs
// and bare anchors are out of scope: this test does no network I/O, and an
// in-page anchor is a rendering concern rather than a tree fact.
func TestDocsLinkedPathsExist(t *testing.T) {
	for _, doc := range markdownFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		for _, m := range mdLink.FindAllStringSubmatch(string(data), -1) {
			target := m[1]
			switch {
			case strings.Contains(target, "://"), // external URL
				strings.HasPrefix(target, "#"),       // in-page anchor
				strings.HasPrefix(target, "mailto:"): // address
				continue
			}
			// Strip an #anchor: the file must exist, the anchor is not our business.
			if i := strings.IndexByte(target, '#'); i >= 0 {
				target = target[:i]
			}
			if target == "" {
				continue
			}
			resolved := filepath.Join(filepath.Dir(filepath.Join(repoRoot, doc)), target)
			if _, statErr := os.Stat(resolved); statErr != nil {
				t.Errorf("%s links to %q, which does not exist (a link that 404s is a reader sent nowhere)", doc, m[1])
			}
		}
	}
}

// makeTarget matches a `make <target>` mention in prose, skipping any VAR=value
// prefix so `make LINT_ALL=1 lint` resolves to the lint target.
var makeTarget = regexp.MustCompile("`make ((?:[A-Z_]+=[^ `]+ )*)([a-z][a-z-]*)`")

// TestDocsMakeTargetsExist guards the promise that every `make <target>` the docs
// tell a contributor to run is a target the Makefile actually defines. This is
// the cheapest possible defence against a renamed target: the docs said
// `make check` for months while nobody noticed it had grown a third dependency,
// and a rename would have been just as invisible.
func TestDocsMakeTargetsExist(t *testing.T) {
	data, err := os.ReadFile(filepath.Join(repoRoot, "Makefile"))
	if err != nil {
		t.Fatalf("read Makefile: %v", err)
	}
	defined := map[string]bool{}
	for line := range strings.SplitSeq(string(data), "\n") {
		if i := strings.IndexByte(line, ':'); i > 0 {
			name := strings.TrimSpace(line[:i])
			if name != "" && !strings.ContainsAny(name, " \t.$") {
				defined[name] = true
			}
		}
	}
	if len(defined) == 0 {
		t.Fatal("parsed no targets from the Makefile — the parser is broken, not the docs")
	}
	for _, doc := range markdownFiles(t) {
		body, readErr := os.ReadFile(filepath.Join(repoRoot, doc))
		if readErr != nil {
			t.Fatalf("read %s: %v", doc, readErr)
		}
		for _, m := range makeTarget.FindAllStringSubmatch(string(body), -1) {
			if !defined[m[2]] {
				t.Errorf("%s tells the reader to run `make %s`, which the Makefile does not define", doc, m[2])
			}
		}
	}
}

// retiredClaim is one statement the docs must never make again, with the reason
// it was retired. Each is LINE-SCOPED and requires several tokens to co-occur, so
// a doc can still discuss the retired behaviour historically — which these docs
// deliberately do, to stop it being reintroduced — without tripping the gate.
type retiredClaim struct {
	label string
	// wants must all appear on the line (lowercased) for the claim to match.
	wants []string
	// unless clears the match: the negated or historical form of the sentence.
	unless []string
	why    string
}

var retiredClaims = []retiredClaim{
	{
		label:  "the install flow is a function in the command tier",
		wants:  []string{"runinstall", "composes"},
		unless: []string{"adr-0005", "moved", "used to"},
		why: "ADR-0005 moved the flow behind install.Run in internal/install/flow.go; " +
			"cmd/villa/install.go wires Deps and renders the Result.",
	},
	{
		label:  "`make lint` falls back to `go vet`",
		wants:  []string{"make lint", "go vet"},
		unless: []string{"no fall back", "no `go vet` fallback", "could never fail", "deliberately"},
		why: "that fallback was deleted because it made the gate unable to fail: in `A && B || C` " +
			"a FAILING lint ran C and exited 0. It was documented as current in four files long after removal.",
	},
	{
		label:  "`golangci-lint` is optional locally / only if installed",
		wants:  []string{"golangci-lint", "if installed"},
		unless: []string{"nothing needs to be", "no local install"},
		why:    "`make lint` runs the pinned version through `go run`; nothing needs to be on PATH.",
	},
	{
		label: "this repo has no CI",
		// "no ci" alone over-matches ordinary prose ("needs no CI wiring"). Both
		// retired sentences claimed CI was not CONFIGURED, so require that word.
		wants:  []string{"no ci", "configured"},
		unless: []string{},
		why:    ".github/workflows/ci.yml runs on every push and pull request, and gates six checks.",
	},
	{
		label:  "there is no .github/workflows directory",
		wants:  []string{"no `.github", "directory"},
		unless: []string{},
		why:    ".github/workflows/ci.yml exists.",
	},
	{
		label: "`make check` is vet + test only",
		wants: []string{"make check", "vet", "test"},
		// "first half" is how the CI table honestly describes a step that mirrors PART
		// of check — that sentence is not claiming check stops at vet + test.
		unless: []string{"race", "first half"},
		why:    "`check` is vet + test + test-race; omitting the race gate hides CR-01/WR-04.",
	},
}

// TestDocsNoRetiredClaims fails the build when a doc reasserts something the tree
// has stopped supporting. The seed set is the five claims a full audit of this
// repo's docs actually found and killed — each had survived multiple milestones
// because prose has no compiler.
//
// When you retire a behaviour, add its claim here in the SAME commit that removes
// it. That is the only way this gate stays ahead of the docs rather than
// recording what someone already had to find by hand.
func TestDocsNoRetiredClaims(t *testing.T) {
	for _, doc := range markdownFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			low := strings.ToLower(line)
			for _, c := range retiredClaims {
				if !containsAll(low, c.wants) || containsAny(low, c.unless) {
					continue
				}
				t.Errorf("%s:%d reasserts a retired claim (%s): %s\n    line: %s",
					doc, n+1, c.label, c.why, strings.TrimSpace(line))
			}
		}
	}
}

func containsAll(s string, subs []string) bool {
	for _, sub := range subs {
		if !strings.Contains(s, strings.ToLower(sub)) {
			return false
		}
	}
	return len(subs) > 0
}

func containsAny(s string, subs []string) bool {
	for _, sub := range subs {
		if strings.Contains(s, strings.ToLower(sub)) {
			return true
		}
	}
	return false
}

// updateOutboundClaim is the SINGLE source of the dishonest-outbound regex for
// `villa update`. It is the docs-side twin of internal/websafe's injection-honesty
// gate: that one bans "injection-safe"/"immune"/"blocks injection" because the web
// guard flags and never blocks (ADR-0002); this one bans claims that update
// contacts nothing, because update demonstrably reaches the network.
//
// WHAT UPDATE ACTUALLY DOES, and why a blanket claim is always wrong: the check
// phase is one HTTPS GET to the release endpoint, made by villa; the fetch phase is
// `podman pull` against the registries in the compiled-in allowlist — gigabytes,
// multiple hosts, performed by PODMAN rather than by villa. A sentence that covers
// both with one figure is true of villa's process and false of the operation, which
// is the exact class of error where the accounting code is not the code doing the
// work. So the two claims are stated separately in README.md, and this gate stops a
// future contributor collapsing them back into a comfortable one-liner.
//
// SCOPING, deliberately narrow. The patterns anchor on update/`villa update` in the
// SAME line as the offending claim, so the gate cannot fire on the memory stack's
// legitimate "adds zero new outbound" (which is true: memory is a loopback vector
// store) or on the v1.4 coding agent's "proven zero-outbound at runtime" (also
// true, and proven by `villa verify agent`). Those are different subsystems making
// different, earned claims. A gate that flagged them would be noise a reviewer
// learns to ignore, which is worse than no gate — the same reasoning
// TestSeamGrepGate's scoping comment records.
func updateOutboundClaim() *regexp.Regexp {
	return regexp.MustCompile(`(?i)update[^.\n]*\b(fully offline|works offline|contacts nothing|reaches nothing|no outbound|zero outbound|without network|no network access)\b`)
}

// TestDocsUpdateOutboundHonesty fails when a doc claims `villa update` does not
// reach the network. Update contacts the release endpoint on check and every
// pinned registry on fetch; the honest framing is two separate bounded claims, not
// an absence.
//
// This is load-bearing rather than editorial, for the reason ADR-0002 gives about
// the injection guard: the operator's trust is calibrated by what the docs claim,
// and a claim that overstates the bound is worse than a claim that admits it. A
// user who believes update is offline will not think about when it last checked,
// which is precisely the thing they must think about — villa never checks on its
// own, so staleness is the operator's job to notice.
func TestDocsUpdateOutboundHonesty(t *testing.T) {
	pattern := updateOutboundClaim()
	for _, doc := range markdownFiles(t) {
		data, err := os.ReadFile(filepath.Join(repoRoot, doc))
		if err != nil {
			t.Fatalf("read %s: %v", doc, err)
		}
		for n, line := range strings.Split(string(data), "\n") {
			if pattern.MatchString(line) {
				t.Errorf("%s:%d claims `villa update` reaches nothing, which is false — "+
					"check is one GET to the release endpoint, fetch is `podman pull` against "+
					"the pinned registries. State the two bounds separately instead.\n    line: %s",
					doc, n+1, strings.TrimSpace(line))
			}
		}
	}
}
