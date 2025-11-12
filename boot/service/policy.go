//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/policy"
)

func Policy() boot.Plugin {
	return boot.New(boot.P{
		Name:      YAMLPolicyName,
		Phase:     boot.PostInit,
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
