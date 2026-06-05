package dashboard

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
)

// guardedRouter mounts requireSameOrigin on an /api subrouter with a POST echo, so the
// guard can be exercised in isolation (Plan 04's real POST handler lands later, but the
// guard belongs to the server now — Pitfall 7 / T-05-04).
func guardedRouter() http.Handler {
	r := chi.NewRouter()
	r.Route("/api", func(r chi.Router) {
		r.Use(requireSameOrigin)
		r.Get("/status", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
		r.Post("/models/switch", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusOK) })
	})
	return r
}

// TestSameOriginGuardRejectsCrossOrigin asserts a non-GET cross-origin request to /api
// is rejected with 403 (CSRF guard, T-05-04). Covers both the Sec-Fetch-Site signal
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
		// CR-01: Sec-Fetch-Site: none is NOT a same-origin pass; with no Origin it is rejected.
		{"sec-fetch none and no origin", "application/json", "", "none", "127.0.0.1:8888", http.StatusForbidden},
		// CR-01: none with a cross-origin Origin is rejected via the mandatory Origin check.
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
// auto-pass (CR-01) but falls through to the Origin check: a present, matching Origin
// is accepted; loopback hostname/IP variants are treated as equivalent (WR-04).
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
		// WR-04: localhost Origin vs 127.0.0.1 Host (loopback equivalence) → accepted.
		{"localhost origin vs 127 host", "http://localhost:8888", "none", "127.0.0.1:8888", http.StatusOK},
		// WR-04: 127.0.0.1 Origin vs localhost Host → accepted.
		{"127 origin vs localhost host", "http://127.0.0.1:8888", "none", "localhost:8888", http.StatusOK},
		// WR-04: case-insensitive host authority → accepted.
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

// TestSameOriginGuardPassesGet asserts read-only GETs are never blocked by the guard.
func TestSameOriginGuardPassesGet(t *testing.T) {
	h := guardedRouter()
	req := httptest.NewRequest(http.MethodGet, "/api/status", nil)
	req.Header.Set("Origin", "http://evil.example") // even a cross-origin GET is fine
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /api/status = %d, want 200", rec.Code)
	}
}
