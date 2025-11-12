package otel

type Config struct {
	Enabled bool `json:"enabled" yaml:"enabled"`
}

type Options struct {
	SpanName   string
	Attributes map[string]string
}
