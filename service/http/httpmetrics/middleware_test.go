package httpmetrics

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"

	"github.com/stretchr/testify/assert"
	ctxapi "github.com/wippyai/runtime/api/context"
	"github.com/wippyai/runtime/api/metrics"
	httpapi "github.com/wippyai/runtime/api/service/http"
)

type mockCollector struct {
	mu         sync.Mutex
	counters   map[string]float64
	gauges     map[string]float64
	histograms map[string][]float64
}

func newMockCollector() *mockCollector {
	return &mockCollector{
		counters:   make(map[string]float64),
		gauges:     make(map[string]float64),
		histograms: make(map[string][]float64),
	}
}

func (m *mockCollector) CounterAdd(name string, value float64, _ metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.counters[name] += value
}

func (m *mockCollector) CounterInc(name string, labels metrics.Labels) {
	m.CounterAdd(name, 1, labels)
}

func (m *mockCollector) GaugeSet(name string, value float64, _ metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name] = value
}

func (m *mockCollector) GaugeInc(name string, _ metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name]++
}

func (m *mockCollector) GaugeDec(name string, _ metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.gauges[name]--
}

func (m *mockCollector) HistogramObserve(name string, value float64, _ metrics.Labels) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.histograms[name] = append(m.histograms[name], value)
}

func (m *mockCollector) RegisterExporter(_ metrics.Exporter) error {
	return nil
}

func (m *mockCollector) Close() error {
	return nil
}

func (m *mockCollector) getCounter(name string) float64 { //nolint:unparam
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.counters[name]
}

func (m *mockCollector) getGauge(name string) float64 {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.gauges[name]
}

func (m *mockCollector) getHistogramCount(name string) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	return len(m.histograms[name])
}

func TestCreateHTTPMetricsMiddleware(t *testing.T) {
	t.Run("records request metrics", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, float64(1), collector.getCounter(RequestsTotal))
		assert.Equal(t, float64(0), collector.getGauge(RequestsInFlight))
		assert.Equal(t, 1, collector.getHistogramCount(RequestDuration))
	})

	t.Run("captures different status codes", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		statusCodes := []int{http.StatusOK, http.StatusNotFound, http.StatusInternalServerError}
		for _, code := range statusCodes {
			handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(code)
			}))

			req := httptest.NewRequest("GET", "/test", nil)
			w := httptest.NewRecorder()
			handler.ServeHTTP(w, req)

			assert.Equal(t, code, w.Code)
		}

		assert.Equal(t, float64(3), collector.getCounter(RequestsTotal))
	})

	t.Run("in-flight gauge increments and decrements", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()
		handler.ServeHTTP(w, req)

		// After completion, in-flight should be 0
		assert.Equal(t, float64(0), collector.getGauge(RequestsInFlight))
	})

	t.Run("uses route label from context", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/api/users/123", nil)
		ctx, fc := ctxapi.OpenFrameContext(req.Context())
		_ = fc.Set(httpapi.RouteLabelCtx, "/api/users/{id}")
		req = req.WithContext(ctx)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, float64(1), collector.getCounter(RequestsTotal))
	})

	t.Run("unmatched route label for requests without route info", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/unknown", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
		assert.Equal(t, float64(1), collector.getCounter(RequestsTotal))
	})

	t.Run("records duration accurately", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, 1, collector.getHistogramCount(RequestDuration))
	})

	t.Run("concurrent requests", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusOK)
		}))

		var wg sync.WaitGroup
		for i := 0; i < 100; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				req := httptest.NewRequest("GET", "/test", nil)
				w := httptest.NewRecorder()
				handler.ServeHTTP(w, req)
			}()
		}
		wg.Wait()

		assert.Equal(t, float64(100), collector.getCounter(RequestsTotal))
		assert.Equal(t, float64(0), collector.getGauge(RequestsInFlight))
		assert.Equal(t, 100, collector.getHistogramCount(RequestDuration))
	})

	t.Run("captures status from WriteHeader", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			w.WriteHeader(http.StatusCreated)
		}))

		req := httptest.NewRequest("POST", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusCreated, w.Code)
	})

	t.Run("default status OK when WriteHeader not called", func(t *testing.T) {
		collector := newMockCollector()
		middlewareFactory := CreateHTTPMetricsMiddleware(collector)
		middleware := middlewareFactory(nil)

		handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
			_, _ = w.Write([]byte("OK"))
		}))

		req := httptest.NewRequest("GET", "/test", nil)
		w := httptest.NewRecorder()

		handler.ServeHTTP(w, req)

		assert.Equal(t, http.StatusOK, w.Code)
	})
}

func TestStatusWriter(t *testing.T) {
	t.Run("captures status code", func(t *testing.T) {
		w := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

		sw.WriteHeader(http.StatusNotFound)

		assert.Equal(t, http.StatusNotFound, sw.statusCode)
		assert.Equal(t, http.StatusNotFound, w.Code)
	})

	t.Run("default status code is OK", func(t *testing.T) {
		w := httptest.NewRecorder()
		sw := &statusWriter{ResponseWriter: w, statusCode: http.StatusOK}

		assert.Equal(t, http.StatusOK, sw.statusCode)
	})
}

// Ensure interface compliance
func TestInterfaceCompliance(_ *testing.T) {
	collector := newMockCollector()
	_ = metrics.Collector(collector)
}

func TestWithContextCancel(t *testing.T) {
	collector := newMockCollector()
	middlewareFactory := CreateHTTPMetricsMiddleware(collector)
	middleware := middlewareFactory(nil)

	handler := middleware(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		select {
		case <-r.Context().Done():
			return
		default:
			w.WriteHeader(http.StatusOK)
		}
	}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	req := httptest.NewRequest("GET", "/test", nil)
	req = req.WithContext(ctx)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, float64(1), collector.getCounter(RequestsTotal))
}
