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
	// Driver is the registry ID of the driver instance backing this queue.
	Driver registry.ID `json:"driver"`

	// QueueName overrides the broker-side queue/topic name. When empty the
	// queue entry's registry ID name is used.
	QueueName string `json:"queue_name,omitempty"`

	// Codec selects the per-queue serialization format for message bodies.
	// Must match a payload.Format registered on the transcoder (e.g.
	// payload.JSON, payload.MsgPack). Empty defaults to payload.JSON.
	Codec string `json:"codec,omitempty"`

	// DeadLetter configures dead-letter routing at the manager layer.
	DeadLetter *DeadLetter `json:"dead_letter,omitempty"`

	// DriverOptions carries driver-specific queue options under a per-driver
	// sub-bag. Example: {"amqp": {"durable": true, "message_ttl": "15m"}}.
	DriverOptions attrs.Bag `json:"driver_options,omitempty"`
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
	Queue         registry.ID `json:"queue"`
	Func          registry.ID `json:"func"`
	Concurrency   int         `json:"concurrency"`
	Prefetch      int         `json:"prefetch"`
	AutoAck       bool        `json:"auto_ack,omitempty"`
	DriverOptions attrs.Bag   `json:"driver_options,omitempty"`
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
