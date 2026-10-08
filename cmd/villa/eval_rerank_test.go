package main

import (
	"strings"
	"testing"
)

// TestParseRerankScoresMapsResultsToDocumentOrder: llama-server answers a rerank
// request with one result per document carrying its index and a raw score, in
// score order; the seam hands the suite one score per document in document order,
// and refuses an answer that does not cover every document.
func TestParseRerankScoresMapsResultsToDocumentOrder(t *testing.T) {
	out := []byte(`{"model":"bge-reranker-v2-m3","object":"list","usage":{"prompt_tokens":92,"total_tokens":92},` +
		`"results":[{"index":2,"relevance_score":-1.144},{"index":0,"relevance_score":-1.515},{"index":1,"relevance_score":-4.008}]}`)
	got, err := parseRerankScores(out, 3)
	if err != nil {
		t.Fatalf("parseRerankScores: %v", err)
	}
	if len(got) != 3 || got[0] != -1.515 || got[1] != -4.008 || got[2] != -1.144 {
		t.Errorf("scores = %v, want [-1.515 -4.008 -1.144] in document order", got)
	}

	for name, bad := range map[string]string{
		"not json":           `{`,
		"missing a result":   `{"results":[{"index":0,"relevance_score":1}]}`,
		"index past the end": `{"results":[{"index":0,"relevance_score":1},{"index":2,"relevance_score":1}]}`,
		"no results":         `{"error":{"message":"input is too large to process"}}`,
	} {
		if _, err := parseRerankScores([]byte(bad), 2); err == nil {
			t.Errorf("%s: parseRerankScores accepted %s", name, bad)
		}
	}
}

// TestEvalConductsRerankCasesWhenTheGateIsOn: the verb answers the rerank gate
// from the config it loaded, so with memory and the reranker on the rerank case is
// conducted through the rerank seam and graded, while a memory-only config skips it.
func TestEvalConductsRerankCasesWhenTheGateIsOn(t *testing.T) {
	all := map[string]string{"pa": "4", "pb": "30", "pc": "x"}
	cfg := evalTestConfig()
	cfg.MemoryEnabled, cfg.Reranker = true, true

	_, out, _ := runEvalCmd(false, true, fakeEvalDeps(cfg, &evalStoreMem{}, all, nil))
	if !strings.Contains(out, `"case_id": "retrieval-e",
      "status": "passed",
      "excerpt": "top 1 (scores -2.000 0.500)"`) {
		t.Errorf("reranker on: the rerank case was not conducted and passed:\n%s", out)
	}

	cfg.Reranker = false
	_, out, _ = runEvalCmd(false, true, fakeEvalDeps(cfg, &evalStoreMem{}, all, nil))
	if !strings.Contains(out, `"case_id": "retrieval-e",
      "status": "skipped",
      "detail": "reranker off"`) {
		t.Errorf("memory without the reranker: the rerank case was not skipped:\n%s", out)
	}
}
