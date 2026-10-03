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

const childSlotsResolverName = "process.child_slots"

// evalSettings holds the lua.eval.* configuration:
//
//	lua.eval.cache_size            compile cache entries of the eval_runner host
//	lua.eval.cache_ttl             lifetime of compile cache entries; zero keeps them
//	lua.eval.max_steps             default step limit of eval_runner; zero is unlimited
//	lua.eval.program_cache_size    compiled programs cached by the eval module
//	lua.eval.spawn_host            host that runs eval module processes; empty uses the caller's
//	lua.eval.detached_lifetime     run-time bound of a detached eval process
type evalSettings struct {
	spawnHost        string
	cacheSize        int
	programCacheSize int
	cacheTTL         time.Duration
	detachedLifetime time.Duration
	maxSteps         uint64
}

func resolveEvalSettings(luaCfg boot.Config) (evalSettings, error) {
	settings := evalSettings{
		cacheSize:        defaultEvalCacheSize,
		maxSteps:         evalhost.DefaultMaxSteps,
		programCacheSize: evalhost.DefaultEvalProgramCacheSize,
		detachedLifetime: evalhost.DefaultDetachedEvalLifetime,
	}
	if luaCfg == nil {
		return settings, nil
	}
	settings.cacheSize = luaCfg.GetInt("eval.cache_size", settings.cacheSize)
	settings.cacheTTL = luaCfg.GetDuration("eval.cache_ttl", settings.cacheTTL)
	settings.programCacheSize = luaCfg.GetInt("eval.program_cache_size", settings.programCacheSize)
	if settings.programCacheSize <= 0 {
		return settings, fmt.Errorf("lua.eval.program_cache_size must be positive")
	}
	settings.spawnHost = luaCfg.GetString("eval.spawn_host", "")
	settings.detachedLifetime = luaCfg.GetDuration("eval.detached_lifetime", settings.detachedLifetime)
	if settings.detachedLifetime <= 0 {
		return settings, fmt.Errorf("lua.eval.detached_lifetime must be positive")
	}
	maxSteps, err := evalMaxSteps(luaCfg)
	if err != nil {
		return settings, err
	}
	settings.maxSteps = maxSteps
	return settings, nil
}

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

			var luaCfg boot.Config
			if cfg := boot.GetConfig(ctx); cfg != nil {
				luaCfg = cfg.Sub("lua")
			}
			settings, err := resolveEvalSettings(luaCfg)
			if err != nil {
				return ctx, err
			}

			// Create eval host with dynamic module provider
			evalLogger := logger.Named("eval")
			host := evalhost.NewHost(
				evalLogger,
				cm.GetModuleDefs,
				evalhost.WithProgramCache(evalhost.HostConfig{
					CacheSize: settings.cacheSize,
					CacheTTL:  settings.cacheTTL,
				}),
				evalhost.WithDefaultMaxSteps(settings.maxSteps),
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
			if err := resolvers.Register(childSlotsResolverName, system.FrameResolverOrderChildSlots, processapi.ChildSlotResolver); err != nil {
				return ctx, fmt.Errorf("register child slot resolver: %w", err)
			}
			opts := []evalhost.AdmitterOption{
				evalhost.WithProgramCacheSize(settings.programCacheSize),
				evalhost.WithDetachedLifetime(settings.detachedLifetime),
			}
			if settings.spawnHost != "" {
				opts = append(opts, evalhost.WithSpawnHost(settings.spawnHost))
			}
			apihost.WithEvalHost(ctx, evalhost.NewAdmitter(host, manager, opts...))

			return ctx, nil
		},
	})
}
