// SPDX-License-Identifier: MPL-2.0

package dispatchers

import (
	"context"
	"time"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	netapi "github.com/wippyai/runtime/api/net"
	httpclient "github.com/wippyai/runtime/service/http/client"
)

// HTTPConfig configures the HTTP client dispatcher.
type HTTPConfig struct {
	Timeout         time.Duration
	MaxIdleConns    int
	MaxIdlePerHost  int
	IdleConnTimeout time.Duration
	// MaxClients caps the number of distinct pooled clients. Zero means
	// unbounded. Non-zero is required to bound long-running processes that
	// accumulate many distinct TLS / unix-socket / overlay keys over time.
	MaxClients int
}

// HTTP client dispatcher config keys (read from boot YAML under
// "dispatcher.http"). Programmatic HTTPConfig values take priority when non-zero.
const (
	HTTPTimeout         = "timeout"
	HTTPMaxIdleConns    = "max_idle_conns"
	HTTPMaxIdlePerHost  = "max_idle_per_host"
	HTTPIdleConnTimeout = "idle_conn_timeout"
	HTTPMaxClients      = "max_clients"
)

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

			if bootCfg := boot.GetConfig(ctx); bootCfg != nil {
				if httpCfg := bootCfg.Sub(HTTPDispatcherName); httpCfg != nil {
					if config.Timeout <= 0 {
						config.Timeout = httpCfg.GetDuration(HTTPTimeout, 0)
					}
					if config.MaxIdleConns <= 0 {
						config.MaxIdleConns = httpCfg.GetInt(HTTPMaxIdleConns, 0)
					}
					if config.MaxIdlePerHost <= 0 {
						config.MaxIdlePerHost = httpCfg.GetInt(HTTPMaxIdlePerHost, 0)
					}
					if config.IdleConnTimeout <= 0 {
						config.IdleConnTimeout = httpCfg.GetDuration(HTTPIdleConnTimeout, 0)
					}
					if config.MaxClients <= 0 {
						config.MaxClients = httpCfg.GetInt(HTTPMaxClients, 0)
					}
				}
			}

			var opts []httpclient.Option
			if config.Timeout > 0 || config.MaxIdleConns > 0 || config.MaxClients > 0 {
				opts = append(opts, httpclient.WithPoolConfig(httpclient.PoolConfig{
					Timeout:         config.Timeout,
					MaxIdleConns:    config.MaxIdleConns,
					MaxIdlePerHost:  config.MaxIdlePerHost,
					IdleConnTimeout: config.IdleConnTimeout,
					MaxClients:      config.MaxClients,
				}))
			}

			// Inject network registry for overlay network resolution (graceful: nil is fine)
			if netReg := netapi.GetNetworkRegistry(ctx); netReg != nil {
				opts = append(opts, httpclient.WithNetworkRegistry(netReg))
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
