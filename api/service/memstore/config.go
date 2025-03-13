package memstore

import (
	"fmt"
	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
	"time"
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
