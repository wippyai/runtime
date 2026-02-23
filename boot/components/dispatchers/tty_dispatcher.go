// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/service/terminal/tty"
)

// TTYConfig configures the terminal I/O dispatcher.
type TTYConfig struct {
	Workers int
}

// TTY creates the terminal I/O dispatcher component.
func TTY(cfg ...TTYConfig) boot.Component {
	var svc *tty.Dispatcher
	var config TTYConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}

	return boot.New(boot.P{
		Name:      TTYDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			var opts []tty.Option
			if config.Workers > 0 {
				opts = append(opts, tty.WithWorkers(config.Workers))
			}

			svc = tty.NewDispatcher(opts...)
			svc.RegisterAll(reg.Register)
			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if svc != nil {
				return svc.Start(ctx)
			}
			return nil
		},
		Stop: func(ctx context.Context) error {
			if svc != nil {
				return svc.Stop(ctx)
			}
			return nil
		},
	})
}
