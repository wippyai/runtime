package http

import (
	"github.com/ponyruntime/pony/api/context"
	"net/http"
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
	r *http.Request
	w http.ResponseWriter
}

func (h *RequestContext) Request() *http.Request {
	return h.r
}

func (h *RequestContext) ResponseWriter() http.ResponseWriter {
	return h.w
}
