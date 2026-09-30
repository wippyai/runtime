// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	eventapi "github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"go.uber.org/zap"
)

// EventualReg returns the boot component for the gossip-based name registry.
// LOCAL and EVENTUAL retain independent bindings; composed lookup establishes
// precedence without rejecting a registration in either scope.
func EventualReg() boot.Component {
	var svc *eventual.Service

	return boot.New(boot.P{
		Name:      EventualRegName,
		DependsOn: []boot.Name{ClusterName, TopologyName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("eventualreg")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}
			node := relayapi.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailable
			}
			cfg := eventual.Config{
				LocalNodeID:      node.ID(),
				MetricsCollector: metricsapi.GetCollector(ctx),
				Logger:           logger,
				Bus:              eventapi.GetBus(ctx),
				// The router still delivers EVENTUAL's own conflict notifications.
				Revoker: relayapi.GetRouter(ctx),
			}
			var memSvc *membership.Service
			if m := clusterapi.GetMembership(ctx); m != nil {
				var ok bool
				memSvc, ok = m.(*membership.Service)
				if !ok {
					return ctx, fmt.Errorf("eventualreg: membership service has unexpected type %T", m)
				}
				cfg.LocalNodeID = memSvc.LocalNode().ID
				cfg.Peers = &membershipPeerInventory{m: memSvc}
				cfg.Sender = &eventualRegSender{m: memSvc}
			} else if bootCfg := boot.GetConfig(ctx); bootCfg != nil && bootCfg.Sub(ClusterName).GetBool(ClusterEnabled, false) {
				return ctx, fmt.Errorf("eventualreg: cluster enabled but membership not available")
			}
			svc = eventual.NewService(cfg)
			if memSvc != nil {
				if err := memSvc.RegisterUserDelegate(eventual.NewDelegate(svc, logger)); err != nil {
					return ctx, fmt.Errorf("eventualreg: register delegate: %w", err)
				}
			}
			if pidReg := topology.GetRegistry(ctx); pidReg != nil {
				if setter, ok := pidReg.(interface {
					SetEventualRegistry(topology.EventualRegistry)
				}); ok {
					setter.SetEventualRegistry(svc)
				}
			}
			ctx = topology.WithEventualRegistry(ctx, svc)
			logger.Info("eventualreg loaded", zap.String("node", cfg.LocalNodeID))
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if svc == nil {
				return nil
			}
			return svc.Start(ctx)
		},
		Stop: func(_ context.Context) error {
			if svc == nil {
				return nil
			}
			return svc.Stop()
		},
	})
}

type eventualRegSender struct{ m *membership.Service }

func (s *eventualRegSender) Send(target string, payload []byte) error {
	if s == nil || s.m == nil {
		return fmt.Errorf("eventualreg: sender not wired")
	}
	return s.m.SendUserMessage(target, eventual.DelegateKind, payload)
}

type membershipPeerInventory struct{ m *membership.Service }

func (p *membershipPeerInventory) AlivePeers() []string {
	if p == nil || p.m == nil {
		return nil
	}
	nodes := p.m.Nodes()
	self := p.m.LocalNode().ID
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ID != self {
			out = append(out, n.ID)
		}
	}
	return out
}
