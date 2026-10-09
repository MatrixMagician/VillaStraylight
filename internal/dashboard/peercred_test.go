package dashboard

import (
	"context"
	"io"
	"net"
	"os"
	"strings"
	"syscall"
	"testing"
)

// TestParseProcNetTCPLine pins the column layout of a /proc/net/tcp[6] data row:
// local_address is field 1, rem_address field 2, st field 3, uid field 7. Fixture
// lines are copied verbatim from a real /proc/net/tcp and /proc/net/tcp6 on this host.
func TestParseProcNetTCPLine(t *testing.T) {
	cases := []struct {
		name   string
		line   string
		want   procNetTCPRow
		wantOK bool
	}{
		{
			name:   "ipv4 listening row",
			line:   "   0: 0100007F:DFFF 00000000:0000 0A 00000000:00000000 00:00000000 00000000  1000        0 380922 1 00000000fa13c07f 100 0 0 10 0",
			want:   procNetTCPRow{local: "0100007F:DFFF", remote: "00000000:0000", state: "0A", uid: 1000},
			wantOK: true,
		},
		{
			name:   "ipv4 established row",
			line:   "  27: 6201A8C0:9932 A5429522:01BB 01 00000000:00000000 02:00000040 00000000  1000        0 25804404 2 0000000072bcf8b0 20 4 30 42 -1",
			want:   procNetTCPRow{local: "6201A8C0:9932", remote: "A5429522:01BB", state: "01", uid: 1000},
			wantOK: true,
		},
		{
			name:   "ipv4 time-wait row",
			line:   "  17: 0100007F:8997 0100007F:B6DA 06 00000000:00000000 03:000008F0 00000000     0        0 0 3 00000000201e200b",
			want:   procNetTCPRow{local: "0100007F:8997", remote: "0100007F:B6DA", state: "06", uid: 0},
			wantOK: true,
		},
		{
			name:   "ipv6 row",
			line:   "   0: 00000000000000000000000001000000:0277 00000000000000000000000000000000:0000 0A 00000000:00000000 00:00000000 00000000     0        0 10821 1 00000000bec1ec59 100 0 0 10 0",
			want:   procNetTCPRow{local: "00000000000000000000000001000000:0277", remote: "00000000000000000000000000000000:0000", state: "0A", uid: 0},
			wantOK: true,
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
			row, ok := parseProcNetTCPLine(tc.line)
			if ok != tc.wantOK {
				t.Fatalf("ok = %v, want %v", ok, tc.wantOK)
			}
			if ok && row != tc.want {
				t.Fatalf("got %+v, want %+v", row, tc.want)
			}
		})
	}
}

// TestScanProcNetTCP pins that a row is the peer's socket only when it matches the
// whole connection AND is established: Linux lets several sockets share one local
// address:port (connections to distinct remotes, and TIME_WAIT rows, which list
// uid 0), so the first row with the peer's local address can belong to a
// different socket. Anything short of all three is (0, false), fail closed.
func TestScanProcNetTCP(t *testing.T) {
	const ipv4Header = "  sl  local_address rem_address   st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"
	const ipv6Header = "  sl  local_address                         remote_address                        st tx_queue rx_queue tr tm->when retrnsmt   uid  timeout inode\n"

	// The peer 127.0.0.1:50000 connected to the dashboard at 127.0.0.1:8888 (22B8).
	const (
		peer      = "0100007F:C350"
		dashboard = "0100007F:22B8"
	)
	established := "   2: 0100007F:C350 0100007F:22B8 01 00000000:00000000 00:00000000 00000000  1001        0 400100 1 00000000aa13c07f 20 4 30 10 -1\n"

	cases := []struct {
		name      string
		fixture   string
		peer      string
		dashboard string
		wantUID   int
		wantOK    bool
	}{
		{
			name: "same local address to another remote is skipped",
			fixture: ipv4Header +
				"   1: 0100007F:C350 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  2002        0 400099 1 00000000bb13c07f 20 4 30 10 -1\n" +
				established,
			peer: peer, dashboard: dashboard, wantUID: 1001, wantOK: true,
		},
		{
			name: "time-wait row on the same connection is skipped",
			fixture: ipv4Header +
				"   0: 0100007F:C350 0100007F:22B8 06 00000000:00000000 03:000008F0 00000000     0        0 0 3 00000000201e200b\n" +
				established,
			peer: peer, dashboard: dashboard, wantUID: 1001, wantOK: true,
		},
		{
			name: "no row matches all three fails closed",
			fixture: ipv4Header +
				"   0: 0100007F:C350 0100007F:22B8 06 00000000:00000000 03:000008F0 00000000     0        0 0 3 00000000201e200b\n" +
				"   1: 0100007F:C350 0100007F:1F90 01 00000000:00000000 00:00000000 00000000  2002        0 400099 1 00000000bb13c07f 20 4 30 10 -1\n" +
				"   2: 0100007F:22B8 0100007F:C350 01 00000000:00000000 00:00000000 00000000  1001        0 400101 1 00000000cc13c07f 20 4 30 10 -1\n",
			peer: peer, dashboard: dashboard, wantUID: 0, wantOK: false,
		},
		{
			name:    "case-insensitive addresses",
			fixture: ipv4Header + established,
			peer:    "0100007f:c350", dashboard: "0100007f:22b8", wantUID: 1001, wantOK: true,
		},
		{
			name: "ipv4-mapped connection in tcp6",
			fixture: ipv6Header +
				"   0: 0000000000000000FFFF00000100007F:C350 0000000000000000FFFF00000100007F:22B8 06 00000000:00000000 03:000008F0 00000000     0        0 0 3 00000000201e200b\n" +
				"   1: 0000000000000000FFFF00000100007F:C350 0000000000000000FFFF00000100007F:22B8 01 00000000:00000000 00:00000000 00000000  1001        0 400102 1 00000000dd13c07f 20 4 30 10 -1\n",
			peer: "0000000000000000FFFF00000100007F:C350", dashboard: "0000000000000000FFFF00000100007F:22B8", wantUID: 1001, wantOK: true,
		},
		{
			name: "ipv6 connection",
			fixture: ipv6Header +
				"   0: 00000000000000000000000001000000:C350 00000000000000000000000001000000:22B8 01 00000000:00000000 00:00000000 00000000    26        0 17201 1 0000000088f11e42 20 4 30 10 -1\n",
			peer: "00000000000000000000000001000000:C350", dashboard: "00000000000000000000000001000000:22B8", wantUID: 26, wantOK: true,
		},
		{name: "empty file", fixture: "", peer: peer, dashboard: dashboard, wantOK: false},
		{name: "header only", fixture: ipv4Header, peer: peer, dashboard: dashboard, wantOK: false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			uid, ok := scanProcNetTCP(strings.NewReader(tc.fixture), tc.peer, tc.dashboard)
			if ok != tc.wantOK || uid != tc.wantUID {
				t.Fatalf("scanProcNetTCP = (%d, %v), want (%d, %v)", uid, ok, tc.wantUID, tc.wantOK)
			}
		})
	}
}

// TestEncodeProcNetAddr pins the address encoding per file: /proc/net/tcp holds
// AF_INET sockets in one word, /proc/net/tcp6 holds AF_INET6 sockets in four, and
// an IPv4 connection on an AF_INET6 socket appears there IPv4-mapped. An IPv6
// address has no /proc/net/tcp encoding, so it reports false rather than a needle.
func TestEncodeProcNetAddr(t *testing.T) {
	cases := []struct {
		name   string
		ip     net.IP
		port   int
		v6     bool
		want   string
		wantOK bool
	}{
		{"ipv4 loopback in tcp", net.ParseIP("127.0.0.1"), 0xDFFF, false, "0100007F:DFFF", true},
		{"ipv4 low port zero-padded", net.ParseIP("127.0.0.1"), 80, false, "0100007F:0050", true},
		{"ipv4-mapped form in tcp", net.ParseIP("::ffff:127.0.0.1"), 0x22B8, false, "0100007F:22B8", true},
		{"ipv4 in tcp6 is mapped", net.ParseIP("127.0.0.1"), 0x22B8, true, "0000000000000000FFFF00000100007F:22B8", true},
		{"ipv6 loopback in tcp6", net.ParseIP("::1"), 0x0277, true, "00000000000000000000000001000000:0277", true},
		{"ipv6 loopback has no tcp form", net.ParseIP("::1"), 0x0277, false, "", false},
		{"nil ip", nil, 80, true, "", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, ok := encodeProcNetAddr(tc.ip, tc.port, tc.v6)
			if got != tc.want || ok != tc.wantOK {
				t.Fatalf("encodeProcNetAddr(%v, %d, v6=%v) = (%q, %v), want (%q, %v)", tc.ip, tc.port, tc.v6, got, ok, tc.want, tc.wantOK)
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

	uid, found := lookupPeerUID(accepted.RemoteAddr().(*net.TCPAddr), accepted.LocalAddr().(*net.TCPAddr))
	if !found {
		t.Fatal("lookupPeerUID did not find the dialed connection's socket in /proc/net/tcp[6]")
	}
	wantUID := os.Getuid()
	if uid != wantUID {
		t.Fatalf("lookupPeerUID = %d, want %d (os.Getuid)", uid, wantUID)
	}
}

// reuseAddrDialer dials from 127.0.0.1:port with SO_REUSEADDR, so a second dial
// can bind a local port whose previous connection still sits in TIME_WAIT.
func reuseAddrDialer(port int) *net.Dialer {
	return &net.Dialer{
		LocalAddr: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: port},
		Control: func(_, _ string, c syscall.RawConn) error {
			var serr error
			if err := c.Control(func(fd uintptr) {
				serr = syscall.SetsockoptInt(int(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
			}); err != nil {
				return err
			}
			return serr
		},
	}
}

// TestLookupPeerUIDRealTimeWaitDecoy reproduces the CI flake on the real
// /proc/net/tcp: the peer's local port also names an earlier connection, now in
// TIME_WAIT and listed with uid 0. The lookup must still resolve the live
// connection to os.Getuid(), not the TIME_WAIT row that shares its local address.
func TestLookupPeerUIDRealTimeWaitDecoy(t *testing.T) {
	ctx := context.Background()
	lnDecoy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer lnDecoy.Close()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	defer ln.Close()

	acceptedCh := make(chan net.Conn, 1)
	go func() {
		conn, err := lnDecoy.Accept()
		if err != nil {
			return
		}
		acceptedCh <- conn
	}()
	decoy, err := reuseAddrDialer(0).DialContext(ctx, "tcp", lnDecoy.Addr().String())
	if err != nil {
		t.Fatalf("dial decoy: %v", err)
	}
	decoyServer := <-acceptedCh
	port := decoy.LocalAddr().(*net.TCPAddr).Port
	decoy.Close()
	_, _ = io.Copy(io.Discard, decoyServer)
	decoyServer.Close()

	go func() {
		conn, err := ln.Accept()
		if err != nil {
			return
		}
		acceptedCh <- conn
	}()
	dialed, err := reuseAddrDialer(port).DialContext(ctx, "tcp", ln.Addr().String())
	if err != nil {
		t.Fatalf("dial from port %d: %v", port, err)
	}
	defer dialed.Close()
	accepted := <-acceptedCh
	defer accepted.Close()

	uid, found := lookupPeerUID(accepted.RemoteAddr().(*net.TCPAddr), accepted.LocalAddr().(*net.TCPAddr))
	if !found || uid != os.Getuid() {
		t.Fatalf("lookupPeerUID = (%d, %v), want (%d, true)", uid, found, os.Getuid())
	}
}

// TestLookupPeerUIDNilAddr asserts the fail-closed contract on a nil address: no
// address to resolve is refused, never assumed to be this process.
func TestLookupPeerUIDNilAddr(t *testing.T) {
	addr := &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8888}
	if _, ok := lookupPeerUID(nil, addr); ok {
		t.Fatal("lookupPeerUID(nil, local) should fail closed (ok=false), got ok=true")
	}
	if _, ok := lookupPeerUID(addr, nil); ok {
		t.Fatal("lookupPeerUID(peer, nil) should fail closed (ok=false), got ok=true")
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

// dashboardAddr is the accepting end every fakeConn reports unless a case sets its own.
var dashboardAddr = &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 8888}

// fakeConn is a minimal net.Conn recording whether Close was called, with fixed
// RemoteAddr and LocalAddr so the lookup seam can key off them deterministically.
type fakeConn struct {
	net.Conn
	remote net.Addr
	local  net.Addr
	closed bool
}

func (f *fakeConn) RemoteAddr() net.Addr { return f.remote }
func (f *fakeConn) LocalAddr() net.Addr {
	if f.local == nil {
		return dashboardAddr
	}
	return f.local
}
func (f *fakeConn) Close() error { f.closed = true; return nil }

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
	otherEnd := &fakeConn{remote: goodAddr, local: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 9999}}

	inner := &fakeUIDListener{conns: []net.Conn{bad, unresolvable, otherEnd, good}}
	l := &peerUIDListener{
		Listener:   inner,
		allowedUID: 1000,
		lookup: func(addr, local *net.TCPAddr) (int, bool) {
			if !local.IP.Equal(dashboardAddr.IP) || local.Port != dashboardAddr.Port {
				return 0, false
			}
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
	if !otherEnd.closed {
		t.Error("connection whose accepting end the lookup did not match was not closed")
	}
	if good.closed {
		t.Error("allowed connection must not be closed by the gate")
	}
}

// TestPeerUIDListenerAcceptNonTCP asserts a non-TCP RemoteAddr or LocalAddr (should
// not occur on a "tcp"-listener but must not panic or be trusted) is refused.
func TestPeerUIDListenerAcceptNonTCP(t *testing.T) {
	nonTCPRemote := &fakeConn{remote: &net.UnixAddr{Name: "irrelevant"}}
	nonTCPLocal := &fakeConn{
		remote: &net.TCPAddr{IP: net.ParseIP("127.0.0.1"), Port: 2},
		local:  &net.UnixAddr{Name: "irrelevant"},
	}
	inner := &fakeUIDListener{conns: []net.Conn{nonTCPRemote, nonTCPLocal}}
	l := &peerUIDListener{
		Listener:   inner,
		allowedUID: 1000,
		lookup:     func(_, _ *net.TCPAddr) (int, bool) { return 1000, true },
	}

	_, err := l.Accept()
	if err == nil {
		t.Fatal("Accept should have kept looping past the non-TCP conns and hit the closed inner listener")
	}
	if !nonTCPRemote.closed {
		t.Error("non-TCP RemoteAddr connection was not closed")
	}
	if !nonTCPLocal.closed {
		t.Error("non-TCP LocalAddr connection was not closed")
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
