package toolserver

// web.go is villa_web_search. The tool takes a query and nothing else: it sends the query
// to SearXNG as GET /search?q=<q>&format=json, keeps the first count results whose URL the
// loader would accept, fetches them through websafe.Loader over the SSRF-guarded client,
// and returns each result's text guarded and fenced. No parameter, header or result field
// reaches a URL the unit requests except through those steps.

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

type webResult struct {
	Title     string          `json:"title"`
	URL       string          `json:"url"`
	Content   string          `json:"content"`
	Fetched   bool            `json:"fetched"`
	Truncated bool            `json:"truncated"`
	Guard     websafe.Verdict `json:"guard"`
}

type webResponse struct {
	Results []webResult `json:"results"`
}

// searxResult is one entry of SearXNG's format=json results[]. Only these fields are read.
type searxResult struct {
	URL     string `json:"url"`
	Title   string `json:"title"`
	Content string `json:"content"`
}

func (s *Server) webSearch(c *call) outcome {
	items, refused, err := s.searxng(c.ctx, c.q)
	if err != nil {
		return fail(c.ctx)
	}

	cands := make([]searxResult, 0, c.count)
	for _, it := range items {
		if len(cands) == c.count {
			break
		}
		if acceptableResultURL(it.URL) {
			cands = append(cands, it)
		}
	}

	var urls []string
	seen := map[string]bool{}
	for _, it := range cands {
		if !seen[it.URL] {
			seen[it.URL] = true
			urls = append(urls, it.URL)
		}
	}
	pages := map[string]websafe.Page{}
	for _, p := range s.loader.Load(c.ctx, urls) {
		pages[p.Source] = p
	}
	if c.ctx.Err() != nil {
		return fail(c.ctx)
	}

	out := webResponse{Results: []webResult{}}
	flagged := 0
	for _, it := range cands {
		res, ok := buildWebResult(it, pages)
		if !ok {
			continue
		}
		if res.Guard.Detected {
			flagged++
		}
		out.Results = append(out.Results, res)
	}
	// An empty answer is only an answer when no engine refused to give one. SearXNG answers
	// 200 with no results when every engine is suspended or behind a CAPTCHA, and passing that
	// on as {"results": []} would tell the model, and through it the user, that nothing exists.
	if len(out.Results) == 0 && len(refused) > 0 {
		return outcome{status: http.StatusBadGateway, body: map[string]string{"error": enginesRefusedText(refused)}}
	}
	return outcome{status: http.StatusOK, body: out, results: len(out.Results), flagged: flagged}
}

// Limits on the engine text a refusal repeats. The text is SearXNG's, not the caller's, but it
// is still upstream text that Open WebUI hands to the model.
const (
	refusedFieldRunes = 60
	refusedMaxEngines = 4
)

// engineRefusal is one entry of SearXNG's unresponsive_engines: an engine and why it did not
// answer. Reason is empty when SearXNG gave none.
type engineRefusal struct{ Engine, Reason string }

// parseUnresponsive reads SearXNG's unresponsive_engines, an array of [engine, reason] arrays.
// An entry that is not a non-empty array whose first element is a non-empty string is skipped,
// and a value that is not an array at all reads as none.
func parseUnresponsive(raw json.RawMessage) []engineRefusal {
	var entries []json.RawMessage
	if json.Unmarshal(raw, &entries) != nil {
		return nil
	}
	var out []engineRefusal
	for _, e := range entries {
		var pair []any
		if json.Unmarshal(e, &pair) != nil || len(pair) == 0 {
			continue
		}
		name, _ := pair[0].(string)
		if name = cleanEngineField(name); name == "" {
			continue
		}
		reason := ""
		if len(pair) > 1 {
			r, _ := pair[1].(string)
			reason = cleanEngineField(r)
		}
		out = append(out, engineRefusal{Engine: name, Reason: reason})
	}
	return out
}

// cleanEngineField drops control characters and cuts the text to refusedFieldRunes.
func cleanEngineField(s string) string {
	s = strings.TrimSpace(strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == utf8.RuneError {
			return -1
		}
		return r
	}, s))
	if r := []rune(s); len(r) > refusedFieldRunes {
		return string(r[:refusedFieldRunes])
	}
	return s
}

// enginesRefusedText is the fixed-shape message of the 502: the first four engines and their
// reasons, and a count of the rest.
func enginesRefusedText(refused []engineRefusal) string {
	var parts []string
	for _, r := range refused[:min(len(refused), refusedMaxEngines)] {
		if r.Reason == "" {
			parts = append(parts, r.Engine)
			continue
		}
		parts = append(parts, r.Engine+" ("+r.Reason+")")
	}
	text := "search engines refused: " + strings.Join(parts, ", ")
	if more := len(refused) - refusedMaxEngines; more > 0 {
		text += fmt.Sprintf(" and %d more", more)
	}
	return text
}

// buildWebResult guards one result. The fetched page, when there is one, is already guarded
// and fenced by the loader. Otherwise the SearXNG snippet is guarded here, and a result with
// neither text is dropped. A fence that cannot get a nonce drops the result too: it fails
// closed rather than carrying a forgeable delimiter.
func buildWebResult(it searxResult, pages map[string]websafe.Page) (webResult, bool) {
	title, tv := websafe.GuardTitle(it.Title, titleMaxRunes)
	res := webResult{Title: title, URL: it.URL}
	if p, ok := pages[it.URL]; ok {
		res.Content, res.Fetched, res.Truncated = p.Content, true, p.Truncated
		res.Guard = websafe.MergeVerdicts(p.Verdict, tv)
		return res, true
	}
	if len(bytes.TrimSpace([]byte(it.Content))) == 0 {
		return webResult{}, false
	}
	g, err := websafe.Guard(it.Content, websafe.SourceWeb, webMaxRunes)
	if err != nil {
		return webResult{}, false
	}
	res.Content, res.Truncated = g.Content, g.Truncated
	res.Guard = websafe.MergeVerdicts(g.Verdict, tv)
	return res, true
}

// acceptableResultURL is the filter on a SearXNG result URL: parseable, http or https, at
// most 2048 bytes, and a host the loader does not refuse by name. The connect-time check on
// the resolved address is the SSRF client's.
func acceptableResultURL(raw string) bool {
	if raw == "" || len(raw) > maxResultURL {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return false
	}
	h := u.Hostname()
	return h != "" && !websafe.HostRejected(h)
}

// searxng asks SearXNG for q. The request is built here from the fixed base URL and two
// fixed keys, and q is URL-encoded into one parameter, so the model steers nothing else.
func (s *Server) searxng(ctx context.Context, q string) ([]searxResult, []engineRefusal, error) {
	target := s.cfg.SearxngURL + "/search?" + url.Values{"q": {q}, "format": {"json"}}.Encode()
	body, err := s.upstream(ctx, s.lim.SearxngLeg, s.lim.SearxngBody, http.MethodGet, target, nil)
	if err != nil {
		return nil, nil, err
	}
	var env struct {
		Results      *[]searxResult  `json:"results"`
		Unresponsive json.RawMessage `json:"unresponsive_engines"`
	}
	if err := json.Unmarshal(body, &env); err != nil {
		return nil, nil, err
	}
	if env.Results == nil {
		return nil, nil, errors.New("searxng: answer has no results field")
	}
	return *env.Results, parseUnresponsive(env.Unresponsive), nil
}

// upstream performs one request to a fixed upstream under a leg deadline and returns the
// body, read to at most maxBody. Anything but a 200, or a longer body, is an error. The
// error text is for the caller's control flow only and never reaches a response.
func (s *Server) upstream(ctx context.Context, leg time.Duration, maxBody int64, method, target string, body []byte) ([]byte, error) {
	ctx, cancel := context.WithTimeout(ctx, leg)
	defer cancel()
	var rd io.Reader
	if body != nil {
		rd = bytes.NewReader(body)
	}
	req, err := http.NewRequestWithContext(ctx, method, target, rd)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Accept", "application/json")
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := s.d.Upstream(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("upstream status %d", resp.StatusCode)
	}
	b, err := io.ReadAll(io.LimitReader(resp.Body, maxBody+1))
	if err != nil {
		return nil, err
	}
	if int64(len(b)) > maxBody {
		return nil, errors.New("upstream body over the cap")
	}
	return b, nil
}
