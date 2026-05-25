// SPDX-License-Identifier: MPL-2.0

package aws

import (
	"context"

	"github.com/wippyai/runtime/api/boot"
	envapi "github.com/wippyai/runtime/api/env"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/aws/s3"
	"go.uber.org/zap"
)

func S3() boot.Component {
	return boot.New(boot.P{
		Name:      S3Name,
		DependsOn: []boot.Name{bootsystem.EnvironmentName, bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("s3")
			if logger == nil {
				return ctx, ErrLoggerNotAvailable
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, ErrTranscoderNotAvailable
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, ErrEventBusNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			envRegistry := envapi.GetRegistry(ctx)

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register storage dependency patterns
			storagePatterns := []regapi.DependencyPattern{
				{Path: "data.store", Description: "Reference to a store (e.g., 'session')"},
				{Path: "data.storage", Description: "Reference to a storage"},
				{Path: "data.bucket", Description: "Reference to a storage bucket"},
				{Path: "data.bucket_env", Description: "Env variable holding the storage bucket name"},
				{Path: "data.endpoint_env", Description: "Env variable holding the S3-compatible endpoint URL"},
			}
			for _, pattern := range storagePatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register storage dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			manager := s3.NewManager(
				bus,
				dtt,
				logger.Named("cloudstorage.s3"),
				envRegistry,
			)

			handlers.RegisterListener("cloudstorage.s3", manager)
			return ctx, nil
		},
	})
}
