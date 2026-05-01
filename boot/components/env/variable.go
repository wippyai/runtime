// SPDX-License-Identifier: MPL-2.0

package env

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	envsvc "github.com/wippyai/runtime/service/env"
	"go.uber.org/zap"
)

func Variable() boot.Component {
	return boot.New(boot.P{
		Name:      VariableName,
		DependsOn: []boot.Name{bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			reg := regapi.GetRegistry(ctx)
			if reg != nil {
				err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
					Path:        "data.storage",
					Description: "Environment variable storage backend",
				})
				if err != nil {
					logger.Warn("failed to register env variable dependency pattern",
						zap.String("path", "data.storage"),
						zap.Error(err))
				}
			}

			manager := envsvc.NewVariableManager(
				bus,
				dtt,
				logger.Named("env.variable"),
			)

			handlers.RegisterListener("env.variable", manager)
			return ctx, nil
		},
	})
}
