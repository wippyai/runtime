package process

import (
	"encoding/json"
	"fmt"
	"runtime"
	"time"

	"github.com/ponyruntime/pony/api/registry"
	"github.com/ponyruntime/pony/api/supervisor"
)

// Registry kind constants for Process Host components
const (
	// KindHost identifies a process host service component
	KindHost registry.Kind = "process.host"
)

type EntryConfig struct {
	HostConfig HostConfig                 `json:"host_config"`
	Lifecycle  supervisor.LifecycleConfig `json:"lifecycle"`
}

// HostConfig represents configuration for a process host service
type HostConfig struct {
	// Process execution settings
	MaxProcesses  int `json:"max_processes"`  // Maximum number of concurrent processes
	Workers       int `json:"workers"`        // Number of workers processing steps
	MessageBuffer int `json:"message_buffer"` // Size of message buffer per process

	// Queue sizes
	ProcessQueueSize int `json:"process_queue_size"` // Size of the process launch queue
	StepQueueSize    int `json:"step_queue_size"`    // Size of the step execution queue

	// Timeouts
	StepTimeout     time.Duration `json:"step_timeout"`     // Maximum time allowed for a single step execution
	LaunchTimeout   time.Duration `json:"launch_timeout"`   // Timeout for process launch operations
	ShutdownTimeout time.Duration `json:"shutdown_timeout"` // Timeout for graceful shutdown
}

// UnmarshalJSON implements custom unmarshaling for HostConfig to handle time.Duration fields
func (c *HostConfig) UnmarshalJSON(data []byte) error {
	type Alias HostConfig
	aux := &struct {
		StepTimeout     string `json:"step_timeout"`
		LaunchTimeout   string `json:"launch_timeout"`
		ShutdownTimeout string `json:"shutdown_timeout"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.StepTimeout != "" {
		c.StepTimeout, err = time.ParseDuration(aux.StepTimeout)
		if err != nil {
			return fmt.Errorf("invalid step_timeout duration format: %w", err)
		}
	}

	if aux.LaunchTimeout != "" {
		c.LaunchTimeout, err = time.ParseDuration(aux.LaunchTimeout)
		if err != nil {
			return fmt.Errorf("invalid launch_timeout duration format: %w", err)
		}
	}

	if aux.ShutdownTimeout != "" {
		c.ShutdownTimeout, err = time.ParseDuration(aux.ShutdownTimeout)
		if err != nil {
			return fmt.Errorf("invalid shutdown_timeout duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for HostConfig to handle time.Duration fields
func (c *HostConfig) MarshalJSON() ([]byte, error) {
	type Alias HostConfig
	return json.Marshal(&struct {
		StepTimeout     string `json:"step_timeout"`
		LaunchTimeout   string `json:"launch_timeout"`
		ShutdownTimeout string `json:"shutdown_timeout"`
		*Alias
	}{
		StepTimeout:     c.StepTimeout.String(),
		LaunchTimeout:   c.LaunchTimeout.String(),
		ShutdownTimeout: c.ShutdownTimeout.String(),
		Alias:           (*Alias)(c),
	})
}

// DefaultConfig returns a HostConfig with sensible defaults
func DefaultConfig() *EntryConfig {
	// Initialize lifecycle defaults
	lifecycle := supervisor.LifecycleConfig{}
	lifecycle.InitDefaults()

	return &EntryConfig{
		HostConfig: HostConfig{
			MaxProcesses:  100,
			Workers:       runtime.NumCPU(),
			MessageBuffer: 1000,

			ProcessQueueSize: 1000,
			StepQueueSize:    5000,

			StepTimeout:     30 * time.Second,
			LaunchTimeout:   1 * time.Minute,
			ShutdownTimeout: 2 * time.Minute,
		},
		Lifecycle: lifecycle,
	}
}

func (cfg *EntryConfig) InitDefaults() {
	cfg.Lifecycle.InitDefaults()

	if cfg.HostConfig.Workers == 0 {
		cfg.HostConfig.Workers = runtime.NumCPU()
	}

	// default buffer sizes
	if cfg.HostConfig.MessageBuffer == 0 {
		cfg.HostConfig.MessageBuffer = 10000
	}

	if cfg.HostConfig.ProcessQueueSize == 0 {
		cfg.HostConfig.ProcessQueueSize = 10000
	}

	if cfg.HostConfig.StepQueueSize == 0 {
		cfg.HostConfig.StepQueueSize = 10000
	}

	if cfg.HostConfig.StepTimeout == 0 {
		cfg.HostConfig.StepTimeout = 30 * time.Second
	}

	if cfg.HostConfig.LaunchTimeout == 0 {
		cfg.HostConfig.LaunchTimeout = 1 * time.Minute
	}

	if cfg.HostConfig.ShutdownTimeout == 0 {
		cfg.HostConfig.ShutdownTimeout = 2 * time.Minute
	}
}

// Validate checks if the configuration is valid
func (cfg *EntryConfig) Validate() error {
	c := cfg.HostConfig

	if c.MaxProcesses < 0 {
		return fmt.Errorf("max_processes must be greater or equal 0 (no limit)")
	}

	if c.Workers <= 0 {
		return fmt.Errorf("workers must be greater than 0")
	}

	if c.MessageBuffer <= 0 {
		return fmt.Errorf("message_buffer must be greater than 0")
	}

	if c.ProcessQueueSize <= 0 {
		return fmt.Errorf("process_queue_size must be greater than 0")
	}

	if c.StepQueueSize <= 0 {
		return fmt.Errorf("step_queue_size must be greater than 0")
	}

	if c.StepTimeout <= 0 {
		return fmt.Errorf("step_timeout must be greater than 0")
	}

	if c.LaunchTimeout <= 0 {
		return fmt.Errorf("launch_timeout must be greater than 0")
	}

	if c.ShutdownTimeout <= 0 {
		return fmt.Errorf("shutdown_timeout must be greater than 0")
	}

	return nil
}
