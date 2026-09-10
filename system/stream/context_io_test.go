// SPDX-License-Identifier: MPL-2.0
package stream

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
)

type interruptibleStream struct {
	entered chan struct{}
	calls   atomic.Int32
}

func (*interruptibleStream) Read([]byte) (int, error) { return 0, errors.New("non-context read used") }
func (*interruptibleStream) Write([]byte) (int, error) {
	return 0, errors.New("non-context write used")
}
func (*interruptibleStream) Close() error { return nil }
func (s *interruptibleStream) ReadContext(ctx context.Context, _ []byte) (int, error) {
	s.calls.Add(1)
	close(s.entered)
	<-ctx.Done()
	return 0, ctx.Err()
}
func (s *interruptibleStream) WriteContext(ctx context.Context, _ []byte) (int, error) {
	s.calls.Add(1)
	close(s.entered)
	<-ctx.Done()
	return 1, ctx.Err()
}

func TestDispatcherInterruptsContextIO(t *testing.T) {
	for _, op := range []string{"read", "write"} {
		for _, end := range []string{"caller", "shutdown"} {
			t.Run(op+"/"+end, func(t *testing.T) {
				ctx, store := setupTestContext()
				defer store.Close()
				callCtx, cancel := context.WithCancel(ctx)
				defer cancel()
				d := NewDispatcher(WithWorkers(1))
				if err := d.Start(ctx); err != nil {
					t.Fatal(err)
				}
				stopped := false
				defer func() {
					cancel()
					if !stopped {
						d.Stop(context.Background())
					}
				}()
				s := &interruptibleStream{entered: make(chan struct{})}
				id := Insert(resource.GetTable(ctx), s)
				var cmd dispatcher.Command = streamapi.ReadCmd{StreamID: id, Size: 8}
				if op == "write" {
					cmd = streamapi.WriteCmd{StreamID: id, Data: []byte("payload")}
				}
				recv := newSyncReceiver()
				d.submit(callCtx, cmd, 1, recv)
				select {
				case <-s.entered:
				case <-time.After(time.Second):
					t.Fatal("context IO did not start")
				}
				if end == "caller" {
					cancel()
				} else {
					done := make(chan struct{})
					go func() { d.Stop(context.Background()); close(done) }()
					select {
					case <-done:
						stopped = true
					case <-time.After(time.Second):
						t.Fatal("shutdown did not interrupt IO")
					}
				}
				recv.wait(t)
				if !errors.Is(recv.err, context.Canceled) {
					t.Fatalf("operation error: %v", recv.err)
				}
				if op == "write" && recv.data != int64(1) {
					t.Fatalf("partial count lost: %v", recv.data)
				}
				if s.calls.Load() != 1 {
					t.Fatalf("operation retried: %d calls", s.calls.Load())
				}
			})
		}
	}
}

func TestWriteContextPreservesPartialCount(t *testing.T) {
	table := resource.NewTable()
	defer table.Close()
	s := &interruptibleStream{entered: make(chan struct{})}
	id := Insert(table, s)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() { <-s.entered; cancel() }()
	n, err := WriteContext(ctx, table, id, []byte("payload"))
	if n != 1 || !errors.Is(err, context.Canceled) {
		t.Fatalf("partial result: n=%d err=%v", n, err)
	}
	if s.calls.Load() != 1 {
		t.Fatal("partial write retried")
	}
}
