package storage

import (
	"context"
	"fmt"

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
	tokenstore "github.com/wippyai/runtime/service/store/token"
	"go.uber.org/zap"
)

func TokenStore() boot.Component {
	return boot.New(boot.P{
		Name:      TokenStoreName,
		DependsOn: []boot.ComponentName{bootcore.RegistryName, bootsystem.ResourcesName, bootcore.SecurityName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx)
			if logger == nil {
				return ctx, fmt.Errorf("logger not available in context")
			}

			dtt := payload.GetTranscoder(ctx)
			if dtt == nil {
				return ctx, fmt.Errorf("transcoder not available in context")
			}

			bus := event.GetBus(ctx)
			if bus == nil {
				return ctx, fmt.Errorf("event bus not available in context")
			}

			resources := resourceapi.GetRegistry(ctx)
			if resources == nil {
				return ctx, fmt.Errorf("resource registry not available in context")
			}

			security, _ := secapi.GetRegistry(ctx)
			if security == nil {
				return ctx, fmt.Errorf("security registry not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
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
