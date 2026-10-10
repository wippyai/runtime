// SPDX-License-Identifier: MPL-2.0

package supervisor

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/pid"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/supervisor"
	topologyapi "github.com/wippyai/runtime/api/topology"
)

func TestServiceOutdatedUsesPIDIdentity(t *testing.T) {
	for _, cachedChild := range []bool{false, true} {
		name := "raw_child_cached_sender"
		if cachedChild {
			name = "cached_child_raw_sender"
		}
		t.Run(name, func(t *testing.T) {
			child := pid.PID{Node: "test-node", Host: "test-host", UniqID: "child"}
			sender := child.Precomputed()
			if cachedChild {
				child, sender = sender, child
			}
			require.True(t, child.Equal(sender), "these represent the same process")
			svc := newTestService()
			node := &mockNode{}
			ctx, cancel := context.WithCancel(setupTestContext(node, &mockTopology{}, &mockProcessManager{startedPID: child}))
			defer cancel()
			status, err := svc.Start(ctx)
			require.NoError(t, err)
			node.attachCh <- relay.NewPackage(sender, svc.supervisorPID, topologyapi.TopicEvents,
				payload.New(&topologyapi.ExitEvent{From: sender, Kind: topologyapi.OutdatedRejected}))
			select {
			case reason := <-status:
				require.Equal(t, supervisor.Restart{Graceful: true}, reason)
			case <-time.After(500 * time.Millisecond):
				t.Fatal("valid restart notice was ignored because PID cache differs")
			}
		})
	}
}

func TestSupervisorOwnerContinuity(t *testing.T) {
	ctx, frame := ctxapi.OpenFrameContext(ctxapi.NewRootContext())
	defer ctxapi.ReleaseFrameContext(frame)
	parent := pid.PID{Node: "test-node", Host: "supervisor", UniqID: "owner"}
	require.NoError(t, frame.Set(processapi.OutdatedSupervisorKey, parent))
	frame.Seal()
	_, child := ctxapi.ForkFrameContext(ctx)
	defer ctxapi.ReleaseFrameContext(child)
	_, inherited := child.Get(processapi.OutdatedSupervisorKey)
	require.False(t, inherited, "spawned processes must not inherit restart policy")
	_, continuation, err := ctxapi.ContinueFrameContext(ctx)
	require.NoError(t, err)
	defer ctxapi.ReleaseFrameContext(continuation)
	owner, exists := continuation.Get(processapi.OutdatedSupervisorKey)
	require.True(t, exists, "native upgrade must retain its supervisor")
	require.Equal(t, parent, owner)
}
