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
		{name: "context"}, {name: "request-close", direct: true}, {name: "kill-error", direct: true, killError: signalError},
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
			for _, want := range []int{int(syscall.SIGTERM), int(syscall.SIGKILL)} {
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
				} else if direct {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
				require.NotErrorIs(t, err, ErrShutdownTimeout)
			case <-time.After(time.Second):
				t.Fatal("proxy did not reap the child after escalation")
			}
		})
	}
}
