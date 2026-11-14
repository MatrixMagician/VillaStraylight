// Package toolserver is the pure core of villa's own tool server (ADR-0042): a fixed,
// read-only OpenAPI tool server that publishes web search and recall search to Open WebUI
// as model-callable tools. A process serves ONE set of tools, so the unit that parses
// untrusted web pages never holds read access to the recall index.
//
// One Go table (table.go) is the source of both the registered routes and the OpenAPI
// document (openapi.go), so a tool cannot be served without being in the document nor
// documented without being served.
//
// PURE: the package opens no socket and reads no file. Every upstream (SearXNG, the
// embedder, Qdrant), the SSRF-guarded fetch client, the recall scope file and the clock
// arrive through Deps, and the cmd tier wires them to the host. The network addresses are
// Config fields composed from the internal/config constants in the command tier.
//
// Posture, by construction:
//   - every route is a GET and any other method is a 405;
//   - a bearer on every route but /healthz, compared in constant time, and the core
//     refuses to be built with an empty key;
//   - no tool takes a URL, and the recall tool's identity is a header Open WebUI fills,
//     never a parameter the model can set;
//   - every returned text goes through websafe.Guard: fenced, flagged, never claimed safe;
//   - a refusal is a fixed string and never echoes a query, a header or an upstream body,
//     because Open WebUI hands a 4xx or 5xx body to the model.
package toolserver

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// ContractVersion is the tool contract version, the document's info.version. A change to a
// parameter, a response field or a description bumps it and refreezes the golden.
const ContractVersion = "1"

// IdentityHeader is the one place the identity header's name lives. Open WebUI fills it on
// the recall connection with the session user's id.
const IdentityHeader = "X-Villa-User-Id"

// The server's own limits that are not per-request: the header cap and the connection
// deadlines the cmd tier puts on its http.Server. There is deliberately no WriteTimeout,
// because the request deadline bounds the work.
const (
	MaxHeaderBytes    = 16 << 10
	ReadHeaderTimeout = 30 * time.Second
	IdleTimeout       = 120 * time.Second
)

// Set names which tools a process serves.
type Set string

const (
	// SetWeb is villa_web_search.
	SetWeb Set = "web"
	// SetRecall is villa_recall_search.
	SetRecall Set = "recall"
)

// ParseSet returns the Set named by s, and an error for anything but "web" or "recall".
func ParseSet(s string) (Set, error) {
	switch Set(s) {
	case SetWeb, SetRecall:
		return Set(s), nil
	}
	return "", fmt.Errorf("unknown tool set %q (want web or recall)", s)
}

// Limits are the per-request and per-unit caps. A zero field takes its default.
type Limits struct {
	MaxInFlight int           // requests in flight per unit
	BucketSize  int           // rate bucket capacity
	RefillEvery time.Duration // one token back per this interval

	WebDeadline    time.Duration // whole web request
	RecallDeadline time.Duration // whole recall request
	SearxngLeg     time.Duration
	EmbedLeg       time.Duration
	QdrantLeg      time.Duration

	SearxngBody int64 // upstream bodies read, in bytes
	EmbedBody   int64
	QdrantBody  int64
	ScopeBody   int64
}

// DefaultLimits are the contract's limits (ADR-0042 "Limits"). Open WebUI's own tool timeout
// defaults to 300 s, so these deadlines always fire first.
func DefaultLimits() Limits {
	return Limits{
		MaxInFlight:    4,
		BucketSize:     30,
		RefillEvery:    2 * time.Second,
		WebDeadline:    25 * time.Second,
		RecallDeadline: 10 * time.Second,
		SearxngLeg:     8 * time.Second,
		EmbedLeg:       5 * time.Second,
		QdrantLeg:      5 * time.Second,
		SearxngBody:    1 << 20,
		EmbedBody:      1 << 20,
		QdrantBody:     2 << 20,
		ScopeBody:      8 << 20,
	}
}

func (l Limits) withDefaults() Limits {
	d := DefaultLimits()
	if l.MaxInFlight > 0 {
		d.MaxInFlight = l.MaxInFlight
	}
	if l.BucketSize > 0 {
		d.BucketSize = l.BucketSize
	}
	if l.RefillEvery > 0 {
		d.RefillEvery = l.RefillEvery
	}
	if l.WebDeadline > 0 {
		d.WebDeadline = l.WebDeadline
	}
	if l.RecallDeadline > 0 {
		d.RecallDeadline = l.RecallDeadline
	}
	if l.SearxngLeg > 0 {
		d.SearxngLeg = l.SearxngLeg
	}
	if l.EmbedLeg > 0 {
		d.EmbedLeg = l.EmbedLeg
	}
	if l.QdrantLeg > 0 {
		d.QdrantLeg = l.QdrantLeg
	}
	if l.SearxngBody > 0 {
		d.SearxngBody = l.SearxngBody
	}
	if l.EmbedBody > 0 {
		d.EmbedBody = l.EmbedBody
	}
	if l.QdrantBody > 0 {
		d.QdrantBody = l.QdrantBody
	}
	if l.ScopeBody > 0 {
		d.ScopeBody = l.ScopeBody
	}
	return d
}

// Config is what a server is built from. The URLs are base URLs with no trailing slash,
// composed by the command tier from internal/config.
type Config struct {
	Set Set
	// Key is the bearer every route but /healthz requires. An empty or blank key is an
	// error: the core has no accept-any path.
	Key string

	SearxngURL string // web set

	EmbedURL       string // recall set
	EmbeddingModel string
	QdrantURL      string
	// Collection is Qdrant's collection for Open WebUI knowledge, <prefix>_knowledge.
	Collection string

	Limits Limits
}

// Deps is the injected seam: everything with a side effect.
type Deps struct {
	// Now is the clock the rate bucket and the request log read.
	Now func() time.Time
	// Upstream performs a request to SearXNG, the embedder or Qdrant. The live wiring is a
	// plain client that follows no redirect, because every target is a fixed address.
	Upstream func(*http.Request) (*http.Response, error)
	// FetchClient fetches result pages. The live wiring is websafe.SafeClient over
	// WebBounds(), so the connect-time SSRF hook validates every dialed address. Web set.
	FetchClient *http.Client
	// ReadScope opens the recall scope document. It is called on every recall request, so a
	// rewrite by the host is picked up without a restart. Recall set.
	ReadScope func() (io.ReadCloser, error)
	// Logf receives one line per tool request. Nil discards it.
	Logf func(format string, args ...any)
}

// Server is one set's tool server.
type Server struct {
	cfg    Config
	d      Deps
	lim    Limits
	tools  map[string]Tool
	spec   []byte
	loader *websafe.Loader

	inflight chan struct{}
	bucket   *bucket
}

// New builds the server for cfg.Set over the compiled-in tool table. It refuses an empty
// key, an unknown set and a missing seam.
func New(cfg Config, d Deps) (*Server, error) {
	return newServer(cfg, d, toolsFor(cfg.Set))
}

// newServer is New over an explicit table, so a test can prove the table drives both the
// routes and the document.
func newServer(cfg Config, d Deps, tools []Tool) (*Server, error) {
	if err := validate(cfg, d); err != nil {
		return nil, err
	}
	byPath := make(map[string]Tool, len(tools))
	for _, t := range tools {
		if t.Set != cfg.Set {
			return nil, fmt.Errorf("tool %s belongs to set %q, not %q", t.OperationID, t.Set, cfg.Set)
		}
		if _, dup := byPath[t.Path]; dup || t.serve == nil {
			return nil, fmt.Errorf("tool table: bad or duplicate entry %q", t.Path)
		}
		byPath[t.Path] = t
	}
	spec, err := buildSpec(cfg.Set, tools)
	if err != nil {
		return nil, err
	}
	lim := cfg.Limits.withDefaults()
	s := &Server{
		cfg: cfg, d: d, lim: lim, tools: byPath, spec: spec,
		inflight: make(chan struct{}, lim.MaxInFlight),
		bucket:   &bucket{size: lim.BucketSize, every: lim.RefillEvery},
	}
	if cfg.Set == SetWeb {
		s.loader = websafe.NewLoader(websafe.Deps{Client: d.FetchClient}, WebBounds())
	}
	return s, nil
}

func validate(cfg Config, d Deps) error {
	if strings.TrimSpace(cfg.Key) == "" {
		return errors.New("toolserver: refusing an empty bearer key")
	}
	if _, err := ParseSet(string(cfg.Set)); err != nil {
		return fmt.Errorf("toolserver: %w", err)
	}
	if d.Now == nil || d.Upstream == nil {
		return errors.New("toolserver: Deps.Now and Deps.Upstream are required")
	}
	switch cfg.Set {
	case SetWeb:
		if cfg.SearxngURL == "" || d.FetchClient == nil {
			return errors.New("toolserver: the web set needs SearxngURL and a FetchClient")
		}
	case SetRecall:
		if cfg.EmbedURL == "" || cfg.QdrantURL == "" || cfg.EmbeddingModel == "" || cfg.Collection == "" || d.ReadScope == nil {
			return errors.New("toolserver: the recall set needs EmbedURL, EmbeddingModel, QdrantURL, Collection and a ReadScope")
		}
	}
	return nil
}

// Handler returns the http.Handler for the server.
func (s *Server) Handler() http.Handler { return http.HandlerFunc(s.serveHTTP) }

// call is a validated tool request.
type call struct {
	ctx    context.Context
	q      string
	count  int
	userID string
}

// outcome is what a handler produced: a status, a JSON body and the numbers the log line
// reports. A refusal is an outcome too, with a fixed body.
type outcome struct {
	status  int
	body    any
	header  map[string]string
	results int
	flagged int
}

// refusalText is the fixed body text per status. None of it can hold input.
var refusalText = map[int]string{
	http.StatusNotFound:           "not found",
	http.StatusMethodNotAllowed:   "method not allowed: only GET is served",
	http.StatusUnauthorized:       "unauthorized",
	http.StatusForbidden:          "forbidden: the caller's identity is missing or malformed",
	http.StatusTooManyRequests:    "too many requests: try again shortly",
	http.StatusBadGateway:         "the upstream service failed or answered out of shape",
	http.StatusServiceUnavailable: "recall is not available: the recall index has not been built, run `villa recall index`",
	http.StatusGatewayTimeout:     "the request deadline passed",
}

func refuse(status int) outcome {
	return outcome{status: status, body: map[string]string{"error": refusalText[status]}}
}

func refuseBadRequest(t Tool) outcome {
	text := fmt.Sprintf("invalid request: q is required (1 to %d characters, no control characters), "+
		"count is an optional integer from 1 to %d, and no other parameter is accepted", maxQueryRunes, t.MaxCount)
	return outcome{status: http.StatusBadRequest, body: map[string]string{"error": text}}
}

// fail maps an upstream error to a refusal: a passed request deadline is a 504, anything
// else a 502. The error itself is never reported.
func fail(ctx context.Context) outcome {
	if ctx.Err() != nil {
		return refuse(http.StatusGatewayTimeout)
	}
	return refuse(http.StatusBadGateway)
}

func (s *Server) serveHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/healthz":
		if o, ok := s.gate(r, false); !ok {
			s.writeOutcome(w, o)
			return
		}
		s.writeOutcome(w, outcome{status: http.StatusOK, body: map[string]string{"status": "ok"}})
	case "/openapi.json":
		if o, ok := s.gate(r, true); !ok {
			s.writeOutcome(w, o)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write(s.spec)
	default:
		t, ok := s.tools[r.URL.Path]
		if !ok {
			s.writeOutcome(w, refuse(http.StatusNotFound))
			return
		}
		start := s.d.Now()
		o := s.serveTool(r, t)
		s.writeOutcome(w, o)
		if s.d.Logf != nil {
			s.d.Logf("tool=%s status=%d duration=%s results=%d flagged=%d",
				t.OperationID, o.status, s.d.Now().Sub(start), o.results, o.flagged)
		}
	}
}

// gate is the method and bearer check for the two built-in routes. It returns the refusal
// and false when the request may not proceed.
func (s *Server) gate(r *http.Request, auth bool) (outcome, bool) {
	if r.Method != http.MethodGet {
		return methodNotAllowed(), false
	}
	if auth && !s.authOK(r) {
		return refuse(http.StatusUnauthorized), false
	}
	return outcome{}, true
}

func methodNotAllowed() outcome {
	o := refuse(http.StatusMethodNotAllowed)
	o.header = map[string]string{"Allow": http.MethodGet}
	return o
}

// serveTool runs the gates in their order: method, bearer, identity, concurrency and rate,
// parameters, then the tool.
func (s *Server) serveTool(r *http.Request, t Tool) outcome {
	if r.Method != http.MethodGet {
		return methodNotAllowed()
	}
	if !s.authOK(r) {
		return refuse(http.StatusUnauthorized)
	}
	var userID string
	if t.Identity {
		id, ok := identityOf(r)
		if !ok {
			return refuse(http.StatusForbidden)
		}
		userID = id
	}
	release, ok := s.admit()
	if !ok {
		o := refuse(http.StatusTooManyRequests)
		o.header = map[string]string{"Retry-After": strconv.Itoa(int(retryAfter / time.Second))}
		return o
	}
	defer release()

	q, count, ok := parseParams(r, t)
	if !ok {
		return refuseBadRequest(t)
	}
	deadline := s.lim.WebDeadline
	if t.Set == SetRecall {
		deadline = s.lim.RecallDeadline
	}
	ctx, cancel := context.WithTimeout(r.Context(), deadline)
	defer cancel()
	return t.serve(s, &call{ctx: ctx, q: q, count: count, userID: userID})
}

// retryAfter is the Retry-After the 429 carries.
const retryAfter = 2 * time.Second

func (s *Server) writeOutcome(w http.ResponseWriter, o outcome) {
	h := w.Header()
	h.Set("Content-Type", "application/json")
	h.Set("Cache-Control", "no-store")
	for k, v := range o.header {
		h.Set(k, v)
	}
	var buf strings.Builder
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(o.body); err != nil {
		w.WriteHeader(http.StatusInternalServerError)
		_, _ = w.Write([]byte(`{"error":"internal error"}` + "\n"))
		return
	}
	w.WriteHeader(o.status)
	_, _ = w.Write([]byte(buf.String()))
}

// bucket is a token bucket on an injected clock: size tokens, one back per interval. It
// accrues no credit while full, so a long idle never buys more than size.
type bucket struct {
	mu      sync.Mutex
	size    int
	every   time.Duration
	tokens  int
	last    time.Time
	started bool
}

func (b *bucket) take(now time.Time) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if !b.started {
		b.tokens, b.last, b.started = b.size, now, true
	}
	if n := int(now.Sub(b.last) / b.every); n > 0 {
		b.tokens = min(b.size, b.tokens+n)
		b.last = b.last.Add(time.Duration(n) * b.every)
	}
	if b.tokens == b.size {
		b.last = now
	}
	if b.tokens == 0 {
		return false
	}
	b.tokens--
	return true
}

// admit takes an in-flight slot and a rate token, or neither. The slot comes first so a
// refusal for concurrency spends no token.
func (s *Server) admit() (release func(), ok bool) {
	select {
	case s.inflight <- struct{}{}:
	default:
		return nil, false
	}
	if !s.bucket.take(s.d.Now()) {
		<-s.inflight
		return nil, false
	}
	return func() { <-s.inflight }, true
}

// WebBounds is the loader's bound set for the web tool: the loader defaults plus the
// per-result text cap. The cmd tier builds the SSRF-guarded client over the same value.
func WebBounds() websafe.Bounds {
	b := websafe.DefaultBounds()
	b.MaxContentRunes = webMaxRunes
	return b
}
