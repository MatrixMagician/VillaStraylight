package main

// verify_search_json.go holds the BYTE-FROZEN `--json` view for `villa verify search`
// (schema v1, greenfield — no prior verify command emitted JSON, so there is no append-only
// constraint against an earlier verify schema; RESEARCH A5). The contract is guarded by
// TestVerifySearchJSON against cmd/villa/testdata/verify-search.json.golden — evolve it
// append-only and refreeze intentionally with `go test ./cmd/villa/ -run TestVerifySearchJSON -update`.

import (
	"encoding/json"
	"io"
)

// verifySearchJSONSchema is the AUTHORITATIVE schema version of the verify-search --json
// contract. It starts at 1 (greenfield). Any field addition is append-only + a schema bump.
const verifySearchJSONSchema = 1

// verifySearchView is the deterministic --json shape. The verdict is the three-state string
// (PASS/FAIL/REJECT — the operator-facing names, distinct from the internal enum) and detail
// is the refuse-with-remediation / pass text. Marshaled with indentation for a human-diffable
// golden; field order is fixed by the struct so the bytes are stable.
type verifySearchView struct {
	Schema  int    `json:"schema"`
	Verdict string `json:"verdict"`
	Detail  string `json:"detail"`
}

// verdictName maps the internal searchStatus to the byte-frozen operator-facing verdict
// string. Any value other than pass/fail is REJECT by construction so an unexpected status
// can never silently render as a green PASS.
func verdictName(s searchStatus) string {
	switch s {
	case searchPass:
		return "PASS"
	case searchFail:
		return "FAIL"
	default:
		return "REJECT"
	}
}

// renderVerifySearchJSON marshals the verdict to the byte-frozen schema-v1 contract and
// writes it (with a trailing newline) to w. It is deterministic (fixed field order, fixed
// indentation) so the golden compare is byte-stable.
func renderVerifySearchJSON(w io.Writer, proof searchProof) error {
	view := verifySearchView{
		Schema:  verifySearchJSONSchema,
		Verdict: verdictName(proof.status),
		Detail:  proof.detail,
	}
	b, err := json.MarshalIndent(view, "", "  ")
	if err != nil {
		return err
	}
	b = append(b, '\n')
	_, err = w.Write(b)
	return err
}
