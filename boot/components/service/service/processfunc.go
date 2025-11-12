//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootsystem "github.com/ponyruntime/pony/boot/components/system/system"
	"github.com/ponyruntime/pony/service/processfunc"
)

func ProcessFunc() boot.Component {
	return boot.New(boot.P{
		Name:      "process_function_bridge",
		Phase:     boot.PostInit,
		DependsOn: []string{bootsystem.ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			processes := procapi.GetManager(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			handler := processfunc.WithProcessFunctionBridge(
				logger.Named("pfunc"),
				bus,
				processes,
			)

			handlers.Register(handler)
			return ctx, nil
		},
	})
}
