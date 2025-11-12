//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	native "github.com/ponyruntime/pony/service/exec"
)

func Exec() boot.Component {
	return boot.New(boot.P{
		Name:      NativeExecName,
		Phase:     boot.PostInit,
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
