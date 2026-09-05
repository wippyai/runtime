// SPDX-License-Identifier: MPL-2.0

// Package host provides host service configuration.
package host

import (
	"runtime"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

// Execution class constants for HostConfig.
const (
	WorkerClassDefault = ""
	WorkerClassActor   = "actor"
	WorkerClassWASM    = "wasm"
)

// Host configuration errors.
var (
	ErrInvalidWorkerClass = apierror.New(apierror.Invalid, "worker class must be empty, \"actor\", or \"wasm\"").WithRetryable(apierror.False)
)

// Registry kind constants for Process Host components
const (
	// Host identifies a process host service component
	Host registry.Kind = "process.host"
)

// EntryConfig represents the full configuration entry for a process host service including lifecycle management.
type EntryConfig struct {
	Lifecycle  supervisor.LifecycleConfig `json:"lifecycle"`
	HostConfig Config                     `json:"host"`
}

// Config represents configuration for a process host service
type Config struct {
	// Scheduler settings
	Workers        int    `json:"workers"`                    // Number of worker goroutines (default: NumCPU)
	QueueSize      int    `json:"queue_size"`                 // Global queue capacity (default: 1024)
	LocalQueueSize int    `json:"local_queue_size"`           // Per-worker local deque size (default: 256)
	WorkerClass    string `json:"worker_class,omitempty"`     // Execution class: "" (actor/default) or "wasm"
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

	switch c.WorkerClass {
	case WorkerClassDefault, WorkerClassActor, WorkerClassWASM:
	default:
		return ErrInvalidWorkerClass
	}

	return nil
}
