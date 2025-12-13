package dispatchers

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/system/stream"
)

// StreamConfig configures the stream dispatcher.
type StreamConfig struct {
	Workers int
}

// Stream creates the stream dispatcher component.
func Stream(cfg ...StreamConfig) boot.Component {
	var svc *stream.Dispatcher
	var config StreamConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}

	return boot.New(boot.P{
		Name:      StreamDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			var opts []stream.Option
			if config.Workers > 0 {
				opts = append(opts, stream.WithWorkers(config.Workers))
			}

			svc = stream.NewDispatcher(opts...)
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
