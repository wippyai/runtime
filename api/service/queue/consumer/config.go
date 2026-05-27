// SPDX-License-Identifier: MPL-2.0

// Package consumer holds the registry-kind identifier, errors, and
// supervisor-lifecycle wrapper for queue.consumer entries.
package consumer

import (
	"encoding/json"
	"fmt"

	apierror "github.com/wippyai/runtime/api/error"
	queueapi "github.com/wippyai/runtime/api/queue"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Kind identifies consumer entries in the registry.
const Kind registry.Kind = "queue.consumer"

const (
	DefaultConcurrency = 1
	DefaultPrefetch    = 10
	MaxConcurrency     = 1000
	MaxPrefetch        = 10000
)

// Config is the registry-entry shape for kind=queue.consumer. It wraps
// queueapi.ConsumerOptions with the supervisor lifecycle block (which is
// a supervisor-layer concern, not a queue-layer one).
type Config struct {
	queueapi.ConsumerOptions
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// UnmarshalJSON merges the flat entry fields into the embedded core type.
func (c *Config) UnmarshalJSON(data []byte) error {
	var aux struct {
		queueapi.ConsumerOptions
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	c.ConsumerOptions = aux.ConsumerOptions
	c.Lifecycle = aux.Lifecycle
	return nil
}

// Validate validates the consumer configuration and sets defaults.
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
