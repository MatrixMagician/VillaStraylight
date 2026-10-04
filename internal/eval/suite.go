package eval

// suite.go embeds the villa-authored capability suite and loads it fail-closed: an
// unknown grader kind, an unknown field, a duplicate id, or a case missing what its
// kind needs is a load error, never a case that silently passes (ADR-0018).

import (
	"bytes"
	_ "embed"
	"encoding/json"
	"fmt"
	"regexp"
)

//go:embed cases.json
var suiteJSON []byte

// SuiteVersion identifies the embedded suite. It changes whenever a capability case
// is added, removed or edited, which orphans every eval baseline recorded under the
// old version: a baseline is never compared across suites.
//
// suiteSHA256 is the sha256 of cases.json at this version. TestSuiteVersionPinsTheCases
// fails when the file changes, so the two move together.
const (
	SuiteVersion = 1
	suiteSHA256  = "4780ddc5b0e966c9d425dcba10ff3c63cc671c76b887a4b0fdf1720fd8bfc2f6"
)

// Suite returns the embedded capability cases in suite order.
func Suite() ([]Case, error) { return parseSuite(suiteJSON) }

// parseSuite decodes a suite strictly and validates every case.
func parseSuite(data []byte) ([]Case, error) {
	var cases []Case
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&cases); err != nil {
		return nil, fmt.Errorf("eval: parse suite: %w", err)
	}
	if err := checkSuite(cases); err != nil {
		return nil, err
	}
	return cases, nil
}

// checkSuite validates each case and refuses a duplicate id.
func checkSuite(cases []Case) error {
	seen := make(map[string]bool, len(cases))
	for _, c := range cases {
		if err := validate(c); err != nil {
			return err
		}
		if seen[c.ID] {
			return fmt.Errorf("eval: duplicate capability case id %q", c.ID)
		}
		seen[c.ID] = true
	}
	return nil
}

// validate refuses a case whose kind the grader table does not hold, or which lacks
// something every case or its kind needs.
func validate(c Case) error {
	row, ok := graders[c.Grader.Kind]
	if !ok {
		return fmt.Errorf("eval: capability case %q: unknown grader kind %q", c.ID, c.Grader.Kind)
	}
	if need := missing(c, row); need != "" {
		return fmt.Errorf("eval: capability case %q (grader %q) needs %s", c.ID, c.Grader.Kind, need)
	}
	return nil
}

// missing names the first requirement c does not meet, or "".
func missing(c Case, row grader) string {
	for _, r := range []struct {
		required, met bool
		what          string
	}{
		{true, c.ID != "", "an id"},
		{true, c.Prompt != "", "a prompt"},
		{true, c.MaxTokens > 0, "a positive max_tokens"},
		{row.want, c.Grader.Want != "", "grader.want"},
		{row.pattern, compiles(c.Grader.Pattern), "a compilable grader.pattern"},
		{row.keys, len(c.Grader.Keys) > 0, "grader.keys"},
		{row.tools, len(c.Tools) > 0, "tools"},
	} {
		if r.required && !r.met {
			return r.what
		}
	}
	return ""
}

// compiles reports whether pattern is a non-empty, valid regular expression.
func compiles(pattern string) bool {
	_, err := regexp.Compile(pattern)
	return pattern != "" && err == nil
}
