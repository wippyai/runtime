// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/system/globalreg"
	"github.com/wippyai/runtime/system/health"
	sysraft "github.com/wippyai/runtime/system/raft"
	sysrelay "github.com/wippyai/runtime/system/relay"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// raftLivenessLastContactCeiling is the maximum time since last leader
// contact at which the activity-based liveness check still reports
// healthy for a VOTER follower. Calibrated to be a few times the
// heartbeat timeout so we don't flap during a normal election.
const raftLivenessLastContactCeiling = 30 * time.Second

// raftLivenessNonVoterCeiling is the equivalent ceiling for non-voter
// (learner) followers. At scale the leader fans heartbeats out to
// every server each tick — at 60+ replicas this fan-out alone bounds
// the per-follower heartbeat rate, and a non-voter's last_contact
// naturally lags well past the 30s voter ceiling under chaos.
//
// A non-voter that loses contact does not affect quorum. The cost of
// being wrong here is bounded: the worst case is a partitioned learner
// stays up serving stale snapshots — which is exactly the role of a
// non-voter — versus the previous behavior where kubelet would cycle
// every non-voter under chaos load and cascade the cluster. Detection
// of permanent isolation falls to the gossip-side health check
// (cluster.gossip), which has its own staleness window.
const raftLivenessNonVoterCeiling = 5 * time.Minute

// Context keys for raft and global registry components.
var (
	raftNodeKey     = &ctxapi.Key{Name: "raft.node"}
	globalRegFSMKey = &ctxapi.Key{Name: "globalreg.fsm"}
	globalRegSvcKey = &ctxapi.Key{Name: "globalreg.service"}
)

// Raft returns a boot component that initializes the Raft consensus layer
// and the global registry service. Raft is only active when the cluster
// is enabled and raft is explicitly enabled in config.
func Raft() boot.Component {
	var raftNode *sysraft.Node
	var memberHandler *sysraft.MembershipHandler
	var globalRegSvc *globalreg.Service
	var logger *zap.Logger
	var handlerCfg sysraft.HandlerConfig

	return boot.New(boot.P{
		Name:      RaftName,
		DependsOn: []boot.Name{ClusterName, TopologyName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("raft")
			cfg := boot.GetConfig(ctx)

			if cfg == nil {
				return ctx, nil
			}

			raftCfg := cfg.Sub(RaftName)
			if !raftCfg.GetBool(RaftEnabled, false) {
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

			topo := topology.GetTopology(ctx)
			if topo == nil {
				return ctx, nil
			}

			// Build raft config from the configuration.
			//
			// snapshot_threshold / snapshot_interval / snapshot_retain /
			// trailing_logs / heartbeat_timeout / election_timeout /
			// commit_timeout were previously not threaded from the boot
			// config. That meant operators setting `raft.trailing_logs: 100`
			// in the runtime configmap had no effect — hashicorp/raft would
			// keep the default 10240 entries in memory, a real source of
			// memory pressure under partition.
			rc := raftapi.Config{
				DataDir:           raftCfg.GetString(RaftDataDir, ""),
				BindAddr:          raftCfg.GetString(RaftBindAddr, "0.0.0.0"),
				BindPort:          raftCfg.GetInt(RaftBindPort, 7960),
				AutoPort:          raftCfg.GetBool(RaftAutoPort, true),
				AdvertiseAddr:     raftCfg.GetString(RaftAdvertiseAddr, ""),
				Bootstrap:         raftCfg.GetBool(RaftBootstrap, false),
				SnapshotThreshold: uint64(raftCfg.GetInt(RaftSnapshotThreshold, 0)),
				SnapshotInterval:  raftCfg.GetDuration(RaftSnapshotInterval, 0),
				SnapshotRetain:    raftCfg.GetInt(RaftSnapshotRetain, 0),
				TrailingLogs:      uint64(raftCfg.GetInt(RaftTrailingLogs, 0)),
				MaxAppendEntries:  raftCfg.GetInt(RaftMaxAppendEntries, 0),
				HeartbeatTimeout:  raftCfg.GetDuration(RaftHeartbeatTimeout, 0),
				ElectionTimeout:   raftCfg.GetDuration(RaftElectionTimeout, 0),
				CommitTimeout:     raftCfg.GetDuration(RaftCommitTimeout, 0),
			}

			// Capture handler config for the Start phase. Reading it here
			// keeps all config parsing in Load.
			handlerCfg = sysraft.HandlerConfig{
				MaxVoters:         raftCfg.GetInt(RaftMaxVoters, 5),
				ReconcileDebounce: raftCfg.GetDuration(RaftReconcileDebounce, 2*time.Second),
				ReconcileTimeout:  raftCfg.GetDuration(RaftReconcileTimeout, 2*time.Second),
			}

			// Create the global registry FSM (the state machine Raft replicates).
			fsm := globalreg.NewFSM()

			// Create the Raft node, wired to the metrics collector and global
			// OTel providers so raft_*, gossip_*, pg_* series flow when those
			// subsystems do work.
			coll := metricsapi.GetCollector(ctx)
			mp := otel.GetMeterProvider()
			tp := otel.GetTracerProvider()

			raftNode = sysraft.NewNode(node.ID(), wrapFSM(fsm), rc, bus, logger.Named("node"), coll, mp, tp)

			// Wire the internode connection manager so the mesh-backed
			// transport can register its ClassRaftMesh receiver during
			// Start. Without it, Start fails fast with a clear error.
			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				if v := ac.Get(connMgrKey); v != nil {
					if cm, ok := v.(internode.ConnectionManager); ok {
						raftNode.SetConnectionManager(cm)
					}
				}
			}

			// Create the global registry service wrapping Raft + FSM.
			router := relay.GetRouter(ctx)
			if router == nil {
				return ctx, fmt.Errorf("raft: relay router not available")
			}
			globalRegSvc = globalreg.NewService(
				raftNode, fsm, bus, topo,
				router,
				nil,
				node.ID(),
				logger.Named("globalreg"),
				coll, mp, tp,
			)

			// Wire the global registry into the Router for receiver-side
			// fence token validation on every message send. The fence
			// rejection callback funnels into the globalreg telemetry so
			// the relay package can stay metrics-agnostic.
			if concreteRouter, ok := router.(*sysrelay.Router); ok {
				concreteRouter.SetGlobalRegistry(globalRegSvc)
				concreteRouter.SetOnFenceReject(globalRegSvc.RecordFenceRejection)
			}

			// Register the global registry as a relay host synchronously
			// so exit events from topology can route to it.
			if err := node.RegisterHost(globalreg.HostID, globalRegSvc); err != nil {
				return ctx, fmt.Errorf("raft: register globalreg relay host: %w", err)
			}

			// Store components in context.
			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				ac.With(raftNodeKey, raftNode)
				ac.With(globalRegFSMKey, fsm)
				ac.With(globalRegSvcKey, globalRegSvc)
			}

			// Register the global registry in the topology context
			// so PIDRegistry can check it for conflicts.
			ctx = raftapi.WithService(ctx, raftNode)

			// Wire the global registry into the PID registry for
			// transparent lookup (global names take priority over local).
			pidReg := topology.GetRegistry(ctx)
			if pidReg != nil {
				if setter, ok := pidReg.(interface{ SetGlobalRegistry(topology.GlobalRegistry) }); ok {
					setter.SetGlobalRegistry(globalRegSvc)
				}
			}

			// Register on both context keys:
			// - topology.GlobalRegistry for PIDRegistry integration (transparent lookup)
			// - globalreg.Registry for direct Lua module access
			ctx = topology.WithGlobalRegistry(ctx, globalRegSvc)
			ctx = globalregapi.WithRegistry(ctx, globalRegSvc)

			logger.Info("raft initialized",
				zap.String("bind_addr", rc.BindAddr), //nolint:staticcheck // diagnostic: surfaces value during deprecation cycle
				zap.Int("bind_port", rc.BindPort),    //nolint:staticcheck // diagnostic: surfaces value during deprecation cycle
				zap.Bool("auto_port", rc.AutoPort),   //nolint:staticcheck // diagnostic: surfaces value during deprecation cycle
				zap.Bool("bootstrap", rc.Bootstrap),
			)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if raftNode == nil {
				return nil
			}

			logger.Info("starting raft node")
			statusCh, err := raftNode.Start(ctx)
			if err != nil {
				return fmt.Errorf("start raft: %w", err)
			}
			// Drain the status channel in background so it doesn't block senders.
			go func() {
				for range statusCh { //nolint:revive // intentionally empty: draining channel
				}
			}()

			// Liveness check: a follower that has not heard from the leader
			// in the per-role ceiling is on the wrong side of a partition.
			// Leaders return time.Time{} from LastContact, which we accept
			// as healthy (the leader IS the contact).
			//
			// Voter and non-voter followers use different ceilings: voters
			// gate quorum and must be timely (30s); non-voters are
			// replication-only and at 60+ replica scale the leader's
			// heartbeat fan-out alone makes a tighter window unachievable.
			// See the constants for the rationale.
			health.Register("raft.last_contact", func() error {
				switch raftNode.State() {
				case raftapi.Leader:
					return nil
				case raftapi.Shutdown:
					return fmt.Errorf("raft shut down")
				}
				last := raftNode.LastContact()
				if last.IsZero() {
					// Follower with never-contacted leader (just started or
					// permanently isolated). Tolerated for the kubelet
					// initialDelay window; after that the ceiling kicks in.
					return nil
				}
				ceiling := raftLivenessLastContactCeiling
				role := "voter"
				if !raftNode.IsVoter() {
					ceiling = raftLivenessNonVoterCeiling
					role = "non-voter"
				}
				if since := time.Since(last); since > ceiling {
					return fmt.Errorf("no leader contact in %s (%s ceiling %s)",
						since.Round(time.Second), role, ceiling)
				}
				return nil
			})

			// Add raft_port to membership node metadata so other nodes
			// know how to reach our Raft transport.
			actualPort := raftNode.ActualPort()
			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				if val := ac.Get(membershipServiceKey); val != nil {
					if m, ok := val.(interface{ UpdateMeta(map[string]string) }); ok {
						m.UpdateMeta(map[string]string{
							"raft_port": strconv.Itoa(actualPort),
						})
					}
				}
			}

			// Wait for leader election before proceeding. For a single-node
			// bootstrap this is near-instant; for multi-node it may take a few seconds.
			leaderCh := raftNode.LeaderCh()
			select {
			case <-leaderCh:
				logger.Info("raft leader election completed")
			case <-time.After(10 * time.Second):
				logger.Warn("raft leader election timed out (continuing anyway)")
			}

			// Resolve cluster membership once for both the raft membership
			// handler and the globalreg Root-scope path. Without membership
			// the reconciler cannot read raft_port hints and Root scope
			// cannot snapshot the live-node set, so we log per-feature.
			var membership clusterapi.Membership
			if ac != nil {
				if val := ac.Get(membershipServiceKey); val != nil {
					if m, ok := val.(clusterapi.Membership); ok {
						membership = m
					}
				}
			}

			// Start membership handler to sync Raft voters with cluster membership.
			bus := event.GetBus(ctx)
			if bus != nil && ac != nil {
				if membership == nil {
					logger.Warn("raft membership handler skipped: cluster.Membership not available in context")
				} else {
					memberHandler = sysraft.NewMembershipHandler(
						raftNode, membership, bus, handlerCfg, logger.Named("membership"),
					)
					if err := memberHandler.Start(ctx); err != nil {
						return fmt.Errorf("start raft membership handler: %w", err)
					}
				}
			}

			// Start the global registry service.
			if globalRegSvc != nil {
				if membership != nil {
					globalRegSvc.SetMembership(membership)
				} else {
					logger.Warn("globalreg Root scope disabled: cluster.Membership not available")
				}
				if _, err := globalRegSvc.Start(ctx); err != nil {
					return fmt.Errorf("start global registry: %w", err)
				}
			}

			logger.Info("raft node started", zap.Int("port", actualPort))
			return nil
		},
		Stop: func(_ context.Context) error {
			if globalRegSvc != nil {
				logger.Info("stopping global registry service")
				_ = globalRegSvc.Stop(context.Background())
			}

			if memberHandler != nil {
				memberHandler.Stop()
			}

			if raftNode != nil {
				logger.Info("stopping raft node")
				if err := raftNode.Stop(context.Background()); err != nil {
					logger.Error("failed to stop raft node", zap.Error(err))
				}
			}

			return nil
		},
	})
}
