package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	native "github.com/wippyai/runtime/service/exec"
)

func Exec() boot.Component {
	return boot.New(boot.P{
		Name:      NativeExecName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := native.NewManager(
				bus,
				dtt,
				logger.Named("exec"),
			)

			handlers.RegisterListener("exec.native", manager)
			return ctx, nil
		},
	})
}
