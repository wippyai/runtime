//go:build !plugin_minimal

package service

import (
	"context"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/aws/s3"
)

func S3() boot.Component {
	return boot.New(boot.P{
		Name:      S3Name,
		Phase:     boot.PostInit,
		DependsOn: nil,
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := s3.NewManager(
				bus,
				dtt,
				logger.Named("cloudstorage.s3"),
			)

			handlers.RegisterListener("cloudstorage.s3", manager)
			return ctx, nil
		},
	})
}
