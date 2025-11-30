package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/http"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      HTTPName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := http.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
