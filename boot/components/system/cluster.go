// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"os"
	"strconv"
	"strings"

	"github.com/wippyai/runtime/api/boot"
	clusterapi "github.com/wippyai/runtime/api/cluster"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	relayapi "github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/cluster/internode"
	"github.com/wippyai/runtime/cluster/membership"
	"github.com/wippyai/runtime/system/relay"
	"go.uber.org/zap"
)

// Context keys for cluster components
var (
	internodeServiceKey  = &ctxapi.Key{Name: "cluster.internode"}
	membershipServiceKey = &ctxapi.Key{Name: "cluster.membership"}
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

// WithMembership attaches Membership service to context todo move to api
func WithMembership(ctx context.Context, m clusterapi.Membership) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(membershipServiceKey) == nil {
		ac.With(membershipServiceKey, m)
	}
	return ctx
}

func Cluster() boot.Component {
	var membershipSvc *membership.Service
	var internodeSvc *internode.Service
	var connMgr internode.ConnectionManager
	var logger *zap.Logger

	return boot.New(boot.P{
		Name: ClusterName,
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
			tempConnMgr := internode.NewConnectionManager(connManagerCfg)
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
			connMgr = internode.NewConnectionManager(connManagerCfg)

			// Create node metadata with internode port
			nodeMeta := clusterapi.NodeMeta{
				"version":        "1.0.0",
				"role":           "wippy",
				"internode_port": strconv.Itoa(actualPort),
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

			membershipSvc = membership.NewService(memberCfg, bus, logger.Named("membership"))

			// Create package callback for internode service
			pkgCallback := func(pkg *relayapi.Package) error {
				err := node.Send(pkg)
				if err != nil {
					topic := ""
					if len(pkg.Messages) > 0 {
						topic = pkg.Messages[0].Topic
					}
					logger.Warn("internode delivery failed",
						zap.String("target_host", pkg.Target.Host),
						zap.String("target_node", pkg.Target.Node),
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
			ctx = WithMembership(ctx, membershipSvc)
			ctx = WithInternodeService(ctx, internodeSvc)

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
