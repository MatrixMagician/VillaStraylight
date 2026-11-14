package main

import (
	"context"
	"encoding/json"
	"errors"
	"slices"
	"strings"
	"testing"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
)

// webCurlCall is one in-network curl the fake saw.
type webCurlCall struct {
	stdin string
	args  []string
}

func fakeWebCurl(out string, code int, err error, calls *[]webCurlCall) webCurl {
	return func(_ context.Context, stdin []byte, args []string) ([]byte, int, error) {
		*calls = append(*calls, webCurlCall{string(stdin), args})
		return []byte(out), code, err
	}
}

func noComplete(context.Context, config.VillaConfig, llm.ChatRequest) (string, error) {
	return "", errors.New("unused")
}

// TestSearchSendsTheQueryOnStdinNotArgv guards #252's doctrine for the research
// client: the question's words go to SearXNG in a curl config on stdin, so they
// never appear on a command line another local user can read, and the URL is the
// config-resolved SearXNG address.
func TestSearchSendsTheQueryOnStdinNotArgv(t *testing.T) {
	var calls []webCurlCall
	d := webDeps(fakeWebCurl(`{"results":[{"url":"https://a.example/x","title":"A","content":"snippet"},{"url":"ftp://b.example/"},{"url":"https://c.example/y"},{"url":""}]}`, 0, nil, &calls), noComplete)
	urls, err := d.Search(t.Context(), config.VillaConfig{}, `rocm "7.2" \ notes`)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(urls, []string{"https://a.example/x", "https://c.example/y"}) {
		t.Errorf("urls = %v, want only the http(s) result URLs", urls)
	}
	if len(calls) != 1 || !slices.Equal(calls[0].args, []string{"-K", "-"}) {
		t.Fatalf("curl args = %v, want only -K -", calls)
	}
	for _, want := range []string{
		`url = "http://villa-searxng:8080/search"`,
		`data-urlencode = "q=rocm \"7.2\" \\ notes"`,
		`data-urlencode = "format=json"`,
		"get", "fail",
	} {
		if !strings.Contains(calls[0].stdin, want) {
			t.Errorf("curl config lacks %q:\n%s", want, calls[0].stdin)
		}
	}
}

// TestSearchNamesEnginesThatRefused: SearXNG answers 200 with no results when every
// engine behind it refuses (measured on the dev host: brave "Suspended: too many
// requests", duckduckgo "CAPTCHA"). That is the search not answering, not a question
// with no hits, so it is an error naming the engines. Empty results with no refusing
// engine stay a plain empty result.
func TestSearchNamesEnginesThatRefused(t *testing.T) {
	refused := `{"results":[],"unresponsive_engines":[["brave","Suspended: too many requests"],["duckduckgo","CAPTCHA"]]}`
	_, err := searxngSearch(context.Background(), fakeWebCurl(refused, 0, nil, new([]webCurlCall)), "q")
	if err == nil || !strings.Contains(err.Error(), "brave (Suspended: too many requests)") || !strings.Contains(err.Error(), "duckduckgo (CAPTCHA)") {
		t.Fatalf("err = %v, want it to name both refusing engines", err)
	}

	urls, err := searxngSearch(context.Background(), fakeWebCurl(`{"results":[],"unresponsive_engines":[]}`, 0, nil, new([]webCurlCall)), "q")
	if err != nil || len(urls) != 0 {
		t.Fatalf("no hits and no refusing engine: urls=%v err=%v, want empty and nil", urls, err)
	}

	// One engine refusing while another answers is a normal search.
	partial := `{"results":[{"url":"https://example.org/a"}],"unresponsive_engines":[["brave","timeout"]]}`
	urls, err = searxngSearch(context.Background(), fakeWebCurl(partial, 0, nil, new([]webCurlCall)), "q")
	if err != nil || len(urls) != 1 {
		t.Fatalf("partial refusal: urls=%v err=%v, want one URL and nil", urls, err)
	}

	// The engine text is SearXNG's, but it is still printed to a terminal: control
	// characters are dropped and it is cut short.
	noisy := `{"results":[],"unresponsive_engines":[["bra\u001b[31mve","` + strings.Repeat("x", 500) + `"]]}`
	_, err = searxngSearch(context.Background(), fakeWebCurl(noisy, 0, nil, new([]webCurlCall)), "q")
	if err == nil || strings.ContainsRune(err.Error(), 0x1b) || len(err.Error()) > 400 {
		t.Fatalf("err = %q, want control characters dropped and the text bounded", err)
	}
}

// TestSearchReportsAnUnreachableSearxng guards that a curl failure is an error
// naming the exit code (the loop turns it into a failed task with remediation),
// and an unreadable answer is an error too, never an empty result.
func TestSearchReportsAnUnreachableSearxng(t *testing.T) {
	var calls []webCurlCall
	d := webDeps(fakeWebCurl("", 7, errors.New("exit status 7"), &calls), noComplete)
	if _, err := d.Search(t.Context(), config.VillaConfig{}, "q"); err == nil || !strings.Contains(err.Error(), "curl exit 7") {
		t.Errorf("err = %v, want curl exit 7", err)
	}
	d = webDeps(fakeWebCurl("<html>not json</html>", 0, nil, &calls), noComplete)
	if _, err := d.Search(t.Context(), config.VillaConfig{}, "q"); err == nil {
		t.Error("an unreadable SearXNG answer must be an error, not zero results")
	}
	d = webDeps(fakeWebCurl(`{"results":[]}`, 0, nil, &calls), noComplete)
	if urls, err := d.Search(t.Context(), config.VillaConfig{}, "q"); err != nil || len(urls) != 0 {
		t.Errorf("an empty result list is no URLs and no error, got %v, %v", urls, err)
	}
}

// TestFetchSendsTheBearerAndURLsOnStdin guards the loader call: the Open WebUI
// contract (POST {"urls":[...]} with the bearer), with the secret in the curl
// config on stdin, never in argv, and the answer mapped to pages with websafe's
// guard verdict.
func TestFetchSendsTheBearerAndURLsOnStdin(t *testing.T) {
	var calls []webCurlCall
	reply := `[{"page_content":"FENCED ONE","metadata":{"source":"https://a.example/x","title":"A","guard":{"detected":false}}},` +
		`{"page_content":"FENCED TWO","metadata":{"source":"https://b.example/","title":"B","guard":{"detected":true,"rules":["override"]}}}]`
	d := webDeps(fakeWebCurl(reply, 0, nil, &calls), noComplete)
	cfg := config.VillaConfig{WebLoaderSecret: "s3cret-token"}
	pages, err := d.Fetch(t.Context(), cfg, []string{"https://a.example/x", "https://b.example/"})
	if err != nil {
		t.Fatal(err)
	}
	if len(pages) != 2 || pages[0].Content != "FENCED ONE" || pages[0].Flagged || !pages[1].Flagged || pages[1].Rules[0] != "override" || pages[1].URL != "https://b.example/" {
		t.Errorf("pages = %+v", pages)
	}
	c := calls[0]
	if !slices.Equal(c.args, []string{"-K", "-"}) || strings.Contains(strings.Join(c.args, " "), "s3cret-token") {
		t.Errorf("args = %v, want -K - with no secret", c.args)
	}
	for _, want := range []string{`url = "http://villa-websafe:8090/load"`, `header = "Authorization: Bearer s3cret-token"`, `header = "Content-Type: application/json"`} {
		if !strings.Contains(c.stdin, want) {
			t.Errorf("curl config lacks %q:\n%s", want, c.stdin)
		}
	}
	data := lineValue(t, c.stdin, "data")
	var body struct{ URLs []string }
	if err := json.Unmarshal([]byte(unquoteCurl(data)), &body); err != nil || len(body.URLs) != 2 {
		t.Errorf("data = %q: %v", data, err)
	}
}

// TestFetchWithoutASecretSendsNoAuthorization guards the documented empty-secret
// posture: no header at all rather than an empty bearer.
func TestFetchWithoutASecretSendsNoAuthorization(t *testing.T) {
	var calls []webCurlCall
	d := webDeps(fakeWebCurl(`[]`, 0, nil, &calls), noComplete)
	pages, err := d.Fetch(t.Context(), config.VillaConfig{}, []string{"https://a.example/"})
	if err != nil || len(pages) != 0 {
		t.Errorf("pages = %v, err = %v; an empty array is no pages and no error", pages, err)
	}
	if strings.Contains(calls[0].stdin, "Authorization") {
		t.Errorf("curl config carries an Authorization header with no secret:\n%s", calls[0].stdin)
	}
}

// TestFetchFailureIsAnError guards that a 401, a refused connection or a garbled
// answer from the loader is an error, which the loop reads as "the loader is not
// answering", never as a page-less success.
func TestFetchFailureIsAnError(t *testing.T) {
	var calls []webCurlCall
	for name, d := range map[string]struct {
		out  string
		code int
		err  error
	}{
		"http error": {"", 22, errors.New("exit status 22")},
		"refused":    {"", 7, errors.New("exit status 7")},
		"garbled":    {"oops", 0, nil},
	} {
		w := webDeps(fakeWebCurl(d.out, d.code, d.err, &calls), noComplete)
		if _, err := w.Fetch(t.Context(), config.VillaConfig{}, []string{"https://a.example/"}); err == nil {
			t.Errorf("%s: Fetch must fail", name)
		}
	}
}

// TestCurlConfigQuoteEscapes guards the one place web-adjacent text becomes curl
// config syntax: a quote, a backslash or a line break in a value cannot end the
// value or smuggle in an option of its own.
func TestCurlConfigQuoteEscapes(t *testing.T) {
	in := "a\"b\\c\nurl = \"http://evil/\"\r\td"
	got := curlConfigQuote(in)
	if strings.ContainsAny(got, "\n\r") {
		t.Errorf("a raw line break survived: %q", got)
	}
	if got != `"a\"b\\c\nurl = \"http://evil/\"\r\td"` {
		t.Errorf("got %s", got)
	}
}

// lineValue returns the quoted value after "<name> = " on the first config line
// that starts with it.
func lineValue(t *testing.T, config, name string) string {
	t.Helper()
	for _, l := range strings.Split(config, "\n") {
		if v, ok := strings.CutPrefix(l, name+" = "); ok {
			return v
		}
	}
	t.Fatalf("no %q line in %s", name, config)
	return ""
}

// unquoteCurl reverses curlConfigQuote for the escapes the tests use.
func unquoteCurl(s string) string {
	s = strings.TrimSuffix(strings.TrimPrefix(s, `"`), `"`)
	return strings.NewReplacer(`\"`, `"`, `\\`, `\`, `\n`, "\n").Replace(s)
}
