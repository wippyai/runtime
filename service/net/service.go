package net

import (
	"context"
	"errors"
	"net"

	"github.com/wippyai/runtime/runtime/security"
)

// ErrAccessDenied is returned when a network operation is blocked by security policy.
var ErrAccessDenied = errors.New("network access denied")

func isPrivateIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

func checkPrivateIP(ctx context.Context, address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		host = address
	}

	if ip := net.ParseIP(host); ip != nil {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "socket.private_ip", host, nil) {
				return ErrAccessDenied
			}
		}
		return nil
	}

	ips, err := net.DefaultResolver.LookupIP(ctx, "ip", host)
	if err != nil {
		return nil
	}

	for _, ip := range ips {
		if isPrivateIP(ip) {
			if !security.IsAllowed(ctx, "socket.private_ip", ip.String(), nil) {
				return ErrAccessDenied
			}
		}
	}

	return nil
}

// SecureService enforces security checks before delegating to standard net operations.
type SecureService struct{}

// NewSecureService creates a SecureService.
func NewSecureService() *SecureService {
	return &SecureService{}
}

func (s *SecureService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	if !security.IsAllowed(ctx, "socket.connect", address, nil) {
		return nil, ErrAccessDenied
	}
	if err := checkPrivateIP(ctx, address); err != nil {
		return nil, err
	}
	d := net.Dialer{}
	return d.DialContext(ctx, network, address)
}

func (s *SecureService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	if !security.IsAllowed(ctx, "socket.listen", address, nil) {
		return nil, ErrAccessDenied
	}
	lc := net.ListenConfig{}
	return lc.Listen(ctx, network, address)
}

func (s *SecureService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	if !security.IsAllowed(ctx, "socket.listen", address, nil) {
		return nil, ErrAccessDenied
	}
	lc := net.ListenConfig{}
	return lc.ListenPacket(ctx, network, address)
}

func (s *SecureService) LookupHost(ctx context.Context, host string) ([]string, error) {
	if !security.IsAllowed(ctx, "socket.resolve", host, nil) {
		return nil, ErrAccessDenied
	}
	r := net.Resolver{}
	return r.LookupHost(ctx, host)
}
