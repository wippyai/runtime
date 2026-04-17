// SPDX-License-Identifier: MPL-2.0

// Package socks5 implements the SOCKS5 overlay network driver. It handles
// plain SOCKS5 proxies as well as Tor's SOCKS5 listener; with
// IsolateStreams enabled the dialer picks fresh random credentials per
// connection so Tor allocates a separate circuit each dial.
package socks5

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"net"

	netapi "github.com/wippyai/runtime/api/net"
	netservice "github.com/wippyai/runtime/service/net"
	"golang.org/x/net/proxy"
)

var _ netapi.Service = (*Service)(nil)

// Service routes connections through a SOCKS5 proxy. Remote hostnames are
// resolved by the proxy (SOCKS5 ATYP=DOMAINNAME).
type Service struct {
	baseAuth       *proxy.Auth
	dialer         proxy.Dialer
	addr           string
	isolateStreams bool
}

// NewService creates a Service bound to the configured proxy.
func NewService(cfg *netapi.SOCKS5Config) (*Service, error) {
	addr := fmt.Sprintf("%s:%d", cfg.Host, cfg.Port)

	var auth *proxy.Auth
	if cfg.Username != "" || cfg.Password != "" {
		auth = &proxy.Auth{User: cfg.Username, Password: cfg.Password}
	}

	d, err := proxy.SOCKS5("tcp", addr, auth, proxy.Direct)
	if err != nil {
		return nil, netservice.NewDialerCreateError("socks5", err)
	}

	return &Service{
		baseAuth:       auth,
		dialer:         d,
		addr:           addr,
		isolateStreams: cfg.IsolateStreams,
	}, nil
}

func (s *Service) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	dialer := s.dialer

	if s.isolateStreams {
		cred, err := randomIsolationCredential()
		if err != nil {
			return nil, netservice.NewIsolationCredentialError(err)
		}
		d, err := proxy.SOCKS5("tcp", s.addr, cred, proxy.Direct)
		if err != nil {
			return nil, netservice.NewDialerCreateError("socks5", err)
		}
		dialer = d
	}

	if cd, ok := dialer.(proxy.ContextDialer); ok {
		return cd.DialContext(ctx, network, address)
	}
	return dialer.Dial(network, address)
}

func (s *Service) Listen(_ context.Context, _, _ string) (net.Listener, error) {
	return nil, netservice.NewUnsupportedOperationError("socks5", "inbound listeners are not exposed over SOCKS5")
}

func (s *Service) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, netservice.NewUnsupportedOperationError("socks5", "UDP associate not implemented")
}

func (s *Service) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, netservice.NewUnsupportedOperationError("socks5", "DNS resolved by proxy at dial time")
}

// randomIsolationCredential generates random SOCKS5 auth credentials for
// Tor stream isolation. Tor maps each unique user:pass pair to a separate
// circuit.
func randomIsolationCredential() (*proxy.Auth, error) {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return nil, err
	}
	user := hex.EncodeToString(b[:])
	return &proxy.Auth{User: user, Password: user}, nil
}
