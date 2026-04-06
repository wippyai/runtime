// SPDX-License-Identifier: MPL-2.0

package net

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"strings"
	"sync/atomic"
	"time"

	netapi "github.com/wippyai/runtime/api/net"
)

var _ netapi.Service = (*I2PService)(nil)

// samSessionCounter generates unique session IDs across concurrent DialContext calls.
var samSessionCounter uint64

// samStreamConn wraps a SAM stream connection with its associated control connection.
// The control connection keeps the SAM session alive. Closing the stream also
// closes the control connection, destroying the session.
type samStreamConn struct {
	net.Conn
	ctrl net.Conn
}

func (c *samStreamConn) Close() error {
	err := c.Conn.Close()
	c.ctrl.Close()
	return err
}

// I2PService routes connections through an I2P SAM v3 bridge.
type I2PService struct {
	addr        string
	sessionName string
}

// NewI2PService creates a new I2P service connected to a SAM bridge.
func NewI2PService(cfg *netapi.I2PConfig) (*I2PService, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	sessionName := cfg.SessionName
	if sessionName == "" {
		sessionName = "wippy"
	}
	return &I2PService{addr: addr, sessionName: sessionName}, nil
}

func (s *I2PService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return samDial(ctx, s.addr, s.sessionName, network, address)
}

func (s *I2PService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("i2p: %w (listener not yet implemented)", netapi.ErrNotSupported)
}

func (s *I2PService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("i2p: %w (UDP not yet implemented)", netapi.ErrNotSupported)
}

func (s *I2PService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("i2p: %w (DNS resolved via SAM at dial time)", netapi.ErrNotSupported)
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
	// The value runs until the next space or end of line.
	if spIdx := strings.IndexByte(rest, ' '); spIdx >= 0 {
		return rest[:spIdx], true
	}
	return rest, true
}

// samDial connects to the SAM bridge using the SAM v3.1+ two-connection protocol:
//
//  1. Control connection: HELLO + SESSION CREATE (creates the session)
//  2. Stream connection: HELLO + STREAM CONNECT (opens a data stream)
//
// Each call generates a unique session ID to avoid DUPLICATED_ID errors on
// concurrent dials. The returned connection wraps both the stream and control
// connections; closing it destroys the session.
func samDial(ctx context.Context, samAddr, sessionBase, _ /* network */, address string) (net.Conn, error) {
	// Unique session ID per dial to avoid DUPLICATED_ID on concurrent calls.
	id := atomic.AddUint64(&samSessionCounter, 1)
	sessionID := fmt.Sprintf("%s-%d", sessionBase, id)

	d := net.Dialer{}

	// --- Control connection: HELLO + SESSION CREATE ---
	ctrlConn, err := d.DialContext(ctx, "tcp", samAddr)
	if err != nil {
		return nil, fmt.Errorf("i2p: failed to connect to SAM bridge at %s: %w", samAddr, err)
	}

	// Propagate context deadline to the connection for the setup phase.
	if deadline, ok := ctx.Deadline(); ok {
		ctrlConn.SetDeadline(deadline) //nolint:errcheck
	}

	ctrlReader := bufio.NewReader(ctrlConn)

	if _, err := fmt.Fprintf(ctrlConn, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: SAM handshake failed: %w", err)
	}

	resp, err := samReadLine(ctrlReader)
	if err != nil {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: SAM handshake response failed: %w", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: unexpected SAM handshake response: %s", resp)
	}

	if _, err := fmt.Fprintf(ctrlConn, "SESSION CREATE STYLE=STREAM ID=%s DESTINATION=TRANSIENT\n", sessionID); err != nil {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: SESSION CREATE failed: %w", err)
	}

	resp, err = samReadLine(ctrlReader)
	if err != nil {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: SESSION CREATE response failed: %w", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: SESSION CREATE rejected: %s", resp)
	}

	// Clear deadline — the control connection stays alive for the session lifetime.
	ctrlConn.SetDeadline(time.Time{}) //nolint:errcheck

	// Strip port from address — SAM DESTINATION is just the hostname/key.
	// I2P destinations don't use TCP ports; the optional TO_PORT parameter
	// can convey a virtual port if needed.
	dest := address
	if host, port, err := net.SplitHostPort(address); err == nil {
		dest = host
		_ = port // I2P doesn't use TCP ports; could pass as TO_PORT if needed
	}

	// --- Stream connection: HELLO + STREAM CONNECT ---
	streamConn, err := d.DialContext(ctx, "tcp", samAddr)
	if err != nil {
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: failed to open stream connection to SAM bridge: %w", err)
	}

	if deadline, ok := ctx.Deadline(); ok {
		streamConn.SetDeadline(deadline) //nolint:errcheck
	}

	streamReader := bufio.NewReader(streamConn)

	if _, err := fmt.Fprintf(streamConn, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: stream SAM handshake failed: %w", err)
	}

	resp, err = samReadLine(streamReader)
	if err != nil {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: stream SAM handshake response failed: %w", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: stream unexpected SAM handshake response: %s", resp)
	}

	if _, err := fmt.Fprintf(streamConn, "STREAM CONNECT ID=%s DESTINATION=%s SILENT=false\n", sessionID, dest); err != nil {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT failed: %w", err)
	}

	resp, err = samReadLine(streamReader)
	if err != nil {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT response failed: %w", err)
	}
	if result, ok := samParseResult(resp); !ok || result != "OK" {
		streamConn.Close()
		ctrlConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT rejected: %s", resp)
	}

	// Clear deadline — data transfer should not be limited by the setup timeout.
	streamConn.SetDeadline(time.Time{}) //nolint:errcheck

	return &samStreamConn{Conn: streamConn, ctrl: ctrlConn}, nil
}
