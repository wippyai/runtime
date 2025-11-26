package metrics

type MetricType string

const (
	TypeCounter   MetricType = "counter"
	TypeGauge     MetricType = "gauge"
	TypeHistogram MetricType = "histogram"
)

type Labels map[string]string
