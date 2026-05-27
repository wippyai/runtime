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
	"github.com/wippyai/runtime/service/aws/config"
	"go.uber.org/zap"
)

func AWS() boot.Component {
	return boot.New(boot.P{
		Name:      ConfigName,
		DependsOn: []boot.Name{bootsystem.EnvironmentName, bootcore.RegistryName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			envRegistry := envapi.GetRegistry(ctx)
			handlers := bootpkg.GetHandlerRegistry(ctx)

			if reg := regapi.GetRegistry(ctx); reg != nil {
				awsPatterns := []regapi.DependencyPattern{
					{Path: "data.region_env", Description: "Env variable holding the AWS region"},
					{Path: "data.access_key_id_env", Description: "Env variable holding the AWS access key ID"},
					{Path: "data.secret_access_key_env", Description: "Env variable holding the AWS secret access key"},
				}
				for _, pattern := range awsPatterns {
					if err := reg.RegisterDependencyPattern(pattern); err != nil {
						logger.Warn("failed to register AWS dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
					}
				}
			}

			manager := config.NewManager(
				bus,
				dtt,
				logger.Named("config.aws"),
				envRegistry,
			)

			handlers.RegisterListener("config.aws", manager)
			return ctx, nil
		},
	})
}
