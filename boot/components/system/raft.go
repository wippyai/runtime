// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
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
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/globalreg"
	"github.com/wippyai/runtime/system/health"
	sysraft "github.com/wippyai/runtime/system/raft"
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
	var nodeLeftSub *eventbus.Subscriber
	var bootstrapWatcher *sysraft.BootstrapWatcher
	var logger *zap.Logger
	var handlerCfg sysraft.HandlerConfig
	var bootstrapExpect int

	return boot.New(boot.P{
		Name:      RaftName,
		DependsOn: []boot.Name{ClusterName, TopologyName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger = logapi.GetLogger(ctx).Named("raft")
			cfg := boot.GetConfig(ctx)

			if cfg == nil {
				return ctx, nil
			}

			// Raft config lives under cluster.raft.* — enabling cluster
			// auto-enables raft (default true). Set cluster.raft.enabled=false
			// to opt out (e.g. workers that should never be raft members).
			raftCfg := cfg.Sub(ClusterName)
			if !raftCfg.GetBool(ClusterRaftEnabled, true) {
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

			// Raft runs diskless (no data_dir): cluster state is ephemeral,
			// restarts rejoin from peer state. Mesh transport addresses peers
			// by NodeID over the internode layer, so no bind_addr/port.
			//
			// Cluster formation follows the Consul/Nomad gossip-driven pattern:
			// each node ships the single knob BootstrapExpect (the expected
			// initial-quorum size) and joins gossip. A small watcher observes
			// the converged gossip view; when exactly BootstrapExpect
			// raft-eligible peers are stably visible all of them
			// deterministically derive the same sorted server list and call
			// BootstrapCluster with it. Nodes joining a running cluster see
			// existing peers with raft_status=in and skip bootstrap; the
			// leader's reconciler adds them via AddVoter.
			bootstrapExpect = raftCfg.GetInt(ClusterRaftBootstrapExpect, 1)
			rc := raftapi.Config{
				BootstrapExpect:   bootstrapExpect,
				SnapshotThreshold: uint64(raftCfg.GetInt(ClusterRaftSnapshotThreshold, 0)),
				SnapshotInterval:  raftCfg.GetDuration(ClusterRaftSnapshotInterval, 0),
				SnapshotRetain:    raftCfg.GetInt(ClusterRaftSnapshotRetain, 0),
				TrailingLogs:      uint64(raftCfg.GetInt(ClusterRaftTrailingLogs, 0)),
				MaxAppendEntries:  raftCfg.GetInt(ClusterRaftMaxAppendEntries, 0),
				HeartbeatTimeout:  raftCfg.GetDuration(ClusterRaftHeartbeatTimeout, 0),
				ElectionTimeout:   raftCfg.GetDuration(ClusterRaftElectionTimeout, 0),
				CommitTimeout:     raftCfg.GetDuration(ClusterRaftCommitTimeout, 0),
			}

			// Capture handler config for the Start phase. Reading it here
			// keeps all config parsing in Load.
			handlerCfg = sysraft.HandlerConfig{
				MaxVoters:         raftCfg.GetInt(ClusterRaftMaxVoters, 5),
				MaxStandbys:       raftCfg.GetInt(ClusterRaftMaxStandbys, 4),
				ReconcileDebounce: raftCfg.GetDuration(ClusterRaftReconcileDebounce, 2*time.Second),
				ReconcileTimeout:  raftCfg.GetDuration(ClusterRaftReconcileTimeout, 2*time.Second),
			}

			// Raft rides the internode mesh exclusively. Without a connection
			// manager (cluster transport not wired — e.g. a single-node
			// process with no internode layer) raft cannot form, so the whole
			// component no-ops: raftNode stays nil and every Start-phase hook
			// guards on it. This is the path single-node/CLI runs take when
			// cluster gossip is enabled but the internode mesh is not.
			var connMgr internode.ConnectionManager
			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				if v := ac.Get(connMgrKey); v != nil {
					connMgr, _ = v.(internode.ConnectionManager)
				}
			}
			if connMgr == nil {
				logger.Warn("raft disabled: internode connection manager not available (no mesh transport)")
				return ctx, nil
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
			raftNode.SetConnectionManager(connMgr)

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

			// Tune the leader-reachability monitor that gates name-readiness.
			// Zero values keep the service defaults.
			globalRegSvc.SetLeaderProbeConfig(
				raftCfg.GetDuration(ClusterRaftLeaderProbeInterval, 0),
				raftCfg.GetInt(ClusterRaftLeaderProbeGrace, 0),
			)

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

			// Wire the LOCAL/EVENTUAL presence reader used by the Strong-scope
			// conditional ack. Resolution is lazy because the eventual registry
			// lands in context after a separate component loads; a call-time
			// lookup catches whichever registries are wired by then.
			globalRegSvc.SetLocalPresence(&localPresenceChecker{ctx: ctx})

			// Wire the LOCAL/EVENTUAL revoker the join-epoch barrier uses to drop
			// conflicting names before flipping ready. Lazy resolution mirrors the
			// presence checker.
			globalRegSvc.SetLocalNameRevoker(&localNameRevoker{ctx: ctx})

			// Wire the active-binding dissemination plane. The Dissem is a
			// UserDelegate on the membership multiplex (kind 0xC1) that gossips
			// leader-broadcast active-binding deltas to every node, including
			// non-members whose local FSM is empty. The Service Lookup consults
			// the cache on FSM-miss so non-members resolve names locally.
			if mem := clusterapi.GetMembership(ctx); mem != nil {
				if memSvc, ok := mem.(*membership.Service); ok {
					dissem := globalreg.NewDissem(node.ID(), logger.Named("dissem"))
					if err := memSvc.RegisterUserDelegate(dissem); err != nil {
						return ctx, fmt.Errorf("raft: register globalreg dissem delegate: %w", err)
					}
					globalRegSvc.SetDissem(dissem)
				}
			}

			logger.Info("raft initialized",
				zap.Int("bootstrap_expect", rc.BootstrapExpect),
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

			// Resolve cluster membership once for both the raft membership
			// handler and the globalreg Strong-scope path. Without membership
			// the reconciler cannot read node metadata for candidate selection
			// and Strong scope cannot snapshot the live-node set, so we log
			// per-feature.
			membership := clusterapi.GetMembership(ctx)
			bus := event.GetBus(ctx)

			// Start the gossip-driven bootstrap watcher BEFORE waiting on
			// leader election so the watcher can actually form the cluster
			// during that wait. The watcher is a no-op when BootstrapExpect
			// is 0 (joining an existing cluster); for Expect==1 it
			// bootstraps with self synchronously inside Start.
			if membership != nil && bus != nil {
				bootstrapWatcher = sysraft.NewBootstrapWatcher(
					raftNode.LocalID(),
					sysraft.BootstrapWatcherConfig{Expect: bootstrapExpect},
					raftNode,
					membership,
					bus,
					logger.Named("bootstrap"),
				)
				if err := bootstrapWatcher.Start(ctx); err != nil {
					return fmt.Errorf("start raft bootstrap watcher: %w", err)
				}
			} else if bootstrapExpect != 0 {
				logger.Warn("bootstrap watcher skipped (no membership or bus); raft cannot form a cluster",
					zap.Int("bootstrap_expect", bootstrapExpect))
			}

			// Wait for leader election before proceeding. For a single-node
			// bootstrap this is near-instant; for multi-node it may take
			// longer while the watcher waits for peers in gossip.
			leaderCh := raftNode.LeaderCh()
			select {
			case <-leaderCh:
				logger.Info("raft leader election completed")
			case <-time.After(10 * time.Second):
				logger.Warn("raft leader election timed out (continuing anyway)")
			}

			// Start membership handler to sync Raft voters with cluster membership.
			if bus != nil {
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

			// Tear down a departed node's mesh transport session on
			// NodeLeft so its per-peer yamux session, classConn, and
			// acceptLoop goroutine are released instead of leaking, and so
			// a rejoin builds a fresh session against the new incarnation.
			if bus != nil {
				sub, err := eventbus.NewSubscriber(ctx, bus, clusterapi.System, clusterapi.NodeLeft,
					func(e event.Event) {
						ne, ok := e.Data.(clusterapi.NodeEvent)
						if !ok {
							return
						}
						raftNode.OnNodeLeft(ne.Node.ID)
					})
				if err != nil {
					return fmt.Errorf("subscribe raft node-left: %w", err)
				}
				nodeLeftSub = sub
			}

			// Start the global registry service.
			if globalRegSvc != nil {
				if membership != nil {
					globalRegSvc.SetMembership(membership)
				} else {
					logger.Warn("globalreg Strong scope disabled: cluster.Membership not available")
				}
				// Wire the deterministic raft-member derivation seam. The
				// closure captures the cluster-uniform MaxVoters/MaxStandbys
				// so every node arrives at the same member set for the same
				// gossip snapshot — the shared write plane non-members fall
				// back to when no leader is directly known.
				maxVoters := handlerCfg.MaxVoters
				maxStandbys := handlerCfg.MaxStandbys
				globalRegSvc.SetMemberDeriver(func(nodes []clusterapi.NodeInfo) []clusterapi.NodeID {
					return sysraft.DeriveMembers(nodes, maxVoters, maxStandbys)
				})
				if _, err := globalRegSvc.Start(ctx); err != nil {
					return fmt.Errorf("start global registry: %w", err)
				}
			}

			logger.Info("raft node started")
			return nil
		},
		Stop: func(_ context.Context) error {
			if nodeLeftSub != nil {
				nodeLeftSub.Close()
			}

			if bootstrapWatcher != nil {
				bootstrapWatcher.Stop()
			}

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

