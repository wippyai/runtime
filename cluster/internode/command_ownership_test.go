// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"net"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestControlLoopCleanupDisposesQueuedWaitingAndLateConnections(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		states := setupStateManager()
		states.CreateNodeState("peer")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		loop := &nodeControlLoop{
			ctx: ctx, cancel: cancel, nodeID: "peer", nodeState: states.GetNodeState("peer"),
			manager: &manager{nodeStates: states}, commands: make(chan nodeCommand, 1),
		}
		newCommand := func() (*NodeConnection, nodeCommand) {
			a, b := net.Pipe()
			t.Cleanup(func() { _ = a.Close(); _ = b.Close() })
			conn := newNodeConnection(a, "peer", DefaultNodeConnectionConfig(), zap.NewNop())
			return conn, nodeCommand{Type: cmdConnected, Data: connectedData{Connection: conn}}
		}
		queued, first := newCommand()
		loop.enqueueCommand(first)
		require.False(t, queued.closed.Load())
		waiting, second := newCommand()
		done := make(chan struct{})
		go func() { loop.enqueueCommand(second); close(done) }()
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("second command did not wait on full queue")
		default:
		}
		loop.cleanup()
		<-done
		require.True(t, queued.closed.Load(), "queue owns the accepted connection until handled or disposed")
		require.True(t, waiting.closed.Load(), "canceled sender must dispose its unaccepted connection")
		require.Empty(t, loop.commands)
		late, third := newCommand()
		loop.sendCommandToSelf(third)
		require.True(t, late.closed.Load(), "late producer cannot repopulate retired queue")
		require.Empty(t, loop.commands)
		loop.cleanup()
	})
}
