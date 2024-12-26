package http

import (
	"encoding/json"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"time"
)

const (
	KindServer   registry.Kind = "http.service"
	KindRouter   registry.Kind = "http.router"
	KindEndpoint registry.Kind = "http.endpoint"

	ServerID string = "server"
	RouterID string = "router"
)

// ServerConfig represents the initial configuration for the Timeouts service.
type (
	ServerConfig struct {
		Meta      registry.Metadata          `json:"meta"`
		Addr      string                     `json:"addr"`
		Timeouts  TimeoutConfig              `json:"timeouts"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
	}

	// TimeoutConfig represents global Timeouts-level configuration options.
	TimeoutConfig struct {
		ReadTimeout  time.Duration `json:"read"`
		WriteTimeout time.Duration `json:"write"`
		IdleTimeout  time.Duration `json:"idle"`
	}

	// RouterConfig represents the configuration for a group of endpoints (a router).
	RouterConfig struct {
		Meta        registry.Metadata `json:"meta"`        // Metadata
		Prefix      string            `json:"prefix"`      // URL prefix for this group
		Middlewares []string          `json:"middlewares"` // Middleware names
		Options     map[string]string `json:"options"`     // Middleware options
	}

	// EndpointConfig represents the configuration for a single endpoint.
	EndpointConfig struct {
		Meta    registry.Metadata `json:"meta"`    // Metadata
		Path    string            `json:"path"`    // URL path
		Method  string            `json:"method"`  // Timeouts method
		Options map[string]string `json:"options"` // Endpoint options
	}
)

// UnmarshalJSON implements custom unmarshaling for TimeoutConfig to handle time.Duration fields.
func (c *TimeoutConfig) UnmarshalJSON(data []byte) error {
	type Alias TimeoutConfig
	aux := &struct {
		ReadTimeout  string `json:"read"`
		WriteTimeout string `json:"write"`
		IdleTimeout  string `json:"idle"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.ReadTimeout != "" {
		c.ReadTimeout, err = time.ParseDuration(aux.ReadTimeout)
		if err != nil {
			return fmt.Errorf("invalid ReadTimeout duration format: %w", err)
		}
	}

	if aux.WriteTimeout != "" {
		c.WriteTimeout, err = time.ParseDuration(aux.WriteTimeout)
		if err != nil {
			return fmt.Errorf("invalid WriteTimeout duration format: %w", err)
		}
	}

	if aux.IdleTimeout != "" {
		c.IdleTimeout, err = time.ParseDuration(aux.IdleTimeout)
		if err != nil {
			return fmt.Errorf("invalid IdleTimeout duration format: %w", err)
		}
	}

	return nil
}
