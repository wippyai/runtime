package http

import (
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constants for HTTP service components.
// These identify different types of HTTP-related components in the registry.
const (
	// KindServer identifies an HTTP server component
	KindServer registry.Kind = "http.service"
	// KindRouter identifies an HTTP router component
	KindRouter registry.Kind = "http.router"
	// KindEndpoint identifies an HTTP endpoint component
	KindEndpoint registry.Kind = "http.endpoint"
	// KindStatic identifies a static file server component
	KindStatic registry.Kind = "http.static"
	// ServerID is the key used to identify the server in configuration metadata
	ServerID string = "server"
	// RouterID is the key used to identify the router in configuration metadata
	RouterID string = "router"
)

// ServerConfig represents the initial configuration for the Timeouts service.
type (
	ServerConfig struct {
		Meta      registry.Metadata          `json:"meta"`
		Addr      string                     `json:"addr"`
		Timeouts  TimeoutConfig              `json:"timeouts"`
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
		Host      HostConfig                 `json:"host"`
	}
	HostConfig struct {
		BufferSize  int `json:"buffer_size"`  // Internal job channel buffer size
		WorkerCount int `json:"worker_count"` // Number of concurrent worker goroutines
	}
	// TimeoutConfig represents global Timeouts-level configuration options.
	TimeoutConfig struct {
		ReadTimeout  time.Duration `json:"read"`
		WriteTimeout time.Duration `json:"write"`
		IdleTimeout  time.Duration `json:"idle"`
	}
	// RouterConfig represents the configuration for a group of endpoints (a router).
	RouterConfig struct {
		Meta           registry.Metadata `json:"meta"`            // Metadata
		Server         registry.ID       `json:"server"`          // Server Source
		Prefix         string            `json:"prefix"`          // URL prefix for this group
		Middleware     []string          `json:"middleware"`      // Middleware names
		Options        map[string]string `json:"options"`         // Middleware options
		PostMiddleware []string          `json:"post_middleware"` // Post-match middleware names
		PostOptions    map[string]string `json:"post_options"`    // Post-match middleware options
	}
	// EndpointConfig represents the configuration for a single endpoint.
	EndpointConfig struct {
		Meta   registry.Metadata `json:"meta"`   // Metadata
		Path   string            `json:"path"`   // URL path
		Method string            `json:"method"` // Timeouts method
		Func   registry.ID       `json:"func"`   // Func function
	}
	// StaticConfig represents the configuration for a static file server endpoint
	StaticConfig struct {
		Meta      registry.Metadata `json:"meta"`      // Metadata
		Path      string            `json:"path"`      // URL path prefix to serve under
		FS        registry.ID       `json:"fs"`        // Name of the filesystem to serve from
		Directory string            `json:"directory"` // Directory within the filesystem to serve
		Options   StaticOptions     `json:"options"`   // Optional configuration
	}
	StaticOptions struct {
		IndexFile    string `json:"index"` // Index file (e.g. "index.html")
		SPA          bool   `json:"spa"`   // If true, serve IndexFile for all paths
		CacheControl string `json:"cache"` // Cache-Control header value
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
		return fmt.Errorf("failed to unmarshal TimeoutConfig JSON: %w", err)
	}
	var err error
	if aux.ReadTimeout != "" {
		c.ReadTimeout, err = time.ParseDuration(aux.ReadTimeout)
		if err != nil {
			return fmt.Errorf("TimeoutConfig.ReadTimeout: invalid duration format '%s': %w", aux.ReadTimeout, err)
		}
	}
	if aux.WriteTimeout != "" {
		c.WriteTimeout, err = time.ParseDuration(aux.WriteTimeout)
		if err != nil {
			return fmt.Errorf("TimeoutConfig.WriteTimeout: invalid duration format '%s': %w", aux.WriteTimeout, err)
		}
	}
	if aux.IdleTimeout != "" {
		c.IdleTimeout, err = time.ParseDuration(aux.IdleTimeout)
		if err != nil {
			return fmt.Errorf("TimeoutConfig.IdleTimeout: invalid duration format '%s': %w", aux.IdleTimeout, err)
		}
	}
	return nil
}

// Validate checks if the server configuration is valid
func (c *ServerConfig) Validate() error {
	// Get server identifier for better error messages
	serverID := "unknown"
	if c.Meta != nil {
		if id := c.Meta.StringValue("id"); id != "" {
			serverID = id
		} else if name := c.Meta.StringValue("name"); name != "" {
			serverID = name
		}
	}

	if c.Meta == nil {
		return fmt.Errorf("ServerConfig[%s]: metadata cannot be nil - all server entries must have valid metadata", serverID)
	}

	if c.Addr == "" {
		return fmt.Errorf("ServerConfig[%s]: server address (addr field) cannot be empty", serverID)
	}

	// Validate timeouts with context
	if err := c.Timeouts.Validate(); err != nil {
		return fmt.Errorf("ServerConfig[%s]: invalid timeout configuration: %w", serverID, err)
	}

	// Validate lifecycle config with specific field names
	if c.Lifecycle.StartTimeout < 0 {
		return fmt.Errorf("ServerConfig[%s]: lifecycle.start_timeout (%v) must be positive or zero (default)", serverID, c.Lifecycle.StartTimeout)
	}
	if c.Lifecycle.StopTimeout < 0 {
		return fmt.Errorf("ServerConfig[%s]: lifecycle.stop_timeout (%v) must be positive or zero (default)", serverID, c.Lifecycle.StopTimeout)
	}

	// Validate host config with specific field names
	if c.Host.BufferSize < 0 {
		return fmt.Errorf("ServerConfig[%s]: host.buffer_size (%d) must be positive or zero (default)", serverID, c.Host.BufferSize)
	}
	if c.Host.WorkerCount < 0 {
		return fmt.Errorf("ServerConfig[%s]: host.worker_count (%d) must be positive or zero (default)", serverID, c.Host.WorkerCount)
	}

	return nil
}

// Validate checks if the timeout configuration is valid
func (c *TimeoutConfig) Validate() error {
	if c.ReadTimeout < 0 {
		return fmt.Errorf("TimeoutConfig: read_timeout (%v) must be positive or zero (default)", c.ReadTimeout)
	}
	if c.WriteTimeout < 0 {
		return fmt.Errorf("TimeoutConfig: write_timeout (%v) must be positive or zero (default)", c.WriteTimeout)
	}
	if c.IdleTimeout < 0 {
		return fmt.Errorf("TimeoutConfig: idle_timeout (%v) must be positive or zero (default)", c.IdleTimeout)
	}
	return nil
}

// Validate checks if the router configuration is valid
func (c *RouterConfig) Validate() error {
	// Get router identifier for better error messages
	routerID := "unknown"
	if c.Meta != nil {
		if id := c.Meta.StringValue("id"); id != "" {
			routerID = id
		} else if name := c.Meta.StringValue("name"); name != "" {
			routerID = name
		}
	}

	if c.Meta == nil {
		return fmt.Errorf("RouterConfig[%s]: metadata cannot be nil - all router entries must have valid metadata", routerID)
	}

	serverID := c.Meta.StringValue(ServerID)
	if serverID == "" {
		return fmt.Errorf("RouterConfig[%s]: metadata.%s cannot be empty - router must reference a valid server", routerID, ServerID)
	}

	// Additional validation for router-specific fields
	if c.Prefix != "" && !strings.HasPrefix(c.Prefix, "/") {
		return fmt.Errorf("RouterConfig[%s]: prefix '%s' must start with '/' or be empty", routerID, c.Prefix)
	}

	return nil
}

// Validate checks if the endpoint configuration is valid
func (c *EndpointConfig) Validate() error {
	// Get endpoint identifier for better error messages
	endpointID := "unknown"
	if c.Meta != nil {
		if id := c.Meta.StringValue("id"); id != "" {
			endpointID = id
		} else if name := c.Meta.StringValue("name"); name != "" {
			endpointID = name
		}
	}

	if c.Meta == nil {
		return fmt.Errorf("EndpointConfig[%s]: metadata cannot be nil - all endpoint entries must have valid metadata with required fields (method, path, func)", endpointID)
	}

	if c.Func.Name == "" {
		return fmt.Errorf("EndpointConfig[%s]: func.name cannot be empty - endpoint must reference a valid function", endpointID)
	}

	if c.Path == "" {
		return fmt.Errorf("EndpointConfig[%s]: path cannot be empty - endpoint must have a valid URL path", endpointID)
	}

	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("EndpointConfig[%s]: path '%s' must start with '/'", endpointID, c.Path)
	}

	if c.Method == "" {
		return fmt.Errorf("EndpointConfig[%s]: method cannot be empty - endpoint must specify HTTP method (GET, POST, PUT, DELETE, etc.)", endpointID)
	}

	// Validate HTTP method with detailed error
	switch c.Method {
	case http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
		http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace:
		// Valid HTTP methods
	default:
		validMethods := []string{http.MethodGet, http.MethodPost, http.MethodPut, http.MethodDelete,
			http.MethodPatch, http.MethodHead, http.MethodOptions, http.MethodTrace}
		return fmt.Errorf("EndpointConfig[%s]: invalid HTTP method '%s' - must be one of: %v", endpointID, c.Method, validMethods)
	}

	// Verify required metadata with specific field reference
	routerID := c.Meta.StringValue(RouterID)
	if routerID == "" {
		return fmt.Errorf("EndpointConfig[%s]: metadata.%s cannot be empty - endpoint must reference a valid router", endpointID, RouterID)
	}

	return nil
}

// Validate checks if the static config is valid
func (c *StaticConfig) Validate() error {
	// Get static config identifier for better error messages
	staticID := "unknown"
	if c.Meta != nil {
		if id := c.Meta.StringValue("id"); id != "" {
			staticID = id
		} else if name := c.Meta.StringValue("name"); name != "" {
			staticID = name
		}
	}

	if c.Meta == nil {
		return fmt.Errorf("StaticConfig[%s]: metadata cannot be nil - all static server entries must have valid metadata", staticID)
	}

	if c.Path == "" {
		return fmt.Errorf("StaticConfig[%s]: path cannot be empty - static server must have a valid URL path prefix", staticID)
	}

	if !strings.HasPrefix(c.Path, "/") {
		return fmt.Errorf("StaticConfig[%s]: path '%s' must start with '/'", staticID, c.Path)
	}

	// Verify required metadata with specific field reference
	serverID := c.Meta.StringValue(ServerID)
	if serverID == "" {
		return fmt.Errorf("StaticConfig[%s]: metadata.%s cannot be empty - static server must reference a valid server", staticID, ServerID)
	}

	// Validate filesystem reference
	if c.FS.Name == "" {
		return fmt.Errorf("StaticConfig[%s]: fs.name cannot be empty - static server must reference a valid filesystem", staticID)
	}

	// Validate directory path if specified
	if c.Directory != "" {
		if strings.Contains(c.Directory, "..") {
			return fmt.Errorf("StaticConfig[%s]: directory '%s' cannot contain '..' for security reasons", staticID, c.Directory)
		}
		if strings.HasPrefix(c.Directory, "/") {
			return fmt.Errorf("StaticConfig[%s]: directory '%s' should be relative (not start with '/')", staticID, c.Directory)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for TimeoutConfig to handle time.Duration fields.
func (c *TimeoutConfig) MarshalJSON() ([]byte, error) {
	type Alias TimeoutConfig
	result, err := json.Marshal(&struct {
		ReadTimeout  string `json:"read"`
		WriteTimeout string `json:"write"`
		IdleTimeout  string `json:"idle"`
		*Alias
	}{
		ReadTimeout:  c.ReadTimeout.String(),
		WriteTimeout: c.WriteTimeout.String(),
		IdleTimeout:  c.IdleTimeout.String(),
		Alias:        (*Alias)(c),
	})
	if err != nil {
		return nil, fmt.Errorf("failed to marshal TimeoutConfig to JSON: %w", err)
	}
	return result, nil
}
