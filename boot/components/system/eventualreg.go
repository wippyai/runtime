// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/eventualreg"
	"go.uber.org/zap"
)

// EventualReg returns the boot component for the gossip-based name registry.
// It depends on cluster (membership delegate registration) and topology
// (PIDRegistry shadow-check wiring).
func EventualReg() boot.Component {
	var svc *eventualreg.Service

	return boot.New(boot.P{
		Name:      EventualRegName,
		DependsOn: []boot.Name{ClusterName, TopologyName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("eventualreg")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			m := clusterapi.GetMembership(ctx)
			if m == nil {
				logger.Debug("eventualreg: cluster disabled, skipping")
				return ctx, nil
			}
			memSvc, ok := m.(*membership.Service)
			if !ok {
				return ctx, fmt.Errorf("eventualreg: membership service has unexpected type %T", m)
			}

			cfg := eventualreg.Config{
				LocalNodeID:      memSvc.LocalNode().ID,
				Peers:            &membershipPeerInventory{m: memSvc},
				CrossScope:       newCrossScopeChecker(ctx),
				MetricsCollector: metricsapi.GetCollector(ctx),
				Logger:           logger,
				Sender:           &eventualRegSender{m: memSvc},
			}
			svc = eventualreg.NewService(cfg)

			delegate := eventualreg.NewDelegate(svc, logger)
			if err := memSvc.RegisterUserDelegate(delegate); err != nil {
				return ctx, fmt.Errorf("eventualreg: register delegate: %w", err)
			}

			// Wire shadow-check into the existing PIDRegistry so a LOCAL
			// register cannot shadow an EVENTUAL one.
			pidReg := topology.GetRegistry(ctx)
			if pidReg != nil {
				if setter, ok := pidReg.(interface {
					SetEventualRegistry(topology.EventualRegistry)
				}); ok {
					setter.SetEventualRegistry(svc)
				}
			}

			ctx = topology.WithEventualRegistry(ctx, svc)

			// Publish the service into app context so the admin HTTP
			// server can expose its digest / CV for the gossip-
			// convergence invariant in the chaos harness.
			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				ac.With(eventualRegSvcKey, svc)
			}

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

// eventualRegSender adapts membership.Service to eventualreg.MessageSender.
// Routes targeted shard-pull frames to a specific peer via the
// reliable TCP user-message channel, using the eventualreg delegate's
// Kind byte to land in the peer's eventualreg.OnFrame dispatcher.
type eventualRegSender struct {
	m *membership.Service
}

func (s *eventualRegSender) Send(target string, payload []byte) error {
	if s == nil || s.m == nil {
		return fmt.Errorf("eventualreg: sender not wired")
	}
	return s.m.SendUserMessage(target, eventualreg.DelegateKind, payload)
}

// membershipPeerInventory adapts membership.Service to eventualreg.PeerInventory.
type membershipPeerInventory struct {
	m *membership.Service
}

func (p *membershipPeerInventory) AlivePeers() []string {
	if p == nil || p.m == nil {
		return nil
	}
	nodes := p.m.Nodes()
	self := p.m.LocalNode().ID
	out := make([]string, 0, len(nodes))
	for _, n := range nodes {
		if n.ID == self {
			continue
		}
		out = append(out, n.ID)
	}
	return out
}

// crossScopeChecker satisfies eventualreg.CrossScopeChecker by consulting the
// GLOBAL and LOCAL registries on every Register call. We resolve registries
// lazily because raft (which provides GLOBAL) lands in context AFTER this
// component loads — Lookup-time resolution catches the wired-up version.
type crossScopeChecker struct {
	ctx context.Context
}

func newCrossScopeChecker(ctx context.Context) *crossScopeChecker {
	return &crossScopeChecker{ctx: ctx}
}

// LookupOther returns (PID, true) if `name` is held in any non-Eventual scope.
func (c *crossScopeChecker) LookupOther(name string) (pid.PID, bool) {
	if c == nil {
		return pid.PID{}, false
	}
	if gr := topology.GetGlobalRegistry(c.ctx); gr != nil {
		if res, err := gr.Lookup(c.ctx, name); err == nil && res.Found {
			return res.PID, true
		}
	}
	if lr := topology.GetRegistry(c.ctx); lr != nil {
		if p, ok := lr.Lookup(name); ok {
			return p, true
		}
	}
	return pid.PID{}, false
}
