package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
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
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			cfg := boot.GetConfig(ctx)

			// Get cache sizes from config with defaults
			protoCacheSize := 60000
			mainCacheSize := 10000
			typeCheckEnabled := false
			typeCheckStrict := false

			if cfg != nil {
				luaCfg := cfg.Sub("lua")
				if luaCfg != nil {
					protoCacheSize = luaCfg.GetInt("proto_cache_size", protoCacheSize)
					mainCacheSize = luaCfg.GetInt("main_cache_size", mainCacheSize)
					typeSysCfg := luaCfg.Sub("type_system")
					if typeSysCfg != nil {
						typeCheckEnabled = typeSysCfg.GetBool("enabled", typeCheckEnabled)
						typeCheckStrict = typeSysCfg.GetBool("strict", typeCheckStrict)
					}
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
			workflows := workflowlua.NewManager(logger.Named("lua.workflow"), codeManager, bus, processFactory)

			handlers.Register(reghandler.NewTransactionHandler(codeManager))
			handlers.Register(component.NewHandler("function.lua.**", funcs))
			handlers.Register(component.NewHandler("library.lua.**", libraries))
			handlers.Register(component.NewHandler("process.lua.**", processes))
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
