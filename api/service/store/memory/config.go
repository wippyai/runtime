// Package memory provides in-memory store service configuration.
package memory

import (
	"encoding/json"
	"time"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constant for the memory KV store
const (
	// KV identifies a memory KV store in the registry
	KV registry.Kind = "store.memory"
)

// Config defines configuration for an in-memory key-value store
type Config struct {
	Lifecycle       supervisor.LifecycleConfig `json:"lifecycle"`
	MaxSize         int                        `json:"max_size"`
	CleanupInterval time.Duration              `json:"cleanup_interval,omitzero,format:units"`
}

// Validate checks if the configuration is valid
// Returns an error if any configuration values are invalid
func (c *Config) Validate() error {
	// MaxSize must be non-negative (0 means unlimited)
	if c.MaxSize < 0 {
		return ErrInvalidMaxSize
	}

	// CleanupInterval must be non-negative (0 means no cleanup)
	if c.CleanupInterval < 0 {
		return ErrInvalidCleanupInterval
	}

	return nil
}

// InitDefaults initializes the configuration with sensible defaults
// Called during configuration loading to ensure all values have reasonable defaults
func (c *Config) InitDefaults() {
	// Default to 10K entries if not specified
	if c.MaxSize == 0 {
		c.MaxSize = 10000
	}

	// Default to checking every 5 minutes if not specified
	if c.CleanupInterval == 0 {
		c.CleanupInterval = 5 * time.Minute
	}

	// Initialize lifecycle defaults from supervisor package
	c.Lifecycle.InitDefaults()
}

// configJSON is used for JSON marshaling/unmarshaling with string duration
type configJSON struct {
	CleanupInterval string                     `json:"cleanup_interval,omitempty"`
	Lifecycle       supervisor.LifecycleConfig `json:"lifecycle"`
	MaxSize         int                        `json:"max_size"`
}

// UnmarshalJSON implements json.Unmarshaler to handle duration strings
func (c *Config) UnmarshalJSON(data []byte) error {
	var raw configJSON
	if err := json.Unmarshal(data, &raw); err != nil {
		return err
	}

	c.MaxSize = raw.MaxSize
	c.Lifecycle = raw.Lifecycle

	if raw.CleanupInterval != "" {
		d, err := time.ParseDuration(raw.CleanupInterval)
		if err != nil {
			return err
		}
		c.CleanupInterval = d
	}

	return nil
}

// MarshalJSON implements json.Marshaler to output duration as string
func (c Config) MarshalJSON() ([]byte, error) {
	raw := configJSON{
		MaxSize:   c.MaxSize,
		Lifecycle: c.Lifecycle,
	}
	if c.CleanupInterval != 0 {
		raw.CleanupInterval = c.CleanupInterval.String()
	}
	return json.Marshal(raw)
}
