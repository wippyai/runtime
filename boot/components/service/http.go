// SPDX-License-Identifier: MPL-2.0

package service

import (
	"context"
	nethttp "net/http"

	"github.com/wippyai/runtime/api/boot"
	"github.com/wippyai/runtime/api/event"
	fsapi "github.com/wippyai/runtime/api/fs"
	funcapi "github.com/wippyai/runtime/api/function"
	logapi "github.com/wippyai/runtime/api/logs"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	"github.com/wippyai/runtime/api/payload"
	"github.com/wippyai/runtime/api/process"
	regapi "github.com/wippyai/runtime/api/registry"
	httpapi "github.com/wippyai/runtime/api/service/http"
	bootpkg "github.com/wippyai/runtime/boot"
	bootcore "github.com/wippyai/runtime/boot/components/core"
	bootsystem "github.com/wippyai/runtime/boot/components/system"
	"github.com/wippyai/runtime/service/http"
	"github.com/wippyai/runtime/service/http/middleware/compress"
	"github.com/wippyai/runtime/service/http/middleware/cors"
	"github.com/wippyai/runtime/service/http/middleware/fileserve"
	"github.com/wippyai/runtime/service/http/middleware/firewall"
	"github.com/wippyai/runtime/service/http/middleware/httpmetrics"
	"github.com/wippyai/runtime/service/http/middleware/ratelimit"
	"github.com/wippyai/runtime/service/http/middleware/realip"
	"github.com/wippyai/runtime/service/http/middleware/sserelay"
	"github.com/wippyai/runtime/service/http/middleware/wsrelay"
	"github.com/wippyai/runtime/service/security/tokenstore"
	"go.uber.org/zap"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      HTTPName,
		DependsOn: []boot.Name{bootcore.RegistryName, bootsystem.FunctionsName, bootsystem.FilesystemName},
		Load: func(ctx context.Context) (context.Context, error) {
			logger := logapi.GetLogger(ctx).Named("http")
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

			funcs := funcapi.GetRegistry(ctx)
			if funcs == nil {
				return ctx, ErrFunctionRegistryNotAvailable
			}

			fsRegistry := fsapi.GetRegistry(ctx)
			if fsRegistry == nil {
				return ctx, ErrFilesystemRegistryNotAvailable
			}

			handlers := bootpkg.GetHandlerRegistry(ctx)
			if handlers == nil {
				return ctx, ErrHandlerRegistryNotAvailable
			}

			reg := regapi.GetRegistry(ctx)
			if reg == nil {
				return ctx, ErrRegistryNotAvailable
			}

			pidGen := process.GetPIDGenerator(ctx)

			// Register HTTP dependency patterns
			httpPatterns := []regapi.DependencyPattern{
				{Path: "meta.server", Description: "Reference to HTTP server in metadata"},
				{Path: "meta.router", Description: "Reference to router component in metadata"},
				{Path: "data.server", Description: "Reference to HTTP server in data section"},
				{Path: "data.middleware", Description: "List of middleware components", AllowWildcard: true},
				{Path: "data.post_middleware", Description: "List of post-middleware components", AllowWildcard: true},
				{Path: "data.network", Description: "Overlay network backing this HTTP service"},
				{Path: "data.tls.*_env", Description: "Env variables referenced from TLS config", AllowWildcard: true},
			}
			for _, pattern := range httpPatterns {
				if err := reg.RegisterDependencyPattern(pattern); err != nil {
					logger.Warn("failed to register HTTP dependency pattern", zap.String("path", pattern.Path), zap.Error(err))
				}
			}

			endpointFactory, err := http.NewEndpointFactory(funcs)
			if err != nil {
				return ctx, NewEndpointFactoryError(err)
			}

			relayManager := wsrelay.NewWebSocketRelay(ctx, logger.Named("ws"), pidGen)
			sseRelayManager := sserelay.NewSSERelay(ctx, logger.Named("sse"), pidGen)

			midRegistry := http.NewMiddlewareRegistry(logger.Named("mw"))

			// Register built-in middleware
			_ = midRegistry.Register(cors.MiddlewareName, cors.CreateCORSMiddleware)
			_ = midRegistry.Register(realip.MiddlewareName, realip.CreateRealIPMiddleware)
			_ = midRegistry.Register("websocket_relay", relayManager.CreateMiddleware)
			_ = midRegistry.Register(sserelay.MiddlewareName, sseRelayManager.CreateMiddleware)
			_ = midRegistry.Register(tokenstore.MiddlewareName, tokenstore.CreateTokenAuthMiddleware)
			_ = midRegistry.Register(firewall.ResourceMiddlewareName, firewall.CreateResourceFirewallMiddleware)
			_ = midRegistry.Register(firewall.EndpointMiddlewareName, firewall.CreateEndpointFirewallMiddleware)

			// Register new middleware
			_ = midRegistry.Register(compress.MiddlewareName, compress.CreateCompressMiddleware)
			rateLimitManager := ratelimit.NewManager(ctx)
			_ = midRegistry.Register(ratelimit.MiddlewareName, rateLimitManager.CreateMiddleware)
			_ = midRegistry.Register(fileserve.MiddlewareName, func(options map[string]string) func(nethttp.Handler) nethttp.Handler {
				return fileserve.CreateFileServeMiddleware(options, fsRegistry)
			})

			// Register metrics middleware if collector available
			collector := metricsapi.GetCollector(ctx)
			if collector != nil {
				_ = midRegistry.Register(httpmetrics.MiddlewareName, httpmetrics.CreateHTTPMetricsMiddleware(collector))
			}

			// Store registry in context for other components
			ctx = httpapi.WithMiddlewareRegistry(ctx, midRegistry)

			// Create static factory with middleware support (after middleware registry is set up)
			staticFactory, err := http.NewStaticFactory(fsRegistry, midRegistry)
			if err != nil {
				return ctx, NewStaticFactoryError(err)
			}

			manager, err := http.NewManager(
				dtt,
				bus,
				http.NewServerFactory(midRegistry),
				endpointFactory,
				staticFactory,
				logger,
			)
			if err != nil {
				return ctx, NewHTTPManagerError(err)
			}

			handlers.RegisterListener("http.*", manager)
			return ctx, nil
		},
	})
}
