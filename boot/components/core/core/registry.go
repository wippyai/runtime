package core

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	regapi "github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/system/registry"
	"github.com/ponyruntime/pony/system/registry/history"
	"github.com/ponyruntime/pony/system/registry/runner"
	regtop "github.com/ponyruntime/pony/system/registry/topology"
)

func Registry() boot.Component {
	return boot.New(boot.P{
		Name:      RegistryName,
		Phase:     boot.PreInit,
		DependsOn: []string{LoggerName, EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			reg := registry.NewRegistry(
				history.NewMemory(),
				runner.NewBusRunner(bus, logger.Named("runner")),
				regtop.NewStateBuilder(logger),
				logger.Named("registry"),
			)

			return regapi.WithRegistry(ctx, reg), nil
		},
	})
}
