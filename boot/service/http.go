package service

import (
	"context"
	"fmt"

	"github.com/ponyruntime/pony/api/boot"
	"github.com/ponyruntime/pony/api/event"
	fsapi "github.com/ponyruntime/pony/api/fs"
	funcapi "github.com/ponyruntime/pony/api/function"
	logapi "github.com/ponyruntime/pony/api/logs"
	"github.com/ponyruntime/pony/api/payload"
	bootpkg "github.com/ponyruntime/pony/boot"
	"github.com/ponyruntime/pony/service/http"
	"github.com/ponyruntime/pony/service/http/cors"
	"github.com/ponyruntime/pony/service/http/firewall"
	"github.com/ponyruntime/pony/service/http/realip"
	"github.com/ponyruntime/pony/service/http/websocketrelay"
	"github.com/ponyruntime/pony/service/tokenstore"
	reghandler "github.com/ponyruntime/pony/system/registry/events"
)

func init() {
	bootpkg.MustRegister(bootpkg.NewService(bootpkg.ServiceP{
		Name:      bootpkg.HTTP,
		Phase:     boot.PostInit,
		DependsOn: []string{bootpkg.Functions, bootpkg.Filesystem},
		Load: func(ctx context.Context) (context.Context, interface{}, error) {
			logger := logapi.GetLogger(ctx)
			dtt := payload.GetTranscoder(ctx)
			bus := event.GetBus(ctx)
			funcs := funcapi.GetRegistry(ctx)
			fsRegistry := fsapi.GetRegistry(ctx)

			endpointFactory, err := http.NewEndpointFactory(funcs)
			if err != nil {
				return ctx, nil, fmt.Errorf("failed to create endpoint factory: %w", err)
			}

			staticFactory, err := http.NewStaticFactory(fsRegistry)
			if err != nil {
				return ctx, nil, fmt.Errorf("failed to create static factory: %w", err)
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
				return ctx, nil, fmt.Errorf("failed to create http manager: %w", err)
			}

			handler := reghandler.NewRegistryHandler("http.*", manager)
			return ctx, handler, nil
		},
	}))
}
