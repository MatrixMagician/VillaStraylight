package main

import (
	"bytes"
	"context"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/toolserver"
)

// tools_serve_test.go drives the hidden `villa tools-serve` cmd off-network: runToolsServe
// is fed a fake Serve (no real listener) and in-memory upstreams through the Deps seam, in
// the shape of websafe_test.go. The pure core is tested in internal/toolserver.

// toolsServeTestCmd builds a cobra command with the flags runToolsServe reads, set to the
// given set, plus captured out/err.
func toolsServeTestCmd(t *testing.T, set string, extra ...string) (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	t.Helper()
	cmd := newToolsServe()
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	args := []string{}
	if set != "" {
		args = append(args, "--set", set)
	}
	if err := cmd.ParseFlags(append(args, extra...)); err != nil {
		t.Fatalf("ParseFlags: %v", err)
	}
	return cmd, &out, &errOut
}

// stubToolsDeps returns deps whose upstreams answer from memory and whose Serve captures
// the handler instead of binding a socket.
func stubToolsDeps(key string, handler *http.Handler) *toolsServeDeps {
	return &toolsServeDeps{
		Key: key,
		Upstream: func(req *http.Request) (*http.Response, error) {
			rec := httptest.NewRecorder()
			switch {
			case strings.HasSuffix(req.URL.Path, "/search"):
				_, _ = rec.WriteString(`{"results":[]}`)
			case strings.HasSuffix(req.URL.Path, "/v1/embeddings"):
				_, _ = rec.WriteString(`{"data":[{"embedding":[0.5]}]}`)
			default:
				_, _ = rec.WriteString(`{"result":{"points":[]}}`)
			}
			return rec.Result(), nil
		},
		FetchClient: &http.Client{},
		OpenScope: func(string) (io.ReadCloser, error) {
			return io.NopCloser(strings.NewReader(`{"schema_version":1,"knowledge_id":"kb","files":{"f":"u1"}}`)), nil
		},
		Serve: func(_ context.Context, _ string, h http.Handler) error {
			*handler = h
			return nil
		},
	}
}

// TestToolServeRefusesEmptyBearer: runToolsServe refuses to start with an empty bearer,
// for either set, before it binds anything, and says where the key comes from.
func TestToolServeRefusesEmptyBearer(t *testing.T) {
	for _, set := range []string{"web", "recall"} {
		cmd, out, errOut := toolsServeTestCmd(t, set, "--embedding-model", "m")
		var h http.Handler
		d := stubToolsDeps("", &h)
		served := false
		d.Serve = func(context.Context, string, http.Handler) error { served = true; return nil }

		if code := runToolsServe(cmd, nil, d); code != exitBlocked {
			t.Errorf("%s: runToolsServe = %d, want %d (exitBlocked)", set, code, exitBlocked)
		}
		if served {
			t.Errorf("%s: Serve was called with an empty bearer", set)
		}
		if !strings.Contains(errOut.String(), "VILLA_TOOLS_KEY") {
			t.Errorf("%s: the refusal must name VILLA_TOOLS_KEY: %q", set, errOut.String())
		}
		if strings.Contains(out.String(), "listening") {
			t.Errorf("%s: printed a listen line before refusing: %q", set, out.String())
		}
	}
}

// TestToolsServeRefusesBadSetAndFlags: an unknown or missing set, and the recall set
// without an embedding model, refuse to start.
func TestToolsServeRefusesBadSetAndFlags(t *testing.T) {
	for name, args := range map[string][]string{
		"unknown set":   {"--set", "both"},
		"missing set":   nil,
		"empty set":     {"--set", ""},
		"recall no emb": {"--set", "recall"},
	} {
		cmd := newToolsServe()
		var errOut bytes.Buffer
		cmd.SetErr(&errOut)
		cmd.SetOut(io.Discard)
		if err := cmd.ParseFlags(args); err != nil {
			t.Fatal(err)
		}
		var h http.Handler
		d := stubToolsDeps("key", &h)
		if code := runToolsServe(cmd, nil, d); code != exitBlocked {
			t.Errorf("%s: runToolsServe = %d, want exitBlocked", name, code)
		}
		if h != nil {
			t.Errorf("%s: a handler was served", name)
		}
	}
}

// TestRunToolsServeServesOneSet: each set serves its own route and the shared ones, and
// nothing of the other set.
func TestRunToolsServeServesOneSet(t *testing.T) {
	get := func(h http.Handler, path string, hdr ...string) *httptest.ResponseRecorder {
		r := httptest.NewRequest(http.MethodGet, path, nil)
		r.Header.Set("Authorization", "Bearer key")
		for i := 0; i+1 < len(hdr); i += 2 {
			r.Header.Set(hdr[i], hdr[i+1])
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, r)
		return rec
	}

	cmd, out, _ := toolsServeTestCmd(t, "web")
	var h http.Handler
	if code := runToolsServe(cmd, nil, stubToolsDeps("key", &h)); code != exitPass {
		t.Fatalf("web: runToolsServe = %d, want exitPass", code)
	}
	if !strings.Contains(out.String(), "listening on http://0.0.0.0:8092") {
		t.Errorf("web: listen line = %q", out.String())
	}
	if rec := get(h, "/healthz"); rec.Code != 200 {
		t.Errorf("web /healthz = %d", rec.Code)
	}
	if rec := get(h, "/web/search?q=x"); rec.Code != 200 {
		t.Errorf("web /web/search = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/recall/search?q=x", toolserver.IdentityHeader, "u1"); rec.Code != 404 {
		t.Errorf("the web set serves the recall route: %d", rec.Code)
	}

	cmd, out, _ = toolsServeTestCmd(t, "recall", "--embedding-model", "m", "--port", "8093")
	h = nil
	if code := runToolsServe(cmd, nil, stubToolsDeps("key", &h)); code != exitPass {
		t.Fatalf("recall: runToolsServe = %d, want exitPass", code)
	}
	if !strings.Contains(out.String(), "listening on http://0.0.0.0:8093") {
		t.Errorf("recall: listen line = %q", out.String())
	}
	if rec := get(h, "/recall/search?q=x"); rec.Code != 403 {
		t.Errorf("recall without identity = %d, want 403", rec.Code)
	}
	if rec := get(h, "/recall/search?q=x", toolserver.IdentityHeader, "u1"); rec.Code != 200 {
		t.Errorf("recall with identity = %d: %s", rec.Code, rec.Body.String())
	}
	if rec := get(h, "/web/search?q=x"); rec.Code != 404 {
		t.Errorf("the recall set serves the web route: %d", rec.Code)
	}
}

// TestToolsServeCommandIsHidden: `villa tools-serve` is registered in the tree, Hidden (an
// internal container entrypoint), and takes no positional argument.
func TestToolsServeCommandIsHidden(t *testing.T) {
	var found *cobra.Command
	for _, c := range newRoot().Commands() {
		if c.Name() == "tools-serve" {
			found = c
		}
	}
	if found == nil {
		t.Fatal("tools-serve is not registered in the cobra tree")
	}
	if !found.Hidden {
		t.Error("tools-serve must be Hidden")
	}
	for _, f := range []string{"set", "host", "port", "embedding-model", "scope-file"} {
		if found.Flags().Lookup(f) == nil {
			t.Errorf("tools-serve has no --%s flag", f)
		}
	}
	if err := found.Args(found, []string{"extra"}); err == nil {
		t.Error("tools-serve must take no positional argument")
	}
}

// TestToolsServeHTTPServerCaps: the production server sets the read deadlines and the
// 16 KiB header cap, and has no WriteTimeout. net/http reads 4 KiB past MaxHeaderBytes before
// it refuses, so a request with headers well over the cap is a
// 431 from the real server.
func TestToolsServeHTTPServerCaps(t *testing.T) {
	srv := toolsHTTPServer("127.0.0.1:0", http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	if srv.ReadHeaderTimeout != toolserver.ReadHeaderTimeout || srv.IdleTimeout != toolserver.IdleTimeout {
		t.Errorf("deadlines = %v / %v", srv.ReadHeaderTimeout, srv.IdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want none", srv.WriteTimeout)
	}
	if srv.MaxHeaderBytes != 16<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want 16 KiB", srv.MaxHeaderBytes)
	}

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Skipf("no loopback listener: %v", err)
	}
	go func() { _ = srv.Serve(ln) }()
	defer srv.Close()

	do := func(pad int) int {
		req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/", nil)
		req.Header.Set("X-Pad", strings.Repeat("a", pad))
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		return resp.StatusCode
	}
	if got := do(100); got != http.StatusOK {
		t.Errorf("a small request = %d, want 200", got)
	}
	if got := do(24 << 10); got != http.StatusRequestHeaderFieldsTooLarge {
		t.Errorf("a 24 KiB header = %d, want 431", got)
	}
}

// TestLiveToolsServeDeps: the live wiring reads the bearer from VILLA_TOOLS_KEY, fetches
// through the SSRF-guarded client (a loopback target is refused at connect time), and
// follows no redirect on the fixed upstreams.
func TestLiveToolsServeDeps(t *testing.T) {
	t.Setenv("VILLA_TOOLS_KEY", "from-env")
	d, err := liveToolsServeDeps()
	if err != nil {
		t.Fatal(err)
	}
	if d.Key != "from-env" {
		t.Errorf("Key = %q", d.Key)
	}

	secret := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("internal")) }))
	defer secret.Close()
	if resp, err := d.FetchClient.Get(secret.URL); err == nil {
		resp.Body.Close()
		t.Fatal("the fetch client reached a loopback address: it is not the SSRF-guarded client")
	} else if !strings.Contains(err.Error(), "SSRF") {
		t.Errorf("want an SSRF refusal, got %v", err)
	}

	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("elsewhere")) }))
	defer target.Close()
	redirector := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer redirector.Close()
	req, _ := http.NewRequest(http.MethodGet, redirector.URL, nil)
	resp, err := d.Upstream(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusFound {
		t.Errorf("the upstream client followed a redirect (status %d)", resp.StatusCode)
	}

	t.Setenv("VILLA_TOOLS_KEY", "")
	if d, err := liveToolsServeDeps(); err != nil || d.Key != "" {
		t.Errorf("an unset key is carried as empty for runToolsServe to refuse: %v %q", err, d.Key)
	}
}
