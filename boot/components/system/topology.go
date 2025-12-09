package system

import (
	"context"
	"errors"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	relayapi "github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/topology"
	"go.uber.org/zap"
)

const TopologyName = "system.topology"

func Topology() boot.Component {
	return boot.New(boot.P{
		Name: TopologyName,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("topology")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			node := relayapi.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailable
			}

			router := relayapi.GetRouter(ctx)
			if router == nil {
				return ctx, ErrRouterNotAvailable
			}

			topo := topology.NewTopology(router, node.ID())
			pidReg := topology.NewPIDRegistry(topology.WithLogger(logger.Named("pid")))

			// Start event listener for node failures
			bus := event.GetBus(ctx)
			if bus != nil {
				listener := newTopologyEventListener(topo, logger)
				listener.start(ctx, bus)
			}

			ctx = topapi.WithTopology(ctx, topo)
			ctx = topapi.WithRegistry(ctx, pidReg)

			logger.Info("topology and pid registry initialized")
			return ctx, nil
		},
	})
}

// topologyEventListener handles node exit events from multiple sources.
type topologyEventListener struct {
	topo   *topology.Topology
	events chan event.Event
	logger *zap.Logger
}

func newTopologyEventListener(topo *topology.Topology, logger *zap.Logger) *topologyEventListener {
	return &topologyEventListener{
		topo:   topo,
		events: make(chan event.Event, 64),
		logger: logger,
	}
}

func (l *topologyEventListener) start(ctx context.Context, bus event.Bus) {
	// Subscribe to peer delete events (relay system)
	_, _ = bus.Subscribe(ctx, relayapi.System, l.events)
	// Subscribe to cluster node left events
	_, _ = bus.SubscribeP(ctx, cluster.System, cluster.NodeLeftEventKind, l.events)

	// Single event loop
	go func() {
		for {
			select {
			case <-ctx.Done():
				return
			case evt := <-l.events:
				// Filter for relevant events
				if evt.Kind != relayapi.PeerDelete && evt.Kind != cluster.NodeLeftEventKind {
					continue
				}
				nodeID := relayapi.NodeID(evt.Path)
				l.logger.Debug("handling node exit",
					zap.String("nodeID", string(nodeID)),
					zap.String("system", evt.System),
					zap.String("kind", evt.Kind))
				l.topo.HandleNodeExit(nodeID, errors.New("node disconnected"))
			}
		}
	}()
}
