package memstore

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constant for the memory KV store
const (
	// KindMemoryKV identifies a memory KV store in the registry
	KindMemoryKV registry.Kind = "store.memory"
)

// MemoryConfig defines configuration for an in-memory key-value store
type MemoryConfig struct {
	// MaxSize is the maximum number of entries in the store (0 = unlimited)
	// When the store reaches this size, new entries will be rejected with ErrStoreFull
	MaxSize int `json:"max_size"`

	// CleanupInterval is how often the store checks for expired entries
	// The store will run a background task at this interval to remove entries with expired TTLs
	// Set to 0 to disable automatic cleanup
	CleanupInterval time.Duration `json:"cleanup_interval"`

	// Lifecycle configuration for supervisor
	// Controls how the store is started, stopped, and managed by the system supervisor
	Lifecycle supervisor.LifecycleConfig `json:"lifecycle"`
}

// Validate checks if the configuration is valid
// Returns an error if any configuration values are invalid
func (c *MemoryConfig) Validate() error {
	// MaxSize must be non-negative (0 means unlimited)
	if c.MaxSize < 0 {
		return fmt.Errorf("max_size must be greater than or equal to 0")
	}

	// CleanupInterval must be non-negative (0 means no cleanup)
	if c.CleanupInterval < 0 {
		return fmt.Errorf("cleanup_interval must be greater than or equal to 0")
	}

	return nil
}

// InitDefaults initializes the configuration with sensible defaults
// Called during configuration loading to ensure all values have reasonable defaults
func (c *MemoryConfig) InitDefaults() {
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

// UnmarshalJSON implements custom unmarshaling for MemoryConfig to handle time.Duration fields.
func (c *MemoryConfig) UnmarshalJSON(data []byte) error {
	type Alias MemoryConfig
	aux := &struct {
		CleanupInterval string `json:"cleanup_interval"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.CleanupInterval != "" {
		c.CleanupInterval, err = time.ParseDuration(aux.CleanupInterval)
		if err != nil {
			return fmt.Errorf("invalid CleanupInterval duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for MemoryConfig to handle time.Duration fields.
func (c *MemoryConfig) MarshalJSON() ([]byte, error) {
	type Alias MemoryConfig
	return json.Marshal(&struct {
		CleanupInterval string `json:"cleanup_interval"`
		*Alias
	}{
		CleanupInterval: c.CleanupInterval.String(),
		Alias:           (*Alias)(c),
	})
}
