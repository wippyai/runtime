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
	"github.com/wippyai/runtime/api/pid"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/topology/namereg/admission"
	"github.com/wippyai/runtime/system/topology/namereg/eventual"
	"go.uber.org/zap"
)

// EventualReg returns the boot component for the gossip-based name registry.
// It depends on cluster (membership delegate registration) and topology
// (PIDRegistry shadow-check wiring).
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

			m := clusterapi.GetMembership(ctx)
			if m == nil {
				logger.Debug("eventualreg: cluster disabled, skipping")
				return ctx, nil
			}
			memSvc, ok := m.(*membership.Service)
			if !ok {
				return ctx, fmt.Errorf("eventualreg: membership service has unexpected type %T", m)
			}

			cfg := eventual.Config{
				Admission:   admission.FromContext(ctx),
				LocalNodeID: memSvc.LocalNode().ID,
				Peers:       &membershipPeerInventory{m: memSvc},
				CrossScope:  newCrossScopeChecker(ctx),
				StrongReservation: func(name string) (pid.PID, bool) {
					if gr := topology.GetGlobalRegistry(ctx); gr != nil {
						return gr.IsStrongReserved(name)
					}
					return pid.PID{}, false
				},
				MetricsCollector: metricsapi.GetCollector(ctx),
				Logger:           logger,
				Bus:              eventapi.GetBus(ctx),
				Sender:           &eventualRegSender{m: memSvc},
				// Relay router delivers name_revoked notifications to local
				// processes that lose a name to a different origin's winner.
				Revoker: relayapi.GetRouter(ctx),
			}
			svc = eventual.NewService(cfg)

			delegate := eventual.NewDelegate(svc, logger)
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

// eventualRegSender adapts membership.Service to eventual.MessageSender.
// Routes targeted shard-pull frames to a specific peer via the
// reliable TCP user-message channel, using the eventualreg delegate's
// Kind byte to land in the peer's eventual.OnFrame dispatcher.
type eventualRegSender struct {
	m *membership.Service
}

func (s *eventualRegSender) Send(target string, payload []byte) error {
	if s == nil || s.m == nil {
		return fmt.Errorf("eventualreg: sender not wired")
	}
	return s.m.SendUserMessage(target, eventual.DelegateKind, payload)
}

// membershipPeerInventory adapts membership.Service to eventual.PeerInventory.
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

// crossScopeChecker satisfies eventual.CrossScopeChecker by consulting the
// CONSISTENT and LOCAL registries on every Register call. We resolve registries
// lazily because raft (which provides CONSISTENT) lands in context AFTER this
// component loads — Lookup-time resolution catches the wired-up version.
type crossScopeChecker struct {
	ctx context.Context
}

func newCrossScopeChecker(ctx context.Context) *crossScopeChecker {
	return &crossScopeChecker{ctx: ctx}
}

// LookupOther returns a conflicting claim from any non-Eventual scope before
// returning a same-owner claim. The composed PIDRegistry.Lookup also sees
// EVENTUAL's own winner, so only its dedicated LOCAL table is used here.
func (c *crossScopeChecker) LookupOther(name string, proposed pid.PID) (pid.PID, bool, error) {
	if c == nil {
		return pid.PID{}, false, nil
	}
	var matching bool
	if gr := topology.GetGlobalRegistry(c.ctx); gr != nil {
		res, err := gr.Lookup(c.ctx, name)
		if err != nil {
			return pid.PID{}, false, err
		}
		if res.Found {
			if !res.PID.Equal(proposed) {
				return res.PID, true, nil
			}
			matching = true
		}
		// A held Strong reservation blocks an EVENTUAL bind to a different pid
		// for the duration of the promotion window.
		if reserved, ok := gr.IsStrongReserved(name); ok {
			if !reserved.Equal(proposed) {
				return reserved, true, nil
			}
			matching = true
		}
	}
	if lr := topology.GetRegistry(c.ctx); lr != nil {
		local, ok := lr.(interface{ LookupLocal(string) (pid.PID, bool) })
		if !ok {
			return pid.PID{}, false, fmt.Errorf("eventualreg: LOCAL registry %T lacks LookupLocal", lr)
		}
		if p, found := local.LookupLocal(name); found {
			if !p.Equal(proposed) {
				return p, true, nil
			}
			matching = true
		}
	}
	if matching {
		return proposed, true, nil
	}
	return pid.PID{}, false, nil
}

// NameReady reports the join-epoch barrier status from the global registry.
// When no global registry is wired (cluster/raft disabled) the node has no
// Strong namespace to learn, so it is always ready.
func (c *crossScopeChecker) NameReady() bool {
	if c == nil {
		return true
	}
	if gr := topology.GetGlobalRegistry(c.ctx); gr != nil {
		return gr.NameReady()
	}
	return true
}

// localPresenceChecker satisfies global.LocalPresence. It reads LOCAL and
// EVENTUAL non-presence for the Strong-scope conditional ack WITHOUT going
// through the composed lookups (which re-enter globalreg and would
// self-reference a held reservation). LOCAL uses PIDRegistry.LookupLocal — the
// registry's own table only; EVENTUAL uses the eventual registry's own state.
// Registries are resolved lazily because they land in context as their
// components load.
type localPresenceChecker struct {
	ctx context.Context
}

func (c *localPresenceChecker) LookupLocal(name string) (pid.PID, bool) {
	if c == nil {
		return pid.PID{}, false
	}
	lr := topology.GetRegistry(c.ctx)
	if lr == nil {
		return pid.PID{}, false
	}
	local, ok := lr.(interface {
		LookupLocal(string) (pid.PID, bool)
	})
	if !ok {
		return pid.PID{}, false
	}
	return local.LookupLocal(name)
}

func (c *localPresenceChecker) LookupEventual(name string) (pid.PID, bool) {
	if c == nil {
		return pid.PID{}, false
	}
	er := topology.GetEventualRegistry(c.ctx)
	if er == nil {
		return pid.PID{}, false
	}
	if raw, ok := er.(interface {
		LookupUnfiltered(string) (pid.PID, bool)
	}); ok {
		return raw.LookupUnfiltered(name)
	}
	res, err := er.Lookup(c.ctx, name)
	if err != nil {
		return pid.PID{}, false
	}
	return res.PID, res.Found
}

func (c *localPresenceChecker) conflictingClaim(name string, proposed pid.PID) (pid.PID, bool, error) {
	if cp, ok := c.LookupLocal(name); ok && !cp.Equal(proposed) {
		return cp, true, nil
	}
	// EVENTUAL's visible winner is insufficient: an old or concurrent live
	// dot may hide underneath it and reappear when the winner unregisters.
	// Inspect raw per-origin state under the shared admission gate. Public
	// EVENTUAL Lookup filters Strong reservations and cannot be vote evidence.
	if er := topology.GetEventualRegistry(c.ctx); er != nil {
		raw, ok := er.(interface {
			ConflictingLiveClaim(string, pid.PID) (pid.PID, bool, error)
		})
		if !ok {
			return pid.PID{}, false, fmt.Errorf("strong naming: EVENTUAL registry %T lacks ConflictingLiveClaim", er)
		}
		return raw.ConflictingLiveClaim(name, proposed)
	}
	return pid.PID{}, false, nil
}

// localNameRevoker satisfies global.LocalNameRevoker. The join-epoch barrier
// uses it to drop a LOCAL or EVENTUAL binding this node holds for a name a Strong
// reservation owns cluster-wide, before flipping name_ready. Registries are
// resolved lazily because they land in context as their components load.
type localNameRevoker struct {
	ctx context.Context
}

// RevokeLocal removes a LOCAL binding of name held to a pid different from keep
// and signals the losing process via the relay router. Returns true if revoked.
func (c *localNameRevoker) RevokeLocal(name string, keep pid.PID) bool {
	if c == nil {
		return false
	}
	lr := topology.GetRegistry(c.ctx)
	if lr == nil {
		return false
	}
	local, ok := lr.(interface {
		LookupLocal(string) (pid.PID, bool)
	})
	if !ok {
		return false
	}
	held, found := local.LookupLocal(name)
	if !found || held == keep {
		return false
	}
	if !lr.Unregister(name) {
		return false
	}
	if router := relayapi.GetRouter(c.ctx); router != nil {
		_ = router.Send(topology.CancelPackage(topology.SystemPID, held, "name revoked: "+name))
	}
	return true
}

// RevokeEventual removes an EVENTUAL binding of name held to a pid different from
// keep, tombstoning and signaling via the eventual service. Returns true if
// revoked.
func (c *localNameRevoker) RevokeEventual(name string, keep pid.PID) bool {
	if c == nil {
		return false
	}
	er := topology.GetEventualRegistry(c.ctx)
	if er == nil {
		return false
	}
	rev, ok := er.(interface {
		RevokeForStrong(string, pid.PID) bool
	})
	if !ok {
		return false
	}
	return rev.RevokeForStrong(name, keep)
}
