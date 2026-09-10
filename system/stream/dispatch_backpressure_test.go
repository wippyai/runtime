// SPDX-License-Identifier: MPL-2.0
package stream

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	"github.com/wippyai/runtime/api/runtime/resource"
	streamapi "github.com/wippyai/runtime/api/stream"
)

type gatedReader struct {
	entered chan struct{}
	release chan struct{}
	once    sync.Once
}

func (r *gatedReader) Read([]byte) (int, error) {
	r.once.Do(func() { close(r.entered) })
	<-r.release
	return 0, io.EOF
}
func (*gatedReader) Close() error { return nil }

func TestDispatcherSaturationDoesNotExecuteOnCaller(t *testing.T) {
	ctx, store := setupTestContext()
	defer store.Close()
	d := NewDispatcher(WithWorkers(1))
	if err := d.Start(ctx); err != nil {
		t.Fatal(err)
	}
	r := &gatedReader{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() { close(r.release); d.Stop(context.Background()) }()
	id := Insert(resource.GetTable(ctx), r)
	first := newSyncReceiver()
	d.submit(ctx, streamapi.ReadCmd{StreamID: id}, 1, first)
	select {
	case <-r.entered:
	case <-time.After(time.Second):
		t.Fatal("read not running")
	}
	for i := 0; i < 2; i++ {
		d.submit(ctx, streamapi.StatCmd{StreamID: id}, uint64(i+2), newSyncReceiver())
	}
	rejected := newSyncReceiver()
	returned := make(chan struct{})
	go func() { d.submit(ctx, streamapi.ReadCmd{StreamID: id}, 4, rejected); close(returned) }()
	select {
	case <-returned:
	case <-time.After(time.Second):
		t.Fatal("saturated dispatcher blocked caller on IO")
	}
	rejected.wait(t)
	if !errors.Is(rejected.err, streamapi.ErrBusy) {
		t.Fatalf("rejection = %v", rejected.err)
	}
}

func TestDispatcherRejectsUnavailableAndCancelled(t *testing.T) {
	d := NewDispatcher()
	recv := newSyncReceiver()
	d.submit(context.Background(), streamapi.ReadCmd{}, 1, recv)
	recv.wait(t)
	if !errors.Is(recv.err, streamapi.ErrUnavailable) {
		t.Fatalf("unstarted dispatcher: %v", recv.err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	recv = newSyncReceiver()
	d.submit(ctx, streamapi.ReadCmd{}, 2, recv)
	recv.wait(t)
	if !errors.Is(recv.err, context.Canceled) {
		t.Fatalf("cancelled command: %v", recv.err)
	}
}
