package main

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// status_live_props_test.go is the GHSA-qxg9 (ADR-0011) half of liveProps: a
// keyed llama-server 401s an unauthenticated /props, which used to degrade the
// config-identity drift check to a permanent Unknown. This proves the request
// now carries the same Bearer credential every other authed probe
// (metrics.ScrapeMetricsAuth et al.) sends.

// TestLivePropsSendsAuthorizationHeader: a non-empty apiKey must be sent as
// `Authorization: Bearer <key>`.
func TestLivePropsSendsAuthorizationHeader(t *testing.T) {
	var gotAuth string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		w.Write([]byte(`{"model_path":"/models/x.gguf","n_ctx":4096}`))
	}))
	defer srv.Close()

	got := liveProps(srv.URL, "hunter2hunter2")
	if got == nil {
		t.Fatal("liveProps returned nil, want a parsed PropsInfo")
	}
	if want := "Bearer hunter2hunter2"; gotAuth != want {
		t.Errorf("Authorization header = %q, want %q", gotAuth, want)
	}
}

// TestLivePropsEmptyKeySendsNoHeader: "" (an unmigrated host, or a backend that
// has never required a key) must send no Authorization header at all, matching
// the pre-existing behavior against an unkeyed server.
func TestLivePropsEmptyKeySendsNoHeader(t *testing.T) {
	var gotAuth string
	sawHeader := false
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAuth = r.Header.Get("Authorization")
		sawHeader = gotAuth != ""
		w.Write([]byte(`{"model_path":"/models/x.gguf","n_ctx":4096}`))
	}))
	defer srv.Close()

	if got := liveProps(srv.URL, ""); got == nil {
		t.Fatal("liveProps returned nil, want a parsed PropsInfo")
	}
	if sawHeader {
		t.Errorf("empty apiKey must send no Authorization header, got %q", gotAuth)
	}
}
