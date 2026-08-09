// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	api "github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel/attribute"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func newTestMeterProvider() (*sdkmetric.MeterProvider, *sdkmetric.ManualReader) {
	reader := sdkmetric.NewManualReader()
	mp := sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader))
	return mp, reader
}

func collectMetrics(t *testing.T, reader *sdkmetric.ManualReader) []metricdata.Metrics {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(context.Background(), &rm))
	require.NotEmpty(t, rm.ScopeMetrics, "expected at least one scope")
	return rm.ScopeMetrics[0].Metrics
}

func findMetric(t *testing.T, metrics []metricdata.Metrics, name string) metricdata.Metrics {
	t.Helper()
	for _, m := range metrics {
		if m.Name == name {
			return m
		}
	}
	t.Fatalf("metric %q not found", name)
	return metricdata.Metrics{}
}

func assertAttributeValue(t *testing.T, set attribute.Set, key, want string) {
	t.Helper()
	for iter := set.Iter(); iter.Next(); {
		kv := iter.Attribute()
		if string(kv.Key) == key {
			assert.Equalf(t, want, kv.Value.AsString(), "attribute %q", key)
			return
		}
	}
	t.Fatalf("attribute %q not found in data point", key)
}

func TestMetricsExporter_Counter(t *testing.T) {
	mp, reader := newTestMeterProvider()
	defer func() { _ = mp.Shutdown(context.Background()) }()
	exp := NewMetricsExporter(mp)

	require.NoError(t, exp.Record("wippy_x_total", api.TypeCounter, 5, api.Labels{"k": "v"}))

	m := findMetric(t, collectMetrics(t, reader), "wippy_x_total")
	sum, ok := m.Data.(metricdata.Sum[float64])
	require.Truef(t, ok, "counter Data should be Sum[float64], got %T", m.Data)
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, 5.0, sum.DataPoints[0].Value)
	assertAttributeValue(t, sum.DataPoints[0].Attributes, "k", "v")
}

func TestMetricsExporter_Gauge(t *testing.T) {
	mp, reader := newTestMeterProvider()
	defer func() { _ = mp.Shutdown(context.Background()) }()
	exp := NewMetricsExporter(mp)

	require.NoError(t, exp.Record("wippy_q", api.TypeGauge, 42, api.Labels{"slot": "s1"}))

	m := findMetric(t, collectMetrics(t, reader), "wippy_q")
	g, ok := m.Data.(metricdata.Gauge[float64])
	require.Truef(t, ok, "gauge Data should be Gauge[float64], got %T", m.Data)
	require.Len(t, g.DataPoints, 1)
	assert.Equal(t, 42.0, g.DataPoints[0].Value)
	assertAttributeValue(t, g.DataPoints[0].Attributes, "slot", "s1")
}

func TestMetricsExporter_HistogramBuckets(t *testing.T) {
	mp, reader := newTestMeterProvider()
	defer func() { _ = mp.Shutdown(context.Background()) }()
	exp := NewMetricsExporter(mp)

	require.NoError(t, exp.Record("wippy_d_seconds", api.TypeHistogram, 0.7, api.Labels{}))

	m := findMetric(t, collectMetrics(t, reader), "wippy_d_seconds")
	h, ok := m.Data.(metricdata.Histogram[float64])
	require.Truef(t, ok, "histogram Data should be Histogram[float64], got %T", m.Data)
	require.Len(t, h.DataPoints, 1)
	assert.Equal(t, uint64(1), h.DataPoints[0].Count)
	assert.Equal(t, 0.7, h.DataPoints[0].Sum)
	assert.Equal(t, otelHistogramBuckets, h.DataPoints[0].Bounds,
		"histogram bounds must stay aligned with Prometheus DefBuckets to avoid histogram_quantile corruption")
}

func TestMetricsExporter_InstrumentCaching(t *testing.T) {
	mp, reader := newTestMeterProvider()
	defer func() { _ = mp.Shutdown(context.Background()) }()
	exp := NewMetricsExporter(mp)

	require.NoError(t, exp.Record("wippy_c_total", api.TypeCounter, 1, api.Labels{}))
	require.NoError(t, exp.Record("wippy_c_total", api.TypeCounter, 2, api.Labels{}))

	var matches []metricdata.Metrics
	for _, m := range collectMetrics(t, reader) {
		if m.Name == "wippy_c_total" {
			matches = append(matches, m)
		}
	}
	require.Len(t, matches, 1, "two records for the same name must reuse one instrument")

	sum := matches[0].Data.(metricdata.Sum[float64])
	require.Len(t, sum.DataPoints, 1)
	assert.Equal(t, 3.0, sum.DataPoints[0].Value, "counter must accumulate 1+2")
}
