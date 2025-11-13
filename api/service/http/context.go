// Package http provides HTTP service configuration.
package http

import (
	context "context"
	"net/http"

	"github.com/ponyruntime/pony/api/registry"

	ctxapi "github.com/ponyruntime/pony/api/context"
)

// Context keys for storing HTTP-specific values in the request context
var (
	// todo: privatize
	RequestCtx        = &ctxapi.Key{Name: "http.request"}
	RouteCtx          = &ctxapi.Key{Name: "http.route"}
	ContextServerID   = &ctxapi.Key{Name: "http.server_id"}
	ContextHost       = &ctxapi.Key{Name: "http.server.host"}
	EndpointConfigCtx = &ctxapi.Key{Name: "http.endpoint_config"}
)

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
	val, ok := fc.Get(RequestCtx)
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
	val, ok := fc.Get(RouteCtx)
	if !ok {
		return nil, false
	}
	info, ok := val.(*RouteInfo)
	return info, ok
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
