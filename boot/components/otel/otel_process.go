package otel

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	processapi "github.com/wippyai/runtime/api/process"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

const lifecycleName boot.Name = "system.lifecycle"

func OTelProcess() boot.Component {
	return boot.New(boot.P{
		Name:      ProcessName,
		DependsOn: []boot.Name{Name, lifecycleName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			lifecycleReg := processapi.GetLifecycleRegistry(ctx)
			if lifecycleReg == nil {
				return ctx, nil
			}

			lifecycleReg.Register("otel", svc)

			return ctx, nil
		},
	})
}
