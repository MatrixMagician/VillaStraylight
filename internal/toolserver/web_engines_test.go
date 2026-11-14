package toolserver

import (
	"net/http"
	"strings"
	"testing"
)

// searxWithEngines serves a SearXNG reply with the given results and unresponsive_engines
// value, as SearXNG writes it: each entry an [engine, reason] pair.
func searxWithEngines(results []searxItem, unresponsive string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		items := []string{}
		for _, i := range results {
			items = append(items, `{"url":"`+i.URL+`","title":"`+i.Title+`","content":"`+i.Content+`"}`)
		}
		_, _ = w.Write([]byte(`{"results":[` + strings.Join(items, ",") + `],"unresponsive_engines":` + unresponsive + `}`))
	}
}

// TestWebSearchEnginesRefusedIsNotAnEmptySuccess: when every engine refuses, SearXNG answers
// 200 with no results and names the engines in unresponsive_engines. Reading that as "nothing
// exists" would tell the user a search that never ran found nothing, so it is a 502 that names
// the engines. Zero results with no engine refusing stays an empty 200, and results with one
// engine refusing stay a normal 200.
func TestWebSearchEnginesRefusedIsNotAnEmptySuccess(t *testing.T) {
	live := []searxItem{{URL: "http://page.test/a", Title: "a", Content: "snippet a"}}
	long := strings.Repeat("x", 100)
	cases := []struct {
		name         string
		results      []searxItem
		unresponsive string
		status       int
		wantText     []string
		notText      []string
	}{
		{"all engines refused", nil, `[["brave","Suspended: too many requests"],["duckduckgo","CAPTCHA"]]`, 502,
			[]string{"search engines refused: brave (Suspended: too many requests), duckduckgo (CAPTCHA)"}, nil},
		{"no results and no engine refusing", nil, `[]`, 200, nil, nil},
		{"unresponsive field null", nil, `null`, 200, nil, nil},
		{"results with one engine refusing", live, `[["brave","Suspended: too many requests"]]`, 200, nil, []string{"brave"}},
		{"control characters are stripped", nil, `[["br\u001bave","\u001b[31mred\u0007"]]`, 502,
			[]string{"brave ([31mred)"}, []string{"\x1b", "\x07"}},
		{"each field is capped at 60 runes", nil, `[["` + long + `","` + long + `"]]`, 502,
			[]string{strings.Repeat("x", 60) + " (" + strings.Repeat("x", 60) + ")"}, []string{strings.Repeat("x", 61)}},
		{"at most four engines are named", nil, `[["e1","r"],["e2","r"],["e3","r"],["e4","r"],["e5","r"],["e6","r"]]`, 502,
			[]string{"e1 (r), e2 (r), e3 (r), e4 (r) and 2 more"}, []string{"e5 (", "e6 ("}},
		{"an entry without a reason names the engine only", nil, `[["brave"]]`, 502,
			[]string{"search engines refused: brave"}, []string{"()"}},
		{"entries that are not pairs of strings are ignored", nil, `[["brave","CAPTCHA"],"x",[1,2],[]]`, 502,
			[]string{"search engines refused: brave (CAPTCHA)"}, nil},
	}
	for _, c := range cases {
		e := newEnv(t, SetWeb, func(e *env) {
			e.b.handle(searxngHost, searxWithEngines(c.results, c.unresponsive))
			e.b.handle("page.test", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusInternalServerError) })
		})
		rec := e.get("/web/search?q=x")
		if rec.Code != c.status {
			t.Errorf("%s: status %d, want %d (%s)", c.name, rec.Code, c.status, rec.Body.String())
			continue
		}
		body := rec.Body.String()
		if c.status == 200 && !strings.HasPrefix(body, `{"results":[`) {
			t.Errorf("%s: a 200 must keep the results shape: %s", c.name, body)
		}
		if c.status == 502 {
			if text := errorBody(t, rec); !strings.HasPrefix(text, "search engines refused: ") {
				t.Errorf("%s: refusal text %q", c.name, text)
			}
		}
		for _, w := range c.wantText {
			if !strings.Contains(body, w) {
				t.Errorf("%s: body %s lacks %q", c.name, body, w)
			}
		}
		for _, n := range c.notText {
			if strings.Contains(body, n) {
				t.Errorf("%s: body %s holds %q", c.name, body, n)
			}
		}
	}
}

// TestWebSearchEnginesRefusedDoesNotEchoTheQuery: the 502 names SearXNG's own engine text and
// nothing the caller sent.
func TestWebSearchEnginesRefusedDoesNotEchoTheQuery(t *testing.T) {
	const canary = "CANARY-5c1e"
	e := newEnv(t, SetWeb, func(e *env) {
		e.b.handle(searxngHost, searxWithEngines(nil, `[["brave","CAPTCHA"]]`))
	})
	rec := e.get("/web/search?q=" + canary)
	mustStatus(t, rec, http.StatusBadGateway)
	if strings.Contains(rec.Body.String(), canary) {
		t.Errorf("the refusal echoes the query: %s", rec.Body.String())
	}
}
