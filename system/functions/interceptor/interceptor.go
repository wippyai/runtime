package interceptor

import (
	"context"
	"time"
)

// Execution represents a function execution context
type Execution struct {
	FunctionID   string
	Options      map[string]interface{}
	Context      context.Context
	Interceptors []Interceptor
	Result       interface{}
	Error        error
	StartTime    time.Time
	EndTime      time.Time
}

// Interceptor defines the interface for function execution interceptors
type Interceptor interface {
	// Before is called before the function execution
	Before(ctx context.Context, execution *Execution) error

	// After is called after the function execution
	After(ctx context.Context, execution *Execution, result interface{}, err error) error
}

// Option represents a function execution option
type Option interface {
	// Validate validates the option
	Validate() error

	// Apply applies the option to the execution
	Apply(execution *Execution) error
}

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

// NewExecution creates a new execution context
func NewExecution(functionID string, opts map[string]interface{}) *Execution {
	return &Execution{
		FunctionID:   functionID,
		Options:      opts,
		Context:      context.Background(),
		Interceptors: make([]Interceptor, 0),
		StartTime:    time.Now(),
	}
}
