package toolserver

// helpers_test.go holds the fakes every toolserver test drives: a map of in-memory
// backends (SearXNG, the embedder, Qdrant and a fetched page host) behind the Deps seam,
// a recording of every request they receive, and an injected clock. Nothing here opens a
// socket or touches the network.

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"
)

const (
	testKey      = "test-bearer-key"
	searxngHost  = "searxng.test"
	embedHost    = "embed.test"
	qdrantHost   = "qdrant.test"
	testModel    = "test-embed-model"
	testCollect  = "open-webui_knowledge"
	testKnowledg = "kb-1"
)

// recorded is one upstream request as the fake backends saw it.
type recorded struct {
	Method, Host, Path, RawQuery string
	Header                       http.Header
	Body                         []byte
}

// backends is the set of fake upstreams, keyed by host. A handler may be replaced by a
// test before the first request.
type backends struct {
	mu       sync.Mutex
	reqs     []recorded
	handlers map[string]http.HandlerFunc
}

func newBackends() *backends {
	return &backends{handlers: map[string]http.HandlerFunc{}}
}

func (b *backends) handle(host string, h http.HandlerFunc) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.handlers[host] = h
}

// Do is the Deps.Upstream seam: it records the request and serves it in memory.
func (b *backends) Do(req *http.Request) (*http.Response, error) {
	var body []byte
	if req.Body != nil {
		body, _ = io.ReadAll(req.Body)
		req.Body = io.NopCloser(bytes.NewReader(body))
	}
	b.mu.Lock()
	b.reqs = append(b.reqs, recorded{
		Method: req.Method, Host: req.URL.Host, Path: req.URL.Path, RawQuery: req.URL.RawQuery,
		Header: req.Header.Clone(), Body: body,
	})
	h := b.handlers[req.URL.Host]
	b.mu.Unlock()
	if h == nil {
		return nil, fmt.Errorf("no backend for host %q", req.URL.Host)
	}
	rec := httptest.NewRecorder()
	h(rec, req)
	return rec.Result(), nil
}

// to returns the requests that reached host.
func (b *backends) to(host string) []recorded {
	b.mu.Lock()
	defer b.mu.Unlock()
	var out []recorded
	for _, r := range b.reqs {
		if r.Host == host {
			out = append(out, r)
		}
	}
	return out
}

func (b *backends) total() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.reqs)
}

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// env is one server under test with its fakes.
type env struct {
	t   *testing.T
	b   *backends
	cfg Config
	d   Deps
	srv *Server
	h   http.Handler

	mu       sync.Mutex
	clock    time.Time
	logs     []string
	scope    []byte
	scopeErr error
}

func (e *env) advance(d time.Duration) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.clock = e.clock.Add(d)
}

func (e *env) logLines() []string {
	e.mu.Lock()
	defer e.mu.Unlock()
	return append([]string(nil), e.logs...)
}

func (e *env) setScope(s string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scope, e.scopeErr = []byte(s), nil
}

func (e *env) failScope(err error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	e.scopeErr = err
}

// newEnv builds a server for set over fresh fakes. opt may edit the Config or Deps before
// New runs, and the backends before the first request.
func newEnv(t *testing.T, set Set, opt ...func(*env)) *env {
	t.Helper()
	e := &env{t: t, b: newBackends(), clock: time.Unix(1_700_000_000, 0)}
	e.cfg = Config{
		Set:            set,
		Key:            testKey,
		SearxngURL:     "http://" + searxngHost,
		EmbedURL:       "http://" + embedHost,
		QdrantURL:      "http://" + qdrantHost,
		EmbeddingModel: testModel,
		Collection:     testCollect,
	}
	e.d = Deps{
		Now:         func() time.Time { e.mu.Lock(); defer e.mu.Unlock(); return e.clock },
		Upstream:    e.b.Do,
		FetchClient: &http.Client{Transport: roundTripFunc(e.b.Do)},
		ReadScope: func() (io.ReadCloser, error) {
			e.mu.Lock()
			defer e.mu.Unlock()
			if e.scopeErr != nil {
				return nil, e.scopeErr
			}
			return io.NopCloser(bytes.NewReader(e.scope)), nil
		},
		Logf: func(format string, args ...any) {
			e.mu.Lock()
			defer e.mu.Unlock()
			e.logs = append(e.logs, fmt.Sprintf(format, args...))
		},
	}
	e.setScope(fmt.Sprintf(`{"schema_version":1,"knowledge_id":%q,"files":{"f1":"u1","f2":"u1","f3":"u2"}}`, testKnowledg))
	e.b.handle(searxngHost, searxResponse(nil))
	e.b.handle(embedHost, embedResponse([]float64{0.25, -0.5, 1}))
	e.b.handle(qdrantHost, qdrantResponse(nil))
	for _, o := range opt {
		o(e)
	}
	srv, err := New(e.cfg, e.d)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	e.srv, e.h = srv, srv.Handler()
	return e
}

// req sends one request with the default bearer and the given extra headers.
func (e *env) req(method, target string, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	r := httptest.NewRequest(method, target, nil)
	r.Header.Set("Authorization", "Bearer "+testKey)
	for i := 0; i+1 < len(hdr); i += 2 {
		r.Header.Add(hdr[i], hdr[i+1])
	}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	return rec
}

func (e *env) get(target string, hdr ...string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.req(http.MethodGet, target, hdr...)
}

// recall issues a recall search as userID.
func (e *env) recall(query, userID string) *httptest.ResponseRecorder {
	e.t.Helper()
	return e.get("/recall/search?"+query, IdentityHeader, userID)
}

// searxItem is one SearXNG result.
type searxItem struct{ URL, Title, Content string }

func searxResponse(items []searxItem) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		type item struct {
			URL     string `json:"url"`
			Title   string `json:"title"`
			Content string `json:"content"`
			Engine  string `json:"engine"`
		}
		out := struct {
			Results []item `json:"results"`
		}{Results: []item{}}
		for _, i := range items {
			out.Results = append(out.Results, item{URL: i.URL, Title: i.Title, Content: i.Content, Engine: "test"})
		}
		_ = json.NewEncoder(w).Encode(out)
	}
}

func embedResponse(vec []float64) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		_ = json.NewEncoder(w).Encode(map[string]any{"data": []any{map[string]any{"embedding": vec}}})
	}
}

// qdrantHit is one point of a fake Qdrant answer.
type qdrantHitFixture struct {
	Score          float64
	Text           string
	FileID, Name   string
	Tenant         string
	OmitFileID     bool
	OmitTenantInfo bool
	OmitName       bool
}

func qdrantResponse(hits []qdrantHitFixture) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		points := []any{}
		for i, h := range hits {
			meta := map[string]any{}
			if !h.OmitName {
				meta["name"] = h.Name
			}
			if !h.OmitFileID {
				meta["file_id"] = h.FileID
			}
			payload := map[string]any{"text": h.Text, "metadata": meta}
			if !h.OmitTenantInfo {
				tenant := h.Tenant
				if tenant == "" {
					tenant = testKnowledg
				}
				payload["tenant_id"] = tenant
			}
			points = append(points, map[string]any{"id": fmt.Sprintf("p%d", i), "version": 1, "score": h.Score, "payload": payload})
		}
		_ = json.NewEncoder(w).Encode(map[string]any{"result": map[string]any{"points": points}, "status": "ok", "time": 0.001})
	}
}

// page serves a fixed fetched page body at host's path.
func page(status int, body string) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(status)
		_, _ = w.Write([]byte(body))
	}
}

// decode parses a recorder's body into v.
func decode(t *testing.T, rec *httptest.ResponseRecorder, v any) {
	t.Helper()
	if err := json.Unmarshal(rec.Body.Bytes(), v); err != nil {
		t.Fatalf("body is not JSON: %v\n%s", err, rec.Body.String())
	}
}

type webResp struct {
	Results []struct {
		Title     string `json:"title"`
		URL       string `json:"url"`
		Content   string `json:"content"`
		Fetched   bool   `json:"fetched"`
		Truncated bool   `json:"truncated"`
		Guard     struct {
			Detected bool     `json:"detected"`
			Rules    []string `json:"rules"`
		} `json:"guard"`
	} `json:"results"`
}

type recallResp struct {
	Results []struct {
		ChatID    string  `json:"chat_id"`
		Content   string  `json:"content"`
		Score     float64 `json:"score"`
		Truncated bool    `json:"truncated"`
		Guard     struct {
			Detected bool     `json:"detected"`
			Rules    []string `json:"rules"`
		} `json:"guard"`
	} `json:"results"`
}

func mustStatus(t *testing.T, rec *httptest.ResponseRecorder, want int) {
	t.Helper()
	if rec.Code != want {
		t.Fatalf("status = %d, want %d; body: %s", rec.Code, want, rec.Body.String())
	}
}

// errorBody returns the {"error": ...} text of a refusal.
func errorBody(t *testing.T, rec *httptest.ResponseRecorder) string {
	t.Helper()
	var b struct {
		Error string `json:"error"`
	}
	decode(t, rec, &b)
	if b.Error == "" {
		t.Fatalf("refusal body has no error text: %s", rec.Body.String())
	}
	return b.Error
}

var errBoom = errors.New("boom")

// toolPath is the route of the one tool a set serves.
func toolPath(set Set) string {
	for _, t := range toolTable {
		if t.Set == set {
			return t.Path
		}
	}
	return ""
}
