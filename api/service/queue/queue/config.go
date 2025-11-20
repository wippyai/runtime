package queue

import (
	"fmt"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

const Kind registry.Kind = "queue.queue"

type Config struct {
	Driver  registry.ID `json:"driver"`
	Options attrs.Bag   `json:"options"`
}

func (c *Config) Validate() error {
	if c.Driver.Name == "" {
		return fmt.Errorf("driver ID is required")
	}
	return nil
}

func (c *Config) InitDefaults() {
	if c.Options == nil {
		c.Options = attrs.NewBag()
	}
}
