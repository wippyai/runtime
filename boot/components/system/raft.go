// SPDX-License-Identifier: MPL-2.0

package system

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/event"
	globalregapi "github.com/wippyai/runtime/api/globalreg"
	logapi "github.com/wippyai/runtime/api/logs"
	raftapi "github.com/wippyai/runtime/api/raft"
	"github.com/wippyai/runtime/api/relay"
	"github.com/wippyai/runtime/api/topology"
	"github.com/wippyai/runtime/system/globalreg"
	sysraft "github.com/wippyai/runtime/system/raft"
	"go.uber.org/zap"
)

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
			rc := raftapi.Config{
				DataDir:       raftCfg.GetString(RaftDataDir, ""),
				BindAddr:      raftCfg.GetString(RaftBindAddr, "0.0.0.0"),
				BindPort:      raftCfg.GetInt(RaftBindPort, 7960),
				AutoPort:      raftCfg.GetBool(RaftAutoPort, true),
				AdvertiseAddr: raftCfg.GetString(RaftAdvertiseAddr, ""),
				Bootstrap:     raftCfg.GetBool(RaftBootstrap, false),
			}

			// Create the global registry FSM (the state machine Raft replicates).
			fsm := globalreg.NewFSM()

			// Create the Raft node.
			raftNode = sysraft.NewNode(node.ID(), fsm, rc, bus, logger.Named("node"))

			// Create the global registry service wrapping Raft + FSM.
			router := relay.GetRouter(ctx)
			if router == nil {
				return ctx, fmt.Errorf("raft: relay router not available")
			}
			globalRegSvc = globalreg.NewService(
				raftNode, fsm, bus, topo,
				router,
				node.ID(),
				logger.Named("globalreg"),
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

			logger.Info("raft initialized",
				zap.String("bind_addr", rc.BindAddr),
				zap.Int("bind_port", rc.BindPort),
				zap.Bool("auto_port", rc.AutoPort),
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

			// Start membership handler to sync Raft voters with cluster membership.
			bus := event.GetBus(ctx)
			if bus != nil {
				memberHandler = sysraft.NewMembershipHandler(raftNode, bus, logger.Named("membership"))
				if err := memberHandler.Start(ctx); err != nil {
					return fmt.Errorf("start raft membership handler: %w", err)
				}
			}

			// Start the global registry service.
			if globalRegSvc != nil {
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
