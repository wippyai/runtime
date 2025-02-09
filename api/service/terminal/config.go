package terminal

import (
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Default timeout values
const (
	DefaultStartTimeout = 10 * time.Second
	DefaultStopTimeout  = 5 * time.Second
	DefaultCloseTimeout = 3 * time.Second
)

// ServiceConfig represents the configuration for a terminal service
type ServiceConfig struct {
	Meta      registry.Metadata          `json:"meta"`
	Target    registry.ID                `json:"target"`    // Name of the terminal app to use
	HideLogs  bool                       `json:"hide_logs"` // Redirect logs (all) to the event bus, releases io.Output
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
}

// Validate checks if the service configuration is valid
func (c *ServiceConfig) Validate() error {
	if c.Meta == nil {
		return fmt.Errorf("metadata cannot be nil")
	}
	//if c.Handler == "" {
	//	return fmt.Errorf("target cannot be empty")
	//}
	// todo: fix it
	return nil
}

// InitDefaults initializes the ServiceConfig with default values
func (c *ServiceConfig) InitDefaults() {
	c.Lifecycle.InitDefaults()
}
