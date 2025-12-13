// Package metrics provides metrics collection and export abstractions.
package metrics

// MetricType constants.
const (
	TypeCounter   MetricType = "counter"
	TypeGauge     MetricType = "gauge"
	TypeHistogram MetricType = "histogram"
)

type (
	// MetricType identifies the type of metric.
	MetricType string

	// Labels are key-value pairs attached to metrics.
	Labels map[string]string

	// Collector fans out metrics to registered exporters.
	Collector interface {
		// CounterInc increments a counter by 1.
		CounterInc(name string, labels Labels)

		// CounterAdd adds delta to a counter.
		CounterAdd(name string, delta float64, labels Labels)

		// GaugeSet sets a gauge to a specific value.
		GaugeSet(name string, value float64, labels Labels)

		// GaugeInc increments a gauge by 1.
		GaugeInc(name string, labels Labels)

		// GaugeDec decrements a gauge by 1.
		GaugeDec(name string, labels Labels)

		// HistogramObserve records a value in a histogram.
		HistogramObserve(name string, value float64, labels Labels)

		// RegisterExporter adds an exporter to receive metrics.
		RegisterExporter(e Exporter) error

		// Close stops the collector and flushes pending metrics.
		Close() error
	}

	// Exporter bridges metrics to external systems (OTEL, Prometheus, etc).
	Exporter interface {
		// Name returns the exporter name.
		Name() string

		// Record sends a metric to the external system.
		Record(name string, typ MetricType, value float64, labels Labels) error

		// Close releases exporter resources.
		Close() error
	}
)
