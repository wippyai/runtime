// SPDX-License-Identifier: MPL-2.0

// Package i2p implements the I2P overlay network driver. It routes
// connections through an I2P SAM v3 bridge using the two-connection
// protocol: a persistent control connection carrying the session, plus a
// per-dial stream connection.
package i2p

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	netapi "github.com/wippyai/runtime/api/net"
	netservice "github.com/wippyai/runtime/service/net"
)

var _ netapi.Service = (*Service)(nil)

// sessionCounter makes session IDs unique across concurrent DialContext
// calls to avoid DUPLICATED_ID errors from the SAM bridge.
var sessionCounter uint64

// streamConn couples a SAM stream connection to its control connection.
// The control connection must stay open for the session lifetime; closing
// the stream also closes the control connection to destroy the session.
//
// Reads go through reader so that any application bytes buffered during the
// newline-terminated SAM handshake (when TCP coalesces the STATUS reply with
// the first payload segment) are still delivered to callers.
type streamConn struct {
	net.Conn
	reader *bufio.Reader
	ctrl   net.Conn
}

func (c *streamConn) Read(p []byte) (int, error) {
	return c.reader.Read(p)
}

func (c *streamConn) Close() error {
	err := c.Conn.Close()
	c.ctrl.Close()
	return err
}

// Service routes connections through an I2P SAM v3 bridge.
type Service struct {
	addr        string
	sessionName string
}

// NewService creates a Service pointing at the SAM bridge described by cfg.
// SessionName is used as the prefix for per-dial session IDs; defaults to
// "wippy" when empty.
func NewService(cfg *netapi.I2PConfig) (*Service, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	sessionName := cfg.SessionName
	if sessionName == "" {
		sessionName = "wippy"
	}
	return &Service{addr: addr, sessionName: sessionName}, nil
}

func (s *Service) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return samDial(ctx, s.addr, s.sessionName, network, address)
}

func (s *Service) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, netservice.NewUnsupportedOperationError("i2p", "UDP not yet implemented")
}

func (s *Service) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, netservice.NewUnsupportedOperationError("i2p", "DNS resolved via SAM at dial time")
}

// samReadLine reads a single newline-terminated response from the SAM bridge.
func samReadLine(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadString('\n')
	if err != nil {
		return "", err
	}
	return strings.TrimRight(line, "\r\n"), nil
}

// samParseResult extracts the RESULT= value from a SAM response line.
// Returns the result value (e.g. "OK", "NOVERSION", "I2P_ERROR") and true,
// or empty string and false if no RESULT= key is found.
func samParseResult(line string) (string, bool) {
	const key = "RESULT="
	idx := strings.Index(line, key)
	if idx < 0 {
		return "", false
	}
	rest := line[idx+len(key):]
	if spIdx := strings.IndexByte(rest, ' '); spIdx >= 0 {
		return rest[:spIdx], true
	}
	return rest, true
}

// samDial connects to the SAM bridge using the SAM v3.1+ two-connection
// protocol:
//
//  1. Control connection: HELLO + SESSION CREATE
//  2. Stream connection: HELLO + STREAM CONNECT
//
// Each call generates a unique session ID to avoid DUPLICATED_ID errors on
// concurrent dials. The returned connection wraps both the stream and
// control connections; closing it destroys the session.
func samDial(ctx context.Context, samAddr, sessionBase, _, address string) (net.Conn, error) {
	id := atomic.AddUint64(&sessionCounter, 1)
	sessionID := fmt.Sprintf("%s-%d", sessionBase, id)

	d := net.Dialer{}

	ctrlConn, err := d.DialContext(ctx, "tcp", samAddr)
	if err != nil {
		return nil, netservice.NewProtocolError("i2p", "SAM bridge connect", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		ctrlConn.SetDeadline(deadline) //nolint:errcheck
	}

	ctrlReader := bufio.NewReader(ctrlConn)

	if _, err := fmt.Fprintf(ctrlConn, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SAM handshake", err)
	}

	resp, err := samReadLine(ctrlReader)
	if err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SAM handshake response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		ctrlConn.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "SAM handshake", resp)
	}

	if _, err := fmt.Fprintf(ctrlConn, "SESSION CREATE STYLE=STREAM ID=%s DESTINATION=TRANSIENT\n", sessionID); err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SESSION CREATE", err)
	}

	resp, err = samReadLine(ctrlReader)
	if err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SESSION CREATE response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		ctrlConn.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "SESSION CREATE", resp)
	}

	ctrlConn.SetDeadline(time.Time{}) //nolint:errcheck

	// Strip port from address — SAM DESTINATION is just the hostname/key.
	dest := address
	if host, port, err := net.SplitHostPort(address); err == nil {
		dest = host
		_ = port // I2P doesn't use TCP ports; could pass as TO_PORT if needed
	}

	streamConnRaw, err := d.DialContext(ctx, "tcp", samAddr)
	if err != nil {
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "SAM bridge stream connect", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		streamConnRaw.SetDeadline(deadline) //nolint:errcheck
	}

	streamReader := bufio.NewReader(streamConnRaw)

	if _, err := fmt.Fprintf(streamConnRaw, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "stream SAM handshake", err)
	}

	resp, err = samReadLine(streamReader)
	if err != nil {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "stream SAM handshake response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "stream SAM handshake", resp)
	}

	if _, err := fmt.Fprintf(streamConnRaw, "STREAM CONNECT ID=%s DESTINATION=%s SILENT=false\n", sessionID, dest); err != nil {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "STREAM CONNECT", err)
	}

	resp, err = samReadLine(streamReader)
	if err != nil {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolError("i2p", "STREAM CONNECT response", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		streamConnRaw.Close()
		ctrlConn.Close()
		return nil, netservice.NewProtocolRejectError("i2p", "STREAM CONNECT", resp)
	}

	streamConnRaw.SetDeadline(time.Time{}) //nolint:errcheck

	return &streamConn{Conn: streamConnRaw, reader: streamReader, ctrl: ctrlConn}, nil
}
