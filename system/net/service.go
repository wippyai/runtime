// SPDX-License-Identifier: MPL-2.0

package net

import (
	"context"
	"errors"
	"net"
	"net/netip"
	"strings"
	"syscall"

	netapi "github.com/wippyai/runtime/api/net"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/runtime/security"
)

func isIPNetwork(network string) bool {
	return strings.HasPrefix(network, "tcp") ||
		strings.HasPrefix(network, "udp") ||
		strings.HasPrefix(network, "ip")
}

func isPrivateIP(addr netip.Addr) bool {
	if !addr.IsValid() {
		return false
	}
	addr = addr.Unmap()
	return addr.IsLoopback() || addr.IsPrivate() || addr.IsLinkLocalUnicast() ||
		addr.IsLinkLocalMulticast() || addr.IsUnspecified()
}

func checkPrivateIPAddress(ctx context.Context, addr netip.Addr) error {
	addr = addr.Unmap()
	if isPrivateIP(addr) && !security.IsAllowed(ctx, "socket.private_ip", addr.String(), nil) {
		return netapi.ErrAccessDenied
	}
	return nil
}

func checkPrivateLiteral(ctx context.Context, address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil || port == "" {
		if err == nil {
			err = errors.New("missing port in address")
		}
		return NewInvalidAddressError(address, err)
	}
	if addr, err := netip.ParseAddr(host); err == nil {
		return checkPrivateIPAddress(ctx, addr)
	}
	return nil
}

func controlHook(ctx context.Context, network, address string, _ syscall.RawConn) error {
	if !isIPNetwork(network) {
		return nil
	}
	ap, err := netip.ParseAddrPort(address)
	if err != nil {
		if strings.HasPrefix(network, "ip") {
			if addr, aErr := netip.ParseAddr(address); aErr == nil {
				return checkPrivateIPAddress(ctx, addr)
			}
		}
		return NewInvalidAddressError(address, err)
	}
	return checkPrivateIPAddress(ctx, ap.Addr())
}

// SecureService enforces security checks before delegating to standard net operations.
type SecureService struct{}

// NewSecureService creates a SecureService.
func NewSecureService() *SecureService {
	return &SecureService{}
}

func (s *SecureService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !security.IsAllowed(ctx, "socket.connect", address, nil) {
		return nil, netapi.ErrAccessDenied
	}
	if networkID := netapi.GetDefaultNetwork(ctx); networkID != "" {
		if err := checkPrivateLiteral(ctx, address); err != nil {
			return nil, err
		}
		reg := netapi.GetNetworkRegistry(ctx)
		if reg == nil {
			return nil, NewNetworkRegistryMissingError(networkID)
		}
		svc, err := reg.GetNetwork(registry.ParseID(networkID))
		if err != nil {
			return nil, NewNetworkLookupError(networkID, err)
		}
		return svc.DialContext(ctx, network, address)
	}
	d := net.Dialer{
		ControlContext: controlHook,
	}
	return d.DialContext(ctx, network, address)
}

func (s *SecureService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if !security.IsAllowed(ctx, "socket.listen", address, nil) {
		return nil, netapi.ErrAccessDenied
	}
	lc := net.ListenConfig{}
	return lc.Listen(ctx, network, address)
}

func (s *SecureService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	if !security.IsAllowed(ctx, "socket.listen", address, nil) {
		return nil, netapi.ErrAccessDenied
	}
	if networkID := netapi.GetDefaultNetwork(ctx); networkID != "" {
		reg := netapi.GetNetworkRegistry(ctx)
		if reg == nil {
			return nil, NewNetworkRegistryMissingError(networkID)
		}
		svc, err := reg.GetNetwork(registry.ParseID(networkID))
		if err != nil {
			return nil, NewNetworkLookupError(networkID, err)
		}
		return svc.ListenPacket(ctx, network, address)
	}
	lc := net.ListenConfig{}
	return lc.ListenPacket(ctx, network, address)
}

func (s *SecureService) LookupHost(ctx context.Context, host string) ([]string, error) {
	if !security.IsAllowed(ctx, "socket.resolve", host, nil) {
		return nil, netapi.ErrAccessDenied
	}
	if networkID := netapi.GetDefaultNetwork(ctx); networkID != "" {
		reg := netapi.GetNetworkRegistry(ctx)
		if reg == nil {
			return nil, NewNetworkRegistryMissingError(networkID)
		}
		svc, err := reg.GetNetwork(registry.ParseID(networkID))
		if err != nil {
			return nil, NewNetworkLookupError(networkID, err)
		}
		return svc.LookupHost(ctx, host)
	}
	r := net.Resolver{}
	return r.LookupHost(ctx, host)
}
