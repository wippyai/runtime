package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constants for Temporal service components
const (
	// KindClient identifies a temporal client component
	KindClient registry.Kind = "temporal.client"

	// ClientCtx is the context key for storing client references
	ClientCtx = ctxKey("temporal_client")
)

// ctxKey is a type for context keys
type ctxKey string

// AuthType represents the authentication type for Temporal connection
type AuthType string

const (
	// AuthTypeNone represents no authentication
	AuthTypeNone AuthType = "none"
	// AuthTypeAPIKey represents API key authentication
	AuthTypeAPIKey AuthType = "api_key"
	// AuthTypeTLS represents TLS certificate authentication
	AuthTypeTLS AuthType = "tls"
)

// ClientConfig represents configuration for connecting to a Temporal service
type ClientConfig struct {
	Meta        registry.Metadata          `json:"meta"`         // Metadata
	Connect     ConnectionConfig           `json:"connect"`      // Connection details
	Auth        AuthConfig                 `json:"auth"`         // Authentication configuration
	TQPrefix    string                     `json:"tq_prefix"`    // Task queue prefix for all task queues using this client
	HealthCheck HealthCheckConfig          `json:"health_check"` // Health check configuration
	Lifecycle   supervisor.LifecycleConfig `json:"lifecycle"`    // Lifecycle management config
}

// ConnectionConfig defines the connection parameters for Temporal service
type ConnectionConfig struct {
	Address   string `json:"address"`   // Temporal service address (host:port)
	Namespace string `json:"namespace"` // Temporal namespace
}

// AuthConfig defines authentication parameters for Temporal service
type AuthConfig struct {
	Type    AuthType `json:"type"`               // Authentication type
	APIKey  string   `json:"api_key,omitempty"`  // API key for authentication
	KeyFile string   `json:"key_file,omitempty"` // Path to API key file (alternative to inline key)

	// TLS-specific configuration (for future use)
	CertFile string `json:"cert_file,omitempty"` // TLS certificate file
	KeyPEM   string `json:"key_pem,omitempty"`   // TLS key PEM
	CertPEM  string `json:"cert_pem,omitempty"`  // TLS certificate PEM
}

// HealthCheckConfig defines health check configuration for the client
type HealthCheckConfig struct {
	Interval time.Duration `json:"interval"` // How often to check client health
	Enabled  bool          `json:"enabled"`  // Whether health check is enabled
}

// UnmarshalJSON implements custom unmarshaling for HealthCheckConfig to handle time.Duration fields.
func (c *HealthCheckConfig) UnmarshalJSON(data []byte) error {
	type Alias HealthCheckConfig
	aux := &struct {
		Interval string `json:"interval"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.Interval != "" {
		c.Interval, err = time.ParseDuration(aux.Interval)
		if err != nil {
			return fmt.Errorf("invalid Interval duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for HealthCheckConfig to handle time.Duration fields.
func (c *HealthCheckConfig) MarshalJSON() ([]byte, error) {
	type Alias HealthCheckConfig
	return json.Marshal(&struct {
		Interval string `json:"interval"`
		*Alias
	}{
		Interval: c.Interval.String(),
		Alias:    (*Alias)(c),
	})
}

// InitDefaults initializes the ClientConfig with default values
func (c *ClientConfig) InitDefaults() {
	// Set default connection values
	if c.Connect.Address == "" {
		c.Connect.Address = "localhost:7233"
	}
	if c.Connect.Namespace == "" {
		c.Connect.Namespace = "default"
	}

	// Set default auth type
	if c.Auth.Type == "" {
		c.Auth.Type = AuthTypeNone
	}

	// Set default health check values
	if c.HealthCheck.Interval == 0 {
		c.HealthCheck.Interval = 1 * time.Minute
	}
	if !c.HealthCheck.Enabled {
		c.HealthCheck.Enabled = true
	}

	// Initialize lifecycle defaults
	c.Lifecycle.InitDefaults()
}

// Validate checks if the client configuration is valid
func (c *ClientConfig) Validate() error {
	// Validate connection configuration
	if c.Connect.Address == "" {
		return fmt.Errorf("temporal service address cannot be empty")
	}

	if c.Connect.Namespace == "" {
		return fmt.Errorf("temporal namespace cannot be empty")
	}

	// Validate authentication configuration
	switch c.Auth.Type {
	case AuthTypeNone:
		// No validation needed
	case AuthTypeAPIKey:
		if c.Auth.APIKey == "" && c.Auth.KeyFile == "" {
			return fmt.Errorf("API key or key file path must be provided when using API key authentication")
		}
	case AuthTypeTLS:
		if (c.Auth.CertPEM == "" || c.Auth.KeyPEM == "") && c.Auth.CertFile == "" {
			return fmt.Errorf("certificate and key must be provided when using TLS authentication")
		}
	default:
		return fmt.Errorf("unsupported authentication type: %s", c.Auth.Type)
	}

	// Validate health check configuration
	if c.HealthCheck.Enabled && c.HealthCheck.Interval < time.Second {
		return fmt.Errorf("health check interval must be at least 1 second")
	}

	return nil
}
