// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"io"
	"net"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
)

const startDeadline = 25 * time.Millisecond

func TestDispatcher_StartDeadline_BlockedJobExpires(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan struct{})
			seen := make(chan context.Context, 1)
			d := NewDispatcher(blockedStartService(kind, entered, seen, nil))
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(context.Background(), t, d, op, kind, startDeadline)
			require.NoError(t, ack.Err)
			require.False(t, op.Ready())

			jobCtx := waitJobCtx(t, entered, seen)
			deadline, ok := jobCtx.Deadline()
			require.True(t, ok)
			require.False(t, deadline.IsZero())

			waitReady(t, op)
			<-jobCtx.Done()
			require.ErrorIs(t, jobCtx.Err(), context.DeadlineExceeded)

			value, err, ready := op.Take()
			require.True(t, ready)
			require.Nil(t, value)
			require.ErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestDispatcher_StartDeadline_LateResourceClosedOnce(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan struct{})
			seen := make(chan context.Context, 1)
			late, done := newLateStartResource(t, kind)
			d := NewDispatcher(blockedStartService(kind, entered, seen, late))
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(context.Background(), t, d, op, kind, startDeadline)
			require.NoError(t, ack.Err)
			<-entered

			waitReady(t, op)
			select {
			case <-done:
			case <-time.After(3 * time.Second):
				t.Fatal("late start resource was not closed")
			}
			require.Equal(t, int32(1), lateCloses(late))

			value, err, ready := op.Take()
			require.True(t, ready)
			require.Nil(t, value)
			require.ErrorIs(t, err, context.DeadlineExceeded)
			require.Equal(t, int32(1), lateCloses(late))
		})
	}
}

func TestDispatcher_StartDeadline_ParentCancelWins(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			entered := make(chan struct{})
			seen := make(chan context.Context, 1)
			d := NewDispatcher(blockedStartService(kind, entered, seen, nil))
			parent, cancel := context.WithCancel(context.Background())
			defer cancel()
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(parent, t, d, op, kind, 5*time.Second)
			require.NoError(t, ack.Err)
			jobCtx := waitJobCtx(t, entered, seen)
			_, ok := jobCtx.Deadline()
			require.True(t, ok)

			cancel()
			waitReady(t, op)
			<-jobCtx.Done()
			require.ErrorIs(t, jobCtx.Err(), context.Canceled)
			require.NotErrorIs(t, jobCtx.Err(), context.DeadlineExceeded)

			value, err, ready := op.Take()
			require.True(t, ready)
			require.Nil(t, value)
			require.ErrorIs(t, err, context.Canceled)
			require.NotErrorIs(t, err, context.DeadlineExceeded)
		})
	}
}

func TestDispatcher_StartDeadline_CompletedSurvivesUntilTake(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			seen := make(chan context.Context, 1)
			resource := newImmediateStartResource(t, kind)
			d := NewDispatcher(immediateStartService(kind, seen, resource))
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(context.Background(), t, d, op, kind, startDeadline)
			require.NoError(t, ack.Err)
			waitReady(t, op)

			jobCtx := recvJobCtx(t, seen)
			deadline, ok := jobCtx.Deadline()
			require.True(t, ok)
			waitPast(t, deadline)

			value, err, ready := op.Take()
			require.True(t, ready)
			require.NoError(t, err)
			require.Equal(t, resource, value)
			require.Equal(t, int32(0), lateCloses(resource))
			require.NoError(t, value.Close())
		})
	}
}

func TestDispatcher_StartDeadline_ListenerAcceptsAfterStartDeadline(t *testing.T) {
	seen := make(chan context.Context, 1)
	svc := &mockNetService{
		listenFunc: func(ctx context.Context, network, address string) (net.Listener, error) {
			seen <- ctx
			return new(net.ListenConfig).Listen(ctx, network, address)
		},
	}
	d := NewDispatcher(svc)
	op := socketapi.NewPendingOperation()
	defer op.Close()

	ack := ackStartKind(context.Background(), t, d, op, "listen", startDeadline)
	require.NoError(t, ack.Err)
	waitReady(t, op)

	jobCtx := recvJobCtx(t, seen)
	deadline, ok := jobCtx.Deadline()
	require.True(t, ok)
	waitPast(t, deadline)

	value, err, ready := op.Take()
	require.True(t, ready)
	require.NoError(t, err)
	ln, ok := value.(net.Listener)
	require.True(t, ok)
	defer ln.Close()

	accepted := make(chan net.Conn, 1)
	acceptErr := make(chan error, 1)
	go func() {
		conn, err := ln.Accept()
		if err != nil {
			acceptErr <- err
			return
		}
		accepted <- conn
	}()

	dialer := &net.Dialer{}
	client, err := dialer.DialContext(context.Background(), ln.Addr().Network(), ln.Addr().String())
	require.NoError(t, err)
	defer client.Close()

	select {
	case conn := <-accepted:
		require.NotNil(t, conn)
		_ = conn.Close()
	case err := <-acceptErr:
		t.Fatalf("accept after start deadline: %v", err)
	case <-time.After(3 * time.Second):
		t.Fatal("accept after start deadline did not complete")
	}
}

func TestDispatcher_StartDeadline_InvalidTimeoutDoesNotStart(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			var starts atomic.Int32
			d := NewDispatcher(countingStartService(t, kind, &starts))
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(context.Background(), t, d, op, kind, -time.Millisecond)
			require.ErrorIs(t, ack.Err, socketapi.ErrInvalidTimeout)
			require.Equal(t, int32(0), starts.Load())
			require.False(t, op.Ready())

			_, started := op.Start(context.Background())
			require.True(t, started)
			op.Complete(nil, io.EOF)
		})
	}
}

func TestDispatcher_StartDeadline_ZeroKeepsCallerContext(t *testing.T) {
	for _, kind := range []string{"connect", "listen"} {
		t.Run(kind, func(t *testing.T) {
			seen := make(chan context.Context, 1)
			resource := newImmediateStartResource(t, kind)
			d := NewDispatcher(immediateStartService(kind, seen, resource))
			op := socketapi.NewPendingOperation()
			defer op.Close()

			ack := ackStartKind(context.Background(), t, d, op, kind, 0)
			require.NoError(t, ack.Err)
			waitReady(t, op)

			jobCtx := recvJobCtx(t, seen)
			_, ok := jobCtx.Deadline()
			require.False(t, ok)

			value, err, ready := op.Take()
			require.True(t, ready)
			require.NoError(t, err)
			require.Equal(t, resource, value)
			require.NoError(t, value.Close())
		})
	}
}

func ackStartKind(ctx context.Context, t *testing.T, d *Dispatcher, op *socketapi.PendingOperation, kind string, timeout time.Duration) *socketapi.StartResult {
	t.Helper()
	recv := newCaptureReceiver()
	switch kind {
	case "connect":
		require.NoError(t, d.handleStartConnect(ctx, &socketapi.StartConnectCmd{
			Operation: op,
			Network:   "tcp",
			Address:   "127.0.0.1:1",
			Timeout:   timeout,
		}, 1, recv))
	case "listen":
		require.NoError(t, d.handleStartListen(ctx, &socketapi.StartListenCmd{
			Operation: op,
			Network:   "tcp",
			Address:   "127.0.0.1:0",
			Timeout:   timeout,
		}, 1, recv))
	default:
		t.Fatalf("unknown start kind %q", kind)
	}
	return ackStart(t, recv)
}

func blockedStartService(kind string, entered chan struct{}, seen chan context.Context, late io.Closer) *mockNetService {
	wait := func(ctx context.Context) (io.Closer, error) {
		seen <- ctx
		close(entered)
		<-ctx.Done()
		if late != nil {
			return late, nil
		}
		return nil, ctx.Err()
	}
	switch kind {
	case "connect":
		return &mockNetService{
			dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
				value, err := wait(ctx)
				if value == nil {
					return nil, err
				}
				return value.(net.Conn), err
			},
		}
	default:
		return &mockNetService{
			listenFunc: func(ctx context.Context, _, _ string) (net.Listener, error) {
				value, err := wait(ctx)
				if value == nil {
					return nil, err
				}
				return value.(net.Listener), err
			},
		}
	}
}

func immediateStartService(kind string, seen chan context.Context, resource io.Closer) *mockNetService {
	switch kind {
	case "connect":
		return &mockNetService{
			dialFunc: func(ctx context.Context, _, _ string) (net.Conn, error) {
				seen <- ctx
				return resource.(net.Conn), nil
			},
		}
	default:
		return &mockNetService{
			listenFunc: func(ctx context.Context, _, _ string) (net.Listener, error) {
				seen <- ctx
				return resource.(net.Listener), nil
			},
		}
	}
}

func countingStartService(t *testing.T, kind string, starts *atomic.Int32) *mockNetService {
	t.Helper()
	switch kind {
	case "connect":
		return &mockNetService{
			dialFunc: func(context.Context, string, string) (net.Conn, error) {
				starts.Add(1)
				t.Fatal("negative timeout must not start connect")
				return nil, nil
			},
		}
	default:
		return &mockNetService{
			listenFunc: func(context.Context, string, string) (net.Listener, error) {
				starts.Add(1)
				t.Fatal("negative timeout must not start listen")
				return nil, nil
			},
		}
	}
}

func newImmediateStartResource(t *testing.T, kind string) io.Closer {
	t.Helper()
	if kind == "connect" {
		return newTrackedConn(t)
	}
	return newTrackedListener(t)
}

func newLateStartResource(t *testing.T, kind string) (io.Closer, <-chan struct{}) {
	t.Helper()
	if kind == "connect" {
		conn := newTrackedConn(t)
		return conn, conn.done
	}
	ln := newTrackedListener(t)
	return ln, ln.done
}

func lateCloses(value io.Closer) int32 {
	switch c := value.(type) {
	case *trackedConn:
		return c.closes()
	case *trackedListener:
		return c.closes()
	default:
		return -1
	}
}

func waitJobCtx(t *testing.T, entered <-chan struct{}, seen <-chan context.Context) context.Context {
	t.Helper()
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("start job did not run")
	}
	return recvJobCtx(t, seen)
}

func recvJobCtx(t *testing.T, seen <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-seen:
		return ctx
	case <-time.After(3 * time.Second):
		t.Fatal("start job did not publish context")
	}
	return nil
}

func waitPast(t *testing.T, deadline time.Time) {
	t.Helper()
	d := time.Until(deadline) + 5*time.Millisecond
	if d <= 0 {
		return
	}
	timer := time.NewTimer(d)
	defer timer.Stop()
	select {
	case <-timer.C:
	case <-time.After(3 * time.Second):
		t.Fatal("timed out waiting past start deadline")
	}
}
