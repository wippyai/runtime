package system

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	resapi "github.com/ponyruntime/pony/api/resource"
	"github.com/ponyruntime/pony/system/resource"
)

func Resources() boot.Component {
	var resources *resource.Registry

	return boot.New(boot.P{
		Name:      ResourcesName,
		Phase:     boot.Init,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)

			resources = resource.NewResourceRegistry(bus, logger.Named("resources"))
			return resapi.WithRegistry(ctx, resources), nil
		},
		Start: func(ctx context.Context) error {
			if resources != nil {
				return resources.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if resources != nil {
				return resources.Stop()
			}
			return nil
		},
	})
}
