package metrics

// Exporter is for external bridges (OTEL, Prometheus, future Analytics)
type Exporter interface {
	Name() string
	Record(name string, typ MetricType, value float64, labels Labels) error
	Close() error
}
