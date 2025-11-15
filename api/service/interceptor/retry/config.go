// Package retry provides retry interceptor configuration.
package retry

// Config provides configuration for retry interceptor.
type Config struct {
	Enabled     bool `json:"enabled" yaml:"enabled"`
	MaxAttempts int  `json:"max_attempts" yaml:"max_attempts"`
}

// Options provides runtime options for retry behavior.
type Options struct {
	MaxAttempts int      `json:"max_attempts"`
	BackoffMs   int      `json:"backoff_ms"`
	RetryKinds  []string `json:"retry_kinds,omitempty"` // Only retry these error kinds (whitelist)
	SkipKinds   []string `json:"skip_kinds,omitempty"`  // Never retry these error kinds (blacklist)
}
