package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	regapi "github.com/ponyruntime/pony/api/registry"
	resourceapi "github.com/ponyruntime/pony/api/resource"
	secapi "github.com/ponyruntime/pony/api/security"
	bootpkg "github.com/ponyruntime/pony/boot"
	bootcore "github.com/ponyruntime/pony/boot/components/core/core"
	bootsystem "github.com/ponyruntime/pony/boot/components/system/system"
	"github.com/ponyruntime/pony/service/tokenstore"
	"go.uber.org/zap"
)

func TokenStore() boot.Component {
	return boot.New(boot.P{
		Name:      TokenStoreName,
		Phase:     boot.PostInit,
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
