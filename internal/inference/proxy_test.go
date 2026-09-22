package inference

import "testing"

// TestProxyAllowed guards the ONE decision villa-inferproxy makes (GHSA-gvp9,
// ADR-0011): only POST /v1/chat/completions and GET /v1/models are forwarded.
// Every other method+path pair — including /slots, /metrics, /props, and every
// other /v1 route — must be refused, not merely "not explicitly allowed": a
// permissive default would silently widen the allowlist the next time a route
// is added elsewhere in this package.
func TestProxyAllowed(t *testing.T) {
	cases := []struct {
		method, path string
		want         bool
	}{
		{"POST", "/v1/chat/completions", true},
		{"GET", "/v1/models", true},
		{"GET", "/v1/chat/completions", false},
		{"POST", "/v1/models", false},
		{"GET", "/slots", false},
		{"GET", "/metrics", false},
		{"GET", "/props", false},
		{"GET", "/health", false},
		{"POST", "/v1/embeddings", false},
		{"DELETE", "/v1/chat/completions", false},
		{"POST", "/v1/chat/completions/", false},
		{"", "", false},
	}
	for _, tc := range cases {
		if got := ProxyAllowed(tc.method, tc.path); got != tc.want {
			t.Errorf("ProxyAllowed(%q, %q) = %v, want %v", tc.method, tc.path, got, tc.want)
		}
	}
}
