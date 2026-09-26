// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	topoapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

// linkDownCounter counts link down events reaching local processes.
type linkDownCounter struct{ n atomic.Int32 }

func (c *linkDownCounter) Send(pkg *relayapi.Package) error {
	for _, msg := range pkg.Messages {
		for _, p := range msg.Payloads {
			if exit, ok := p.Data().(*topoapi.ExitEvent); ok && exit.Kind == topoapi.LinkDown {
				c.n.Add(1)
			}
		}
	}
	return nil
}

// The listener breaks monitors of a deleted relay peer. Cluster nodes are
// left to the internode transport, which breaks them synchronously when a
// session ends; a membership departure alone does not break them here.
func TestTopologyListenerBreaksMonitorsOfDeletedPeers(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	bus := eventbus.NewBus()
	downs := &linkDownCounter{}
	topo := topology.NewTopology(downs, "local")
	watcher := pid.PID{Node: "local", Host: "host", UniqID: "watcher"}
	require.NoError(t, topo.Register(watcher))
	require.NoError(t, topo.Monitor(watcher, pid.PID{Node: "peer", Host: "host", UniqID: "watched"}))

	listener := newTopologyEventListener(topo, bus, zap.NewNop())
	require.NoError(t, listener.Start(ctx))
	defer func() { require.NoError(t, listener.Stop(ctx)) }()

	bus.Send(ctx, event.Event{System: cluster.System, Kind: cluster.NodeLeft, Path: "peer",
		Data: cluster.NodeEvent{Node: cluster.NodeInfo{ID: "peer"}}})
	require.Never(t, func() bool { return downs.n.Load() > 0 }, 100*time.Millisecond, time.Millisecond)
	bus.Send(ctx, event.Event{System: relayapi.System, Kind: relayapi.PeerDelete, Path: "peer"})
	require.Eventually(t, func() bool { return downs.n.Load() == 1 }, 2*time.Second, time.Millisecond)
}
