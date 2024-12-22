package http

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"time"
)

// ServerConfig represents the initial configuration for the Timeouts server.
type (
	ServerConfig struct {
		Meta     registry.Metadata        `json:"meta"`     // Metadata
		Addr     string                   `json:"addr"`     // Address to listen on
		Service  supervisor.ServiceConfig `json:"service"`  // Service lifecycle configuration
		Timeouts TimeoutConfig            `json:"timeouts"` // Global Timeouts options
	}

	// TimeoutConfig represents global Timeouts-level configuration options.
	TimeoutConfig struct {
		ReadTimeout  time.Duration `json:"read_timeout"`
		WriteTimeout time.Duration `json:"write_timeout"`
		IdleTimeout  time.Duration `json:"idle_timeout"`
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
