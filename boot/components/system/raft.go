// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	raftapi "github.com/wippyai/runtime/api/cluster/raft"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/pid"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	globalapi "github.com/wippyai/runtime/api/topology/namereg/global"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/cluster/membership"
	sysraft "github.com/wippyai/runtime/cluster/raft"
	"github.com/wippyai/runtime/cluster/raft/multiplex"
	"github.com/wippyai/runtime/system/eventbus"
	"github.com/wippyai/runtime/system/health"
	systemkv "github.com/wippyai/runtime/system/kv"
	"github.com/wippyai/runtime/system/topology/namereg/global"
	"github.com/wippyai/runtime/system/topology/namereg/kvbacked"
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
	globalRegFSMKey = &ctxapi.Key{Name: "global.fsm"}
	globalRegSvcKey = &ctxapi.Key{Name: "global.service"}
	kvEngineKey     = &ctxapi.Key{Name: "kv.raft.engine"}
)

// GetKVRaftEngine returns the shared raft-backed kv engine wired by the raft
// boot component, or nil when raft is disabled. store.kv.raft entries scope it
// by namespace.
func GetKVRaftEngine(ctx context.Context) *systemkv.RaftEngine {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	v := ac.Get(kvEngineKey)
	if v == nil {
		return nil
	}
	eng, _ := v.(*systemkv.RaftEngine)
	return eng
}

// Raft returns a boot component that initializes the Raft consensus layer
// and the global registry service. Raft is only active when the cluster
// is enabled and raft is explicitly enabled in config.
// loadClientRegistry wires the kv-backed name registry on a node that runs no
// raft Node (cluster.raft.role=client / raft.enabled=false). Such a node has no
// FSM, so it forwards every kv op over the relay to a raft member it picks from
// the gossip view (sysraft.PickForwardTarget); that member re-forwards to the
// leader. Lookups resolve from the gossiped dissem cache, then cold-miss
// forward-resolve through the leader. The registry is exposed on the same two
// context facades as the server path, so every consumer is backend-agnostic.
//
// It is a no-op (returns ctx unchanged) when the kv backend is not selected or a
// prerequisite (relay, membership, topology) is missing — preserving the prior
// behavior where a client wired no global registry at all. raftCfg is cluster.*.
func loadClientRegistry(ctx context.Context, raftCfg boot.Config, logger *zap.Logger) (context.Context, error) {
	if logger == nil {
		logger = zap.NewNop()
	}
	if !strings.EqualFold(
		raftCfg.GetString(ClusterRaftRegistryBackend, registryBackendKV), registryBackendKV) {
		return ctx, nil
	}

	bus := event.GetBus(ctx)
	node := relay.GetNode(ctx)
	router := relay.GetRouter(ctx)
	topo := topology.GetTopology(ctx)
	memSvc, _ := clusterapi.GetMembership(ctx).(*membership.Service)
	if bus == nil || node == nil || router == nil || topo == nil || memSvc == nil {
		return ctx, nil
	}

	selfID := node.ID()
	kvFSM := systemkv.NewRaftFSM(bus)
	submitter := systemkv.ClientSubmitter{Resolve: func() (raftapi.ServerID, bool) {
		return sysraft.PickForwardTarget(memSvc.Nodes(), selfID)
	}}
	kvEngine := systemkv.NewRaftEngine(submitter, kvFSM, bus, selfID, router, logger.Named("kv"))
	if err := node.RegisterHost(systemkv.KVRaftHostID, kvEngine); err != nil {
		return ctx, fmt.Errorf("raft(client): register kv relay host: %w", err)
	}
	if err := kvEngine.Start(ctx); err != nil {
		return ctx, fmt.Errorf("raft(client): start kv engine: %w", err)
	}

	kvReg := kvbacked.NewService(kvEngine, selfID, nil, logger.Named("kvreg"))
	kvReg.SetTopology(topo)
	kvReg.SetNonMember(func() bool { return true })
	kvReg.SetLeaderFunc(func() bool { return false })
	if err := node.RegisterHost(kvbacked.RegistryHostID, kvReg); err != nil {
		return ctx, fmt.Errorf("raft(client): register kv registry relay host: %w", err)
	}

	dissem := global.NewDissem(selfID, logger.Named("dissem"))
	if err := memSvc.RegisterUserDelegate(dissem); err != nil {
		return ctx, fmt.Errorf("raft(client): register dissem delegate: %w", err)
	}
	kvReg.ConfigureDissem(dissem)

	var liveReg interface {
		topology.GlobalRegistry
		globalapi.Registry
	} = kvReg

	if pidReg := topology.GetRegistry(ctx); pidReg != nil {
		if setter, ok := pidReg.(interface {
			SetGlobalRegistry(topology.GlobalRegistry)
		}); ok {
			setter.SetGlobalRegistry(liveReg)
		}
	}
	ctx = topology.WithGlobalRegistry(ctx, liveReg)
	ctx = globalapi.WithRegistry(ctx, liveReg)

	logger.Info("raft(client): kv name registry wired (non-member, forward-resolve)")
	return ctx, nil
}

func Raft() boot.Component {
	var raftNode *sysraft.Node
	var memberHandler *sysraft.MembershipHandler
	var globalRegSvc *global.Service
	var nodeLeftSub *eventbus.Subscriber
	var bootstrapWatcher *sysraft.BootstrapWatcher
	var logger *zap.Logger
	var handlerCfg sysraft.HandlerConfig
	var kvEngine *systemkv.RaftEngine
	var lockSvc *systemkv.LockService
	var kvReg *kvbacked.Service
	var useKVRegistry bool
	var bootstrapExpect int
	var globalDissemTombstoneRetention time.Duration

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
			// auto-enables a raft server (default). Set cluster.raft.role=client
			// (or cluster.raft.enabled=false) for nodes that should route over
			// gossip+dissem without running a raft Node.
			raftCfg := cfg.Sub(ClusterName)
			if !clusterRaftEnabled(raftCfg) {
				return loadClientRegistry(ctx, raftCfg, logger)
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

			// Raft is fs-durable by default (data_dir, below). Mesh transport
			// addresses peers by NodeID over the internode layer, so no
			// bind_addr/port.
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
			// store.kv.raft requires a durable raft; default the node data_dir to
			// ~/.wippy/store so raft is fs-durable out of the box (no toggle). The
			// shared raft (registry + kv) lives under <data_dir>/_sys/raft.
			if base := nodeDataDir(raftCfg); base != "" {
				rc.DataDir = filepath.Join(base, "_sys", "raft")
			} else {
				logger.Warn("raft: no data_dir and no home directory; running diskless")
			}

			// Capture handler config for the Start phase. Reading it here
			// keeps all config parsing in Load.
			handlerCfg = sysraft.HandlerConfig{
				MaxVoters:         raftCfg.GetInt(ClusterRaftMaxVoters, 5),
				MaxStandbys:       raftCfg.GetInt(ClusterRaftMaxStandbys, 4),
				ReconcileDebounce: raftCfg.GetDuration(ClusterRaftReconcileDebounce, 2*time.Second),
				ReconcileTimeout:  raftCfg.GetDuration(ClusterRaftReconcileTimeout, 2*time.Second),
			}
			globalDissemTombstoneRetention = raftCfg.GetDuration(ClusterRaftGlobalDissemTombstoneRetention, 0)

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
			fsm := global.NewFSM()

			// Create the Raft node, wired to the metrics collector and global
			// OTel providers so raft_*, gossip_*, pg_* series flow when those
			// subsystems do work.
			coll := metricsapi.GetCollector(ctx)
			mp := otel.GetMeterProvider()
			tp := otel.GetTracerProvider()

			// Single shared raft: the FSM slot is a multiplex router so the kv
			// state machine (store.kv.raft) rides the same node alongside the
			// global registry. Untagged commands go to the registry; kv-tagged
			// commands go to the kv FSM.
			kvFSM := systemkv.NewRaftFSM(bus)
			rootFSM := multiplex.New(wrapFSM(fsm), kvFSM)
			raftNode = sysraft.NewNode(node.ID(), rootFSM, rc, bus, logger.Named("node"), coll, mp, tp)
			raftNode.SetConnectionManager(connMgr)

			// Relay router: carries global-registry and kv leader-forwarding.
			router := relay.GetRouter(ctx)
			if router == nil {
				return ctx, fmt.Errorf("raft: relay router not available")
			}

			// kv engine forwards follower writes to the leader over the relay;
			// register it as a relay host so forwarded requests/responses land.
			kvEngine = systemkv.NewRaftEngine(raftNode, kvFSM, bus, node.ID(), router, logger.Named("kv"))
			if err := node.RegisterHost(systemkv.KVRaftHostID, kvEngine); err != nil {
				return ctx, fmt.Errorf("raft: register kv relay host: %w", err)
			}

			// Distributed locks over the shared kv (_sys:lock): linearizable
			// acquire/release, auto-release on holder process exit (topology
			// monitor -> the lock relay host) and on node leave (ReapNode, below).
			lockSvc = systemkv.NewLockService(kvEngine, topo, node.ID(), logger.Named("lock"))
			if err := node.RegisterHost(systemkv.LockHostID, lockSvc); err != nil {
				return ctx, fmt.Errorf("raft: register lock relay host: %w", err)
			}
			ctx = systemkv.WithLockService(ctx, lockSvc)
			globalRegSvc = global.NewService(
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
			if err := node.RegisterHost(global.HostID, globalRegSvc); err != nil {
				return ctx, fmt.Errorf("raft: register globalreg relay host: %w", err)
			}

			// Store components in context.
			ac := ctxapi.AppFromContext(ctx)
			if ac != nil {
				ac.With(raftNodeKey, raftNode)
				ac.With(globalRegFSMKey, fsm)
				ac.With(globalRegSvcKey, globalRegSvc)
				ac.With(kvEngineKey, kvEngine)
			}

			// Register the global registry in the topology context
			// so PIDRegistry can check it for conflicts.
			ctx = raftapi.WithService(ctx, raftNode)

			// Select the live name-registry backend. Default "kv" serves the two
			// dedicated registry FSM authoritative; "kv" serves the same two
			// facades from the shared kv keyspace (_sys:registry).
			useKVRegistry = strings.EqualFold(
				raftCfg.GetString(ClusterRaftRegistryBackend, registryBackendKV), registryBackendKV)

			var liveReg interface {
				topology.GlobalRegistry
				globalapi.Registry
			} = globalRegSvc

			if useKVRegistry {
				kvReg = kvbacked.NewService(kvEngine, node.ID(), nil, logger.Named("kvreg"))
				kvReg.SetTopology(topo)
				kvReg.ConfigureStrong(kvbacked.StrongDeps{
					Membership: func() []pid.NodeID {
						ms, ok := clusterapi.GetMembership(ctx).(*membership.Service)
						if !ok || ms == nil {
							return nil
						}
						var out []pid.NodeID
						for _, n := range ms.Nodes() {
							if n.ID != "" {
								out = append(out, n.ID)
							}
						}
						return out
					},
					IsLeader: raftNode.IsLeader,
					LocalConflict: func(name string, _ pid.PID) (pid.PID, bool) {
						lp := &localPresenceChecker{ctx: ctx}
						if cp, ok := lp.LookupLocal(name); ok {
							return cp, true
						}
						if cp, ok := lp.LookupEventual(name); ok {
							return cp, true
						}
						return pid.PID{}, false
					},
				})
				if err := node.RegisterHost(kvbacked.RegistryHostID, kvReg); err != nil {
					return ctx, fmt.Errorf("raft: register kv registry relay host: %w", err)
				}
				liveReg = kvReg
			}

			// Wire the global registry into the PID registry for
			// transparent lookup (global names take priority over local).
			pidReg := topology.GetRegistry(ctx)
			if pidReg != nil {
				if setter, ok := pidReg.(interface{ SetGlobalRegistry(topology.GlobalRegistry) }); ok {
					setter.SetGlobalRegistry(liveReg)
				}
			}

			// Register on both context keys:
			// - topology.GlobalRegistry for PIDRegistry integration (transparent lookup)
			// - global.Registry for direct Lua module access
			ctx = topology.WithGlobalRegistry(ctx, liveReg)
			ctx = globalapi.WithRegistry(ctx, liveReg)

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
					dissem := global.NewDissem(node.ID(), logger.Named("dissem"),
						global.WithTombstoneRetention(globalDissemTombstoneRetention))
					if err := memSvc.RegisterUserDelegate(dissem); err != nil {
						return ctx, fmt.Errorf("raft: register globalreg dissem delegate: %w", err)
					}
					if useKVRegistry {
						kvReg.ConfigureDissem(dissem)
						kvReg.SetLeaderFunc(raftNode.IsLeader)
					} else {
						globalRegSvc.SetDissem(dissem)
					}
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

			// Start the kv engine's leader-side lease sweeper.
			if kvEngine != nil {
				if err := kvEngine.Start(ctx); err != nil {
					return fmt.Errorf("start kv engine: %w", err)
				}
			}

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
						if lockSvc != nil {
							lockSvc.ReapNode(ne.Node.ID)
						}
						if kvReg != nil {
							kvReg.DropNode(ne.Node.ID)
						}
					})
				if err != nil {
					return fmt.Errorf("subscribe raft node-left: %w", err)
				}
				nodeLeftSub = sub
			}

			// Start the live name registry: the kv reconciler when the kv
			// backend is selected, otherwise the dedicated registry FSM service.
			if useKVRegistry {
				if kvReg != nil {
					if err := kvReg.StartReconciler(ctx); err != nil {
						return fmt.Errorf("start kv registry reconciler: %w", err)
					}
				}
			} else if globalRegSvc != nil {
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

			if kvEngine != nil {
				_ = kvEngine.Stop()
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
