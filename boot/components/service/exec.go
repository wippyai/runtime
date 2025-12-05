package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/exec/docker"
	"github.com/wippyai/runtime/service/exec/native"
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

			nativeManager := native.NewManager(
				bus,
				dtt,
				logger.Named("exec.native"),
			)
			handlers.RegisterListener("exec.native", nativeManager)

			dockerManager := docker.NewManager(
				bus,
				dtt,
				logger.Named("exec.docker"),
			)
			handlers.RegisterListener("exec.docker", dockerManager)

			return ctx, nil
		},
	})
}
