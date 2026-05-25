// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"time"

	hraft "github.com/hashicorp/raft"
	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/kv"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/system/kvraft"
	sysraft "github.com/wippyai/runtime/system/raft"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// kvraftDataDirSuffix is appended to RaftDataDir for the kvraft replication
// group's bolt store, so it doesn't share files with the globalreg raft.
const kvraftDataDirSuffix = "/kvraft"

// KVRaft returns a boot component that initializes a SECOND Raft replication
// group dedicated to the cluster KV store. It runs in parallel with
// globalreg's raft on a different bind port + data dir.
//
// Disabled by default — opt-in via `kvraft.enabled: true` in boot config.
// Reuses the same `cluster.raft` settings (max_voters, timeouts, etc.)
// unless overridden under `kvraft.*`.
func KVRaft() boot.Component {
	var raftNode *sysraft.Node
	var fsm *kvraft.FSM
	var svc *kvraft.Service
	var memberHandler *sysraft.MembershipHandler
	var handlerCfg sysraft.HandlerConfig
	var logger *zap.Logger

	return boot.New(boot.P{
		Name:      KVRaftName,
		DependsOn: []boot.Name{ClusterName, RaftName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("kvraft")
			cfg := boot.GetConfig(ctx)
			if cfg == nil {
				return ctx, nil
			}

			kvCfg := cfg.Sub(KVRaftName)
			if !kvCfg.GetBool(RaftEnabled, false) {
				logger.Debug("kvraft disabled")
				return ctx, nil
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, nil
			}
			node := relay.GetNode(ctx)
			if node == nil {
				return ctx, nil
			}

			// Derive raft.Config. Defaults to a port one above globalreg's
			// (so a typical pod uses 7960 for globalreg and 7965 for kvraft).
			rc := raftapi.Config{
				DataDir:           kvCfg.GetString(RaftDataDir, "") + kvraftDataDirSuffix,
				BindAddr:          kvCfg.GetString(RaftBindAddr, "0.0.0.0"),
				BindPort:          kvCfg.GetInt(RaftBindPort, 7965),
				AutoPort:          kvCfg.GetBool(RaftAutoPort, true),
				AdvertiseAddr:     kvCfg.GetString(RaftAdvertiseAddr, ""),
				Bootstrap:         kvCfg.GetBool(RaftBootstrap, false),
				SnapshotThreshold: uint64(kvCfg.GetInt(RaftSnapshotThreshold, 0)),
				SnapshotInterval:  kvCfg.GetDuration(RaftSnapshotInterval, 0),
				SnapshotRetain:    kvCfg.GetInt(RaftSnapshotRetain, 0),
				TrailingLogs:      uint64(kvCfg.GetInt(RaftTrailingLogs, 0)),
				MaxAppendEntries:  kvCfg.GetInt(RaftMaxAppendEntries, 0),
				HeartbeatTimeout:  kvCfg.GetDuration(RaftHeartbeatTimeout, 0),
				ElectionTimeout:   kvCfg.GetDuration(RaftElectionTimeout, 0),
				CommitTimeout:     kvCfg.GetDuration(RaftCommitTimeout, 0),
			}

			handlerCfg = sysraft.HandlerConfig{
				MaxVoters:         kvCfg.GetInt(RaftMaxVoters, 5),
				ReconcileDebounce: kvCfg.GetDuration(RaftReconcileDebounce, 2*time.Second),
				ReconcileTimeout:  kvCfg.GetDuration(RaftReconcileTimeout, 2*time.Second),
			}

			fsm = kvraft.NewFSM()

			coll := metricsapi.GetCollector(ctx)
			mp := otel.GetMeterProvider()
			tp := otel.GetTracerProvider()
			raftNode = sysraft.NewNode(node.ID(), wrapHraftFSM(fsm), rc, bus, logger.Named("node"), coll, mp, tp)

			service, err := kvraft.NewService(kvraft.Config{
				Raft:             raftNode,
				FSM:              fsm,
				MetricsCollector: coll,
				Logger:           logger,
			})
			if err != nil {
				return ctx, fmt.Errorf("kvraft: new service: %w", err)
			}
			svc = service

			combo := kvComboGet(ctx)
			combo.raft = svc
			ctx = kv.WithRegistry(ctx, combo)

			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				ac.With(kvRaftSvcKey, svc)
				ac.With(kvRaftNodeKey, raftNode)
			}

			logger.Info("kvraft initialized",
				zap.String("bind_addr", rc.BindAddr), //nolint:staticcheck // diagnostic: surfaces value during deprecation cycle
				zap.Int("bind_port", rc.BindPort),    //nolint:staticcheck // diagnostic: surfaces value during deprecation cycle
				zap.Bool("bootstrap", rc.Bootstrap))
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if raftNode == nil {
				return nil
			}
			if _, err := raftNode.Start(ctx); err != nil {
				return fmt.Errorf("kvraft: start raft node: %w", err)
			}
			memSvc := clusterapi.GetMembership(ctx)
			if memSvc != nil {
				memberHandler = sysraft.NewMembershipHandler(raftNode, memSvc, event.GetBus(ctx), handlerCfg, logger)
				if err := memberHandler.Start(ctx); err != nil {
					return fmt.Errorf("kvraft: start membership handler: %w", err)
				}
			}
			if svc != nil {
				if err := svc.Start(ctx); err != nil {
					return fmt.Errorf("kvraft: start service: %w", err)
				}
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if svc != nil {
				_ = svc.Stop()
			}
			if memberHandler != nil {
				memberHandler.Stop()
			}
			if raftNode != nil {
				_ = raftNode.Stop(context.Background())
			}
			return nil
		},
	})
}

// kvRaftSvcKey holds the kvraft.Service in the app context.
var kvRaftSvcKey = &ctxapi.Key{Name: "kvraft.service"}

// kvRaftNodeKey holds the *sysraft.Node powering the kvraft replication
// group, so the admin HTTP server can publish its raft status alongside
// globalreg's.
var kvRaftNodeKey = &ctxapi.Key{Name: "kvraft.node"}

// wrapHraftFSM is a no-op trampoline matching `wrapFSM` over in raft.go —
// kept here so kvraft doesn't depend on a globalreg-specific helper. The
// kvraft.FSM already implements hraft.FSM; we just type-assert/wrap as
// needed for instrumentation hooks.
func wrapHraftFSM(f *kvraft.FSM) hraft.FSM { return f }
