package metrics

// Collector is a common interface for all metric types.
type Collector interface {
	// Name returns the metric name without prefix.
	Name() string
	// Help returns the metric help text.
	Help() string
	// Type returns the metric type (counter, gauge, etc).
	Type() string
	// Labels returns the label names this collector was created with.
	Labels() []string
}

// Counter represents a cumulative metric that can only increase.
type Counter interface {
	Collector
	// Inc increments the counter by 1.
	Inc(labels map[string]string)
	// Add adds the given value to the counter.
	Add(value float64, labels map[string]string)
	// Values returns all current counter values with their label sets.
	Values() ([]CounterValues, error)
}

// CounterValues represents a counter value with its associated labels.
type CounterValues struct {
	Value  float64
	Labels map[string]string
}

// Gauge represents a metric that can arbitrarily go up and down.
type Gauge interface {
	Collector
	// Set sets the gauge to the given value.
	Set(value float64, labels map[string]string)
	// Inc increments the gauge by 1.
	Inc(labels map[string]string)
	// Dec decrements the gauge by 1.
	Dec(labels map[string]string)
	// Add adds the given value to the gauge.
	Add(value float64, labels map[string]string)
	// Sub subtracts the given value from the gauge.
	Sub(value float64, labels map[string]string)
	// Value returns the current gauge value for the given label combination.
	Value(labels map[string]string) (float64, error)
}

// Histogram represents a metric that samples observations and counts them in configurable buckets.
type Histogram interface {
	Collector
	// Observe adds a single observation to the histogram.
	Observe(value float64, labels map[string]string)
	// Value returns the current state of the histogram for the given label combination.
	Value(labels map[string]string) (*HistogramValue, error)
	// Values returns all current histogram samples with their associated label sets.
	Values() ([]HistogramValues, error)
}

// HistogramValue holds the aggregated histogram data.
type HistogramValue struct {
	// Sum of all observations.
	Sum float64
	// Count of observations.
	Count uint64
	// Buckets maps upper bounds to cumulative counts.
	Buckets map[float64]uint64
}

// HistogramValues associates a HistogramValue with its label set.
type HistogramValues struct {
	Value  *HistogramValue
	Labels map[string]string
}

// Summary represents a metric that samples observations and calculates quantiles.
type Summary interface {
	Collector
	// Observe adds a single observation to the summary.
	Observe(value float64, labels map[string]string)
	// Value returns the current state of the summary for the given label combination.
	Value(labels map[string]string) (*SummaryValue, error)
	// Values returns all current summary samples with their associated label sets.
	Values() ([]SummaryValues, error)
}

// SummaryValue holds the aggregated summary data.
type SummaryValue struct {
	// Sum of all observations.
	Sum float64
	// Count of observations.
	Count uint64
	// Quantiles maps quantiles (e.g., 0.5, 0.9, etc.) to their current values.
	Quantiles map[float64]float64
}

// SummaryValues associates a SummaryValue with its label set.
type SummaryValues struct {
	Value  *SummaryValue
	Labels map[string]string
}
