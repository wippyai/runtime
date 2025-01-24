package terminal

import (
	"encoding/json"
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

// TimeoutConfig represents terminal operation timeouts
type TimeoutConfig struct {
	// StopTimeout is how long to wait for terminal to stop gracefully
	StopTimeout time.Duration `json:"stop"`
	// StartTimeout is how long to wait for terminal update to complete
	StartTimeout time.Duration `json:"update"`
	// CloseTimeout is how long to wait for terminal close operation
	CloseTimeout time.Duration `json:"close"`
}

// InitDefaults initializes the TimeoutConfig with default values if they are not set
func (c *TimeoutConfig) InitDefaults() {
	if c.StopTimeout == 0 {
		c.StopTimeout = DefaultStopTimeout
	}
	if c.StartTimeout == 0 {
		c.StartTimeout = DefaultStartTimeout
	}
	if c.CloseTimeout == 0 {
		c.CloseTimeout = DefaultCloseTimeout
	}
}

// InitDefaults initializes the ServiceConfig with default values
func (c *ServiceConfig) InitDefaults() {
	c.Timeouts.InitDefaults()
	c.Lifecycle.InitDefaults()
}

// UnmarshalJSON implements custom unmarshaling for TimeoutConfig
func (c *TimeoutConfig) UnmarshalJSON(data []byte) error {
	type Alias TimeoutConfig
	aux := &struct {
		StopTimeout   string `json:"stop"`
		UpdateTimeout string `json:"update"`
		CloseTimeout  string `json:"close"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.StopTimeout != "" {
		c.StopTimeout, err = time.ParseDuration(aux.StopTimeout)
		if err != nil {
			return fmt.Errorf("invalid StopTimeout duration format: %w", err)
		}
	}

	if aux.UpdateTimeout != "" {
		c.StartTimeout, err = time.ParseDuration(aux.UpdateTimeout)
		if err != nil {
			return fmt.Errorf("invalid StartTimeout duration format: %w", err)
		}
	}

	if aux.CloseTimeout != "" {
		c.CloseTimeout, err = time.ParseDuration(aux.CloseTimeout)
		if err != nil {
			return fmt.Errorf("invalid CloseTimeout duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for TimeoutConfig
func (c *TimeoutConfig) MarshalJSON() ([]byte, error) {
	type Alias TimeoutConfig
	return json.Marshal(&struct {
		StopTimeout   string `json:"stop"`
		UpdateTimeout string `json:"update"`
		CloseTimeout  string `json:"close"`
		*Alias
	}{
		StopTimeout:   c.StopTimeout.String(),
		UpdateTimeout: c.StartTimeout.String(),
		CloseTimeout:  c.CloseTimeout.String(),
		Alias:         (*Alias)(c),
	})
}

// Validate checks if the timeout configuration is valid
func (c *TimeoutConfig) Validate() error {
	if c.StopTimeout < 0 {
		return fmt.Errorf("stop timeout must be positive or zero (default)")
	}
	if c.StartTimeout < 0 {
		return fmt.Errorf("update timeout must be positive or zero (default)")
	}
	if c.CloseTimeout < 0 {
		return fmt.Errorf("close timeout must be positive or zero (default)")
	}
	return nil
}

// ServiceConfig represents the configuration for a terminal service
type ServiceConfig struct {
	Meta      registry.Metadata          `json:"meta"`
	Target    registry.ID                `json:"target"`    // ID of the terminal app to use
	Timeouts  TimeoutConfig              `json:"timeouts"`  // Terminal operation timeouts
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
}

// Validate checks if the service configuration is valid
func (c *ServiceConfig) Validate() error {
	if c.Meta == nil {
		return fmt.Errorf("metadata cannot be nil")
	}
	if c.Target == "" {
		return fmt.Errorf("target cannot be empty")
	}
	if err := c.Timeouts.Validate(); err != nil {
		return fmt.Errorf("invalid timeout configuration: %w", err)
	}
	return nil
}
