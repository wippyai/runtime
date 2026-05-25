// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/kv"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/kveventual"
	"go.uber.org/zap"
)

// KVEventual returns the boot component for the gossip-based KV store.
func KVEventual() boot.Component {
	var svc *kveventual.Service

	return boot.New(boot.P{
		Name:      KVEventualName,
		DependsOn: []boot.Name{ClusterName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("kveventual")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			m := clusterapi.GetMembership(ctx)
			if m == nil {
				logger.Debug("kveventual: cluster disabled, skipping")
				return ctx, nil
			}
			memSvc, ok := m.(*membership.Service)
			if !ok {
				return ctx, fmt.Errorf("kveventual: membership service has unexpected type %T", m)
			}

			cfg := kveventual.Config{
				LocalNodeID:      memSvc.LocalNode().ID,
				Peers:            &membershipPeerInventory{m: memSvc},
				MetricsCollector: metricsapi.GetCollector(ctx),
				Logger:           logger,
			}
			svc = kveventual.NewService(cfg)

			if err := memSvc.RegisterUserDelegate(svc); err != nil {
				return ctx, fmt.Errorf("kveventual: register delegate: %w", err)
			}

			combo := kvComboGet(ctx)
			combo.eventual = svc
			ctx = kv.WithRegistry(ctx, combo)

			logger.Info("kveventual loaded", zap.String("node", cfg.LocalNodeID))
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

// kvProviderEventualBackend is the small surface kvComboProvider needs to
// resolve eventual spaces. Lets us avoid leaking kveventual.Service across
// the boot boundary.
type kvProviderEventualBackend interface {
	Open(name string) (kv.KV, error)
}

// kvProviderRaftBackend is the strong-consistency analog.
type kvProviderRaftBackend interface {
	Open(name string) (kv.KV, error)
}

// kvComboProvider implements kv.ProviderRegistry by dispatching to whichever
// backend supports the requested mode. Either side may be nil — the missing
// mode returns ErrSpaceUnknown so userland can detect "feature disabled".
type kvComboProvider struct {
	raft     kvProviderRaftBackend
	eventual kvProviderEventualBackend
}

func (c *kvComboProvider) OpenRaft(name string) (kv.KV, error) {
	if c == nil || c.raft == nil {
		return nil, kv.ErrSpaceUnknown
	}
	return c.raft.Open(name)
}

func (c *kvComboProvider) OpenEventual(name string) (kv.KV, error) {
	if c == nil || c.eventual == nil {
		return nil, kv.ErrSpaceUnknown
	}
	return c.eventual.Open(name)
}

var kvComboKey = &ctxapi.Key{Name: "kv.combo_provider"}

// kvComboGet returns the per-process combo provider, creating one on first
// call. Stored in the AppContext so both KV boot components share the same
// instance.
func kvComboGet(ctx context.Context) *kvComboProvider {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if v := ac.Get(kvComboKey); v != nil {
		return v.(*kvComboProvider)
	}
	combo := &kvComboProvider{}
	ac.With(kvComboKey, combo)
	return combo
}
