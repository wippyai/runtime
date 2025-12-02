package otel

import (
	"context"
	"net/http"

	ctxapi "github.com/wippyai/runtime/api/context"
	apiinterceptor "github.com/wippyai/runtime/api/function"
	"github.com/wippyai/runtime/api/process"
	queueapi "github.com/wippyai/runtime/api/queue"
)

var serviceCtx = &ctxapi.Key{Name: "otel.service"}

// Service provides OpenTelemetry tracing capabilities
type Service interface {
	process.Lifecycle

	// HTTPMiddleware returns HTTP middleware for W3C trace context propagation
	HTTPMiddleware() func(http.Handler) http.Handler

	// Interceptor returns function call interceptor for tracing
	Interceptor() apiinterceptor.Interceptor

	// QueuePublishInterceptor returns PublishInterceptor for queue message tracing
	QueuePublishInterceptor() queueapi.PublishInterceptor
}

// WithService stores OTEL service in AppContext
func WithService(ctx context.Context, service Service) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(serviceCtx) == nil {
		ac.With(serviceCtx, service)
	}
	return ctx
}

// GetService retrieves OTEL service from AppContext
func GetService(ctx context.Context) Service {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(serviceCtx); val != nil {
		if service, ok := val.(Service); ok {
			return service
		}
	}
	return nil
}
