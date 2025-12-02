package store

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	storesystem "github.com/wippyai/runtime/system/store"
)

// DefaultWorkers is the default number of workers for the store dispatcher.
const DefaultWorkers = 4

// Dispatcher creates the store dispatcher boot component.
func Dispatcher(workers int) boot.Component {
	if workers <= 0 {
		workers = DefaultWorkers
	}

	var d *storesystem.Dispatcher

	return boot.New(boot.P{
		Name:      DispatcherName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}

			d = storesystem.NewDispatcher(workers)
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
