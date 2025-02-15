package metrics

import (
	"fmt"
	"github.com/prometheus/client_golang/prometheus"
	dto "github.com/prometheus/client_model/go"
)

// prometheusSummary implements Summary using Prometheus
type prometheusSummary struct {
	baseCollector
	summary    prometheus.Summary
	vec        *prometheus.SummaryVec
	objectives map[float64]float64
}

func newSummary(name, help string, objectives map[float64]float64, labels []string, register func(prometheus.Collector) error) (*prometheusSummary, error) {
	if objectives == nil {
		objectives = map[float64]float64{
			0.5:  0.05,  // 50th percentile with 5% error
			0.9:  0.01,  // 90th percentile with 1% error
			0.99: 0.001, // 99th percentile with 0.1% error
		}
	}

	s := &prometheusSummary{
		baseCollector: newBaseCollector(name, help, "summary", labels),
		objectives:    objectives,
	}

	if len(labels) > 0 {
		vec := prometheus.NewSummaryVec(
			prometheus.SummaryOpts{
				Name:       name,
				Help:       help,
				Objectives: objectives,
			},
			labels,
		)
		if err := register(vec); err != nil {
			return nil, fmt.Errorf("registering summary vector: %w", err)
		}
		s.vec = vec
		s.summary = vec.WithLabelValues(make([]string, len(labels))...).(prometheus.Summary)
	} else {
		summary := prometheus.NewSummary(
			prometheus.SummaryOpts{
				Name:       name,
				Help:       help,
				Objectives: objectives,
			},
		)
		if err := register(summary); err != nil {
			return nil, fmt.Errorf("registering summary: %w", err)
		}
		s.summary = summary
	}

	return s, nil
}

func (s *prometheusSummary) Observe(value float64, labels map[string]string) {
	if s.vec != nil && len(labels) > 0 {
		s.vec.With(prometheus.Labels(labels)).Observe(value)
	} else {
		s.summary.Observe(value)
	}
}

type SummaryValue struct {
	// Sum of all observations
	Sum float64
	// Count of observations
	Count uint64
	// Quantiles maps quantiles (e.g., 0.5 for median) to their values
	Quantiles map[float64]float64
}

func (s *prometheusSummary) Value(labels map[string]string) (*SummaryValue, error) {
	var metric dto.Metric
	var err error

	if s.vec != nil && len(labels) > 0 {
		err = s.vec.With(prometheus.Labels(labels)).(prometheus.Summary).Write(&metric)
	} else {
		err = s.summary.Write(&metric)
	}

	if err != nil {
		return nil, fmt.Errorf("reading summary value: %w", err)
	}

	if metric.Summary == nil {
		return nil, fmt.Errorf("invalid summary metric data")
	}

	result := &SummaryValue{
		Sum:       *metric.Summary.SampleSum,
		Count:     *metric.Summary.SampleCount,
		Quantiles: make(map[float64]float64, len(metric.Summary.Quantile)),
	}

	for _, q := range metric.Summary.Quantile {
		if q.Quantile != nil && q.Value != nil {
			result.Quantiles[*q.Quantile] = *q.Value
		}
	}

	return result, nil
}

// Additional helper methods for summaries

func (s *prometheusSummary) Objectives() map[float64]float64 {
	return s.objectives
}

func (s *prometheusSummary) Reset(labels map[string]string) error {
	// Note: Prometheus doesn't support direct summary reset
	// This is a no-op in the Prometheus implementation
	return nil
}
