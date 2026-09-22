// peercred.go implements GHSA-6mq8's caller-authentication check: the dashboard's
// 127.0.0.1 listener is reachable by every local UNIX account, not only its owner,
// so each accepted TCP connection is checked against the owning UID of the process
// that opened it. Linux-only — it reads /proc/net/tcp[6], which is a Linux-specific
// procfs surface with no portable equivalent — and fails CLOSED: a connection whose
// peer cannot be resolved is refused exactly like one that resolves to the wrong
// UID, never assumed innocent.
package dashboard

import (
	"bufio"
	"encoding/hex"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
)

// procNetTCPPaths are consulted in order. tcp6 also carries IPv4-mapped
// connections on a dual-stack listener, so both are always checked regardless of
// the peer's address family.
var procNetTCPPaths = []string{"/proc/net/tcp", "/proc/net/tcp6"}

// lookupPeerUID resolves the UID that owns the local socket at addr — i.e. the
// connecting peer's own end of the connection, which is what net.Conn.RemoteAddr
// reports on the accepting side — by scanning /proc/net/tcp and /proc/net/tcp6.
// It fails closed: any read error or an unmatched socket reports (0, false), never
// a default UID.
func lookupPeerUID(addr *net.TCPAddr) (int, bool) {
	if addr == nil {
		return 0, false
	}
	needle := encodeProcNetAddr(addr.IP, addr.Port)
	for _, path := range procNetTCPPaths {
		f, err := os.Open(path) //nolint:gosec // fixed procfs path
		if err != nil {
			continue
		}
		uid, ok := scanProcNetTCP(f, needle)
		f.Close()
		if ok {
			return uid, true
		}
	}
	return 0, false
}

// scanProcNetTCP is the pure line-scanning core: given the body of a /proc/net/tcp[6]
// file and a needle in that file's own "ADDR:PORT" hex encoding, it returns the uid
// column of the first matching row. Separated from lookupPeerUID so it is
// table-testable against fixture text without a real /proc.
func scanProcNetTCP(r io.Reader, needle string) (int, bool) {
	sc := bufio.NewScanner(r)
	sc.Scan() // header line ("sl local_address rem_address st ... uid ...")
	for sc.Scan() {
		localAddr, uid, ok := parseProcNetTCPLine(sc.Text())
		if ok && strings.EqualFold(localAddr, needle) {
			return uid, true
		}
	}
	return 0, false
}

// parseProcNetTCPLine parses one data row of /proc/net/tcp[6]. The columns are
// whitespace-separated: sl, local_address, rem_address, st, tx_queue:rx_queue,
// tr:tm->when, retrnsmt, uid, timeout, inode, ... — so uid is field index 7.
func parseProcNetTCPLine(line string) (localAddr string, uid int, ok bool) {
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return "", 0, false
	}
	uid, err := strconv.Atoi(fields[7])
	if err != nil {
		return "", 0, false
	}
	return fields[1], uid, true
}

// encodeProcNetAddr renders ip:port in /proc/net/tcp[6]'s own encoding: uppercase
// hex, each 32-bit word byte-reversed from network order (the kernel prints its
// native little-endian in-memory representation of the address), colon-joined with
// the port in plain (non-reversed) uppercase hex.
func encodeProcNetAddr(ip net.IP, port int) string {
	var b strings.Builder
	if v4 := ip.To4(); v4 != nil {
		b.WriteString(encodeProcNetWord(v4))
	} else {
		v6 := ip.To16()
		for i := 0; i < 16; i += 4 {
			b.WriteString(encodeProcNetWord(v6[i : i+4]))
		}
	}
	fmt.Fprintf(&b, ":%04X", port)
	return b.String()
}

// encodeProcNetWord reverses one 4-byte big-endian chunk into /proc/net/tcp's
// per-word little-endian hex, e.g. 127.0.0.1 ([7F 00 00 01]) -> "0100007F".
func encodeProcNetWord(b []byte) string {
	rev := [4]byte{b[3], b[2], b[1], b[0]}
	return strings.ToUpper(hex.EncodeToString(rev[:]))
}

// peerUIDListener wraps a net.Listener so every accepted connection is checked
// against allowedUID BEFORE net/http ever sees it — this is what makes the check
// apply to every request, including a bare GET of the SPA shell, rather than only
// the routes a header-based middleware could gate. A peer that fails the check
// (unresolvable, or resolves to a different UID) is closed immediately and Accept
// loops for the next connection: one hostile or unresolvable peer must never make
// the listener return an error and take the long-lived dashboard down.
type peerUIDListener struct {
	net.Listener
	allowedUID int
	lookup     func(*net.TCPAddr) (int, bool)
}

// newPeerUIDListener wraps inner, defaulting lookup to the real /proc/net/tcp[6]
// scan; a test supplies a fake lookup to exercise the accept/refuse loop off-host.
func newPeerUIDListener(inner net.Listener, allowedUID int) *peerUIDListener {
	return &peerUIDListener{Listener: inner, allowedUID: allowedUID, lookup: lookupPeerUID}
}

// Accept only returns connections whose peer UID matches allowedUID. It never
// returns a refused connection's error to the caller — refusing silently by
// closing and retrying is what keeps a probing/hostile local peer from being able
// to stop the dashboard from accepting anyone else.
func (l *peerUIDListener) Accept() (net.Conn, error) {
	for {
		conn, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		tcpAddr, ok := conn.RemoteAddr().(*net.TCPAddr)
		if !ok {
			conn.Close()
			continue
		}
		uid, ok := l.lookup(tcpAddr)
		if !ok || uid != l.allowedUID {
			conn.Close()
			continue
		}
		return conn, nil
	}
}
