// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"fmt"
	"net"

	netapi "github.com/wippyai/runtime/api/net"
)

var _ netapi.Service = (*I2PService)(nil)

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

// samDial connects to the SAM bridge and performs a STREAM CONNECT.
// This is a minimal SAM v3 client implementation.
func samDial(ctx context.Context, samAddr, sessionName, _ /* network */, address string) (net.Conn, error) {
	d := net.Dialer{}
	samConn, err := d.DialContext(ctx, "tcp", samAddr)
	if err != nil {
		return nil, fmt.Errorf("i2p: failed to connect to SAM bridge at %s: %w", samAddr, err)
	}

	// SAM v3 handshake
	if _, err := fmt.Fprintf(samConn, "HELLO VERSION MIN=3.0 MAX=3.3\n"); err != nil {
		samConn.Close()
		return nil, fmt.Errorf("i2p: SAM handshake failed: %w", err)
	}

	buf := make([]byte, 1024)
	n, err := samConn.Read(buf)
	if err != nil {
		samConn.Close()
		return nil, fmt.Errorf("i2p: SAM handshake response failed: %w", err)
	}
	resp := string(buf[:n])
	if len(resp) < 13 || resp[:13] != "HELLO REPLY R" {
		samConn.Close()
		return nil, fmt.Errorf("i2p: unexpected SAM response: %s", resp)
	}

	// STREAM CONNECT to target
	if _, err := fmt.Fprintf(samConn, "STREAM CONNECT ID=%s DESTINATION=%s SILENT=false\n", sessionName, address); err != nil {
		samConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT failed: %w", err)
	}

	n, err = samConn.Read(buf)
	if err != nil {
		samConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT response failed: %w", err)
	}
	resp = string(buf[:n])
	if len(resp) < 22 || resp[:22] != "STREAM STATUS RESULT=O" {
		samConn.Close()
		return nil, fmt.Errorf("i2p: STREAM CONNECT rejected: %s", resp)
	}

	return samConn, nil
}
