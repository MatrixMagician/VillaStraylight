package dashboard

import (
	"net"
	"os"
	"strings"
	"testing"
)

// TestParseProcNetTCPLine pins the column layout of a /proc/net/tcp[6] data row:
// local_address is field 1, uid is field 7. Fixture lines are copied verbatim from
// a real /proc/net/tcp and /proc/net/tcp6 on this host.
func TestParseProcNetTCPLine(t *testing.T) {
	cases := []struct {
		name     string
		line     string
		wantAddr string
		wantUID  int
		wantOK   bool
	}{
		{
			name:     "ipv4 row",
			line:     "   0: 0100007F:DFFF 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 380922 1 00000000fa13c07f 100 0 0 10 0",
			wantAddr: "0100007F:DFFF",
			wantUID:  1000,
			wantOK:   true,
		},
		{
			name:     "ipv6 row",
			line:     "   0: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10821 1 00000000bec1ec59 100 0 0 10 0",
			wantAddr: "00000000000000000000000001000000:0277",
			wantUID:  0,
			wantOK:   true,
		},
		{
			name:   "too few fields",
			line:   "  0: 0100007F:DFFF",
			wantOK: false,
		},
		{
			name:   "empty",
			line:   "",
			wantOK: false,
		},
		{
			name:   "non-numeric uid",
			line:   "0: 0100007F:DFFF 00000000:0000 0A 00000000:00000000 00:00000000 00000000  NOTANUM 0 380922",
			wantOK: false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr, uid, ok := parseProcNetTCPLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if !ok {
				return
			}
			if addr != tc.wantAddr || uid != tc.wantUID {
				t.Fatalf("got (%q, %d), want (%q, %d)", addr, uid, tc.wantAddr, tc.wantUID)
			}
		})
	}
}

// TestScanProcNetTCP pins the whole-file scan against fixture text mirroring real
// /proc/net/tcp (IPv4) and /proc/net/tcp6 (IPv6) content, including the header line
// that must be skipped rather than mistaken for a data row.
func TestScanProcNetTCP(t *testing.T) {
	ipv4Fixture := "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 0100007F:DFFF 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 380922 1 00000000fa13c07f 100 0 0 10 0\n" +
		"   1: 0100007F:9A8B 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 382674 1 000000004c85f9a8 100 0 0 10 0\n"

	ipv6Fixture := "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n" +
		"   0: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10821 1 00000000bec1ec59 100 0 0 10 0\n" +
		"   1: 00000000000000000000000001000000:1538 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000    26        0 17201 1 0000000088f11e42 100 0 0 10 0\n"

	cases := []struct {
		name    string
		fixture string
		needle  string
		wantUID int
		wantOK  bool
	}{
		{"ipv4 match", ipv4Fixture, "0100007F:9A8B", 1000, true},
		{"ipv4 no match", ipv4Fixture, "0100007F:FFFF", 0, false},
		{"ipv4 case-insensitive needle", ipv4Fixture, "0100007f:dfff", 1000, true},
		{"ipv6 match", ipv6Fixture, "00000000000000000000000001000000:1538", 26, true},
		{"ipv6 no match", ipv6Fixture, "00000000000000000000000001000000:FFFF", 0, false},
		{"empty file", "", "0100007F:DFFF", 0, false},
		{"header only", "  sl  local_address rem_address\n", "0100007F:DFFF", 0, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uid, ok := scanProcNetTCP(strings.NewReader(tc.fixture), tc.needle)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && uid != tc.wantUID {
				t.Fatalf("uid = %d, want %d", uid, tc.wantUID)
			}
		})
	}
}

// TestEncodeProcNetAddr pins the address encoding against the real fixture values
// captured above, both directions (IPv4 single-word, IPv6 four-word).
func TestEncodeProcNetAddr(t *testing.T) {
	cases := []struct {
		name string
		ip   net.IP
		port int
		want string
	}{
		{"ipv4 loopback", net.ParseIP("127.0.0.1"), 0xDFFF, "0100007F:DFFF"},
		{"ipv6 loopback", net.ParseIP("::1"), 0x0277, "00000000000000000000000001000000:0277"},
		{"ipv4 low port zero-padded", net.ParseIP("127.0.0.1"), 80, "0100007F:0050"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := encodeProcNetAddr(tc.ip, tc.port)
			if got != tc.want {
				t.Fatalf("encodeProcNetAddr(%v, %d) = %q, want %q", tc.ip, tc.port, got, tc.want)
			}
		})
	}
}

// TestLookupPeerUIDReal proves the real /proc/net/tcp[6] path end to end: open an
// actual loopback listener, connect to it from this same process, and assert the
// lookup resolves the accepted connection's peer to os.Getuid() — the exact
// invariant the dashboard's Accept-time gate relies on.
func TestLookupPeerUIDReal(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	acceptedCh := make(chan net.Conn, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		acceptedCh <- conn
	}()

	dialed, err := net.Dial("tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial: %v", err)
	}
	defer dialed.Close()

	accepted := <-acceptedCh
	defer accepted.Close()

	peerAddr, ok := accepted.RemoteAddr().(*net.TCPAddr)
	if !ok {
		t.Fatalf("RemoteAddr is not *net.TCPAddr: %T", accepted.RemoteAddr())
	}

	uid, found := lookupPeerUID(peerAddr)
	if !found {
		t.Fatal("lookupPeerUID did not find the dialed connection's socket in /proc/net/tcp[6]")
	}
	wantUID := os.Getuid()
	if uid != wantUID {
		t.Fatalf("lookupPeerUID = %d, want %d (os.Getuid)", uid, wantUID)
	}
}

// TestLookupPeerUIDNilAddr asserts the fail-closed contract on a nil address: no
// address to resolve is refused, never assumed to be this process.
func TestLookupPeerUIDNilAddr(t *testing.T) {
	if _, ok := lookupPeerUID(nil); ok {
		t.Fatal("lookupPeerUID(nil) should fail closed (ok=false), got ok=true")
	}
}

// fakeUIDListener is a minimal net.Listener test double whose Accept returns a
// scripted sequence of connections, so peerUIDListener's retry-on-refuse loop can
// be exercised without a real socket per case.
type fakeUIDListener struct {
	conns []net.Conn
	i     int
}

func (f *fakeUIDListener) Accept() (net.Conn, error) {
	if f.i >= len(f.conns) {
		return nil, net.ErrClosed
	}
	c := f.conns[f.i]
	f.i++
	return c, nil
}
func (f *fakeUIDListener) Close() error   { return nil }
func (f *fakeUIDListener) Addr() net.Addr { return &net.TCPAddr{} }

// fakeConn is a minimal net.Conn recording whether Close was called, with a fixed
// RemoteAddr so the lookup seam can key off it deterministically.
type fakeConn struct {
	net.Conn
	remote net.Addr
	closed bool
}

func (f *fakeConn) RemoteAddr() net.Addr { return f.remote }
func (f *fakeConn) Close() error         { f.closed = true; return nil }

// TestPeerUIDListenerAccept pins the Accept-time gate (GHSA-6mq8): a connection
// whose peer UID does not match allowedUID is closed and never returned, and
// Accept keeps looping until it finds (or runs out of) an allowed one — one
// hostile peer must not make Accept return an error and take the listener down.
func TestPeerUIDListenerAccept(t *testing.T) {
	badAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 1}
	goodAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2}
	unresolvableAddr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 3}

	bad := &fakeConn{remote: badAddr}
	good := &fakeConn{remote: goodAddr}
	unresolvable := &fakeConn{remote: unresolvableAddr}

	inner := &fakeUIDListener{conns: []net.Conn{bad, unresolvable, good}}
	l := &peerUIDListener{
		Listener:   inner,
		allowedUID: 1000,
		lookup: func(addr *net.TCPAddr) (int, bool) {
			switch addr.Port {
			case 1:
				return 2000, true // wrong UID
			case 3:
				return 0, false // unresolvable — fail closed
			case 2:
				return 1000, true // matches allowedUID
			default:
				return 0, false
			}
		},
	}

	conn, err := l.Accept()
	if err != nil {
		t.Fatalf("Accept: %v", err)
	}
	if conn != good {
		t.Fatalf("Accept returned %v, want the allowed connection", conn)
	}
	if !bad.closed {
		t.Error("wrong-UID connection was not closed")
	}
	if !unresolvable.closed {
		t.Error("unresolvable connection was not closed (fail-closed contract)")
	}
	if good.closed {
		t.Error("allowed connection must not be closed by the gate")
	}
}

// TestPeerUIDListenerAcceptNonTCP asserts a non-TCP RemoteAddr (should not occur on
// a "tcp"-listener but must not panic or be trusted) is refused.
func TestPeerUIDListenerAcceptNonTCP(t *testing.T) {
	nonTCP := &fakeConn{remote: &net.UnixAddr{Name: "irrelevant"}}
	inner := &fakeUIDListener{conns: []net.Conn{nonTCP}}
	l := &peerUIDListener{Listener: inner, allowedUID: 1000, lookup: lookupPeerUID}

	_, err := l.Accept()
	if err == nil {
		t.Fatal("Accept should have kept looping past the non-TCP conn and hit the closed inner listener")
	}
	if !nonTCP.closed {
		t.Error("non-TCP connection was not closed")
	}
}

// TestNewPeerUIDListenerDefaultsLookup asserts newPeerUIDListener wires the real
// lookupPeerUID, not a nil func, so production use never nil-panics.
func TestNewPeerUIDListenerDefaultsLookup(t *testing.T) {
	inner := &fakeUIDListener{}
	l := newPeerUIDListener(inner, os.Getuid())
	if l.lookup == nil {
		t.Fatal("newPeerUIDListener left lookup nil")
	}
	if l.allowedUID != os.Getuid() {
		t.Fatalf("allowedUID = %d, want %d", l.allowedUID, os.Getuid())
	}
}
