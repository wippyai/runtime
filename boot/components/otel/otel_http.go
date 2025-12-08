package otel

import (
	"context"
	"net/http"

	"github.com/wippyai/runtime/api/boot"
	httpapi "github.com/wippyai/runtime/api/service/http"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

func HTTP() boot.Component {
	return boot.New(boot.P{
		Name:      OTelHTTPName,
		DependsOn: []boot.Name{OTelName, httpName},
		Load: func(ctx context.Context) (context.Context, error) {
			middlewareRegistry := httpapi.GetMiddlewareRegistry(ctx)
			if middlewareRegistry == nil {
				return ctx, ErrHTTPMiddlewareNotAvailable
			}

			svc := otelapi.GetService(ctx)
			if svc == nil {
				// Register no-op middleware when OTEL is disabled
				if err := middlewareRegistry.Register("otel", func(_ map[string]string) func(http.Handler) http.Handler {
					return func(next http.Handler) http.Handler {
						return next
					}
				}); err != nil {
					return ctx, err
				}
			} else {
				// Register actual OTEL middleware when enabled
				if err := middlewareRegistry.Register("otel", func(_ map[string]string) func(http.Handler) http.Handler {
					return svc.HTTPMiddleware()
				}); err != nil {
					return ctx, err
				}
			}

			return ctx, nil
		},
		Start: func(_ context.Context) error {
			return nil
		},
		Stop: func(_ context.Context) error {
			return nil
		},
	})
}
