package http

import (
	"net/http"

	"github.com/ponyruntime/pony/api/context"
)

// Context keys for storing HTTP-specific values in the request context
var (
	// RequestCtx is the context key for storing the HTTP request context
	RequestCtx = &context.Key{Name: "http.request"} //nolint:gochecknoglobals
	// RouteCtx is the context key for storing the current route information
	RouteCtx = &context.Key{Name: "http.route"} //nolint:gochecknoglobals
)

// RouteInfo contains information about the matched route for the current request.
// It includes routing parameters, endpoint configuration, and matching details.
type RouteInfo struct {
	Params     map[string]string // URL parameters extracted from the route
	Endpoint   EndpointConfig    // Configuration for the matched endpoint
	MatchedURI string            // The URI pattern that matched the request
	EndpointID string            // Unique identifier for the endpoint
}

// RequestContext wraps an HTTP request and response writer with additional
// functionality for handling HTTP responses in the service.
type RequestContext struct {
	r               *http.Request
	w               http.ResponseWriter
	responseHandled bool
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
