package system

import (
	"context"
	"errors"
	"sync"

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
	var listener *topologyEventListener

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

			bus := event.GetBus(ctx)
			if bus != nil {
				listener = newTopologyEventListener(topo, bus, logger)
				if err := listener.Start(ctx); err != nil {
					return ctx, err
				}
			}

			ctx = topapi.WithTopology(ctx, topo)
			ctx = topapi.WithRegistry(ctx, pidReg)

			logger.Info("topology and pid registry initialized")
			return ctx, nil
		},
		Stop: func(ctx context.Context) error {
			if listener != nil {
				return listener.Stop(ctx)
			}
			return nil
		},
	})
}

// topologyEventListener handles node exit events from multiple sources.
type topologyEventListener struct {
	topo   *topology.Topology
	bus    event.Bus
	logger *zap.Logger

	events chan event.Event
	subIDs []event.SubscriberID

	ctx    context.Context
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func newTopologyEventListener(topo *topology.Topology, bus event.Bus, logger *zap.Logger) *topologyEventListener {
	return &topologyEventListener{
		topo:   topo,
		bus:    bus,
		logger: logger,
		events: make(chan event.Event, 64),
	}
}

func (l *topologyEventListener) Start(ctx context.Context) error {
	l.ctx, l.cancel = context.WithCancel(ctx)

	// Subscribe to peer delete events (relay system)
	subID1, err := l.bus.Subscribe(l.ctx, relayapi.System, l.events)
	if err != nil {
		return err
	}
	l.subIDs = append(l.subIDs, subID1)

	// Subscribe to cluster node left events
	subID2, err := l.bus.SubscribeP(l.ctx, cluster.System, cluster.NodeLeftEventKind, l.events)
	if err != nil {
		l.bus.Unsubscribe(l.ctx, subID1)
		return err
	}
	l.subIDs = append(l.subIDs, subID2)

	l.wg.Add(1)
	go l.eventLoop()

	return nil
}

func (l *topologyEventListener) Stop(ctx context.Context) error {
	l.cancel()

	for _, subID := range l.subIDs {
		l.bus.Unsubscribe(ctx, subID)
	}

	close(l.events)
	l.wg.Wait()
	return nil
}

func (l *topologyEventListener) eventLoop() {
	defer l.wg.Done()

	for evt := range l.events {
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
