// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"encoding/binary"
	"net/netip"
)

// IPAddress is the canonical WASI network.ip-address variant, without a port.
type IPAddress struct {
	IPv4 *IPv4Address `wit:"ipv4"`
	IPv6 *IPv6Address `wit:"ipv6"`
}

func parseIPAddress(text string) *IPAddress {
	address, err := netip.ParseAddr(text)
	if err != nil || address.Zone() != "" {
		return nil
	}
	address = address.Unmap()
	if address.Is4() {
		bytes := IPv4Address(address.As4())
		return &IPAddress{IPv4: &bytes}
	}
	bytes := address.As16()
	var words IPv6Address
	for i := range words {
		words[i] = binary.BigEndian.Uint16(bytes[2*i:])
	}
	return &IPAddress{IPv6: &words}
}

func (a *IPAddress) String() string {
	if a == nil {
		return ""
	}
	if a.IPv4 != nil && a.IPv6 == nil {
		return netip.AddrFrom4([4]byte(*a.IPv4)).String()
	}
	if a.IPv6 != nil && a.IPv4 == nil {
		var bytes [16]byte
		for i, word := range a.IPv6 {
			binary.BigEndian.PutUint16(bytes[2*i:], word)
		}
		return netip.AddrFrom16(bytes).String()
	}
	return ""
}
