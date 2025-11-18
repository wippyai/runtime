package system

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	resapi "github.com/wippyai/runtime/api/resource"
	"github.com/wippyai/runtime/system/resource"
)

func Resources() boot.Component {
	var resources *resource.Registry

	return boot.New(boot.P{
		Name:      ResourcesName,
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
		Stop: func(_ context.Context) error {
			if resources != nil {
				return resources.Stop()
			}
			return nil
		},
	})
}
