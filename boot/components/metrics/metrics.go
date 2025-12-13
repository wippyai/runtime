package metrics

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	api "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
	impl "github.com/wippyai/runtime/service/metrics"
)

func Metrics() boot.Component {
	var collector api.Collector

	return boot.New(boot.P{
		Name:      MetricsName,
		DependsOn: []boot.Name{},
		Load: func(ctx context.Context) (context.Context, error) {
			cfg := loadConfig(ctx)
			collector = impl.NewCollector(cfg)
			ctx = api.WithCollector(ctx, collector)
			return ctx, nil
		},
		Stop: func(_ context.Context) error {
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
