// SPDX-License-Identifier: MPL-2.0

package sockets

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestS04UDPBindRejectsWrongAsyncType(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBindInProgress)
	handle := resources.Add(socket)

	left, right := net.Pipe()
	carried := &closeCountingConn{Conn: left}
	t.Cleanup(func() { _ = right.Close() })
	ctx := rewindContext(t, &socketapi.ConnectResult{Conn: carried})

	err := host.MethodUDPSocketStartBind(ctx, handle, 0, IPSocketAddress{})
	requireNetworkError(t, err, NetworkErrorInvalidArgument)
	if carried.closes.Load() != 1 {
		t.Fatalf("unadopted connection close count = %d, want 1", carried.closes.Load())
	}
	if socket.Conn() != nil || socket.State() != preview2.UDPStateBindInProgress {
		t.Fatalf("socket changed after rejected result: conn = %v, state = %d", socket.Conn(), socket.State())
	}
}

func TestS11UDPStreamDefaultRemote(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen UDP: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBound)
	socket.SetConn(conn)
	handle := resources.Add(socket)
	remote := *SocketAddressFromHostPort("127.0.0.1", 45678)

	incomingHandle, outgoingHandle, networkErr := host.MethodUDPSocketStream(context.Background(), handle, &remote)
	if networkErr != nil {
		t.Fatalf("create datagram streams: %v", networkErr)
	}
	if incomingHandle == 0 || outgoingHandle == 0 || incomingHandle == outgoingHandle {
		t.Fatalf("stream handles = (%d, %d), want distinct nonzero handles", incomingHandle, outgoingHandle)
	}
	incomingResource, ok := resources.Get(incomingHandle)
	if !ok {
		t.Fatal("incoming stream not found")
	}
	incoming, ok := incomingResource.(*preview2.IncomingDatagramStreamResource)
	if !ok {
		t.Fatalf("incoming resource type = %T", incomingResource)
	}
	outgoingResource, ok := resources.Get(outgoingHandle)
	if !ok {
		t.Fatal("outgoing stream not found")
	}
	outgoing, ok := outgoingResource.(*preview2.OutgoingDatagramStreamResource)
	if !ok {
		t.Fatalf("outgoing resource type = %T", outgoingResource)
	}
	if address, port, present := incoming.RemoteAddr(); !present || address != remote.IPString() || port != remote.Port() {
		t.Fatalf("incoming default remote = (%q, %d, %v), want (%q, %d, true)", address, port, present, remote.IPString(), remote.Port())
	}
	if address, port, present := outgoing.RemoteAddr(); !present || address != remote.IPString() || port != remote.Port() {
		t.Fatalf("outgoing default remote = (%q, %d, %v), want (%q, %d, true)", address, port, present, remote.IPString(), remote.Port())
	}
	storedRemote, networkErr := host.MethodUDPSocketRemoteAddress(context.Background(), handle)
	if networkErr != nil || storedRemote == nil || !storedRemote.Equal(&remote) {
		t.Fatalf("socket default remote = %#v, error = %v, want %#v", storedRemote, networkErr, remote)
	}
}

func TestS12UDPSendRequiresDestination(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen sender: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen peer: %v", err)
	}
	t.Cleanup(func() { _ = peer.Close() })
	if err := peer.SetReadDeadline(time.Now().Add(20 * time.Millisecond)); err != nil {
		t.Fatal(err)
	}

	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBound)
	socket.SetConn(sender)
	handle := resources.Add(socket)
	_, outgoingHandle, networkErr := host.MethodUDPSocketStream(context.Background(), handle, nil)
	if networkErr != nil {
		t.Fatalf("create datagram streams: %v", networkErr)
	}

	sent, networkErr := host.MethodOutgoingDatagramStreamSend(context.Background(), outgoingHandle, []OutgoingDatagram{{Data: []byte("must-not-send")}})
	requireNetworkError(t, networkErr, NetworkErrorInvalidArgument)
	if sent != 0 {
		t.Fatalf("sent count = %d, want zero", sent)
	}
	buffer := make([]byte, 32)
	if _, _, err := peer.ReadFromUDP(buffer); err == nil {
		t.Fatal("peer received a packet without a destination")
	} else {
		var netErr net.Error
		if !errors.As(err, &netErr) || !netErr.Timeout() {
			t.Fatalf("peer read error = %v, want deadline timeout", err)
		}
	}
}

func TestS13UDPDatagramLoopback(t *testing.T) {
	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)
	sender, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen sender: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	receiver, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.ParseIP("127.0.0.1")})
	if err != nil {
		t.Fatalf("listen receiver: %v", err)
	}
	t.Cleanup(func() { _ = receiver.Close() })
	deadline := time.Now().Add(2 * time.Second)
	if err := sender.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}

	senderSocket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	senderSocket.SetState(preview2.UDPStateBound)
	senderSocket.SetConn(sender)
	senderHandle := resources.Add(senderSocket)
	receiverAddress := receiver.LocalAddr().(*net.UDPAddr)
	remote := *SocketAddressFromIP(receiverAddress.IP, uint16(receiverAddress.Port))
	_, outgoingHandle, networkErr := host.MethodUDPSocketStream(context.Background(), senderHandle, &remote)
	if networkErr != nil {
		t.Fatalf("create outgoing stream: %v", networkErr)
	}

	receiverSocket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	receiverSocket.SetState(preview2.UDPStateBound)
	receiverSocket.SetConn(receiver)
	receiverHandle := resources.Add(receiverSocket)
	incomingHandle, _, networkErr := host.MethodUDPSocketStream(context.Background(), receiverHandle, nil)
	if networkErr != nil {
		t.Fatalf("create incoming stream: %v", networkErr)
	}

	payload := []byte("wasi-datagram")
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(context.Background(), outgoingHandle, []OutgoingDatagram{{Data: payload}})
	if networkErr != nil || sent != 1 {
		t.Fatalf("send datagram: count = %d, error = %v", sent, networkErr)
	}
	received, networkErr := host.MethodIncomingDatagramStreamReceive(context.Background(), incomingHandle, 1)
	if networkErr != nil {
		t.Fatalf("receive datagram: %v", networkErr)
	}
	if len(received) != 1 || string(received[0].Data) != string(payload) {
		t.Fatalf("received datagrams = %#v, want payload %q", received, payload)
	}
	senderAddress := sender.LocalAddr().(*net.UDPAddr)
	wantSender := *SocketAddressFromIP(senderAddress.IP, uint16(senderAddress.Port))
	if !received[0].RemoteAddress.Equal(&wantSender) {
		t.Fatalf("sender address = %#v, want %#v", received[0].RemoteAddress, wantSender)
	}
}

func TestUDPDatagramLoopbackIPv6Zone(t *testing.T) {
	sender, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Skipf("IPv6 not supported on host: %v", err)
	}
	t.Cleanup(func() { _ = sender.Close() })
	receiver, err := net.ListenUDP("udp6", &net.UDPAddr{IP: net.IPv6loopback})
	if err != nil {
		t.Skipf("IPv6 not supported on host: %v", err)
	}
	t.Cleanup(func() { _ = receiver.Close() })

	deadline := time.Now().Add(2 * time.Second)
	if err := sender.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	if err := receiver.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}

	resources := preview2.NewResourceTable()
	host := NewUDPHost(resources)

	senderSocket := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	senderSocket.SetState(preview2.UDPStateBound)
	senderSocket.SetConn(sender)
	senderHandle := resources.Add(senderSocket)

	receiverAddress := receiver.LocalAddr().(*net.UDPAddr)
	remote := *SocketAddressFromNetAddr(receiverAddress)
	_, outgoingHandle, networkErr := host.MethodUDPSocketStream(context.Background(), senderHandle, &remote)
	if networkErr != nil {
		t.Fatalf("create outgoing stream: %v", networkErr)
	}

	receiverSocket := preview2.NewUDPSocketResource(AddressFamilyIPv6)
	receiverSocket.SetState(preview2.UDPStateBound)
	receiverSocket.SetConn(receiver)
	receiverHandle := resources.Add(receiverSocket)
	incomingHandle, _, networkErr := host.MethodUDPSocketStream(context.Background(), receiverHandle, nil)
	if networkErr != nil {
		t.Fatalf("create incoming stream: %v", networkErr)
	}

	payload := []byte("wasi-ipv6-datagram")
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(context.Background(), outgoingHandle, []OutgoingDatagram{{Data: payload}})
	if networkErr != nil || sent != 1 {
		t.Fatalf("send datagram: count = %d, error = %v", sent, networkErr)
	}

	received, networkErr := host.MethodIncomingDatagramStreamReceive(context.Background(), incomingHandle, 1)
	if networkErr != nil {
		t.Fatalf("receive datagram: %v", networkErr)
	}
	if len(received) != 1 || string(received[0].Data) != string(payload) {
		t.Fatalf("received datagrams = %#v, want payload %q", received, payload)
	}

	senderAddress := sender.LocalAddr().(*net.UDPAddr)
	wantSender := *SocketAddressFromNetAddr(senderAddress)
	if !received[0].RemoteAddress.Equal(&wantSender) {
		t.Fatalf("sender address = %#v, want %#v", received[0].RemoteAddress, wantSender)
	}
}
