package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// prometheusHistogram implements Histogram using Prometheus
type prometheusHistogram struct {
	baseCollector
	histogram prometheus.Histogram
	vec       *prometheus.HistogramVec
	buckets   []float64
}

func newHistogram(name, help string, buckets []float64, labels []string, register func(prometheus.Collector) error) (*prometheusHistogram, error) {
	if len(buckets) == 0 {
		buckets = prometheus.DefBuckets
	}

	h := &prometheusHistogram{
		baseCollector: newBaseCollector(name, help, "histogram", labels),
		buckets:       buckets,
	}

	if len(labels) > 0 {
		vec := prometheus.NewHistogramVec(
			prometheus.HistogramOpts{
				Name:    name,
				Help:    help,
				Buckets: buckets,
			},
			labels,
		)
		if err := register(vec); err != nil {
			return nil, fmt.Errorf("registering histogram vector: %w", err)
		}
		h.vec = vec
		h.histogram = vec.WithLabelValues(make([]string, len(labels))...).(prometheus.Histogram)
	} else {
		histogram := prometheus.NewHistogram(
			prometheus.HistogramOpts{
				Name:    name,
				Help:    help,
				Buckets: buckets,
			},
		)
		if err := register(histogram); err != nil {
			return nil, fmt.Errorf("registering histogram: %w", err)
		}
		h.histogram = histogram
	}

	return h, nil
}

func (h *prometheusHistogram) Observe(value float64, labels map[string]string) {
	if h.vec != nil && len(labels) > 0 {
		h.vec.With(prometheus.Labels(labels)).Observe(value)
	} else {
		h.histogram.Observe(value)
	}
}

type HistogramValue struct {
	// Sum of all observations
	Sum float64
	// Count of observations
	Count uint64
	// Buckets maps upper bounds to observation counts
	Buckets map[float64]uint64
}

func (h *prometheusHistogram) Value(labels map[string]string) (*HistogramValue, error) {
	var metric dto.Metric
	var err error

	if h.vec != nil && len(labels) > 0 {
		err = h.vec.With(prometheus.Labels(labels)).(prometheus.Histogram).Write(&metric)
	} else {
		err = h.histogram.Write(&metric)
	}

	if err != nil {
		return nil, fmt.Errorf("reading histogram value: %w", err)
	}

	if metric.Histogram == nil {
		return nil, fmt.Errorf("invalid histogram metric data")
	}

	result := &HistogramValue{
		Sum:     *metric.Histogram.SampleSum,
		Count:   *metric.Histogram.SampleCount,
		Buckets: make(map[float64]uint64, len(metric.Histogram.Bucket)),
	}

	for _, bucket := range metric.Histogram.Bucket {
		if bucket.UpperBound != nil && bucket.CumulativeCount != nil {
			result.Buckets[*bucket.UpperBound] = *bucket.CumulativeCount
		}
	}

	return result, nil
}

// Additional helper methods for histograms

func (h *prometheusHistogram) Buckets() []float64 {
	return h.buckets
}

func (h *prometheusHistogram) Reset(labels map[string]string) error {
	// Note: Prometheus doesn't support direct histogram reset
	// This is a no-op in the Prometheus implementation
	return nil
}
