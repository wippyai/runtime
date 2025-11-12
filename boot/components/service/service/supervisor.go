//go:build !plugin_minimal

package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	procapi "github.com/ponyruntime/pony/api/process"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootsystem "github.com/ponyruntime/pony/boot/components/system/system"
	service "github.com/ponyruntime/pony/service/supervisor"
	"github.com/ponyruntime/pony/system/process"
)

func ProcessSupervisor() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessSupervisorName,
		Phase:     boot.PostInit,
		DependsOn: []string{bootsystem.ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			bus := event.GetBus(ctx)
			processes := procapi.GetManager(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			processManager, ok := processes.(*process.Manager)
			if !ok {
				return ctx, fmt.Errorf("process manager is not of expected type")
			}

			manager := service.NewSupervisorServiceManager(
				bus,
				processManager,
				logger.Named("super"),
			)

			handlers.RegisterListener("process.service", manager)
			return ctx, nil
		},
	})
}
