// SPDX-License-Identifier: MPL-2.0

package prometheus

import (
	"net/http"
	"sort"
	"strings"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	api "github.com/wippyai/runtime/api/metrics"
	lru "github.com/wippyai/runtime/internal/cache"
)

var (
	// Pool for label key slices (most metrics have 1-3 labels)
	labelKeysPool = sync.Pool{
		New: func() any {
			s := make([]string, 0, 4)
			return &s
		},
	}
	// Pool for label value slices
	labelValsPool = sync.Pool{
		New: func() any {
			s := make([]string, 0, 4)
			return &s
		},
	}
	// Pool for strings.Builder used in metricKey
	builderPool = sync.Pool{
		New: func() any {
			return &strings.Builder{}
		},
	}
)

const defaultMaxCardinality = 1024

type seriesEntry struct {
	deleteFn  func(lvs ...string) bool
	labelVals []string
}

type Exporter struct {
	registry       *prometheus.Registry
	counters       map[string]*prometheus.CounterVec
	gauges         map[string]*prometheus.GaugeVec
	histograms     map[string]*prometheus.HistogramVec
	series         *lru.Cache[string, *seriesEntry]
	maxCardinality int
	mu             sync.RWMutex
}

type ExporterConfig struct {
	MaxCardinality int
}

func NewExporter() *Exporter {
	return NewExporterWithConfig(ExporterConfig{MaxCardinality: defaultMaxCardinality})
}

func NewExporterWithConfig(cfg ExporterConfig) *Exporter {
	if cfg.MaxCardinality <= 0 {
		cfg.MaxCardinality = defaultMaxCardinality
	}
	e := &Exporter{
		registry:       prometheus.NewRegistry(),
		counters:       make(map[string]*prometheus.CounterVec),
		gauges:         make(map[string]*prometheus.GaugeVec),
		histograms:     make(map[string]*prometheus.HistogramVec),
		maxCardinality: cfg.MaxCardinality,
	}
	e.series = lru.New[string, *seriesEntry](
		lru.WithCapacity(cfg.MaxCardinality),
		lru.WithOnEvict(func(_ string, entry *seriesEntry) {
			if entry != nil && entry.deleteFn != nil {
				entry.deleteFn(entry.labelVals...)
			}
		}),
	)
	return e
}

func (e *Exporter) Name() string {
	return "prometheus"
}

func (e *Exporter) Record(name string, typ api.MetricType, value float64, labels api.Labels) error {
	labelNamesPtr := acquireSortedLabelKeys(labels)
	labelValsPtr := acquireLabelVals(labels, labelNamesPtr)

	var labelNames, labelVals []string
	if labelNamesPtr != nil {
		labelNames = *labelNamesPtr
	}
	if labelValsPtr != nil {
		labelVals = *labelValsPtr
	}

	key := buildMetricKey(name, labelNames)
	sig := buildSeriesSignature(key, labelVals)

	if _, ok := e.series.Get(sig); ok {
		switch typ {
		case api.TypeCounter:
			e.getOrCreateCounter(key, name, labelNames).WithLabelValues(labelVals...).Add(value)
		case api.TypeGauge:
			e.getOrCreateGauge(key, name, labelNames).WithLabelValues(labelVals...).Set(value)
		case api.TypeHistogram:
			e.getOrCreateHistogram(key, name, labelNames).WithLabelValues(labelVals...).Observe(value)
		}
	} else {
		valsCopy := make([]string, len(labelVals))
		copy(valsCopy, labelVals)

		switch typ {
		case api.TypeCounter:
			c := e.getOrCreateCounter(key, name, labelNames)
			_ = e.series.Set(sig, &seriesEntry{deleteFn: c.DeleteLabelValues, labelVals: valsCopy})
			c.WithLabelValues(labelVals...).Add(value)
		case api.TypeGauge:
			g := e.getOrCreateGauge(key, name, labelNames)
			_ = e.series.Set(sig, &seriesEntry{deleteFn: g.DeleteLabelValues, labelVals: valsCopy})
			g.WithLabelValues(labelVals...).Set(value)
		case api.TypeHistogram:
			h := e.getOrCreateHistogram(key, name, labelNames)
			_ = e.series.Set(sig, &seriesEntry{deleteFn: h.DeleteLabelValues, labelVals: valsCopy})
			h.WithLabelValues(labelVals...).Observe(value)
		}
	}

	releaseLabelSlice(labelNamesPtr)
	releaseLabelValsSlice(labelValsPtr)

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

	// Copy labelNames since prometheus stores a reference and the original may be pooled
	names := make([]string, len(labelNames))
	copy(names, labelNames)

	c = prometheus.NewCounterVec(prometheus.CounterOpts{Name: name}, names)
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

	// Copy labelNames since prometheus stores a reference and the original may be pooled
	names := make([]string, len(labelNames))
	copy(names, labelNames)

	g = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: name}, names)
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

	// Copy labelNames since prometheus stores a reference and the original may be pooled
	names := make([]string, len(labelNames))
	copy(names, labelNames)

	h = prometheus.NewHistogramVec(prometheus.HistogramOpts{
		Name:    name,
		Buckets: prometheus.DefBuckets,
	}, names)
	e.registry.MustRegister(h)
	e.histograms[key] = h
	return h
}

func (e *Exporter) Handler() http.Handler {
	return promhttp.HandlerFor(e.registry, promhttp.HandlerOpts{})
}

func (e *Exporter) Close() error {
	if e.series != nil {
		e.series.Close()
	}
	return nil
}

func acquireSortedLabelKeys(labels api.Labels) *[]string {
	if len(labels) == 0 {
		return nil
	}
	keys := labelKeysPool.Get().(*[]string)
	*keys = (*keys)[:0]
	for k := range labels {
		*keys = append(*keys, k)
	}
	sort.Strings(*keys)
	return keys
}

func acquireLabelVals(labels api.Labels, keys *[]string) *[]string {
	if keys == nil || len(*keys) == 0 {
		return nil
	}
	vals := labelValsPool.Get().(*[]string)
	*vals = (*vals)[:0]
	for _, k := range *keys {
		*vals = append(*vals, labels[k])
	}
	return vals
}

func releaseLabelSlice(s *[]string) {
	if s == nil {
		return
	}
	if cap(*s) <= 8 {
		*s = (*s)[:0]
		labelKeysPool.Put(s)
	}
}

func releaseLabelValsSlice(s *[]string) {
	if s == nil {
		return
	}
	if cap(*s) <= 8 {
		*s = (*s)[:0]
		labelValsPool.Put(s)
	}
}

func buildMetricKey(name string, labelNames []string) string {
	if len(labelNames) == 0 {
		return name
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.WriteString(name)
	b.WriteByte('{')
	for i, ln := range labelNames {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(ln)
	}
	b.WriteByte('}')
	key := b.String()
	builderPool.Put(b)
	return key
}

func buildSeriesSignature(metricKey string, labelVals []string) string {
	if len(labelVals) == 0 {
		return metricKey
	}
	b := builderPool.Get().(*strings.Builder)
	b.Reset()
	b.WriteString(metricKey)
	b.WriteByte('|')
	for i, v := range labelVals {
		if i > 0 {
			b.WriteByte(',')
		}
		b.WriteString(v)
	}
	sig := b.String()
	builderPool.Put(b)
	return sig
}
