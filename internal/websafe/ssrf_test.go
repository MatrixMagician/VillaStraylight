// ssrf_test.go guards the SSRF reject-set, the connect-time Control hook, the
// per-hop CheckRedirect re-validation, and the hostname reject-set.
//
// These tests are the load-bearing security proof of Phase 31: an SSRF-internal
// target (loopback / RFC1918 / link-local / metadata / CGNAT / ULA / villa-* host)
// MUST be rejected at connect time, never fetched, and a 2xx->302->internal redirect
// chain MUST be rejected by re-validating every hop. Validation runs on the CONNECTED
// IP (net.Dialer.Control), not the hostname, so DNS-rebinding TOCTOU is defeated.
package websafe

import (
	"net/http"
	"net/http/httptest"
	"net/netip"
	"strings"
	"testing"
)

// TestSSRFRejectSet asserts the full CONTEXT Area 3 reject-set blocks and that a
// public IP passes — ipRejected is the prefix/predicate core of.
func TestSSRFRejectSet(t *testing.T) {
	rejected := []string{
		"127.0.0.1",        // loopback v4
		"::1",              // loopback v6
		"10.0.0.5",         // RFC1918
		"172.16.0.1",       // RFC1918
		"192.168.1.1",      // RFC1918
		"169.254.169.254",  // cloud metadata (link-local v4)
		"fe80::1",          // link-local v6
		"100.64.0.1",       // CGNAT
		"fc00::1",          // ULA v6
		"0.0.0.0",          // "this network" / unspecified
		"::ffff:127.0.0.1", // v4-mapped-v6 internal addr
		"::ffff:10.0.0.5",  // v4-mapped-v6 RFC1918
		"::7f00:1",         // deprecated IPv4-compatible v6 (::127.0.0.1)
		"64:ff9b::7f00:1",  // NAT64 embedding 127.0.0.1
		"2002:7f00:1::1",   // 6to4 embedding an internal v4
		"224.0.0.1",        // multicast v4 (all-hosts)
		"ff02::1",          // multicast v6 (link-local all-nodes)
	}
	for _, s := range rejected {
		ip, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		if !ipRejected(ip) {
			t.Errorf("ipRejected(%s) = false, want true (internal address must be rejected)", s)
		}
	}

	allowed := []string{
		"93.184.216.34",                      // example.com (public)
		"8.8.8.8",                            // public DNS
		"2606:2800:220:1:248:1893:25c8:1946", // public v6
	}
	for _, s := range allowed {
		ip, err := netip.ParseAddr(s)
		if err != nil {
			t.Fatalf("parse %q: %v", s, err)
		}
		if ipRejected(ip) {
			t.Errorf("ipRejected(%s) = true, want false (public address must pass)", s)
		}
	}
}

// TestHostRejected asserts the defense-in-depth hostname reject-set blocks internal
// service names regardless of resolution (villa-*, *.network, *.localhost, localhost).
func TestHostRejected(t *testing.T) {
	rejected := []string{
		"localhost",
		"villa-websafe",
		"villa-qdrant",
		"anything.network",
		"x.localhost",
		"VILLA-OPENWEBUI", // case-insensitive
	}
	for _, h := range rejected {
		if !hostRejected(h) {
			t.Errorf("hostRejected(%q) = false, want true", h)
		}
	}
	allowed := []string{
		"example.com",
		"en.wikipedia.org",
		"networkworld.com", // contains "network" but does not END with .network
	}
	for _, h := range allowed {
		if hostRejected(h) {
			t.Errorf("hostRejected(%q) = true, want false", h)
		}
	}
}

// TestControlConnectTime asserts the net.Dialer.Control hook rejects an internal
// connected IP and permits a public one — this is the TOCTOU-safe placement (it runs
// AFTER DNS resolution, BEFORE connect, on the actual socket IP).
func TestControlConnectTime(t *testing.T) {
	if err := control("tcp", "127.0.0.1:80", nil); err == nil {
		t.Error("control(127.0.0.1:80) = nil, want SSRF error")
	} else if !strings.Contains(strings.ToLower(err.Error()), "ssrf") {
		t.Errorf("control(127.0.0.1:80) error %q does not mention SSRF", err)
	}
	if err := control("tcp", "[::1]:80", nil); err == nil {
		t.Error("control([::1]:80) = nil, want SSRF error")
	}
	if err := control("tcp", "93.184.216.34:443", nil); err != nil {
		t.Errorf("control(93.184.216.34:443) = %v, want nil (public IP)", err)
	}
}

// TestSSRFInternalHostCase is the EXPLICIT Phase-33 family-(c) internal-host contract
// the `villa verify search` SSRF assertion proves the shipped guard
// refuses the cloud-metadata IP, loopback, and the managed-service container-DNS names.
// It names the verify-search family directly (the broader reject-set is covered by
// TestSSRFRejectSet/TestHostRejected above; this is the focused security-property pin).
func TestSSRFInternalHostCase(t *testing.T) {
	if !ipRejected(netip.MustParseAddr("169.254.169.254")) {
		t.Error("ipRejected(169.254.169.254) = false, want true (cloud-metadata IP must be refused)")
	}
	if !ipRejected(netip.MustParseAddr("127.0.0.1")) {
		t.Error("ipRejected(127.0.0.1) = false, want true (loopback must be refused)")
	}
	if !hostRejected("villa-searxng") {
		t.Error("hostRejected(villa-searxng) = false, want true (managed-service DNS must be refused)")
	}
	if !hostRejected("localhost") {
		t.Error("hostRejected(localhost) = false, want true")
	}
	// The connect-time Control hook returns a non-nil SSRF error for an internal address.
	if err := control("tcp", "169.254.169.254:80", nil); err == nil {
		t.Error("control(169.254.169.254:80) = nil, want SSRF error (connect-time refusal)")
	}
}

// TestRedirectRevalidation asserts the CheckRedirect re-check rejects an internal
// redirect target, a non-http(s) redirect scheme, and a >MaxRedirects chain.
func TestRedirectRevalidation(t *testing.T) {
	b := DefaultBounds()
	client := SafeClient(b)

	t.Run("internal-redirect-host-rejected", func(t *testing.T) {
		// A server that 302-redirects to a loopback host name: CheckRedirect's
		// hostRejected check fires before any dial, so the request fails.
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "http://localhost/internal", http.StatusFound)
		}))
		defer srv.Close()
		if _, err := client.Get(srv.URL); err == nil {
			t.Error("redirect to internal host succeeded, want SSRF rejection")
		}
	})

	t.Run("non-http-scheme-redirect-rejected", func(t *testing.T) {
		srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, "file:///etc/passwd", http.StatusFound)
		}))
		defer srv.Close()
		if _, err := client.Get(srv.URL); err == nil {
			t.Error("redirect to non-http(s) scheme succeeded, want rejection")
		}
	})

	t.Run("too-many-redirects-rejected", func(t *testing.T) {
		// A self-referential redirect loop: must trip the MaxRedirects cap, not hang.
		var srv *httptest.Server
		srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			http.Redirect(w, r, srv.URL+"/next", http.StatusFound)
		}))
		defer srv.Close()
		if _, err := client.Get(srv.URL); err == nil {
			t.Error("infinite redirect chain succeeded, want too-many-redirects rejection")
		}
	})
}
