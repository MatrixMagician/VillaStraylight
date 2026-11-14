package toolserver

// recall.go is villa_recall_search. It answers from the caller's own transcripts only, by
// four independent steps:
//
//  1. the identity header (checked before this runs) names the caller;
//  2. the scope document, read on every request, maps each transcript file id to its owner;
//  3. the Qdrant query is filtered to the scope's collection and to the caller's file ids;
//  4. every returned point whose file id is not the caller's is dropped again here, so a
//     layout change in Open WebUI's storage can empty or fail a result but never widen one.
//
// There is no unscoped query: no scope is a 503, and a caller who owns no transcript gets
// the empty result without an embedding or a Qdrant call.

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"

	"github.com/MatrixMagician/VillaStraylight/internal/recall"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

type recallResult struct {
	ChatID    string          `json:"chat_id"`
	Content   string          `json:"content"`
	Score     float64         `json:"score"`
	Truncated bool            `json:"truncated"`
	Guard     websafe.Verdict `json:"guard"`
}

type recallResponse struct {
	Results []recallResult `json:"results"`
}

func (s *Server) recallSearch(c *call) outcome {
	scope, err := s.readScope()
	if err != nil {
		return refuse(http.StatusServiceUnavailable)
	}
	files := scope.FilesOf(c.userID)
	if len(files) == 0 {
		return outcome{status: http.StatusOK, body: recallResponse{Results: []recallResult{}}}
	}

	embedBody, err := buildEmbedRequest(c.q, s.cfg.EmbeddingModel)
	if err != nil {
		return refuse(http.StatusBadGateway)
	}
	raw, err := s.upstream(c.ctx, s.lim.EmbedLeg, s.lim.EmbedBody, http.MethodPost, s.cfg.EmbedURL+"/v1/embeddings", embedBody)
	if err != nil {
		return fail(c.ctx)
	}
	vec, err := parseEmbedResponse(raw)
	if err != nil {
		return refuse(http.StatusBadGateway)
	}

	query, err := buildQdrantQuery(vec, c.count, scope.KnowledgeID, files)
	if err != nil {
		return refuse(http.StatusBadGateway)
	}
	target := s.cfg.QdrantURL + "/collections/" + url.PathEscape(s.cfg.Collection) + "/points/query"
	raw, err = s.upstream(c.ctx, s.lim.QdrantLeg, s.lim.QdrantBody, http.MethodPost, target, query)
	if err != nil {
		return fail(c.ctx)
	}
	hits, err := parseQdrantQuery(raw)
	if err != nil {
		return refuse(http.StatusBadGateway)
	}

	mine := make(map[string]bool, len(files))
	for _, f := range files {
		mine[f] = true
	}
	out := recallResponse{Results: []recallResult{}}
	flagged := 0
	for _, h := range hits {
		if len(out.Results) == c.count {
			break
		}
		if !mine[h.FileID] || h.Tenant != scope.KnowledgeID {
			continue
		}
		g, err := websafe.Guard(h.Text, websafe.SourceRecall, recallMaxRunes)
		if err != nil {
			continue
		}
		chatID, _ := recall.ChatIDOf(h.Name)
		if g.Verdict.Detected {
			flagged++
		}
		out.Results = append(out.Results, recallResult{
			ChatID: chatID, Content: g.Content, Score: h.Score, Truncated: g.Truncated, Guard: g.Verdict,
		})
	}
	return outcome{status: http.StatusOK, body: out, results: len(out.Results), flagged: flagged}
}

// readScope reads and validates the scope document. Any failure, a document over the cap,
// another schema version or a missing collection id is an error, never a fallback.
func (s *Server) readScope() (recall.Scope, error) {
	rc, err := s.d.ReadScope()
	if err != nil {
		return recall.Scope{}, err
	}
	defer rc.Close()
	b, err := io.ReadAll(io.LimitReader(rc, s.lim.ScopeBody+1))
	if err != nil {
		return recall.Scope{}, err
	}
	if int64(len(b)) > s.lim.ScopeBody {
		return recall.Scope{}, errors.New("scope document over the cap")
	}
	var sc recall.Scope
	if err := json.Unmarshal(b, &sc); err != nil {
		return recall.Scope{}, err
	}
	if sc.SchemaVersion != recall.ScopeSchemaVersion {
		return recall.Scope{}, errors.New("scope document has another schema version")
	}
	if sc.KnowledgeID == "" {
		return recall.Scope{}, errors.New("scope document names no collection")
	}
	return sc, nil
}
