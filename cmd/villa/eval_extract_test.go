package main

import (
	"strings"
	"testing"
)

// TestEvalConductsExtractCasesWhenTheGateIsOn: the verb answers the extractor gate
// from the config it loaded, so with memory and the extractor on the extract case
// is conducted through the extract seam and graded, while a memory-only config
// skips it (ADR-0029).
func TestEvalConductsExtractCasesWhenTheGateIsOn(t *testing.T) {
	all := map[string]string{"pa": "4", "pb": "30", "pc": "x"}
	cfg := evalTestConfig()
	cfg.MemoryEnabled, cfg.Extractor = true, true

	_, out, _ := runEvalCmd(false, true, fakeEvalDeps(cfg, &evalStoreMem{}, all, nil))
	if !strings.Contains(out, `"case_id": "extract-f",
      "status": "passed",
      "excerpt": "Pre-heat: run the timer for 6 minutes below -12 degrees."`) {
		t.Errorf("extractor on: the extract case was not conducted and passed:\n%s", out)
	}

	cfg.Extractor = false
	_, out, _ = runEvalCmd(false, true, fakeEvalDeps(cfg, &evalStoreMem{}, all, nil))
	if !strings.Contains(out, `"case_id": "extract-f",
      "status": "skipped",
      "detail": "extractor off"`) {
		t.Errorf("memory without the extractor: the extract case was not skipped:\n%s", out)
	}
}
