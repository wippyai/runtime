// SPDX-License-Identifier: MPL-2.0

package sserelay

import (
	"context"
	"net/http"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/registry"
	"go.uber.org/zap"
)

func TestSessionConcurrent1000(t *testing.T) {
	runConcurrentSessions(t, 1000)
}

func TestSessionConcurrent5000(t *testing.T) {
	if testing.Short() && os.Getenv("SSE_RELAY_STRESS") != "1" {
		t.Skip("set SSE_RELAY_STRESS=1 to run 5000-connection stress test in short mode")
	}
	runConcurrentSessions(t, 5000)
}

func runConcurrentSessions(t *testing.T, n int) {
	host := newMockHost()
	node := newMockNode()
	topo := newMockTopology()
	tc := &mockTranscoder{}
	pg := &testPIDGen{}

	var wg sync.WaitGroup
	errCh := make(chan error, n)

	start := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start

			s, err := NewSession(
				context.Background(),
				RelayCommand{HeartbeatInterval: "0s"},
				registry.NewID("app", "server"),
				host,
				node,
				topo,
				tc,
				pg,
				zap.NewNop(),
			)
			if err != nil {
				errCh <- err
				return
			}

			reqCtx, cancelReq := context.WithCancel(context.Background())
			done := make(chan error, 1)
			go func() {
				done <- s.Serve(reqCtx, &discardWriter{h: make(http.Header)})
			}()

			time.Sleep(2 * time.Millisecond)
			cancelReq()

			select {
			case err := <-done:
				if err != nil {
					errCh <- err
				}
			case <-time.After(5 * time.Second):
				errCh <- context.DeadlineExceeded
			}
		}()
	}

	close(start)

	done := make(chan struct{})
	go func() {
		wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(60 * time.Second):
		t.Fatalf("timed out waiting for %d concurrent sessions", n)
	}

	close(errCh)
	for err := range errCh {
		require.NoError(t, err)
	}
}

type discardWriter struct {
	h http.Header
}

func (w *discardWriter) Header() http.Header {
	return w.h
}

func (w *discardWriter) Write(p []byte) (int, error) {
	return len(p), nil
}

func (w *discardWriter) WriteHeader(_ int) {}

func (w *discardWriter) Flush() {}
