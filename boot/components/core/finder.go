package core

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	logapi "github.com/wippyai/runtime/api/logs"
	regapi "github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/registry/finder"
	"go.uber.org/zap"
)

func Finder() boot.Component {
	return boot.New(boot.P{
		Name:      FinderName,
		DependsOn: []boot.ComponentName{RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Read configuration
			var opts []finder.Option
			cfg := boot.GetConfig(ctx)
			if cfg != nil {
				finderCfg := cfg.Sub(string(FinderName))

				// Query cache size configuration
				queryCacheSize := finderCfg.GetInt(string(FinderQueryCacheSize), 1000)
				if queryCacheSize > 0 {
					opts = append(opts, finder.WithQueryCacheSize(queryCacheSize))
				}

				// Regex cache size configuration
				regexCacheSize := finderCfg.GetInt(string(FinderRegexCacheSize), 100)
				if regexCacheSize > 0 {
					opts = append(opts, finder.WithRegexCacheSize(regexCacheSize))
				}

				logger.Debug("finder configuration loaded",
					zap.Int("query_cache_size", queryCacheSize),
					zap.Int("regex_cache_size", regexCacheSize))
			}

			f := finder.NewFinder(reg, logger.Named("finder"), opts...)

			return regapi.WithFinder(ctx, f), nil
		},
	})
}
