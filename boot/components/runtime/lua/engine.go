package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	bteaapp "github.com/wippyai/runtime/runtime/lua/component/btea"
	funclua "github.com/wippyai/runtime/runtime/lua/component/function"
	"github.com/wippyai/runtime/runtime/lua/component/library"
	proclua "github.com/wippyai/runtime/runtime/lua/component/process"
	workflowlua "github.com/wippyai/runtime/runtime/lua/component/workflow"
	envlua "github.com/wippyai/runtime/runtime/lua/modules/env"
	loggermod "github.com/wippyai/runtime/runtime/lua/modules/logger"
	reghandler "github.com/wippyai/runtime/system/registry/events"
)

func Engine() boot.Component {
	return boot.New(boot.P{
		Name:      LuaEngineName,
		DependsOn: []boot.ComponentName{},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			cfg := boot.GetConfig(ctx)

			// Get cache sizes from config with defaults
			protoCacheSize := 60000
			mainCacheSize := 10000
			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					protoCacheSize = luaCfg.GetInt("proto_cache_size", protoCacheSize)
					mainCacheSize = luaCfg.GetInt("main_cache_size", mainCacheSize)
				}
			}

			codeManager, err := code.NewCodeManager(
				logger.Named("lua"),
				bus,
				code.Config{
					Modules: []luaapi.Module{
						// Core infrastructure modules only
						// Other modules are added via individual components
						envlua.NewEnvModule(),
						loggermod.NewLoggerModule(logger),
					},
					ProtoCacheSize: protoCacheSize,
					MainCacheSize:  mainCacheSize,
				},
			)
			if err != nil {
				return ctx, err
			}

			// Store code manager in context for other plugins to use
			ctx = SetCodeManager(ctx, codeManager)

			// Create component managers
			funcs := funclua.NewManager(logger.Named("lua.funcs"), codeManager, bus)
			libraries := library.NewManager(logger.Named("lua.libs"), codeManager)
			processes := proclua.NewProcessManager(logger.Named("lua.proc"), codeManager, bus)
			workflows := workflowlua.NewManager(logger.Named("lua.workflow"), codeManager, bus)
			terminalApps := bteaapp.NewBteaManager(logger.Named("lua.bteaapp"), codeManager, bus)

			// Register all handlers
			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua", funcs))
			handlers.Register(component.NewHandler("library.lua", libraries))
			handlers.Register(component.NewHandler("process.lua", processes))
			handlers.Register(component.NewHandler("workflow.lua", workflows))
			handlers.Register(component.NewHandler("btea.app.lua", terminalApps))

			return ctx, nil
		},
	})
}
