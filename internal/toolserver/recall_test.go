package toolserver

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestRecallRefusesWithoutIdentity: an absent or repeated identity header is a 403 before
// the scope is read, the embedder is called or Qdrant is queried.
func TestRecallRefusesWithoutIdentity(t *testing.T) {
	scopeReads := 0
	e := newEnv(t, SetRecall, func(e *env) {
		inner := e.d.ReadScope
		e.d.ReadScope = func() (io.ReadCloser, error) { scopeReads++; return inner() }
	})
	mustStatus(t, e.get("/recall/search?q=x"), http.StatusForbidden)

	r := httptest.NewRequest(http.MethodGet, "/recall/search?q=x", nil)
	r.Header.Set("Authorization", "Bearer "+testKey)
	r.Header["X-Villa-User-Id"] = []string{"u1", "u1"}
	rec := httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusForbidden)

	r = httptest.NewRequest(http.MethodGet, "/recall/search?q=x", nil)
	r.Header.Set("Authorization", "Bearer "+testKey)
	r.Header["X-Villa-User-Id"] = []string{"u1", "u2"}
	rec = httptest.NewRecorder()
	e.h.ServeHTTP(rec, r)
	mustStatus(t, rec, http.StatusForbidden)

	if e.b.total() != 0 || scopeReads != 0 {
		t.Errorf("a refused call went on: %d upstream requests, %d scope reads", e.b.total(), scopeReads)
	}
	if IdentityHeader != "X-Villa-User-Id" {
		t.Errorf("IdentityHeader = %q", IdentityHeader)
	}
}

// TestRecallRefusesMalformedIdentity: the identity must match ^[A-Za-z0-9_-]{1,64}$. It is
// used as a map key and never placed in a query, so anything else is a 403.
func TestRecallRefusesMalformedIdentity(t *testing.T) {
	e := newEnv(t, SetRecall)
	for _, id := range []string{"", " ", "a b", "a,b", "a;b", "../x", "u1'", `u1"`, "é", "a\x00b", strings.Repeat("a", 65), "u1 ", " u1", "a/b", "a=b", "{{USER_ID}}", "a.b", "a:b"} {
		if rec := e.recall("q=x", id); rec.Code != http.StatusForbidden {
			t.Errorf("identity %q: status %d, want 403", id, rec.Code)
		}
	}
	if e.b.total() != 0 {
		t.Errorf("a malformed identity reached a backend")
	}
	for _, id := range []string{"u1", "A-b_9", strings.Repeat("a", 64), "9f1c2d3e-4b5a-6789-8abc-0123456789ab"} {
		if rec := e.recall("q=x", id); rec.Code != http.StatusOK {
			t.Errorf("identity %q: status %d, want 200", id, rec.Code)
		}
	}
}

// TestRecallRefusesWithoutScope: a missing, unreadable, oversized, malformed or empty scope
// is a 503 that names the remedy, and there is no unscoped fallback: nothing is embedded
// or queried.
func TestRecallRefusesWithoutScope(t *testing.T) {
	cases := map[string]func(*env){
		"read error":     func(e *env) { e.failScope(errBoom) },
		"empty file":     func(e *env) { e.setScope("") },
		"not json":       func(e *env) { e.setScope("not json") },
		"no collection":  func(e *env) { e.setScope(`{"schema_version":1,"knowledge_id":"","files":{"f1":"u1"}}`) },
		"future schema":  func(e *env) { e.setScope(`{"schema_version":2,"knowledge_id":"kb","files":{"f1":"u1"}}`) },
		"zero schema":    func(e *env) { e.setScope(`{"knowledge_id":"kb","files":{"f1":"u1"}}`) },
		"files wrongly":  func(e *env) { e.setScope(`{"schema_version":1,"knowledge_id":"kb","files":["f1"]}`) },
		"trailing bytes": func(e *env) { e.setScope(`{"schema_version":1,"knowledge_id":"kb","files":{}} x`) },
	}
	for name, mut := range cases {
		e := newEnv(t, SetRecall)
		mut(e)
		rec := e.recall("q=x", "u1")
		if rec.Code != http.StatusServiceUnavailable {
			t.Errorf("%s: status %d, want 503", name, rec.Code)
			continue
		}
		if !strings.Contains(errorBody(t, rec), "villa recall index") {
			t.Errorf("%s: the 503 text must name `villa recall index`: %s", name, rec.Body.String())
		}
		if e.b.total() != 0 {
			t.Errorf("%s: an unscoped query was issued", name)
		}
	}
}

// TestRecallQueryIsScopedToTheCallersFiles: the embed request is the plain one Open WebUI
// sends, and the Qdrant query carries the collection filter and ONLY the caller's file
// ids, whichever caller it is. A caller with no file gets the empty result and no call.
func TestRecallQueryIsScopedToTheCallersFiles(t *testing.T) {
	e := newEnv(t, SetRecall)
	mustStatus(t, e.recall("q=what+did+I+say&count=7", "u1"), http.StatusOK)

	emb := e.b.to(embedHost)
	if len(emb) != 1 || emb[0].Method != http.MethodPost || emb[0].Path != "/v1/embeddings" {
		t.Fatalf("embedder requests = %+v", emb)
	}
	var eb map[string]any
	if err := json.Unmarshal(emb[0].Body, &eb); err != nil {
		t.Fatal(err)
	}
	if len(eb) != 2 || eb["model"] != testModel {
		t.Errorf("embed body = %s, want exactly input and model", emb[0].Body)
	}
	if in, _ := eb["input"].([]any); len(in) != 1 || in[0] != "what did I say" {
		t.Errorf("embed input = %v (no task prefix)", eb["input"])
	}

	qd := e.b.to(qdrantHost)
	if len(qd) != 1 {
		t.Fatalf("Qdrant saw %d requests, want 1", len(qd))
	}
	assertScopedQuery(t, qd[0], []string{"f1", "f2"}, 7)

	e2 := newEnv(t, SetRecall)
	mustStatus(t, e2.recall("q=x", "u2"), http.StatusOK)
	assertScopedQuery(t, e2.b.to(qdrantHost)[0], []string{"f3"}, 5)

	e3 := newEnv(t, SetRecall)
	rec := e3.recall("q=x", "u-owns-nothing")
	mustStatus(t, rec, http.StatusOK)
	if strings.TrimSpace(rec.Body.String()) != `{"results":[]}` {
		t.Errorf("body = %s", rec.Body.String())
	}
	if e3.b.total() != 0 {
		t.Errorf("a caller with no transcript caused %d upstream calls", e3.b.total())
	}
}

// assertScopedQuery checks one Qdrant request body: the vector, the limit, the tenant
// match, and exactly wantFiles under metadata.file_id.
func assertScopedQuery(t *testing.T, r recorded, wantFiles []string, wantLimit int) {
	t.Helper()
	if r.Method != http.MethodPost || r.Path != "/collections/"+testCollect+"/points/query" {
		t.Errorf("Qdrant request = %s %s", r.Method, r.Path)
	}
	var body struct {
		Query       []float64 `json:"query"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
		Filter      struct {
			Must []struct {
				Key   string `json:"key"`
				Match struct {
					Value *string  `json:"value"`
					Any   []string `json:"any"`
				} `json:"match"`
			} `json:"must"`
			Should  json.RawMessage `json:"should"`
			MustNot json.RawMessage `json:"must_not"`
		} `json:"filter"`
	}
	if err := json.Unmarshal(r.Body, &body); err != nil {
		t.Fatalf("Qdrant body: %v", err)
	}
	if len(body.Query) != 3 || body.Query[0] != 0.25 || body.Limit != wantLimit || !body.WithPayload {
		t.Errorf("query=%v limit=%d with_payload=%v", body.Query, body.Limit, body.WithPayload)
	}
	if body.Filter.Should != nil || body.Filter.MustNot != nil || len(body.Filter.Must) != 2 {
		t.Fatalf("the filter must be exactly two must conditions: %s", r.Body)
	}
	tenant, files := body.Filter.Must[0], body.Filter.Must[1]
	if tenant.Key != "tenant_id" || tenant.Match.Value == nil || *tenant.Match.Value != testKnowledg {
		t.Errorf("tenant condition = %+v", tenant)
	}
	if files.Key != "metadata.file_id" || strings.Join(files.Match.Any, ",") != strings.Join(wantFiles, ",") || files.Match.Value != nil {
		t.Errorf("file condition = %+v, want any %v", files, wantFiles)
	}
}

// TestRecallDropsHitsOutsideTheCallersFiles: whatever Qdrant returns, only points whose
// file id is in the caller's set (and whose tenant is the scope's collection) are
// returned. A point with no file id is dropped too.
func TestRecallDropsHitsOutsideTheCallersFiles(t *testing.T) {
	e := newEnv(t, SetRecall, func(e *env) {
		e.b.handle(qdrantHost, qdrantResponse([]qdrantHitFixture{
			{Score: 0.9, Text: "mine-1", FileID: "f1", Name: "villa-recall-chat-a.txt"},
			{Score: 0.8, Text: "OTHER-USERS", FileID: "f3", Name: "villa-recall-chat-b.txt"},
			{Score: 0.7, Text: "NO-FILE-ID", OmitFileID: true, Name: "villa-recall-chat-c.txt"},
			{Score: 0.6, Text: "UNKNOWN-FILE", FileID: "f-planted", Name: "villa-recall-chat-d.txt"},
			{Score: 0.5, Text: "mine-2", FileID: "f2", Name: "not-a-transcript-name.txt"},
			{Score: 0.4, Text: "OTHER-COLLECTION", FileID: "f1", Tenant: "kb-other", Name: "villa-recall-chat-e.txt"},
			{Score: 0.3, Text: "NO-TENANT", FileID: "f1", OmitTenantInfo: true, Name: "villa-recall-chat-f.txt"},
		}))
	})
	var got recallResp
	rec := e.recall("q=x", "u1")
	mustStatus(t, rec, http.StatusOK)
	decode(t, rec, &got)
	if len(got.Results) != 2 {
		t.Fatalf("results = %d, want the 2 of the caller's files: %s", len(got.Results), rec.Body.String())
	}
	for _, bad := range []string{"OTHER-USERS", "NO-FILE-ID", "UNKNOWN-FILE", "OTHER-COLLECTION", "NO-TENANT"} {
		if strings.Contains(rec.Body.String(), bad) {
			t.Errorf("a point outside the caller's files was returned: %s", bad)
		}
	}
	if got.Results[0].ChatID != "chat-a" || got.Results[0].Score != 0.9 || !strings.Contains(got.Results[0].Content, "mine-1") {
		t.Errorf("first result = %+v", got.Results[0])
	}
	if got.Results[1].ChatID != "" || got.Results[1].Score != 0.5 {
		t.Errorf("a name that does not parse leaves chat_id empty and the score unchanged: %+v", got.Results[1])
	}

	// The same answer read as the other user keeps only that user's point.
	var got2 recallResp
	rec = e.recall("q=x", "u2")
	decode(t, rec, &got2)
	if len(got2.Results) != 1 || !strings.Contains(got2.Results[0].Content, "OTHER-USERS") {
		t.Errorf("u2's result = %+v", got2.Results)
	}
}

// TestRecallIssuesOnlyAQuery: the only request that ever reaches Qdrant is one POST to the
// query endpoint of the one collection, and the only embedder request is the embeddings
// one. There is no write, no delete, no scroll and no collection call.
func TestRecallIssuesOnlyAQuery(t *testing.T) {
	e := newEnv(t, SetRecall)
	for i := range 5 {
		mustStatus(t, e.recall("q=x&count="+string(rune('1'+i)), "u1"), http.StatusOK)
	}
	qd := e.b.to(qdrantHost)
	if len(qd) != 5 {
		t.Fatalf("Qdrant saw %d requests, want 5", len(qd))
	}
	for _, r := range qd {
		if r.Method != http.MethodPost || r.Path != "/collections/"+testCollect+"/points/query" || r.RawQuery != "" {
			t.Errorf("Qdrant request %s %s?%s is not the one allowed query", r.Method, r.Path, r.RawQuery)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get(IdentityHeader) != "" {
			t.Errorf("a caller header was forwarded to Qdrant: %v", r.Header)
		}
	}
	for _, r := range e.b.to(embedHost) {
		if r.Method != http.MethodPost || r.Path != "/v1/embeddings" {
			t.Errorf("embedder request %s %s", r.Method, r.Path)
		}
		if r.Header.Get("Authorization") != "" || r.Header.Get(IdentityHeader) != "" {
			t.Errorf("a caller header was forwarded to the embedder: %v", r.Header)
		}
	}
	if len(e.b.to(searxngHost)) != 0 {
		t.Error("the recall set made a web request")
	}
}

// TestRecallBackendFailuresAre502: an embedder or Qdrant failure, or an answer out of
// shape, is a 502.
func TestRecallBackendFailuresAre502(t *testing.T) {
	cases := map[string]func(*env){
		"embedder 500":       func(e *env) { e.b.handle(embedHost, page(500, "")) },
		"embedder not json":  func(e *env) { e.b.handle(embedHost, page(200, "x")) },
		"embedder no vector": func(e *env) { e.b.handle(embedHost, page(200, `{"data":[]}`)) },
		"embedder empty vec": func(e *env) { e.b.handle(embedHost, page(200, `{"data":[{"embedding":[]}]}`)) },
		"qdrant 500":         func(e *env) { e.b.handle(qdrantHost, page(500, "")) },
		"qdrant 404":         func(e *env) { e.b.handle(qdrantHost, page(404, `{"status":{"error":"Not found"}}`)) },
		"qdrant not json":    func(e *env) { e.b.handle(qdrantHost, page(200, "x")) },
		"qdrant no result":   func(e *env) { e.b.handle(qdrantHost, page(200, `{"status":"ok"}`)) },
		"qdrant old shape":   func(e *env) { e.b.handle(qdrantHost, page(200, `{"result":[{"id":1,"score":1}]}`)) },
	}
	for name, mut := range cases {
		e := newEnv(t, SetRecall, mut)
		if rec := e.recall("q=x", "u1"); rec.Code != http.StatusBadGateway {
			t.Errorf("%s: status %d, want 502", name, rec.Code)
		}
	}
}
