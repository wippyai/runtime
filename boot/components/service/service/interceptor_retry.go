package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/service/interceptor/retry"
)

func InterceptorRetry() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorRetryName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{InterceptorManagerName},
		Load: func(ctx context.Context) (context.Context, error) {
			bus := event.GetBus(ctx)

			bus.Send(ctx, event.Event{
				System: "interceptor",
				Kind:   "interceptor.register",
				Path:   "interceptor/retry",
				Data:   payload.New(retry.New()),
			})

			return ctx, nil
		},
	})
}
