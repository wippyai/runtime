// Package http provides HTTP service configuration.
package http

import (
	"context"
	"net/http"

	"github.com/wippyai/runtime/api/registry"

	ctxapi "github.com/wippyai/runtime/api/context"
)

// Context keys for storing HTTP-specific values in the request context
var (
	requestCtx         = &ctxapi.Key{Name: "http.request"}
	routeCtx           = &ctxapi.Key{Name: "http.route"}
	routeLabelCtx      = &ctxapi.Key{Name: "http.route_label"}
	serverIDCtx        = &ctxapi.Key{Name: "http.server_id"}
	serverHostCtx      = &ctxapi.Key{Name: "http.server.host"}
	serverCtx          = &ctxapi.Key{Name: "http.server"}
	middlewareRegistry = &ctxapi.Key{Name: "http.middleware_registry"}
)

// RequestCtxKey returns the context key for HTTP request context.
// Used when passing request context via Task.Context pairs.
func RequestCtxKey() *ctxapi.Key {
	return requestCtx
}

// ServerIDCtxKey returns the context key for server ID.
// Used when passing server ID via Task.Context pairs.
func ServerIDCtxKey() *ctxapi.Key {
	return serverIDCtx
}

// ServerCtxKey returns the context key for the HTTP server object.
// Used for WebSocket relay attachment.
func ServerCtxKey() *ctxapi.Key {
	return serverCtx
}

// RouteInfo contains information about the matched route for the current request.
// It includes routing parameters, endpoint configuration, and matching details.
type RouteInfo struct {
	Params     map[string]string // URL parameters extracted from the route
	Endpoint   registry.ID       // ID of the matched endpoint configuration
	Func       registry.ID       // Identifier for the function to be called
	MatchedURI string            // The URI pattern that matched the request
}

// RequestContext wraps an HTTP request and response writer with additional
// functionality for handling HTTP responses in the service.
type RequestContext struct {
	r               *http.Request
	w               http.ResponseWriter
	responseHandled bool
}

// GetRequestContext retrieves HTTP request context from FrameContext
func GetRequestContext(ctx context.Context) (*RequestContext, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	val, ok := fc.Get(requestCtx)
	if !ok {
		return nil, false
	}
	reqCtx, ok := val.(*RequestContext)
	return reqCtx, ok
}

// GetRouteInfo retrieves route information from FrameContext
func GetRouteInfo(ctx context.Context) (*RouteInfo, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil, false
	}
	val, ok := fc.Get(routeCtx)
	if !ok {
		return nil, false
	}
	info, ok := val.(*RouteInfo)
	return info, ok
}

// GetRouteLabel retrieves the immutable route label string from FrameContext.
// This is safe to use throughout request lifecycle including in defers.
// Returns empty string and false if not available.
func GetRouteLabel(ctx context.Context) (string, bool) {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return "", false
	}
	val, ok := fc.Get(routeLabelCtx)
	if !ok {
		return "", false
	}
	label, ok := val.(string)
	return label, ok
}

// SetRouteInfo stores route information in FrameContext.
func SetRouteInfo(ctx context.Context, info *RouteInfo) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(routeCtx, info)
}

// SetRouteLabel stores the route label string in FrameContext.
func SetRouteLabel(ctx context.Context, label string) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(routeLabelCtx, label)
}

// SetServerID stores the server ID in FrameContext.
func SetServerID(ctx context.Context, serverID string) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(serverIDCtx, serverID)
}

// SetServerHost stores the server host in FrameContext.
func SetServerHost(ctx context.Context, host string) error {
	fc := ctxapi.FrameFromContext(ctx)
	if fc == nil {
		return nil
	}
	return fc.Set(serverHostCtx, host)
}

// NewRequestContext creates a new RequestContext instance with the provided
// HTTP request and response writer.
func NewRequestContext(r *http.Request, w http.ResponseWriter) *RequestContext {
	return &RequestContext{r: r, w: w}
}

// Request returns the underlying HTTP request.
func (h *RequestContext) Request() *http.Request {
	return h.r
}

// ResponseWriter returns the underlying HTTP response writer.
func (h *RequestContext) ResponseWriter() http.ResponseWriter {
	return h.w
}

// MarkHandled indicates that a response has been sent for this request.
func (h *RequestContext) MarkHandled() {
	h.responseHandled = true
}

// ResponseHandled returns true if a response has already been sent for this request.
func (h *RequestContext) ResponseHandled() bool {
	return h.responseHandled
}

// SetRequest sets the HTTP request (for pooling)
func (h *RequestContext) SetRequest(r *http.Request) {
	h.r = r
}

// SetResponseWriter sets the HTTP response writer (for pooling)
func (h *RequestContext) SetResponseWriter(w http.ResponseWriter) {
	h.w = w
}

// ResetHandled resets the response handled flag (for pooling)
func (h *RequestContext) ResetHandled() {
	h.responseHandled = false
}

// MiddlewareCreator is a function that creates a middleware handler from options
type MiddlewareCreator = func(options map[string]string) func(http.Handler) http.Handler

// MiddlewareRegistry defines the interface for HTTP middleware registration
type MiddlewareRegistry interface {
	Register(name string, creator MiddlewareCreator) error
	Unregister(name string) error
	CreateMiddleware(name string, options map[string]string) (func(http.Handler) http.Handler, error)
}

// WithMiddlewareRegistry adds the middleware registry to the context
func WithMiddlewareRegistry(ctx context.Context, registry MiddlewareRegistry) context.Context {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return ctx
	}
	if ac.Get(middlewareRegistry) == nil {
		ac.With(middlewareRegistry, registry)
	}
	return ctx
}

// GetMiddlewareRegistry retrieves the middleware registry from the context
func GetMiddlewareRegistry(ctx context.Context) MiddlewareRegistry {
	ac := ctxapi.AppFromContext(ctx)
	if ac == nil {
		return nil
	}
	if val := ac.Get(middlewareRegistry); val != nil {
		if registry, ok := val.(MiddlewareRegistry); ok {
			return registry
		}
	}
	return nil
}
