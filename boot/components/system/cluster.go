// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"crypto/ed25519"
	"encoding/base64"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	metricsboot "github.com/wippyai/runtime/boot/components/metrics"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/health"
	"github.com/wippyai/runtime/system/relay"
	"go.opentelemetry.io/otel"
	"go.uber.org/zap"
)

// raft role values for cluster.raft.role. server (default) runs a raft
// Node; client is pure gossip + dissem routing with no raft Node.
const (
	raftRoleServer = "server"
	raftRoleClient = "client"
)

// clusterRaftEnabled reports whether this node runs a raft Node. It composes
// the role knob with the low-level enabled flag: raft runs only when
// cluster.raft.enabled is true AND cluster.raft.role is not "client". Either
// knob set to off yields a pure gossip+dissem client, so no combination of
// the two can contradict. clusterCfg is the cluster.* sub-config.
func clusterRaftEnabled(clusterCfg boot.Config) bool {
	if !clusterCfg.GetBool(ClusterRaftEnabled, true) {
		return false
	}
	return !strings.EqualFold(clusterCfg.GetString(ClusterRaftRole, raftRoleServer), raftRoleClient)
}

// internodeAdvertiseEndpoint returns an optional v2 endpoint for upgraded
// peers. It leaves v1 internode_port metadata unchanged, so old peers continue
// to use the membership IP and bound port during a rolling upgrade.
func internodeAdvertiseEndpoint(clusterCfg boot.Config, bindPort int) (string, int, error) {
	addr := strings.TrimSpace(clusterCfg.GetString(ClusterInternodeAdvertiseAddr, ""))
	configuredPort := clusterCfg.GetInt(ClusterInternodeAdvertisePort, 0)
	if addr == "" {
		if configuredPort != 0 {
			return "", 0, fmt.Errorf("cluster.internode.advertise_port requires advertise_addr")
		}
		return "", bindPort, nil
	}
	if !internode.ValidEndpointHost(addr) {
		return "", 0, fmt.Errorf("cluster.internode.advertise_addr must be an IP address or DNS hostname, got %q", addr)
	}
	if configuredPort == 0 {
		configuredPort = bindPort
	}
	if configuredPort < 1 || configuredPort > 65535 {
		return "", 0, fmt.Errorf("cluster.internode.advertise_port must be between 1 and 65535, got %d", configuredPort)
	}
	return addr, configuredPort, nil
}

// clusterHealthScoreCeiling is the maximum memberlist health score
// (where 0 = healthy) at which the activity-based liveness check still
// reports healthy. Memberlist scores 1 or 2 commonly during chaos
// before stabilizing; scores beyond this indicate sustained probe
// failure consistent with partition isolation.
const clusterHealthScoreCeiling = 4

// clusterGossipBootGrace is the window after Start during which the
// gossip health check returns healthy unconditionally — gives a
// freshly-launched pod time to join the cluster before kubelet's
// liveness probe (3 failures × periodSeconds=5) can decide to SIGTERM
// it. Without this, every chaos-killed pod is observed to exit 0
// during rejoin and StatefulSet loops it into CrashLoopBackOff.
const clusterGossipBootGrace = 60 * time.Second

// Context keys for cluster components
var (
	internodeServiceKey = &ctxapi.Key{Name: "cluster.internode"}
	connMgrKey          = &ctxapi.Key{Name: "cluster.internode.conn"}
)

// WithInternodeService attaches InternodeService to context
func WithInternodeService(ctx context.Context, svc *internode.Service) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(internodeServiceKey) == nil {
		ac.With(internodeServiceKey, svc)
	}
	return ctx
}

func Cluster() boot.Component {
	var membershipSvc *membership.Service
	var internodeSvc *internode.Service
	var connMgr internode.ConnectionManager
	var logger *zap.Logger
	var advertiseConfig boot.Config
	var internodeActive, membershipActive bool
	var lifecycle sync.Mutex
	type execution struct {
		cancelWatch func() bool
		done        chan struct{}
	}
	var current *execution
	// The lifecycle lock also serializes context cancellation with boot calls.
	// Clear ownership before cleanup so a failed Start followed by shutdown
	// cannot stop a service twice.
	stopServices := func() {
		if internodeActive {
			internodeActive = false
			if err := internodeSvc.Stop(); err != nil {
				logger.Error("failed to stop internode service", zap.Error(err))
			}
		}
		if membershipActive {
			membershipActive = false
			if err := membershipSvc.Stop(); err != nil {
				logger.Error("failed to stop membership service", zap.Error(err))
			}
		}
	}

	return boot.New(boot.P{
		Name:      ClusterName,
		DependsOn: []boot.Name{metricsboot.Name},
		Load: func(ctx context.Context) (context.Context, error) {
			lifecycle.Lock()
			defer lifecycle.Unlock()
			if current != nil || internodeActive || membershipActive {
				return ctx, fmt.Errorf("cluster component already started")
			}
			logger = logapi.GetLogger(ctx).Named("cluster")
			cfg := boot.GetConfig(ctx)

			if cfg == nil {
				return ctx, nil
			}

			clusterCfg := cfg.Sub(ClusterName)
			if !clusterCfg.GetBool(ClusterEnabled, false) {
				logger.Debug("cluster disabled")
				return ctx, nil
			}

			// Get node name
			nodeName := clusterCfg.GetString(ClusterNodeName, "")
			if nodeName == "" {
				hostname, err := os.Hostname()
				if err != nil {
					return ctx, NewHostnameError(err)
				}
				nodeName = hostname
			}

			// Get dependencies from context
			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailableForCluster
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailableForCluster
			}

			node := relayapi.GetNode(ctx)
			if node == nil {
				return ctx, ErrRelayNotAvailableForCluster
			}

			// Parse join addresses
			var joinAddrs []string
			joinStr := clusterCfg.GetString(ClusterMembershipJoin, "")
			if joinStr != "" {
				for _, addr := range strings.Split(joinStr, ",") {
					joinAddrs = append(joinAddrs, strings.TrimSpace(addr))
				}
			}

			secretKey, err := membership.ResolveSecretKey(
				clusterCfg.GetString(ClusterMembershipSecret, ""),
				clusterCfg.GetString(ClusterMembershipSecretFile, ""),
			)
			if err != nil {
				return ctx, membership.NewLoadSecretKeyError(err)
			}
			signingKey, err := internode.ResolveIdentityKey(
				clusterCfg.GetString(ClusterInternodeIdentityKey, ""),
				clusterCfg.GetString(ClusterInternodeIdentityKeyFile, ""),
			)
			if err != nil {
				return ctx, err
			}
			publicKey := signingKey.Public().(ed25519.PublicKey)
			trustedPeerKeys := make(map[clusterapi.NodeID]ed25519.PublicKey)
			trustedKeysCfg := clusterCfg.Sub(ClusterInternodeTrustedPeerKeys)
			for _, id := range trustedKeysCfg.Keys() {
				trustedKey, err := internode.ParseIdentityPublicKey(trustedKeysCfg.GetString(id, ""))
				if err != nil {
					return ctx, fmt.Errorf("invalid trusted internode key for %q: %w", id, err)
				}
				trustedPeerKeys[id] = trustedKey
			}
			trustedLocalKey, ok := trustedPeerKeys[nodeName]
			if !ok || !publicKey.Equal(trustedLocalKey) {
				return ctx, fmt.Errorf("trusted internode key for local node %q is required and must match its identity", nodeName)
			}

			var peerKeySource clusterapi.PeerKeySource
			if rawSource, present := clusterCfg.Get(ClusterInternodePeerKeySource); present {
				var valid bool
				peerKeySource, valid = rawSource.(clusterapi.PeerKeySource)
				if !valid || peerKeySource == nil {
					return ctx, fmt.Errorf("cluster.internode.peer_key_source requires a native PeerKeySource")
				}
			}

			// Create message codec
			messageCodec := internode.NewMessageCodec(dtt)

			// Create connection manager config
			connManagerCfg := internode.DefaultManagerConfig()
			connManagerCfg.TLS, err = clusterTLSConfig(clusterCfg)
			if err != nil {
				return ctx, err
			}
			connManagerCfg.LocalNodeID = nodeName
			connManagerCfg.BindAddr = clusterCfg.GetString(ClusterInternodeBindAddr, "0.0.0.0")
			connManagerCfg.BindPort = clusterCfg.GetInt(ClusterInternodeBindPort, 0)
			connManagerCfg.AutoPort = clusterCfg.GetBool(ClusterInternodeAutoPort, true)
			connManagerCfg.Logger = logger.Named("internode.conn")
			connManagerCfg.AuthenticationKey = secretKey
			connManagerCfg.SigningKey = signingKey
			connManagerCfg.RequireAuthentication = true
			connManagerCfg.ResolvePeerKey = func(id clusterapi.NodeID) (ed25519.PublicKey, bool) {
				return internode.ResolveMemberKey(nodeName, id, trustedPeerKeys, peerKeySource, membershipSvc)
			}
			connManagerCfg.AuthorizePeer = func(id clusterapi.NodeID, _ net.Addr) bool {
				_, ok := connManagerCfg.ResolvePeerKey(id)
				return ok
			}

			connMgr = internode.NewConnectionManager(connManagerCfg, metricsapi.GetCollector(ctx))
			advertiseConfig = clusterCfg
			// Validate overrides now; resolve an automatic port from the live
			// listener during Start, before membership can advertise it.
			_, _, err = internodeAdvertiseEndpoint(clusterCfg, 1)
			if err != nil {
				return ctx, err
			}

			// Create node metadata with the externally reachable internode endpoint
			// and raft-eligibility hints. raft_eligible / raft_priority / failure_domain are advertised so the
			// Raft membership reconciler can pick voters without a separate channel.
			//
			// raft_eligible is forced to false on any node that won't run a
			// raft Node (role=client or enabled=false): such a node cannot
			// accept AddVoter, so the leader's reconciler must never pick it.
			// Without this a gossip-only client still advertises
			// raft_eligible=true (the default), the reconciler tries to add
			// it as a voter, AddVoter targets a pod with no raft, and the
			// leader thrashes on the failing operation.
			raftEligible := clusterRaftEnabled(clusterCfg) && clusterCfg.GetBool(ClusterRaftEligible, true)
			nodeMeta := clusterapi.NodeMeta{
				"version":                         "1.0.0",
				internode.MetadataSurfaceProtocol: "1",
				internode.MetadataSurfaceGraphics: "1",
				"role":                            "wippy",
				internode.MetadataPublicKey:       base64.RawStdEncoding.EncodeToString(publicKey),
				"raft_eligible":                   strconv.FormatBool(raftEligible),
				"raft_priority":                   strconv.Itoa(clusterCfg.GetInt(ClusterRaftPriority, 100)),
				"failure_domain":                  clusterCfg.GetString(ClusterFailureDomain, ""),
			}

			// Create membership service config
			memberCfg := membership.Config{
				NodeName:    nodeName,
				BindAddr:    clusterCfg.GetString(ClusterMembershipBindAddr, "0.0.0.0"),
				BindPort:    clusterCfg.GetInt(ClusterMembershipBindPort, 7946),
				JoinAddrs:   joinAddrs,
				SecretKey:   secretKey,
				AdvertiseIP: clusterCfg.GetString(ClusterMembershipAdvertise, ""),
				GossipInterval: clusterCfg.GetDuration(
					ClusterMembershipGossipInterval,
					membership.DefaultGossipInterval,
				),
				PushPullInterval: clusterCfg.GetDuration(
					ClusterMembershipPushPullInterval,
					membership.DefaultPushPullInterval,
				),
				DeadNodeReclaimTime: clusterCfg.GetDuration(
					ClusterMembershipDeadNodeReclaimTime,
					membership.DefaultDeadNodeReclaimTime,
				),
				ProbeInterval: clusterCfg.GetDuration(
					ClusterMembershipProbeInterval,
					0,
				),
				ProbeTimeout: clusterCfg.GetDuration(
					ClusterMembershipProbeTimeout,
					0,
				),
				TCPTimeout: clusterCfg.GetDuration(
					ClusterMembershipTCPTimeout,
					0,
				),
				SuspicionMult: clusterCfg.GetInt(
					ClusterMembershipSuspicionMult,
					0,
				),
				VeryVerbose: false,
				Meta:        nodeMeta,
			}

			membershipSvc = membership.NewService(
				memberCfg, bus, logger.Named("membership"),
				metricsapi.GetCollector(ctx),
				otel.GetMeterProvider(),
				otel.GetTracerProvider(),
			)

			// Create package callback for internode service
			pkgCallback := func(pkg *relayapi.Package) error {
				// Copy fields before Send — Send may release the package.
				targetHost := pkg.Target.Host
				targetNode := pkg.Target.Node
				topic := ""
				if len(pkg.Messages) > 0 {
					topic = pkg.Messages[0].Topic
				}
				err := node.Send(pkg)
				if err != nil {
					// Hot path under partition: targets in-flight when peer
					// torn down. The Service-side onMessage already counts
					// this as internode_dropped_total{reason="delivery_failed"};
					// keep this at DEBUG to retain the rich context but stay
					// quiet during chaos.
					logger.Debug("internode delivery failed",
						zap.String("target_host", targetHost),
						zap.String("target_node", targetNode),
						zap.String("topic", topic),
						zap.Error(err),
					)
				}
				return err
			}

			// Create internode service
			internodeSvc = internode.NewService(
				logger.Named("internode"),
				connMgr,
				messageCodec,
				pkgCallback,
				bus,
				membershipSvc,
			)

			// Enable internode routing on the existing router.
			// The router was created during bootstrap with nil internode;
			// now we set the internode service so cross-node messages are
			// forwarded correctly.
			router := relayapi.GetRouter(ctx)
			if router == nil {
				return ctx, ErrRouterNotAvailable
			}
			if sysRouter, ok := router.(*relay.Router); ok {
				sysRouter.SetInternode(internodeSvc)
			} else {
				return ctx, ErrRouterNotAvailable
			}

			// Store cluster components in context
			ctx = clusterapi.WithMembership(ctx, membershipSvc)
			ctx = WithInternodeService(ctx, internodeSvc)

			// Expose the connection manager so the mesh-backed Raft
			// transport (system/raft) can ride on the same internode
			// connection as gossip, relay, and PG broadcast traffic.
			// No separate Raft listener is bound.
			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				ac.With(connMgrKey, connMgr)
			}

			logger.Info("cluster initialized",
				zap.String("node_name", nodeName),
				zap.Int("membership_port", memberCfg.BindPort),
				zap.Strings("join_addrs", joinAddrs),
			)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			lifecycle.Lock()
			defer lifecycle.Unlock()
			if err := ctx.Err(); err != nil {
				return err
			}
			if current != nil || internodeActive || membershipActive {
				return fmt.Errorf("cluster component already started")
			}
			if internodeSvc != nil {
				if err := internodeSvc.Start(ctx); err != nil {
					return NewInternodeStartError(err)
				}
				internodeActive = true
				actualPort := connMgr.GetListenPort()
				addr, port, err := internodeAdvertiseEndpoint(advertiseConfig, actualPort)
				if err != nil {
					stopServices()
					return err
				}
				meta := map[string]string{internode.MetadataPort: strconv.Itoa(actualPort)}
				if addr != "" {
					meta[internode.MetadataAdvertiseAddr] = addr
					meta[internode.MetadataAdvertisePort] = strconv.Itoa(port)
				}
				membershipSvc.UpdateMeta(meta)
			}
			if membershipSvc != nil {
				logger.Info("starting cluster membership service")
				// Membership can acquire sockets before reporting a join error.
				membershipActive = true
				if err := membershipSvc.Start(ctx); err != nil {
					stopServices()
					return NewMembershipStartError(err)
				}
			}

			// Liveness check: memberlist HealthScore reports zero when
			// gossip probes are clean. Anything non-zero means probes
			// are failing — typically because we're partitioned. We
			// tolerate transient suspects (score 1 or 2) but report
			// unhealthy past the ceiling.
			//
			// Boot grace window: non-bootstrap pods have a legitimate
			// startup interval before they finish joining the cluster,
			// during which HealthScore() reads above the ceiling — not
			// because the pod is partitioned, but because gossip
			// hasn't converged yet. Without a grace window, kubelet
			// sends SIGTERM after 3× failureThreshold (~15s), the
			// runtime shuts down cleanly (exit 0), and the StatefulSet
			// retries — produces the CrashLoopBackOff pattern observed
			// every chaos cycle.
			startedAt := time.Now()
			if membershipSvc != nil {
				health.Register("cluster.gossip", func() error {
					if time.Since(startedAt) < clusterGossipBootGrace {
						return nil
					}
					score := membershipSvc.HealthScore()
					switch {
					case score < 0:
						return fmt.Errorf("memberlist not running")
					case score > clusterHealthScoreCeiling:
						return fmt.Errorf("memberlist health score %d exceeds ceiling %d",
							score, clusterHealthScoreCeiling)
					}
					return nil
				})
			}

			// Network admission ends with this execution, independently of reverse
			// loader shutdown order (a supervisor may still be draining actors).
			run := &execution{done: make(chan struct{})}
			current = run
			run.cancelWatch = context.AfterFunc(ctx, func() {
				defer close(run.done)
				lifecycle.Lock()
				defer lifecycle.Unlock()
				if current == run {
					stopServices()
				}
			})
			return nil
		},
		Stop: func(_ context.Context) error {
			lifecycle.Lock()
			run := current
			join := run != nil && !run.cancelWatch()
			stopServices()
			current = nil
			lifecycle.Unlock()
			// An already scheduled callback may be waiting for the lock. Join it
			// without holding the lock; its identity cannot stop a later Start.
			if join {
				<-run.done
			}
			return nil
		},
	})
}
