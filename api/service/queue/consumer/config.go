package consumer

import (
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	DefaultConcurrency = 1
	DefaultPrefetch    = 10
	MaxConcurrency     = 1000
	MaxPrefetch        = 10000
)

// Config represents the configuration for a queue consumer
type Config struct {
	// Queue is the registry ID of the queue to consume from
	Queue registry.ID `json:"queue"`

	// Func is the registry ID of the function to invoke for each message
	Func registry.ID `json:"func"`

	// Concurrency is the number of concurrent workers processing messages
	Concurrency int `json:"concurrency"`

	// Prefetch is the delivery channel buffer size
	Prefetch int `json:"prefetch"`

	// Lifecycle defines the supervisor lifecycle configuration
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate validates the consumer configuration and sets defaults
func (c *Config) Validate() error {
	if c.Queue.Name == "" {
		return ErrQueueIDRequired
	}

	if c.Func.Name == "" {
		return ErrFunctionIDRequired
	}

	if c.Concurrency <= 0 {
		c.Concurrency = DefaultConcurrency
	}
	if c.Concurrency > MaxConcurrency {
		return apierror.New(apierror.Invalid, fmt.Sprintf("concurrency %d exceeds maximum %d", c.Concurrency, MaxConcurrency)).WithRetryable(apierror.False)
	}

	if c.Prefetch <= 0 {
		c.Prefetch = DefaultPrefetch
	}
	if c.Prefetch > MaxPrefetch {
		return apierror.New(apierror.Invalid, fmt.Sprintf("prefetch %d exceeds maximum %d", c.Prefetch, MaxPrefetch)).WithRetryable(apierror.False)
	}

	return nil
}
