// SPDX-License-Identifier: MPL-2.0

package function

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	functionapi "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/runtime"
)

// process.spawn creates its frame on the host context, not the call context.
// Its independent lifetime must not delay completion of the spawning function.
func TestAsyncCompletionDoesNotJoinIndependentHostFrame(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		node := &mockRelayNode{packages: make(chan *relay.Package, 1)}
		d := NewDispatcher(node, nil)
		var hostFrame ctxapi.FrameContext
		key := &ctxapi.Key{Name: "independent-actor", Inherit: true}
		reg := &mockRegistry{callFn: func(ctx context.Context, _ runtime.Task) (*runtime.Result, error) {
			executionCtx, executionFrame := ctxapi.ForkFrameContext(ctx)
			defer ctxapi.ReleaseFrameContext(executionFrame)
			require.NoError(t, executionFrame.Set(key, "inherited"))
			// Mirrors service/host.Host.prepareContext: copy inheritable values
			// without retaining the caller's execution or cancellation context.
			_, hostFrame = ctxapi.SnapshotFrameContextOn(context.Background(), executionCtx)
			return &runtime.Result{Value: payload.New("completed")}, nil
		}}
		frame, fc := ctxapi.OpenFrameContext(functionapi.WithRegistry(ctxapi.NewRootContext(), reg))
		defer ctxapi.ReleaseFrameContext(fc)
		require.NoError(t, runtime.SetFramePID(frame, pid.PID{Host: "test", UniqID: "independent-frame"}))
		startAsyncForTest(frame, t, d, "function", "independent-frame")
		synctest.Wait()
		beforeHostExit := len(node.packages)
		value, inherited := hostFrame.Get(key)
		// Always release the independent frame before reporting a regression.
		ctxapi.ReleaseFrameContext(hostFrame)
		synctest.Wait()
		require.True(t, inherited)
		require.Equal(t, "inherited", value)
		require.Equal(t, 1, beforeHostExit, "a spawned independent actor must not keep its spawning function's future pending")
	})
}
