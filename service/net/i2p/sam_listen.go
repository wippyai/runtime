// SPDX-License-Identifier: MPL-2.0

package i2p

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	netservice "github.com/wippyai/runtime/service/net"
)

// listenerCounter makes listener session IDs unique across concurrent Listen
// calls on the same Service.
var listenerCounter uint64

// samListener accepts inbound I2P streams on a persistent SAM session.
// The control connection carries the session for the listener's lifetime;
// closing the listener closes the control connection and destroys the
// session, which in turn causes any in-flight STREAM ACCEPTs to error out.
type samListener struct {
	ctrlConn  net.Conn
	closeCh   chan struct{}
	pending   map[net.Conn]struct{}
	samAddr   string
	sessionID string
	ourDest   string
	closeOnce sync.Once
	mu        sync.Mutex
}

// samAddr is the net.Addr reported by listeners and accepted connections
// backed by an I2P SAM session.
type samAddr struct {
	dest    string
	session string
}

func (a samAddr) Network() string { return "i2p" }
func (a samAddr) String() string {
	if a.dest != "" {
		return a.dest
	}
	return "i2p:" + a.session
}

// Listen opens a SAM session with a transient destination and returns a
// net.Listener whose Accept issues STREAM ACCEPT on fresh SAM sockets. The
// address argument is currently ignored — I2P peers are identified by their
// destination key, not by TCP address.
func (s *Service) Listen(ctx context.Context, _, _ string) (net.Listener, error) {
	id := atomic.AddUint64(&listenerCounter, 1)
	sessionID := fmt.Sprintf("%s-listen-%d", s.sessionName, id)

	d := net.Dialer{}
	ctrlConn, err := d.DialContext(ctx, "tcp", s.addr)
	if err != nil {
		return nil, netservice.NewProtocolError("i2p", "SAM bridge connect", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		ctrlConn.SetDeadline(deadline) //nolint:errcheck
	}

	reader := bufio.NewReader(ctrlConn)

	if err := samHandshake(ctrlConn, reader); err != nil {
		ctrlConn.Close()
		return nil, err
	}

	if _, err := fmt.Fprintf(ctrlConn, "SESSION CREATE STYLE=STREAM ID=%s DESTINATION=TRANSIENT\n", sessionID); err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SESSION CREATE", err)
	}
	resp, err := samReadLine(reader)
	if err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SESSION CREATE response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		ctrlConn.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "SESSION CREATE", resp)
	}

	ourDest := samLookupMe(ctrlConn, reader)

	ctrlConn.SetDeadline(time.Time{}) //nolint:errcheck

	return &samListener{
		samAddr:   s.addr,
		sessionID: sessionID,
		ctrlConn:  ctrlConn,
		ourDest:   ourDest,
		closeCh:   make(chan struct{}),
		pending:   make(map[net.Conn]struct{}),
	}, nil
}

// Accept opens a fresh SAM socket, issues STREAM ACCEPT against the
// listener's session, and blocks until a peer connects. The returned
// connection carries any bytes buffered past the destination header.
func (l *samListener) Accept() (net.Conn, error) {
	select {
	case <-l.closeCh:
		return nil, net.ErrClosed
	default:
	}

	d := net.Dialer{}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// Cancel the dial if the listener is closed while connecting.
	go func() {
		select {
		case <-l.closeCh:
			cancel()
		case <-ctx.Done():
		}
	}()

	socket, err := d.DialContext(ctx, "tcp", l.samAddr)
	if err != nil {
		select {
		case <-l.closeCh:
			return nil, net.ErrClosed
		default:
		}
		return nil, netservice.NewProtocolError("i2p", "SAM bridge connect", err)
	}

	l.trackPending(socket)
	defer l.untrackPending(socket)

	reader := bufio.NewReader(socket)

	if err := samHandshake(socket, reader); err != nil {
		socket.Close()
		return nil, err
	}

	if _, err := fmt.Fprintf(socket, "STREAM ACCEPT ID=%s SILENT=false\n", l.sessionID); err != nil {
		socket.Close()
		return nil, netservice.NewProtocolError("i2p", "STREAM ACCEPT", err)
	}

	resp, err := samReadLine(reader)
	if err != nil {
		socket.Close()
		return nil, l.closedOr(netservice.NewProtocolError("i2p", "STREAM ACCEPT status", err))
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		socket.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "STREAM ACCEPT", resp)
	}

	// SAM bridge blocks here until a peer connects; then it emits the
	// destination header on a single line.
	header, err := samReadLine(reader)
	if err != nil {
		socket.Close()
		return nil, l.closedOr(netservice.NewProtocolError("i2p", "STREAM ACCEPT destination", err))
	}

	peerDest := header
	if sp := strings.IndexByte(header, ' '); sp >= 0 {
		peerDest = header[:sp]
	}

	return &samAcceptConn{
		Conn:   socket,
		reader: reader,
		local:  samAddr{dest: l.ourDest, session: l.sessionID},
		remote: samAddr{dest: peerDest, session: l.sessionID},
	}, nil
}

func (l *samListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closeCh)
	})

	l.mu.Lock()
	pending := make([]net.Conn, 0, len(l.pending))
	for c := range l.pending {
		pending = append(pending, c)
	}
	l.pending = nil
	l.mu.Unlock()
	for _, c := range pending {
		c.Close()
	}

	return l.ctrlConn.Close()
}

func (l *samListener) Addr() net.Addr {
	return samAddr{dest: l.ourDest, session: l.sessionID}
}

func (l *samListener) trackPending(c net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending != nil {
		l.pending[c] = struct{}{}
	}
}

func (l *samListener) untrackPending(c net.Conn) {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.pending != nil {
		delete(l.pending, c)
	}
}

func (l *samListener) closedOr(err error) error {
	select {
	case <-l.closeCh:
		return net.ErrClosed
	default:
		return err
	}
}

// samAcceptConn wraps the SAM socket returned from STREAM ACCEPT. Reads go
// through the bufio reader so that any bytes coalesced with the destination
// header line are still delivered to the caller.
type samAcceptConn struct {
	net.Conn
	reader *bufio.Reader
	local  samAddr
	remote samAddr
}

func (c *samAcceptConn) Read(p []byte) (int, error) { return c.reader.Read(p) }
func (c *samAcceptConn) LocalAddr() net.Addr        { return c.local }
func (c *samAcceptConn) RemoteAddr() net.Addr       { return c.remote }

// samHandshake performs HELLO VERSION on a freshly opened SAM socket.
func samHandshake(conn net.Conn, reader *bufio.Reader) error {
	if _, err := fmt.Fprintf(conn, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		return netservice.NewProtocolError("i2p", "SAM handshake", err)
	}
	resp, err := samReadLine(reader)
	if err != nil {
		return netservice.NewProtocolError("i2p", "SAM handshake response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		return netservice.NewProtocolRejectError("i2p", "SAM handshake", resp)
	}
	return nil
}

// samLookupMe asks the SAM bridge for our own destination key on the current
// session. Best-effort: returns "" if the lookup fails. The listener still
// works; only Addr().String() loses its .b32-ish identity.
func samLookupMe(conn net.Conn, reader *bufio.Reader) string {
	if _, err := fmt.Fprintf(conn, "NAMING LOOKUP NAME=ME\n"); err != nil {
		return ""
	}
	resp, err := samReadLine(reader)
	if err != nil {
		return ""
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		return ""
	}
	const key = "VALUE="
	idx := strings.Index(resp, key)
	if idx < 0 {
		return ""
	}
	rest := resp[idx+len(key):]
	if sp := strings.IndexByte(rest, ' '); sp >= 0 {
		return rest[:sp]
	}
	return rest
}
