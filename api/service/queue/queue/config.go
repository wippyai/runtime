package queue

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Kind identifies queue entries in the registry.
const Kind registry.Kind = "queue.queue"

// Config describes a queue entry and its driver options.
type Config struct {
	Driver  registry.ID `json:"driver"`
	Options attrs.Bag   `json:"options"`
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
	if c.Options == nil {
		c.Options = attrs.NewBag()
	}
}
