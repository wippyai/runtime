// SPDX-License-Identifier: MPL-2.0

package socket

import (
	"context"
	"errors"
	"net"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	socketapi "github.com/wippyai/runtime/api/socket"
)

type trackListener struct {
	net.Listener
	acceptStarted chan struct{}
	acceptDone    chan struct{}
	closed        chan struct{}
	acceptOnce    sync.Once
	closeOnce     sync.Once
}

func newTrackListener(inner net.Listener) *trackListener {
	return &trackListener{
		Listener:      inner,
		acceptStarted: make(chan struct{}),
		acceptDone:    make(chan struct{}),
		closed:        make(chan struct{}),
	}
}

func (l *trackListener) Accept() (net.Conn, error) {
	l.acceptOnce.Do(func() {
		close(l.acceptStarted)
	})
	conn, err := l.Listener.Accept()
	select {
	case <-l.acceptDone:
	default:
		close(l.acceptDone)
	}
	return conn, err
}

func (l *trackListener) Close() error {
	l.closeOnce.Do(func() {
		close(l.closed)
	})
	return l.Listener.Close()
}

func TestDispatcher_HandleAccept_DeterministicBlockedCancellation(t *testing.T) {
	rawLn, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)

	ln := newTrackListener(rawLn)
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	cmd := &socketapi.AcceptCmd{Listener: ln}
	err = d.handleAccept(ctx, cmd, 1, recv)
	require.NoError(t, err)

	// Wait deterministically until Accept() is genuinely blocked.
	<-ln.acceptStarted

	// Cancel context while Accept is blocked.
	cancel()

	// Cancellation hook must close the listener, unblocking Accept().
	select {
	case <-ln.closed:
	case <-time.After(3 * time.Second):
		t.Fatal("cancellation hook did not close listener within timeout")
	}

	select {
	case <-ln.acceptDone:
	case <-time.After(3 * time.Second):
		t.Fatal("blocked Accept() did not unblock after cancellation within timeout")
	}

	// ResultReceiver must not receive completion on canceled context.
	select {
	case <-recv.done:
		t.Fatal("receiver.CompleteYield should not have been called on canceled context")
	case <-time.After(100 * time.Millisecond):
	}
}

func TestDispatcher_HandleAccept_SuccessfulAcceptKeepsListenerUsable(t *testing.T) {
	ln, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer ln.Close()

	d := NewDispatcher(&mockNetService{})
	ctx := context.Background()

	// First accept
	recv1 := newCaptureReceiver()
	cmd1 := &socketapi.AcceptCmd{Listener: ln}
	err = d.handleAccept(ctx, cmd1, 1, recv1)
	require.NoError(t, err)

	client1, err := (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	require.NoError(t, err)
	defer client1.Close()

	data1, err1 := recv1.wait()
	require.NoError(t, err1)
	result1 := data1.(*socketapi.AcceptResult)
	require.NoError(t, result1.Err)
	require.NotNil(t, result1.Conn)
	defer result1.Conn.Close()

	// Verify listener is still usable by performing a second accept on the same listener
	recv2 := newCaptureReceiver()
	cmd2 := &socketapi.AcceptCmd{Listener: ln}
	err = d.handleAccept(ctx, cmd2, 2, recv2)
	require.NoError(t, err)

	client2, err := (&net.Dialer{}).DialContext(ctx, "tcp", ln.Addr().String())
	require.NoError(t, err)
	defer client2.Close()

	data2, err2 := recv2.wait()
	require.NoError(t, err2)
	result2 := data2.(*socketapi.AcceptResult)
	require.NoError(t, result2.Err)
	require.NotNil(t, result2.Conn)
	defer result2.Conn.Close()
}

type trackConn struct {
	net.Conn
	closed atomic.Bool
}

func (c *trackConn) Close() error {
	c.closed.Store(true)
	return c.Conn.Close()
}

type trackConnListener struct {
	net.Listener
	acceptedConns []*trackConn
	mu            sync.Mutex
}

func newTrackConnListener(inner net.Listener) *trackConnListener {
	return &trackConnListener{Listener: inner}
}

func (l *trackConnListener) Accept() (net.Conn, error) {
	conn, err := l.Listener.Accept()
	if err != nil {
		return nil, err
	}
	tc := &trackConn{Conn: conn}
	l.mu.Lock()
	l.acceptedConns = append(l.acceptedConns, tc)
	l.mu.Unlock()
	return tc, nil
}

func (l *trackConnListener) getAcceptedConns() []*trackConn {
	l.mu.Lock()
	defer l.mu.Unlock()
	copied := make([]*trackConn, len(l.acceptedConns))
	copy(copied, l.acceptedConns)
	return copied
}

func TestDispatcher_HandleAccept_CancellationRaceResourceOwnership(t *testing.T) {
	d := NewDispatcher(&mockNetService{})

	for i := 0; i < 50; i++ {
		rawLn, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
		require.NoError(t, err)

		ln := newTrackConnListener(rawLn)
		ctx, cancel := context.WithCancel(context.Background())
		recv := newCaptureReceiver()

		cmd := &socketapi.AcceptCmd{Listener: ln}
		err = d.handleAccept(ctx, cmd, uint64(i+1), recv)
		require.NoError(t, err)

		// Race dial vs cancel
		var wg sync.WaitGroup
		wg.Add(2)

		var clientConn net.Conn
		go func() {
			defer wg.Done()
			c, dialErr := (&net.Dialer{}).DialContext(context.Background(), "tcp", rawLn.Addr().String())
			if dialErr == nil {
				clientConn = c
			}
		}()

		go func() {
			defer wg.Done()
			cancel()
		}()

		wg.Wait()
		if clientConn != nil {
			_ = clientConn.Close()
		}

		// Wait briefly for handler goroutine to finish
		select {
		case <-recv.done:
			// Normal completion won the race: ResultReceiver owns the conn
			data, recvErr := recv.wait()
			require.NoError(t, recvErr)
			result := data.(*socketapi.AcceptResult)
			if result.Conn != nil {
				_ = result.Conn.Close()
			}
		case <-time.After(50 * time.Millisecond):
			// Cancellation won: ResultReceiver was not called.
			// Any accepted conn must have been closed by the handler.
			for _, tc := range ln.getAcceptedConns() {
				// Wait briefly if close is in progress
				require.Eventually(t, func() bool {
					return tc.closed.Load()
				}, 500*time.Millisecond, 10*time.Millisecond, "accepted conn must be closed when ctx is canceled")
			}
		}

		_ = rawLn.Close()
	}
}

func TestDispatcher_HandleAccept_AlreadyCanceledContext(t *testing.T) {
	rawLn, err := new(net.ListenConfig).Listen(context.Background(), "tcp", "127.0.0.1:0")
	require.NoError(t, err)
	defer rawLn.Close()

	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()

	ctx, cancel := context.WithCancel(context.Background())
	cancel() // Already canceled

	cmd := &socketapi.AcceptCmd{Listener: rawLn}
	err = d.handleAccept(ctx, cmd, 1, recv)
	require.NoError(t, err)

	// Receiver should never be called
	select {
	case <-recv.done:
		t.Fatal("receiver.CompleteYield should not be called for already canceled context")
	case <-time.After(100 * time.Millisecond):
	}

	// Listener should have been closed by the cancellation hook
	_, acceptErr := rawLn.Accept()
	assert.True(t, errors.Is(acceptErr, net.ErrClosed) || acceptErr != nil, "listener should be closed")
}

func TestDispatcher_HandleAccept_NilListener(t *testing.T) {
	d := NewDispatcher(&mockNetService{})
	recv := newCaptureReceiver()

	cmd := &socketapi.AcceptCmd{Listener: nil}
	err := d.handleAccept(context.Background(), cmd, 1, recv)
	require.NoError(t, err)

	data, recvErr := recv.wait()
	require.NoError(t, recvErr)
	result := data.(*socketapi.AcceptResult)
	require.Error(t, result.Err)
	assert.Contains(t, result.Err.Error(), "nil listener")
}
