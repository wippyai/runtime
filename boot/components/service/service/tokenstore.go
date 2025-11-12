//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	bootsystem "github.com/ponyruntime/pony/boot/components/system/system"
	"github.com/ponyruntime/pony/service/tokenstore"
)

func TokenStore() boot.Component {
	return boot.New(boot.P{
		Name:      TokenStoreName,
		Phase:     boot.PostInit,
		DependsOn: []string{bootsystem.ResourcesName, bootcore.SecurityName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			resources := resourceapi.GetRegistry(ctx)
			security, _ := secapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := tokenstore.NewManager(
				bus,
				dtt,
				resources,
				security,
				logger.Named("tstore"),
			)

			handlers.RegisterListener("security.token_store", manager)
			return ctx, nil
		},
	})
}
