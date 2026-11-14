package toolserver

import (
	"encoding/json"
	"os"
	"strings"
	"testing"
)

// The response parser is pinned against testdata/qdrant-query-response.captured.json, a
// capture of villa-qdrant 1.18.2 taken on the dev host on 2026-11-14 with every id, name and
// text replaced. Its structure and types are as Qdrant returned them: there is no top-level
// usage key, and metadata.embedding_config is an object, not a string.

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

// TestQdrantResponseFixture parses the captured fixture. It asserts the fixture still says
// it is a capture and still has the two shape facts a hand-written one got wrong, so it
// cannot be swapped back for a guess.
func TestQdrantResponseFixture(t *testing.T) {
	raw, err := os.ReadFile("testdata/qdrant-query-response.captured.json")
	if err != nil {
		t.Fatal(err)
	}
	var doc map[string]any
	if err := json.Unmarshal(raw, &doc); err != nil {
		t.Fatal(err)
	}
	if note, _ := doc["_captured"].(string); !strings.Contains(note, "villa-qdrant 1.18.2") {
		t.Fatalf("the fixture must carry the capture note naming the Qdrant version: %q", note)
	}
	if _, ok := doc["usage"]; ok {
		t.Error("the capture has no top-level usage key")
	}
	points := doc["result"].(map[string]any)["points"].([]any)
	meta := points[0].(map[string]any)["payload"].(map[string]any)["metadata"].(map[string]any)
	if _, ok := meta["embedding_config"].(map[string]any); !ok {
		t.Errorf("embedding_config must be an object in the capture: %T", meta["embedding_config"])
	}

	hits, err := parseQdrantQuery(raw)
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 2 {
		t.Fatalf("hits = %d, want 2", len(hits))
	}
	h := hits[0]
	if h.Score != 0.5115 || h.FileID != "file-0" || h.Tenant != "knowledge-id" ||
		h.Name != "villa-recall-00000000-0000-4000-8000-000000000000.txt" || h.Text != "redacted chunk 0" {
		t.Errorf("first hit = %+v", h)
	}
	if hits[1].FileID != "file-1" || hits[1].Score != 0.4631 {
		t.Errorf("second hit = %+v", hits[1])
	}
}

// TestQdrantPointWithoutAName: the collection holds other tenants' points and some have no
// metadata.name at all. The parser returns such a point with an empty name and no error.
func TestQdrantPointWithoutAName(t *testing.T) {
	hits, err := parseQdrantQuery([]byte(`{"result":{"points":[{"id":"x","version":1,"score":0.3,` +
		`"payload":{"text":"t","tenant_id":"other","metadata":{"file_id":"f9","created_by":"u"}}}]},"status":"ok","time":0.1}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(hits) != 1 || hits[0].Name != "" || hits[0].Tenant != "other" || hits[0].FileID != "f9" {
		t.Errorf("hits = %+v", hits)
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
