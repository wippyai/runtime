package http

import (
	"github.com/ponyruntime/pony/api/context"
	"net/http"
)

const (
	RouteInfoCtx = context.Key("routeInfo")   //nolint:gochecknoglobals
	RequestCtx   = context.Key("httpRequest") //nolint:gochecknoglobals
)

type RouteInfo struct {
	Params     map[string]string
	Endpoint   EndpointConfig
	MatchedURI string
	EndpointID string
}

// ----- this is module specific part

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
