// Package host provides host service configuration.
package host

import (
	"runtime"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Registry kind constants for Process Host components
const (
	// KindHost identifies a process host service component
	KindHost registry.Kind = "process.host"
)

// EntryConfig represents the full configuration entry for a process host service including lifecycle management.
type EntryConfig struct {
	HostConfig Config                     `json:"host"`
	Lifecycle  supervisor.LifecycleConfig `json:"lifecycle"`
}

// Config represents configuration for a process host service
type Config struct {
	// Scheduler settings
	Workers        int `json:"workers"`          // Number of worker goroutines (default: NumCPU)
	QueueSize      int `json:"queue_size"`       // Global queue capacity (default: 1024)
	LocalQueueSize int `json:"local_queue_size"` // Per-worker local deque size (default: 256)
}

func (cfg *EntryConfig) initDefaults() {
	cfg.Lifecycle.InitDefaults()

	if cfg.HostConfig.Workers == 0 {
		cfg.HostConfig.Workers = runtime.NumCPU()
	}

	if cfg.HostConfig.QueueSize == 0 {
		cfg.HostConfig.QueueSize = 1024
	}

	if cfg.HostConfig.LocalQueueSize == 0 {
		cfg.HostConfig.LocalQueueSize = 256
	}
}

// Validate checks if the configuration is valid
func (cfg *EntryConfig) Validate() error {
	cfg.initDefaults()

	c := cfg.HostConfig

	if c.Workers <= 0 {
		return ErrInvalidWorkers
	}

	if c.QueueSize <= 0 {
		return ErrInvalidQueueSize
	}

	if c.LocalQueueSize <= 0 {
		return ErrInvalidLocalQueueSize
	}

	return nil
}
