package temporal

import (
	"encoding/json"
	"time"

	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const (
	// Client identifies a Temporal client configuration in the registry
	Client registry.Kind = "temporal.client"
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
	Meta attrs.Bag `json:"meta"`

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
			return NewInvalidConnectionTimeoutError(err)
		}
	}

	if aux.KeepAliveTime != "" {
		c.KeepAliveTime, err = time.ParseDuration(aux.KeepAliveTime)
		if err != nil {
			return NewInvalidKeepAliveTimeError(err)
		}
	}

	if aux.KeepAliveTimeout != "" {
		c.KeepAliveTimeout, err = time.ParseDuration(aux.KeepAliveTimeout)
		if err != nil {
			return NewInvalidKeepAliveTimeoutError(err)
		}
	}

	if aux.HealthCheck != nil && aux.HealthCheck.Interval != "" {
		c.HealthCheck.Interval, err = time.ParseDuration(aux.HealthCheck.Interval)
		if err != nil {
			return NewInvalidHealthCheckIntervalError(err)
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
		return ErrAddressRequired
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
			return ErrAPIKeySourceRequired
		}
		if sources > 1 {
			return ErrAPIKeySourceConflict
		}

	case AuthTypeMTLS:
		// Must have certificate and key
		hasCert := c.Auth.CertFile != "" || c.Auth.CertPEM != ""
		hasKey := c.Auth.KeyFile != "" || c.Auth.KeyPEM != "" || c.Auth.KeyPEMEnv != ""

		if !hasCert {
			return ErrMTLSCertRequired
		}
		if !hasKey {
			return ErrMTLSKeyRequired
		}

		// Check for conflicting sources
		if c.Auth.CertFile != "" && c.Auth.CertPEM != "" {
			return ErrMTLSCertConflict
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
			return ErrMTLSKeyConflict
		}

	default:
		return NewInvalidAuthTypeError(c.Auth.Type)
	}

	// Validate TLS config
	if c.TLS != nil && c.TLS.Enabled {
		if c.TLS.InsecureSkipVerify && c.TLS.ServerName != "" {
			return ErrTLSConfigConflict
		}
	}

	// Validate timeouts
	if c.ConnectionTimeout < 0 {
		return ErrConnectionTimeoutInvalid
	}
	if c.KeepAliveTime < 0 {
		return ErrKeepAliveTimeInvalid
	}
	if c.KeepAliveTimeout < 0 {
		return ErrKeepAliveTimeoutInvalid
	}

	if c.HealthCheck.Enabled && c.HealthCheck.Interval <= 0 {
		return ErrHealthCheckIntervalInvalid
	}

	return nil
}
