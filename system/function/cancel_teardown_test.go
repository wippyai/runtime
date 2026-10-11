// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

func TestAsyncCancelCompletesFutureAfterTeardown(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		canceled := make(chan struct{})
		teardown := make(chan struct{})
		frameTeardown := make(chan struct{})
		released := false
		frameReleased := false
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			require.NoError(t, ctxapi.FrameFromContext(ctx).Set(&ctxapi.Key{Name: "cancel-cleanup"}, ctxapi.CloserFunc(func() error {
				<-frameTeardown
				frameReleased = true
				return nil
			})))
			<-ctx.Done()
			close(canceled)
			<-teardown
			released = true
			return nil, ctx.Err()
		}}
		ctx := function.WithRegistry(ctxapi.NewRootContext(), reg)
		frame, fc := ctxapi.OpenFrameContext(ctx)
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "cancel"}))
		start := function.AcquireAsyncStartCmd()
		defer start.Release()
		start.Topic = "cancel"
		start.Task.ID = registry.NewID("test", "function")
		require.NoError(t, d.handleAsyncStart(frame, start, 0, &testReceiver{cb: func(any, error) {}}))
		synctest.Wait()
		cancel := function.AcquireAsyncCancelCmd()
		defer cancel.Release()
		cancel.Topic = start.Topic
		done := make(chan struct{})
		require.NoError(t, d.handleAsyncCancel(frame, cancel, 0, &testReceiver{cb: func(any, error) { close(done) }}))
		<-canceled
		// Repeated cancellation must neither drop tracking nor publish a terminal.
		require.NoError(t, d.handleAsyncCancel(frame, cancel, 0, &testReceiver{}))
		synctest.Wait()
		select {
		case <-done:
		default:
			t.Fatal("cancel blocked on teardown")
		}
		// Always unblock cleanup, including when the regression fails.
		early := len(node.packages)
		close(teardown)
		synctest.Wait()
		earlyFrame := len(node.packages)
		close(frameTeardown)
		synctest.Wait()
		require.Zero(t, earlyFrame, "future completed before execution frame teardown")
		require.Zero(t, early, "future completed before call teardown")
		require.True(t, released)
		require.True(t, frameReleased, "call frame cleanup must complete before the future")
		pkg := <-node.packages
		require.Len(t, pkg.Messages, 1)
		require.Len(t, pkg.Messages[0].Payloads, 2)
		require.Len(t, node.packages, 0, "cancellation must publish exactly one outcome")
		require.Equal(t, payload.GoError, pkg.Messages[0].Payloads[0].Format())
		require.ErrorIs(t, pkg.Messages[0].Payloads[0].Data().(error), context.Canceled)
		require.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[1]))
	})
}

func TestAsyncCancelCompletedCallAddsNoTerminal(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		frame, fc := ctxapi.OpenFrameContext(function.WithRegistry(ctxapi.NewRootContext(), &mockRegistry{}))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "completed-cancel"}))
		startAsyncForTest(frame, t, d, "function", "completed-cancel")
		synctest.Wait()
		require.Len(t, node.packages, 1)
		cancel := function.AcquireAsyncCancelCmd()
		defer cancel.Release()
		cancel.Topic = "completed-cancel"
		ack := false
		require.NoError(t, d.handleAsyncCancel(frame, cancel, 0, &testReceiver{cb: func(any, error) { ack = true }}))
		require.True(t, ack)
		require.Len(t, node.packages, 1, "the original outcome must remain the only terminal")
	})
}

func TestAsyncCompletionDoesNotRemoveReplacementCall(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		releaseFirst := make(chan struct{})
		secondCanceled := make(chan struct{})
		releaseSecond := make(chan struct{})
		reg := &mockRegistry{callFn: func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
			if task.ID.Name == "first" {
				<-releaseFirst
				return &runtime.Result{}, nil
			}
			select {
			case <-ctx.Done():
				close(secondCanceled)
			case <-releaseSecond:
			}
			return nil, ctx.Err()
		}}
		frame, fc := ctxapi.OpenFrameContext(function.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "replacement"}))
		startAsyncForTest(frame, t, d, "first", "replacement")
		startAsyncForTest(frame, t, d, "second", "replacement")
		close(releaseFirst)
		synctest.Wait()
		cancel := function.AcquireAsyncCancelCmd()
		defer cancel.Release()
		cancel.Topic = "replacement"
		ack := make(chan struct{})
		require.NoError(t, d.handleAsyncCancel(frame, cancel, 0, &testReceiver{cb: func(any, error) { close(ack) }}))
		<-ack
		synctest.Wait()
		wasCanceled := false
		select {
		case <-secondCanceled:
			wasCanceled = true
		default:
		}
		// Release a broken baseline's untracked call before reporting failure.
		close(releaseSecond)
		synctest.Wait()
		require.True(t, wasCanceled, "older completion removed the replacement cancellation handle")
	})
}

// A cancellation request does not override an outcome returned by the call.
func TestAsyncCancelPreservesSuccessfulOutcome(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			<-ctx.Done()
			return &runtime.Result{Value: payload.New("completed")}, nil
		}}
		frame, fc := ctxapi.OpenFrameContext(function.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "cancel-success"}))
		startAsyncForTest(frame, t, d, "function", "cancel-success")
		cancelAsyncForTest(frame, t, d, "cancel-success")
		synctest.Wait()
		require.Len(t, node.packages, 1)
		pkg := <-node.packages
		require.Len(t, pkg.Messages[0].Payloads, 2)
		require.Equal(t, "completed", pkg.Messages[0].Payloads[0].Data())
		require.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[1]))
	})
}
