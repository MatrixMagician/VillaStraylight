package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestShellRendersChatLinkFromConfig asserts the served index (the html/template
// shell) renders the header chat link using the CONFIG'd ChatPort,
// not a hard-coded 3000, and carries rel="noopener noreferrer" against
// reverse-tabnabbing.
func TestShellRendersChatLinkFromConfig(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 4242, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("index code = %d, want 200", rec.Code)
	}
	body := rec.Body.String()
	if !strings.Contains(body, "Open Chat") {
		t.Fatalf("shell missing 'Open Chat' link\n%s", body)
	}
	if !strings.Contains(body, `rel="noopener noreferrer"`) {
		t.Fatalf("chat link missing rel=noopener noreferrer (reverse-tabnabbing)\n%s", body)
	}
	if !strings.Contains(body, "http://127.0.0.1:4242") {
		t.Fatalf("chat link does not use config'd ChatPort 4242\n%s", body)
	}
	if strings.Contains(body, "http://127.0.0.1:3000") {
		t.Fatalf("chat link hard-codes 3000 instead of using config'd ChatPort")
	}
	if ct := rec.Header().Get("Content-Type"); !strings.HasPrefix(ct, "text/html") {
		t.Fatalf("index Content-Type = %q, want text/html", ct)
	}
}

// TestStaticAssetsServed asserts the embedded dashboard.css and dashboard.js are
// served verbatim (pure go build, embed.FS) and that the JS carries the
// visibilitychange pause + /api/status poll.
func TestStaticAssetsServed(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	for _, tc := range []struct {
		path string
		want string
	}{
		{"/dashboard.css", "--bg-dominant"},
		{"/dashboard.js", "visibilitychange"},
		{"/dashboard.js", "/api/status"},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		rec := httptest.NewRecorder()
		srv.Handler().ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Fatalf("GET %s = %d, want 200", tc.path, rec.Code)
		}
		if !strings.Contains(rec.Body.String(), tc.want) {
			t.Fatalf("GET %s missing %q", tc.path, tc.want)
		}
	}
}

// TestShellCarriesTheIDsThePollLoopWrites asserts the served shell ships every mount
// point dashboard.js writes into. The two files are coupled by bare string ids with no
// compiler between them: drop an id from the shell and the panel silently stops
// updating — the page still renders, still polls, and still shows its honest
// placeholder, so nothing fails. This test is that missing compiler.
func TestShellCarriesTheIDsThePollLoopWrites(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	rec := httptest.NewRecorder()
	srv.Handler().ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("index code = %d, want 200", rec.Code)
	}
	shell := rec.Body.String()

	for _, id := range []string{
		// Global.
		"connection-banner", "overall-verdict",
		// Status strip (DASH-07) — the read-model over the existing polls.
		"strip-verdict-dot", "strip-verdict-sub",
		"strip-model", "strip-model-sub",
		"strip-mem", "strip-mem-bar", "strip-mem-fill",
		"strip-gen", "strip-gen-sub",
		// Panels. health-rows takes the service rows, health-backend the backend
		// identity rows — two columns, two owners (renderHealth / renderBackend).
		"health-rows", "health-backend",
		"performance-body", "gpu-body", "models-body", "models-count",
		// Hidden-until-data subsystem panels + their bodies.
		"memory-panel", "memory-body",
		"agent-panel", "agent-body",
		"web-search-panel", "web-search-body",
		// The guarded switch dialog (the single sanctioned write).
		"switch-dialog", "switch-dialog-title", "switch-dialog-fit",
		"switch-cancel", "switch-confirm",
	} {
		if !strings.Contains(shell, `id="`+id+`"`) {
			t.Errorf("shell is missing id=%q — dashboard.js writes into it and would silently no-op", id)
		}
	}

	// The three optional panels must ship hidden: an install with the subsystem off
	// renders no trace of it (CTRL-02 / D-03 / D-05, hidden-until-data).
	for _, panel := range []string{"memory-panel", "agent-panel", "web-search-panel"} {
		idx := strings.Index(shell, `id="`+panel+`"`)
		if idx < 0 {
			continue // already reported above
		}
		tag := shell[idx:]
		if end := strings.Index(tag, ">"); end >= 0 {
			tag = tag[:end]
		}
		if !strings.Contains(tag, "hidden") {
			t.Errorf("%s does not ship hidden — a subsystem-off install would render an empty panel", panel)
		}
	}
}
