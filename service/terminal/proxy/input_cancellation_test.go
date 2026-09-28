// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"io"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

// The process ignores TERM and stops consuming input. Only KILL releases the
// pending write, so shutdown escalation must run independently of terminal I/O.
type stubbornInputProcess struct {
	killError error
	*shutdownProcess
	entered     chan struct{}
	released    chan struct{}
	releaseOnce sync.Once
}

func (p *stubbornInputProcess) WriteStdin([]byte) error {
	close(p.entered)
	<-p.released
	return io.ErrClosedPipe
}
func (p *stubbornInputProcess) Signal(sig int) error {
	p.signals <- sig
	if sig == int(syscall.SIGKILL) {
		p.releaseOnce.Do(func() { close(p.released) })
		return p.killError
	}
	return nil
}
func TestProxyCancellationEscalatesDuringBlockedInput(t *testing.T) {
	signalError := errors.New("kill response failed")
	for _, tc := range []struct {
		killError error
		name      string
		direct    bool
	}{
		{name: "context"},
		{name: "context-kill-error", killError: signalError},
		{name: "request-close", direct: true},
		{name: "kill-error", direct: true, killError: signalError},
	} {
		direct := tc.direct
		name := tc.name
		t.Run(name, func(t *testing.T) {
			reader, writer := io.Pipe()
			defer writer.Close()
			process := &stubbornInputProcess{
				shutdownProcess: &shutdownProcess{testProcess: &testProcess{stdout: reader}, wait: make(chan error, 1), signals: make(chan int, 4)},
				entered:         make(chan struct{}), released: make(chan struct{}), killError: tc.killError,
			}
			bridge, err := New(process, &testSurface{}, 10, 2)
			require.NoError(t, err)
			bridge.shutdownGrace = 20 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			events := make(chan ttyapi.Event, 1)
			done := make(chan error, 1)
			completed := false
			go func() { done <- bridge.Run(ctx, events) }()
			defer func() {
				if completed {
					return
				}
				process.releaseOnce.Do(func() { close(process.released) })
				process.wait <- errors.New("killed")
				_ = writer.Close()
				select {
				case <-done:
				case <-time.After(time.Second):
				}
			}()
			events <- ttyapi.Event{Type: "paste", Paste: "blocked"}
			select {
			case <-process.entered:
			case <-time.After(time.Second):
				t.Fatal("write not entered")
			}
			if direct {
				bridge.RequestClose()
			} else {
				cancel()
			}
			for _, want := range []int{int(syscall.SIGHUP), int(syscall.SIGKILL)} {
				select {
				case got := <-process.signals:
					require.Equal(t, want, got)
				case <-time.After(time.Second):
					t.Fatalf("shutdown did not deliver signal %d during blocked input", want)
				}
			}
			process.wait <- errors.New("killed")
			require.NoError(t, writer.Close())
			select {
			case err := <-done:
				completed = true
				if tc.killError != nil {
					require.ErrorIs(t, err, tc.killError)
					require.Equal(t, 1, countCause(err, tc.killError), "the kill failure is reported once")
				}
				switch {
				case direct:
					if tc.killError == nil {
						require.NoError(t, err)
					}
				case tc.killError == nil:
					require.Equal(t, context.Canceled, err, "shutdown carries the cancellation cause itself")
				default:
					require.ErrorIs(t, err, context.Canceled)
				}
				if !direct {
					require.Equal(t, 1, countCause(err, context.Canceled), "the cancellation cause is reported once")
				}
				require.NotErrorIs(t, err, ErrShutdownTimeout)
			case <-time.After(time.Second):
				t.Fatal("proxy did not reap the child after escalation")
			}
		})
	}
}

// countCause reports how many times target appears in err's tree, walking both
// single-error wrapping and errors.Join branches.
func countCause(err, target error) int {
	if err == nil {
		return 0
	}
	if joined, ok := err.(interface{ Unwrap() []error }); ok {
		total := 0
		for _, inner := range joined.Unwrap() {
			total += countCause(inner, target)
		}
		return total
	}
	if wrapped, ok := err.(interface{ Unwrap() error }); ok {
		return countCause(wrapped.Unwrap(), target)
	}
	if errors.Is(err, target) {
		return 1
	}
	return 0
}

// exitCancelSurface cancels the run context while the proxy renders the final
// frame of a child that has already exited.
type exitCancelSurface struct {
	cancel    context.CancelFunc
	presented chan struct{}
	once      sync.Once
}

func (s *exitCancelSurface) Present(ttyapi.Frame) (ttyapi.PresentStats, error) {
	s.once.Do(func() {
		s.cancel()
		close(s.presented)
	})
	return ttyapi.PresentStats{}, nil
}

func (*exitCancelSurface) Invalidate()  {}
func (*exitCancelSurface) Close() error { return nil }

// A cancellation that arrives once the child has already finished reports the
// child's own outcome, matching the completion contract Run applies to a close
// request that races normal exit.
func TestProxyCancellationAfterChildExitReportsChildOutcome(t *testing.T) {
	reader, writer := io.Pipe()
	process := &shutdownProcess{
		testProcess: &testProcess{stdout: reader, input: make(chan []byte, 1)},
		wait:        make(chan error, 1),
		signals:     make(chan int, 4),
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	surface := &exitCancelSurface{cancel: cancel, presented: make(chan struct{})}
	bridge, err := New(process, surface, 10, 2)
	require.NoError(t, err)
	done := make(chan error, 1)
	go func() { done <- bridge.Run(ctx, make(chan ttyapi.Event)) }()

	process.wait <- nil
	require.NoError(t, writer.Close())
	select {
	case err := <-done:
		require.NoError(t, err, "cancellation after the child exits is not a failure of the run")
	case <-time.After(time.Second):
		t.Fatal("proxy did not finish after the child exited")
	}
	select {
	case <-surface.presented:
	default:
		t.Fatal("cancellation did not reach the run before it returned")
	}
}
