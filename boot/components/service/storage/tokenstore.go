package storage

import (
	"context"
	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	resourceapi "github.com/wippyai/runtime/api/resource"
	secapi "github.com/wippyai/runtime/api/security"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/security/tokenstore"
	"go.uber.org/zap"
)

func TokenStore() boot.Component {
	return boot.New(boot.P{
		Name:      TokenStoreName,
		DependsOn: []string{bootcore.RegistryName, bootsystem.ResourcesName, bootcore.SecurityName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("tokenstore")
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

			resources := resourceapi.GetRegistry(ctx)
			if resources == nil {
				return ctx, ErrResourceRegistryNotAvailable
			}

			security, _ := secapi.GetRegistry(ctx)
			if security == nil {
				return ctx, ErrSecurityRegistryNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			// Register security dependency patterns
			securityPatterns := []regapi.DependencyPattern{
				{Path: "data.token_store", Description: "Reference to token storage"},
				{Path: "data.lifecycle.security.policies", Description: "Security policies", AllowWildcard: true},
				{Path: "data.lifecycle.security.groups", Description: "Security groups", AllowWildcard: true},
				{Path: "data.security.policies", Description: "Direct security policies", AllowWildcard: true},
				{Path: "data.security.groups", Description: "Direct security groups", AllowWildcard: true},
				{Path: "data.security.token_store", Description: "Token store reference"},
			}
			for _, pattern := range securityPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register security dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

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
