package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	relayapi "github.com/wippyai/runtime/api/relay"
	topapi "github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/topology"
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

			topo := topology.NewTopology(node, router, node.ID())
			pidReg := topology.NewPIDRegistry(topology.WithLogger(logger.Named("pid")))

			ctx = topapi.WithTopology(ctx, topo)
			ctx = topapi.WithRegistry(ctx, pidReg)

			logger.Info("topology and pid registry initialized")
			return ctx, nil
		},
	})
}
