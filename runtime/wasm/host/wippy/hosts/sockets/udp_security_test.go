// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/attrs"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	secapi "github.com/wippyai/runtime/api/security"
	secsystem "github.com/wippyai/runtime/system/security"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.uber.org/zap"
)

type udpPolicy struct {
	allowed map[string]bool
	id      registry.ID
	seen    [][2]string
}

func newUDPPolicy() *udpPolicy {
	return &udpPolicy{
		id:      registry.NewID("test", "udp"),
		allowed: make(map[string]bool),
	}
}

func (p *udpPolicy) ID() registry.ID { return p.id }

func (p *udpPolicy) Evaluate(_ secapi.Actor, action, resource string, _ attrs.Bag) secapi.Result {
	p.seen = append(p.seen, [2]string{action, resource})
	if p.allowed[action+":"+resource] {
		return secapi.Allow
	}
	return secapi.Deny
}

func (p *udpPolicy) allow(action, resource string) {
	p.allowed[action+":"+resource] = true
}

func (p *udpPolicy) allowConnect(addr *IPSocketAddress) {
	p.allow("socket.connect", addr.String())
}

func (p *udpPolicy) allowPrivateIP(addr *IPSocketAddress) {
	p.allow("socket.private_ip", addr.IP().String())
}

func (p *udpPolicy) saw(action, resource string) bool {
	for _, pair := range p.seen {
		if pair[0] == action && pair[1] == resource {
			return true
		}
	}
	return false
}

func udpActorScope(t *testing.T, policy *udpPolicy) context.Context {
	t.Helper()
	ctx := ctxapi.NewRootContext()
	ctx = logs.WithLogger(ctx, zap.NewNop())
	ctx, frame := ctxapi.OpenFrameContext(ctx)
	t.Cleanup(func() { ctxapi.ReleaseFrameContext(frame) })
	require.NoError(t, secapi.SetActor(ctx, secapi.Actor{ID: "udp-test"}))
	require.NoError(t, secapi.SetScope(ctx, secsystem.NewScope([]secapi.Policy{policy})))
	return ctx
}

func mustAddr(t *testing.T, host string, port uint16) *IPSocketAddress {
	t.Helper()
	addr := SocketAddressFromHostPort(host, port)
	require.NotNil(t, addr)
	return addr
}

func TestAuthorizeUDPDestination_ConnectWithoutPrivateIP(t *testing.T) {
	cases := []struct {
		name string
		host string
		ip   string
	}{
		{name: "ipv4-loopback", host: "127.0.0.1", ip: "127.0.0.1"},
		{name: "ipv4-private", host: "192.168.1.1", ip: "192.168.1.1"},
		{name: "ipv4-linklocal-unicast", host: "169.254.1.1", ip: "169.254.1.1"},
		{name: "ipv4-linklocal-multicast", host: "224.0.0.1", ip: "224.0.0.1"},
		{name: "ipv4-unspecified", host: "0.0.0.0", ip: "0.0.0.0"},
		{name: "ipv6-loopback", host: "::1", ip: "::1"},
		{name: "ipv6-private", host: "fd00::1", ip: "fd00::1"},
		{name: "ipv6-linklocal-unicast", host: "fe80::1", ip: "fe80::1"},
		{name: "ipv6-linklocal-multicast", host: "ff02::1", ip: "ff02::1"},
		{name: "ipv6-unspecified", host: "::", ip: "::"},
		{name: "ipv6-mapped-private", host: "::ffff:10.0.0.1", ip: "10.0.0.1"},
		{name: "ipv6-mapped-loopback", host: "::ffff:127.0.0.1", ip: "127.0.0.1"},
		{name: "ipv6-mapped-linklocal", host: "::ffff:169.254.1.1", ip: "169.254.1.1"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := mustAddr(t, tc.host, 9)
			require.Equal(t, tc.ip, addr.IP().String())

			connectOnly := newUDPPolicy()
			connectOnly.allowConnect(addr)
			err := authorizeUDPDestination(udpActorScope(t, connectOnly), addr)
			requireNetworkError(t, err, NetworkErrorAccessDenied)
			require.True(t, connectOnly.saw("socket.connect", addr.String()))
			require.True(t, connectOnly.saw("socket.private_ip", tc.ip))

			both := newUDPPolicy()
			both.allowConnect(addr)
			both.allowPrivateIP(addr)
			require.Nil(t, authorizeUDPDestination(udpActorScope(t, both), addr))
			require.True(t, both.saw("socket.private_ip", tc.ip))
		})
	}
}

func TestAuthorizeUDPDestination_PublicConnect(t *testing.T) {
	cases := []struct {
		name string
		host string
	}{
		{name: "ipv4", host: "8.8.8.8"},
		{name: "ipv6", host: "2001:4860:4860::8888"},
		{name: "ipv6-mapped", host: "::ffff:8.8.8.8"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			addr := mustAddr(t, tc.host, 53)
			policy := newUDPPolicy()
			policy.allowConnect(addr)
			require.Nil(t, authorizeUDPDestination(udpActorScope(t, policy), addr))
			require.True(t, policy.saw("socket.connect", addr.String()))
			for _, pair := range policy.seen {
				require.NotEqual(t, "socket.private_ip", pair[0])
			}
		})
	}
}

func TestAuthorizeUDPDestination_IPv6ScopeUsesIPNotZone(t *testing.T) {
	addr := NewIPv6SocketAddress([8]uint16{0xfe80, 0, 0, 0, 0, 0, 0, 1}, 9, 0, 1)
	require.Equal(t, "fe80::1", addr.IP().String())
	require.Equal(t, "fe80::1%1", addr.IPString())

	zoneOnly := newUDPPolicy()
	zoneOnly.allowConnect(addr)
	zoneOnly.allow("socket.private_ip", addr.IPString())
	err := authorizeUDPDestination(udpActorScope(t, zoneOnly), addr)
	requireNetworkError(t, err, NetworkErrorAccessDenied)
	require.True(t, zoneOnly.saw("socket.private_ip", "fe80::1"))
	require.False(t, zoneOnly.saw("socket.private_ip", "fe80::1%1"))

	ipLiteral := newUDPPolicy()
	ipLiteral.allowConnect(addr)
	ipLiteral.allow("socket.private_ip", "fe80::1")
	require.Nil(t, authorizeUDPDestination(udpActorScope(t, ipLiteral), addr))
}

func TestAuthorizeUDPDestination_NonstrictRootFixtureAllowsPrivate(t *testing.T) {
	addr := mustAddr(t, "127.0.0.1", 9)
	require.Nil(t, authorizeUDPDestination(udpTestContext(), addr))
}

func TestAuthorizeUDPDestination_NilAddress(t *testing.T) {
	requireNetworkError(t, authorizeUDPDestination(udpTestContext(), nil), NetworkErrorInvalidArgument)
}

func TestUDPStream_PrivateIPPolicy(t *testing.T) {
	private := mustAddr(t, "127.0.0.1", 9)
	public := mustAddr(t, "8.8.8.8", 53)

	t.Run("connect-without-private-ip", func(t *testing.T) {
		host, socket, handle := boundUDPTestHost(t)
		policy := newUDPPolicy()
		policy.allowConnect(private)
		incoming, outgoing, err := host.MethodUDPSocketStream(udpActorScope(t, policy), handle, private)
		require.Zero(t, incoming)
		require.Zero(t, outgoing)
		requireNetworkError(t, err, NetworkErrorAccessDenied)
		require.Empty(t, socket.RemoteAddr())
		inHandle, outHandle := socket.StreamHandles()
		require.Zero(t, inHandle)
		require.Zero(t, outHandle)
	})

	t.Run("connect-and-private-ip", func(t *testing.T) {
		host, socket, handle := boundUDPTestHost(t)
		policy := newUDPPolicy()
		policy.allowConnect(private)
		policy.allowPrivateIP(private)
		incoming, outgoing, err := host.MethodUDPSocketStream(udpActorScope(t, policy), handle, private)
		require.Nil(t, err)
		require.NotZero(t, incoming)
		require.NotZero(t, outgoing)
		require.Equal(t, private.IPString(), socket.RemoteAddr())
	})

	t.Run("public-connect", func(t *testing.T) {
		host, _, handle := boundUDPTestHost(t)
		policy := newUDPPolicy()
		policy.allowConnect(public)
		incoming, outgoing, err := host.MethodUDPSocketStream(udpActorScope(t, policy), handle, public)
		require.Nil(t, err)
		require.NotZero(t, incoming)
		require.NotZero(t, outgoing)
	})
}

func TestUDPStream_IPv6PrivateAndPublic(t *testing.T) {
	host, socket, handle := boundUDPTestHostFamily(t, AddressFamilyIPv6, net.IPv6loopback)
	private := mustAddr(t, "::1", 9)
	public := mustAddr(t, "2001:4860:4860::8888", 53)
	mapped := NewIPv6SocketAddress([8]uint16{0, 0, 0, 0, 0, 0xffff, 0xc0a8, 0x0001}, 9, 0, 0)
	require.Equal(t, "192.168.0.1", mapped.IP().String())

	connectOnly := newUDPPolicy()
	connectOnly.allowConnect(private)
	incoming, outgoing, err := host.MethodUDPSocketStream(udpActorScope(t, connectOnly), handle, private)
	require.Zero(t, incoming)
	require.Zero(t, outgoing)
	requireNetworkError(t, err, NetworkErrorAccessDenied)
	require.Empty(t, socket.RemoteAddr())

	mappedOnly := newUDPPolicy()
	mappedOnly.allowConnect(mapped)
	incoming, outgoing, err = host.MethodUDPSocketStream(udpActorScope(t, mappedOnly), handle, mapped)
	require.Zero(t, incoming)
	require.Zero(t, outgoing)
	requireNetworkError(t, err, NetworkErrorAccessDenied)
	require.True(t, mappedOnly.saw("socket.private_ip", "192.168.0.1"))

	both := newUDPPolicy()
	both.allowConnect(private)
	both.allowPrivateIP(private)
	incoming, outgoing, err = host.MethodUDPSocketStream(udpActorScope(t, both), handle, private)
	require.Nil(t, err)
	require.NotZero(t, incoming)
	require.NotZero(t, outgoing)

	host.ResourceDropIncomingDatagramStream(udpTestContext(), incoming)
	host.ResourceDropOutgoingDatagramStream(udpTestContext(), outgoing)

	publicPolicy := newUDPPolicy()
	publicPolicy.allowConnect(public)
	incoming, outgoing, err = host.MethodUDPSocketStream(udpActorScope(t, publicPolicy), handle, public)
	require.Nil(t, err)
	require.NotZero(t, incoming)
	require.NotZero(t, outgoing)
}

func TestUDPSend_PrivateIPNeverEnqueued(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	authorized := SocketAddressFromNetAddr(peer.LocalAddr())
	denied := mustAddr(t, "10.0.0.1", 9)

	_, outgoing, networkErr := host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, networkErr)

	connectOnly := newUDPPolicy()
	connectOnly.allowConnect(denied)
	requireUDPSendPermit(t, host, outgoing)
	before, checkErr := host.MethodOutgoingDatagramStreamCheckSend(udpTestContext(), outgoing)
	require.Nil(t, checkErr)
	require.Positive(t, before)
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(udpActorScope(t, connectOnly), outgoing, []OutgoingDatagram{
		{RemoteAddress: denied, Data: []byte("denied")},
	})
	require.Zero(t, sent)
	requireNetworkError(t, networkErr, NetworkErrorAccessDenied)
	require.True(t, connectOnly.saw("socket.private_ip", "10.0.0.1"))

	after, checkErr := host.MethodOutgoingDatagramStreamCheckSend(udpTestContext(), outgoing)
	require.Nil(t, checkErr)
	require.Equal(t, before, after, "denied datagram must not occupy send queue")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	buffer := make([]byte, 32)
	_, _, readErr := peer.ReadFromUDP(buffer)
	require.Error(t, readErr)
	var netErr net.Error
	require.True(t, errors.As(readErr, &netErr) && netErr.Timeout(), "peer must not receive a denied datagram")

	both := newUDPPolicy()
	both.allowConnect(authorized)
	both.allowPrivateIP(authorized)
	both.allowConnect(denied)
	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr = host.MethodOutgoingDatagramStreamSend(udpActorScope(t, both), outgoing, []OutgoingDatagram{
		{RemoteAddress: authorized, Data: []byte("authorized")},
		{RemoteAddress: denied, Data: []byte("denied-after")},
	})
	require.Equal(t, uint64(1), sent)
	require.Nil(t, networkErr)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, _, readErr := peer.ReadFromUDP(buffer)
	require.NoError(t, readErr)
	require.Equal(t, "authorized", string(buffer[:n]))
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	_, _, readErr = peer.ReadFromUDP(buffer)
	require.Error(t, readErr)
	require.True(t, errors.As(readErr, &netErr) && netErr.Timeout(), "peer must receive only the authorized datagram")
}

func TestUDPSend_ConnectedStreamPrivateIP(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	remote := SocketAddressFromNetAddr(peer.LocalAddr())

	connectOnly := newUDPPolicy()
	connectOnly.allowConnect(remote)
	_, _, networkErr := host.MethodUDPSocketStream(udpActorScope(t, connectOnly), handle, remote)
	requireNetworkError(t, networkErr, NetworkErrorAccessDenied)

	both := newUDPPolicy()
	both.allowConnect(remote)
	both.allowPrivateIP(remote)
	_, outgoing, networkErr := host.MethodUDPSocketStream(udpActorScope(t, both), handle, remote)
	require.Nil(t, networkErr)
	requireUDPSendPermit(t, host, outgoing)

	sendPolicy := newUDPPolicy()
	sendPolicy.allowConnect(remote)
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(udpActorScope(t, sendPolicy), outgoing, []OutgoingDatagram{
		{Data: []byte("denied-connected")},
	})
	require.Zero(t, sent)
	requireNetworkError(t, networkErr, NetworkErrorAccessDenied)

	require.NoError(t, peer.SetReadDeadline(time.Now().Add(30*time.Millisecond)))
	buffer := make([]byte, 32)
	_, _, readErr := peer.ReadFromUDP(buffer)
	require.Error(t, readErr)

	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr = host.MethodOutgoingDatagramStreamSend(udpActorScope(t, both), outgoing, []OutgoingDatagram{
		{Data: []byte("connected-ok")},
	})
	require.Nil(t, networkErr)
	require.Equal(t, uint64(1), sent)
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(2*time.Second)))
	n, _, readErr := peer.ReadFromUDP(buffer)
	require.NoError(t, readErr)
	require.Equal(t, "connected-ok", string(buffer[:n]))
}

func boundUDPTestHostFamily(t *testing.T, family uint8, ip net.IP) (*UDPHost, *preview2.UDPSocketResource, uint32) {
	t.Helper()
	network := "udp4"
	if family == AddressFamilyIPv6 {
		network = "udp6"
	}
	conn, err := net.ListenUDP(network, &net.UDPAddr{IP: ip})
	if err != nil {
		t.Skipf("%s not supported: %v", network, err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	socket := preview2.NewUDPSocketResource(family)
	socket.SetState(preview2.UDPStateBound)
	socket.SetConn(conn)
	return NewUDPHost(table), socket, table.Add(socket)
}
