package system

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	regapi "github.com/ponyruntime/pony/api/registry"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	"github.com/ponyruntime/pony/system/process"
	"go.uber.org/zap"
)

func Process() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			node := pubsubapi.GetNode(ctx)

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Register process dependency patterns
			processPatterns := []regapi.DependencyPattern{
				{Path: "data.host", Description: "Reference to a host component"},
				{Path: "data.process", Description: "Reference to a process component"},
			}
			for _, pattern := range processPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register process dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			prototypes := process.NewPrototypeFactory(bus, logger.Named("prototypes"))
			hosts := process.NewHostRegistry(bus, logger.Named("hosts"))

			// Node may not be available yet - it's initialized in app.go after plugins load
			var nodeID string
			if node != nil {
				nodeID = node.ID()
			} else {
				nodeID = "local"
			}

			processes := process.NewProcessManager(
				hosts,
				prototypes,
				nodeID,
				logger.Named("processes"),
			)

			ctx = procapi.WithManager(ctx, processes)
			ctx = procapi.WithPrototypes(ctx, prototypes)
			ctx = procapi.WithHosts(ctx, hosts)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			prototypes := procapi.GetPrototypes(ctx)
			if err := prototypes.Start(ctx); err != nil {
				return err
			}

			hosts := procapi.GetHosts(ctx)
			return hosts.Start(ctx)
		},
		Stop: func(ctx context.Context) error {
			hosts := procapi.GetHosts(ctx)
			if err := hosts.Stop(); err != nil {
				return err
			}

			prototypes := procapi.GetPrototypes(ctx)
			return prototypes.Stop()
		},
	})
}
