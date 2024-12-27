package http

import (
	"encoding/json"
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"net/http"
	"strings"
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

// Validate checks if the server configuration is valid
func (c *ServerConfig) Validate() error {
	if c.Addr == "" {
		return fmt.Errorf("server address cannot be empty")
	}

	// Validate timeouts
	if err := c.Timeouts.Validate(); err != nil {
		return fmt.Errorf("invalid timeout configuration: %w", err)
	}

	// Validate lifecycle config
	if c.Lifecycle.StartTimeout < 0 {
		return fmt.Errorf("start timeout must be positive or zero (default)")
	}

	if c.Lifecycle.StopTimeout < 0 {
		return fmt.Errorf("stop timeout must be positive or zero (default)")
	}

	return nil
}

// Validate checks if the timeout configuration is valid
func (c *TimeoutConfig) Validate() error {
	if c.ReadTimeout < 0 {
		return fmt.Errorf("read timeout must be positive or zero (default)")
	}
	if c.WriteTimeout < 0 {
		return fmt.Errorf("write timeout must be positive or zero (default)")
	}
	if c.IdleTimeout < 0 {
		return fmt.Errorf("idle timeout must be positive or zero (default)")
	}
	return nil
}

// Validate checks if the router configuration is valid
func (c *RouterConfig) Validate() error {
	if c.Meta == nil {
		return fmt.Errorf("metadata cannot be nil")
	}

	routerID := c.Meta.StringValue(ServerID)
	if routerID == "" {
		return fmt.Errorf("server in metadata cannot be empty")
	}

	// Validate middleware configuration
	for _, mw := range c.Middlewares {
		switch mw {
		case "timeout":
			if c.Options != nil {
				if timeout, exists := c.Options["timeout"]; exists {
					if _, err := time.ParseDuration(timeout); err != nil {
						return fmt.Errorf("invalid timeout duration in middleware options: %w", err)
					}
				}
			}
		case "recoverer", "request_id", "real_ip":
			// These middleware don't require additional validation
			continue
		default:
			return fmt.Errorf("unsupported middleware: %s", mw)
		}
	}

	return nil
}

// Validate checks if the endpoint configuration is valid
func (c *EndpointConfig) Validate() error {
	if c.Path == "" {
		return fmt.Errorf("endpoint path cannot be empty")
	}

	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("endpoint path must start with /")
	}

	if c.Method == "" {
		return fmt.Errorf("endpoint method cannot be empty")
	}

	// Validate HTTP method
	switch c.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace:
		// Valid HTTP methods
	default:
		return fmt.Errorf("invalid HTTP method: %s", c.Method)
	}

	if c.Meta == nil {
		return fmt.Errorf("metadata cannot be nil")
	}

	// Verify required metadata
	serverID := c.Meta.StringValue(ServerID)
	if serverID == "" {
		return fmt.Errorf("server in metadata cannot be empty")
	}

	return nil
}
