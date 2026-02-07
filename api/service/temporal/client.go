// Package temporal defines the API types, configuration structs, context keys,
// and event constants for the Temporal integration service layer.
package temporal

import (
	"encoding/json"
	"fmt"
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
	Meta              attrs.Bag                  `json:"meta"`
	TLS               *TLSConfig                 `json:"tls,omitempty"`
	Auth              AuthConfig                 `json:"auth"`
	Address           string                     `json:"address"`
	Namespace         string                     `json:"namespace,omitempty"`
	TQPrefix          string                     `json:"tq_prefix,omitempty"`
	Lifecycle         supervisor.LifecycleConfig `json:"lifecycle,omitempty"`
	HealthCheck       HealthCheckConfig          `json:"health_check,omitempty"`
	ConnectionTimeout time.Duration              `json:"connection_timeout,omitzero,format:units"`
	KeepAliveTime     time.Duration              `json:"keep_alive_time,omitzero,format:units"`
	KeepAliveTimeout  time.Duration              `json:"keep_alive_timeout,omitzero,format:units"`
}

// AuthConfig defines authentication settings for a Temporal client.
// Exactly one auth method is active per Type: "none" (default), "api_key"
// (requires one of APIKey/APIKeyEnv/APIKeyFile), or "mtls" (requires
// exactly one cert source and one key source).
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
	CAFile             string `json:"ca_file,omitempty"`
	ServerName         string `json:"server_name,omitempty"`
	Enabled            bool   `json:"enabled"`
	InsecureSkipVerify bool   `json:"insecure_skip_verify,omitempty"`
}

// HealthCheckConfig defines health check settings
type HealthCheckConfig struct {
	Enabled  bool          `json:"enabled"`                        // Enable health checks
	Interval time.Duration `json:"interval,omitzero,format:units"` // Check interval (default: 30s)
}

// InitDefaults sets zero-value fields to sensible defaults: namespace "default",
// connection timeout 10s, keep-alive time 30s, keep-alive timeout 10s,
// health check interval 30s (when enabled), and auth type "none".
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

// UnmarshalJSON implements custom unmarshaling for ClientConfig to parse duration strings.
func (c *ClientConfig) UnmarshalJSON(data []byte) error {
	type Alias ClientConfig
	aux := &struct {
		*Alias
		ConnectionTimeout string `json:"connection_timeout"`
		KeepAliveTime     string `json:"keep_alive_time"`
		KeepAliveTimeout  string `json:"keep_alive_timeout"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.ConnectionTimeout != "" {
		d, err := time.ParseDuration(aux.ConnectionTimeout)
		if err != nil {
			return fmt.Errorf("invalid connection_timeout: %w", err)
		}
		c.ConnectionTimeout = d
	}
	if aux.KeepAliveTime != "" {
		d, err := time.ParseDuration(aux.KeepAliveTime)
		if err != nil {
			return fmt.Errorf("invalid keep_alive_time: %w", err)
		}
		c.KeepAliveTime = d
	}
	if aux.KeepAliveTimeout != "" {
		d, err := time.ParseDuration(aux.KeepAliveTimeout)
		if err != nil {
			return fmt.Errorf("invalid keep_alive_timeout: %w", err)
		}
		c.KeepAliveTimeout = d
	}
	return nil
}

// UnmarshalJSON implements custom unmarshaling for HealthCheckConfig to parse duration strings.
func (c *HealthCheckConfig) UnmarshalJSON(data []byte) error {
	type Alias HealthCheckConfig
	aux := &struct {
		*Alias
		Interval string `json:"interval"`
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, aux); err != nil {
		return err
	}

	if aux.Interval != "" {
		d, err := time.ParseDuration(aux.Interval)
		if err != nil {
			return fmt.Errorf("invalid interval: %w", err)
		}
		c.Interval = d
	}
	return nil
}
