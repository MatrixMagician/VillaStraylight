package dashboard

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// journalLine builds one `journalctl -o json` fixture line with a string MESSAGE.
func journalLine(t *testing.T, realtimeUS, unit, message string) string {
	t.Helper()
	b, err := json.Marshal(struct {
		RealTime string `json:"__REALTIME_TIMESTAMP"`
		Unit     string `json:"_SYSTEMD_USER_UNIT"`
		Message  string `json:"MESSAGE"`
	}{realtimeUS, unit, message})
	if err != nil {
		t.Fatalf("marshal fixture line: %v", err)
	}
	return string(b)
}

// TestParseJournalJSONRealFixture asserts a real multi-line `-o json` capture
// reduces to oldest-first lines with the unit's .service suffix stripped.
func TestParseJournalJSONRealFixture(t *testing.T) {
	text := strings.Join([]string{
		journalLine(t, "1000000", "villa-llama.service", "server started"),
		journalLine(t, "2000000", "villa-openwebui.service", "listening on :8080"),
		journalLine(t, "3000000", "villa-llama.service", "model loaded"),
	}, "\n")

	got := ParseJournalJSON(text, 10)
	if !got.Available {
		t.Fatalf("expected Available=true, got %+v", got)
	}
	want := []JournalLine{
		{At: formatJournalTime("1000000"), Unit: "villa-llama", Message: "server started"},
		{At: formatJournalTime("2000000"), Unit: "villa-openwebui", Message: "listening on :8080"},
		{At: formatJournalTime("3000000"), Unit: "villa-llama", Message: "model loaded"},
	}
	if len(got.Lines) != len(want) {
		t.Fatalf("got %d lines, want %d: %+v", len(got.Lines), len(want), got.Lines)
	}
	for i, w := range want {
		if got.Lines[i] != w {
			t.Errorf("line[%d] = %+v, want %+v", i, got.Lines[i], w)
		}
	}
}

// TestParseJournalJSONSkipsMalformedLine asserts one bad line does not blank the
// whole view — the surrounding good lines still come through.
func TestParseJournalJSONSkipsMalformedLine(t *testing.T) {
	text := strings.Join([]string{
		journalLine(t, "1000000", "villa-llama.service", "ok before"),
		`{not valid json`,
		journalLine(t, "2000000", "villa-llama.service", "ok after"),
	}, "\n")

	got := ParseJournalJSON(text, 10)
	if !got.Available || len(got.Lines) != 2 {
		t.Fatalf("got %+v, want 2 lines with the malformed one dropped", got)
	}
	if got.Lines[0].Message != "ok before" || got.Lines[1].Message != "ok after" {
		t.Fatalf("unexpected lines: %+v", got.Lines)
	}
}

// TestParseJournalJSONSkipsByteArrayMessage covers journald's non-UTF-8 encoding:
// MESSAGE becomes an array of byte numbers instead of a string, and that record
// must be dropped rather than crash the reduction.
func TestParseJournalJSONSkipsByteArrayMessage(t *testing.T) {
	text := strings.Join([]string{
		`{"__REALTIME_TIMESTAMP":"1000000","_SYSTEMD_USER_UNIT":"villa-llama.service","MESSAGE":[104,105]}`,
		journalLine(t, "2000000", "villa-llama.service", "the only good line"),
	}, "\n")

	got := ParseJournalJSON(text, 10)
	if !got.Available || len(got.Lines) != 1 {
		t.Fatalf("got %+v, want exactly 1 surviving line", got)
	}
	if got.Lines[0].Message != "the only good line" {
		t.Fatalf("unexpected line: %+v", got.Lines[0])
	}
}

// TestParseJournalJSONEmptyInput asserts no input yields the unavailable zero
// view, never an empty-but-available one (which would read as a quiet stack).
func TestParseJournalJSONEmptyInput(t *testing.T) {
	got := ParseJournalJSON("", 10)
	if got.Available || len(got.Lines) != 0 {
		t.Fatalf("empty input should yield the unavailable zero view, got %+v", got)
	}
}

// TestParseJournalJSONMaxBound asserts more input than max keeps the MOST RECENT
// max lines, oldest-first order intact — not the first max seen.
func TestParseJournalJSONMaxBound(t *testing.T) {
	var lines []string
	for i := 1; i <= 5; i++ {
		lines = append(lines, journalLine(t, fmt.Sprintf("%d000000", i), "villa-llama.service", fmt.Sprintf("line %d", i)))
	}
	got := ParseJournalJSON(strings.Join(lines, "\n"), 2)
	if len(got.Lines) != 2 {
		t.Fatalf("got %d lines, want 2 (max bound)", len(got.Lines))
	}
	if got.Lines[0].Message != "line 4" || got.Lines[1].Message != "line 5" {
		t.Fatalf("max bound should keep the most recent lines oldest-first, got %+v", got.Lines)
	}
}

// TestHandleJournalFoldsInjectedSeam asserts GET /api/journal serializes whatever
// the injected Journal seam returns, unmodified — the handler does no parsing.
func TestHandleJournalFoldsInjectedSeam(t *testing.T) {
	want := JournalView{Available: true, Lines: []JournalLine{{At: "10:00:00", Unit: "villa-llama", Message: "hello"}}}
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
		Journal:       func() JournalView { return want },
	})

	req := httptest.NewRequest(http.MethodGet, "/api/journal", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("journal code = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var got JournalView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if got.Available != want.Available || len(got.Lines) != len(want.Lines) || got.Lines[0] != want.Lines[0] {
		t.Fatalf("got %+v, want %+v", got, want)
	}
}

// TestHandleJournalNilSeamDefaultsToUnavailable proves a Server built without
// Config.Journal renders unavailable rather than nil-panicking.
func TestHandleJournalNilSeamDefaultsToUnavailable(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})

	req := httptest.NewRequest(http.MethodGet, "/api/journal", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("journal code = %d, want 200", rec.Code)
	}
	var got JournalView
	if err := json.Unmarshal(rec.Body.Bytes(), &got); err != nil {
		t.Fatalf("decode body: %v\n%s", err, rec.Body.String())
	}
	if got.Available {
		t.Fatalf("nil seam should render unavailable, got %+v", got)
	}
}
