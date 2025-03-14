package websocket_relay

import (
	"fmt"
	"net/http"
)

// responseWrapper wraps the ResponseWriter to capture headers
type responseWrapper struct {
	http.ResponseWriter
	headers http.Header
}

func newResponseWrapper(w http.ResponseWriter) *responseWrapper {
	return &responseWrapper{
		ResponseWriter: w,
		headers:        w.Header(),
	}
}

func (rw *responseWrapper) Header() http.Header {
	return rw.headers
}

func (rw *responseWrapper) Write(data []byte) (int, error) {
	// Capture the response body if needed
	return rw.ResponseWriter.Write(data)
}

func (rw *responseWrapper) WriteHeader(statusCode int) {
	rw.headers.Set(WSRelayHeader, fmt.Sprintf("%d", statusCode))
	rw.ResponseWriter.WriteHeader(statusCode)
}

func (rw *responseWrapper) Flush() {
	if flusher, ok := rw.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
