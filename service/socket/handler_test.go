package socket

import (
	"context"
	"fmt"
	"net"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
)

type mockNetService struct {
	dialFunc         func(ctx context.Context, network, address string) (net.Conn, error)
	listenFunc       func(ctx context.Context, network, address string) (net.Listener, error)
	listenPacketFunc func(ctx context.Context, network, address string) (net.PacketConn, error)
	lookupFunc       func(ctx context.Context, host string) ([]string, error)
}

func (m *mockNetService) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	return m.dialFunc(ctx, network, address)
}

func (m *mockNetService) Listen(ctx context.Context, network, address string) (net.Listener, error) {
	return m.listenFunc(ctx, network, address)
}

func (m *mockNetService) ListenPacket(ctx context.Context, network, address string) (net.PacketConn, error) {
	return m.listenPacketFunc(ctx, network, address)
}

func (m *mockNetService) LookupHost(ctx context.Context, host string) ([]string, error) {
	return m.lookupFunc(ctx, host)
}

type captureReceiver struct {
	data any
	err  error
	done chan struct{}
	mu   sync.Mutex
}

func newCaptureReceiver() *captureReceiver {
	return &captureReceiver{done: make(chan struct{})}
}

func (r *captureReceiver) CompleteYield(_ uint64, data any, err error) {
	r.mu.Lock()
	r.data = data
	r.err = err
	r.mu.Unlock()
	close(r.done)
}

func (r *captureReceiver) wait() (any, error) {
	<-r.done
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.data, r.err
}

func TestDispatcher_RegisterAll(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	registered := make(map[dispatcher.CommandID]dispatcher.Handler)
	d.RegisterAll(func(id dispatcher.CommandID, h dispatcher.Handler) {
		registered[id] = h
	})

	assert.Contains(t, registered, socketapi.SocketConnect)
	assert.Contains(t, registered, socketapi.SocketListen)
	assert.Contains(t, registered, socketapi.SocketAccept)
	assert.Contains(t, registered, socketapi.SocketBind)
	assert.Contains(t, registered, socketapi.SocketResolve)
}

func TestDispatcher_HandleConnect(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	svc := &mockNetService{
		dialFunc: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ConnectCmd{Network: "tcp", Address: ln.Addr().String()}
	err = d.handleConnect(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ConnectResult)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Conn)
	_ = result.Conn.Close()
}

func TestDispatcher_HandleListen(t *testing.T) {
	svc := &mockNetService{
		listenFunc: func(ctx context.Context, network, address string) (net.Listener, error) {
			return new(net.ListenConfig).Listen(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ListenCmd{Network: "tcp", Address: "127.0.0.1:0"}
	err := d.handleListen(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ListenResult)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Listener)
	_ = result.Listener.Close()
}

func TestDispatcher_HandleAccept(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	// Connect in background so accept has something
	go func() {
		conn, _ := (&net.Dialer{}).DialContext(context.Background(), "tcp", ln.Addr().String())
		if conn != nil {
			defer conn.Close()
		}
	}()

	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()

	cmd := &socketapi.AcceptCmd{Listener: ln}
	err = d.handleAccept(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.AcceptResult)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Conn)
	_ = result.Conn.Close()
}

func TestDispatcher_HandleBind(t *testing.T) {
	svc := &mockNetService{
		listenPacketFunc: func(ctx context.Context, network, address string) (net.PacketConn, error) {
			return new(net.ListenConfig).ListenPacket(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.BindCmd{Network: "udp", Address: "127.0.0.1:0"}
	err := d.handleBind(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.BindResult)
	require.NoError(t, result.Err)
	require.NotNil(t, result.Conn)
	_ = result.Conn.Close()
}

func TestDispatcher_HandleConnect_Error(t *testing.T) {
	svc := &mockNetService{
		dialFunc: func(_ context.Context, _, _ string) (net.Conn, error) {
			return nil, fmt.Errorf("connection refused")
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ConnectCmd{Network: "tcp", Address: "192.0.2.1:1"}
	err := d.handleConnect(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ConnectResult)
	require.Error(t, result.Err)
	assert.Nil(t, result.Conn)
}

func TestDispatcher_HandleListen_Error(t *testing.T) {
	svc := &mockNetService{
		listenFunc: func(_ context.Context, _, _ string) (net.Listener, error) {
			return nil, fmt.Errorf("address in use")
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ListenCmd{Network: "tcp", Address: "127.0.0.1:1"}
	err := d.handleListen(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ListenResult)
	require.Error(t, result.Err)
	assert.Nil(t, result.Listener)
}

func TestDispatcher_HandleBind_Error(t *testing.T) {
	svc := &mockNetService{
		listenPacketFunc: func(_ context.Context, _, _ string) (net.PacketConn, error) {
			return nil, fmt.Errorf("bind failed")
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.BindCmd{Network: "udp", Address: "127.0.0.1:1"}
	err := d.handleBind(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.BindResult)
	require.Error(t, result.Err)
	assert.Nil(t, result.Conn)
}

func TestDispatcher_HandleResolve_Error(t *testing.T) {
	svc := &mockNetService{
		lookupFunc: func(_ context.Context, _ string) ([]string, error) {
			return nil, fmt.Errorf("no such host")
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ResolveCmd{Host: "nonexistent.invalid"}
	err := d.handleResolve(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ResolveResult)
	require.Error(t, result.Err)
	assert.Nil(t, result.Addresses)
}

func TestDispatcher_HandleResolve(t *testing.T) {
	svc := &mockNetService{
		lookupFunc: func(_ context.Context, host string) ([]string, error) {
			return []string{"127.0.0.1"}, nil
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()

	cmd := &socketapi.ResolveCmd{Host: "localhost"}
	err := d.handleResolve(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, _ := recv.wait()
	result := data.(*socketapi.ResolveResult)
	require.NoError(t, result.Err)
	assert.Equal(t, []string{"127.0.0.1"}, result.Addresses)
}
