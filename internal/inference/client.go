package inference

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"
	"unicode"

	"github.com/MatrixMagician/VillaStraylight/internal/detect"
	"github.com/MatrixMagician/VillaStraylight/internal/llm"
	"github.com/MatrixMagician/VillaStraylight/internal/metrics"
)

// client.go is the control plane's ONE caller of a llama-server unit (ADR-0014). It
// owns the unit's address, the api key and every route villa itself calls on it:
// health, models, props, metrics, slots and chat completions. The key used to be
// threaded by hand through five bearer implementations and about twenty reads of
// config.InferenceSecret, and a caller that missed it got a silent 401 that its
// proof then read as Unknown (#252). Here the key is attached in one place, so a
// caller cannot reach a keyed route without it.
//
// The client does not deliver the key to third-party consumers (Open WebUI's
// connection list, the agents' provider configs, villa-inferproxy's forward leg):
// those processes call llama-server themselves, and only the key hand-off is villa's.

// readTimeout bounds every single-shot read the client makes, so a wedged
// llama-server cannot hang a caller that passed a background context. Chat
// completions stream for as long as a generation takes and carry their own bound.
const readTimeout = 3 * time.Second

// maxReadBody bounds a single-shot response body (memory-exhaustion guard). A body
// over it is refused whole, never parsed truncated: a counter or gauge line severed
// mid-value still parses, as a smaller and wrong number.
const maxReadBody = 64 << 10

// The llama-server routes villa calls. /health is the one it calls without the key.
const (
	routeHealth  = "/health"
	routeModels  = "/v1/models"
	routeProps   = "/props"
	routeMetrics = "/metrics"
	routeSlots   = "/slots"
	routeV1      = "/v1"
	routeChat    = "/v1/chat/completions"
)

// Client is the authenticated inference client. Build it once from a loaded config
// (cmd/villa's inferenceClient, or inNetworkInferenceClient for a probe that runs on
// villa.network) and pass the value around: callers hold a Client, never an endpoint
// string plus a key. The zero value addresses nothing, and every read on it
// degrades to the route's typed-Unknown.
type Client struct {
	root string
	key  string
	http *http.Client
	err  error // set when the key was refused; every route then fails with it
}

// errBadKey is the refusal for a key with a control character. It names the config
// field and never the key.
var errBadKey = errors.New("inference client: the api key contains a control character " +
	"(a hand-edited config.toml?): remove inference_secret from config.toml and run `villa up` " +
	"to generate a fresh one")

// NewClient builds a client for the llama-server at root (its base URL, without
// /v1). key is the LLAMA_API_KEY bearer the unit was rendered with; "" sends none,
// which a keyed unit answers with a 401 on every route but /health.
//
// A key with a control character is refused, fail closed: it could only come from a
// hand-edited config, and a newline in it would inject header lines through curl's
// `-H @-`. The returned client sends nothing; each route returns errBadKey (a
// CurlRequest carries it in Err).
func NewClient(root, key string) Client {
	c := Client{
		root: strings.TrimRight(root, "/"),
		key:  key,
		http: &http.Client{Timeout: readTimeout},
	}
	if strings.IndexFunc(key, unicode.IsControl) >= 0 {
		c.err = errBadKey
	}
	return c
}

// String shows the address and whether a key is set, never the key, so a stray log
// line or error wrap of a client cannot leak the credential.
func (c Client) String() string {
	key := "<none>"
	if c.key != "" {
		key = "<redacted>"
	}
	return fmt.Sprintf("inference.Client{root: %q, key: %s}", c.root, key)
}

// Format redacts under every verb: fmt would otherwise print the struct's fields
// for %v, %+v, %#v and %d.
func (c Client) Format(f fmt.State, _ rune) { _, _ = io.WriteString(f, c.String()) }

// HostClient is the client for the host-published loopback endpoint the primary
// inference unit and the transient validate run both publish on.
func HostClient(key string) Client { return NewClient(endpointURL(), key) }

// Health does one GET /health and returns the status code: 200 is ready, 503 is
// still loading the model. It sends no key: /health is llama-server's documented
// public route, so readiness reads the same with or without a credential.
func (c Client) Health(ctx context.Context) (int, error) {
	resp, err := c.get(ctx, routeHealth, false)
	if err != nil {
		return 0, err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, maxReadBody))
	return resp.StatusCode, nil
}

// PollHealth polls /health until it returns 200, bounded by timeout, treating a 503
// ("Loading model") as still loading. It is the readiness gate ONLY (Pitfall 5): a
// 200 means "accepting requests", never "offload happened".
func (c Client) PollHealth(ctx context.Context, timeout time.Duration) detect.Bool {
	return c.pollHealth(ctx, timeout, pollInterval)
}

// Models reports how many models GET /v1/models lists. reached is false only on a
// transport error; a non-200, an unparseable body and an empty list all read as
// zero, which a caller treats as "up but not serving a model yet".
func (c Client) Models(ctx context.Context) (n int, reached bool) {
	resp, err := c.get(ctx, routeModels, true)
	if err != nil {
		return 0, false
	}
	defer resp.Body.Close()
	body, ok := readBounded(resp)
	if !ok {
		return 0, true
	}
	var raw struct {
		Data []json.RawMessage `json:"data"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return 0, true
	}
	return len(raw.Data), true
}

// Props reads GET /props for the config-identity drift overlay (corroboration only,
// never the residency proof). A transport error, a non-200 or an unparseable body
// yields nil: the typed-Unknown RunningOffloadVerdict never turns into a PASS or a
// FAIL.
func (c Client) Props(ctx context.Context) *PropsInfo {
	body, ok := c.read(ctx, routeProps)
	if !ok {
		return nil
	}
	var raw struct {
		ModelPath     string `json:"model_path"`
		DefaultParams struct {
			NCtx int `json:"n_ctx"`
		} `json:"default_generation_settings"`
		NCtx int `json:"n_ctx"`
	}
	if json.Unmarshal(body, &raw) != nil {
		return nil
	}
	nctx := raw.NCtx
	if nctx == 0 {
		nctx = raw.DefaultParams.NCtx
	}
	return &PropsInfo{ModelPath: raw.ModelPath, NCtx: nctx}
}

// Perf reads the /metrics rate gauges. ok=false (a 404 when --metrics is off, a 401,
// a transport error, an over-cap body) is the typed-Unknown the panel renders as
// "unavailable", never a zero rate shown as real.
func (c Client) Perf(ctx context.Context) (metrics.PerfSnapshot, bool) {
	body, ok := c.read(ctx, routeMetrics)
	if !ok {
		return metrics.PerfSnapshot{}, false
	}
	return metrics.ParsePerf(body), true
}

// Counters reads the /metrics cumulative token counters, each with its own Known
// flag. ok=false means the whole scrape was unavailable.
func (c Client) Counters(ctx context.Context) (metrics.CounterSample, bool) {
	body, ok := c.read(ctx, routeMetrics)
	if !ok {
		return metrics.CounterSample{}, false
	}
	return metrics.ParseCounters(body), true
}

// CacheCounters reads the /metrics prompt-cache reuse pair, each with its own Known
// flag. ok=false means the whole scrape was unavailable.
func (c Client) CacheCounters(ctx context.Context) (metrics.CacheSample, bool) {
	body, ok := c.read(ctx, routeMetrics)
	if !ok {
		return metrics.CacheSample{}, false
	}
	return metrics.ParseCacheCounters(body), true
}

// Slots reads GET /slots as the narrow, prompt-free []metrics.Slot view. ok=false
// (--no-slots, a 401, a transport error, a malformed body) is typed-Unknown.
func (c Client) Slots(ctx context.Context) ([]metrics.Slot, bool) {
	body, ok := c.read(ctx, routeSlots)
	if !ok {
		return nil, false
	}
	return metrics.ParseSlots(body)
}

// Chat is the chat-completions route, streaming (StreamChat) and not (Complete),
// over internal/llm's OpenAI wire client with the key attached and the whole
// exchange bounded by timeout.
func (c Client) Chat(timeout time.Duration) *llm.OpenAIClient {
	return llm.NewOpenAIClient(llm.Options{BaseURL: c.root + routeV1, Timeout: timeout, APIKey: c.key})
}

// CurlRequest is one call to a llama-server route made by curl inside a helper
// container on villa.network, rather than by this process. Args is curl's argv for
// the route; when the client holds a key, Args tells curl to read its headers from
// stdin (`-H @-`) and Stdin carries the Authorization line, so the key never appears
// on podman's or curl's command line, which any local user can read from /proc.
//
// Err is set when the client refused its key (see NewClient); the caller must not run
// the request.
type CurlRequest struct {
	Args  []string
	Stdin []byte
	Err   error
}

// CurlChatCompletions is a POST of body to the chat-completions route.
func (c Client) CurlChatCompletions(body []byte) CurlRequest {
	return c.curl("-X", "POST", c.root+routeChat, "-H", "Content-Type: application/json", "-d", string(body))
}

// CurlModels is a GET of the model list.
func (c Client) CurlModels() CurlRequest { return c.curl(c.root + routeModels) }

func (c Client) curl(args ...string) CurlRequest {
	if c.err != nil {
		return CurlRequest{Err: c.err}
	}
	if c.key == "" {
		return CurlRequest{Args: args}
	}
	return CurlRequest{
		Args:  append([]string{"-H", "@-"}, args...),
		Stdin: []byte("Authorization: Bearer " + c.key + "\n"),
	}
}

// get issues one bounded GET of route, attaching the key when keyed is true and the
// client holds one.
func (c Client) get(ctx context.Context, route string, keyed bool) (*http.Response, error) {
	if c.err != nil {
		return nil, c.err
	}
	if c.http == nil {
		return nil, fmt.Errorf("inference client: no endpoint")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.root+route, nil)
	if err != nil {
		return nil, err
	}
	if keyed && c.key != "" {
		req.Header.Set("Authorization", "Bearer "+c.key)
	}
	return c.http.Do(req)
}

// read is a keyed GET that yields the body of a 200 only, bounded by maxReadBody.
func (c Client) read(ctx context.Context, route string) ([]byte, bool) {
	resp, err := c.get(ctx, route, true)
	if err != nil {
		return nil, false
	}
	defer resp.Body.Close()
	return readBounded(resp)
}

// readBounded reads a 200 body, refusing a read error or a body over maxReadBody
// rather than handing a truncated one to a parser.
func readBounded(resp *http.Response) ([]byte, bool) {
	if resp.StatusCode != http.StatusOK {
		return nil, false
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, maxReadBody+1))
	if err != nil || len(body) > maxReadBody {
		return nil, false
	}
	return body, true
}
