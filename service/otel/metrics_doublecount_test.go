// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	metricsapi "github.com/wippyai/runtime/api/metrics"
	apicfg "github.com/wippyai/runtime/api/service/metrics"
	"github.com/wippyai/runtime/service/metrics"
	"github.com/wippyai/runtime/service/metrics/prometheus"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

// TestCollector_PrometheusAndOTel_NoDoubleCount verifies that a single
// observation fans out to both the in-pod Prometheus exporter and the OTel
// bridge exporter, each reporting value 1 (not 2, not conflicting series).
func TestCollector_PrometheusAndOTel_NoDoubleCount(t *testing.T) {
	coll := metrics.NewCollector(apicfg.Config{})

	promExp := prometheus.NewExporter()
	require.NoError(t, coll.RegisterExporter(promExp))

	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	defer func() { _ = mp.Shutdown(context.Background()) }()
	require.NoError(t, coll.RegisterExporter(NewMetricsExporter(mp)))

	coll.CounterInc("wippy_dual_test_total", metricsapi.Labels{"k": "v"})

	// Closing drains the buffer and runs a final flush to every exporter.
	require.NoError(t, coll.Close())

	// Prometheus side.
	rec := httptest.NewRecorder()
	promExp.Handler().ServeHTTP(rec, httptest.NewRequestWithContext(context.Background(), http.MethodGet, "/metrics", nil))
	body := rec.Body.String()
	assert.True(t,
		strings.Contains(body, `wippy_dual_test_total{k="v"} 1`),
		"prometheus side must report value 1 once, got:\n%s", body)

	// OTel side.
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.NotEmpty(t, rm.ScopeMetrics)
	var total float64
	found := false
	for _, m := range rm.ScopeMetrics[0].Metrics {
		if m.Name != "wippy_dual_test_total" {
			continue
		}
		sum, ok := m.Data.(metricdata.Sum[float64])
		if !ok {
			continue
		}
		for _, dp := range sum.DataPoints {
			for it := dp.Attributes.Iter(); it.Next(); {
				kv := it.Attribute()
				if string(kv.Key) == "k" && kv.Value.AsString() == "v" {
					total, found = dp.Value, true
				}
			}
		}
	}
	require.True(t, found, "OTel side must carry the k=v data point")
	assert.Equal(t, 1.0, total, "OTel side must report value 1 (no double-count)")
}
