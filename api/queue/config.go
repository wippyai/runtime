// SPDX-License-Identifier: MPL-2.0

package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Config is the queue.queue registry-entry shape and the typed input to
// Driver.DeclareQueue. Universal fields sit at the top; broker-specific
// fields live under DriverOptions keyed by driver kind name (e.g. "amqp",
// "sqs", "kafka"). A driver reads only its own sub-bag; keys for other
// drivers are dormant.
type Config struct {
	DeadLetter    *DeadLetter `json:"dead_letter,omitempty"`
	DriverOptions attrs.Bag   `json:"driver_options,omitempty"`
	Driver        registry.ID `json:"driver"`
	QueueName     string      `json:"queue_name,omitempty"`
	Codec         string      `json:"codec,omitempty"`
}

// DeadLetter configures dead-letter handling for a queue.
type DeadLetter struct {
	Queue       registry.ID `json:"queue"`
	MaxAttempts int         `json:"max_attempts"`
}

// Validate checks queue configuration constraints.
func (c *Config) Validate() error {
	if c.Driver.Name == "" {
		return ErrDriverIDRequired
	}
	return nil
}

// InitDefaults initializes default values.
func (c *Config) InitDefaults() {
	if c.DriverOptions == nil {
		c.DriverOptions = attrs.NewBag()
	}
}

// DriverBag returns the sub-bag of DriverOptions for the named driver
// (e.g. "amqp", "sqs"). Returns an empty bag when the key is absent so
// callers can GetString/GetInt with defaults safely.
func (c *Config) DriverBag(driver string) attrs.Bag {
	if c == nil || c.DriverOptions == nil {
		return attrs.NewBag()
	}
	if b, ok := c.DriverOptions.GetBag(driver); ok {
		return b
	}
	return attrs.NewBag()
}

// ConsumerOptions is the queue.consumer registry-entry shape and the
// typed input to Driver.Attach. Universal fields at the top; broker-
// specific keys under DriverOptions.
type ConsumerOptions struct {
	DriverOptions attrs.Bag   `json:"driver_options,omitempty"`
	Queue         registry.ID `json:"queue"`
	Func          registry.ID `json:"func"`
	Concurrency   int         `json:"concurrency"`
	Prefetch      int         `json:"prefetch"`
	AutoAck       bool        `json:"auto_ack,omitempty"`
}

// DriverBag returns the sub-bag of DriverOptions for the named driver.
func (o *ConsumerOptions) DriverBag(driver string) attrs.Bag {
	if o == nil || o.DriverOptions == nil {
		return attrs.NewBag()
	}
	if b, ok := o.DriverOptions.GetBag(driver); ok {
		return b
	}
	return attrs.NewBag()
}
