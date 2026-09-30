package inference

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
)

// keyedLlama is an httptest llama-server run with --api-key: /health and /v1/models
// are public, every other route answers 401 without `Authorization: Bearer <key>`.
// It records the Authorization header each route received.
type keyedLlama struct {
	*httptest.Server
	mu   sync.Mutex
	auth map[string][]string
}

func newKeyedLlama(t *testing.T, key string) *keyedLlama {
	t.Helper()
	s := &keyedLlama{auth: map[string][]string{}}
	s.Server = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got := r.Header.Get("Authorization")
		s.mu.Lock()
		s.auth[r.URL.Path] = append(s.auth[r.URL.Path], got)
		s.mu.Unlock()
		public := r.URL.Path == "/health" || r.URL.Path == "/v1/models"
		if !public && got != "Bearer "+key {
			http.Error(w, `{"error":"Invalid API Key"}`, http.StatusUnauthorized)
			return
		}
		switch r.URL.Path {
		case "/health":
			_, _ = w.Write([]byte(`{"status":"ok"}`))
		case "/v1/models":
			_, _ = w.Write([]byte(`{"data":[{"id":"qwen3"}]}`))
		case "/props":
			_, _ = w.Write([]byte(`{"model_path":"/models/qwen3.gguf","default_generation_settings":{"n_ctx":8192}}`))
		case "/metrics":
			_, _ = w.Write([]byte("llamacpp:predicted_tokens_seconds 41.25\n" +
				"llamacpp:prompt_tokens_total 130572\nllamacpp:tokens_predicted_total 48913\n" +
				"llamacpp:prompt_tokens_cache_n_total 4096\nllamacpp:tokens_cache_n_total 3072\n"))
		case "/slots":
			_, _ = w.Write([]byte(`[{"id":0,"n_ctx":8192,"is_processing":true}]`))
		case "/v1/chat/completions":
			var req struct {
				Stream bool `json:"stream"`
			}
			_ = json.NewDecoder(r.Body).Decode(&req)
			if req.Stream {
				sseDeltas(w, "o", "k")
				return
			}
			_, _ = w.Write([]byte(`{"timings":{"predicted_n":2,"predicted_per_second":20}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s
}

// headers returns every Authorization header the route received, in order.
func (s *keyedLlama) headers(route string) []string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.auth[route]
}

// driveEveryRoute calls every route the client owns once, and reports which ones
// came back as a real reading.
func driveEveryRoute(t *testing.T, c Client) map[string]bool {
	t.Helper()
	ctx := t.Context()
	got := map[string]bool{}
	code, err := c.Health(ctx)
	got["/health"] = err == nil && code == http.StatusOK
	ready := c.PollHealth(ctx, time.Second)
	got["/health (poll)"] = ready.Known && ready.Value
	n, reached := c.Models(ctx)
	got["/v1/models"] = reached && n == 1
	props := c.Props(ctx)
	got["/props"] = props != nil && props.ModelPath == "/models/qwen3.gguf" && props.NCtx == 8192
	perf, ok := c.Perf(ctx)
	got["/metrics (perf)"] = ok && perf.GenTokensPerSec == 41.25
	counters, ok := c.Counters(ctx)
	got["/metrics (counters)"] = ok && counters.PromptTokensKnown && counters.PromptTokensTotal == 130572
	cache, ok := c.CacheCounters(ctx)
	got["/metrics (cache)"] = ok && cache.CacheKnown && cache.CacheN == 3072
	slots, ok := c.Slots(ctx)
	got["/slots"] = ok && len(slots) == 1 && slots[0].IsProcessing
	timings, err := c.Chat(time.Second).Complete(ctx, llm.ChatRequest{Model: "qwen3",
		Messages: []llm.Message{{Role: llm.RoleUser, Content: "hi"}}}, 8, 1, 0)
	got["chat (complete)"] = err == nil && timings.PredictedN == 2
	got["chat (stream)"] = c.GenerationProbe(ctx, "qwen3").OK
	return got
}

// TestClientSendsTheKeyOnEveryRouteButHealth is the fixture ADR-0014 rests on:
// against a keyed llama-server, the client reaches every route it owns — so each
// keyed route received the bearer — and /health received none, because readiness
// must read the same with or without a credential.
func TestClientSendsTheKeyOnEveryRouteButHealth(t *testing.T) {
	const key = "s3cret-inference-key"
	srv := newKeyedLlama(t, key)

	for route, ok := range driveEveryRoute(t, NewClient(srv.URL, key)) {
		if !ok {
			t.Errorf("%s: no reading through a keyed client against a keyed server", route)
		}
	}
	for _, route := range []string{"/v1/models", "/props", "/metrics", "/slots", "/v1/chat/completions"} {
		hs := srv.headers(route)
		if len(hs) == 0 {
			t.Errorf("%s was never called", route)
		}
		for _, h := range hs {
			if h != "Bearer "+key {
				t.Errorf("%s received Authorization %q, want the bearer", route, h)
			}
		}
	}
	for _, h := range srv.headers("/health") {
		if h != "" {
			t.Errorf("/health received Authorization %q, want none", h)
		}
	}
}

// TestKeylessClientReadsOnlyThePublicRoutes is the 401-by-omission #252 was: a
// client built without the key still reads /health and the public model list, and
// every keyed route degrades to its typed-Unknown rather than a fabricated reading
// or a panic. No request carries an Authorization header at all.
func TestKeylessClientReadsOnlyThePublicRoutes(t *testing.T) {
	srv := newKeyedLlama(t, "s3cret-inference-key")

	got := driveEveryRoute(t, NewClient(srv.URL, ""))
	for route, ok := range got {
		public := strings.HasPrefix(route, "/health") || route == "/v1/models"
		if ok != public {
			t.Errorf("%s: reading=%v, want %v (only the public routes answer a keyless client)", route, ok, public)
		}
	}
	srv.mu.Lock()
	defer srv.mu.Unlock()
	for route, hs := range srv.auth {
		for _, h := range hs {
			if h != "" {
				t.Errorf("%s received Authorization %q from a keyless client", route, h)
			}
		}
	}
}

// TestCurlRequestKeepsTheKeyOffTheCommandLine: an in-network curl request names the
// route and reads its Authorization line from stdin (`-H @-`), so the key is never
// in the argv podman and curl run with; a keyless client adds neither.
func TestCurlRequestKeepsTheKeyOffTheCommandLine(t *testing.T) {
	const key = "s3cret-inference-key"
	c := NewClient("http://villa-llama:8080", key)
	for name, req := range map[string]CurlRequest{
		"chat":   c.CurlChatCompletions([]byte(`{"model":"qwen3"}`)),
		"models": c.CurlModels(),
	} {
		argv := strings.Join(req.Args, " ")
		if strings.Contains(argv, key) {
			t.Errorf("%s: the key is on the command line: %q", name, argv)
		}
		if !strings.Contains(argv, "-H @-") {
			t.Errorf("%s: curl is not told to read headers from stdin: %q", name, argv)
		}
		if string(req.Stdin) != "Authorization: Bearer "+key+"\n" {
			t.Errorf("%s: stdin = %q, want the Authorization line", name, req.Stdin)
		}
	}
	if got := c.CurlChatCompletions(nil).Args; !contains(got, "http://villa-llama:8080/v1/chat/completions") {
		t.Errorf("chat curl args %q do not name the in-network chat route", got)
	}
	if got := c.CurlModels().Args; !contains(got, "http://villa-llama:8080/v1/models") {
		t.Errorf("models curl args %q do not name the in-network model route", got)
	}

	keyless := NewClient("http://villa-llama:8080", "").CurlChatCompletions(nil)
	if keyless.Stdin != nil || contains(keyless.Args, "@-") {
		t.Errorf("a keyless request must carry no header source, got args %q stdin %q", keyless.Args, keyless.Stdin)
	}
}

func contains(xs []string, want string) bool {
	for _, x := range xs {
		if x == want {
			return true
		}
	}
	return false
}

// TestUnavailableScrapeIsTypedUnknown is the Pitfall 2 guard, now the client's: a
// 404 /metrics (--metrics absent), a transport error and a body over the read cap
// all yield ok=false with a zero reading, never a zero rate or count presented as
// real. The over-cap case is refused whole because a counter line severed mid-value
// (`...predicted_total 1305` from `130572`) still parses, and the reset-aware usage
// fold would read it as a counter reset.
func TestUnavailableScrapeIsTypedUnknown(t *testing.T) {
	notFound := httptest.NewServer(http.NotFoundHandler())
	defer notFound.Close()

	var big strings.Builder
	big.WriteString("llamacpp:prompt_tokens_total 130572\nllamacpp:tokens_cache_n_total 3072\n")
	for big.Len() < maxReadBody+1 {
		big.WriteString("# padding past the read cap\n")
	}
	overCap := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(big.String()))
	}))
	defer overCap.Close()

	for name, c := range map[string]Client{
		"404":             NewClient(notFound.URL, "k"),
		"transport error": NewClient("http://127.0.0.1:1", "k"), // nothing listens on the discard port
		"over-cap body":   NewClient(overCap.URL, "k"),
		"zero client":     {},
	} {
		ctx := t.Context()
		if snap, ok := c.Perf(ctx); ok || snap != (metrics.PerfSnapshot{}) {
			t.Errorf("%s: Perf = %+v ok=%v, want the zero snapshot and ok=false", name, snap, ok)
		}
		if _, ok := c.Counters(ctx); ok {
			t.Errorf("%s: Counters ok=true, want false", name)
		}
		if _, ok := c.CacheCounters(ctx); ok {
			t.Errorf("%s: CacheCounters ok=true, want false", name)
		}
		if _, ok := c.Slots(ctx); ok {
			t.Errorf("%s: Slots ok=true, want false", name)
		}
		if c.Props(ctx) != nil {
			t.Errorf("%s: Props non-nil, want the typed-Unknown nil", name)
		}
	}
}

// TestClientFormatNeverPrintsTheKey: a Client formatted with any verb, %+v and %#v
// included, shows its address and that a key is set, never the key. A stray
// log line or error wrap of the client must not leak the credential.
func TestClientFormatNeverPrintsTheKey(t *testing.T) {
	const key = "s3cret-inference-key"
	keyed := NewClient("http://villa-llama:8080", key)
	for _, verb := range []string{"%v", "%+v", "%#v", "%s", "%d"} {
		got := fmt.Sprintf(verb, keyed)
		if strings.Contains(got, key) {
			t.Errorf("%s printed the key: %q", verb, got)
		}
		if want := `inference.Client{root: "http://villa-llama:8080", key: <redacted>}`; got != want {
			t.Errorf("%s = %q, want %q", verb, got, want)
		}
	}
	if got, want := fmt.Sprint(&keyed), `inference.Client{root: "http://villa-llama:8080", key: <redacted>}`; got != want {
		t.Errorf("a pointer to the client printed %q, want %q", got, want)
	}
	if got, want := fmt.Sprintf("%+v", NewClient("http://villa-llama:8080", "")), `inference.Client{root: "http://villa-llama:8080", key: <none>}`; got != want {
		t.Errorf("a keyless client printed %q, want %q", got, want)
	}
}

// TestClientRefusesAControlCharacterKey: a key with a control character (only
// possible in a hand-edited config.toml) would inject header lines through curl's
// `-H @-`. The client refuses it: every read fails with an error that names
// config.toml's inference_secret and never echoes the key, and nothing is sent.
func TestClientRefusesAControlCharacterKey(t *testing.T) {
	var hits int
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { hits++ }))
	defer srv.Close()
	ctx := t.Context()
	for name, key := range map[string]string{
		"newline": "abc\nX-Injected: 1", "carriage return": "abc\rdef", "nul": "abc\x00def",
		"tab": "abc\tdef", "delete": "abc\x7fdef", "c1 control": "abc\u0085def",
	} {
		c := NewClient(srv.URL, key)
		code, err := c.Health(ctx)
		if err == nil || code != 0 {
			t.Errorf("%s: Health = (%d, %v), want a refusal", name, code, err)
			continue
		}
		if !strings.Contains(err.Error(), "inference_secret") {
			t.Errorf("%s: error %q does not name config.toml's inference_secret", name, err)
		}
		if strings.Contains(err.Error(), "abc") {
			t.Errorf("%s: error %q echoes the key", name, err)
		}
		if _, reached := c.Models(ctx); reached {
			t.Errorf("%s: Models reached the server", name)
		}
		if c.Props(ctx) != nil {
			t.Errorf("%s: Props reached the server", name)
		}
		if err := c.Chat(time.Second).StreamChat(ctx, llm.ChatRequest{Model: "m"}, func(string) error { return nil }); err == nil {
			t.Errorf("%s: StreamChat sent a request with the malformed key", name)
		} else if strings.Contains(err.Error(), "abc") {
			t.Errorf("%s: StreamChat error %q echoes the key", name, err)
		}
	}
	if hits != 0 {
		t.Errorf("a refused client sent %d request(s) to the server", hits)
	}
}
