//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	"github.com/ponyruntime/pony/api/payload"
	"github.com/ponyruntime/pony/service/interceptor/retry"
)

func InterceptorRetry() boot.Component {
	return boot.New(boot.P{
		Name:      InterceptorRetryName,
		Phase:     boot.PostInit,
		DependsOn: []string{"eventbus", "interceptor-manager"},
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
