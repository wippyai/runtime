package store

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	storeservice "github.com/wippyai/runtime/service/store"
)

// DefaultWorkers is the default number of workers for the store dispatcher.
const DefaultWorkers = 4

// Dispatcher creates the store dispatcher boot component.
func Dispatcher(workers int) boot.Component {
	if workers <= 0 {
		workers = DefaultWorkers
	}

	var d *storeservice.Dispatcher

	return boot.New(boot.P{
		Name:      DispatcherName,
		DependsOn: []boot.Name{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			d = storeservice.NewDispatcher(workers)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			return d.Start(ctx)
		},
		Stop: func(ctx context.Context) error {
			return d.Stop(ctx)
		},
	})
}
