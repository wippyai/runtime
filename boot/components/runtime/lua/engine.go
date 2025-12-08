package lua

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/runtime/lua/code"
	"github.com/wippyai/runtime/runtime/lua/component"
	funclua "github.com/wippyai/runtime/runtime/lua/component/function"
	"github.com/wippyai/runtime/runtime/lua/component/library"
	proclua "github.com/wippyai/runtime/runtime/lua/component/process"
	workflowlua "github.com/wippyai/runtime/runtime/lua/component/workflow"
	"github.com/wippyai/runtime/runtime/lua/modules/channel"
	"github.com/wippyai/runtime/runtime/lua/modules/ostime"
	processmod "github.com/wippyai/runtime/runtime/lua/modules/process"
	reghandler "github.com/wippyai/runtime/system/registry/events"
)

func Engine() boot.Component {
	var funcs *funclua.Manager

	return boot.New(boot.P{
		Name:      LuaEngineName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName},
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
						ostime.Module,
						processmod.Module,
						channel.Module,
					},
					ProtoCacheSize: protoCacheSize,
					MainCacheSize:  mainCacheSize,
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

			// Create function manager with dispatcher
			funcs = funclua.NewManager(
				logger.Named("lua.func"),
				codeManager,
				bus,
				disp,
			)
			libraries := library.NewManager(logger.Named("lua.lib"), codeManager)

			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua", funcs))
			handlers.Register(component.NewHandler("library.lua", libraries))

			// Register process and workflow managers
			processes := proclua.NewManager(logger.Named("lua.process"), codeManager, bus)
			workflows := workflowlua.NewManager(logger.Named("lua.workflow"), codeManager, bus)
			handlers.Register(component.NewHandler("process.lua", processes))
			handlers.Register(component.NewHandler("workflow.lua", workflows))

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
