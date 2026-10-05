// SPDX-License-Identifier: MPL-2.0

package proxy

import (
	"context"
	"errors"
	"io"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

func TestResponseQueueCloseReleasesFullProducer(t *testing.T) {
	var queue responseQueue
	queue.init()
	t.Cleanup(queue.close)
	queued := []byte(strings.Repeat("x", responseQueueLimit))
	queue.push(queued)
	done := make(chan struct{})
	go func() {
		queue.push([]byte("discarded"))
		close(done)
	}()
	select {
	case <-done:
		t.Fatal("full queue did not apply backpressure")
	case <-time.After(10 * time.Millisecond):
	}
	queue.close()
	queue.close() // Independent shutdown owners may both retire the queue.
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("closing a full queue did not release its producer")
	}
	data, ok := queue.next()
	require.True(t, ok)
	require.Equal(t, queued, data, "already queued replies remain drainable")
	queue.push([]byte("also discarded"))
	_, ok = queue.next()
	require.False(t, ok, "closed queue must not accept new replies")
}

type failingResponseProcess struct {
	err error
	*testProcess
	entered chan struct{}
	release chan struct{}
}

func (p *failingResponseProcess) WriteStdin([]byte) error {
	close(p.entered)
	<-p.release
	return p.err
}

func TestCopyResponsesFailureReleasesFullProducer(t *testing.T) {
	for _, failure := range []error{io.ErrClosedPipe, errors.New("PTY input failed")} {
		t.Run(failure.Error(), func(t *testing.T) {
			process := &failingResponseProcess{
				testProcess: &testProcess{}, err: failure,
				entered: make(chan struct{}), release: make(chan struct{}),
			}
			bridge, err := New(process, &testSurface{}, 10, 2)
			require.NoError(t, err)
			t.Cleanup(bridge.responses.close)
			bridge.responses.push([]byte("first reply"))
			responseDone := make(chan error, 1)
			go bridge.copyResponses(responseDone)
			select {
			case <-process.entered:
			case <-time.After(time.Second):
				t.Fatal("response writer did not enter")
			}
			bridge.responses.push(make([]byte, responseQueueLimit))
			producerDone := make(chan struct{})
			go func() {
				bridge.responses.push([]byte("last reply"))
				close(producerDone)
			}()
			close(process.release)
			select {
			case err := <-responseDone:
				require.ErrorIs(t, err, failure)
			case <-time.After(time.Second):
				t.Fatal("response writer did not report its failure")
			}
			select {
			case <-producerDone:
			case <-time.After(time.Second):
				t.Fatal("failed response writer stranded a full-queue producer")
			}
		})
	}
}

// A reply flood can block the parser under screenMu. Shutdown must release it
// independently of Run, which may itself be waiting for that lock on resize.
func TestProxyShutdownDuringSaturatedResponses(t *testing.T) {
	for _, direct := range []bool{false, true} {
		name := "context"
		if direct {
			name = "request-close"
		}
		t.Run(name, func(t *testing.T) {
			process := &stubbornInputProcess{
				shutdownProcess: &shutdownProcess{
					testProcess: &testProcess{},
					wait:        make(chan error, 1), signals: make(chan int, 4),
				},
				entered: make(chan struct{}), released: make(chan struct{}),
			}
			bridge, err := New(process, &testSurface{}, 10, 2)
			require.NoError(t, err)
			_, err = bridge.writeOutput([]byte("\x1b[c"))
			require.NoError(t, err)
			require.Positive(t, bridge.responses.size)
			reply, ok := bridge.responses.next()
			require.True(t, ok)
			// One in-flight reply, a full queue, and a blocked producer; avoid
			// unrelated output draining against the short shutdown grace.
			queries := responseQueueLimit/len(reply) + 3
			process.stdout = io.NopCloser(strings.NewReader(strings.Repeat("\x1b[c", queries)))
			bridge.shutdownGrace = 100 * time.Millisecond
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			events := make(chan ttyapi.Event)
			done := make(chan error, 1)
			completed := false
			go func() { done <- bridge.Run(ctx, events) }()
			t.Cleanup(func() {
				bridge.responses.close()
				process.releaseOnce.Do(func() { close(process.released) })
				select {
				case process.wait <- nil:
				default:
				}
				if !completed {
					select {
					case <-done:
					case <-time.After(time.Second):
					}
				}
			})
			select {
			case <-process.entered:
			case <-time.After(time.Second):
				t.Fatal("query response did not reach child input")
			}
			require.Eventually(t, func() bool {
				bridge.responses.mu.Lock()
				defer bridge.responses.mu.Unlock()
				return bridge.responses.size >= responseQueueLimit
			}, time.Second, time.Millisecond, "real terminal queries must saturate the reply queue")
			if bridge.screenMu.TryLock() {
				bridge.screenMu.Unlock()
				t.Fatal("saturated parser unexpectedly released the screen lock")
			}
			select {
			case events <- ttyapi.Event{Type: "resize", Width: 20, Height: 4}:
			case <-time.After(time.Second):
				t.Fatal("Run did not admit resize while the parser was saturated")
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
					t.Fatalf("shutdown did not deliver signal %d", want)
				}
			}
			process.wait <- errors.New("killed")
			select {
			case err := <-done:
				completed = true
				if direct {
					require.NoError(t, err)
				} else {
					require.ErrorIs(t, err, context.Canceled)
				}
				require.NotErrorIs(t, err, ErrShutdownTimeout)
			case <-time.After(time.Second):
				t.Fatal("Run remained blocked after shutdown and child reap")
			}
		})
	}
}
