package eval

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"testing"
)

// TestSuiteVersionPinsTheCases guards ADR-0018's suite version: editing, adding or
// removing a capability case must bump SuiteVersion, because a baseline recorded
// under one suite must never be compared against another. The sha256 of the
// embedded cases.json is pinned beside the version; a changed file fails here until
// both are moved together.
func TestSuiteVersionPinsTheCases(t *testing.T) {
	sum := sha256.Sum256(suiteJSON)
	if got := hex.EncodeToString(sum[:]); got != suiteSHA256 {
		t.Fatalf("cases.json changed (sha256 %s, pinned %s for suite version %d): bump SuiteVersion "+
			"and re-pin suiteSHA256 together — every eval baseline recorded under the old suite is "+
			"orphaned by design", got, suiteSHA256, SuiteVersion)
	}
}

// TestEmbeddedSuiteLoadsFailClosed guards ADR-0018's "cases are data": the embedded
// suite must load, every case must name a grader kind the table holds and carry what
// that kind needs, and the suite must span the categories it was written for. A
// typo'd kind or field is a load failure here, never a case that silently passes.
func TestEmbeddedSuiteLoadsFailClosed(t *testing.T) {
	cases, err := Suite()
	if err != nil {
		t.Fatalf("Suite: %v", err)
	}
	if len(cases) < 16 {
		t.Fatalf("suite holds %d capability cases, want at least 16", len(cases))
	}
	prefixes := map[string]int{}
	tools := 0
	for _, c := range cases {
		prefixes[strings.SplitN(c.ID, "-", 2)[0]]++
		if len(c.Tools) > 0 {
			tools++
		}
	}
	for _, p := range []string{"arith", "format", "extract", "json", "code", "units", "tool"} {
		if prefixes[p] == 0 {
			t.Errorf("no capability case in category %q (ids by prefix: %v)", p, prefixes)
		}
	}
	if tools < 4 {
		t.Errorf("%d tool-call capability cases, want at least 4", tools)
	}
}

// TestParseSuiteRefusesMalformedCases: every rule the loader enforces refuses on its
// own — an unknown kind, an unknown field, a duplicate id, and each requirement a
// kind declares (ADR-0018: an unknown kind in the suite is a load failure, never a
// silent pass).
func TestParseSuiteRefusesMalformedCases(t *testing.T) {
	const tool = `"tools":[{"type":"function","function":{"name":"f"}}]`
	for _, c := range []struct {
		name, json, wantErr string
	}{
		{"not json", `{`, "parse suite"},
		{"unknown field", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"exact","want":"x","patern":"y"}}]`, "unknown field"},
		{"unknown kind", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"vibes"}}]`, "unknown grader kind"},
		{"no id", `[{"prompt":"p","max_tokens":8,"grader":{"kind":"exact","want":"x"}}]`, "an id"},
		{"no prompt", `[{"id":"a","max_tokens":8,"grader":{"kind":"exact","want":"x"}}]`, "a prompt"},
		{"no token bound", `[{"id":"a","prompt":"p","grader":{"kind":"exact","want":"x"}}]`, "max_tokens"},
		{"exact without want", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"exact"}}]`, "grader.want"},
		{"regex that does not compile", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"regex","pattern":"("}}]`, "grader.pattern"},
		{"json without keys", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"json"}}]`, "grader.keys"},
		{"tool without tools", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"tool","want":"f","keys":{"x":1}}}]`, "tools"},
		{"no_tool without tools", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"no_tool","pattern":"x"}}]`, "tools"},
		{"duplicate id", `[{"id":"a","prompt":"p","max_tokens":8,"grader":{"kind":"exact","want":"x"}},` +
			`{"id":"a","prompt":"q","max_tokens":8,"grader":{"kind":"exact","want":"y"}}]`, "duplicate"},
	} {
		t.Run(c.name, func(t *testing.T) {
			if _, err := parseSuite([]byte(c.json)); err == nil || !strings.Contains(err.Error(), c.wantErr) {
				t.Fatalf("parseSuite err = %v, want one containing %q", err, c.wantErr)
			}
		})
	}

	ok := `[{"id":"a","prompt":"p","max_tokens":8,` + tool + `,"grader":{"kind":"tool","want":"f","keys":{"x":1}}}]`
	if _, err := parseSuite([]byte(ok)); err != nil {
		t.Fatalf("a well-formed tool case was refused: %v", err)
	}
}
