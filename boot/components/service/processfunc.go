package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pidgen"
	procapi "github.com/wippyai/runtime/api/process"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/processfunc"
)

func ProcessFunc() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessFuncName,
		DependsOn: []boot.ComponentName{bootsystem.ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			processes := procapi.GetManager(ctx)
			pidGen := pidgen.GetGenerator(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			handler := processfunc.WithProcessFunctionBridge(
				logger.Named("pfunc"),
				bus,
				processes,
				pidGen,
			)

			handlers.Register(handler)
			return ctx, nil
		},
	})
}
