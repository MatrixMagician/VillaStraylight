package inference

import "net/http"

// proxy.go is the ONE decision villa-inferproxy makes about a request before
// forwarding it (GHSA-gvp9, ADR-0011): is this method+path pair one of the two
// routes the sandbox may reach? Everything else is refused. It is pure so the
// allowlist is testable without a live proxy or a live llama-server, mirroring
// how internal/websafe keeps its own routing decisions pure and seamed.

// ProxyAllowed reports whether method+path is one of the two routes
// villa-inferproxy forwards to villa-llama: POST /v1/chat/completions and GET
// /v1/models. Every other method+path pair — /slots, /metrics, /props, every
// other /v1 route — is refused. This is a NARROWER allowlist than
// llama-server's whole unauthenticated HTTP surface, never a mirror of it.
func ProxyAllowed(method, path string) bool {
	switch {
	case method == http.MethodPost && path == "/v1/chat/completions":
		return true
	case method == http.MethodGet && path == "/v1/models":
		return true
	default:
		return false
	}
}
