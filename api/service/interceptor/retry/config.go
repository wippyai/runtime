package retry

type Config struct {
	Enabled     bool `json:"enabled" yaml:"enabled"`
	MaxAttempts int  `json:"max_attempts" yaml:"max_attempts"`
}

type Options struct {
	MaxAttempts int `json:"max_attempts"`
	BackoffMs   int `json:"backoff_ms"`
}
