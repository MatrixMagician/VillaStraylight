package dashboard

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestRouteTableResolves pins the whole route table: which method-and-path pairs
// exist, and what a mismatch returns. It is the regression net for the router
// swap — the routes are the contract the embedded UI drives, and a silently
// dropped route would otherwise only show up in the browser.
//
// The six API routes are five GETs and one POST. The POST is exercised through
// the guard (no JSON content type ⇒ 403), which is itself the proof that the
// same-origin guard is still mounted on it.
func TestRouteTableResolves(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})
	h := srv.Handler()

	cases := []struct {
		method   string
		path     string
		wantCode int
		why      string
	}{
		{http.MethodGet, "/api/status", http.StatusOK, "status read-model"},
		{http.MethodGet, "/api/healthz", http.StatusOK, "liveness"},
		{http.MethodGet, "/api/metrics", http.StatusOK, "perf read-model"},
		{http.MethodGet, "/api/gpu", http.StatusOK, "gpu read-model"},
		{http.MethodGet, "/api/models", http.StatusOK, "models read-model"},

		// The one sanctioned mutation, reached through requireSameOrigin: a POST
		// with no JSON content type is refused by the guard, never by the router.
		{http.MethodPost, "/api/models/switch", http.StatusForbidden, "guarded mutation"},

		// A path that no route claims.
		{http.MethodGet, "/api/nope", http.StatusNotFound, "unknown api path"},

		// A non-GET reaches the guard BEFORE routing, so a wrong method on a real
		// path is refused as unguarded rather than as unrouted. That ordering is
		// deliberate: the trust boundary should not leak which paths exist.
		{http.MethodPost, "/api/status", http.StatusForbidden, "wrong method, refused by the guard first"},
		{http.MethodDelete, "/api/models/switch", http.StatusForbidden, "wrong method, refused by the guard first"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != tc.wantCode {
				t.Errorf("%s %s (%s) = %d, want %d; body=%s",
					tc.method, tc.path, tc.why, rec.Code, tc.wantCode, rec.Body.String())
			}
		})
	}
}

// TestNonAPIPathsReachTheUI asserts the catch-all still serves the embedded
// single-page UI, and that the same-origin guard does NOT gate it — the guard
// belongs to the API surface and nothing else.
func TestNonAPIPathsReachTheUI(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})
	h := srv.Handler()

	for _, path := range []string{"/", "/index.html", "/dashboard.css", "/dashboard.js"} {
		req := httptest.NewRequest(http.MethodGet, path, nil)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusOK {
			t.Errorf("GET %s = %d, want 200", path, rec.Code)
		}
	}
}

// TestWrongMethodOnRealPath covers the 405 path proper: a request that SATISFIES
// the same-origin guard and then meets a path that exists but does not serve that
// method. Routing, not the guard, is what answers here.
func TestWrongMethodOnRealPath(t *testing.T) {
	srv := mustNewServer(t, Config{
		StatusDeps:    stubStatusDeps(t),
		ChatPort:      3000,
		DashboardAddr: "127.0.0.1",
		DashboardPort: 8888,
	})
	h := srv.Handler()

	cases := []struct{ method, path string }{
		{http.MethodDelete, "/api/status"},
		{http.MethodPut, "/api/models/switch"},
		{http.MethodPost, "/api/healthz"},
	}

	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, tc.path, nil)
			// Satisfy requireSameOrigin so the request reaches the route table.
			req.Header.Set("Content-Type", "application/json")
			req.Header.Set("Sec-Fetch-Site", "same-origin")
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusMethodNotAllowed {
				t.Errorf("%s %s = %d, want 405", tc.method, tc.path, rec.Code)
			}
		})
	}
}

// TestPanicIsRecovered proves the one middleware worth keeping still works: a
// panicking handler becomes a 500 rather than killing the long-lived dashboard
// service. The real-IP and request-ID middlewares were dropped deliberately.
func TestPanicIsRecovered(t *testing.T) {
	boom := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic("boom")
	})
	rec := httptest.NewRecorder()
	recoverPanic(boom).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("panicking handler = %d, want 500", rec.Code)
	}
}

// TestAbortHandlerPanicIsNotSwallowed guards the one panic that must NOT become a
// 500: http.ErrAbortHandler is the sentinel a handler raises to abandon a response
// deliberately, and net/http suppresses its own log for it. Swallowing it would
// turn an intentional abort into a spurious 500 and a stack trace.
func TestAbortHandlerPanicIsNotSwallowed(t *testing.T) {
	abort := http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		panic(http.ErrAbortHandler)
	})

	defer func() {
		switch rec := recover(); rec {
		case nil:
			t.Error("ErrAbortHandler was swallowed; it must propagate to net/http")
		case http.ErrAbortHandler:
			// Propagated, as it must be.
		default:
			t.Errorf("propagated %v, want http.ErrAbortHandler", rec)
		}
	}()

	rec := httptest.NewRecorder()
	recoverPanic(abort).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
}
