// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"
	"net"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	netapi "github.com/wippyai/runtime/api/net"
	socketapi "github.com/wippyai/runtime/api/socket"
)

func TestStartBindAcknowledgesBeforeCompletion(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer conn.Close()
	svc := &mockNetService{listenPacketFunc: func(context.Context, string, string) (net.PacketConn, error) {
		close(entered)
		<-release
		return conn, nil
	}}
	op := socketapi.NewPendingOperation()
	var releaseOnce sync.Once
	unblock := func() { releaseOnce.Do(func() { close(release) }) }
	defer func() {
		unblock()
		_ = op.Close()
	}()
	recv := newCaptureReceiver()
	require.NoError(t, NewDispatcher(svc).handleStartBind(context.Background(), &socketapi.StartBindCmd{Operation: op, Network: "udp", Address: "127.0.0.1:0"}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	<-entered
	require.False(t, op.Ready())
	unblock()
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.Same(t, conn, value)
	require.NoError(t, value.Close())
}

func TestStartBindDeadlineClosesLateSocket(t *testing.T) {
	returned := make(chan struct{})
	var conn *net.UDPConn
	svc := &mockNetService{listenPacketFunc: func(ctx context.Context, _ string, _ string) (net.PacketConn, error) {
		<-ctx.Done()
		var err error
		conn, err = net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
		close(returned)
		return conn, err
	}}
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, NewDispatcher(svc).handleStartBind(context.Background(), &socketapi.StartBindCmd{Operation: op, Network: "udp", Address: "127.0.0.1:0", Timeout: 10 * time.Millisecond}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	<-returned
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, takeErr, context.DeadlineExceeded)
	require.NoError(t, op.Close())
	require.NotNil(t, conn)
	require.ErrorIs(t, conn.SetDeadline(time.Now()), net.ErrClosed)
}

type wrappedBindPacketConn struct{ net.PacketConn }

func TestStartBindRejectsUnsupportedPacketProviderAndClosesResult(t *testing.T) {
	conn, err := net.ListenUDP("udp4", &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1)})
	require.NoError(t, err)
	defer conn.Close()
	svc := &mockNetService{listenPacketFunc: func(context.Context, string, string) (net.PacketConn, error) {
		return &wrappedBindPacketConn{PacketConn: conn}, nil
	}}
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, NewDispatcher(svc).handleStartBind(context.Background(), &socketapi.StartBindCmd{Operation: op, Network: "udp", Address: "127.0.0.1:0"}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
	require.NoError(t, op.Close())
	require.ErrorIs(t, conn.SetDeadline(time.Now()), net.ErrClosed)
}

func TestStartBindRejectsTypedNilUDPResult(t *testing.T) {
	svc := &mockNetService{listenPacketFunc: func(context.Context, string, string) (net.PacketConn, error) {
		var conn *net.UDPConn
		return conn, nil
	}}
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, NewDispatcher(svc).handleStartBind(context.Background(), &socketapi.StartBindCmd{Operation: op, Network: "udp", Address: "127.0.0.1:0"}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	value, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, err, netapi.ErrNotSupported)
	require.NoError(t, op.Close())
}
