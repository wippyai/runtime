package otel

import (
	"context"
	"fmt"
	"net/http"

	"github.com/wippyai/runtime/api/boot"
	httpapi "github.com/wippyai/runtime/api/service/http"
	otelapi "github.com/wippyai/runtime/api/service/otel"
)

func OTelHTTP() boot.Component {
	return boot.New(boot.P{
		Name:      OTelHTTPName,
		DependsOn: []boot.ComponentName{OTelName, httpName},
		Load: func(ctx context.Context) (context.Context, error) {
			svc := otelapi.GetService(ctx)
			if svc == nil {
				return ctx, nil
			}

			middlewareRegistry := httpapi.GetMiddlewareRegistry(ctx)
			if middlewareRegistry == nil {
				return ctx, fmt.Errorf("HTTP middleware registry not available in context")
			}

			if err := middlewareRegistry.Register("otel", func(_ map[string]string) func(http.Handler) http.Handler {
				return svc.HTTPMiddleware()
			}); err != nil {
				return ctx, err
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
