package toolserver

import (
	"os"
	"strings"
	"testing"
)

// The Qdrant request builder and response parser are pinned here against documented
// shapes, not against a capture. testdata/qdrant-query-response.handwritten.json was
// written by hand from Qdrant's REST documentation and Open WebUI's client, and is marked
// as such. A fixture captured from the pinned Qdrant replaces it (ADR-0042 "Not
// established in the tree").

// TestQdrantQueryRequestShape pins the exact body of the one query the recall tool sends:
// query vector, limit, payload on, and a filter of exactly the tenant and the caller's
// file ids.
func TestQdrantQueryRequestShape(t *testing.T) {
	got, err := buildQdrantQuery([]float64{0.25, -0.5, 1}, 7, "kb-1", []string{"f1", "f2"})
	if err != nil {
		t.Fatal(err)
	}
	const want = `{"query":[0.25,-0.5,1],"limit":7,"with_payload":true,"filter":{"must":[` +
		`{"key":"tenant_id","match":{"value":"kb-1"}},` +
		`{"key":"metadata.file_id","match":{"any":["f1","f2"]}}]}}`
	if string(got) != want {
		t.Errorf("body\n got: %s\nwant: %s", got, want)
	}
	if _, err := buildQdrantQuery(nil, 5, "kb", []string{"f"}); err == nil {
		t.Error("an empty vector must not build a query")
	}
	if _, err := buildQdrantQuery([]float64{1}, 5, "kb", nil); err == nil {
		t.Error("an empty file list must not build a query: there is no unscoped query")
	}
	if _, err := buildQdrantQuery([]float64{1}, 5, "", []string{"f"}); err == nil {
		t.Error("an empty tenant must not build a query")
	}
}

// TestQdrantResponseFixture parses the hand-written fixture.
func TestQdrantResponseFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/qdrant-query-response.handwritten.json")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(raw), "NOT captured from the pinned Qdrant") {
		t.Fatal("the fixture must say it is not a capture until it is replaced by one")
	}
	hits, err := parseQdrantQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	h := hits[0]
	if h.Score != 0.7123 || h.FileID != "file-aaa" || h.Name != "villa-recall-chat-111.txt" || h.Tenant != "kb-1" || !strings.Contains(h.Text, "which ferry") {
		t.Errorf("first hit = %+v", h)
	}
	if hits[1].FileID != "file-bbb" || hits[1].Score != 0.6011 {
		t.Errorf("second hit = %+v", hits[1])
	}
}

// TestQdrantResponseOutOfShape: a body that is not the query answer is an error, and a
// point missing its fields is returned empty so the post-filter drops it.
func TestQdrantResponseOutOfShape(t *testing.T) {
	for _, bad := range []string{``, `x`, `{}`, `{"result":null}`, `{"result":[]}`, `{"result":{}}`, `{"result":{"points":null}}`, `{"result":{"points":"x"}}`} {
		if _, err := parseQdrantQuery([]byte(bad)); err == nil {
			t.Errorf("parseQdrantQuery(%q) succeeded", bad)
		}
	}
	hits, err := parseQdrantQuery([]byte(`{"result":{"points":[{"id":1,"score":0.5},{"id":2,"score":0.4,"payload":{"text":5,"metadata":"x"}}]}}`))
	if err != nil {
		t.Fatal(err)
	}
	for _, h := range hits {
		if h.FileID != "" || h.Text != "" {
			t.Errorf("a malformed point must come back empty: %+v", h)
		}
	}
	if hits, err := parseQdrantQuery([]byte(`{"result":{"points":[]}}`)); err != nil || len(hits) != 0 {
		t.Errorf("no points is an empty answer: %v %v", hits, err)
	}
}

// TestEmbeddingRequestAndResponse pins the embedder exchange: the body is exactly input
// and model, and the answer must hold one non-empty vector.
func TestEmbeddingRequestAndResponse(t *testing.T) {
	body, err := buildEmbedRequest("what?", "m")
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != `{"input":["what?"],"model":"m"}` {
		t.Errorf("embed body = %s", body)
	}
	vec, err := parseEmbedResponse([]byte(`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1,0.2]}],"model":"m","usage":{}}`))
	if err != nil || len(vec) != 2 || vec[1] != 0.2 {
		t.Errorf("vec=%v err=%v", vec, err)
	}
	for _, bad := range []string{``, `{}`, `{"data":[]}`, `{"data":[{"embedding":[]}]}`, `{"data":[{"embedding":[1]},{"embedding":[2]}]}`} {
		if _, err := parseEmbedResponse([]byte(bad)); err == nil {
			t.Errorf("parseEmbedResponse(%q) succeeded", bad)
		}
	}
}
