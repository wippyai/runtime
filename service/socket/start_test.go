// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
)

type trackedConn struct {
	net.Conn
	done chan struct{}
	once sync.Once
	n    atomic.Int32
}

func newTrackedConn(t *testing.T) *trackedConn {
	t.Helper()
	left, right := net.Pipe()
	t.Cleanup(func() {
		_ = left.Close()
		_ = right.Close()
	})
	return &trackedConn{Conn: left, done: make(chan struct{})}
}

func (c *trackedConn) Close() error {
	c.n.Add(1)
	c.once.Do(func() { close(c.done) })
	return c.Conn.Close()
}

func (c *trackedConn) closes() int32 { return c.n.Load() }

type trackedListener struct {
	net.Listener
	done chan struct{}
	once sync.Once
	n    atomic.Int32
}

func newTrackedListener(t *testing.T) *trackedListener {
	t.Helper()
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	t.Cleanup(func() { _ = ln.Close() })
	return &trackedListener{Listener: ln, done: make(chan struct{})}
}

func (l *trackedListener) Close() error {
	l.n.Add(1)
	l.once.Do(func() { close(l.done) })
	return l.Listener.Close()
}

func (l *trackedListener) closes() int32 { return l.n.Load() }

func waitReady(t *testing.T, op *socketapi.PendingOperation) {
	t.Helper()
	select {
	case <-op.Notify():
	case <-time.After(3 * time.Second):
		t.Fatal("operation did not become ready")
	}
}

func ackStart(t *testing.T, recv *captureReceiver) *socketapi.StartResult {
	t.Helper()
	data, err := recv.wait()
	require.NoError(t, err)
	result, ok := data.(*socketapi.StartResult)
	require.True(t, ok)
	require.NotNil(t, result)
	return result
}

func TestDispatcher_HandleStartConnect_ACKBeforeCompletion(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	conn := newTrackedConn(t)
	svc := &mockNetService{
		dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return conn, nil
			}
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()
	op := socketapi.NewPendingOperation()
	defer op.Close()

	err := d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{
		Operation: op,
		Network:   "tcp",
		Address:   "127.0.0.1:1",
	}, 1, recv)
	require.NoError(t, err)

	ack := ackStart(t, recv)
	require.NoError(t, ack.Err)
	<-entered
	require.False(t, op.Ready())
	value, takeErr, ready := op.Take()
	require.False(t, ready)
	require.Nil(t, value)
	require.NoError(t, takeErr)

	close(release)
	waitReady(t, op)
	value, takeErr, ready = op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.Equal(t, conn, value)
	require.Equal(t, int32(0), conn.closes())
	require.NoError(t, value.Close())
}

func TestDispatcher_HandleStartListen_ACKBeforeCompletion(t *testing.T) {
	entered := make(chan struct{})
	release := make(chan struct{})
	ln := newTrackedListener(t)
	svc := &mockNetService{
		listenFunc: func(ctx context.Context, _, _ string) (net.Listener, error) {
			close(entered)
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return ln, nil
			}
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()
	op := socketapi.NewPendingOperation()
	defer op.Close()

	err := d.handleStartListen(context.Background(), &socketapi.StartListenCmd{
		Operation: op,
		Network:   "tcp",
		Address:   "127.0.0.1:0",
	}, 1, recv)
	require.NoError(t, err)

	ack := ackStart(t, recv)
	require.NoError(t, ack.Err)
	<-entered
	require.False(t, op.Ready())

	close(release)
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.Equal(t, ln, value)
	require.NoError(t, value.Close())
}

func TestDispatcher_HandleStartConnect_TwoOperationsCoexist(t *testing.T) {
	var mu sync.Mutex
	entered := 0
	started := make(chan struct{})
	release := make(chan struct{})
	first := newTrackedConn(t)
	second := newTrackedConn(t)
	svc := &mockNetService{
		dialFunc: func(ctx context.Context, _, address string) (net.Conn, error) {
			mu.Lock()
			entered++
			if entered == 2 {
				close(started)
			}
			mu.Unlock()
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				if address == "first" {
					return first, nil
				}
				return second, nil
			}
		},
	}
	d := NewDispatcher(svc)
	op1 := socketapi.NewPendingOperation()
	op2 := socketapi.NewPendingOperation()
	defer op1.Close()
	defer op2.Close()
	recv1 := newCaptureReceiver()
	recv2 := newCaptureReceiver()

	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op1, Address: "first"}, 1, recv1))
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op2, Address: "second"}, 2, recv2))
	require.NoError(t, ackStart(t, recv1).Err)
	require.NoError(t, ackStart(t, recv2).Err)

	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("both start jobs did not run together")
	}
	require.False(t, op1.Ready())
	require.False(t, op2.Ready())

	close(release)
	waitReady(t, op1)
	waitReady(t, op2)
	v1, err1, ok1 := op1.Take()
	v2, err2, ok2 := op2.Take()
	require.True(t, ok1)
	require.True(t, ok2)
	require.NoError(t, err1)
	require.NoError(t, err2)
	require.Equal(t, first, v1)
	require.Equal(t, second, v2)
	require.NoError(t, v1.Close())
	require.NoError(t, v2.Close())
}

func TestDispatcher_HandleStartConnect_CancelLateResultClosesOnce(t *testing.T) {
	entered := make(chan struct{})
	late := newTrackedConn(t)
	svc := &mockNetService{
		dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
			close(entered)
			<-ctx.Done()
			return late, nil
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()
	op := socketapi.NewPendingOperation()

	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	<-entered
	require.NoError(t, op.Close())
	select {
	case <-late.done:
	case <-time.After(3 * time.Second):
		t.Fatal("late connect result was not closed")
	}
	require.Equal(t, int32(1), late.closes())
}

func TestDispatcher_HandleStartListen_CancelLateResultClosesOnce(t *testing.T) {
	entered := make(chan struct{})
	late := newTrackedListener(t)
	svc := &mockNetService{
		listenFunc: func(ctx context.Context, _, _ string) (net.Listener, error) {
			close(entered)
			<-ctx.Done()
			return late, nil
		},
	}
	d := NewDispatcher(svc)
	recv := newCaptureReceiver()
	op := socketapi.NewPendingOperation()

	require.NoError(t, d.handleStartListen(context.Background(), &socketapi.StartListenCmd{Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	<-entered
	require.NoError(t, op.Close())
	select {
	case <-late.done:
	case <-time.After(3 * time.Second):
		t.Fatal("late listen result was not closed")
	}
	require.Equal(t, int32(1), late.closes())
}

func TestDispatcher_HandleStartConnect_CloseBeforeStartRejected(t *testing.T) {
	d := NewDispatcher(&mockNetService{
		dialFunc: func(context.Context, string, string) (net.Conn, error) {
			t.Fatal("closed operation must not start dial")
			return nil, nil
		},
	})
	op := socketapi.NewPendingOperation()
	require.NoError(t, op.Close())
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 1, recv))
	ack := ackStart(t, recv)
	require.ErrorIs(t, ack.Err, socketapi.ErrAlreadyStarted)
	_, ok := op.Start(context.Background())
	require.False(t, ok)
}

func TestDispatcher_HandleStartConnect_RepeatedAndNilRejected(t *testing.T) {
	var dials atomic.Int32
	entered := make(chan struct{})
	svc := &mockNetService{
		dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
			if dials.Add(1) == 1 {
				close(entered)
			}
			<-ctx.Done()
			return nil, ctx.Err()
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()

	recv1 := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 1, recv1))
	require.NoError(t, ackStart(t, recv1).Err)
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("start job did not run")
	}

	recv2 := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 2, recv2))
	require.ErrorIs(t, ackStart(t, recv2).Err, socketapi.ErrAlreadyStarted)

	recv3 := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{}, 3, recv3))
	require.ErrorIs(t, ackStart(t, recv3).Err, socketapi.ErrNilOperation)

	recv4 := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.ConnectCmd{}, 4, recv4))
	require.ErrorIs(t, ackStart(t, recv4).Err, socketapi.ErrNilOperation)

	require.Equal(t, int32(1), dials.Load())
}

func TestDispatcher_HandleStartListen_InvalidRejected(t *testing.T) {
	d := NewDispatcher(&mockNetService{
		listenFunc: func(context.Context, string, string) (net.Listener, error) {
			t.Fatal("invalid listen must not run")
			return nil, nil
		},
	})
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartListen(context.Background(), &socketapi.ListenCmd{}, 1, recv))
	require.ErrorIs(t, ackStart(t, recv).Err, socketapi.ErrNilOperation)
}

func TestDispatcher_HandleStartConnect_StaleAndDoubleTake(t *testing.T) {
	release := make(chan struct{})
	conn := newTrackedConn(t)
	svc := &mockNetService{
		dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-release:
				return conn, nil
			}
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)

	value, err, ready := op.Take()
	require.False(t, ready)
	require.Nil(t, value)
	require.NoError(t, err)

	close(release)
	waitReady(t, op)
	first, err, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, err)
	require.Equal(t, conn, first)
	second, err, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, second)
	require.ErrorIs(t, err, socketapi.ErrAlreadyTaken)
	require.NoError(t, first.Close())
}

type gatedTrackedConn struct {
	net.Conn
	entered chan struct{}
	release chan struct{}
	quota   *atomic.Int32
	n       atomic.Int32
	enter   sync.Once
	quotaDo sync.Once
}

func (c *gatedTrackedConn) Close() error {
	c.n.Add(1)
	c.enter.Do(func() { close(c.entered) })
	<-c.release
	c.quotaDo.Do(func() {
		if c.quota != nil {
			c.quota.Add(-1)
		}
	})
	return c.Conn.Close()
}

func (c *gatedTrackedConn) closes() int32 { return c.n.Load() }

func TestDispatcher_HandleStartConnect_GatedParentCancelConcurrentTakeClose(t *testing.T) {
	quota := &atomic.Int32{}
	quota.Store(1)
	conn := &gatedTrackedConn{
		Conn:    newTrackedConn(t).Conn,
		entered: make(chan struct{}),
		release: make(chan struct{}),
		quota:   quota,
	}
	svc := &mockNetService{
		dialFunc: func(context.Context, string, string) (net.Conn, error) {
			return conn, nil
		},
	}
	d := NewDispatcher(svc)
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	op := socketapi.NewPendingOperation()
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(parent, &socketapi.StartConnectCmd{Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	require.Equal(t, int32(1), quota.Load())
	cancel()
	select {
	case <-conn.entered:
	case <-time.After(3 * time.Second):
		t.Fatal("parent cancel did not start dispose")
	}
	require.Equal(t, int32(1), quota.Load())

	var taken io.Closer
	takeDone := make(chan struct{})
	closeDone := make(chan struct{})
	go func() {
		value, _, ready := op.Take()
		if ready {
			taken = value
		}
		close(takeDone)
	}()
	go func() {
		_ = op.Close()
		close(closeDone)
	}()

	select {
	case <-closeDone:
		t.Fatal("Close returned before physical dispose finished")
	case <-time.After(50 * time.Millisecond):
	}

	select {
	case <-takeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Take did not return")
	}
	require.Nil(t, taken)
	require.Equal(t, int32(1), quota.Load())

	select {
	case <-closeDone:
		t.Fatal("Close returned before physical dispose finished")
	default:
	}

	close(conn.release)
	select {
	case <-closeDone:
	case <-time.After(3 * time.Second):
		t.Fatal("Close did not join physical dispose")
	}
	require.Equal(t, int32(0), quota.Load())
	require.Equal(t, int32(1), conn.closes())
}

func TestDispatcher_HandleStartConnect_ClosedResourceOwnershipRace(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	for i := 0; i < 50; i++ {
		entered := make(chan struct{})
		late := newTrackedConn(t)
		d.netSvc = &mockNetService{
			dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
				close(entered)
				<-ctx.Done()
				return late, ctx.Err()
			},
		}
		op := socketapi.NewPendingOperation()
		recv := newCaptureReceiver()
		require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, uint64(i+1), recv))
		require.NoError(t, ackStart(t, recv).Err)
		<-entered

		var taken io.Closer
		var wg sync.WaitGroup
		wg.Add(2)
		go func() {
			defer wg.Done()
			_ = op.Close()
		}()
		go func() {
			defer wg.Done()
			value, _, ready := op.Take()
			if ready {
				taken = value
			}
		}()
		wg.Wait()
		if taken != nil {
			_ = taken.Close()
		}
		require.Equal(t, int32(1), late.closes())
	}
}

func TestDispatcher_HandleStartConnect_Success(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	svc := &mockNetService{
		dialFunc: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{
		Operation: op,
		Network:   "tcp",
		Address:   ln.Addr().String(),
	}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.NotNil(t, value)
	require.NoError(t, value.Close())
}

func TestDispatcher_HandleStartListen_Success(t *testing.T) {
	svc := &mockNetService{
		listenFunc: func(ctx context.Context, network, address string) (net.Listener, error) {
			return new(net.ListenConfig).Listen(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartListen(context.Background(), &socketapi.StartListenCmd{
		Operation: op,
		Network:   "tcp",
		Address:   "127.0.0.1:0",
	}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, takeErr)
	require.NotNil(t, value)
	require.NoError(t, value.Close())
}

func TestDispatcher_HandleStartConnect_DialError(t *testing.T) {
	boom := errors.New("connection refused")
	svc := &mockNetService{
		dialFunc: func(context.Context, string, string) (net.Conn, error) {
			return nil, boom
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()
	recv := newCaptureReceiver()
	require.NoError(t, d.handleStartConnect(context.Background(), &socketapi.StartConnectCmd{Operation: op}, 1, recv))
	require.NoError(t, ackStart(t, recv).Err)
	waitReady(t, op)
	value, takeErr, ready := op.Take()
	require.True(t, ready)
	require.Nil(t, value)
	require.ErrorIs(t, takeErr, boom)
}

func TestDispatcher_HandleConnectAndListenUnchanged(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	svc := &mockNetService{
		dialFunc: func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, address)
		},
		listenFunc: func(ctx context.Context, network, address string) (net.Listener, error) {
			return new(net.ListenConfig).Listen(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)

	connectRecv := newCaptureReceiver()
	require.NoError(t, d.handleConnect(context.Background(), &socketapi.ConnectCmd{Network: "tcp", Address: ln.Addr().String()}, 1, connectRecv))
	connectData, _ := connectRecv.wait()
	connectResult := connectData.(*socketapi.ConnectResult)
	require.NoError(t, connectResult.Err)
	require.NotNil(t, connectResult.Conn)
	_ = connectResult.Conn.Close()

	listenRecv := newCaptureReceiver()
	require.NoError(t, d.handleListen(context.Background(), &socketapi.ListenCmd{Network: "tcp", Address: "127.0.0.1:0"}, 2, listenRecv))
	listenData, _ := listenRecv.wait()
	listenResult := listenData.(*socketapi.ListenResult)
	require.NoError(t, listenResult.Err)
	require.NotNil(t, listenResult.Listener)
	_ = listenResult.Listener.Close()
}
