// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	bootpkg "github.com/wippyai/runtime/boot"
	pgservice "github.com/wippyai/runtime/service/pg"
)

func PG() boot.Component {
	return boot.New(boot.P{
		Name:      PGName,
		DependsOn: []boot.Name{TopologyName, ClusterName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailable
			}

			topo := topology.GetTopology(ctx)
			if topo == nil {
				return ctx, ErrTopologyNotAvailable
			}

			bus := event.GetBus(ctx)
			dtt := payload.GetTranscoder(ctx)

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			membership := clusterapi.GetMembership(ctx)

			manager := pgservice.NewManager(bus, dtt, topo, membership, node.ID(), logger.Named("pg"))
			handlers.RegisterListener("pg.scope", manager)

			return ctx, nil
		},
	})
}
