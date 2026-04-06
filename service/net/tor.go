// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	netapi "github.com/wippyai/runtime/api/net"
	"golang.org/x/net/proxy"
)

var _ netapi.Service = (*TorService)(nil)

// TorService routes connections through a Tor SOCKS5 proxy.
// DNS resolution for .onion addresses is handled remotely by the proxy.
type TorService struct {
	dialer         proxy.Dialer
	addr           string
	isolateStreams bool
}

// NewTorService creates a new Tor service connected to a SOCKS5 proxy.
func NewTorService(cfg *netapi.TorConfig) (*TorService, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)
	d, err := proxy.SOCKS5("tcp", addr, nil, proxy.Direct)
	if err != nil {
		return nil, fmt.Errorf("tor: failed to create SOCKS5 dialer: %w", err)
	}
	return &TorService{dialer: d, addr: addr, isolateStreams: cfg.IsolateStreams}, nil
}

func (s *TorService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := s.dialer

	// When stream isolation is enabled, create a per-connection SOCKS5 dialer
	// with random credentials. Tor treats each unique user:pass pair as a
	// separate circuit, providing stream isolation.
	if s.isolateStreams {
		cred, err := randomIsolationCredential()
		if err != nil {
			return nil, fmt.Errorf("tor: stream isolation credential: %w", err)
		}
		d, err := proxy.SOCKS5("tcp", s.addr, cred, proxy.Direct)
		if err != nil {
			return nil, fmt.Errorf("tor: failed to create isolated SOCKS5 dialer: %w", err)
		}
		dialer = d
	}

	if cd, ok := dialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, address)
	}
	return dialer.Dial(network, address)
}

func (s *TorService) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, fmt.Errorf("tor: %w (use hidden services via control port)", netapi.ErrNotSupported)
}

func (s *TorService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("tor: %w (UDP not available)", netapi.ErrNotSupported)
}

func (s *TorService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("tor: %w (DNS resolved remotely at dial time)", netapi.ErrNotSupported)
}

// randomIsolationCredential generates random SOCKS5 auth credentials
// for Tor stream isolation. Each unique user:pass pair gets a separate circuit.
func randomIsolationCredential() (*proxy.Auth, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	user := hex.EncodeToString(b[:])
	return &proxy.Auth{User: user, Password: user}, nil
}
