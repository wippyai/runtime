package context

import "net/http"

type HttpContextCarrier struct {
	r *http.Request
	w http.ResponseWriter
}

func (h *HttpContextCarrier) Request() *http.Request {
	return h.r
}

func (h *HttpContextCarrier) ResponseWriter() http.ResponseWriter {
	return h.w
}
