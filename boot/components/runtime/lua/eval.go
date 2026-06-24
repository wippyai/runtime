// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

// defaultEvalCacheSize bounds the eval compile cache when not configured.
const defaultEvalCacheSize = 256

const EvalHostName boot.Name = "runtime.lua.eval"

// Eval creates the eval host boot component.
func Eval() boot.Component {
	return boot.New(boot.P{
		Name:      EvalHostName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, EngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherRegistrarNotFound
			}

			// Get code manager for dynamic module lookup
			cm := GetCodeManager(ctx)
			if cm == nil {
				return ctx, ErrCodeManagerNotFound
			}

			// Resolve compile-cache settings (bounds recompilation of frequently
			// evaluated source).
			evalCacheSize := defaultEvalCacheSize
			var evalCacheTTL time.Duration
			if cfg := boot.GetConfig(ctx); cfg != nil {
				if luaCfg := cfg.Sub("lua"); luaCfg != nil {
					evalCacheSize = luaCfg.GetInt("eval.cache_size", evalCacheSize)
					evalCacheTTL = luaCfg.GetDuration("eval.cache_ttl", evalCacheTTL)
				}
			}

			// Create eval host with dynamic module provider
			evalLogger := logger.Named("eval")
			host := evalhost.NewHost(
				evalLogger,
				cm.GetModuleDefs,
				evalhost.WithProgramCache(evalhost.HostConfig{
					CacheSize: evalCacheSize,
					CacheTTL:  evalCacheTTL,
				}),
			)

			// Set up import loader to load library sources from code manager
			host.WithImportLoader(func(id registry.ID) (string, error) {
				node, err := cm.GetNode(id)
				if err != nil {
					return "", err
				}
				if node.Source == "" {
					return "", fmt.Errorf("import %s has no source (bytecode libraries not supported in eval)", id)
				}
				return node.Source, nil
			})

			// Register dispatcher handlers
			d := evalhost.NewDispatcher(host)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
