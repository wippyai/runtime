// SPDX-License-Identifier: MPL-2.0

package otel

import (
	"context"
	"sync"

	api "github.com/wippyai/runtime/api/metrics"
	"go.opentelemetry.io/otel/attribute"
	otelmetric "go.opentelemetry.io/otel/metric"
)

type MetricsExporter struct {
	meter    otelmetric.Meter
	counters map[string]otelmetric.Float64Counter
	gauges   map[string]otelmetric.Float64Gauge
	histos   map[string]otelmetric.Float64Histogram
	mu       sync.RWMutex
}

func NewMetricsExporter(provider otelmetric.MeterProvider) *MetricsExporter {
	return &MetricsExporter{
		meter:    provider.Meter("wippy-runtime"),
		counters: make(map[string]otelmetric.Float64Counter),
		gauges:   make(map[string]otelmetric.Float64Gauge),
		histos:   make(map[string]otelmetric.Float64Histogram),
	}
}

func (e *MetricsExporter) Name() string {
	return "otel"
}

func (e *MetricsExporter) Record(name string, typ api.MetricType, value float64, labels api.Labels) error {
	attrs := labelsToAttributes(labels)

	switch typ {
	case api.TypeCounter:
		counter, err := e.getOrCreateCounter(name)
		if err != nil {
			return err
		}
		counter.Add(context.Background(), value, otelmetric.WithAttributes(attrs...))

	case api.TypeGauge:
		gauge, err := e.getOrCreateGauge(name)
		if err != nil {
			return err
		}
		gauge.Record(context.Background(), value, otelmetric.WithAttributes(attrs...))

	case api.TypeHistogram:
		histo, err := e.getOrCreateHistogram(name)
		if err != nil {
			return err
		}
		histo.Record(context.Background(), value, otelmetric.WithAttributes(attrs...))
	}

	return nil
}

func (e *MetricsExporter) getOrCreateCounter(name string) (otelmetric.Float64Counter, error) {
	e.mu.RLock()
	c, ok := e.counters[name]
	e.mu.RUnlock()
	if ok {
		return c, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if c, ok = e.counters[name]; ok {
		return c, nil
	}

	c, err := e.meter.Float64Counter(name)
	if err != nil {
		return nil, err
	}
	e.counters[name] = c
	return c, nil
}

func (e *MetricsExporter) getOrCreateGauge(name string) (otelmetric.Float64Gauge, error) {
	e.mu.RLock()
	g, ok := e.gauges[name]
	e.mu.RUnlock()
	if ok {
		return g, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if g, ok = e.gauges[name]; ok {
		return g, nil
	}

	g, err := e.meter.Float64Gauge(name)
	if err != nil {
		return nil, err
	}
	e.gauges[name] = g
	return g, nil
}

// otelHistogramBuckets aligns OTel histogram bucket boundaries with the
// Prometheus client_golang DefBuckets that our in-pod Prometheus exporter
// uses (see service/metrics/prometheus/exporter.go). Without this, OTel's
// SDK default boundaries (5, 10, 25, 50, 75, 100, 250, 500, 750, 1000,
// 2500, 5000, 7500, 10000) get pushed to Prometheus alongside the in-pod
// /metrics scrape that uses (0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1,
// 2.5, 5, 10). The two schemes get joined into one bucket label set, and
// histogram_quantile() over the merged series returns wildly wrong
// percentiles (saw pg_op_p99 reported as 4.9s when actual p99 was <5ms,
// because the `le=10000` bucket from the OTel side caught observations
// that should have been in `le=0.005` from the Prometheus side).
//
// Both export paths now produce the same bucket set, so the merged series
// is a true sum and histogram_quantile() returns the correct percentile.
var otelHistogramBuckets = []float64{0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10}

func (e *MetricsExporter) getOrCreateHistogram(name string) (otelmetric.Float64Histogram, error) {
	e.mu.RLock()
	h, ok := e.histos[name]
	e.mu.RUnlock()
	if ok {
		return h, nil
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if h, ok = e.histos[name]; ok {
		return h, nil
	}

	h, err := e.meter.Float64Histogram(name,
		otelmetric.WithExplicitBucketBoundaries(otelHistogramBuckets...))
	if err != nil {
		return nil, err
	}
	e.histos[name] = h
	return h, nil
}

func (e *MetricsExporter) Close() error {
	return nil
}

func labelsToAttributes(labels api.Labels) []attribute.KeyValue {
	if len(labels) == 0 {
		return nil
	}
	attrs := make([]attribute.KeyValue, 0, len(labels))
	for k, v := range labels {
		attrs = append(attrs, attribute.String(k, v))
	}
	return attrs
}
