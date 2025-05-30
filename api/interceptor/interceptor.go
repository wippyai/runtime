package interceptor

import (
	"time"
)

// CommonOptions represents common execution options
type CommonOptions struct {
	Timeout     time.Duration
	RetryPolicy *RetryPolicy
	RateLimit   *RateLimit
	WorkflowID  string
	TaskQueue   string
}

// RetryPolicy defines retry behavior
type RetryPolicy struct {
	MaxAttempts     int
	InitialInterval time.Duration
	MaxInterval     time.Duration
	Multiplier      float64
}

// RateLimit defines rate limiting behavior
type RateLimit struct {
	RequestsPerSecond int
	Burst             int
}
