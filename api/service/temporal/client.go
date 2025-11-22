package temporal

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	// KindClient identifies a Temporal client configuration in the registry
	KindClient registry.Kind = "temporal.client"
)

// AuthType defines the authentication mechanism for connecting to Temporal
type AuthType string

const (
	AuthTypeNone   AuthType = "none"
	AuthTypeAPIKey AuthType = "api_key"
	AuthTypeMTLS   AuthType = "mtls"
)

// ClientConfig defines the configuration for a Temporal client connection
type ClientConfig struct {
	Meta registry.Metadata `json:"meta"`

	// Connection settings
	Address   string `json:"address"`             // Temporal server address (host:port)
	Namespace string `json:"namespace,omitempty"` // Temporal namespace (default: "default")

	// Authentication
	Auth AuthConfig `json:"auth"`

	// TLS configuration
	TLS *TLSConfig `json:"tls,omitempty"`

	// Task queue prefix for all operations through this client
	TQPrefix string `json:"tq_prefix,omitempty"`

	// Health check configuration
	HealthCheck HealthCheckConfig `json:"health_check,omitempty"`

	// Connection options
	ConnectionTimeout time.Duration `json:"connection_timeout,omitempty"` // Default: 10s
	KeepAliveTime     time.Duration `json:"keep_alive_time,omitempty"`    // Default: 30s
	KeepAliveTimeout  time.Duration `json:"keep_alive_timeout,omitempty"` // Default: 10s

	// Lifecycle configuration
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle,omitempty"`
}

// AuthConfig defines authentication settings
type AuthConfig struct {
	Type AuthType `json:"type"` // none, api_key, mtls

	// API Key authentication
	APIKey     string `json:"api_key,omitempty"`
	APIKeyEnv  string `json:"api_key_env,omitempty"`  // Environment variable name
	APIKeyFile string `json:"api_key_file,omitempty"` // Path to file containing API key

	// mTLS authentication (certificate + private key)
	CertFile  string `json:"cert_file,omitempty"`   // Path to certificate file
	CertPEM   string `json:"cert_pem,omitempty"`    // Certificate as PEM string
	KeyFile   string `json:"key_file,omitempty"`    // Path to private key file
	KeyPEM    string `json:"key_pem,omitempty"`     // Private key as PEM string
	KeyPEMEnv string `json:"key_pem_env,omitempty"` // Private key from environment variable
}

// TLSConfig defines TLS connection settings
type TLSConfig struct {
	Enabled            bool   `json:"enabled"`                        // Enable TLS
	CAFile             string `json:"ca_file,omitempty"`              // Path to CA certificate
	ServerName         string `json:"server_name,omitempty"`          // Override server name for verification
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"` // Skip certificate verification (dev only)
}

// HealthCheckConfig defines health check settings
type HealthCheckConfig struct {
	Enabled  bool          `json:"enabled"`            // Enable health checks
	Interval time.Duration `json:"interval,omitempty"` // Check interval (default: 30s)
}

// UnmarshalJSON provides custom unmarshaling for ClientConfig, handling nested time.Duration fields
func (c *ClientConfig) UnmarshalJSON(data []byte) error {
	type Alias ClientConfig
	aux := &struct {
		ConnectionTimeout string `json:"connection_timeout"`
		KeepAliveTime     string `json:"keep_alive_time"`
		KeepAliveTimeout  string `json:"keep_alive_timeout"`
		HealthCheck       *struct {
			Enabled  bool   `json:"enabled"`
			Interval string `json:"interval"`
		} `json:"health_check"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.ConnectionTimeout != "" {
		c.ConnectionTimeout, err = time.ParseDuration(aux.ConnectionTimeout)
		if err != nil {
			return fmt.Errorf("invalid connection_timeout duration format: %w", err)
		}
	}

	if aux.KeepAliveTime != "" {
		c.KeepAliveTime, err = time.ParseDuration(aux.KeepAliveTime)
		if err != nil {
			return fmt.Errorf("invalid keep_alive_time duration format: %w", err)
		}
	}

	if aux.KeepAliveTimeout != "" {
		c.KeepAliveTimeout, err = time.ParseDuration(aux.KeepAliveTimeout)
		if err != nil {
			return fmt.Errorf("invalid keep_alive_timeout duration format: %w", err)
		}
	}

	if aux.HealthCheck != nil && aux.HealthCheck.Interval != "" {
		c.HealthCheck.Interval, err = time.ParseDuration(aux.HealthCheck.Interval)
		if err != nil {
			return fmt.Errorf("invalid health_check.interval duration format: %w", err)
		}
	}

	return nil
}

// InitDefaults initializes default values for the configuration
func (c *ClientConfig) InitDefaults() {
	if c.Namespace == "" {
		c.Namespace = "default"
	}

	if c.ConnectionTimeout == 0 {
		c.ConnectionTimeout = 10 * time.Second
	}

	if c.KeepAliveTime == 0 {
		c.KeepAliveTime = 30 * time.Second
	}

	if c.KeepAliveTimeout == 0 {
		c.KeepAliveTimeout = 10 * time.Second
	}

	if c.HealthCheck.Enabled && c.HealthCheck.Interval == 0 {
		c.HealthCheck.Interval = 30 * time.Second
	}

	if c.Auth.Type == "" {
		c.Auth.Type = AuthTypeNone
	}
}

// Validate checks if the configuration is valid
func (c *ClientConfig) Validate() error {
	if c.Address == "" {
		return fmt.Errorf("address is required")
	}

	// Validate auth configuration
	switch c.Auth.Type {
	case AuthTypeNone:
		// No additional validation needed

	case AuthTypeAPIKey:
		// Must have exactly one API key source
		sources := 0
		if c.Auth.APIKey != "" {
			sources++
		}
		if c.Auth.APIKeyEnv != "" {
			sources++
		}
		if c.Auth.APIKeyFile != "" {
			sources++
		}
		if sources == 0 {
			return fmt.Errorf("api_key auth requires one of: api_key, api_key_env, or api_key_file")
		}
		if sources > 1 {
			return fmt.Errorf("api_key auth: only one of api_key, api_key_env, or api_key_file should be specified")
		}

	case AuthTypeMTLS:
		// Must have certificate and key
		hasCert := c.Auth.CertFile != "" || c.Auth.CertPEM != ""
		hasKey := c.Auth.KeyFile != "" || c.Auth.KeyPEM != "" || c.Auth.KeyPEMEnv != ""

		if !hasCert {
			return fmt.Errorf("mtls auth requires certificate (cert_file or cert_pem)")
		}
		if !hasKey {
			return fmt.Errorf("mtls auth requires private key (key_file, key_pem, or key_pem_env)")
		}

		// Check for conflicting sources
		if c.Auth.CertFile != "" && c.Auth.CertPEM != "" {
			return fmt.Errorf("mtls auth: specify either cert_file or cert_pem, not both")
		}

		keySources := 0
		if c.Auth.KeyFile != "" {
			keySources++
		}
		if c.Auth.KeyPEM != "" {
			keySources++
		}
		if c.Auth.KeyPEMEnv != "" {
			keySources++
		}
		if keySources > 1 {
			return fmt.Errorf("mtls auth: specify only one of key_file, key_pem, or key_pem_env")
		}

	default:
		return fmt.Errorf("invalid auth type: %s (must be none, api_key, or mtls)", c.Auth.Type)
	}

	// Validate TLS config
	if c.TLS != nil && c.TLS.Enabled {
		if c.TLS.InsecureSkipVerify && c.TLS.ServerName != "" {
			return fmt.Errorf("tls: insecure_skip_verify and server_name are mutually exclusive")
		}
	}

	// Validate timeouts
	if c.ConnectionTimeout < 0 {
		return fmt.Errorf("connection_timeout must be positive")
	}
	if c.KeepAliveTime < 0 {
		return fmt.Errorf("keep_alive_time must be positive")
	}
	if c.KeepAliveTimeout < 0 {
		return fmt.Errorf("keep_alive_timeout must be positive")
	}

	if c.HealthCheck.Enabled && c.HealthCheck.Interval <= 0 {
		return fmt.Errorf("health_check interval must be positive when enabled")
	}

	return nil
}
