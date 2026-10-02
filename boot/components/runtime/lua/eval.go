// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/boot"
	ctxapi "github.com/wippyai/runtime/api/context"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	apihost "github.com/wippyai/runtime/api/host"
	logapi "github.com/wippyai/runtime/api/logs"
	processapi "github.com/wippyai/runtime/api/process"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
)

// defaultEvalCacheSize bounds the eval compile cache when not configured.
const defaultEvalCacheSize = 256

const EvalHostName boot.Name = "runtime.lua.eval"

// FrameResolverOrderChildSlots is the apply order of the child slot resolver.
const FrameResolverOrderChildSlots = 100

const childSlotsResolverName = "process.child_slots"

func evalMaxSteps(luaCfg boot.Config) (uint64, error) {
	if luaCfg == nil {
		return evalhost.DefaultMaxSteps, nil
	}
	configured := luaCfg.GetInt("eval.max_steps", int(evalhost.DefaultMaxSteps))
	if configured < 0 {
		return 0, fmt.Errorf("lua.eval.max_steps cannot be negative")
	}
	return uint64(configured), nil
}

// Eval creates the eval host boot component.
func Eval() boot.Component {
	return boot.New(boot.P{
		Name:      EvalHostName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, EngineName, system.ProcessManagerName, system.FrameResolversName},
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
			maxSteps := evalhost.DefaultMaxSteps
			programCacheSize := evalhost.DefaultEvalProgramCacheSize
			var spawnHost string
			if cfg := boot.GetConfig(ctx); cfg != nil {
				if luaCfg := cfg.Sub("lua"); luaCfg != nil {
					evalCacheSize = luaCfg.GetInt("eval.cache_size", evalCacheSize)
					evalCacheTTL = luaCfg.GetDuration("eval.cache_ttl", evalCacheTTL)
					programCacheSize = luaCfg.GetInt("eval.program_cache_size", programCacheSize)
					spawnHost = luaCfg.GetString("eval.spawn_host", "")
					resolvedMaxSteps, err := evalMaxSteps(luaCfg)
					if err != nil {
						return ctx, err
					}
					maxSteps = resolvedMaxSteps
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
				evalhost.WithDefaultMaxSteps(maxSteps),
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

			// The eval module runs programs as processes started through the
			// process manager; spawns from a limited eval reserve child slots.
			manager := processapi.GetManager(ctx)
			if manager == nil {
				return ctx, ErrProcessManagerNotFound
			}
			resolvers := ctxapi.FrameResolversFrom(ctx)
			if resolvers == nil {
				return ctx, ErrFrameResolversNotFound
			}
			if err := resolvers.Register(childSlotsResolverName, FrameResolverOrderChildSlots, processapi.ChildSlotResolver); err != nil {
				return ctx, fmt.Errorf("register child slot resolver: %w", err)
			}
			opts := []evalhost.AdmitterOption{evalhost.WithProgramCacheSize(programCacheSize)}
			if spawnHost != "" {
				opts = append(opts, evalhost.WithSpawnHost(spawnHost))
			}
			apihost.WithEvalHost(ctx, evalhost.NewAdmitter(host, manager, opts...))

			return ctx, nil
		},
	})
}
