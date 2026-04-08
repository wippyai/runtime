// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	pgapi "github.com/wippyai/runtime/api/pg"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	systempg "github.com/wippyai/runtime/system/pg"
)

func PG() boot.Component {
	var svc *systempg.Service

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

			router := relay.GetRouter(ctx)
			if router == nil {
				return ctx, ErrRouterNotAvailable
			}

			topo := topology.GetTopology(ctx)
			if topo == nil {
				return ctx, ErrTopologyNotAvailable
			}

			bus := event.GetBus(ctx)

			var membership clusterapi.Membership
			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				if val := ac.Get(membershipServiceKey); val != nil {
					if m, ok := val.(clusterapi.Membership); ok {
						membership = m
					}
				}
			}

			svc = systempg.NewService(logger, router, topo, membership, bus, node.ID())

			// Register as relay host so inter-node pg messages are routed to this service
			if err := node.RegisterHost(pgapi.HostID, svc); err != nil {
				return ctx, err
			}

			ctx = pgapi.WithProcessGroups(ctx, svc)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if svc == nil {
				return nil
			}
			return svc.Start(ctx)
		},
		Stop: func(ctx context.Context) error {
			if svc == nil {
				return nil
			}
			return svc.Stop(ctx)
		},
	})
}
