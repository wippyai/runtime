package http

import (
	"net/http"

	"github.com/ponyruntime/pony/api/context"
)

var (
	RequestCtx = &context.Key{Name: "http.request"} //nolint:gochecknoglobals
	RouteCtx   = &context.Key{Name: "http.route"}   //nolint:gochecknoglobals
)

type RouteInfo struct {
	Params     map[string]string
	Endpoint   EndpointConfig
	MatchedURI string
	EndpointID string
}

type RequestContext struct {
	r               *http.Request
	w               http.ResponseWriter
	responseHandled bool
}

func NewRequestContext(r *http.Request, w http.ResponseWriter) *RequestContext {
	return &RequestContext{r: r, w: w}
}

func (h *RequestContext) Request() *http.Request {
	return h.r
}

func (h *RequestContext) ResponseWriter() http.ResponseWriter {
	return h.w
}

func (h *RequestContext) MarkHandled() {
	h.responseHandled = true
}

func (h *RequestContext) ResponseHandled() bool {
	return h.responseHandled
}
