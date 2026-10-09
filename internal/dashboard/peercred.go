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

// procNetTCPFiles are consulted in order. /proc/net/tcp lists AF_INET sockets and
// /proc/net/tcp6 lists AF_INET6 sockets, where an IPv4 connection appears
// IPv4-mapped, so each file's addresses are encoded in that file's own family and
// both are always checked regardless of the peer's address family.
var procNetTCPFiles = []struct {
	path string
	v6   bool
}{
	{"/proc/net/tcp", false},
	{"/proc/net/tcp6", true},
}

// tcpEstablished is the st column of an ESTABLISHED socket.
const tcpEstablished = "01"

// lookupPeerUID resolves the UID that owns the connecting peer's socket: the
// /proc/net/tcp[6] row whose local_address is peer (net.Conn.RemoteAddr on the
// accepting side), whose rem_address is local (the accepting side's
// net.Conn.LocalAddr), and whose state is ESTABLISHED. Several sockets can share
// the peer's address:port (connections to other remotes, TIME_WAIT rows), so
// only the whole connection names one socket. It fails closed: any read error or
// an unmatched socket reports (0, false), never a default UID.
func lookupPeerUID(peer, local *net.TCPAddr) (int, bool) {
	if peer == nil || local == nil {
		return 0, false
	}
	for _, file := range procNetTCPFiles {
		peerAddr, ok := encodeProcNetAddr(peer.IP, peer.Port, file.v6)
		if !ok {
			continue
		}
		localAddr, ok := encodeProcNetAddr(local.IP, local.Port, file.v6)
		if !ok {
			continue
		}
		f, err := os.Open(file.path) //nolint:gosec // fixed procfs path
		if err != nil {
			continue
		}
		uid, ok := scanProcNetTCP(f, peerAddr, localAddr)
		f.Close()
		if ok {
			return uid, true
		}
	}
	return 0, false
}

// procNetTCPRow is the part of one /proc/net/tcp[6] data row the lookup reads,
// addresses in the file's own "ADDR:PORT" hex encoding.
type procNetTCPRow struct {
	local, remote string
	state         string
	uid           int
}

// scanProcNetTCP is the pure line-scanning core: given the body of a /proc/net/tcp[6]
// file, it returns the uid of the ESTABLISHED row whose local_address is peer and
// whose rem_address is local, both in that file's own encoding. Separated from
// lookupPeerUID so it is table-testable against fixture text without a real /proc.
func scanProcNetTCP(r io.Reader, peer, local string) (int, bool) {
	sc := bufio.NewScanner(r)
	sc.Scan() // header line ("sl local_address rem_address st ... uid ...")
	for sc.Scan() {
		row, ok := parseProcNetTCPLine(sc.Text())
		if ok && row.state == tcpEstablished &&
			strings.EqualFold(row.local, peer) && strings.EqualFold(row.remote, local) {
			return row.uid, true
		}
	}
	return 0, false
}

// parseProcNetTCPLine parses one data row of /proc/net/tcp[6]. The columns are
// whitespace-separated: sl, local_address, rem_address, st, tx_queue:rx_queue,
// tr:tm->when, retrnsmt, uid, timeout, inode, ... — so uid is field index 7.
func parseProcNetTCPLine(line string) (procNetTCPRow, bool) {
	fields := strings.Fields(line)
	if len(fields) < 8 {
		return procNetTCPRow{}, false
	}
	uid, err := strconv.Atoi(fields[7])
	if err != nil {
		return procNetTCPRow{}, false
	}
	return procNetTCPRow{local: fields[1], remote: fields[2], state: fields[3], uid: uid}, true
}

// encodeProcNetAddr renders ip:port in /proc/net/tcp[6]'s own encoding: uppercase
// hex, each 32-bit word byte-reversed from network order (the kernel prints its
// native little-endian in-memory representation of the address), colon-joined with
// the port in plain (non-reversed) uppercase hex. v6 selects /proc/net/tcp6's four
// words, where an IPv4 address is IPv4-mapped; /proc/net/tcp has one word, so an
// IPv6 address has no encoding there and reports false.
func encodeProcNetAddr(ip net.IP, port int, v6 bool) (string, bool) {
	addr := ip.To4()
	if v6 {
		addr = ip.To16()
	}
	if addr == nil {
		return "", false
	}
	var b strings.Builder
	for i := 0; i < len(addr); i += 4 {
		b.WriteString(encodeProcNetWord(addr[i : i+4]))
	}
	fmt.Fprintf(&b, ":%04X", port)
	return b.String(), true
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
	lookup     func(peer, local *net.TCPAddr) (int, bool)
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
		peer, peerOK := conn.RemoteAddr().(*net.TCPAddr)
		local, localOK := conn.LocalAddr().(*net.TCPAddr)
		if !peerOK || !localOK {
			conn.Close()
			continue
		}
		uid, ok := l.lookup(peer, local)
		if !ok || uid != l.allowedUID {
			conn.Close()
			continue
		}
		return conn, nil
	}
}
