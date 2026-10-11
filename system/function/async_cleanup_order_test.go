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

// A context-bound worker stops on cancellation; frame teardown joins it.
// Successful async completion must cancel the call before joining its resources.
func TestAsyncCompletionCancelsBeforeFrameCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			workerStopped := make(chan struct{})
			go func() {
				<-ctx.Done()
				close(workerStopped)
			}()
			require.NoError(t, ctxapi.FrameFromContext(ctx).Set(&ctxapi.Key{Name: "context-worker"}, ctxapi.CloserFunc(func() error {
				<-workerStopped
				return nil
			})))
			return &runtime.Result{Value: payload.New("completed")}, nil
		}}
		ctx := function.WithRegistry(ctxapi.NewRootContext(), reg)
		frame, fc := ctxapi.OpenFrameContext(ctx)
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "cleanup-order"}))
		start := function.AcquireAsyncStartCmd()
		defer start.Release()
		start.Topic = "cleanup-order"
		start.Task.ID = registry.NewID("test", "function")
		require.NoError(t, d.handleAsyncStart(frame, start, 0, &testReceiver{}))
		synctest.Wait()
		completed := false
		select {
		case <-node.packages:
			completed = true
		default:
		}
		// Unblock a broken implementation so this diagnostic leaves no worker.
		if !completed {
			cancel := function.AcquireAsyncCancelCmd()
			defer cancel.Release()
			cancel.Topic = start.Topic
			require.NoError(t, d.handleAsyncCancel(frame, cancel, 0, &testReceiver{}))
			synctest.Wait()
		}
		require.True(t, completed, "successful async call deadlocks joining frame cleanup before its call context is canceled")
	})
}
