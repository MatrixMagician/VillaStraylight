package dashboard

import (
	"net"
	"strings"
	"testing"
)

// mustNewServer constructs a Server, failing the test on a (loopback-validation or
// asset) error so the common happy-path tests stay terse. The non-loopback
// refusal is asserted directly against NewServer in TestNewServerRefusesNonLoopback.
func mustNewServer(t *testing.T, cfg Config) *Server {
	t.Helper()
	srv, err := NewServer(cfg)
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	return srv
}

// TestServerAddrIsLoopback asserts the http.Server.Addr is built via
// net.JoinHostPort(DashboardAddr, port) with a 127.0.0.1 default — NEVER ":8888"
// or "0.0.0.0" (Pitfall 6). This is the privacy-posture test
// the threat register requires.
func TestServerAddrIsLoopback(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "127.0.0.1", DashboardPort: 8888})

	addr := srv.Addr()
	want := net.JoinHostPort("127.0.0.1", "8888")
	if addr != want {
		t.Fatalf("server addr = %q, want %q", addr, want)
	}
	if strings.HasPrefix(addr, ":") {
		t.Fatalf("server addr %q binds all interfaces (leading colon) — must be loopback", addr)
	}
	if strings.Contains(addr, "0.0.0.0") {
		t.Fatalf("server addr %q binds 0.0.0.0 — must be loopback", addr)
	}
}

// TestServerAddrDefaultsLoopback asserts an empty DashboardAddr defaults to
// 127.0.0.1 rather than the all-interfaces empty host (Pitfall 6).
func TestServerAddrDefaultsLoopback(t *testing.T) {
	srv := mustNewServer(t, Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: "", DashboardPort: 8888})

	addr := srv.Addr()
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("SplitHostPort(%q): %v", addr, err)
	}
	if host != "127.0.0.1" {
		t.Fatalf("default host = %q, want 127.0.0.1", host)
	}
}

// TestNewServerRefusesNonLoopback asserts NewServer REFUSES a non-loopback bind address
// (e.g. "0.0.0.0") with an error rather than constructing a Server that would bind all
// interfaces — enforcing the posture by construction, not merely by the
// empty-string default. The loopback aliases must still succeed.
func TestNewServerRefusesNonLoopback(t *testing.T) {
	refused := []string{"0.0.0.0", "::", "192.168.1.10", "0.0.0.0:9999"}
	for _, addr := range refused {
		t.Run("refuse_"+addr, func(t *testing.T) {
			srv, err := NewServer(Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: addr, DashboardPort: 8888})
			if err == nil {
				t.Fatalf("NewServer(%q) should be refused, got srv=%v", addr, srv)
			}
			if srv != nil {
				t.Fatalf("NewServer(%q) refusal must return nil Server, got %v", addr, srv)
			}
		})
	}

	allowed := []string{"", "127.0.0.1", "::1", "localhost"}
	for _, addr := range allowed {
		t.Run("allow_"+addr, func(t *testing.T) {
			if _, err := NewServer(Config{StatusDeps: stubStatusDeps(t), ChatPort: 3000, DashboardAddr: addr, DashboardPort: 8888}); err != nil {
				t.Fatalf("NewServer(%q) should be allowed, got err=%v", addr, err)
			}
		})
	}
}

// TestServerHasReadDeadlines pins the slowloris guard on the dashboard's listener.
//
// Go's zero-value http.Server has NO read deadline, so a peer that connects and
// then dribbles request headers holds the connection indefinitely; enough of those
// wedge the process. The dashboard is a long-lived systemd unit, so it must not be
// wedgeable by a stuck local client.
//
// WriteTimeout is asserted ABSENT on purpose: POST /api/models/switch reconciles
// units and restarts the inference service, so an absolute response deadline would
// cut off a slow-but-correct model swap.
func TestServerHasReadDeadlines(t *testing.T) {
	s, err := NewServer(Config{DashboardPort: 8888, ChatPort: 3000})
	if err != nil {
		t.Fatalf("NewServer: %v", err)
	}
	srv := s.httpServer()

	if srv.ReadHeaderTimeout <= 0 {
		t.Error("ReadHeaderTimeout must be set — without it a stalled client holds a " +
			"connection open indefinitely (slowloris)")
	}
	if srv.IdleTimeout <= 0 {
		t.Error("IdleTimeout must be set so idle keep-alive connections are reclaimed")
	}
	if srv.WriteTimeout != 0 {
		t.Errorf("WriteTimeout must stay unset (got %v): POST /api/models/switch restarts "+
			"the inference service and legitimately outruns any fixed response deadline",
			srv.WriteTimeout)
	}
	if srv.Addr != s.Addr() {
		t.Errorf("server Addr = %q, want the loopback-only %q", srv.Addr, s.Addr())
	}
}
