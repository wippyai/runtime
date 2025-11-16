package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/pidgen"
	procapi "github.com/wippyai/runtime/api/process"
	bootpkg "github.com/wippyai/runtime/boot"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	service "github.com/wippyai/runtime/service/supervisor"
	"github.com/wippyai/runtime/system/process"
)

func ProcessSupervisor() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessSupervisorName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootsystem.ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			processes := procapi.GetManager(ctx)
			pidGen := pidgen.GetGenerator(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			processManager, ok := processes.(*process.Manager)
			if !ok {
				return ctx, fmt.Errorf("process manager is not of expected type")
			}

			manager := service.NewSupervisorServiceManager(
				bus,
				processManager,
				logger.Named("super"),
				pidGen,
			)

			handlers.RegisterListener("process.service", manager)
			return ctx, nil
		},
	})
}
