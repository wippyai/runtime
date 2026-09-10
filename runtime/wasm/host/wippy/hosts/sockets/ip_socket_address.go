// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"encoding/binary"
	"net"
	"net/netip"
	"strconv"
	"strings"
)

// IPv4Address is a 4-byte tuple representing an IPv4 address (tuple<u8, u8, u8, u8>).
type IPv4Address [4]uint8

// IPv6Address is an 8-word tuple representing an IPv6 address (tuple<u16, u16, u16, u16, u16, u16, u16, u16>).
type IPv6Address [8]uint16

// IPv4SocketAddress represents the WASI 0.2.8 network ipv4-socket-address record.
type IPv4SocketAddress struct {
	Port    uint16      `wit:"port"`
	Address IPv4Address `wit:"address"`
}

// IPv6SocketAddress represents the WASI 0.2.8 network ipv6-socket-address record.
type IPv6SocketAddress struct {
	Port     uint16      `wit:"port"`
	FlowInfo uint32      `wit:"flow-info"`
	Address  IPv6Address `wit:"address"`
	ScopeID  uint32      `wit:"scope-id"`
}

// IPSocketAddress represents the WASI 0.2.8 network ip-socket-address variant.
// Exactly one pointer case field is non-nil for the active variant case.
type IPSocketAddress struct {
	IPv4 *IPv4SocketAddress `wit:"ipv4"`
	IPv6 *IPv6SocketAddress `wit:"ipv6"`
}

// ScopeIDFromZone resolves a zone string into a numeric IPv6 scope ID.
// Numeric scope IDs (e.g. "1", "42") are parsed directly, while named OS
// interfaces (e.g. "eth0", "lo") are mapped using their OS interface index.
func ScopeIDFromZone(zone string) uint32 {
	if zone == "" {
		return 0
	}
	if n, err := strconv.ParseUint(zone, 10, 32); err == nil {
		return uint32(n)
	}
	if ifi, err := net.InterfaceByName(zone); err == nil && ifi != nil {
		return uint32(ifi.Index)
	}
	return 0
}

// ZoneFromScopeID preserves the numeric scope without querying OS interfaces
// on the guest worker. Go networking accepts numeric IPv6 zones.
func ZoneFromScopeID(scopeID uint32) string {
	if scopeID == 0 {
		return ""
	}
	return strconv.FormatUint(uint64(scopeID), 10)
}

// String returns host:port formatted string (e.g. "127.0.0.1:8080" or "[::1]:8080" or "[fe80::1%lo]:8080").
func (a *IPSocketAddress) String() string {
	if a == nil {
		return ""
	}
	if a.IPv4 != nil {
		ip := net.IPv4(a.IPv4.Address[0], a.IPv4.Address[1], a.IPv4.Address[2], a.IPv4.Address[3])
		return net.JoinHostPort(ip.String(), strconv.Itoa(int(a.IPv4.Port)))
	}
	if a.IPv6 != nil {
		ip := make(net.IP, 16)
		for i := 0; i < 8; i++ {
			binary.BigEndian.PutUint16(ip[i*2:], a.IPv6.Address[i])
		}
		host := ip.String()
		if a.IPv6.ScopeID != 0 {
			if zone := ZoneFromScopeID(a.IPv6.ScopeID); zone != "" {
				host = host + "%" + zone
			}
		}
		return net.JoinHostPort(host, strconv.Itoa(int(a.IPv6.Port)))
	}
	return ""
}

// IP returns the net.IP of the socket address.
func (a *IPSocketAddress) IP() net.IP {
	if a == nil {
		return nil
	}
	if a.IPv4 != nil {
		return net.IPv4(a.IPv4.Address[0], a.IPv4.Address[1], a.IPv4.Address[2], a.IPv4.Address[3])
	}
	if a.IPv6 != nil {
		ip := make(net.IP, 16)
		for i := 0; i < 8; i++ {
			binary.BigEndian.PutUint16(ip[i*2:], a.IPv6.Address[i])
		}
		return ip
	}
	return nil
}

// IPString returns the IP as a string without the port (including zone if present).
func (a *IPSocketAddress) IPString() string {
	if a == nil {
		return ""
	}
	if a.IPv4 != nil {
		ip := net.IPv4(a.IPv4.Address[0], a.IPv4.Address[1], a.IPv4.Address[2], a.IPv4.Address[3])
		return ip.String()
	}
	if a.IPv6 != nil {
		ip := make(net.IP, 16)
		for i := 0; i < 8; i++ {
			binary.BigEndian.PutUint16(ip[i*2:], a.IPv6.Address[i])
		}
		host := ip.String()
		if a.IPv6.ScopeID != 0 {
			if zone := ZoneFromScopeID(a.IPv6.ScopeID); zone != "" {
				host = host + "%" + zone
			}
		}
		return host
	}
	return ""
}

// Port returns the port of the socket address.
func (a *IPSocketAddress) Port() uint16 {
	if a == nil {
		return 0
	}
	if a.IPv4 != nil {
		return a.IPv4.Port
	}
	if a.IPv6 != nil {
		return a.IPv6.Port
	}
	return 0
}

// Equal returns whether two IPSocketAddress values represent the same address.
func (a *IPSocketAddress) Equal(b *IPSocketAddress) bool {
	if a == nil || b == nil {
		return a == b
	}
	if a.IPv4 != nil && b.IPv4 != nil {
		return *a.IPv4 == *b.IPv4
	}
	if a.IPv6 != nil && b.IPv6 != nil {
		return *a.IPv6 == *b.IPv6
	}
	return false
}

// NewIPv4SocketAddress creates an IPv4 IPSocketAddress from a 4-byte array and port.
func NewIPv4SocketAddress(addr [4]uint8, port uint16) *IPSocketAddress {
	return &IPSocketAddress{
		IPv4: &IPv4SocketAddress{
			Port:    port,
			Address: addr,
		},
	}
}

// NewIPv6SocketAddress creates an IPv6 IPSocketAddress from an 8-uint16 array, port, flow-info, and scope-id.
func NewIPv6SocketAddress(addr [8]uint16, port uint16, flowInfo, scopeID uint32) *IPSocketAddress {
	return &IPSocketAddress{
		IPv6: &IPv6SocketAddress{
			Port:     port,
			FlowInfo: flowInfo,
			Address:  addr,
			ScopeID:  scopeID,
		},
	}
}

func socketAddressFromIPAndZone(ip net.IP, port uint16, zone string) *IPSocketAddress {
	if ip == nil {
		return nil
	}
	if zone == "" {
		if ip4 := ip.To4(); ip4 != nil {
			return &IPSocketAddress{
				IPv4: &IPv4SocketAddress{
					Port:    port,
					Address: IPv4Address{ip4[0], ip4[1], ip4[2], ip4[3]},
				},
			}
		}
	}
	if ip16 := ip.To16(); ip16 != nil {
		var addr IPv6Address
		for i := 0; i < 8; i++ {
			addr[i] = binary.BigEndian.Uint16(ip16[i*2:])
		}
		return &IPSocketAddress{
			IPv6: &IPv6SocketAddress{
				Port:    port,
				Address: addr,
				ScopeID: ScopeIDFromZone(zone),
			},
		}
	}
	return nil
}

// SocketAddressFromIP converts a net.IP and port into a canonical IPSocketAddress.
func SocketAddressFromIP(ip net.IP, port uint16) *IPSocketAddress {
	return socketAddressFromIPAndZone(ip, port, "")
}

// SocketAddressFromHostPort parses a host (IP address string) and port into a canonical IPSocketAddress.
func SocketAddressFromHostPort(host string, port uint16) *IPSocketAddress {
	host = strings.Trim(host, "[]")
	if host == "" {
		return nil
	}
	addr, err := netip.ParseAddr(host)
	if err != nil {
		return nil
	}
	if addr.Is4() {
		b := addr.As4()
		return &IPSocketAddress{
			IPv4: &IPv4SocketAddress{
				Port:    port,
				Address: IPv4Address{b[0], b[1], b[2], b[3]},
			},
		}
	}
	if addr.Is6() {
		b := addr.As16()
		var rawAddr IPv6Address
		for i := 0; i < 8; i++ {
			rawAddr[i] = binary.BigEndian.Uint16(b[i*2:])
		}
		return &IPSocketAddress{
			IPv6: &IPv6SocketAddress{
				Port:    port,
				Address: rawAddr,
				ScopeID: ScopeIDFromZone(addr.Zone()),
			},
		}
	}
	return nil
}

// SocketAddressFromNetAddr converts net.Addr to *IPSocketAddress preserving IPv6 zone information.
func SocketAddressFromNetAddr(addr net.Addr) *IPSocketAddress {
	if addr == nil {
		return nil
	}
	switch a := addr.(type) {
	case *net.TCPAddr:
		return socketAddressFromIPAndZone(a.IP, uint16(a.Port), a.Zone)
	case *net.UDPAddr:
		return socketAddressFromIPAndZone(a.IP, uint16(a.Port), a.Zone)
	default:
		host, portStr, err := net.SplitHostPort(addr.String())
		if err != nil {
			return nil
		}
		port, err := strconv.ParseUint(portStr, 10, 16)
		if err != nil {
			return nil
		}
		return SocketAddressFromHostPort(host, uint16(port))
	}
}

// ValidateAddressFamily verifies that the IPSocketAddress variant matches the socket address family.
func ValidateAddressFamily(addr *IPSocketAddress, family uint8) *NetworkError {
	if addr == nil {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if addr.IPv4 != nil && addr.IPv6 != nil {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	if addr.IPv4 == nil && addr.IPv6 == nil {
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	switch family {
	case AddressFamilyIPv4:
		if addr.IPv4 == nil {
			return &NetworkError{Code: NetworkErrorInvalidArgument}
		}
	case AddressFamilyIPv6:
		if addr.IPv6 == nil {
			return &NetworkError{Code: NetworkErrorInvalidArgument}
		}
	default:
		return &NetworkError{Code: NetworkErrorInvalidArgument}
	}
	return nil
}

// ValidateFlowInfo rejects unsupported non-zero flow-info for IPv6 addresses.
func ValidateFlowInfo(addr *IPSocketAddress) *NetworkError {
	if addr != nil && addr.IPv6 != nil && addr.IPv6.FlowInfo != 0 {
		return &NetworkError{Code: NetworkErrorNotSupported}
	}
	return nil
}
