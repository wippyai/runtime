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

	h, err := e.meter.Float64Histogram(name)
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
