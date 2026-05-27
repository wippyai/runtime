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
	bootsys "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/env/composite"
	"go.uber.org/zap"
)

func Composite() boot.Component {
	return boot.New(boot.P{
		Name:      CompositeName,
		DependsOn: []boot.Name{MemoryName, FileName, OSName, StaticName, bootsys.EnvironmentName, bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)
			reg := regapi.GetRegistry(ctx)
			if reg != nil {
				err := reg.RegisterDependencyPattern(regapi.DependencyPattern{
					Path:          "data.storages",
					Description:   "Environment router backing storages",
					AllowWildcard: true,
				})
				if err != nil {
					logger.Warn("failed to register env router dependency pattern",
						zap.String("path", "data.storages"),
						zap.Error(err))
				}
			}

			manager := composite.NewManager(
				bus,
				dtt,
				logger.Named("env.composite"),
			)

			handlers.RegisterListener("env.storage.router", manager)
			return ctx, nil
		},
	})
}
