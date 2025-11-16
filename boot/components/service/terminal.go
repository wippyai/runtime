package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/terminal"
)

func Terminal() boot.Component {
	return boot.New(boot.P{
		Name:      TerminalName,
		Phase:     boot.PostInit,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := terminal.NewTerminalManager(
				bus,
				dtt,
				logger.Named("terminal"),
			)

			handlers.RegisterListener("terminal.host", manager)
			return ctx, nil
		},
	})
}
