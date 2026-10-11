// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
	"github.com/wippyai/runtime/internal/uniqid"
)

// A nested call retains its parent frame while its own teardown is pending.
// Releasing the outer frame's owner must not be mistaken for completed cleanup.
func TestAsyncCancelWaitsForRetainedExecutionFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		releaseNested := make(chan struct{})
		nestedCanceled := make(chan struct{})
		frameClosed := false
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			// Match Registry.executor's execution-local frame.
			executionCtx, executionFrame := ctxapi.ForkFrameContext(ctx)
			defer ctxapi.ReleaseFrameContext(executionFrame)
			require.NoError(t, ctxapi.FrameFromContext(executionCtx).Set(&ctxapi.Key{Name: "retained-cleanup"}, ctxapi.CloserFunc(func() error {
				frameClosed = true
				return nil
			})))
			// Match the child frame retained by a nested function dispatcher.
			nestedCtx, nestedFrame := ctxapi.ForkFrameContext(executionCtx)
			go func() {
				<-nestedCtx.Done()
				close(nestedCanceled)
				<-releaseNested
				ctxapi.ReleaseFrameContext(nestedFrame)
			}()
			<-ctx.Done()
			return nil, ctx.Err()
		}}
		frame, fc := ctxapi.OpenFrameContext(functionapi.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "retained-frame"}))
		startAsyncForTest(frame, t, d, "function", "retained-frame")
		synctest.Wait()
		cancelAsyncForTest(frame, t, d, "retained-frame")
		<-nestedCanceled
		synctest.Wait()
		earlyOutcome := len(node.packages)
		closedBeforeNestedTeardown := frameClosed
		// Unblock the nested worker even when the assertion fails.
		close(releaseNested)
		synctest.Wait()
		require.False(t, closedBeforeNestedTeardown, "fixture must retain the execution frame")
		require.True(t, frameClosed, "nested teardown must eventually release the execution frame")
		require.Zero(t, earlyOutcome, "future completed while its retained execution frame still owned resources")
		require.Len(t, node.packages, 1, "cleanup must publish exactly one final outcome")
	})
}

func TestAsyncOutcomeWaitsForAllNestedCalls(t *testing.T) {
	callErr := errors.New("outer call failed")
	cases := []struct {
		err           error
		result        *runtime.Result
		name          string
		waitForCancel bool
	}{
		{name: "success", result: &runtime.Result{Value: payload.New("completed")}},
		{name: "call error", err: callErr},
		{name: "result error", result: &runtime.Result{Error: callErr}},
		{name: "no result"},
		{name: "success after cancellation", result: &runtime.Result{Value: payload.New("completed")}, waitForCancel: true},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
				d := NewDispatcher(node, nil)
				reg := NewFunctionRegistry(nil, nil)
				releases := []chan struct{}{make(chan struct{}), make(chan struct{})}
				entered := make(chan struct{}, len(releases))
				canceled := make(chan struct{}, len(releases))
				frameClosed := false
				reg.handlers.Store(registry.NewID("test", "nested"), functionapi.Func(func(ctx context.Context, task runtime.Task) (*runtime.Result, error) {
					entered <- struct{}{}
					<-ctx.Done()
					canceled <- struct{}{}
					<-releases[task.Options.(runtime.Bag)["nested_index"].(int)]
					return nil, ctx.Err()
				}))
				reg.handlers.Store(registry.NewID("test", "outer"), functionapi.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
					require.NoError(t, ctxapi.FrameFromContext(ctx).Set(&ctxapi.Key{Name: "nested-outcome-cleanup"}, ctxapi.CloserFunc(func() error {
						frameClosed = true
						return nil
					})))
					for i := range releases {
						nestedCall := &functionapi.CallCmd{Task: runtime.Task{ID: registry.NewID("test", "nested"), Options: runtime.Bag{"nested_index": i}}}
						require.NoError(t, d.handleCall(ctx, nestedCall, uint64(i), &testReceiver{}))
					}
					for range releases {
						<-entered
					}
					if tc.waitForCancel {
						<-ctx.Done()
					}
					return tc.result, tc.err
				}))
				ctx := functionapi.WithRegistry(ctxapi.NewRootContext(), reg)
				ctx = process.WithPIDGenerator(ctx, uniqid.NewPIDGenerator(uniqid.NewGenerator(), ""))
				frame, fc := ctxapi.OpenFrameContext(ctx)
				defer ctxapi.ReleaseFrameContext(fc)
				caller := pid.PID{Host: "test", UniqID: "nested-outcome"}
				require.NoError(t, runtime.SetFramePID(frame, caller))
				startAsyncForTest(frame, t, d, "outer", "nested-outcome")
				if tc.waitForCancel {
					cancelAsyncForTest(frame, t, d, "nested-outcome")
				}
				for range releases {
					<-canceled
				}
				synctest.Wait()
				early := len(node.packages)
				close(releases[0])
				synctest.Wait()
				afterFirst := len(node.packages)
				key := asyncCallKey{target: caller.String(), topic: "nested-outcome"}
				_, tracked := d.asyncCalls.Load(key)
				cancelAsyncForTest(frame, t, d, "nested-outcome")
				closedBeforeLast := frameClosed
				close(releases[1])
				synctest.Wait()
				require.Zero(t, early)
				require.Zero(t, afterFirst, "all children, not just the first, must finish")
				require.True(t, tracked, "cancellation handle must survive retained cleanup")
				require.False(t, closedBeforeLast)
				require.True(t, frameClosed)
				_, tracked = d.asyncCalls.Load(key)
				require.False(t, tracked, "completion must remove the cancellation handle")
				require.Len(t, node.packages, 1)
				pkg := <-node.packages
				require.Len(t, pkg.Messages[0].Payloads, 2)
				require.True(t, payload.IsTerminal(pkg.Messages[0].Payloads[1]))
				expectedErr := tc.err
				if tc.result != nil && tc.result.Error != nil {
					expectedErr = tc.result.Error
				}
				if expectedErr != nil {
					require.ErrorIs(t, pkg.Messages[0].Payloads[0].Data().(error), expectedErr)
				} else if tc.result != nil {
					require.Equal(t, tc.result.Value, pkg.Messages[0].Payloads[0])
				} else {
					require.Nil(t, pkg.Messages[0].Payloads[0])
				}
			})
		})
	}
}

func TestAsyncCancelWaitsForNestedCallCleanup(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 10)}
		d := NewDispatcher(node, nil)
		reg := NewFunctionRegistry(nil, nil)
		releaseNested := make(chan struct{})
		nestedEntered := make(chan struct{})
		nestedCanceled := make(chan struct{})
		frameClosed := false
		reg.handlers.Store(registry.NewID("test", "nested"), functionapi.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			close(nestedEntered)
			<-ctx.Done()
			close(nestedCanceled)
			<-releaseNested
			return nil, ctx.Err()
		}))
		reg.handlers.Store(registry.NewID("test", "outer"), functionapi.Func(func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			require.NoError(t, ctxapi.FrameFromContext(ctx).Set(&ctxapi.Key{Name: "real-registry-cleanup"}, ctxapi.CloserFunc(func() error {
				frameClosed = true
				return nil
			})))
			nestedCall := &functionapi.CallCmd{Task: runtime.Task{ID: registry.NewID("test", "nested")}}
			require.NoError(t, d.handleCall(ctx, nestedCall, 1, &testReceiver{}))
			<-ctx.Done()
			return nil, ctx.Err()
		}))
		ctx := functionapi.WithRegistry(ctxapi.NewRootContext(), reg)
		ctx = process.WithPIDGenerator(ctx, uniqid.NewPIDGenerator(uniqid.NewGenerator(), ""))
		frame, fc := ctxapi.OpenFrameContext(ctx)
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "real-registry"}))
		startAsyncForTest(frame, t, d, "outer", "real-registry")
		<-nestedEntered
		synctest.Wait()
		cancelAsyncForTest(frame, t, d, "real-registry")
		<-nestedCanceled
		synctest.Wait()
		earlyOutcome := len(node.packages)
		closedBeforeNestedTeardown := frameClosed
		close(releaseNested)
		synctest.Wait()
		require.False(t, closedBeforeNestedTeardown, "nested call must retain the real execution frame")
		require.True(t, frameClosed, "cleanup must run once the nested call releases its frames")
		require.Zero(t, earlyOutcome, "real registry published the outer future before execution-frame cleanup")
		require.Len(t, node.packages, 1, "cleanup must publish exactly one final outcome")
	})
}
