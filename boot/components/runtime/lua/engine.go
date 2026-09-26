// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"path/filepath"

	"github.com/wippyai/go-lua/compiler/check"

	glua "github.com/wippyai/go-lua"
	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	logapi "github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	corecomponents "github.com/wippyai/runtime/boot/components/core"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/internal/cachedir"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/code/cache"
	"github.com/wippyai/runtime/runtime/lua/component"
	funclua "github.com/wippyai/runtime/runtime/lua/component/function"
	"github.com/wippyai/runtime/runtime/lua/component/library"
	proclua "github.com/wippyai/runtime/runtime/lua/component/process"
	workflowlua "github.com/wippyai/runtime/runtime/lua/component/workflow"
	"github.com/wippyai/runtime/runtime/lua/engine"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	reghandler "github.com/wippyai/runtime/system/registry/events"
	"go.uber.org/zap"
)

var toolchainIdentityResolver = code.ToolchainIdentity

func Engine() boot.Component {
	var funcs *funclua.Manager
	var started bool

	return boot.New(boot.P{
		Name:      EngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			glua.ConfigureErrorMetadataExtractor(extractLuaErrorMetadata)

			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, ErrDispatcherNotFound
			}

			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			settings := resolveEngineSettings(boot.GetConfig(ctx), logger.Named("lua"))
			settings.Modules = []*luaapi.ModuleDef{
				ostime.Module,
				processmod.Module,
				engine.ChannelModule,
			}

			codeManager, err := code.NewCodeManager(logger.Named("lua"), bus, settings)
			if err != nil {
				return ctx, err
			}

			ctx = SetCodeManager(ctx, codeManager)

			// Get filesystem registry
			fsReg := fsapi.GetRegistry(ctx)

			// Create ProcessFactory for use by all managers
			processFactory := engine.NewProcessFactory(codeManager)

			// Create consolidated managers
			funcs = funclua.NewManager(
				logger.Named("lua.func"),
				codeManager,
				bus,
				disp,
				fsReg,
				processFactory,
			)
			libraries := library.NewManager(logger.Named("lua.lib"), codeManager, fsReg)
			processes := proclua.NewManager(logger.Named("lua.process"), codeManager, bus, fsReg, processFactory)
			workflows := workflowlua.NewManager(logger.Named("lua.workflow"), codeManager, bus, fsReg, processFactory)

			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua.**", funcs))
			handlers.Register(component.NewHandler("library.lua.**", libraries))
			handlers.Register(component.NewHandler("process.lua.**", processes))
			handlers.Register(component.NewHandler("workflow.lua.**", workflows))

			return ctx, nil
		},
		Start: func(ctx context.Context) error {
			if funcs == nil || started {
				return nil
			}
			if err := funcs.Start(ctx); err != nil {
				return err
			}
			started = true
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs == nil || !started {
				return nil
			}
			funcs.Stop()
			started = false
			return nil
		},
	})
}

// typeSystemOptions reads the type-checking semantics from the lua.type_system
// section. Lint, compile-time checks and the LSP all check with these options.
func typeSystemOptions(cfg boot.Config) check.Options {
	return check.Options{
		StrictAny: cfg.GetBool("strict_any", false),
	}
}

func resolveEngineSettings(cfg boot.Config, logger *zap.Logger) code.Config {
	defaultDir := filepath.Join(cachedir.Dir(), "lua")
	settings := code.Config{
		Cache: cache.Config{
			Dir:              defaultDir,
			Mode:             cache.ModeReadWrite,
			Enabled:          true,
			CompileEnabled:   true,
			TypecheckEnabled: true,
			MaxBytes:         cache.DefaultMaxBytes,
			MaxEntries:       cache.DefaultMaxEntries,
			PruneInterval:    cache.DefaultPruneInterval,
		},
		InvalidationWaitTimeout: code.DefaultInvalidationWaitTimeout,
	}
	if cfg != nil {
		registryCfg := cfg.Sub(corecomponents.RegistryName)
		settings.InvalidationWaitTimeout = registryCfg.GetDuration(
			corecomponents.RegistryEventWaitTimeout,
			settings.InvalidationWaitTimeout,
		)

		luaCfg := cfg.Sub("lua")
		settings.InvalidationWaitTimeout = luaCfg.GetDuration("invalidation_wait_timeout", settings.InvalidationWaitTimeout)

		typeSystemCfg := luaCfg.Sub("type_system")
		settings.TypeCheck.Enabled = typeSystemCfg.GetBool("enabled", false)
		settings.TypeCheck.Strict = typeSystemCfg.GetBool("strict", false)
		settings.TypeCheck.Check = typeSystemOptions(typeSystemCfg)

		if _, ok := luaCfg.Get("cache.enabled"); ok {
			settings.Cache.Enabled = luaCfg.GetBool("cache.enabled", settings.Cache.Enabled)
		}
		if rawDir := luaCfg.GetString("cache.dir", ""); rawDir != "" {
			settings.Cache.Dir = rawDir
			if !filepath.IsAbs(settings.Cache.Dir) {
				if baseDir := cfg.GetString("boot.config_dir", ""); baseDir != "" {
					settings.Cache.Dir = filepath.Join(baseDir, settings.Cache.Dir)
				}
			}
		}
		settings.Cache.Mode = cache.ParseMode(luaCfg.GetString("cache.mode", string(settings.Cache.Mode)))
		settings.Cache.CompileEnabled = luaCfg.GetBool("cache.compile.enabled", settings.Cache.CompileEnabled)
		settings.Cache.TypecheckEnabled = luaCfg.GetBool("cache.typecheck.enabled", settings.Cache.TypecheckEnabled)
		settings.Cache.MaxBytes = int64(luaCfg.GetInt("cache.max_bytes", int(settings.Cache.MaxBytes)))
		settings.Cache.MaxEntries = luaCfg.GetInt("cache.max_entries", settings.Cache.MaxEntries)
		settings.Cache.PruneInterval = luaCfg.GetInt("cache.prune_interval", settings.Cache.PruneInterval)
	}

	if !settings.Cache.Enabled {
		return settings
	}

	toolchainID, err := toolchainIdentityResolver()
	if err != nil {
		logger.Warn("lua toolchain identity unavailable; persistent cache disabled",
			zap.Error(err),
		)
		settings.Cache.Enabled = false
		return settings
	}
	settings.Cache.ToolchainIdentity = toolchainID

	if err := cachedir.Probe(settings.Cache.Dir); err != nil {
		logger.Warn("lua cache directory unusable; persistent cache disabled",
			zap.String("dir", settings.Cache.Dir),
			zap.Error(err),
		)
		settings.Cache.Enabled = false
		return settings
	}

	return settings
}

func extractLuaErrorMetadata(err error) *glua.ErrorMetadata {
	if err == nil {
		return nil
	}

	chain := apierror.BuildChain(err)
	if chain == nil {
		return nil
	}
	root := chain.Root()
	if root == nil {
		return nil
	}

	meta := &glua.ErrorMetadata{}
	if root.Kind != "" {
		meta.Kind = glua.Kind(root.Kind)
	}
	if root.Retryable != nil {
		b := *root.Retryable
		meta.Retryable = &b
	}
	if len(root.Details) > 0 {
		meta.Details = make(map[string]any, len(root.Details))
		for k, v := range root.Details {
			meta.Details[k] = v
		}
	}

	if meta.Kind == "" && meta.Retryable == nil && meta.Details == nil {
		return nil
	}
	return meta
}
