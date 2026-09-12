// SPDX-License-Identifier: MPL-2.0
package system

import (
	"context"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/relay"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

func TestTopologyListenerDoesNotTreatProviderDeleteAsNodeExit(t *testing.T) {
	// Feed the loop synchronously so the assertions prove processing rather than
	// relying on a sleep that might finish before the event was handled.
	for _, evt := range []event.Event{
		{System: relayapi.System, Kind: relayapi.PeerDelete, Path: "provider"},
		{System: relayapi.System, Kind: cluster.NodeLeft, Path: "provider"},
	} {
		t.Run(evt.System+"/"+evt.Kind, func(t *testing.T) {
			node := relay.NewNode("local")
			topo := topology.NewTopology(relay.NewRouter(node, nil), "local")
			target := pid.PID{Node: "provider", Host: "queue", UniqID: "target"}
			require.NoError(t, topo.Register(target))
			listener := newTopologyEventListener(topo, eventbus.NewBus(), zap.NewNop())
			listener.ctx = context.Background()
			listener.events <- evt
			close(listener.events)
			listener.wg.Add(1)
			listener.eventLoop()
			require.Error(t, topo.Register(target), "unrelated/local events must preserve existing process state")
		})
	}
}

func TestTopologyListenerStillHandlesPhysicalDisconnect(t *testing.T) {
	node := relay.NewNode("local")
	topo := topology.NewTopology(relay.NewRouter(node, nil), "local")
	target := pid.PID{Node: "remote", Host: "app", UniqID: "target"}
	require.NoError(t, topo.Register(target))
	listener := newTopologyEventListener(topo, eventbus.NewBus(), zap.NewNop())
	listener.ctx = context.Background()
	listener.events <- event.Event{System: cluster.System, Kind: cluster.NodeLeft, Path: "remote"}
	close(listener.events)
	listener.wg.Add(1)
	listener.eventLoop()
	require.NoError(t, topo.Register(target), "physical disconnect still clears the tracked remote state")
}
