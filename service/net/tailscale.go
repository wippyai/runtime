// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"fmt"
	"net"
	"os"

	netapi "github.com/wippyai/runtime/api/net"
	"tailscale.com/tsnet"
)

var _ netapi.Service = (*TailscaleService)(nil)

// tsnetNode abstracts the tsnet.Server methods used by TailscaleService.
// This enables unit testing with a mock implementation.
type tsnetNode interface {
	Dial(ctx context.Context, network, address string) (net.Conn, error)
	Listen(network, address string) (net.Listener, error)
	Close() error
}

// TailscaleService routes connections through a Tailscale tsnet userspace node.
// It supports both outbound dialing and inbound listening.
type TailscaleService struct {
	node tsnetNode
}

// NewTailscaleService creates and starts a new Tailscale tsnet node.
func NewTailscaleService(cfg *netapi.TailscaleConfig) (*TailscaleService, error) {
	hostname := cfg.Hostname
	if hostname == "" {
		hostname = "wippy"
	}

	authKey := cfg.AuthKey
	if authKey == "" && cfg.AuthKeyEnv != "" {
		authKey = os.Getenv(cfg.AuthKeyEnv)
	}

	srv := &tsnet.Server{
		Hostname:  hostname,
		Ephemeral: cfg.Ephemeral,
	}

	if authKey != "" {
		srv.AuthKey = authKey
	}
	if cfg.StateDir != "" {
		srv.Dir = cfg.StateDir
	}
	if cfg.ControlURL != "" {
		srv.ControlURL = cfg.ControlURL
	}

	if err := srv.Start(); err != nil {
		return nil, fmt.Errorf("tailscale: failed to start tsnet node: %w", err)
	}

	return &TailscaleService{node: srv}, nil
}

// newTailscaleServiceWithNode creates a TailscaleService with an injected node
// implementation. This is used for testing with mock tsnet nodes.
func newTailscaleServiceWithNode(node tsnetNode) *TailscaleService {
	return &TailscaleService{node: node}
}

func (s *TailscaleService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return s.node.Dial(ctx, network, address)
}

func (s *TailscaleService) Listen(_ context.Context, network, address string) (net.Listener, error) {
	return s.node.Listen(network, address)
}

func (s *TailscaleService) ListenPacket(_ context.Context, _, _ string) (net.PacketConn, error) {
	return nil, fmt.Errorf("tailscale: %w (use Listen for TCP)", netapi.ErrNotSupported)
}

func (s *TailscaleService) LookupHost(_ context.Context, _ string) ([]string, error) {
	return nil, fmt.Errorf("tailscale: %w (DNS resolved by tailnet)", netapi.ErrNotSupported)
}

// Close shuts down the Tailscale node.
func (s *TailscaleService) Close() error {
	return s.node.Close()
}
