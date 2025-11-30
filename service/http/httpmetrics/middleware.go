package httpmetrics

import (
	"net/http"
	"strconv"
	"time"

	"github.com/wippyai/runtime/api/metrics"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

const (
	MiddlewareName   = "metrics"
	RequestsTotal    = "wippy_http_requests_total"
	RequestDuration  = "wippy_http_request_duration_seconds"
	RequestsInFlight = "wippy_http_requests_in_flight"
)

func CreateHTTPMetricsMiddleware(collector metrics.Collector) func(options map[string]string) func(http.Handler) http.Handler {
	return func(_ map[string]string) func(http.Handler) http.Handler {
		return func(next http.Handler) http.Handler {
			if collector == nil {
				return next
			}
			return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				start := time.Now()

				// Read route ONCE at start, store in local variable
				route := "unmatched"
				if label, ok := httpapi.GetRouteLabel(r.Context()); ok && label != "" {
					route = label
				}

				sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

				collector.GaugeInc(RequestsInFlight, metrics.Labels{
					"method": r.Method,
					"route":  route,
				})
				defer collector.GaugeDec(RequestsInFlight, metrics.Labels{
					"method": r.Method,
					"route":  route,
				})

				next.ServeHTTP(sw, r)

				duration := time.Since(start).Seconds()
				collector.CounterInc(RequestsTotal, metrics.Labels{
					"method": r.Method,
					"route":  route,
					"status": strconv.Itoa(sw.statusCode),
				})
				collector.HistogramObserve(RequestDuration, duration, metrics.Labels{
					"method": r.Method,
					"route":  route,
					"status": strconv.Itoa(sw.statusCode),
				})
			})
		}
	}
}

type statusWriter struct {
	http.ResponseWriter
	statusCode int
}

func (w *statusWriter) WriteHeader(code int) {
	w.statusCode = code
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusWriter) Flush() {
	if flusher, ok := w.ResponseWriter.(http.Flusher); ok {
		flusher.Flush()
	}
}
