// SPDX-License-Identifier: MPL-2.0

package xmlcmd

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	apierror "github.com/wippyai/runtime/api/error"
	xmlapi "github.com/wippyai/runtime/api/xml"
	"github.com/wippyai/runtime/system/xml"
)

func TestDispatcherRejectsFullQueueWithoutInlineIO(t *testing.T) {
	ctx, table := frame(t)
	source := &blockingReader{started: make(chan struct{}), release: make(chan struct{})}
	r, err := NewReader(ctx, source, xml.Options{})
	require.NoError(t, err)
	id := InsertReader(table, r)
	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	t.Cleanup(func() { require.NoError(t, d.Stop(ctx)) })
	t.Cleanup(func() { close(source.release) })
	result := &receiver{ch: make(chan result, 4)}
	command := xmlapi.FillCmd{Reader: id}
	require.NoError(t, d.handle(ctx, command, 1, result))
	select {
	case <-source.started:
	case <-time.After(5 * time.Second):
		t.Fatal("worker did not reach the source")
	}
	for tag := uint64(2); tag <= 3; tag++ {
		require.NoError(t, d.handle(ctx, command, tag, result))
	}
	returned := make(chan error, 1)
	go func() { returned <- d.handle(ctx, command, 4, result) }()
	select {
	case err := <-returned:
		var typed apierror.Error
		require.ErrorAs(t, err, &typed)
		require.Equal(t, apierror.Unavailable, typed.Kind())
	case <-time.After(5 * time.Second):
		t.Fatal("full queue ran blocking XML I/O on the submitting thread")
	}
}

func TestDispatcherRejectsCommandsAfterStop(t *testing.T) {
	ctx, _ := frame(t)
	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(ctx))
	require.NoError(t, d.Stop(ctx))
	result := &receiver{ch: make(chan result, 1)}
	err := d.handle(ctx, xmlapi.FillCmd{Reader: 1}, 1, result)
	var typed apierror.Error
	require.ErrorAs(t, err, &typed)
	require.Equal(t, apierror.Unavailable, typed.Kind())
	require.Empty(t, result.ch, "a rejected command must not also complete a yield")
}

func TestDispatcherRejectsCommandsAfterServiceCancellation(t *testing.T) {
	ctx, _ := frame(t)
	service, cancel := context.WithCancel(context.Background())
	t.Cleanup(cancel)
	d := NewDispatcher(WithWorkers(1))
	require.NoError(t, d.Start(service))
	t.Cleanup(func() { require.NoError(t, d.Stop(context.Background())) })
	cancel()
	result := &receiver{ch: make(chan result, 1)}
	err := d.handle(ctx, xmlapi.FillCmd{Reader: 1}, 1, result)
	require.ErrorIs(t, err, context.Canceled)
	var typed apierror.Error
	require.ErrorAs(t, err, &typed)
	require.Equal(t, apierror.Canceled, typed.Kind())
	require.Empty(t, result.ch, "a rejected command must not also complete a yield")
}

type countingReceiver struct{ completed atomic.Int64 }

func (r *countingReceiver) CompleteYield(uint64, any, error) { r.completed.Add(1) }

func TestDispatcherStopCompletesEveryAcceptedCommand(t *testing.T) {
	ctx, _ := frame(t)
	d := NewDispatcher(WithWorkers(2))
	require.NoError(t, d.Start(ctx))
	result := &countingReceiver{}
	var accepted atomic.Int64
	require.NoError(t, d.handle(ctx, xmlapi.FillCmd{Reader: 1}, 0, result))
	accepted.Add(1)
	var callers sync.WaitGroup
	start := make(chan struct{})
	for range 8 {
		callers.Add(1)
		go func() {
			defer callers.Done()
			<-start
			for tag := range 500 {
				if d.handle(ctx, xmlapi.FillCmd{Reader: 1}, uint64(tag), result) == nil {
					accepted.Add(1)
				}
			}
		}()
	}
	close(start)
	require.NoError(t, d.Stop(context.Background()))
	callers.Wait()
	require.Equal(t, accepted.Load(), result.completed.Load())
	require.NoError(t, d.Stop(context.Background()), "stop is idempotent")
}
