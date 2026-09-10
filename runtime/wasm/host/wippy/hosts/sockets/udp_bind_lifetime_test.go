// SPDX-License-Identifier: MPL-2.0
package sockets

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
	"github.com/wippyai/wasm-runtime/wasi/preview2"
)

func TestUDPLocalAddressRequiresCompletedBind(t *testing.T) {
	table := preview2.NewResourceTable()
	t.Cleanup(func() { require.NoError(t, table.Close()) })
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetLocalAddr("127.0.0.1", 8100)
	handle := table.Add(socket)
	host := NewUDPHost(table)
	for _, state := range []preview2.UDPState{preview2.UDPStateUnbound, preview2.UDPStateBindInProgress} {
		socket.SetState(state)
		address, err := host.MethodUDPSocketLocalAddress(t.Context(), handle)
		require.Nil(t, address)
		requireNetworkError(t, err, NetworkErrorInvalidState)
	}
	socket.SetState(preview2.UDPStateBound)
	address, err := host.MethodUDPSocketLocalAddress(t.Context(), handle)
	require.Nil(t, err)
	require.Equal(t, uint16(8100), address.Port())
}

type udpCleanupGate struct {
	*net.UDPConn
	entered chan struct{}
	release chan struct{}
}

func (c *udpCleanupGate) Close() error {
	close(c.entered)
	<-c.release
	return c.UDPConn.Close()
}

func TestUDPFinishCanceledBindRetainsQuotaDuringCleanup(t *testing.T) {
	table := preview2.NewResourceTableWithLimits(16, 1)
	socket := preview2.NewUDPSocketResource(AddressFamilyIPv4)
	socket.SetState(preview2.UDPStateBindInProgress)
	handle := table.Add(socket)
	raw, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	conn := &udpCleanupGate{UDPConn: raw, entered: make(chan struct{}), release: make(chan struct{})}
	var once sync.Once
	unblock := func() { once.Do(func() { close(conn.release) }) }
	t.Cleanup(func() { unblock(); require.NoError(t, table.Close()); _ = raw.Close() })
	parent, cancel := context.WithCancel(t.Context())
	defer cancel()
	op := socketapi.NewPendingOperation()
	_, started := op.Start(parent)
	require.True(t, started)
	require.NoError(t, socket.SetPendingOperation(op))
	op.Complete(conn, nil)
	cancel()
	select {
	case <-conn.entered:
	case <-time.After(time.Second):
		t.Fatal("cancellation did not start physical close")
	}
	finished := make(chan *NetworkError, 1)
	go func() { finished <- NewUDPHost(table).MethodUDPSocketFinishBind(t.Context(), handle) }()
	require.Eventually(t, func() bool { return socket.PendingError() != nil }, time.Second, time.Millisecond)
	dropped := make(chan struct{})
	go func() { table.Remove(handle); close(dropped) }()
	select {
	case <-dropped:
		t.Fatal("socket drop returned before physical close")
	case <-time.After(50 * time.Millisecond):
	}
	require.Equal(t, 1, table.SocketBudget().Used())
	unblock()
	select {
	case err := <-finished:
		requireNetworkError(t, err, NetworkErrorConnectionAborted)
	case <-time.After(time.Second):
		t.Fatal("finish did not join cleanup")
	}
	select {
	case <-dropped:
	case <-time.After(time.Second):
		t.Fatal("socket drop did not finish")
	}
	require.Zero(t, table.SocketBudget().Used())
	require.ErrorIs(t, raw.SetDeadline(time.Now()), net.ErrClosed)
}
