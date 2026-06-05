package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// TestShellRendersChatLinkFromConfig asserts the served index (the html/template
// shell) renders the header chat link using the CONFIG'd ChatPort (DASH-05/D-12),
// not a hard-coded 3000, and carries rel="noopener noreferrer" against
// reverse-tabnabbing (T-05-05).
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
// served verbatim (pure go build, embed.FS — D-01) and that the JS carries the
// visibilitychange pause + /api/status poll (D-05).
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
