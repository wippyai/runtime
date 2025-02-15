package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// prometheusCounter implements Counter using Prometheus
type prometheusCounter struct {
	baseCollector
	counter prometheus.Counter
	vec     *prometheus.CounterVec
}

func newCounter(name, help string, labels []string, register func(prometheus.Collector) error) (*prometheusCounter, error) {
	c := &prometheusCounter{
		baseCollector: newBaseCollector(name, help, "counter", labels),
	}

	if len(labels) > 0 {
		vec := prometheus.NewCounterVec(
			prometheus.CounterOpts{
				Name: name,
				Help: help,
			},
			labels,
		)
		if err := register(vec); err != nil {
			return nil, fmt.Errorf("registering counter vector: %w", err)
		}
		c.vec = vec
		c.counter = vec.WithLabelValues(make([]string, len(labels))...)
	} else {
		counter := prometheus.NewCounter(
			prometheus.CounterOpts{
				Name: name,
				Help: help,
			},
		)
		if err := register(counter); err != nil {
			return nil, fmt.Errorf("registering counter: %w", err)
		}
		c.counter = counter
	}

	return c, nil
}

func (c *prometheusCounter) Inc(labels map[string]string) {
	if c.vec != nil && len(labels) > 0 {
		c.vec.With(prometheus.Labels(labels)).Inc()
	} else {
		c.counter.Inc()
	}
}

func (c *prometheusCounter) Add(value float64, labels map[string]string) {
	if c.vec != nil && len(labels) > 0 {
		c.vec.With(prometheus.Labels(labels)).Add(value)
	} else {
		c.counter.Add(value)
	}
}

func (c *prometheusCounter) Value(labels map[string]string) (float64, error) {
	var metric dto.Metric
	var err error

	if c.vec != nil && len(labels) > 0 {
		err = c.vec.With(labels).(prometheus.Counter).Write(&metric)
	} else {
		err = c.counter.Write(&metric)
	}

	if err != nil {
		return 0, fmt.Errorf("reading counter value: %w", err)
	}

	if metric.Counter == nil || metric.Counter.Value == nil {
		return 0, fmt.Errorf("invalid counter metric data")
	}

	return *metric.Counter.Value, nil
}
