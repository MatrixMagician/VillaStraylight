package main

import (
	"bytes"
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
)

// inferproxy_test.go drives the hidden `villa inferproxy-serve` cmd off-network:
// runInferproxy is fed a fake Serve (no real listener) via the Deps seam, and the
// forwarded requests land on an httptest upstream standing in for villa-llama —
// no real villa-llama and no real sandbox network are exercised.

// inferproxyTestCmd builds a cobra command with the host/port flags + captured
// out/err, mirroring websafeTestCmd. The flags must exist because
// runInferproxy reads them.
func inferproxyTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("host", "0.0.0.0", "")
	cmd.Flags().Int("port", config.InferproxyPort, "")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// stubLlama is an httptest server standing in for villa-llama, recording the
// method, path and Authorization header it received.
type stubLlama struct {
	srv    *httptest.Server
	method string
	path   string
	auth   string
}

func newStubLlama(t *testing.T) *stubLlama {
	t.Helper()
	s := &stubLlama{}
	s.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		s.method = r.Method
		s.path = r.URL.Path
		s.auth = r.Header.Get("Authorization")
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte(`{"ok":true}`))
	}))
	t.Cleanup(s.srv.Close)
	return s
}

// TestRunInferproxyForwardsAllowedRoute: an allowed route (POST
// /v1/chat/completions) is forwarded to the target with the real api key
// injected as the Bearer credential, and the upstream's response passes
// through unchanged.
func TestRunInferproxyForwardsAllowedRoute(t *testing.T) {
	up := newStubLlama(t)
	cmd, out, _ := inferproxyTestCmd()

	var handler http.Handler
	d := &inferproxyDeps{
		Target:       up.srv.URL,
		APIKey:       "secret-key",
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve: func(_ context.Context, _ string, h http.Handler) error {
			handler = h
			return nil
		},
	}

	code := runInferproxy(cmd, nil, d)
	if code != exitPass {
		t.Fatalf("runInferproxy = %d, want %d (exitPass)", code, exitPass)
	}
	if !strings.Contains(out.String(), "listening on http://0.0.0.0:"+strconv.Itoa(config.InferproxyPort)) {
		t.Fatalf("output missing container-internal listen addr:\n%s", out.String())
	}

	req := httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(`{"model":"m"}`))
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handler status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	if up.method != http.MethodPost || up.path != "/v1/chat/completions" {
		t.Errorf("upstream saw %s %s, want POST /v1/chat/completions", up.method, up.path)
	}
	if want := "Bearer secret-key"; up.auth != want {
		t.Errorf("upstream Authorization = %q, want %q", up.auth, want)
	}
}

// TestRunInferproxyRefusesDisallowedRoute guards GHSA-gvp9/ADR-0011: every
// route OTHER than POST /v1/chat/completions and GET /v1/models is refused
// with 403 BEFORE it reaches the upstream — /slots, /metrics and /props
// fingerprint the host and must never cross the sandbox boundary.
func TestRunInferproxyRefusesDisallowedRoute(t *testing.T) {
	up := newStubLlama(t)
	cmd, _, _ := inferproxyTestCmd()

	var handler http.Handler
	d := &inferproxyDeps{
		Target:       up.srv.URL,
		APIKey:       "secret-key",
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve:        func(_ context.Context, _ string, h http.Handler) error { handler = h; return nil },
	}
	if code := runInferproxy(cmd, nil, d); code != exitPass {
		t.Fatalf("runInferproxy = %d, want exitPass", code)
	}

	for _, route := range []struct{ method, path string }{
		{http.MethodGet, "/slots"},
		{http.MethodGet, "/metrics"},
		{http.MethodGet, "/props"},
		{http.MethodPost, "/v1/embeddings"},
	} {
		req := httptest.NewRequest(route.method, route.path, nil)
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)
		if rec.Code != http.StatusForbidden {
			t.Errorf("%s %s status = %d, want 403", route.method, route.path, rec.Code)
		}
	}
	if up.path != "" {
		t.Errorf("upstream was reached (%s %s) for a refused route — the allowlist must gate BEFORE forwarding", up.method, up.path)
	}
}

// TestRunInferproxyServeError: a serve/bind failure maps to exitBlocked.
func TestRunInferproxyServeError(t *testing.T) {
	cmd, _, errOut := inferproxyTestCmd()
	d := &inferproxyDeps{
		Target:       "http://villa-llama:8080",
		APIKey:       "secret-key",
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve:        func(context.Context, string, http.Handler) error { return errors.New("bind: address in use") },
	}
	if code := runInferproxy(cmd, nil, d); code != exitBlocked {
		t.Fatalf("runInferproxy on serve error = %d, want exitBlocked", code)
	}
	if !strings.Contains(errOut.String(), "bind: address in use") {
		t.Fatalf("stderr missing serve error:\n%s", errOut.String())
	}
}

// TestRunInferproxyRefusesEmptyAPIKey: the live serve path must fail closed on
// an empty bearer (LLAMA_API_KEY unset) rather than running a proxy that can
// only ever produce 401s from villa-llama.
func TestRunInferproxyRefusesEmptyAPIKey(t *testing.T) {
	cmd, _, errOut := inferproxyTestCmd()
	served := false
	d := &inferproxyDeps{
		Target:       "http://villa-llama:8080",
		APIKey:       "", // empty → must refuse before binding
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve:        func(context.Context, string, http.Handler) error { served = true; return nil },
	}
	if code := runInferproxy(cmd, nil, d); code != exitBlocked {
		t.Fatalf("runInferproxy with empty api key = %d, want exitBlocked", code)
	}
	if served {
		t.Fatal("runInferproxy bound the listener with an empty api key — must fail closed first")
	}
	if !strings.Contains(errOut.String(), "empty bearer") {
		t.Fatalf("stderr missing empty-bearer remediation:\n%s", errOut.String())
	}
}

// TestRunInferproxyRefusesInvalidTarget: a malformed/empty Target is refused
// closed rather than constructing a reverse proxy pointed at nothing.
func TestRunInferproxyRefusesInvalidTarget(t *testing.T) {
	cmd, _, errOut := inferproxyTestCmd()
	served := false
	d := &inferproxyDeps{
		Target:       "",
		APIKey:       "secret-key",
		MaxBodyBytes: inferproxyMaxBodyBytes,
		Serve:        func(context.Context, string, http.Handler) error { served = true; return nil },
	}
	if code := runInferproxy(cmd, nil, d); code != exitBlocked {
		t.Fatalf("runInferproxy with empty target = %d, want exitBlocked", code)
	}
	if served {
		t.Fatal("runInferproxy bound the listener with an invalid target")
	}
	if !strings.Contains(errOut.String(), "invalid upstream target") {
		t.Fatalf("stderr missing invalid-target remediation:\n%s", errOut.String())
	}
}

// TestInferproxyCommandIsHidden: `villa inferproxy-serve` is registered and
// Hidden (it is an internal container entrypoint, not a user verb).
func TestInferproxyCommandIsHidden(t *testing.T) {
	cmd := newInferproxy()
	if cmd.Use != "inferproxy-serve" {
		t.Fatalf("Use = %q, want inferproxy-serve", cmd.Use)
	}
	if !cmd.Hidden {
		t.Fatal("newInferproxy() must be Hidden — it is an internal container entrypoint, not a user verb")
	}
}

// TestInferproxyServeSetsReadDeadlines mirrors TestWebsafeServeSetsReadDeadlines:
// the PRODUCTION server carries the read/idle deadlines (slowloris guard) and
// deliberately NO WriteTimeout (a streamed completion can legitimately run long).
func TestInferproxyServeSetsReadDeadlines(t *testing.T) {
	srv := inferproxyHTTPServer("127.0.0.1:0", http.NotFoundHandler())
	if srv.ReadHeaderTimeout != inferproxyReadHeaderTimeout {
		t.Errorf("ReadHeaderTimeout = %v, want %v", srv.ReadHeaderTimeout, inferproxyReadHeaderTimeout)
	}
	if srv.IdleTimeout != inferproxyIdleTimeout {
		t.Errorf("IdleTimeout = %v, want %v", srv.IdleTimeout, inferproxyIdleTimeout)
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout = %v, want 0 (a streamed completion must not be cut off)", srv.WriteTimeout)
	}
}

// TestInferproxyCapsRequestBody guards the ticket's body-size cap: a request
// body larger than MaxBodyBytes is refused (413) rather than forwarded whole —
// exercised over a REAL listener (httptest.NewServer), because
// http.MaxBytesReader's automatic 413 response depends on the server's normal
// connection-handling machinery, not on a bare ResponseRecorder.
func TestInferproxyCapsRequestBody(t *testing.T) {
	up := newStubLlama(t)
	target, err := url.Parse(up.srv.URL)
	if err != nil {
		t.Fatalf("parse upstream URL: %v", err)
	}
	handler := buildInferproxyHandler(target, "secret-key", 8, nil) // tiny cap
	proxy := httptest.NewServer(handler)
	defer proxy.Close()

	resp, err := http.Post(proxy.URL+"/v1/chat/completions", "application/json", strings.NewReader(`{"far too long a body for the cap"}`))
	if err != nil {
		t.Fatalf("POST: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusRequestEntityTooLarge {
		t.Errorf("status = %d, want 413 (request entity too large)", resp.StatusCode)
	}
}
