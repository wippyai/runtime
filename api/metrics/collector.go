package metrics

// Collector is write-only, fans out to registered exporters
type Collector interface {
	CounterInc(name string, labels Labels)
	CounterAdd(name string, delta float64, labels Labels)
	GaugeSet(name string, value float64, labels Labels)
	GaugeInc(name string, labels Labels)
	GaugeDec(name string, labels Labels)
	HistogramObserve(name string, value float64, labels Labels)
	RegisterExporter(e Exporter) error
	Close() error
}
