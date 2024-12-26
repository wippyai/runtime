package http

import (
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"time"
)

const (
	KindServer   registry.Kind = "http.server"
	KindRouter   registry.Kind = "http.router"
	KindEndpoint registry.Kind = "http.endpoint"
)

// ServerConfig represents the initial configuration for the Timeouts service.
type (
	ServerConfig struct {
		Meta     registry.Metadata        `json:"meta"`      // Metadata
		Addr     string                   `json:"addr"`      // Address to listen on
		Timeouts TimeoutConfig            `json:"timeouts"`  // Global Timeouts options
		Service  supervisor.ServiceConfig `json:"lifecycle"` // Service lifecycle configuration
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
