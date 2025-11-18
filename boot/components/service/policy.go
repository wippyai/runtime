package service

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	"github.com/wippyai/runtime/service/policy"
)

func Policy() boot.Component {
	return boot.New(boot.P{
		Name:      YAMLPolicyName,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := policy.NewManager(
				bus,
				policy.NewDefaultFactory(dtt),
				logger.Named("policy"),
			)

			handlers.RegisterListener("security.policy", manager)
			return ctx, nil
		},
	})
}
