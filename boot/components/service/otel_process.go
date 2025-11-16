package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	procapi "github.com/wippyai/runtime/api/process"
	otelapi "github.com/wippyai/runtime/api/service/otel"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/system/process"
)

func OTelProcess() boot.Component {
	return boot.New(boot.P{
		Name:      OTelProcessName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{OTelName, bootsystem.ProcessName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			manager := procapi.GetManager(ctx)
			if manager == nil {
				return ctx, fmt.Errorf("process manager not available in context")
			}

			processManager, ok := manager.(*process.Manager)
			if !ok {
				return ctx, fmt.Errorf("process manager is not *process.Manager")
			}

			processManager.RegisterMutator(svc.ProcessMutator())

			return ctx, nil
		},
		Start: func(_ context.Context) error {
			return nil
		},
		Stop: func(_ context.Context) error {
			return nil
		},
	})
}
