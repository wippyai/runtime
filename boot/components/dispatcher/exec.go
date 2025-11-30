package dispatcher

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/service/dispatcher/exec"
	sysdispatcher "github.com/wippyai/runtime/system/dispatcher"
)

func Exec() boot.Component {
	return boot.New(boot.P{
		Name:      ExecName,
		DependsOn: []boot.ComponentName{DispatcherDeps},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := exec.NewService()
			svc.RegisterAll(sysdispatcher.Register)
			return ctx, nil
		},
	})
}
