//go:build plugin_lua

package lua

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	luaapi "github.com/ponyruntime/pony/api/runtime/lua"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootcore "github.com/ponyruntime/pony/boot/core"
	"github.com/ponyruntime/pony/runtime/lua/code"
	"github.com/ponyruntime/pony/runtime/lua/command"
	"github.com/ponyruntime/pony/runtime/lua/component"
	bteaapp "github.com/ponyruntime/pony/runtime/lua/component/btea"
	funclua "github.com/ponyruntime/pony/runtime/lua/component/function"
	"github.com/ponyruntime/pony/runtime/lua/component/library"
	proclua "github.com/ponyruntime/pony/runtime/lua/component/process"
	ctxmod "github.com/ponyruntime/pony/runtime/lua/modules/ctx"
	envlua "github.com/ponyruntime/pony/runtime/lua/modules/env"
	loggermod "github.com/ponyruntime/pony/runtime/lua/modules/logger"
	"github.com/ponyruntime/pony/runtime/lua/task"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

func Engine() boot.Plugin {
	return boot.New(boot.P{
		Name:      LuaEngineName,
		Phase:     boot.PostInit,
		DependsOn: []string{bootcore.LoggerName, bootcore.EventBusName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			codeManager, err := code.NewCodeManager(
				logger.Named("lua"),
				bus,
				code.Config{
					Modules: []luaapi.Module{
						envlua.NewEnvModule(),
						loggermod.NewLoggerModule(logger),
						ctxmod.NewCtxModule(logger.Named("ctx")),
						task.NewTaskModule(),
						command.NewCommandModule(),
					},
					ProtoCacheSize: 60000,
					MainCacheSize:  10000,
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
			terminalApps := bteaapp.NewBteaManager(logger.Named("lua.bteaapp"), codeManager, bus)

			// Register all handlers
			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua", funcs))
			handlers.Register(component.NewHandler("library.lua", libraries))
			handlers.Register(component.NewHandler("process.lua", processes))
			handlers.Register(component.NewHandler("btea.app.lua", terminalApps))

			return ctx, nil
		},
	})
}
