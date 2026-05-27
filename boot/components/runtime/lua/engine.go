// SPDX-License-Identifier: MPL-2.0

package lua

import (
	"context"
	"path/filepath"

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
)

func Engine() boot.Component {
	var funcs *funclua.Manager

	return boot.New(boot.P{
		Name:      EngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName},
		Load: func(ctx context.Context) (context.Context, error) {
			glua.ConfigureErrorMetadataExtractor(extractLuaErrorMetadata)

			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			cfg := boot.GetConfig(ctx)

			// Get cache sizes from config with defaults
			protoCacheSize := 60000
			mainCacheSize := 10000
			typeCheckEnabled := false
			typeCheckStrict := false
			invalidationWaitTimeout := code.DefaultInvalidationWaitTimeout
			cacheCfg := cache.Config{
				Enabled:          false,
				Dir:              cache.DefaultDir,
				Mode:             cache.ModeReadWrite,
				CompileEnabled:   true,
				TypecheckEnabled: true,
			}
			if cfg != nil {
				registryCfg := cfg.Sub(corecomponents.RegistryName)
				if registryCfg != nil {
					invalidationWaitTimeout = registryCfg.GetDuration(corecomponents.RegistryEventWaitTimeout, invalidationWaitTimeout)
				}
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					protoCacheSize = luaCfg.GetInt("proto_cache_size", protoCacheSize)
					mainCacheSize = luaCfg.GetInt("main_cache_size", mainCacheSize)
					invalidationWaitTimeout = luaCfg.GetDuration("invalidation_wait_timeout", invalidationWaitTimeout)
					typeSysCfg := luaCfg.Sub("type_system")
					if typeSysCfg != nil {
						typeCheckEnabled = typeSysCfg.GetBool("enabled", false)
						typeCheckStrict = typeSysCfg.GetBool("strict", typeCheckStrict)
					}
					cacheCfg.Enabled = typeCheckEnabled
					if _, ok := luaCfg.Get("cache.enabled"); ok {
						cacheCfg.Enabled = luaCfg.GetBool("cache.enabled", cacheCfg.Enabled)
					}
					cacheCfg.Dir = luaCfg.GetString("cache.dir", cacheCfg.Dir)
					if cacheCfg.Dir != "" && !filepath.IsAbs(cacheCfg.Dir) {
						if baseDir := cfg.GetString("boot.config_dir", ""); baseDir != "" {
							cacheCfg.Dir = filepath.Join(baseDir, cacheCfg.Dir)
						}
					}
					cacheCfg.Mode = cache.ParseMode(luaCfg.GetString("cache.mode", string(cacheCfg.Mode)))
					cacheCfg.CompileEnabled = luaCfg.GetBool("cache.compile.enabled", cacheCfg.CompileEnabled)
					cacheCfg.TypecheckEnabled = luaCfg.GetBool("cache.typecheck.enabled", cacheCfg.TypecheckEnabled)
				}
			}

			codeManager, err := code.NewCodeManager(
				logger.Named("lua"),
				bus,
				code.Config{
					Modules: []*luaapi.ModuleDef{
						ostime.Module,
						processmod.Module,
						engine.ChannelModule,
					},
					ProtoCacheSize: protoCacheSize,
					MainCacheSize:  mainCacheSize,
					TypeCheck: code.TypeCheckConfig{
						Enabled: typeCheckEnabled,
						Strict:  typeCheckStrict,
					},
					Cache:                   cacheCfg,
					InvalidationWaitTimeout: invalidationWaitTimeout,
				},
			)
			if err != nil {
				return ctx, err
			}

			ctx = SetCodeManager(ctx, codeManager)

			// Get dispatcher from context
			disp := dispatcherapi.GetDispatcher(ctx)
			if disp == nil {
				return ctx, ErrDispatcherNotFound
			}

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
			if funcs != nil {
				return funcs.Start(ctx)
			}
			return nil
		},
		Stop: func(_ context.Context) error {
			if funcs != nil {
				funcs.Stop()
			}
			return nil
		},
	})
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
