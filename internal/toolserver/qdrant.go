package toolserver

// qdrant.go holds the two upstream exchanges of the recall tool as small pure functions:
// the embeddings request and the scoped Qdrant query, each a builder and a parser.
//
// The shapes follow what the pinned Open WebUI sends (its Qdrant client calls
// query_points with a must filter of the tenant and metadata conditions) and Qdrant's
// documented REST API for POST /collections/{name}/points/query. They are pinned by
// testdata/qdrant-query-response.captured.json, a capture of villa-qdrant 1.18.2 with the
// ids, names and text replaced. The recall tool's only Qdrant call is this one query.

import (
	"bytes"
	"encoding/json"
	"errors"
)

// qdrantHit is one returned point, reduced to what the tool reads. A point whose payload is
// missing a field comes back with that field empty, and the post-filter drops it.
type qdrantHit struct {
	Score  float64
	Text   string
	FileID string
	Name   string
	Tenant string
}

type qdrantCondition struct {
	Key   string      `json:"key"`
	Match qdrantMatch `json:"match"`
}

// qdrantMatch carries exactly one of value (a keyword equal) or any (a keyword in).
type qdrantMatch struct {
	Value *string  `json:"value,omitempty"`
	Any   []string `json:"any,omitempty"`
}

// buildQdrantQuery is the body of the one query: the query vector, the limit, the payload,
// and a filter of exactly two must conditions, the tenant (Open WebUI's Knowledge
// collection id) and the caller's file ids. There is no unscoped query: an empty tenant or
// file list is an error here.
func buildQdrantQuery(vector []float64, limit int, tenantID string, fileIDs []string) ([]byte, error) {
	if len(vector) == 0 {
		return nil, errors.New("qdrant query: empty vector")
	}
	if tenantID == "" || len(fileIDs) == 0 {
		return nil, errors.New("qdrant query: refusing an unscoped query")
	}
	body := struct {
		Query       []float64 `json:"query"`
		Limit       int       `json:"limit"`
		WithPayload bool      `json:"with_payload"`
		Filter      struct {
			Must []qdrantCondition `json:"must"`
		} `json:"filter"`
	}{Query: vector, Limit: limit, WithPayload: true}
	body.Filter.Must = []qdrantCondition{
		{Key: "tenant_id", Match: qdrantMatch{Value: &tenantID}},
		{Key: "metadata.file_id", Match: qdrantMatch{Any: fileIDs}},
	}
	return marshalNoEscape(body)
}

// parseQdrantQuery reads the answer of the query endpoint: result.points[]. A body that is
// not that shape is an error (a 502 upstream of here), and a point missing payload fields is
// returned with them empty.
func parseQdrantQuery(raw []byte) ([]qdrantHit, error) {
	var env struct {
		Result *struct {
			Points *[]struct {
				Score   float64        `json:"score"`
				Payload map[string]any `json:"payload"`
			} `json:"points"`
		} `json:"result"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if env.Result == nil || env.Result.Points == nil {
		return nil, errors.New("qdrant: answer has no result.points")
	}
	hits := make([]qdrantHit, 0, len(*env.Result.Points))
	for _, p := range *env.Result.Points {
		h := qdrantHit{Score: p.Score}
		h.Text, _ = p.Payload["text"].(string)
		h.Tenant, _ = p.Payload["tenant_id"].(string)
		if meta, ok := p.Payload["metadata"].(map[string]any); ok {
			h.FileID, _ = meta["file_id"].(string)
			h.Name, _ = meta["name"].(string)
		}
		hits = append(hits, h)
	}
	return hits, nil
}

// buildEmbedRequest is the body the pinned Open WebUI sends to /v1/embeddings: the input
// list and the model, with no task prefix.
func buildEmbedRequest(q, model string) ([]byte, error) {
	return marshalNoEscape(struct {
		Input []string `json:"input"`
		Model string   `json:"model"`
	}{Input: []string{q}, Model: model})
}

// parseEmbedResponse returns the one embedding of an OpenAI-style /v1/embeddings answer.
func parseEmbedResponse(raw []byte) ([]float64, error) {
	var env struct {
		Data []struct {
			Embedding []float64 `json:"embedding"`
		} `json:"data"`
	}
	if err := json.Unmarshal(raw, &env); err != nil {
		return nil, err
	}
	if len(env.Data) != 1 || len(env.Data[0].Embedding) == 0 {
		return nil, errors.New("embedder: answer does not hold exactly one non-empty embedding")
	}
	return env.Data[0].Embedding, nil
}

// marshalNoEscape is json.Marshal without HTML escaping, so a query's text reaches the
// upstream as the model wrote it.
func marshalNoEscape(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(buf.Bytes(), "\n"), nil
}
