// SPDX-License-Identifier: MPL-2.0

// Package tailscale implements the Tailscale overlay network driver. It
// runs a userspace tsnet node per registry entry and routes DialContext
// / Listen through the tailnet. Authentication uses a raw auth key; the
// enclosing Manager resolves env-var indirection before Create.
package tailscale

import (
	"context"
	"net"

	netapi "github.com/wippyai/runtime/api/net"
	netservice "github.com/wippyai/runtime/service/net"
	"tailscale.com/tsnet"
)

var _ netapi.Service = (*Service)(nil)

// tsnetNode abstracts the tsnet.Server methods used by Service so tests can
// inject a mock node without spinning up a real tsnet.Server.
type tsnetNode interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
	Listen(network, address string) (net.Listener, error)
	ListenTLS(network, address string) (net.Listener, error)
	Close() error
}

// Service routes connections through a Tailscale tsnet userspace node. It
// supports both outbound dialing and inbound listening on the tailnet.
type Service struct {
	node tsnetNode
}

// NewService creates and starts a new Tailscale tsnet node. The caller is
// responsible for populating cfg.AuthKey — upstream code resolves indirect
// references like AuthKeyEnv before reaching here.
func NewService(cfg *netapi.TailscaleConfig) (*Service, error) {
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "wippy"
	}

	srv := &tsnet.Server{
		Hostname:  hostname,
		Ephemeral: cfg.Ephemeral,
	}

	if cfg.AuthKey != "" {
		srv.AuthKey = cfg.AuthKey
	}
	if cfg.StateDir != "" {
		srv.Dir = cfg.StateDir
	}
	if cfg.ControlURL != "" {
		srv.ControlURL = cfg.ControlURL
	}

	if err := srv.Start(); err != nil {
		return nil, netservice.NewServiceStartError("tailscale", err)
	}

	return &Service{node: srv}, nil
}

// newServiceWithNode is a test constructor that skips tsnet.Server startup
// and wires the Service to an injected node implementation.
func newServiceWithNode(node tsnetNode) *Service {
	return &Service{node: node}
}

func (s *Service) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return s.node.Dial(ctx, network, address)
}

func (s *Service) Listen(_ context.Context, network, address string) (net.Listener, error) {
	return s.node.Listen(network, address)
}

// ListenTLS announces on the tailnet with tsnet-managed TLS, using a cert
// issued for the tsnet node's MagicDNS name.
func (s *Service) ListenTLS(_ context.Context, network, address string) (net.Listener, error) {
	return s.node.ListenTLS(network, address)
}

var _ netapi.TLSListener = (*Service)(nil)

func (s *Service) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, netservice.NewUnsupportedOperationError("tailscale", "use Listen for TCP")
}

func (s *Service) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, netservice.NewUnsupportedOperationError("tailscale", "DNS resolved by tailnet")
}

// Close shuts down the Tailscale node.
func (s *Service) Close() error {
	return s.node.Close()
}
