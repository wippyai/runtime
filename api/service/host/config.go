// Package host provides host service configuration.
package host

import (
	"fmt"
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
	// Process execution settings
	MaxProcesses int `json:"max_processes"` // Maximum number of concurrent processes
	Workers      int `json:"workers"`       // Number of workers processing steps

	// Messaging settings (from relay)
	BufferSize         int `json:"buffer_size"`          // Internal job channel buffer size
	WorkerCount        int `json:"worker_count"`         // Number of concurrent message workers
	MessageWorkerCount int `json:"message_worker_count"` // Number of concurrent message workers
}

func (cfg *EntryConfig) initDefaults() {
	cfg.Lifecycle.InitDefaults()

	if cfg.HostConfig.MaxProcesses == 0 {
		cfg.HostConfig.MaxProcesses = 5000
	}

	if cfg.HostConfig.Workers == 0 {
		cfg.HostConfig.Workers = runtime.NumCPU()
	}

	// Messaging defaults
	if cfg.HostConfig.BufferSize == 0 {
		cfg.HostConfig.BufferSize = 1024
	}

	if cfg.HostConfig.WorkerCount == 0 {
		cfg.HostConfig.WorkerCount = runtime.NumCPU()
	}

	if cfg.HostConfig.MessageWorkerCount == 0 {
		cfg.HostConfig.MessageWorkerCount = runtime.NumCPU()
	}
}

// Validate checks if the configuration is valid
func (cfg *EntryConfig) Validate() error {
	cfg.initDefaults()

	c := cfg.HostConfig

	if c.MaxProcesses < 0 {
		return fmt.Errorf("max_processes must be greater or equal 0 (no limit)")
	}

	if c.Workers <= 0 {
		return fmt.Errorf("workers must be greater than 0")
	}

	// Validate messaging settings
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be greater than 0")
	}

	if c.WorkerCount <= 0 {
		return fmt.Errorf("worker_count must be greater than 0")
	}

	return nil
}
