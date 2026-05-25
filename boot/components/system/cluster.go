// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"strings"
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

	return boot.New(boot.P{
		Name:      ClusterName,
		DependsOn: []boot.Name{metricsboot.Name},
		Load: func(ctx context.Context) (context.Context, error) {
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

			// Create message codec
			messageCodec := internode.NewMessageCodec(dtt)

			// Create connection manager config
			connManagerCfg := internode.DefaultManagerConfig()
			connManagerCfg.LocalNodeID = nodeName
			connManagerCfg.BindAddr = clusterCfg.GetString(ClusterInternodeBindAddr, "0.0.0.0")
			connManagerCfg.BindPort = clusterCfg.GetInt(ClusterInternodeBindPort, 0)
			connManagerCfg.AutoPort = clusterCfg.GetBool(ClusterInternodeAutoPort, true)
			connManagerCfg.Logger = logger.Named("internode.conn")

			// Pre-start a temporary connection manager to allocate a port.
			// This discovers the actual port (especially with AutoPort),
			// which is needed for metadata before the real start.
			tempConnMgr := internode.NewConnectionManager(connManagerCfg, metricsapi.GetCollector(ctx))
			tempCtx, tempCancel := context.WithCancel(context.Background())
			dummyCallback := func(_ clusterapi.NodeID, _ []byte) {}

			if err := tempConnMgr.Start(tempCtx, dummyCallback); err != nil {
				tempCancel()
				return ctx, NewConnectionManagerPreStartError(err)
			}

			actualPort := tempConnMgr.GetListenPort()

			if err := tempConnMgr.Stop(); err != nil {
				tempCancel()
				return ctx, NewConnectionManagerStopError(err)
			}
			tempCancel()

			// Pin the discovered port so the real connection manager
			// binds exactly the same port on restart.
			connManagerCfg.BindPort = actualPort
			connManagerCfg.AutoPort = false
			connMgr = internode.NewConnectionManager(connManagerCfg, metricsapi.GetCollector(ctx))

			// Create node metadata with internode port and raft-eligibility hints.
			// raft_eligible / raft_priority / failure_domain are advertised so the
			// Raft membership reconciler can pick voters without a separate channel.
			nodeMeta := clusterapi.NodeMeta{
				"version":        "1.0.0",
				"role":           "wippy",
				"internode_port": strconv.Itoa(actualPort),
				"raft_eligible":  strconv.FormatBool(clusterCfg.GetBool(ClusterRaftEligible, true)),
				"raft_priority":  strconv.Itoa(clusterCfg.GetInt(ClusterRaftPriority, 100)),
				"failure_domain": clusterCfg.GetString(ClusterFailureDomain, ""),
			}

			// Create membership service config
			memberCfg := membership.Config{
				NodeName:     nodeName,
				BindAddr:     clusterCfg.GetString(ClusterMembershipBindAddr, "0.0.0.0"),
				BindPort:     clusterCfg.GetInt(ClusterMembershipBindPort, 7946),
				JoinAddrs:    joinAddrs,
				SecretFile:   clusterCfg.GetString(ClusterMembershipSecretFile, ""),
				SecretString: clusterCfg.GetString(ClusterMembershipSecret, ""),
				AdvertiseIP:  clusterCfg.GetString(ClusterMembershipAdvertise, ""),
				VeryVerbose:  false,
				Meta:         nodeMeta,
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
			// transport (system/raft) and the kvraft transport can ride
			// on the same internode connection as gossip, relay, and PG
			// broadcast traffic. No separate Raft listener is bound.
			if ac := ctxapi.AppFromContext(ctx); ac != nil {
				ac.With(connMgrKey, connMgr)
			}

			logger.Info("cluster initialized",
				zap.String("node_name", nodeName),
				zap.Int("internode_port", actualPort),
				zap.Int("membership_port", memberCfg.BindPort),
				zap.Strings("join_addrs", joinAddrs),
			)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if membershipSvc != nil {
				logger.Info("starting cluster membership service")
				if err := membershipSvc.Start(ctx); err != nil {
					return NewMembershipStartError(err)
				}
			}

			if internodeSvc != nil {
				logger.Info("starting cluster internode service")
				if err := internodeSvc.Start(ctx); err != nil {
					return NewInternodeStartError(err)
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

			return nil
		},
		Stop: func(_ context.Context) error {
			if internodeSvc != nil {
				logger.Info("stopping cluster internode service")
				if err := internodeSvc.Stop(); err != nil {
					logger.Error("failed to stop internode service", zap.Error(err))
				}
			}

			if membershipSvc != nil {
				logger.Info("stopping cluster membership service")
				if err := membershipSvc.Stop(); err != nil {
					logger.Error("failed to stop membership service", zap.Error(err))
				}
			}

			return nil
		},
	})
}
