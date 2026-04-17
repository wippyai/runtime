// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"net"
)

// Dialer abstracts outbound TCP/UDP connections with security enforcement.
type Dialer interface {
	DialContext(ctx context.Context, network, address string) (net.Conn, error)
}

// Listener abstracts binding to network addresses with security enforcement.
type Listener interface {
	Listen(ctx context.Context, network, address string) (net.Listener, error)
}

// TLSListener is implemented by overlay drivers that can terminate TLS
// using a driver-managed certificate (e.g. tsnet's Tailscale-issued
// LetsEncrypt cert). Drivers that cannot should not implement this;
// consumers must type-assert and fall back to plain Listen wrapped with
// the caller's own tls.Config when auto-TLS is unavailable.
type TLSListener interface {
	ListenTLS(ctx context.Context, network, address string) (net.Listener, error)
}

// PacketListener abstracts binding to packet-oriented network addresses with security enforcement.
type PacketListener interface {
	ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error)
}

// Resolver abstracts DNS resolution with security enforcement.
type Resolver interface {
	LookupHost(ctx context.Context, host string) ([]string, error)
}

// Service combines all network operations behind a security layer.
type Service interface {
	Dialer
	Listener
	PacketListener
	Resolver
}
