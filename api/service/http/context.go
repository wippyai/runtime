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
	RequestCtx        = &ctxapi.Key{Name: "http.request", Scope: ctxapi.ScopeThread}
	RouteCtx          = &ctxapi.Key{Name: "http.route", Scope: ctxapi.ScopeThread}
	ContextServerID   = &ctxapi.Key{Name: "http.server_id", Scope: ctxapi.ScopeThread}
	EndpointConfigCtx = &ctxapi.Key{Name: "http.endpoint_config", Scope: ctxapi.ScopeThread}
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

// GetRouteInfo retrieves route information from the context
func GetRouteInfo(ctx context.Context) (*RouteInfo, bool) {
	info, ok := ctx.Value(RouteCtx).(*RouteInfo)
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
