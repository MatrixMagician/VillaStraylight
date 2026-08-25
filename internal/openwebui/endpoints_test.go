package openwebui

// endpoints_test.go proves the connection-list reconciliation: that it converges, that
// it writes only when it must, and that it never loses or misattributes something the
// user owns.
//
// The seamed half runs against an httptest server through a transport adapter that
// mirrors the production contract (non-2xx is an error), so the real paths, the real
// request bodies and the real JSON round-trip are under test rather than stubbed.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

const villaPrimary = "http://villa-llama:8080/v1"
const villaResident = "http://villa-llama-qwen3:8080/v1"
const userEndpoint = "http://desktop.lan:1234/v1"
const userKey = "sk-the-users-own-secret"

// configServer is a fake Open WebUI admin config endpoint. It stores the document as
// raw bytes and replaces it verbatim on update, so a second reconciliation sees exactly
// what the first one wrote.
type configServer struct {
	raw          []byte
	gets, writes int
	srv          *httptest.Server
}

func newConfigServer(t *testing.T, doc string) *configServer {
	t.Helper()
	s := &configServer{raw: []byte(doc)}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case pathOpenAIConfig:
			s.gets++
			_, _ = w.Write(s.raw)
		case pathOpenAIConfigUpdate:
			s.writes++
			body, err := io.ReadAll(r.Body)
			if err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			s.raw = body
			_, _ = w.Write(body)
		default:
			http.Error(w, "unrouted "+r.URL.Path, http.StatusNotFound)
		}
	}))
	t.Cleanup(s.srv.Close)
	return s
}

func (s *configServer) client() *Client { return New(httpTransport(s.srv.URL)) }

// doc reads the stored document back through the same parse the client uses.
func (s *configServer) doc(t *testing.T) Config {
	t.Helper()
	var cfg Config
	if err := json.Unmarshal(s.raw, &cfg); err != nil {
		t.Fatalf("stored document does not parse: %v", err)
	}
	return cfg
}

// httpTransport is the test-side transport adapter. It holds the same contract the
// production curl adapter does: a non-2xx response is an error, never an empty result.
func httpTransport(base string) Transport {
	return func(ctx context.Context, req Request) ([]byte, error) {
		method := req.Method
		if method == "" {
			method = http.MethodGet
		}
		r, err := http.NewRequestWithContext(ctx, method, base+req.Path, bytes.NewReader(req.Body))
		if err != nil {
			return nil, err
		}
		if req.Token != "" {
			r.Header.Set("Authorization", "Bearer "+req.Token)
		}
		if req.Body != nil {
			r.Header.Set("Content-Type", "application/json")
		}
		resp, err := http.DefaultClient.Do(r)
		if err != nil {
			return nil, err
		}
		defer func() { _ = resp.Body.Close() }()
		out, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, err
		}
		if resp.StatusCode < 200 || resp.StatusCode > 299 {
			return nil, fmt.Errorf("http %d", resp.StatusCode)
		}
		return out, nil
	}
}

// wire builds a wire-shape document from parallel collections, so a test can express a
// malformed one that the typed model cannot represent.
func wire(enabled bool, urls, keys []string, configs map[string]string) string {
	raw := map[string]json.RawMessage{}
	for k, v := range configs {
		raw[k] = json.RawMessage(v)
	}
	if urls == nil {
		urls = []string{}
	}
	if keys == nil {
		keys = []string{}
	}
	b, err := json.Marshal(wireConfig{Enabled: enabled, URLs: urls, Keys: keys, Configs: raw})
	if err != nil {
		panic(err)
	}
	return string(b)
}

func urlsOf(cfg Config) []string { return endpointURLs(cfg) }

func keysOf(cfg Config) []string {
	keys := make([]string, len(cfg.Connections))
	for i, conn := range cfg.Connections {
		keys[i] = conn.Key
	}
	return keys
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// TestAnAlreadyCorrectListIsNotWritten is the promise that reconciliation is a
// converge-step, not a periodic overwrite: a document that already says what villa
// wants must report unchanged, so the caller performs no write at all.
func TestAnAlreadyCorrectListIsNotWritten(t *testing.T) {
	for _, tc := range []struct {
		name    string
		doc     string
		want    []string
		changed bool
	}{
		{
			name:    "exact list in exact order",
			doc:     wire(true, []string{villaPrimary, villaResident}, []string{NoAuthAPIKey, NoAuthAPIKey}, nil),
			want:    []string{villaPrimary, villaResident},
			changed: false,
		},
		{
			name:    "ours plus the user's, already settled",
			doc:     wire(true, []string{villaPrimary, userEndpoint}, []string{NoAuthAPIKey, userKey}, nil),
			want:    []string{villaPrimary},
			changed: false,
		},
		{
			name:    "same set in the wrong order is a change",
			doc:     wire(true, []string{villaResident, villaPrimary}, []string{NoAuthAPIKey, NoAuthAPIKey}, nil),
			want:    []string{villaPrimary, villaResident},
			changed: true,
		},
		{
			name:    "a missing resident endpoint is a change",
			doc:     wire(true, []string{villaPrimary}, []string{NoAuthAPIKey}, nil),
			want:    []string{villaPrimary, villaResident},
			changed: true,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var current Config
			if err := json.Unmarshal([]byte(tc.doc), &current); err != nil {
				t.Fatalf("parse: %v", err)
			}
			if _, changed := ReconcileEndpoints(current, tc.want); changed != tc.changed {
				t.Errorf("changed = %v, want %v", changed, tc.changed)
			}
		})
	}
}

// TestSyncEndpointsIsIdempotent is the operational promise: running the reconciliation
// twice performs exactly one write. Anything else means every `villa up` rewrites Open
// WebUI's database and no run can be trusted to be a no-op.
func TestSyncEndpointsIsIdempotent(t *testing.T) {
	s := newConfigServer(t, wire(true, []string{villaPrimary}, []string{NoAuthAPIKey}, nil))
	want := []string{villaPrimary, villaResident}

	first, err := s.client().SyncEndpoints(t.Context(), "tok", want)
	if err != nil {
		t.Fatalf("first sync: %v", err)
	}
	if !first.Wrote {
		t.Fatal("the first sync must write: the resident endpoint was missing")
	}

	second, err := s.client().SyncEndpoints(t.Context(), "tok", want)
	if err != nil {
		t.Fatalf("second sync: %v", err)
	}
	if second.Wrote {
		t.Error("the second sync must not write — reconciliation had already converged")
	}
	if s.writes != 1 {
		t.Errorf("server saw %d writes across two runs, want 1", s.writes)
	}
	if !equalStrings(second.Endpoints, want) {
		t.Errorf("endpoints = %v, want %v", second.Endpoints, want)
	}
}

// TestAUserAddedEndpointSurvives is the do-no-harm promise. A connection the user added
// by hand is not villa's to delete; it must survive reconciliation, after villa's own
// endpoints and in its existing relative order.
func TestAUserAddedEndpointSurvives(t *testing.T) {
	const secondUserEndpoint = "http://laptop.lan:8000/v1"
	s := newConfigServer(t, wire(true,
		[]string{userEndpoint, villaPrimary, secondUserEndpoint},
		[]string{userKey, NoAuthAPIKey, "sk-second"}, nil))

	if _, err := s.client().SyncEndpoints(t.Context(), "tok",
		[]string{villaPrimary, villaResident}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	got := urlsOf(s.doc(t))
	want := []string{villaPrimary, villaResident, userEndpoint, secondUserEndpoint}
	if !equalStrings(got, want) {
		t.Errorf("endpoints = %v, want %v (ours first, the user's after, in their own order)", got, want)
	}
}

// TestACarriedOverEndpointKeepsItsKey is the credential promise. Re-keying the list
// must not hand a carried-over endpoint the no-auth sentinel, which would silently
// break the user's authenticated connection while reporting success.
func TestACarriedOverEndpointKeepsItsKey(t *testing.T) {
	s := newConfigServer(t, wire(true,
		[]string{userEndpoint, villaPrimary},
		[]string{userKey, NoAuthAPIKey}, nil))

	if _, err := s.client().SyncEndpoints(t.Context(), "tok",
		[]string{villaPrimary, villaResident}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := s.doc(t)
	gotKeys := keysOf(stored)
	wantKeys := []string{NoAuthAPIKey, NoAuthAPIKey, userKey}
	if !equalStrings(gotKeys, wantKeys) {
		t.Fatalf("keys = %v, want %v", gotKeys, wantKeys)
	}
	if len(gotKeys) != len(stored.Connections) {
		t.Errorf("the keys array must match the URL count: %d keys, %d URLs", len(gotKeys), len(stored.Connections))
	}
}

// TestPerConnectionConfigFollowsItsEndpointAcrossAReorder is the subtle one.
// OPENAI_API_CONFIGS is keyed by list index, so moving an endpoint without re-keying
// hands its settings — prefix, enable flag, model filter — to whichever endpoint landed
// on its old index. Silent, and wrong in a way nothing surfaces.
func TestPerConnectionConfigFollowsItsEndpointAcrossAReorder(t *testing.T) {
	const primaryCfg = `{"prefix_id":"primary","enable":true}`
	const residentCfg = `{"prefix_id":"resident","enable":false}`
	const userCfg = `{"prefix_id":"desktop","enable":true}`

	// Stored order: resident, user, primary. Wanted order: primary, resident.
	s := newConfigServer(t, wire(true,
		[]string{villaResident, userEndpoint, villaPrimary},
		[]string{NoAuthAPIKey, userKey, NoAuthAPIKey},
		map[string]string{"0": residentCfg, "1": userCfg, "2": primaryCfg}))

	if _, err := s.client().SyncEndpoints(t.Context(), "tok",
		[]string{villaPrimary, villaResident}); err != nil {
		t.Fatalf("sync: %v", err)
	}

	stored := s.doc(t)
	want := map[string]string{
		villaPrimary:  primaryCfg,
		villaResident: residentCfg,
		userEndpoint:  userCfg,
	}
	if len(stored.Connections) != len(want) {
		t.Fatalf("got %d connections, want %d", len(stored.Connections), len(want))
	}
	for _, conn := range stored.Connections {
		var got, expect any
		if err := json.Unmarshal(conn.Config, &got); err != nil {
			t.Fatalf("config for %s does not parse: %v", conn.URL, err)
		}
		if err := json.Unmarshal([]byte(want[conn.URL]), &expect); err != nil {
			t.Fatalf("fixture: %v", err)
		}
		if fmt.Sprint(got) != fmt.Sprint(expect) {
			t.Errorf("%s carries config %s, want %s — a config was reassigned to another endpoint",
				conn.URL, conn.Config, want[conn.URL])
		}
	}

	// The re-keying must be by CURRENT index, or Open WebUI reads it back against the
	// wrong endpoint on its next boot.
	var raw wireConfig
	if err := json.Unmarshal(s.raw, &raw); err != nil {
		t.Fatalf("stored wire document: %v", err)
	}
	for i, url := range raw.URLs {
		key := fmt.Sprint(i)
		if _, ok := raw.Configs[key]; !ok {
			t.Errorf("no OPENAI_API_CONFIGS entry keyed %q for endpoint %s", key, url)
		}
	}
}

// TestSyncEndpointsFailsClosedOnAnUntrustworthyResponse is the refusal promise: a
// response villa cannot trust is an actionable error, never a silently defaulted list —
// and the diagnostic must never carry the user's API key, which is in the body.
func TestSyncEndpointsFailsClosedOnAnUntrustworthyResponse(t *testing.T) {
	for _, tc := range []struct {
		name    string
		handler http.HandlerFunc
		wantIn  string
	}{
		{
			name: "non-200 from the read",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				http.Error(w, "not an admin", http.StatusUnauthorized)
			},
			wantIn: "openai/config",
		},
		{
			name: "malformed body",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(`{"OPENAI_API_BASE_URLS": ["` + villaPrimary + `"], "OPENAI_API_KEYS": ["` + userKey))
			},
			wantIn: "parse openai/config",
		},
		{
			name: "URL and key counts disagree",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(wire(true,
					[]string{villaPrimary, userEndpoint},
					[]string{userKey}, nil)))
			},
			wantIn: "2 base URLs but 1 keys",
		},
		{
			name: "a per-connection config indexes no endpoint",
			handler: func(w http.ResponseWriter, _ *http.Request) {
				_, _ = w.Write([]byte(wire(true,
					[]string{villaPrimary}, []string{NoAuthAPIKey},
					map[string]string{"7": `{"prefix_id":"ghost"}`})))
			},
			wantIn: `keyed "7"`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			srv := httptest.NewServer(tc.handler)
			t.Cleanup(srv.Close)

			got, err := New(httpTransport(srv.URL)).SyncEndpoints(
				t.Context(), "tok", []string{villaPrimary})
			if err == nil {
				t.Fatalf("an untrustworthy response must be an error; got %+v", got)
			}
			if !strings.Contains(err.Error(), tc.wantIn) {
				t.Errorf("error %q must name the failure (%q)", err, tc.wantIn)
			}
			if got.Wrote {
				t.Error("a failed read must never report a write")
			}
			if strings.Contains(err.Error(), userKey) {
				t.Errorf("the error leaked an API key: %q", err)
			}
		})
	}
}

// TestReconcileNeverEmitsMismatchedCollections is the wire-shape invariant the typed
// model exists to guarantee: whatever goes in, the emitted document has exactly as many
// keys as URLs, so Open WebUI never truncates or pads villa's list into something else.
func TestReconcileNeverEmitsMismatchedCollections(t *testing.T) {
	for _, tc := range []struct {
		name    string
		current Config
		want    []string
	}{
		{"empty current", Config{Enabled: true}, []string{villaPrimary, villaResident}},
		{"nothing wanted", Config{Enabled: true, Connections: []Connection{{URL: userEndpoint, Key: userKey}}}, nil},
		{"duplicate in want", Config{Enabled: true}, []string{villaPrimary, villaPrimary}},
		{"empty string in want", Config{Enabled: true}, []string{villaPrimary, ""}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			next, _ := ReconcileEndpoints(tc.current, tc.want)
			body, err := json.Marshal(next)
			if err != nil {
				t.Fatalf("marshal: %v", err)
			}
			var raw wireConfig
			if err := json.Unmarshal(body, &raw); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if len(raw.URLs) != len(raw.Keys) {
				t.Errorf("%d URLs but %d keys: %s", len(raw.URLs), len(raw.Keys), body)
			}
			seen := map[string]bool{}
			for _, u := range raw.URLs {
				if u == "" {
					t.Error("an empty base URL reached the wire document")
				}
				if seen[u] {
					t.Errorf("duplicate endpoint %q reached the wire document", u)
				}
				seen[u] = true
			}
		})
	}
}
