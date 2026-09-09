// SPDX-License-Identifier: MPL-2.0
package tty

import (
	"context"
	"errors"
	"io"
	"sync"
	"testing"
	"time"

	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/service/terminal"
	ttyapi "github.com/wippyai/runtime/api/tty"
)

type isolationReceiver struct{ done chan error }

func (r isolationReceiver) CompleteYield(_ uint64, _ any, err error) { r.done <- err }

type blockingRead struct {
	entered     chan struct{}
	release     chan struct{}
	enteredOnce sync.Once
	releaseOnce sync.Once
}

func (r *blockingRead) Read([]byte) (int, error) {
	r.enteredOnce.Do(func() { close(r.entered) })
	<-r.release
	return 0, io.EOF
}

func (r *blockingRead) unblock() { r.releaseOnce.Do(func() { close(r.release) }) }

func TestBlockedReadDoesNotPreventAnotherTerminalStarting(t *testing.T) {
	d := NewDispatcher()
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingRead{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() { blocked.unblock(); _ = d.Stop(context.Background()) }()
	readCtx := ctxapi.NewRootContext()
	readCtx, _ = ctxapi.OpenFrameContext(readCtx)
	_ = terminal.WithTerminalContext(readCtx, terminal.NewTerminalContext(blocked, nil, nil))
	readResult := isolationReceiver{make(chan error, 1)}
	if err := d.handle(readCtx, ttyapi.ReadLineCmd{}, 1, readResult); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	controlCtx := ctxapi.NewRootContext()
	controlCtx, _ = ctxapi.OpenFrameContext(controlCtx)
	tc := terminal.NewTerminalContext(nil, nil, nil)
	tc.Input = &stubInputController{}
	_ = terminal.WithTerminalContext(controlCtx, tc)
	started := isolationReceiver{make(chan error, 1)}
	if err := d.handle(controlCtx, ttyapi.StartInputCmd{}, 2, started); err != nil {
		t.Fatal(err)
	}
	select {
	case err := <-started.done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("blocked stdin prevented unrelated terminal startup")
	}
}

func TestReadQueueSaturationReturnsBusy(t *testing.T) {
	d := NewDispatcher()
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingRead{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() { blocked.unblock(); _ = d.Stop(context.Background()) }()
	readCtx := ctxapi.NewRootContext()
	readCtx, _ = ctxapi.OpenFrameContext(readCtx)
	_ = terminal.WithTerminalContext(readCtx, terminal.NewTerminalContext(blocked, nil, nil))

	if err := d.handle(readCtx, ttyapi.ReadLineCmd{}, 1, isolationReceiver{make(chan error, 1)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}
	for tag := uint64(2); tag <= 3; tag++ {
		if err := d.handle(readCtx, ttyapi.ReadLineCmd{}, tag, isolationReceiver{make(chan error, 1)}); err != nil {
			t.Fatalf("queued read %d failed: %v", tag, err)
		}
	}
	busyResult := make(chan error, 1)
	go func() {
		busyResult <- d.handle(readCtx, ttyapi.ReadLineCmd{}, 4, isolationReceiver{make(chan error, 1)})
	}()
	select {
	case err := <-busyResult:
		if !errors.Is(err, errDispatcherBusy) {
			t.Fatalf("expected saturated read queue to return dispatcher busy, got %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("saturated read submission blocked the caller")
	}
}

func TestSubmitAndStopSerializeQueueClosure(t *testing.T) {
	d := NewDispatcher()
	if err := d.Start(context.Background()); err != nil {
		t.Fatal(err)
	}
	blocked := &blockingRead{entered: make(chan struct{}), release: make(chan struct{})}
	defer func() { blocked.unblock(); _ = d.Stop(context.Background()) }()
	readCtx := ctxapi.NewRootContext()
	readCtx, _ = ctxapi.OpenFrameContext(readCtx)
	_ = terminal.WithTerminalContext(readCtx, terminal.NewTerminalContext(blocked, nil, nil))
	if err := d.handle(readCtx, ttyapi.ReadLineCmd{}, 1, isolationReceiver{make(chan error, 1)}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-blocked.entered:
	case <-time.After(time.Second):
		t.Fatal("read did not start")
	}

	panics := make(chan any, 1)
	var submitters sync.WaitGroup
	for i := 0; i < 8; i++ {
		submitters.Add(1)
		go func() {
			defer submitters.Done()
			for tag := uint64(2); tag < 34; tag++ {
				func() {
					defer func() {
						if recovered := recover(); recovered != nil {
							select {
							case panics <- recovered:
							default:
							}
						}
					}()
					_ = d.handle(readCtx, ttyapi.ReadLineCmd{}, tag, isolationReceiver{make(chan error, 1)})
				}()
			}
		}()
	}

	stopDone := make(chan error, 1)
	go func() { stopDone <- d.Stop(context.Background()) }()
	blocked.unblock()
	submitters.Wait()
	select {
	case err := <-stopDone:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("dispatcher stop did not complete")
	}
	select {
	case recovered := <-panics:
		t.Fatalf("submit raced Stop with panic: %v", recovered)
	default:
	}
}
