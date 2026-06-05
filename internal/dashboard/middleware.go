package dashboard

import (
	"net"
	"net/http"
	"strings"
)

// requireSameOrigin is the CSRF / same-origin guard applied to the /api subrouter
// (T-05-04 / Pitfall 7). GET and HEAD are read-only and pass through untouched. For
// any state-changing method (the single Plan-04 POST /api/models/switch), it requires:
//
//   - Content-Type: application/json (a simple form/multipart POST cannot set this
//     cross-origin without a CORS preflight the loopback API never grants), AND
//   - a same-origin signal: Sec-Fetch-Site: same-origin (sent by modern browsers) OR,
//     when that signal is absent OR reports "none" (a no-origin navigation, NOT a
//     same-origin guarantee — CR-01), an Origin header consistent with same-origin (a
//     missing Origin on a non-GET is itself suspicious and rejected).
//
// Anything inconsistent → 403. This guards the mutation by construction now, before
// the POST handler lands in Plan 04.
func requireSameOrigin(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if isReadOnlyMethod(r.Method) {
			next.ServeHTTP(w, r)
			return
		}

		// State-changing request: demand a JSON content type. A cross-origin
		// <form> POST (the classic CSRF vector) cannot set application/json.
		if !hasJSONContentType(r) {
			http.Error(w, "forbidden: application/json required", http.StatusForbidden)
			return
		}

		// Prefer the explicit Fetch-Metadata signal when the browser sends it. Only an
		// explicit "same-origin" is treated as a pass. "none" is NOT a same-origin
		// guarantee — it is the value a browser sends for a user-initiated, no-origin
		// navigation (address bar, bookmark, top-level form) and is trivially forgeable
		// by a non-browser local client — so it falls through to a MANDATORY Origin check
		// rather than auto-passing (CR-01).
		switch r.Header.Get("Sec-Fetch-Site") {
		case "same-origin":
			next.ServeHTTP(w, r)
			return
		case "cross-site", "same-site":
			http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
			return
		}

		// "none" or absent (older clients / non-fetch navigations): require a PRESENT,
		// matching Origin. A non-GET with no Origin at all is rejected — a legitimate
		// same-origin fetch sets it, and a forged Sec-Fetch-Site: none can no longer
		// sail through without one (CR-01).
		origin := r.Header.Get("Origin")
		if origin == "" {
			http.Error(w, "forbidden: missing Origin", http.StatusForbidden)
			return
		}
		if !originMatchesHost(origin, r.Host) {
			http.Error(w, "forbidden: cross-origin request", http.StatusForbidden)
			return
		}
		next.ServeHTTP(w, r)
	})
}

// isReadOnlyMethod reports whether the method is a safe, read-only verb that the
// same-origin guard lets through unconditionally.
func isReadOnlyMethod(method string) bool {
	switch method {
	case http.MethodGet, http.MethodHead, http.MethodOptions:
		return true
	default:
		return false
	}
}

// hasJSONContentType reports whether the request declares an application/json body
// (ignoring any ;charset suffix).
func hasJSONContentType(r *http.Request) bool {
	ct := r.Header.Get("Content-Type")
	if i := strings.IndexByte(ct, ';'); i >= 0 {
		ct = ct[:i]
	}
	return strings.EqualFold(strings.TrimSpace(ct), "application/json")
}

// originMatchesHost reports whether an Origin header's authority matches the request
// Host (same-origin for a loopback API). The scheme is stripped; only the authority is
// compared. The comparison is case-insensitive (host authorities are case-insensitive
// per RFC 3986) and treats loopback hostnames as equivalent — localhost, 127.0.0.1 and
// ::1 all denote the single loopback listener, so an Origin of http://localhost:8888
// matches a Host of 127.0.0.1:8888 and vice versa (WR-04). This matters because, after
// CR-01, this Origin check is the primary gate for the "none"/absent Sec-Fetch-Site
// path and must not reject legitimate same-origin loopback requests.
func originMatchesHost(origin, host string) bool {
	authority := origin
	if i := strings.Index(authority, "://"); i >= 0 {
		authority = authority[i+3:]
	}
	if strings.EqualFold(authority, host) {
		return true
	}
	oHost, oPort := splitHostPort(authority)
	hHost, hPort := splitHostPort(host)
	if oPort != hPort {
		return false
	}
	return isLoopbackHost(oHost) && isLoopbackHost(hHost)
}

// splitHostPort splits an authority into its host and port components without erroring
// on a missing port (net.SplitHostPort fails on a bare host). The host is lowercased so
// callers can compare case-insensitively.
func splitHostPort(authority string) (host, port string) {
	if h, p, err := net.SplitHostPort(authority); err == nil {
		return strings.ToLower(h), p
	}
	return strings.ToLower(authority), ""
}

// isLoopbackHost reports whether a hostname denotes the loopback interface — the names
// the dashboard's loopback listener can legitimately be reached by (PRIV-01).
func isLoopbackHost(host string) bool {
	switch host {
	case "localhost", "127.0.0.1", "::1":
		return true
	default:
		return false
	}
}
