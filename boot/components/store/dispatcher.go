package store

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	storesystem "github.com/wippyai/runtime/system/store"
)

// DispatcherConfig holds store dispatcher configuration.
type DispatcherConfig struct {
	// Workers is the number of worker goroutines for async mode.
	// If 0, dispatcher runs in blocking mode (synchronous execution).
	// Default: 0 (blocking mode for testing).
	Workers int
}

// Dispatcher creates the store dispatcher boot component with default config.
// Uses blocking mode by default.
func Dispatcher() boot.Component {
	return DispatcherWithConfig(DispatcherConfig{Workers: 0})
}

// AsyncDispatcher creates the store dispatcher boot component with async mode.
// Use for production to avoid blocking the scheduler.
func AsyncDispatcher(workers int) boot.Component {
	return DispatcherWithConfig(DispatcherConfig{Workers: workers})
}

// DispatcherWithConfig creates the store dispatcher boot component with custom config.
func DispatcherWithConfig(cfg DispatcherConfig) boot.Component {
	var d *storesystem.Dispatcher

	return boot.New(boot.P{
		Name:      DispatcherName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("dispatcher registrar not found in context")
			}

			d = storesystem.NewDispatcher(storesystem.Config{
				Workers: cfg.Workers,
			})
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
