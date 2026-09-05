// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"net"
	"os"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"unsafe"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wasm-runtime/transcoder"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
	"go.bytecodealliance.org/wit"
)

// Sourced from standard WIT fixture at runtime/wasm/engine/testdata/tcp/wit/deps/sockets/network.wit
func loadNetworkWitFixture(t *testing.T) string {
	t.Helper()
	paths := []string{
		"../../../../engine/testdata/tcp/wit/deps/sockets/network.wit",
		"runtime/wasm/engine/testdata/tcp/wit/deps/sockets/network.wit",
		"/home/wolfy-j/wippy/wippy/.worktrees/wasm-actors-perf/runtime/wasm/engine/testdata/tcp/wit/deps/sockets/network.wit",
	}
	for _, p := range paths {
		if content, err := os.ReadFile(p); err == nil {
			return string(content)
		}
	}
	t.Fatal("could not find standard WIT fixture at runtime/wasm/engine/testdata/tcp/wit")
	return ""
}

// Extract enum cases from network.wit
func parseErrorCodeEnumCases(t *testing.T, witContent string) []string {
	t.Helper()
	enumStart := strings.Index(witContent, "enum error-code {")
	require.True(t, enumStart >= 0, "enum error-code not found in network.wit")

	enumEnd := strings.Index(witContent[enumStart:], "}")
	require.True(t, enumEnd >= 0, "closing brace of enum error-code not found")

	block := witContent[enumStart : enumStart+enumEnd]
	lines := strings.Split(block, "\n")
	var cases []string
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "//") || strings.HasPrefix(line, "/*") || line == "" {
			continue
		}
		if strings.HasPrefix(line, "enum error-code") || line == "{" || line == "}" {
			continue
		}
		name := strings.TrimRight(strings.TrimSpace(line), ",")
		if name != "" && !strings.Contains(name, " ") {
			cases = append(cases, name)
		}
	}
	return cases
}

func TestCanonicalErrorCodePayloadAndWITFixture(t *testing.T) {
	witContent := loadNetworkWitFixture(t)
	witCases := parseErrorCodeEnumCases(t, witContent)

	expectedCases := []string{
		"unknown",
		"access-denied",
		"not-supported",
		"invalid-argument",
		"out-of-memory",
		"timeout",
		"concurrency-conflict",
		"not-in-progress",
		"would-block",
		"invalid-state",
		"new-socket-limit",
		"address-not-bindable",
		"address-in-use",
		"remote-unreachable",
		"connection-refused",
		"connection-reset",
		"connection-aborted",
		"datagram-too-large",
		"name-unresolvable",
		"temporary-resolver-failure",
		"permanent-resolver-failure",
	}

	require.Equal(t, expectedCases, witCases, "enum error-code cases in network.wit must match canonical list")

	type witErrorPayloadProvider interface {
		WITErrorPayload() any
	}

	for i := 0; i < len(expectedCases); i++ {
		code := NetworkErrorCode(i)
		err := &NetworkError{Code: code}

		var provider witErrorPayloadProvider = err
		payload := provider.WITErrorPayload()

		disc, ok := payload.(uint32)
		require.True(t, ok, "WITErrorPayload must return uint32 discriminant for enum error-code")
		require.Equal(t, uint32(i), disc, "discriminant for %s must be %d", expectedCases[i], i)
	}

	// Verify out-of-range code is verified and safely clamped to NetworkErrorUnknown
	outOfRangeErr := &NetworkError{Code: NetworkErrorCode(250)}
	require.Equal(t, uint32(NetworkErrorUnknown), outOfRangeErr.WITErrorPayload())

	// Verify nil receiver is safely handled
	var nilErr *NetworkError
	require.Equal(t, uint32(NetworkErrorUnknown), nilErr.WITErrorPayload())
}

func TestNetworkHostAndRegistration(t *testing.T) {
	resources := preview2.NewResourceTable()
	networkHost := NewNetworkHost(resources)
	instanceNetHost := NewInstanceNetworkHost(resources)

	require.Equal(t, NetworkNamespace, networkHost.Namespace())
	require.Equal(t, "wasi:sockets/network@0.2.8", networkHost.Namespace())

	regNetwork := networkHost.Register()
	require.Contains(t, regNetwork, "[resource-drop]network", "network host must own [resource-drop]network")
	require.NotContains(t, regNetwork, "instance-network")

	regInstanceNet := instanceNetHost.Register()
	require.Contains(t, regInstanceNet, "instance-network")
	require.NotContains(t, regInstanceNet, "[resource-drop]network", "instance-network must NOT own [resource-drop]network")

	// Resource drop life-cycle:
	netHandle := instanceNetHost.InstanceNetwork(context.Background())
	require.NotZero(t, netHandle)

	_, ok := resources.Get(netHandle)
	require.True(t, ok, "network resource should exist in table")

	networkHost.ResourceDropNetwork(context.Background(), netHandle)
	_, ok = resources.Get(netHandle)
	require.False(t, ok, "network resource should be removed after drop")
}

func TestIPSocketAddressCanonicalStructureAndCompiledVariantBinder(t *testing.T) {
	// 1. Check Go struct field definitions and tags for compiled variant binder
	ipSockType := reflect.TypeOf(IPSocketAddress{})
	require.Equal(t, reflect.Struct, ipSockType.Kind())
	require.Equal(t, 2, ipSockType.NumField())

	fIPv4 := ipSockType.Field(0)
	require.Equal(t, "IPv4", fIPv4.Name)
	require.Equal(t, reflect.Pointer, fIPv4.Type.Kind())
	require.Equal(t, "ipv4", fIPv4.Tag.Get("wit"))

	fIPv6 := ipSockType.Field(1)
	require.Equal(t, "IPv6", fIPv6.Name)
	require.Equal(t, reflect.Pointer, fIPv6.Type.Kind())
	require.Equal(t, "ipv6", fIPv6.Tag.Get("wit"))

	// 2. Check IPv4SocketAddress
	ipv4SockType := reflect.TypeOf(IPv4SocketAddress{})
	require.Equal(t, reflect.Struct, ipv4SockType.Kind())
	fPort4 := ipv4SockType.Field(0)
	require.Equal(t, "port", fPort4.Tag.Get("wit"))
	require.Equal(t, reflect.Uint16, fPort4.Type.Kind())
	fAddr4 := ipv4SockType.Field(1)
	require.Equal(t, "address", fAddr4.Tag.Get("wit"))
	require.Equal(t, reflect.Array, fAddr4.Type.Kind())
	require.Equal(t, 4, fAddr4.Type.Len())

	// 3. Check IPv6SocketAddress
	ipv6SockType := reflect.TypeOf(IPv6SocketAddress{})
	require.Equal(t, reflect.Struct, ipv6SockType.Kind())
	require.Equal(t, "port", ipv6SockType.Field(0).Tag.Get("wit"))
	require.Equal(t, "flow-info", ipv6SockType.Field(1).Tag.Get("wit"))
	require.Equal(t, "address", ipv6SockType.Field(2).Tag.Get("wit"))
	require.Equal(t, reflect.Array, ipv6SockType.Field(2).Type.Kind())
	require.Equal(t, 8, ipv6SockType.Field(2).Type.Len())
	require.Equal(t, "scope-id", ipv6SockType.Field(3).Tag.Get("wit"))

	// 4. Test compilation with wasm-runtime transcoder compiler
	ipv4AddrTuple := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{wit.U8{}, wit.U8{}, wit.U8{}, wit.U8{}}},
	}
	ipv4SocketRecord := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "port", Type: wit.U16{}},
				{Name: "address", Type: ipv4AddrTuple},
			},
		},
	}
	ipv6AddrTuple := &wit.TypeDef{
		Kind: &wit.Tuple{Types: []wit.Type{
			wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{},
			wit.U16{}, wit.U16{}, wit.U16{}, wit.U16{},
		}},
	}
	ipv6SocketRecord := &wit.TypeDef{
		Kind: &wit.Record{
			Fields: []wit.Field{
				{Name: "port", Type: wit.U16{}},
				{Name: "flow-info", Type: wit.U32{}},
				{Name: "address", Type: ipv6AddrTuple},
				{Name: "scope-id", Type: wit.U32{}},
			},
		},
	}
	ipSocketAddressVariant := &wit.TypeDef{
		Kind: &wit.Variant{
			Cases: []wit.Case{
				{Name: "ipv4", Type: ipv4SocketRecord},
				{Name: "ipv6", Type: ipv6SocketRecord},
			},
		},
	}

	compiler := transcoder.NewCompiler()
	compiled, err := compiler.Compile(ipSocketAddressVariant, ipSockType)
	require.NoError(t, err)
	require.NotNil(t, compiled)
	require.Equal(t, transcoder.KindVariant, compiled.Kind)
	require.Equal(t, 2, len(compiled.Cases))

	// 5. Test Lower/Lift on stack for IPv4
	encoder := transcoder.NewEncoder()
	decoder := transcoder.NewDecoder()

	origIPv4 := SocketAddressFromHostPort("192.0.2.55", 8080)
	stack := make([]uint64, compiled.FlatCount)

	consumed, err := encoder.LowerToStack(compiled, unsafe.Pointer(origIPv4), stack, nil, nil)
	require.NoError(t, err)
	require.Equal(t, compiled.FlatCount, consumed)
	require.Equal(t, uint64(0), stack[0], "IPv4 discriminant is 0")

	var liftedIPv4 IPSocketAddress
	liftConsumed, err := decoder.LiftFromStack(compiled, stack, unsafe.Pointer(&liftedIPv4), nil)
	require.NoError(t, err)
	require.Equal(t, compiled.FlatCount, liftConsumed)
	require.True(t, origIPv4.Equal(&liftedIPv4), "lifted IPv4 must match original")

	// 6. Test Lower/Lift on stack for IPv6
	origIPv6 := SocketAddressFromHostPort("2001:db8::1", 9090)
	stack6 := make([]uint64, compiled.FlatCount)

	consumed6, err := encoder.LowerToStack(compiled, unsafe.Pointer(origIPv6), stack6, nil, nil)
	require.NoError(t, err)
	require.Equal(t, compiled.FlatCount, consumed6)
	require.Equal(t, uint64(1), stack6[0], "IPv6 discriminant is 1")

	var liftedIPv6 IPSocketAddress
	liftConsumed6, err := decoder.LiftFromStack(compiled, stack6, unsafe.Pointer(&liftedIPv6), nil)
	require.NoError(t, err)
	require.Equal(t, compiled.FlatCount, liftConsumed6)
	require.True(t, origIPv6.Equal(&liftedIPv6), "lifted IPv6 must match original")
}

func TestIPSocketAddressConversionsPreserveInternalAPI(t *testing.T) {
	// IPv4 conversion:
	addr4 := SocketAddressFromHostPort("127.0.0.1", 8080)
	require.NotNil(t, addr4)
	require.NotNil(t, addr4.IPv4)
	require.Nil(t, addr4.IPv6)
	require.Equal(t, "127.0.0.1:8080", addr4.String())
	require.Equal(t, "127.0.0.1", addr4.IPString())
	require.Equal(t, uint16(8080), addr4.Port())
	require.True(t, addr4.IP().Equal(net.IPv4(127, 0, 0, 1)))
	require.Equal(t, IPv4Address{127, 0, 0, 1}, addr4.IPv4.Address)

	// IPv6 conversion:
	addr6 := SocketAddressFromHostPort("::1", 9000)
	require.NotNil(t, addr6)
	require.Nil(t, addr6.IPv4)
	require.NotNil(t, addr6.IPv6)
	require.Equal(t, "[::1]:9000", addr6.String())
	require.Equal(t, "::1", addr6.IPString())
	require.Equal(t, uint16(9000), addr6.Port())
	require.True(t, addr6.IP().Equal(net.ParseIP("::1")))

	// Bracketed IPv6 string:
	addr6Bracket := SocketAddressFromHostPort("[fe80::1]", 443)
	require.NotNil(t, addr6Bracket)
	require.NotNil(t, addr6Bracket.IPv6)
	require.Equal(t, "fe80::1", addr6Bracket.IPString())
	require.Equal(t, uint16(443), addr6Bracket.Port())

	// Net.Addr conversions:
	tcpAddr := &net.TCPAddr{IP: net.ParseIP("10.1.2.3"), Port: 1234}
	sockTCP := SocketAddressFromNetAddr(tcpAddr)
	require.NotNil(t, sockTCP)
	require.Equal(t, "10.1.2.3:1234", sockTCP.String())

	udpAddr := &net.UDPAddr{IP: net.ParseIP("2001:db8::2"), Port: 5678}
	sockUDP := SocketAddressFromNetAddr(udpAddr)
	require.NotNil(t, sockUDP)
	require.Equal(t, "[2001:db8::2]:5678", sockUDP.String())

	// Equality:
	clone4 := SocketAddressFromHostPort("127.0.0.1", 8080)
	require.True(t, addr4.Equal(clone4))
	diff4 := SocketAddressFromHostPort("127.0.0.1", 8081)
	require.False(t, addr4.Equal(diff4))
	require.False(t, addr4.Equal(addr6))
	require.False(t, addr4.Equal(nil))
}

func TestTCPAndUDPHostLocalRemoteAddressCanonicalABI(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	udpHost := NewUDPHost(resources)

	// TCP IPv4 socket
	tcpSock := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	tcpHandle := resources.Add(tcpSock)
	tcpSock.SetLocalAddr("192.0.2.1", 8080)
	tcpSock.SetRemoteAddr("192.0.2.2", 80)
	tcpSock.SetState(preview2.TCPStateConnected)

	localTCP, err := tcpHost.MethodTCPSocketLocalAddress(context.Background(), tcpHandle)
	require.Nil(t, err)
	require.NotNil(t, localTCP)
	require.NotNil(t, localTCP.IPv4)
	require.Nil(t, localTCP.IPv6)
	require.Equal(t, "192.0.2.1:8080", localTCP.String())

	remoteTCP, err := tcpHost.MethodTCPSocketRemoteAddress(context.Background(), tcpHandle)
	require.Nil(t, err)
	require.NotNil(t, remoteTCP)
	require.NotNil(t, remoteTCP.IPv4)
	require.Equal(t, "192.0.2.2:80", remoteTCP.String())

	// UDP IPv6 socket
	udpSock := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	udpHandle := resources.Add(udpSock)
	udpSock.SetLocalAddr("::1", 9000)
	udpSock.SetRemoteAddr("2001:db8::1", 9001)
	udpSock.SetState(preview2.UDPStateBound)

	localUDP, err := udpHost.MethodUDPSocketLocalAddress(context.Background(), udpHandle)
	require.Nil(t, err)
	require.NotNil(t, localUDP)
	require.Nil(t, localUDP.IPv4)
	require.NotNil(t, localUDP.IPv6)
	require.Equal(t, "[::1]:9000", localUDP.String())

	remoteUDP, err := udpHost.MethodUDPSocketRemoteAddress(context.Background(), udpHandle)
	require.Nil(t, err)
	require.NotNil(t, remoteUDP)
	require.NotNil(t, remoteUDP.IPv6)
	require.Equal(t, "[2001:db8::1]:9001", remoteUDP.String())
}

func TestIPv6ScopeIDPreservationAndZoneRoundtrip(t *testing.T) {
	// 1. Numeric scope ID roundtrip for an unmapped/virtual scope ID (e.g. 42)
	zone42 := ZoneFromScopeID(42)
	require.Equal(t, "42", zone42)
	scope42 := ScopeIDFromZone(zone42)
	require.Equal(t, uint32(42), scope42)

	// 2. Named OS zone mapping (if loopback exists on this host)
	loIfi, err := net.InterfaceByName("lo")
	if err == nil && loIfi != nil {
		scopeLo := ScopeIDFromZone("lo")
		require.Equal(t, uint32(loIfi.Index), scopeLo)

		zoneFromIdx := ZoneFromScopeID(uint32(loIfi.Index))
		require.Equal(t, strconv.Itoa(loIfi.Index), zoneFromIdx)

		// Roundtrip through zone string
		roundtripScope := ScopeIDFromZone(zoneFromIdx)
		require.Equal(t, uint32(loIfi.Index), roundtripScope)
	}

	// 3. String() and IPString() zone preservation
	addrWithScope := NewIPv6SocketAddress(IPv6Address{0xfe80, 0, 0, 0, 0, 0, 0, 1}, 8080, 0, 42)
	require.Equal(t, "fe80::1%42", addrWithScope.IPString())
	require.Equal(t, "[fe80::1%42]:8080", addrWithScope.String())

	addrNoScope := NewIPv6SocketAddress(IPv6Address{0, 0, 0, 0, 0, 0, 0, 1}, 9000, 0, 0)
	require.Equal(t, "::1", addrNoScope.IPString())
	require.Equal(t, "[::1]:9000", addrNoScope.String())

	// 4. SocketAddressFromNetAddr preserving zone
	tcpAddrWithZone := &net.TCPAddr{IP: net.ParseIP("fe80::1"), Port: 1234, Zone: "42"}
	sockTCP := SocketAddressFromNetAddr(tcpAddrWithZone)
	require.NotNil(t, sockTCP)
	require.NotNil(t, sockTCP.IPv6)
	require.Equal(t, uint32(42), sockTCP.IPv6.ScopeID)
	require.Equal(t, "[fe80::1%42]:1234", sockTCP.String())

	udpAddrWithZone := &net.UDPAddr{IP: net.ParseIP("fe80::2"), Port: 5678, Zone: "42"}
	sockUDP := SocketAddressFromNetAddr(udpAddrWithZone)
	require.NotNil(t, sockUDP)
	require.NotNil(t, sockUDP.IPv6)
	require.Equal(t, uint32(42), sockUDP.IPv6.ScopeID)
	require.Equal(t, "[fe80::2%42]:5678", sockUDP.String())

	// 5. SocketAddressFromHostPort with zone
	parsedWithZone := SocketAddressFromHostPort("fe80::1%42", 7777)
	require.NotNil(t, parsedWithZone)
	require.NotNil(t, parsedWithZone.IPv6)
	require.Equal(t, uint32(42), parsedWithZone.IPv6.ScopeID)
	require.Equal(t, "[fe80::1%42]:7777", parsedWithZone.String())

	// Bracketed with zone
	parsedBracketed := SocketAddressFromHostPort("[fe80::1%42]", 7777)
	require.NotNil(t, parsedBracketed)
	require.NotNil(t, parsedBracketed.IPv6)
	require.Equal(t, uint32(42), parsedBracketed.IPv6.ScopeID)

	// Empty string must return nil (not fabricated 0.0.0.0 fallback)
	require.Nil(t, SocketAddressFromHostPort("", 8080))
}

func TestFlowInfoRejectionAcrossEntrypoints(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	udpHost := NewUDPHost(resources)

	nonzeroFlowAddr := *NewIPv6SocketAddress(IPv6Address{0, 0, 0, 0, 0, 0, 0, 1}, 8080, 123, 0)
	zeroFlowAddr := *NewIPv6SocketAddress(IPv6Address{0, 0, 0, 0, 0, 0, 0, 1}, 8080, 0, 0)

	// ValidateFlowInfo helper
	require.Equal(t, NetworkErrorNotSupported, ValidateFlowInfo(&nonzeroFlowAddr).Code)
	require.Nil(t, ValidateFlowInfo(&zeroFlowAddr))

	// 1. TCP start-bind
	tcpSock := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	tcpHandle := resources.Add(tcpSock)
	errBind := tcpHost.MethodTCPSocketStartBind(context.Background(), tcpHandle, 0, nonzeroFlowAddr)
	require.NotNil(t, errBind)
	require.Equal(t, NetworkErrorNotSupported, errBind.Code)

	// 2. TCP start-connect
	tcpSockConn := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	tcpConnHandle := resources.Add(tcpSockConn)
	errConn := tcpHost.MethodTCPSocketStartConnect(context.Background(), tcpConnHandle, 0, nonzeroFlowAddr)
	require.NotNil(t, errConn)
	require.Equal(t, NetworkErrorNotSupported, errConn.Code)

	// 3. UDP start-bind
	udpSock := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	udpHandle := resources.Add(udpSock)
	errUDPBind := udpHost.MethodUDPSocketStartBind(context.Background(), udpHandle, 0, nonzeroFlowAddr)
	require.NotNil(t, errUDPBind)
	require.Equal(t, NetworkErrorNotSupported, errUDPBind.Code)

	// 4. UDP stream
	udpSockBound := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	udpSockBound.SetState(preview2.UDPStateBound)
	udpBoundHandle := resources.Add(udpSockBound)
	_, _, errStream := udpHost.MethodUDPSocketStream(context.Background(), udpBoundHandle, &nonzeroFlowAddr)
	require.NotNil(t, errStream)
	require.Equal(t, NetworkErrorNotSupported, errStream.Code)

	// 5. UDP outgoing-datagram-stream send
	udpSender, errListen := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback})
	if errListen != nil {
		// Fallback for environments without IPv6 loopback
		udpSender, errListen = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	}
	require.NoError(t, errListen)
	t.Cleanup(func() { _ = udpSender.Close() })
	udpSockBound.SetConn(udpSender)

	_, outHandle, errStreamZero := udpHost.MethodUDPSocketStream(context.Background(), udpBoundHandle, nil)
	require.Nil(t, errStreamZero)
	dgrams := []OutgoingDatagram{
		{RemoteAddress: &nonzeroFlowAddr, Data: []byte("test")},
	}
	sent, errSend := udpHost.MethodOutgoingDatagramStreamSend(context.Background(), outHandle, dgrams)
	require.Equal(t, uint64(0), sent)
	require.NotNil(t, errSend)
	require.Equal(t, NetworkErrorNotSupported, errSend.Code)
}

func TestAddressFamilyValidationAcrossEntrypoints(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	udpHost := NewUDPHost(resources)

	ipv4Addr := *NewIPv4SocketAddress(IPv4Address{127, 0, 0, 1}, 8080)
	ipv6Addr := *NewIPv6SocketAddress(IPv6Address{0, 0, 0, 0, 0, 0, 0, 1}, 8080, 0, 0)
	emptyAddr := IPSocketAddress{}
	bothAddr := IPSocketAddress{IPv4: ipv4Addr.IPv4, IPv6: ipv6Addr.IPv6}

	// ValidateAddressFamily helper
	require.Equal(t, NetworkErrorInvalidArgument, ValidateAddressFamily(&ipv4Addr, AddressFamilyIPv6).Code)
	require.Equal(t, NetworkErrorInvalidArgument, ValidateAddressFamily(&ipv6Addr, AddressFamilyIPv4).Code)
	require.Equal(t, NetworkErrorInvalidArgument, ValidateAddressFamily(&emptyAddr, AddressFamilyIPv4).Code)
	require.Equal(t, NetworkErrorInvalidArgument, ValidateAddressFamily(&bothAddr, AddressFamilyIPv6).Code)
	require.Nil(t, ValidateAddressFamily(&ipv4Addr, AddressFamilyIPv4))
	require.Nil(t, ValidateAddressFamily(&ipv6Addr, AddressFamilyIPv6))

	// TCP IPv6 socket rejecting IPv4
	tcp6 := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	tcp6Handle := resources.Add(tcp6)
	require.Equal(t, NetworkErrorInvalidArgument, tcpHost.MethodTCPSocketStartBind(context.Background(), tcp6Handle, 0, ipv4Addr).Code)
	require.Equal(t, NetworkErrorInvalidArgument, tcpHost.MethodTCPSocketStartConnect(context.Background(), tcp6Handle, 0, ipv4Addr).Code)

	// TCP IPv4 socket rejecting IPv6
	tcp4 := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	tcp4Handle := resources.Add(tcp4)
	require.Equal(t, NetworkErrorInvalidArgument, tcpHost.MethodTCPSocketStartBind(context.Background(), tcp4Handle, 0, ipv6Addr).Code)
	require.Equal(t, NetworkErrorInvalidArgument, tcpHost.MethodTCPSocketStartConnect(context.Background(), tcp4Handle, 0, ipv6Addr).Code)

	// UDP IPv6 socket rejecting IPv4
	udp6 := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	udp6Handle := resources.Add(udp6)
	require.Equal(t, NetworkErrorInvalidArgument, udpHost.MethodUDPSocketStartBind(context.Background(), udp6Handle, 0, ipv4Addr).Code)

	udpConn6, errListen6 := net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv6loopback})
	if errListen6 != nil {
		udpConn6, _ = net.ListenUDP("udp", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	}
	if udpConn6 != nil {
		t.Cleanup(func() { _ = udpConn6.Close() })
	}
	udp6.SetState(preview2.UDPStateBound)
	udp6.SetConn(udpConn6)
	_, _, streamErr := udpHost.MethodUDPSocketStream(context.Background(), udp6Handle, &ipv4Addr)
	require.Equal(t, NetworkErrorInvalidArgument, streamErr.Code)

	_, outHandle, _ := udpHost.MethodUDPSocketStream(context.Background(), udp6Handle, nil)
	sent, sendErr := udpHost.MethodOutgoingDatagramStreamSend(context.Background(), outHandle, []OutgoingDatagram{
		{RemoteAddress: &ipv4Addr, Data: []byte("fail")},
	})
	require.Equal(t, uint64(0), sent)
	require.Equal(t, NetworkErrorInvalidArgument, sendErr.Code)

	// UDP IPv4 socket rejecting IPv6
	udp4 := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	udp4Handle := resources.Add(udp4)
	require.Equal(t, NetworkErrorInvalidArgument, udpHost.MethodUDPSocketStartBind(context.Background(), udp4Handle, 0, ipv6Addr).Code)

	udpConn4, errListen4 := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, errListen4)
	t.Cleanup(func() { _ = udpConn4.Close() })
	udp4.SetState(preview2.UDPStateBound)
	udp4.SetConn(udpConn4)
	_, _, streamErr4 := udpHost.MethodUDPSocketStream(context.Background(), udp4Handle, &ipv6Addr)
	require.Equal(t, NetworkErrorInvalidArgument, streamErr4.Code)

	_, outHandle4, _ := udpHost.MethodUDPSocketStream(context.Background(), udp4Handle, nil)
	sent4, sendErr4 := udpHost.MethodOutgoingDatagramStreamSend(context.Background(), outHandle4, []OutgoingDatagram{
		{RemoteAddress: &ipv6Addr, Data: []byte("fail")},
	})
	require.Equal(t, uint64(0), sent4)
	require.Equal(t, NetworkErrorInvalidArgument, sendErr4.Code)
}

func TestRemovedFabricatedUnspecifiedAddressFallback(t *testing.T) {
	resources := preview2.NewResourceTable()
	tcpHost := NewTCPHost(resources)
	udpHost := NewUDPHost(resources)

	// 1. Unbound TCP socket preserves legitimate unbound semantics (invalid-state)
	tcpSock := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	tcpHandle := resources.Add(tcpSock)
	_, err := tcpHost.MethodTCPSocketLocalAddress(context.Background(), tcpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorInvalidState, err.Code)

	// 2. Bound TCP socket with invalid/empty stored address returns NetworkErrorUnknown, NOT fabricated fallback
	tcpSock.SetState(preview2.TCPStateBound)
	tcpSock.SetLocalAddr("not-an-ip", 8080)
	_, err = tcpHost.MethodTCPSocketLocalAddress(context.Background(), tcpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorUnknown, err.Code)

	// Stored address family mismatch (IPv6 socket with stored IPv4 address)
	tcp6 := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	tcp6.SetState(preview2.TCPStateBound)
	tcp6.SetLocalAddr("127.0.0.1", 8080)
	tcp6Handle := resources.Add(tcp6)
	_, err = tcpHost.MethodTCPSocketLocalAddress(context.Background(), tcp6Handle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorUnknown, err.Code)

	// Connected TCP socket with invalid stored remote address returns NetworkErrorUnknown
	tcpSock.SetState(preview2.TCPStateConnected)
	tcpSock.SetRemoteAddr("invalid-remote", 80)
	_, err = tcpHost.MethodTCPSocketRemoteAddress(context.Background(), tcpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorUnknown, err.Code)

	// 3. Unbound UDP socket preserves legitimate unbound semantics (invalid-state)
	udpSock := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	udpHandle := resources.Add(udpSock)
	_, err = udpHost.MethodUDPSocketLocalAddress(context.Background(), udpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorInvalidState, err.Code)

	// Bound UDP socket with invalid stored local address returns NetworkErrorUnknown
	udpSock.SetState(preview2.UDPStateBound)
	udpSock.SetLocalAddr("invalid-udp", 9000)
	_, err = udpHost.MethodUDPSocketLocalAddress(context.Background(), udpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorUnknown, err.Code)

	// UDP socket with invalid stored remote address returns NetworkErrorUnknown
	udpSock.SetRemoteAddr("invalid-remote-udp", 9001)
	_, err = udpHost.MethodUDPSocketRemoteAddress(context.Background(), udpHandle)
	require.NotNil(t, err)
	require.Equal(t, NetworkErrorUnknown, err.Code)

	// 4. Legitimate bound unspecified addresses are preserved without error
	tcpUnspec := preview2.NewTCPSocketResource(AddressFamilyIPv4)
	tcpUnspec.SetState(preview2.TCPStateBound)
	tcpUnspec.SetLocalAddr("0.0.0.0", 8080)
	hUnspec := resources.Add(tcpUnspec)
	localUnspec, err := tcpHost.MethodTCPSocketLocalAddress(context.Background(), hUnspec)
	require.Nil(t, err)
	require.NotNil(t, localUnspec)
	require.Equal(t, "0.0.0.0:8080", localUnspec.String())

	tcp6Unspec := preview2.NewTCPSocketResource(AddressFamilyIPv6)
	tcp6Unspec.SetState(preview2.TCPStateBound)
	tcp6Unspec.SetLocalAddr("::", 9000)
	h6Unspec := resources.Add(tcp6Unspec)
	local6Unspec, err := tcpHost.MethodTCPSocketLocalAddress(context.Background(), h6Unspec)
	require.Nil(t, err)
	require.NotNil(t, local6Unspec)
	require.Equal(t, "[::]:9000", local6Unspec.String())
}
