// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"path/filepath"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	eventapi "github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/membership"
	systemkv "github.com/wippyai/runtime/system/kv"
	"go.uber.org/zap"
)

var kvCRDTEngineKey = &ctxapi.Key{Name: "kv.crdt.engine"}

// GetKVCRDTEngine returns the shared gossip-CRDT kv engine, or nil when the
// cluster is disabled. store.kv.crdt entries scope it by namespace.
func GetKVCRDTEngine(ctx context.Context) *systemkv.CRDTEngine {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	v := ac.Get(kvCRDTEngineKey)
	if v == nil {
		return nil
	}
	eng, _ := v.(*systemkv.CRDTEngine)
	return eng
}

// KVCRDT returns the boot component for the gossip-CRDT kv engine. It registers
// a membership user-delegate so deltas and full-state push/pull replicate
// store.kv.crdt data across the cluster.
func KVCRDT() boot.Component {
	var engine *systemkv.CRDTEngine

	return boot.New(boot.P{
		Name:      KVCRDTName,
		DependsOn: []boot.Name{ClusterName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("kv-crdt")

			m := clusterapi.GetMembership(ctx)
			if m == nil {
				logger.Debug("kv-crdt: cluster disabled, skipping")
				return ctx, nil
			}
			memSvc, ok := m.(*membership.Service)
			if !ok {
				return ctx, nil
			}

			node := relayapi.GetNode(ctx)
			if node == nil {
				return ctx, nil
			}

			engine = systemkv.NewCRDTEngine(node.ID(), eventapi.GetBus(ctx), logger)

			if cfg := boot.GetConfig(ctx); cfg != nil {
				clusterCfg := cfg.Sub(ClusterName)
				engine.SetTombstoneRetention(clusterCfg.GetDuration(
					ClusterKVCRDTTombstoneRetention,
					systemkv.DefaultTombstoneRetention,
				))
				if clusterCfg.GetBool(ClusterKVCRDTTombstoneGCAlivePeers, false) {
					engine.SetAlivePeers(func() map[string]struct{} {
						alive := map[string]struct{}{node.ID(): {}}
						for _, n := range memSvc.Nodes() {
							if n.ID != "" {
								alive[n.ID] = struct{}{}
							}
						}
						return alive
					})
				}
				if base := nodeDataDir(clusterCfg); base != "" {
					engine.SetDurability(filepath.Join(base, "_sys", "kvcrdt"), 30*time.Second)
				}
			}

			delegate := systemkv.NewCRDTDelegate(engine)
			if err := memSvc.RegisterUserDelegate(delegate); err != nil {
				return ctx, err
			}

			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				ac.With(kvCRDTEngineKey, engine)
			}
			logger.Info("kv-crdt engine loaded", zap.String("node", node.ID()))
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if engine == nil {
				return nil
			}
			return engine.Start(ctx)
		},
		Stop: func(_ context.Context) error {
			if engine == nil {
				return nil
			}
			return engine.Stop()
		},
	})
}
