// Package websafe is the pure, off-hardware-unit-testable fetch core that is the
// sole producer of OWUI page_content (GUARD-01) with a comprehensive SSRF guard
// (GUARD-05) and the verified OWUI external-loader HTTP contract glue (GROUND-01).
//
// This package is a PURE core (CLAUDE.md: orchestrate is the only intentionally-impure
// module). It constructs an *http.Client but performs no network I/O at package scope;
// the live client is built only when the cmd tier (Plan 03) calls SafeClient and serves
// the handler. The network fetch is injected via the Deps func-field seam (websafe.go)
// so the core is fully unit-testable without a live network.
//
// ssrf.go is the SSRF guard: the netip reject-set, the connect-time net.Dialer.Control
// hook (validates the CONNECTED IP, defeating DNS-rebinding TOCTOU), the per-hop
// CheckRedirect re-validation (scheme allowlist + hostname reject + redirect cap), and
// SafeClient which wires them together. There is no in-repo SSRF analog; this is the
// verified stdlib pattern (net.Dialer.Control + net/netip).
//
// TestSeamGrepGate discipline: this file contains NO container-image or
// villa-<service>:<port> network-identity literals. The only host tokens here are the
// reject-set sentinels (loopback / villa- prefix / .network suffix), which are SSRF
// guards — they identify what to REFUSE, not a network endpoint to connect to.
package websafe

import (
	"fmt"
	"net"
	"net/http"
	"net/netip"
	"strings"
	"syscall"
	"time"
)

// Bounds are the conservative per-fetch resource limits (CONTEXT Area 3). They are
// shared by the SSRF client (Timeout, MaxRedirects) and the fetch core (MaxBytes,
// MaxConcurrent), so they live with the SSRF guard that SafeClient consumes.
type Bounds struct {
	// MaxBytes caps each fetched body; bytes beyond are truncated (never an
	// unbounded read from an untrusted site).
	MaxBytes int64
	// Timeout caps each fetch (overall request + per-dial), so a slow/hanging
	// upstream cannot stall a batch.
	Timeout time.Duration
	// MaxConcurrent bounds in-flight fetches across a batch.
	MaxConcurrent int
	// MaxRedirects caps the redirect chain; each hop is SSRF-re-validated.
	MaxRedirects int
}

// DefaultBounds returns the conservative v1.5 defaults (CONTEXT Area 3): ~2 MiB body
// cap, 10s timeout, a small fetch-concurrency bound, and a 5-hop redirect cap. These
// are deliberately conservative; on-hardware tuning is deferred to Phase 33/34.
func DefaultBounds() Bounds {
	return Bounds{
		MaxBytes:      2 << 20, // 2 MiB
		Timeout:       10 * time.Second,
		MaxConcurrent: 4,
		MaxRedirects:  5,
	}
}

// rejectPrefixes is the SSRF reject-set (CONTEXT Area 3, GUARD-05): loopback v4/v6,
// all RFC1918 private ranges, link-local v4 (incl. the 169.254.169.254 cloud-metadata
// IP) + v6, CGNAT, ULA v6, "this network", and the v4-mapped-v6 range (catches a
// mapped-internal address). netip.Prefix.Contains is the allocation-free membership test.
var rejectPrefixes = []netip.Prefix{
	netip.MustParsePrefix("127.0.0.0/8"),    // loopback v4
	netip.MustParsePrefix("::1/128"),        // loopback v6
	netip.MustParsePrefix("10.0.0.0/8"),     // RFC1918
	netip.MustParsePrefix("172.16.0.0/12"),  // RFC1918
	netip.MustParsePrefix("192.168.0.0/16"), // RFC1918
	netip.MustParsePrefix("169.254.0.0/16"), // link-local v4 (incl. 169.254.169.254 metadata)
	netip.MustParsePrefix("fe80::/10"),      // link-local v6
	netip.MustParsePrefix("100.64.0.0/10"),  // CGNAT
	netip.MustParsePrefix("fc00::/7"),       // ULA v6
	netip.MustParsePrefix("0.0.0.0/8"),      // "this network"
	netip.MustParsePrefix("::ffff:0:0/96"),  // v4-mapped v6 (catch mapped-internal)
}

// ipRejected reports whether ip is an internal/reserved address that the fetcher must
// refuse to connect to. It unmaps a v4-in-v6 address first (so a mapped-internal addr
// is caught on its v4 form), checks the stdlib Is* predicates (loopback, link-local,
// private, unspecified), then the explicit rejectPrefixes set. An invalid address is
// rejected (fail-closed).
func ipRejected(ip netip.Addr) bool {
	if ip.Is4In6() {
		ip = ip.Unmap()
	}
	if !ip.IsValid() ||
		ip.IsLoopback() ||
		ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() ||
		ip.IsPrivate() ||
		ip.IsUnspecified() {
		return true
	}
	for _, p := range rejectPrefixes {
		if p.Contains(ip) {
			return true
		}
	}
	return false
}

// hostRejected blocks internal service names regardless of DNS resolution
// (defense-in-depth per GUARD-05 and CONTEXT Area 3): "localhost", any "villa-" prefix
// (the managed-service container-DNS names), and any ".network"/".localhost" suffix.
// This is a name-based backstop; the authoritative check is the connect-time control
// hook on the resolved IP. The comparison is case-insensitive.
func hostRejected(host string) bool {
	h := strings.ToLower(host)
	return h == "localhost" ||
		strings.HasPrefix(h, "villa-") ||
		strings.HasSuffix(h, ".network") ||
		strings.HasSuffix(h, ".localhost")
}

// control is the net.Dialer.Control hook. Go calls it AFTER DNS resolution and BEFORE
// connect, with the ACTUAL socket address — so validating here defeats the DNS-rebinding
// TOCTOU (a public hostname that resolves to an internal IP is caught on the resolved
// IP, not the trusted hostname). It splits host:port, parses the IP, and returns an
// SSRF error when ipRejected.
func control(network, address string, _ syscall.RawConn) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return err
	}
	ip, err := netip.ParseAddr(host)
	if err != nil {
		return fmt.Errorf("SSRF: unparseable connect address %q", host)
	}
	if ipRejected(ip) {
		return fmt.Errorf("SSRF: refusing connection to internal address %s", ip)
	}
	return nil
}

// SafeClient builds the SSRF-guarded *http.Client from the given Bounds: a net.Dialer
// whose Control hook validates the connected IP, an overall request Timeout, and a
// CheckRedirect that (1) caps the chain at b.MaxRedirects, (2) rejects non-http(s)
// redirect schemes, and (3) rejects hostRejected redirect targets. The Control hook
// re-validates the resolved IP on each new dial, so every redirect hop is fully checked.
//
// This builds (does not run) a client; it is invoked from the cmd tier (Plan 03) with
// the same Bounds the Loader uses. Keeping it here keeps every backend/network literal
// out of the cmd caller (TestSeamGrepGate-clean).
func SafeClient(b Bounds) *http.Client {
	d := &net.Dialer{Timeout: b.Timeout, Control: control}
	tr := &http.Transport{DialContext: d.DialContext}
	return &http.Client{
		Timeout:   b.Timeout,
		Transport: tr,
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= b.MaxRedirects {
				return fmt.Errorf("SSRF: too many redirects (>%d)", b.MaxRedirects)
			}
			if req.URL.Scheme != "http" && req.URL.Scheme != "https" {
				return fmt.Errorf("SSRF: non-http(s) redirect scheme %q", req.URL.Scheme)
			}
			if hostRejected(req.URL.Hostname()) {
				return fmt.Errorf("SSRF: refusing internal redirect host %q", req.URL.Hostname())
			}
			// The Control hook re-validates the resolved IP on the new dial.
			return nil
		},
	}
}
