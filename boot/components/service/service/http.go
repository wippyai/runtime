package service

import (
	"context"
	"fmt"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	"github.com/wippyai/runtime/api/payload"
	regapi "github.com/wippyai/runtime/api/registry"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system/system"
	"github.com/wippyai/runtime/service/http"
	"github.com/wippyai/runtime/service/http/cors"
	"github.com/wippyai/runtime/service/http/firewall"
	"github.com/wippyai/runtime/service/http/realip"
	"github.com/wippyai/runtime/service/http/websocketrelay"
	"github.com/wippyai/runtime/service/tokenstore"
	"go.uber.org/zap"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      HTTPName,
		Phase:     boot.PostInit,
		DependsOn: []boot.ComponentName{bootcore.RegistryName, bootsystem.FunctionsName, bootsystem.FilesystemName},
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

			funcs := funcapi.GetRegistry(ctx)
			if funcs == nil {
				return ctx, fmt.Errorf("function registry not available in context")
			}

			fsRegistry := fsapi.GetRegistry(ctx)
			if fsRegistry == nil {
				return ctx, fmt.Errorf("filesystem registry not available in context")
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, fmt.Errorf("handler registry not available in context")
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, fmt.Errorf("registry not available in context")
			}

			// Register HTTP dependency patterns
			httpPatterns := []regapi.DependencyPattern{
				{Path: "meta.server", Description: "Reference to HTTP server in metadata"},
				{Path: "meta.router", Description: "Reference to router component in metadata"},
				{Path: "data.server", Description: "Reference to HTTP server in data section"},
				{Path: "data.middleware", Description: "List of middleware components", AllowWildcard: true},
				{Path: "data.post_middleware", Description: "List of post-middleware components", AllowWildcard: true},
			}
			for _, pattern := range httpPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register HTTP dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			endpointFactory, err := http.NewEndpointFactory(funcs)
			if err != nil {
				return ctx, fmt.Errorf("failed to create endpoint factory: %w", err)
			}

			staticFactory, err := http.NewStaticFactory(fsRegistry)
			if err != nil {
				return ctx, fmt.Errorf("failed to create static factory: %w", err)
			}

			relayManager := websocketrelay.NewWebSocketRelay(ctx, logger.Named("ws"))

			midFactory := http.NewDefaultMiddlewareFactory(
				http.WithLogger(logger.Named("http.md")),
				http.WithMiddlewareCreator(cors.MiddlewareName, cors.CreateCORSMiddleware),
				http.WithMiddlewareCreator(realip.MiddlewareName, realip.CreateRealIPMiddleware),
				http.WithMiddlewareCreator("websocket_relay", relayManager.CreateMiddleware),
				http.WithMiddlewareCreator(tokenstore.MiddlewareName, tokenstore.CreateTokenAuthMiddleware),
				http.WithMiddlewareCreator(firewall.ResourceMiddlewareName, firewall.CreateResourceFirewallMiddleware),
				http.WithMiddlewareCreator(firewall.EndpointMiddlewareName, firewall.CreateEndpointFirewallMiddleware),
			)

			manager, err := http.NewManager(
				dtt,
				bus,
				http.NewServerFactory(midFactory),
				endpointFactory,
				staticFactory,
				logger.Named("http"),
			)
			if err != nil {
				return ctx, fmt.Errorf("failed to create http manager: %w", err)
			}

			handlers.RegisterListener("http.*", manager)
			return ctx, nil
		},
	})
}
