package context

import "net/http"

type HTTPContextCarrier struct {
	r *http.Request
	w http.ResponseWriter
}

// NewHTTPContextCarrier creates a new HttpContextCarrier, after setting the request and response writer, they are real-only
func NewHTTPContextCarrier(r *http.Request, w http.ResponseWriter) *HTTPContextCarrier {
	return &HTTPContextCarrier{
		r: r,
		w: w,
	}
}

func (h *HTTPContextCarrier) Request() *http.Request {
	return h.r
}

func (h *HTTPContextCarrier) ResponseWriter() http.ResponseWriter {
	return h.w
}
