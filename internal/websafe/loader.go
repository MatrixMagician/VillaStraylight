package websafe

// loader.go is the verified OWUI external-loader HTTP contract glue.
//
// When OWUI is configured with WEB_LOADER_ENGINE=external it POSTs the SearXNG result
// URLs to villa-websafe and expects back a JSON array of produced pages:
//
//	POST /load
//	Authorization: Bearer <EXTERNAL_WEB_LOADER_API_KEY>
//	Content-Type: application/json
//	{"urls": ["https://result1", ...]}            // field name is exactly "urls"
//
//	200 [{"page_content": "...", "metadata": {"source": "https://result1", "title": "..."}}]
//
// The metadata map ALSO carries an additive nested guard sub-key (see the Phase-32 note
// below) — {detected, rules} — alongside source and title.
//
// Contract details VERIFIED at the pinned OWUI digest (commit 02dc3e6):
//   - metadata.source flows into OWUI's top-level `sources` citation field, so it MUST
// carry the fetched URL (inline citations to live URLs).
//   - OWUI calls response.raise_for_status(): a non-2xx aborts the WHOLE loader batch.
//     Therefore per-URL failures are represented by OMITTING that URL from the array
//     (skip-and-continue, done by Loader.Load); the handler ALWAYS returns 200 with a
//     (possibly empty, non-nil) array — never a non-2xx for a per-URL failure.
//
// Phase-32 ADDITIVE widening: metadata gains a nested `guard` sub-key carrying
// the verdict {detected, rules} from Page.Verdict. This is the ONLY safe change
// the verified page_content / metadata / source / title tags are NOT renamed. OWUI ignores
// unknown metadata keys (Assumption A3), so the widening is contract-safe; Phase 34
// surfaces these as counters. The widening is guarded by TestLoadMetadataGuard.
//
// The handler is the sole-producer boundary: every byte OWUI embeds or shows
// the model passes through Loader.fetchOne (incl. the Phase-31-stubbed guard seam).
//
// This file is TestSeamGrepGate-clean: it composes no container-image / host-identity
// literals. The /load path token is the single source of truth that Plan 03's
// EXTERNAL_WEB_LOADER_URL composition must match.

import (
	"crypto/subtle"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

// loadPath is the single route the external-loader handler serves. Plan 03 composes
// EXTERNAL_WEB_LOADER_URL to end in this exact path — keep them in sync from here.
const loadPath = "/load"

// maxRequestBodyBytes bounds the inbound request body (the {urls:[...]} JSON), so a
// hostile/oversize POST cannot OOM the loader. The URL list is small by construction
// (count = WEB_SEARCH_RESULT_COUNT); 1 MiB is generous.
const maxRequestBodyBytes = 1 << 20

// LoadRequest is the body OWUI POSTs. The field tag is EXACTLY "urls" per the verified
// contract.
type LoadRequest struct {
	URLs []string `json:"urls"`
}

// LoadResponse is one element of the array OWUI expects back. Field tags are EXACTLY
// "page_content" and "metadata" per the verified contract; metadata.source is the
// citation URL.
type LoadResponse struct {
	PageContent string         `json:"page_content"`
	Metadata    map[string]any `json:"metadata"`
}

// Server is the OWUI external-loader HTTP service: it folds the SSRF-guarded Loader and
// the expected Bearer secret. The live wiring (a real crypto/rand secret + the
// SafeClient-backed Loader) is built in the cmd tier (Plan 03).
type Server struct {
	loader *Loader
	secret string
}

// NewServer constructs a Server over the given Loader and expected Bearer secret. An
// empty secret means any villa.network caller is accepted (documented posture); the
// recommended posture supplies a real secret in Plan 03.
func NewServer(loader *Loader, secret string) *Server {
	return &Server{loader: loader, secret: secret}
}

// Handler returns the http.Handler serving the external-loader contract on loadPath.
// Plan 03's cmd serves this; the path here is the single source of truth.
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc(loadPath, s.HandleLoad)
	return mux
}

// authOK reports whether the request carries the expected Bearer secret. The compare is
// constant-time (crypto/subtle) to avoid leaking the secret via timing. An empty
// configured secret accepts any caller (documented empty-secret posture).
func (s *Server) authOK(r *http.Request) bool {
	if s.secret == "" {
		return true
	}
	const prefix = "Bearer "
	h := r.Header.Get("Authorization")
	if !strings.HasPrefix(h, prefix) {
		return false
	}
	got := strings.TrimPrefix(h, prefix)
	return subtle.ConstantTimeCompare([]byte(got), []byte(s.secret)) == 1
}

// HandleLoad implements the verified OWUI external-loader contract:
//   - reject an unauthenticated caller with 401 (before any fetch);
//   - decode the bounded {urls:[...]} body, 400 on malformed/oversize input;
//   - fetch via the SSRF-guarded, skip-and-continue Loader;
//   - ALWAYS return 200 with a (possibly empty, non-nil) array of produced pages,
//     mapping each Page to {page_content, metadata{source, title}}.
//
// It never returns a non-2xx for a per-URL failure (OWUI raise_for_status would abort
// the whole batch); failures are represented by the URL's absence from the array.
func (s *Server) HandleLoad(w http.ResponseWriter, r *http.Request) {
	if !s.authOK(r) {
		w.WriteHeader(http.StatusUnauthorized)
		return
	}

	var in LoadRequest
	dec := json.NewDecoder(io.LimitReader(r.Body, maxRequestBodyBytes))
	if err := dec.Decode(&in); err != nil {
		w.WriteHeader(http.StatusBadRequest)
		return
	}

	pages := s.loader.Load(r.Context(), in.URLs)
	out := make([]LoadResponse, 0, len(pages))
	for _, p := range pages {
		out = append(out, LoadResponse{
			PageContent: p.Content,
			Metadata: map[string]any{
				"source": p.Source, // → OWUI top-level `sources` citation
				"title":  p.Title,
				// ADDITIVE verdict: a nested sub-key ALONGSIDE the
				// verified contract tags; OWUI ignores unknown metadata keys (A3). Always
				// present (detected:false for benign pages) so Phase 34 can count it.
				"guard": map[string]any{
					"detected": p.Verdict.Detected,
					"rules":    p.Verdict.Rules,
				},
			},
		})
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(out) // ALWAYS 200 with a (partial) array.
}
