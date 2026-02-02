package dispatchers

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	httpclient "github.com/wippyai/runtime/service/http/client"
)

// HTTPConfig configures the HTTP client dispatcher.
type HTTPConfig struct {
	Timeout         time.Duration
	MaxIdleConns    int
	MaxIdlePerHost  int
	IdleConnTimeout time.Duration
}

// HTTP creates the HTTP client dispatcher component.
func HTTP(cfg ...HTTPConfig) boot.Component {
	var d *httpclient.Dispatcher
	var config HTTPConfig
	if len(cfg) > 0 {
		config = cfg[0]
	}

	return boot.New(boot.P{
		Name:      HTTPDispatcherName,
		DependsOn: []boot.Name{DispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherNotFound
			}

			var opts []httpclient.Option
			if config.Timeout > 0 || config.MaxIdleConns > 0 {
				opts = append(opts, httpclient.WithPoolConfig(httpclient.PoolConfig{
					Timeout:         config.Timeout,
					MaxIdleConns:    config.MaxIdleConns,
					MaxIdlePerHost:  config.MaxIdlePerHost,
					IdleConnTimeout: config.IdleConnTimeout,
				}))
			}

			d = httpclient.NewDispatcher(opts...)
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
