// Package tokenstore provides token store service configuration.
package tokenstore

import (
	"encoding/json"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/security"
)

// TokenType constants
const (
	// TokenTypeOpaque represents simple random tokens stored in backend
	TokenTypeOpaque security.TokenType = "opaque"
)

// Registry kind for token stores
const (
	// TokenStore identifies a token store in the registry
	TokenStore registry.Kind = "security.token_store"
)

// Config defines configuration for a token store
type Config struct {
	// Store is the ID of the key-value store to use for token storage
	Store registry.ID `json:"store"`

	// TokenLength is the length of generated tokens in bytes (before encoding)
	// Default is 32 bytes (256 bits)
	TokenLength int `json:"token_length"`

	// TokenKey is an optional key for token validation/signing
	// If provided, tokens will be signed using this key
	TokenKey string `json:"token_key,omitempty"`

	// TokenKeyEnv is an optional environment variable name for the token key
	// If provided, the token key will be read from this environment variable
	TokenKeyEnv string `json:"token_key_env,omitempty"`

	// DefaultExpiration is the default token expiration time if not specified
	DefaultExpiration time.Duration `json:"default_expiration"`
}

// Validate checks if the configuration is valid
func (c *Config) Validate() error {
	if c.Store.Name == "" {
		return ErrStoreIDRequired
	}

	if c.TokenLength <= 0 {
		return ErrTokenLengthMustBePositive
	}

	return nil
}

// InitDefaults initializes default values for the configuration
func (c *Config) InitDefaults() {
	if c.TokenLength == 0 {
		c.TokenLength = 32 // 256 bits
	}

	if c.DefaultExpiration == 0 {
		c.DefaultExpiration = 24 * time.Hour // 1 day
	}
}

// UnmarshalJSON implements custom unmarshaling for Config to handle time.Duration fields
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		DefaultExpiration string `json:"default_expiration"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	if aux.DefaultExpiration != "" {
		var err error
		c.DefaultExpiration, err = time.ParseDuration(aux.DefaultExpiration)
		if err != nil {
			return NewInvalidDefaultExpirationError(err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for Config to handle time.Duration fields
func (c *Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		DefaultExpiration string `json:"default_expiration"`
		*Alias
	}{
		DefaultExpiration: c.DefaultExpiration.String(),
		Alias:             (*Alias)(c),
	})
}
