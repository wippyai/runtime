// SPDX-License-Identifier: MPL-2.0
package internode

import (
	"context"
	"net"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

func TestManagerStopRefusesLateJoinsAndEveryTrafficClass(t *testing.T) {
	states := boundedReliableStates()
	m := &manager{nodeStates: states, logger: zap.NewNop()}
	require.NoError(t, m.Stop())
	m.AddManagedNode("late")
	require.Nil(t, states.GetNodeState("late"))
	// Even a stale externally retained state cannot reopen manager admission.
	states.CreateNodeState("stale")
	for class := Class(0); int(class) < numClasses; class++ {
		require.ErrorIs(t, m.SendToNode("stale", []byte("x"), class), ErrNodeNotManaged)
		require.ErrorIs(t, m.SendToNodeContext(context.Background(), "stale", []byte("x"), class), ErrNodeNotManaged)
	}
	require.ErrorIs(t, m.Start(context.Background(), nil), ErrNodeNotManaged)
	require.Empty(t, m.controlLoops)
}

func TestManagerStopDisposesRefusedConnectedCommand(t *testing.T) {
	states := boundedReliableStates()
	m := &manager{nodeStates: states, logger: zap.NewNop()}
	require.NoError(t, m.Stop())
	a, b := net.Pipe()
	defer a.Close()
	defer b.Close()
	connection := newNodeConnection(a, "late", DefaultNodeConnectionConfig(), zap.NewNop())
	m.sendCommand("late", nodeCommand{Type: cmdConnected, Data: connectedData{Connection: connection}})
	require.True(t, connection.closed.Load())
	require.Empty(t, m.controlLoops)
}

func TestConcurrentManagerStopsBothJoinExistingWorkers(t *testing.T) {
	states := boundedReliableStates()
	m := &manager{nodeStates: states, logger: zap.NewNop()}
	m.wg.Add(1)
	released := false
	defer func() {
		if !released {
			m.wg.Done()
		}
	}()
	first, second := make(chan struct{}), make(chan struct{})
	go func() { _ = m.Stop(); close(first) }()
	require.Eventually(t, m.stopping.Load, time.Second, time.Millisecond)
	go func() { _ = m.Stop(); close(second) }()
	// sync.Once waiters use a mutex, so virtual-time quiescence cannot observe
	// this wait. A bounded real observation checks neither caller reports done.
	require.Never(t, func() bool {
		select {
		case <-first:
			return true
		default:
		}
		select {
		case <-second:
			return true
		default:
		}
		return false
	}, 20*time.Millisecond, time.Millisecond)
	m.wg.Done()
	released = true
	for _, done := range []chan struct{}{first, second} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("Stop did not join after worker release")
		}
	}
}
