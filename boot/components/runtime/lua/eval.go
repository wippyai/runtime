package lua

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	dispatcherapi "github.com/wippyai/runtime/api/dispatcher"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/process"
	luaapi "github.com/wippyai/runtime/api/runtime/lua"
	"github.com/wippyai/runtime/boot/components/dispatchers"
	"github.com/wippyai/runtime/runtime/lua/evalhost"
	envlua "github.com/wippyai/runtime/runtime/lua/modules/env"
	"github.com/wippyai/runtime/runtime/lua/modules/json"
	loggermod "github.com/wippyai/runtime/runtime/lua/modules/logger"
	payloadmod "github.com/wippyai/runtime/runtime/lua/modules/payload"
	timemod "github.com/wippyai/runtime/runtime/lua/modules/time"
)

const EvalHostName boot.Name = "runtime.lua.eval"

// Eval creates the eval host boot component.
func Eval() boot.Component {
	return boot.New(boot.P{
		Name:      EvalHostName,
		DependsOn: []boot.Name{dispatchers.ClockDispatcherName, LuaEngineName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			reg := dispatcherapi.GetRegistrar(ctx)
			if reg == nil {
				return ctx, ErrDispatcherRegistrarNotFound
			}

			// Get process factory from context for ID-based sandbox creation
			factory := process.GetFactory(ctx)

			// Modules available for eval'd code
			modules := []luaapi.Module{
				json.Module,
				timemod.Module,
				payloadmod.Module,
				envlua.Module,
				loggermod.Module,
			}

			// Create eval host with class-based filtering
			host := evalhost.NewHost(
				logger.Named("eval"),
				modules,
				factory,
			)

			// Register eval host in context (we need local factories)
			evalhost.WithHost(ctx, host)

			// Register dispatcher handlers
			d := evalhost.NewDispatcher(host)
			d.RegisterAll(reg.Register)

			return ctx, nil
		},
	})
}
