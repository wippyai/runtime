package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	pubsubapi "github.com/ponyruntime/pony/api/pubsub"
	"github.com/ponyruntime/pony/system/process"
)

func Process() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessName,
		Phase:     boot.Init,
		DependsOn: []string{"eventbus", "logger"},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			node := pubsubapi.GetNode(ctx)

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
