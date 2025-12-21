package prometheus

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/metrics"
)

func TestNewExporter(t *testing.T) {
	e := NewExporter()
	require.NotNil(t, e)
	assert.NotNil(t, e.registry)
	assert.NotNil(t, e.counters)
	assert.NotNil(t, e.gauges)
	assert.NotNil(t, e.histograms)
}

func TestExporter_Name(t *testing.T) {
	e := NewExporter()
	assert.Equal(t, "prometheus", e.Name())
}

func TestExporter_RecordCounter(t *testing.T) {
	e := NewExporter()

	err := e.Record("test_counter", api.TypeCounter, 1.0, nil)
	require.NoError(t, err)

	err = e.Record("test_counter", api.TypeCounter, 2.0, nil)
	require.NoError(t, err)

	assert.Len(t, e.counters, 1)
}

func TestExporter_RecordCounterWithLabels(t *testing.T) {
	e := NewExporter()

	labels := api.Labels{"env": "test", "service": "api"}
	err := e.Record("requests_total", api.TypeCounter, 1.0, labels)
	require.NoError(t, err)

	assert.Len(t, e.counters, 1)
}

func TestExporter_RecordGauge(t *testing.T) {
	e := NewExporter()

	err := e.Record("test_gauge", api.TypeGauge, 42.0, nil)
	require.NoError(t, err)

	err = e.Record("test_gauge", api.TypeGauge, 100.0, nil)
	require.NoError(t, err)

	assert.Len(t, e.gauges, 1)
}

func TestExporter_RecordGaugeWithLabels(t *testing.T) {
	e := NewExporter()

	labels := api.Labels{"host": "server1"}
	err := e.Record("memory_usage", api.TypeGauge, 1024.0, labels)
	require.NoError(t, err)

	assert.Len(t, e.gauges, 1)
}

func TestExporter_RecordHistogram(t *testing.T) {
	e := NewExporter()

	err := e.Record("request_duration", api.TypeHistogram, 0.5, nil)
	require.NoError(t, err)

	err = e.Record("request_duration", api.TypeHistogram, 1.2, nil)
	require.NoError(t, err)

	assert.Len(t, e.histograms, 1)
}

func TestExporter_RecordHistogramWithLabels(t *testing.T) {
	e := NewExporter()

	labels := api.Labels{"method": "GET", "path": "/api"}
	err := e.Record("http_duration", api.TypeHistogram, 0.05, labels)
	require.NoError(t, err)

	assert.Len(t, e.histograms, 1)
}

func TestExporter_Handler(t *testing.T) {
	e := NewExporter()

	_ = e.Record("test_counter", api.TypeCounter, 5.0, nil)
	_ = e.Record("test_gauge", api.TypeGauge, 10.0, nil)

	handler := e.Handler()
	require.NotNil(t, handler)

	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	assert.Equal(t, 200, rec.Code)
	body := rec.Body.String()
	assert.Contains(t, body, "test_counter")
	assert.Contains(t, body, "test_gauge")
}

func TestExporter_Close(t *testing.T) {
	e := NewExporter()
	err := e.Close()
	assert.NoError(t, err)
}

func TestExporter_ConcurrentRecords(t *testing.T) {
	e := NewExporter()

	done := make(chan bool)
	for i := 0; i < 10; i++ {
		go func(id int) {
			for j := 0; j < 100; j++ {
				labels := api.Labels{"worker": string(rune('0' + id))}
				_ = e.Record("concurrent_counter", api.TypeCounter, 1.0, labels)
				_ = e.Record("concurrent_gauge", api.TypeGauge, float64(j), labels)
			}
			done <- true
		}(i)
	}

	for i := 0; i < 10; i++ {
		<-done
	}

	assert.NotEmpty(t, e.counters)
	assert.NotEmpty(t, e.gauges)
}

func TestBuildMetricKey(t *testing.T) {
	tests := []struct {
		name       string
		metricName string
		labels     []string
		expected   string
	}{
		{"no labels", "metric", nil, "metric"},
		{"empty labels", "metric", []string{}, "metric"},
		{"one label", "metric", []string{"env"}, "metric{env}"},
		{"multiple labels", "metric", []string{"env", "host"}, "metric{env,host}"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := buildMetricKey(tt.metricName, tt.labels)
			assert.Equal(t, tt.expected, result)
		})
	}
}

func TestAcquireSortedLabelKeys(t *testing.T) {
	t.Run("nil labels", func(t *testing.T) {
		result := acquireSortedLabelKeys(nil)
		assert.Nil(t, result)
	})

	t.Run("empty labels", func(t *testing.T) {
		result := acquireSortedLabelKeys(api.Labels{})
		assert.Nil(t, result)
	})

	t.Run("sorted keys", func(t *testing.T) {
		labels := api.Labels{"z": "1", "a": "2", "m": "3"}
		result := acquireSortedLabelKeys(labels)
		require.NotNil(t, result)
		assert.Equal(t, []string{"a", "m", "z"}, *result)
		releaseLabelSlice(result)
	})
}

func TestAcquireLabelVals(t *testing.T) {
	t.Run("nil keys", func(t *testing.T) {
		result := acquireLabelVals(api.Labels{"a": "1"}, nil)
		assert.Nil(t, result)
	})

	t.Run("empty keys", func(t *testing.T) {
		keys := []string{}
		result := acquireLabelVals(api.Labels{"a": "1"}, &keys)
		assert.Nil(t, result)
	})

	t.Run("values in order", func(t *testing.T) {
		labels := api.Labels{"a": "val_a", "b": "val_b"}
		keys := []string{"a", "b"}
		result := acquireLabelVals(labels, &keys)
		require.NotNil(t, result)
		assert.Equal(t, []string{"val_a", "val_b"}, *result)
		releaseLabelValsSlice(result)
	})
}

func TestReleaseLabelSlice(t *testing.T) {
	t.Run("nil slice", func(t *testing.T) {
		releaseLabelSlice(nil)
	})

	t.Run("small slice returned to pool", func(t *testing.T) {
		s := make([]string, 0, 4)
		s = append(s, "a", "b")
		releaseLabelSlice(&s)
	})

	t.Run("large slice not returned", func(t *testing.T) {
		s := make([]string, 0, 16)
		releaseLabelSlice(&s)
	})
}

func TestExporter_SameLabelsDifferentValues(t *testing.T) {
	e := NewExporter()

	err := e.Record("metric", api.TypeCounter, 1.0, api.Labels{"env": "prod"})
	require.NoError(t, err)

	err = e.Record("metric", api.TypeCounter, 1.0, api.Labels{"env": "staging"})
	require.NoError(t, err)

	assert.Len(t, e.counters, 1)
}

func TestExporter_HandlerOutputFormat(t *testing.T) {
	e := NewExporter()

	labels := api.Labels{"method": "GET"}
	_ = e.Record("http_requests_total", api.TypeCounter, 10.0, labels)

	handler := e.Handler()
	req := httptest.NewRequest("GET", "/metrics", nil)
	rec := httptest.NewRecorder()
	handler.ServeHTTP(rec, req)

	body := rec.Body.String()
	assert.True(t, strings.Contains(body, "http_requests_total"))
	assert.True(t, strings.Contains(body, `method="GET"`))
}
