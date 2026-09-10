package grounding

import (
	"os"
	"testing"
)

// TestParseReportPrototypeOutputs is the parse table: the four real prototype
// outputs (docs/prototype/cowork/results/audit-*.txt), copied into testdata,
// parsed against the counts actually present in each raw file (the parser
// counts each claim's own UNSUPPORTED marker; it does not trust the model's
// self-reported TOTAL line). The spec's table states 12 unsupported for the
// stand-in memo and 4 for the reconciliation; the raw files only carry 10 and
// 5 UNSUPPORTED markers respectively. The model's own closing tally is
// itself an arithmetic slip in both directions, which is the failure mode
// this audit exists to catch. Per the ticket, the fixtures stay truthful to
// the raw output and this test asserts the real counts, not the spec table's.
func TestParseReportPrototypeOutputs(t *testing.T) {
	cases := []struct {
		name        string
		file        string
		claims      int
		unsupported int
	}{
		{"invented facts (stand-in)", "testdata/audit-standin-T4.txt", 19, 10},
		{"reconciliation, wrong totals row", "testdata/audit-T2.txt", 23, 5},
		{"action items", "testdata/audit-T3.txt", 17, 1},
		{"clean memo", "testdata/audit-T4.txt", 12, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			out := readFixture(t, tc.file)
			rep := parseReport(out)
			if !rep.Checked {
				t.Fatalf("Checked = false, want true (Err=%q)", rep.Err)
			}
			if rep.Err != "" {
				t.Errorf("Err = %q, want empty for a complete response", rep.Err)
			}
			if rep.Claims != tc.claims {
				t.Errorf("Claims = %d, want %d", rep.Claims, tc.claims)
			}
			if len(rep.Unsupported) != tc.unsupported {
				t.Errorf("len(Unsupported) = %d, want %d", len(rep.Unsupported), tc.unsupported)
			}
		})
	}
}

// TestParseReportEmpty guards that an empty model answer never produces a
// silently-passing report.
func TestParseReportEmpty(t *testing.T) {
	rep := parseReport(readFixture(t, "testdata/empty.txt"))
	if rep.Checked {
		t.Fatal("Checked = true, want false for an empty answer")
	}
	if rep.Err == "" {
		t.Error("Err is empty, want a reason")
	}
}

// TestParseReportPreambleOnly guards an answer with prose but zero
// recognisable claims: it must not be reported as a clean (zero-claim) audit.
func TestParseReportPreambleOnly(t *testing.T) {
	rep := parseReport(readFixture(t, "testdata/preamble-only.txt"))
	if rep.Checked {
		t.Fatal("Checked = true, want false for a preamble with no claims")
	}
	if rep.Err == "" {
		t.Error("Err is empty, want a reason")
	}
}

// TestParseReportTruncated guards a response that ends mid-claim (no TOTAL
// line): the claims parsed so far are kept, and Err notes the truncation so
// the runner flags the task instead of trusting a partial count.
func TestParseReportTruncated(t *testing.T) {
	rep := parseReport(readFixture(t, "testdata/truncated.txt"))
	if !rep.Checked {
		t.Fatalf("Checked = false, want true (partial claims are still reported); Err=%q", rep.Err)
	}
	if rep.Claims != 5 {
		t.Errorf("Claims = %d, want 5", rep.Claims)
	}
	if rep.Err == "" {
		t.Error("Err is empty, want a truncation note")
	}
}

func readFixture(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("ReadFile(%s): %v", path, err)
	}
	return string(b)
}
