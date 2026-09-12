// SPDX-License-Identifier: MPL-2.0

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
	var stopMonitoring func() error

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
			registrar, ok := node.(relayapi.OwnedHostRegistrar)
			if !ok {
				return ctx, errors.New("topology requires owned relay host registration")
			}
			monitorConfig := topology.MonitorConfig{MaxRecordsPerCaller: topology.DefaultMonitorMaxRecordsPerCaller, MaxPending: topology.DefaultMonitorMaxPending, RequestTimeout: topology.DefaultMonitorRequestTimeout}
			if cfg := boot.GetConfig(ctx); cfg != nil {
				monitorConfig.MaxRecordsPerCaller = cfg.GetInt("topology.monitor.max_records_per_caller", monitorConfig.MaxRecordsPerCaller)
				monitorConfig.MaxPending = cfg.GetInt("topology.monitor.max_pending", monitorConfig.MaxPending)
				monitorConfig.RequestTimeout = cfg.GetDuration("topology.monitor.request_timeout", monitorConfig.RequestTimeout)
			}
			var err error
			stopMonitoring, err = topo.StartRemoteMonitoring(ctx, registrar, monitorConfig)
			if err != nil {
				return ctx, err
			}

			guard := topapi.GetNameGuard(ctx)
			if guard == nil {
				guard = &topapi.NameGuard{}
				ctx = topapi.WithNameGuard(ctx, guard)
			}
			pidReg := topology.NewPIDRegistry(topology.WithLogger(logger.Named("pid")), topology.WithNameGuard(guard))

			bus := event.GetBus(ctx)
			if bus != nil {
				listener = newTopologyEventListener(topo, bus, logger)
				if err := listener.Start(ctx); err != nil {
					return ctx, errors.Join(err, stopMonitoring())
				}
			}

			ctx = topapi.WithTopology(ctx, topo)
			ctx = topapi.WithRegistry(ctx, pidReg)

			logger.Info("topology and pid registry initialized")
			return ctx, nil
		},
		Stop: func(ctx context.Context) error {
			var monitorErr, listenerErr error
			if stopMonitoring != nil {
				monitorErr = stopMonitoring()
			}
			if listener != nil {
				listenerErr = listener.Stop(ctx)
			}
			return errors.Join(monitorErr, listenerErr)
		},
	})
}

// topologyEventListener handles physical cluster disconnect observations.
// Local provider retirement is fenced by its exact relay registration; a
// PeerDelete command is neither physical disconnection nor process death.
type topologyEventListener struct {
	lifecycleMu      sync.Mutex
	stopOnce         sync.Once
	started, stopped bool
	bus              event.Bus
	ctx              context.Context
	topo             *topology.Topology
	logger           *zap.Logger
	events           chan event.Event
	cancel           context.CancelFunc
	subIDs           []event.SubscriberID
	wg               sync.WaitGroup
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
	if ctx == nil {
		return errors.New("topology listener requires context")
	}
	l.lifecycleMu.Lock()
	defer l.lifecycleMu.Unlock()
	if l.started || l.stopped {
		return errors.New("topology listener lifetime already used")
	}
	l.started = true
	l.ctx, l.cancel = context.WithCancel(ctx)

	// Only physical membership events belong to this listener. A local provider
	// delete may be stale, refused, or precede registration and cannot invalidate
	// every monitor sharing its address.
	subID, err := l.bus.SubscribeP(l.ctx, cluster.System, cluster.NodeLeft, l.events)
	if err != nil {
		l.cancel()
		return err
	}
	l.subIDs = append(l.subIDs, subID)

	l.wg.Add(1)
	go l.eventLoop()

	return nil
}

func (l *topologyEventListener) Stop(_ context.Context) error {
	l.stopOnce.Do(func() {
		l.lifecycleMu.Lock()
		l.stopped = true
		cancel, ctx := l.cancel, l.ctx
		ids := l.subIDs
		l.lifecycleMu.Unlock()
		if cancel != nil {
			cancel()
		}
		// Unsubscribe is the event bus's publication barrier. Join the consumer
		// before closing/draining its channel; no extra drain goroutine is needed.
		for _, id := range ids {
			l.bus.Unsubscribe(ctx, id)
		}
		l.wg.Wait()
		close(l.events)
		for range l.events {
		}
	})
	return nil
}

func (l *topologyEventListener) eventLoop() {
	defer l.wg.Done()

	for {
		select {
		case <-l.ctx.Done():
			return
		case evt, ok := <-l.events:
			if !ok {
				return
			}
			if evt.System != cluster.System || evt.Kind != cluster.NodeLeft {
				continue
			}

			nodeID := evt.Path
			l.logger.Debug("handling node exit",
				zap.String("nodeID", nodeID),
				zap.String("system", evt.System),
				zap.String("kind", evt.Kind))

			l.topo.HandleNodeExit(nodeID, errors.New("node disconnected"))
		}
	}
}
