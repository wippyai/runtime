// SPDX-License-Identifier: MPL-2.0

package aws

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/aws/config"
)

func AWS() boot.Component {
	return boot.New(boot.P{
		Name:      ConfigName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName, bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			manager := config.NewManager(
				bus,
				dtt,
				logger.Named("config.aws"),
			)

			handlers.RegisterListener("config.aws", manager)
			return ctx, nil
		},
	})
}
