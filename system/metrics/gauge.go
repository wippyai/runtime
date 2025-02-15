package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// prometheusGauge implements Gauge using Prometheus
type prometheusGauge struct {
	baseCollector
	gauge prometheus.Gauge
	vec   *prometheus.GaugeVec
}

func newGauge(name, help string, labels []string, register func(prometheus.Collector) error) (*prometheusGauge, error) {
	g := &prometheusGauge{
		baseCollector: newBaseCollector(name, help, "gauge", labels),
	}

	if len(labels) > 0 {
		vec := prometheus.NewGaugeVec(
			prometheus.GaugeOpts{
				Name: name,
				Help: help,
			},
			labels,
		)
		if err := register(vec); err != nil {
			return nil, fmt.Errorf("registering gauge vector: %w", err)
		}
		g.vec = vec
		g.gauge = vec.WithLabelValues(make([]string, len(labels))...)
	} else {
		gauge := prometheus.NewGauge(
			prometheus.GaugeOpts{
				Name: name,
				Help: help,
			},
		)
		if err := register(gauge); err != nil {
			return nil, fmt.Errorf("registering gauge: %w", err)
		}
		g.gauge = gauge
	}

	return g, nil
}

func (g *prometheusGauge) Set(value float64, labels map[string]string) {
	if g.vec != nil && len(labels) > 0 {
		g.vec.With(prometheus.Labels(labels)).Set(value)
	} else {
		g.gauge.Set(value)
	}
}

func (g *prometheusGauge) Inc(labels map[string]string) {
	if g.vec != nil && len(labels) > 0 {
		g.vec.With(prometheus.Labels(labels)).Inc()
	} else {
		g.gauge.Inc()
	}
}

func (g *prometheusGauge) Dec(labels map[string]string) {
	if g.vec != nil && len(labels) > 0 {
		g.vec.With(prometheus.Labels(labels)).Dec()
	} else {
		g.gauge.Dec()
	}
}

func (g *prometheusGauge) Add(value float64, labels map[string]string) {
	if g.vec != nil && len(labels) > 0 {
		g.vec.With(prometheus.Labels(labels)).Add(value)
	} else {
		g.gauge.Add(value)
	}
}

func (g *prometheusGauge) Sub(value float64, labels map[string]string) {
	if g.vec != nil && len(labels) > 0 {
		g.vec.With(prometheus.Labels(labels)).Sub(value)
	} else {
		g.gauge.Sub(value)
	}
}

func (g *prometheusGauge) Value(labels map[string]string) (float64, error) {
	var metric dto.Metric
	var err error

	if g.vec != nil && len(labels) > 0 {
		err = g.vec.With(labels).(prometheus.Gauge).Write(&metric)
	} else {
		err = g.gauge.Write(&metric)
	}

	if err != nil {
		return 0, fmt.Errorf("reading gauge value: %w", err)
	}

	if metric.Gauge == nil || metric.Gauge.Value == nil {
		return 0, fmt.Errorf("invalid gauge metric data")
	}

	return *metric.Gauge.Value, nil
}
