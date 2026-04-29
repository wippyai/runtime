// SPDX-License-Identifier: MPL-2.0

package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
	impl "github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/system/eventbus"
	syslogs "github.com/wippyai/runtime/system/logs"
)

func Metrics() boot.Component {
	var collector api.Collector

	return boot.New(boot.P{
		Name:      Name,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			cfg := loadConfig(ctx)
			collector = impl.NewCollector(cfg)
			ctx = api.WithCollector(ctx, collector)
			// Bind the logger's per-emission hook to this collector so
			// runtime_log_emissions_total starts incrementing. The hook
			// was a no-op before this point because the logger is built
			// in cmd/internal/app/app.go before any boot component runs.
			syslogs.SetEmissionCollector(collector)
			// Bind the eventbus subscriber-cap counters. Same lifecycle
			// problem: the bus is created in boot/infrastructure.go
			// before this component runs.
			if b, ok := event.GetBus(ctx).(*eventbus.Bus); ok {
				b.SetCollector(collector)
			}
			return ctx, nil
		},
		Stop: func(ctx context.Context) error {
			// Detach the logger hook so a stopped collector doesn't keep
			// receiving emissions during shutdown.
			syslogs.SetEmissionCollector(nil)
			if b, ok := event.GetBus(ctx).(*eventbus.Bus); ok {
				b.SetCollector(nil)
			}
			if collector != nil {
				return collector.Close()
			}
			return nil
		},
	})
}

func loadConfig(ctx context.Context) apicfg.Config {
	var cfg apicfg.Config

	bootCfg := boot.GetConfig(ctx)
	if bootCfg == nil {
		return cfg
	}

	metricsCfg := bootCfg.Sub("metrics")
	if metricsCfg == nil {
		return cfg
	}

	cfg.Interceptor.Enabled = metricsCfg.GetBool("interceptor.enabled", false)
	cfg.Buffer.Size = metricsCfg.GetInt("buffer.size", 10000)

	return cfg
}
