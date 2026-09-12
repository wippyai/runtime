// SPDX-License-Identifier: MPL-2.0

// Package host provides host service configuration.
package host

import (
	"runtime"
	"time"

	apierror "github.com/wippyai/runtime/api/error"
	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/api/supervisor"
)

const DefaultRemoteMonitorLimit = 256

// Monitor replies have independent per-host bounds; a stalled peer cannot
// retain an unbounded number of workers or block replies indefinitely.
const DefaultRemoteMonitorReplies = 128
const DefaultRemoteMonitorReplyTimeout = 5 * time.Second

// Execution class constants for HostConfig.
const (
	WorkerClassDefault = ""
	WorkerClassActor   = "actor"
	WorkerClassWASM    = "wasm"
)

// Host configuration errors.
var (
	ErrInvalidRemoteMonitorLimit = apierror.New(apierror.Invalid, "remote_monitor_limit must be positive")
	ErrInvalidWorkerClass        = apierror.New(apierror.Invalid, "worker class must be empty, \"actor\", or \"wasm\"").WithRetryable(apierror.False)
)

// Registry kind constants for Process Host components
const (
	// Host identifies a process host service component
	Host registry.Kind = "process.host"
)

// EntryConfig represents the full configuration entry for a process host service including lifecycle management.
type EntryConfig struct {
	HostConfig Config                     `json:"host"`
	Lifecycle  supervisor.LifecycleConfig `json:"lifecycle"`
}

// Config represents configuration for a process host service
type Config struct {
	// RemoteMonitorLimit bounds active and retired remote observer records per process.
	RemoteMonitorLimit int `json:"remote_monitor_limit,omitempty"`
	// RemoteMonitorReplies bounds admitted, unfinished control replies per host.
	RemoteMonitorReplies int `json:"remote_monitor_replies,omitempty"`
	// RemoteMonitorReplyTimeout bounds reply delivery, never process lifetime.
	// Expiry leaves installation uncertain for the requester; it does not undo it.
	RemoteMonitorReplyTimeout time.Duration `json:"remote_monitor_reply_timeout,omitempty"`
	WorkerClass               string        `json:"worker_class,omitempty"` // Execution class: "" (actor/default) or "wasm"

	// Scheduler settings
	Workers        int `json:"workers"`          // Number of worker goroutines (default: NumCPU)
	QueueSize      int `json:"queue_size"`       // Global queue capacity (default: 1024)
	LocalQueueSize int `json:"local_queue_size"` // Per-worker local deque size (default: 256)
}

func (cfg *EntryConfig) initDefaults() {
	cfg.Lifecycle.InitDefaults()
	if cfg.HostConfig.RemoteMonitorLimit == 0 {
		cfg.HostConfig.RemoteMonitorLimit = DefaultRemoteMonitorLimit
	}

	if cfg.HostConfig.RemoteMonitorReplies == 0 {
		cfg.HostConfig.RemoteMonitorReplies = DefaultRemoteMonitorReplies
	}
	if cfg.HostConfig.RemoteMonitorReplyTimeout == 0 {
		cfg.HostConfig.RemoteMonitorReplyTimeout = DefaultRemoteMonitorReplyTimeout
	}

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
	if c.RemoteMonitorReplies <= 0 || c.RemoteMonitorReplyTimeout <= 0 {
		return apierror.New(apierror.Invalid, "remote monitor reply capacity and timeout must be positive")
	}
	if c.RemoteMonitorLimit <= 0 {
		return ErrInvalidRemoteMonitorLimit
	}

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
