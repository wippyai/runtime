// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestUDPSendValidationAndPartialSuccess(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	_, outgoing, networkErr := host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, networkErr)
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	remote := SocketAddressFromNetAddr(peer.LocalAddr())

	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(context.Background(), outgoing, []OutgoingDatagram{{RemoteAddress: remote, Data: []byte("denied")}})
	require.Zero(t, sent)
	requireNetworkError(t, networkErr, NetworkErrorAccessDenied)

	oversized := OutgoingDatagram{RemoteAddress: remote, Data: make([]byte, maxUDPDatagramBytes+1)}
	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr = host.MethodOutgoingDatagramStreamSend(udpTestContext(), outgoing, []OutgoingDatagram{oversized})
	require.Zero(t, sent)
	requireNetworkError(t, networkErr, NetworkErrorDatagramTooLarge)

	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr = host.MethodOutgoingDatagramStreamSend(udpTestContext(), outgoing, []OutgoingDatagram{{RemoteAddress: remote, Data: []byte("accepted")}, oversized})
	require.Equal(t, uint64(1), sent)
	require.Nil(t, networkErr, "an error after an accepted packet must return the accepted count")
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(2*time.Second)))
	buffer := make([]byte, 32)
	n, _, err := peer.ReadFromUDP(buffer)
	require.NoError(t, err)
	require.Equal(t, "accepted", string(buffer[:n]), "rejected packets must never reach the peer")
}

func boundUDPTestHost(t *testing.T) (*UDPHost, *preview2.UDPSocketResource, uint32) {
	t.Helper()
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBound)
	socket.SetConn(conn)
	return NewUDPHost(table), socket, table.Add(socket)
}

func TestUDPEmptyReceiveNeverBlocks(t *testing.T) {
	host, socket, handle := boundUDPTestHost(t)
	incoming, _, err := host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, err)
	done := make(chan struct{})
	var packets []IncomingDatagram
	var networkErr *NetworkError
	go func() {
		defer close(done)
		packets, networkErr = host.MethodIncomingDatagramStreamReceive(udpTestContext(), incoming, 16)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		socket.Drop()
		<-done
		t.Fatal("empty UDP receive blocked on network I/O")
	}
	require.Nil(t, networkErr)
	require.Empty(t, packets)
	require.False(t, socket.IncomingPollable().Ready())
}

func TestUDPSendPermitsAreConsumed(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	_, outgoing, err := host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, err)
	ctx := udpTestContext()
	require.Panics(t, func() { host.MethodOutgoingDatagramStreamSend(ctx, outgoing, nil) })
	count, err := host.MethodOutgoingDatagramStreamCheckSend(ctx, outgoing)
	require.Nil(t, err)
	require.Positive(t, count)
	require.LessOrEqual(t, count, uint64(maxUDPBatch))
	sent, err := host.MethodOutgoingDatagramStreamSend(ctx, outgoing, nil)
	require.Nil(t, err)
	require.Zero(t, sent)
	require.Panics(t, func() { host.MethodOutgoingDatagramStreamSend(ctx, outgoing, nil) })
	requireUDPSendPermit(t, host, outgoing)
	require.Panics(t, func() { host.MethodOutgoingDatagramStreamSend(ctx, outgoing, make([]OutgoingDatagram, maxUDPBatch+1)) })
}

func TestUDPConnectedDestinationAndZeroLength(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	peer, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	t.Cleanup(func() { _ = peer.Close() })
	require.NoError(t, peer.SetReadDeadline(time.Now().Add(2*time.Second)))
	remote := SocketAddressFromNetAddr(peer.LocalAddr())
	_, outgoing, networkErr := host.MethodUDPSocketStream(udpTestContext(), handle, remote)
	require.Nil(t, networkErr)
	requireUDPSendPermit(t, host, outgoing)
	other := SocketAddressFromHostPort("127.0.0.2", remote.Port())
	sent, networkErr := host.MethodOutgoingDatagramStreamSend(udpTestContext(), outgoing, []OutgoingDatagram{{RemoteAddress: other, Data: []byte("reject")}})
	require.Zero(t, sent)
	requireNetworkError(t, networkErr, NetworkErrorInvalidArgument)
	requireUDPSendPermit(t, host, outgoing)
	sent, networkErr = host.MethodOutgoingDatagramStreamSend(udpTestContext(), outgoing, []OutgoingDatagram{{Data: nil}})
	require.Nil(t, networkErr)
	require.Equal(t, uint64(1), sent)
	buffer := make([]byte, 32)
	n, _, err := peer.ReadFromUDP(buffer)
	require.NoError(t, err)
	require.Zero(t, n, "zero-length datagrams must be sent, not treated as empty batches")
}

func TestUDPStreamReplacementAndPublicationRollback(t *testing.T) {
	host, _, handle := boundUDPTestHost(t)
	incoming, outgoing, err := host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, err)
	require.Panics(t, func() { host.MethodUDPSocketStream(udpTestContext(), handle, nil) })
	host.ResourceDropIncomingDatagramStream(udpTestContext(), incoming)
	host.ResourceDropOutgoingDatagramStream(udpTestContext(), outgoing)
	_, _, err = host.MethodUDPSocketStream(udpTestContext(), handle, nil)
	require.Nil(t, err)

	table := preview2.NewResourceTableWithLimits(2, 1)
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	limited := NewUDPHost(table)
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBound)
	limitedHandle := table.Add(socket)
	in, out, err := limited.MethodUDPSocketStream(udpTestContext(), limitedHandle, nil)
	require.Zero(t, in)
	require.Zero(t, out)
	requireNetworkError(t, err, NetworkErrorOutOfMemory)
	// A second attempt must fail with quota again, not encounter a leaked stream.
	_, _, err = limited.MethodUDPSocketStream(udpTestContext(), limitedHandle, nil)
	requireNetworkError(t, err, NetworkErrorOutOfMemory)
}
