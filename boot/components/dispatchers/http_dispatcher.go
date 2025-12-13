package dispatchers

import (
	"context"
	"io"
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
	BlockPrivateIPs bool      // Enable SSRF protection (default false for backward compatibility)
	Debug           io.Writer // TODO: remove after testing is complete
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

			// Read from yaml config if no explicit config provided
			if len(cfg) == 0 {
				if bootCfg := boot.GetConfig(ctx); bootCfg != nil {
					httpCfg := bootCfg.Sub("http_client")
					config.BlockPrivateIPs = httpCfg.GetBool("block_private_ips", false)
				}
			}

			var opts []httpclient.Option
			if config.Debug != nil {
				opts = append(opts, httpclient.WithDebug(config.Debug))
			}
			if config.Timeout > 0 || config.MaxIdleConns > 0 || config.BlockPrivateIPs {
				opts = append(opts, httpclient.WithPoolConfig(httpclient.PoolConfig{
					Timeout:         config.Timeout,
					MaxIdleConns:    config.MaxIdleConns,
					MaxIdlePerHost:  config.MaxIdlePerHost,
					IdleConnTimeout: config.IdleConnTimeout,
					BlockPrivateIPs: config.BlockPrivateIPs,
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
