// SPDX-License-Identifier: MPL-2.0
package socket

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/dispatcher"
	socketapi "github.com/wippyai/runtime/api/socket"
)

func TestHandleStreamWait_NilCommandSafe(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	for _, cmd := range []dispatcher.Command{nil, (*socketapi.StreamWaitCmd)(nil), &socketapi.StreamWaitCmd{}} {
		recv := newCaptureReceiver()
		err := d.handleStreamWait(context.Background(), cmd, 1, recv)
		require.Error(t, err)
		select {
		case <-recv.done:
			t.Fatal("invalid stream wait completed")
		default:
		}
	}
}

func TestHandleStreamWait_ZeroDeadlineKeepsCallerContext(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()
	seen := make(chan context.Context, 1)
	require.NoError(t, d.handleStreamWait(context.Background(), &socketapi.StreamWaitCmd{
		Run: func(ctx context.Context) any {
			seen <- ctx
			return "ok"
		},
	}, 1, recv))
	ctx := recvCtx(t, seen)
	_, ok := ctx.Deadline()
	require.False(t, ok)
	data, err := waitRecv(t, recv)
	require.NoError(t, err)
	require.Equal(t, "ok", data)
}

func TestHandleStreamWait_DeadlineCompletesWhileParentAlive(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()
	deadline := time.Now().Add(25 * time.Millisecond)
	seen := make(chan context.Context, 1)
	require.NoError(t, d.handleStreamWait(context.Background(), &socketapi.StreamWaitCmd{
		Deadline: deadline,
		Run: func(ctx context.Context) any {
			seen <- ctx
			<-ctx.Done()
			return ctx.Err()
		},
	}, 1, recv))
	jobCtx := recvCtx(t, seen)
	got, ok := jobCtx.Deadline()
	require.True(t, ok)
	require.Equal(t, deadline, got)
	data, err := waitRecv(t, recv)
	require.NoError(t, err)
	require.ErrorIs(t, data.(error), context.DeadlineExceeded)
	require.NoError(t, context.Background().Err())
}

func TestHandleStreamWait_ParentCancelSuppressesCompletion(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()
	parent, cancel := context.WithCancel(context.Background())
	defer cancel()
	entered := make(chan struct{})
	var ran atomic.Bool
	require.NoError(t, d.handleStreamWait(parent, &socketapi.StreamWaitCmd{
		Deadline: time.Now().Add(time.Second),
		Run: func(ctx context.Context) any {
			close(entered)
			<-ctx.Done()
			ran.Store(true)
			return ctx.Err()
		},
	}, 1, recv))
	select {
	case <-entered:
	case <-time.After(3 * time.Second):
		t.Fatal("stream wait worker did not start")
	}
	cancel()
	select {
	case <-recv.done:
		t.Fatal("canceled parent must not complete yield")
	case <-time.After(100 * time.Millisecond):
	}
	require.True(t, ran.Load())
}

func TestHandleStreamWait_SuccessNotRewrittenAfterDeadline(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()
	deadline := time.Now().Add(40 * time.Millisecond)
	require.NoError(t, d.handleStreamWait(context.Background(), &socketapi.StreamWaitCmd{
		Deadline: deadline,
		Run:      func(context.Context) any { return "ok" },
	}, 1, recv))
	data, err := waitRecv(t, recv)
	require.NoError(t, err)
	require.Equal(t, "ok", data)
	waitPast(t, deadline)
	select {
	case <-recv.done:
	default:
		t.Fatal("success completion was lost")
	}
	data, err = recv.wait()
	require.NoError(t, err)
	require.Equal(t, "ok", data)
}

func TestHandleStreamWait_PastDeadlineExpiresImmediately(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()
	seen := make(chan context.Context, 1)
	require.NoError(t, d.handleStreamWait(context.Background(), &socketapi.StreamWaitCmd{
		Deadline: time.Now().Add(-time.Millisecond),
		Run: func(ctx context.Context) any {
			seen <- ctx
			return ctx.Err()
		},
	}, 1, recv))
	jobCtx := recvCtx(t, seen)
	require.ErrorIs(t, jobCtx.Err(), context.DeadlineExceeded)
	data, err := waitRecv(t, recv)
	require.NoError(t, err)
	require.ErrorIs(t, data.(error), context.DeadlineExceeded)
}

func waitRecv(t *testing.T, recv *captureReceiver) (any, error) {
	t.Helper()
	select {
	case <-recv.done:
		return recv.wait()
	case <-time.After(3 * time.Second):
		t.Fatal("stream wait did not complete")
	}
	return nil, nil
}

func recvCtx(t *testing.T, seen <-chan context.Context) context.Context {
	t.Helper()
	select {
	case ctx := <-seen:
		return ctx
	case <-time.After(3 * time.Second):
		t.Fatal("stream wait did not publish context")
	}
	return nil
}
