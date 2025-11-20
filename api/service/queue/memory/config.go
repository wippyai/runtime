package memory

import (
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const Kind registry.Kind = "queue.driver.memory"

type Config struct {
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

func (c *Config) Validate() error {
	return nil
}

func (c *Config) InitDefaults() {
}
