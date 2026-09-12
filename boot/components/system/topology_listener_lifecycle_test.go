// SPDX-License-Identifier: MPL-2.0
package system

import (
	"context"
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

func TestTopologyListenerStopIsIdempotentAndOneShot(t *testing.T) {
	node := relay.NewNode("local")
	topo := topology.NewTopology(relay.NewRouter(node, nil), "local")
	listener := newTopologyEventListener(topo, eventbus.NewBus(), zap.NewNop())
	require.NoError(t, listener.Start(context.Background()))
	require.NoError(t, listener.Stop(context.Background()))
	require.NotPanics(t, func() { require.NoError(t, listener.Stop(context.Background())) })
	require.Error(t, listener.Start(context.Background()), "stopped listener cannot reuse its old subscription lifetime")
}

func TestTopologyListenerStopBeforeStart(t *testing.T) {
	listener := newTopologyEventListener(nil, eventbus.NewBus(), zap.NewNop())
	require.NotPanics(t, func() { require.NoError(t, listener.Stop(context.Background())) })
	require.Error(t, listener.Start(context.Background()))
}

func TestTopologyListenerConcurrentStopsJoin(t *testing.T) {
	node := relay.NewNode("local")
	topo := topology.NewTopology(relay.NewRouter(node, nil), "local")
	listener := newTopologyEventListener(topo, eventbus.NewBus(), zap.NewNop())
	require.NoError(t, listener.Start(context.Background()))
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := listener.Stop(context.Background()); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
}
