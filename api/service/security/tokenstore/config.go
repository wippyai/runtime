// SPDX-License-Identifier: MPL-2.0

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
	Store             registry.ID   `json:"store"`
	TokenKey          string        `json:"token_key,omitempty"`
	TokenKeyEnv       string        `json:"token_key_env,omitempty"`
	TokenLength       int           `json:"token_length"`
	DefaultExpiration time.Duration `json:"default_expiration,omitzero,format:units"`
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

// configJSON is used for JSON marshaling/unmarshaling with string duration
type configJSON struct {
	Store             registry.ID `json:"store"`
	TokenKey          string      `json:"token_key,omitempty"`
	TokenKeyEnv       string      `json:"token_key_env,omitempty"`
	DefaultExpiration string      `json:"default_expiration,omitempty"`
	TokenLength       int         `json:"token_length"`
}

// UnmarshalJSON implements json.Unmarshaler to handle duration strings
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.Store = raw.Store
	c.TokenLength = raw.TokenLength
	c.TokenKey = raw.TokenKey
	c.TokenKeyEnv = raw.TokenKeyEnv

	if raw.DefaultExpiration != "" {
		d, err := time.ParseDuration(raw.DefaultExpiration)
		if err != nil {
			return err
		}
		c.DefaultExpiration = d
	}

	return nil
}

// MarshalJSON implements json.Marshaler to output duration as string
func (c Config) MarshalJSON() ([]byte, error) {
	raw := configJSON{
		Store:       c.Store,
		TokenLength: c.TokenLength,
		TokenKey:    c.TokenKey,
		TokenKeyEnv: c.TokenKeyEnv,
	}
	if c.DefaultExpiration != 0 {
		raw.DefaultExpiration = c.DefaultExpiration.String()
	}
	return json.Marshal(raw)
}
