package prometheus

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	api "github.com/wippyai/runtime/api/metrics"
)

type Exporter struct {
	registry   *prometheus.Registry
	counters   map[string]*prometheus.CounterVec
	gauges     map[string]*prometheus.GaugeVec
	histograms map[string]*prometheus.HistogramVec
	mu         sync.RWMutex
}

func NewExporter() *Exporter {
	return &Exporter{
		registry:   prometheus.NewRegistry(),
		counters:   make(map[string]*prometheus.CounterVec),
		gauges:     make(map[string]*prometheus.GaugeVec),
		histograms: make(map[string]*prometheus.HistogramVec),
	}
}

func (e *Exporter) Name() string {
	return "prometheus"
}

func (e *Exporter) Record(name string, typ api.MetricType, value float64, labels api.Labels) error {
	labelNames := sortedLabelKeys(labels)
	labelValues := labelVals(labels, labelNames)
	key := metricKey(name, labelNames)

	switch typ {
	case api.TypeCounter:
		counter := e.getOrCreateCounter(key, name, labelNames)
		counter.WithLabelValues(labelValues...).Add(value)

	case api.TypeGauge:
		gauge := e.getOrCreateGauge(key, name, labelNames)
		gauge.WithLabelValues(labelValues...).Set(value)

	case api.TypeHistogram:
		histo := e.getOrCreateHistogram(key, name, labelNames)
		histo.WithLabelValues(labelValues...).Observe(value)
	}

	return nil
}

func (e *Exporter) getOrCreateCounter(key, name string, labelNames []string) *prometheus.CounterVec {
	e.mu.RLock()
	c, ok := e.counters[key]
	e.mu.RUnlock()
	if ok {
		return c
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if c, ok = e.counters[key]; ok {
		return c
	}

	c = prometheus.NewCounterVec(prometheus.CounterOpts{Name: name}, labelNames)
	e.registry.MustRegister(c)
	e.counters[key] = c
	return c
}

func (e *Exporter) getOrCreateGauge(key, name string, labelNames []string) *prometheus.GaugeVec {
	e.mu.RLock()
	g, ok := e.gauges[key]
	e.mu.RUnlock()
	if ok {
		return g
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if g, ok = e.gauges[key]; ok {
		return g
	}

	g = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name}, labelNames)
	e.registry.MustRegister(g)
	e.gauges[key] = g
	return g
}

func (e *Exporter) getOrCreateHistogram(key, name string, labelNames []string) *prometheus.HistogramVec {
	e.mu.RLock()
	h, ok := e.histograms[key]
	e.mu.RUnlock()
	if ok {
		return h
	}

	e.mu.Lock()
	defer e.mu.Unlock()

	if h, ok = e.histograms[key]; ok {
		return h
	}

	h = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Buckets: prometheus.DefBuckets,
	}, labelNames)
	e.registry.MustRegister(h)
	e.histograms[key] = h
	return h
}

func (e *Exporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}

func (e *Exporter) Close() error {
	return nil
}

func sortedLabelKeys(labels api.Labels) []string {
	if len(labels) == 0 {
		return nil
	}
	keys := make([]string, 0, len(labels))
	for k := range labels {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

func labelVals(labels api.Labels, keys []string) []string {
	if len(keys) == 0 {
		return nil
	}
	vals := make([]string, len(keys))
	for i, k := range keys {
		vals[i] = labels[k]
	}
	return vals
}

func metricKey(name string, labelNames []string) string {
	if len(labelNames) == 0 {
		return name
	}
	return name + "{" + strings.Join(labelNames, ",") + "}"
}
