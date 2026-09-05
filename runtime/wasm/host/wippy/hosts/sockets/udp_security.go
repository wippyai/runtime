// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"net"

	"github.com/wippyai/runtime/runtime/security"
)

func isPrivateUDPIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	return ip.IsLoopback() || ip.IsPrivate() || ip.IsLinkLocalUnicast() ||
		ip.IsLinkLocalMulticast() || ip.IsUnspecified()
}

// authorizeUDPDestination checks socket.connect on the destination and
// socket.private_ip on IP.String() for loopback, private, link-local, and
// unspecified literals. The address is an already-validated IPSocketAddress
// literal; authorization uses the IP bytes, including IPv6-mapped IPv4.
func authorizeUDPDestination(ctx context.Context, address *IPSocketAddress) *NetworkError {
	if address == nil {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if !security.IsAllowed(ctx, "socket.connect", address.String(), nil) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}
	ip := address.IP()
	if isPrivateUDPIP(ip) && !security.IsAllowed(ctx, "socket.private_ip", ip.String(), nil) {
		return &NetworkError{Code: NetworkErrorAccessDenied}
	}
	return nil
}
