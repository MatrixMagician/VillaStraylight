package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// guardedPort is the configured DashboardPort every test in this file assumes;
// every legitimate request uses a Host carrying exactly this port.
const guardedPort = 8888

// guardedRouter wraps an /api mux in requireSameOrigin with a POST echo, so the
// guard can be exercised in isolation of the real handlers (Pitfall 7).
// It mirrors how the server mounts the guard: once, around the whole API mux.
func guardedRouter() http.Handler {
	api := http.NewServeMux()
	api.HandleFunc("GET /api/status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	api.HandleFunc("POST /api/models/switch", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })

	root := http.NewServeMux()
	root.Handle("/api/", requireSameOrigin(api, guardedPort))
	return root
}

// TestSameOriginGuardRejectsCrossOrigin asserts a non-GET cross-origin request to /api
// is rejected with 403 (CSRF guard). Covers both the Sec-Fetch-Site signal
// and the Origin-mismatch fallback, plus the missing-Origin and wrong-Content-Type
// rejections.
func TestSameOriginGuardRejectsCrossOrigin(t *testing.T) {
	h := guardedRouter()

	cases := []struct {
		name        string
		contentType string
		origin      string
		secFetch    string
		host        string
		wantStatus  int
	}{
		{"cross-site via sec-fetch", "application/json", "https://evil.example", "cross-site", "127.0.0.1:8888", http.StatusForbidden},
		{"same-site via sec-fetch", "application/json", "http://127.0.0.1:8888", "same-site", "127.0.0.1:8888", http.StatusForbidden},
		{"cross-origin via origin mismatch", "application/json", "http://evil.example", "", "127.0.0.1:8888", http.StatusForbidden},
		{"missing origin and sec-fetch", "application/json", "", "", "127.0.0.1:8888", http.StatusForbidden},
		// Sec-Fetch-Site: none is NOT a same-origin pass; with no Origin it is rejected.
		{"sec-fetch none and no origin", "application/json", "", "none", "127.0.0.1:8888", http.StatusForbidden},
		// none with a cross-origin Origin is rejected via the mandatory Origin check.
		{"sec-fetch none and cross origin", "application/json", "http://evil.example", "none", "127.0.0.1:8888", http.StatusForbidden},
		{"form content-type", "application/x-www-form-urlencoded", "http://127.0.0.1:8888", "same-origin", "127.0.0.1:8888", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/models/switch", strings.NewReader("{}"))
			req.Host = tc.host
			if tc.contentType != "" {
				req.Header.Set("Content-Type", tc.contentType)
			}
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: code = %d, want %d", tc.name, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestSameOriginGuardAllowsSameOriginJSON asserts a same-origin application/json
// non-GET passes the guard (so Plan 04's legitimate POST is not blocked).
func TestSameOriginGuardAllowsSameOriginJSON(t *testing.T) {
	h := guardedRouter()

	// Via Sec-Fetch-Site: same-origin.
	req := httptest.NewRequest(http.MethodPost, "/api/models/switch", strings.NewReader("{}"))
	req.Host = "127.0.0.1:8888"
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Sec-Fetch-Site", "same-origin")
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("same-origin sec-fetch POST = %d, want 200", rec.Code)
	}

	// Via matching Origin (older client, no Sec-Fetch-Site).
	req2 := httptest.NewRequest(http.MethodPost, "/api/models/switch", strings.NewReader("{}"))
	req2.Host = "127.0.0.1:8888"
	req2.Header.Set("Content-Type", "application/json")
	req2.Header.Set("Origin", "http://127.0.0.1:8888")
	rec2 := httptest.NewRecorder()
	h.ServeHTTP(rec2, req2)
	if rec2.Code != http.StatusOK {
		t.Fatalf("same-origin Origin POST = %d, want 200", rec2.Code)
	}
}

// TestSameOriginGuardNoneFallsBackToOrigin asserts that Sec-Fetch-Site: none does NOT
// auto-pass but falls through to the Origin check: a present, matching Origin
// is accepted; loopback hostname/IP variants are treated as equivalent.
func TestSameOriginGuardNoneFallsBackToOrigin(t *testing.T) {
	h := guardedRouter()

	cases := []struct {
		name       string
		origin     string
		secFetch   string
		host       string
		wantStatus int
	}{
		// none + matching Origin → accepted (the Origin is now the gate).
		{"none with matching origin", "http://127.0.0.1:8888", "none", "127.0.0.1:8888", http.StatusOK},
		// none absent entirely + matching Origin → accepted.
		{"absent sec-fetch with matching origin", "http://127.0.0.1:8888", "", "127.0.0.1:8888", http.StatusOK},
		// localhost Origin vs 127.0.0.1 Host (loopback equivalence) → accepted.
		{"localhost origin vs 127 host", "http://localhost:8888", "none", "127.0.0.1:8888", http.StatusOK},
		// 127.0.0.1 Origin vs localhost Host → accepted.
		{"127 origin vs localhost host", "http://127.0.0.1:8888", "none", "localhost:8888", http.StatusOK},
		// case-insensitive host authority → accepted.
		{"uppercase localhost origin", "http://LOCALHOST:8888", "none", "localhost:8888", http.StatusOK},
		// Loopback equivalence must NOT cross ports.
		{"loopback equivalence different port rejected", "http://localhost:9999", "none", "127.0.0.1:8888", http.StatusForbidden},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/api/models/switch", strings.NewReader("{}"))
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/json")
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.secFetch != "" {
				req.Header.Set("Sec-Fetch-Site", tc.secFetch)
			}
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantStatus {
				t.Fatalf("%s: code = %d, want %d", tc.name, rec.Code, tc.wantStatus)
			}
		})
	}
}

// TestSameOriginGuardPassesGet asserts read-only GETs on a legitimate Host are never
// blocked by the guard's Origin/Sec-Fetch-Site checks (those apply only to
// state-changing methods) — the Host allowlist itself is covered separately below.
func TestSameOriginGuardPassesGet(t *testing.T) {
	h := guardedRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Host = "127.0.0.1:8888"
	req.Header.Set("Origin", "http://evil.example") // even a cross-origin GET is fine
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status = %d, want 200", rec.Code)
	}
}

// TestHostAllowed pins hostAllowed's allowlist directly: only localhost, 127.0.0.1
// and [::1], each combined with EXACTLY the configured port, pass.
func TestHostAllowed(t *testing.T) {
	cases := []struct {
		host string
		port int
		want bool
	}{
		{"127.0.0.1:8888", 8888, true},
		{"localhost:8888", 8888, true},
		{"[::1]:8888", 8888, true},
		{"LOCALHOST:8888", 8888, true}, // case-insensitive
		{"attacker.example:8888", 8888, false},
		{"127.0.0.1:9999", 8888, false}, // right host, wrong port
		{"127.0.0.1", 8888, false},      // no port at all
		{"", 8888, false},
	}
	for _, tc := range cases {
		if got := hostAllowed(tc.host, tc.port); got != tc.want {
			t.Errorf("hostAllowed(%q, %d) = %v, want %v", tc.host, tc.port, got, tc.want)
		}
	}
}

// TestSameOriginGuardRejectsSpoofedHost is the GHSA-3r95 regression: a request whose
// Host is not the loopback listener at the configured port must be rejected BEFORE
// the method switch, for GET as much as for POST. This is the DNS-rebinding path —
// a page served from a hostname that resolves to 127.0.0.1 sends
// Sec-Fetch-Site: same-origin (it IS same-origin with itself) with a Host the
// dashboard never configured, so the same-origin signal alone cannot catch it; only
// checking Host against the allowlist does. Confirmed live at 81eacec: a spoofed
// Host previously reached the handler (503 for the POST, 200 for the GET) rather
// than being rejected by the guard.
func TestSameOriginGuardRejectsSpoofedHost(t *testing.T) {
	h := guardedRouter()

	cases := []struct {
		name   string
		method string
		path   string
		host   string
	}{
		{"spoofed host on GET", http.MethodGet, "/api/status", "attacker.example:8888"},
		{"spoofed host on POST", http.MethodPost, "/api/models/switch", "attacker.example:8888"},
		{"right host wrong port", http.MethodGet, "/api/status", "127.0.0.1:9999"},
		{"loopback host no port", http.MethodGet, "/api/status", "127.0.0.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var body *strings.Reader
			if tc.method == http.MethodPost {
				body = strings.NewReader("{}")
			} else {
				body = strings.NewReader("")
			}
			req := httptest.NewRequest(tc.method, tc.path, body)
			req.Host = tc.host
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusForbidden {
				t.Fatalf("%s %s with Host %q = %d, want 403", tc.method, tc.path, tc.host, rec.Code)
			}
		})
	}
}
