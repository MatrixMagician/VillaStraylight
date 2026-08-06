package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/spf13/cobra"

	"github.com/MatrixMagician/VillaStraylight/internal/config"
	"github.com/MatrixMagician/VillaStraylight/internal/inference"
	"github.com/MatrixMagician/VillaStraylight/internal/orchestrate"
	"github.com/MatrixMagician/VillaStraylight/internal/websafe"
)

// websafe_test.go drives the hidden `villa websafe-serve` cmd off-network: runWebsafe is
// fed a fake Serve (no real listener) and a stub HTTP client (httptest-backed, so the SSRF
// guard's live network is never exercised) via the Deps seam. It asserts the served handler
// round-trips the verified OWUI contract ({urls} POST -> [{page_content}] 200), the Bearer is
// enforced, the command is Hidden, and the lifecycle picks up villa-websafe.service.

// websafeTestCmd builds a cobra command with the host/port flags + captured out/err,
// mirroring dashboardTestCmd. The flags must exist because runWebsafe reads them.
func websafeTestCmd() (*cobra.Command, *bytes.Buffer, *bytes.Buffer) {
	cmd := &cobra.Command{Use: "test"}
	cmd.Flags().String("host", "0.0.0.0", "")
	cmd.Flags().Int("port", 8090, "")
	var out, errOut bytes.Buffer
	cmd.SetOut(&out)
	cmd.SetErr(&errOut)
	return cmd, &out, &errOut
}

// stubUpstream is an httptest server standing in for a fetched web page, so the stub client
// in these tests never touches the real network or the SSRF guard's connect-time hook.
func stubUpstream(t *testing.T) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		_, _ = w.Write([]byte("<html><head><title>Hello</title></head><body>grounded body</body></html>"))
	}))
}

// TestRunWebsafeCleanStartAndContractRoundTrip: runWebsafe returns exitPass, prints the
// container-internal listen addr, and the served handler maps a {urls} POST to a
// [{page_content, metadata{source,title}}] 200 array (the verified OWUI external-loader
// contract). The Serve dep captures the handler instead of binding a socket; the fetch uses
// a stub client pointed at an httptest upstream (no real network).
func TestRunWebsafeCleanStartAndContractRoundTrip(t *testing.T) {
	up := stubUpstream(t)
	defer up.Close()

	cmd, out, _ := websafeTestCmd()

	var handler http.Handler
	d := &websafeDeps{
		Client: up.Client(),
		Bounds: websafe.DefaultBounds(),
		Secret: "topsecret",
		Serve: func(_ string, h http.Handler) error {
			handler = h
			return nil
		},
	}

	code := runWebsafe(cmd, nil, d)
	if code != exitPass {
		t.Fatalf("runWebsafe = %d, want %d (exitPass)", code, exitPass)
	}
	if handler == nil {
		t.Fatalf("Serve was not called with the handler")
	}
	if !strings.Contains(out.String(), "listening on http://0.0.0.0:8090") {
		t.Fatalf("output missing container-internal listen addr:\n%s", out.String())
	}

	// Drive the verified contract through the captured handler.
	body, _ := json.Marshal(websafe.LoadRequest{URLs: []string{up.URL}})
	req := httptest.NewRequest(http.MethodPost, "/load", bytes.NewReader(body))
	req.Header.Set("Authorization", "Bearer topsecret")
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("handler status = %d, want 200; body=%s", rec.Code, rec.Body.String())
	}
	var pages []websafe.LoadResponse
	if err := json.Unmarshal(rec.Body.Bytes(), &pages); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	if len(pages) != 1 {
		t.Fatalf("got %d pages, want 1: %s", len(pages), rec.Body.String())
	}
	if !strings.Contains(pages[0].PageContent, "grounded body") {
		t.Errorf("page_content missing fetched body: %q", pages[0].PageContent)
	}
	if pages[0].Metadata["source"] != up.URL {
		t.Errorf("metadata.source = %v, want %q (GROUND-01 citation URL)", pages[0].Metadata["source"], up.URL)
	}
}

// TestRunWebsafeBearerEnforced: a request WITHOUT the configured Bearer is rejected with 401
// before any fetch (GUARD-01 spoofing mitigation, T-31-15).
func TestRunWebsafeBearerEnforced(t *testing.T) {
	cmd, _, _ := websafeTestCmd()

	var handler http.Handler
	d := &websafeDeps{
		Client: http.DefaultClient,
		Bounds: websafe.DefaultBounds(),
		Secret: "topsecret",
		Serve:  func(_ string, h http.Handler) error { handler = h; return nil },
	}
	if code := runWebsafe(cmd, nil, d); code != exitPass {
		t.Fatalf("runWebsafe = %d, want exitPass", code)
	}

	body, _ := json.Marshal(websafe.LoadRequest{URLs: []string{"https://example.com"}})
	req := httptest.NewRequest(http.MethodPost, "/load", bytes.NewReader(body)) // no Authorization header
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)
	if rec.Code != http.StatusUnauthorized {
		t.Fatalf("unauthenticated request status = %d, want 401", rec.Code)
	}
}

// TestRunWebsafeServeError: a serve/bind failure maps to exitBlocked.
func TestRunWebsafeServeError(t *testing.T) {
	cmd, _, errOut := websafeTestCmd()
	d := &websafeDeps{
		Client: http.DefaultClient,
		Bounds: websafe.DefaultBounds(),
		Secret: "topsecret", // non-empty so we reach the serve path, not the empty-bearer refusal
		Serve:  func(string, http.Handler) error { return errors.New("bind: address in use") },
	}
	if code := runWebsafe(cmd, nil, d); code != exitBlocked {
		t.Fatalf("runWebsafe on serve error = %d, want exitBlocked", code)
	}
	if !strings.Contains(errOut.String(), "bind: address in use") {
		t.Fatalf("stderr missing serve error:\n%s", errOut.String())
	}
}

// TestRunWebsafeRefusesEmptyBearer: the live serve path must fail closed on an empty bearer
// (EXTERNAL_WEB_LOADER_API_KEY unset) rather than serving an unauthenticated /load — the
// pure loader's empty-secret accept-any behavior must never reach production.
func TestRunWebsafeRefusesEmptyBearer(t *testing.T) {
	cmd, _, errOut := websafeTestCmd()
	served := false
	d := &websafeDeps{
		Client: http.DefaultClient,
		Bounds: websafe.DefaultBounds(),
		Secret: "", // empty → must refuse before binding
		Serve:  func(string, http.Handler) error { served = true; return nil },
	}
	if code := runWebsafe(cmd, nil, d); code != exitBlocked {
		t.Fatalf("runWebsafe with empty bearer = %d, want exitBlocked", code)
	}
	if served {
		t.Fatal("runWebsafe bound the listener with an empty bearer — must fail closed first")
	}
	if !strings.Contains(errOut.String(), "empty bearer") {
		t.Fatalf("stderr missing empty-bearer remediation:\n%s", errOut.String())
	}
}

// TestWebsafeCommandIsHidden: `villa websafe-serve` is registered and Hidden (it is an
// internal container entrypoint, not a user verb).
func TestWebsafeCommandIsHidden(t *testing.T) {
	cmd := newWebsafe()
	if cmd.Use != "websafe-serve" {
		t.Fatalf("Use = %q, want websafe-serve", cmd.Use)
	}
	if !cmd.Hidden {
		t.Errorf("websafe-serve must be Hidden (internal container entrypoint)")
	}
	// It must be registered in the root tree.
	root := newRoot()
	var found bool
	for _, c := range root.Commands() {
		if c.Name() == "websafe-serve" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("websafe-serve not registered in the cobra tree")
	}
}

// TestWebsafeServiceInLifecycleSet (lifecycle verify): once render appends the
// villa-websafe.container unit (web search on), serviceUnits derives villa-websafe.service
// automatically — so up/down/status manage it with NO lifecycle.go edit. This guards that
// the existing .container->.service derivation covers the new unit.
func TestWebsafeServiceInLifecycleSet(t *testing.T) {
	in := orchestrate.RenderInput{
		Backend: inference.VulkanBackend(),
		Cfg: config.VillaConfig{
			Model: "qwen3-35b-a3b-moe-64", Quant: "UD-Q4_K_M", Ctx: 131072, Backend: "vulkan",
			WebSearchEnabled:     true,
			WebSearchResultCount: 3,
		},
		ModelFile:     "qwen3-35b-a3b-moe-64.gguf",
		ModelsDir:     "/home/villa/.local/share/villa/models",
		HostVillaPath: "/home/villa/.local/bin/villa",
	}
	units, err := orchestrate.Render(in)
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	svcs := serviceUnits(units)
	var found bool
	for _, s := range svcs {
		if s == "villa-websafe.service" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("villa-websafe.service not derived by serviceUnits (lifecycle would not manage it): %v", svcs)
	}

	// And managedServices (the full up/down set) must include it too.
	managed := managedServices(units)
	found = false
	for _, s := range managed {
		if s == "villa-websafe.service" {
			found = true
			break
		}
	}
	if !found {
		t.Errorf("villa-websafe.service not in managedServices: %v", managed)
	}
}
