package host

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
	HostConfig Config                     `json:"host_config"`
	Lifecycle  supervisor.LifecycleConfig `json:"lifecycle"`
}

// Config represents configuration for a process host service
type Config struct {
	// Process execution settings
	MaxProcesses int `json:"max_processes"` // Maximum number of concurrent processes
	Workers      int `json:"workers"`       // Number of workers processing steps

	// Queue sizes
	StepQueueSize int `json:"step_queue_size"` // Len of the step execution queue

	// Messaging settings (from pubsub)
	BufferSize         int           `json:"buffer_size"`          // Internal job channel buffer size
	WorkerCount        int           `json:"worker_count"`         // Number of concurrent message workers
	RetryTimeout       time.Duration `json:"retry_timeout"`        // Timeout for retry attempt on send
	DeliveryTimeout    time.Duration `json:"delivery_timeout"`     // Timeout for delivery to receiver
	MessageWorkerCount int           `json:"message_worker_count"` // Number of concurrent message workers
}

// UnmarshalJSON implements custom unmarshaling for Config to handle time.Duration fields
func (c *Config) UnmarshalJSON(data []byte) error {
	type Alias Config
	aux := &struct {
		RetryTimeout    string `json:"retry_timeout"`
		DeliveryTimeout string `json:"delivery_timeout"`
		*Alias
	}{
		Alias: (*Alias)(c),
	}

	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}

	var err error
	if aux.RetryTimeout != "" {
		c.RetryTimeout, err = time.ParseDuration(aux.RetryTimeout)
		if err != nil {
			return fmt.Errorf("invalid retry_timeout duration format: %w", err)
		}
	}

	if aux.DeliveryTimeout != "" {
		c.DeliveryTimeout, err = time.ParseDuration(aux.DeliveryTimeout)
		if err != nil {
			return fmt.Errorf("invalid delivery_timeout duration format: %w", err)
		}
	}

	return nil
}

// MarshalJSON implements custom marshaling for Config to handle time.Duration fields
func (c *Config) MarshalJSON() ([]byte, error) {
	type Alias Config
	return json.Marshal(&struct {
		RetryTimeout    string `json:"retry_timeout"`
		DeliveryTimeout string `json:"delivery_timeout"`
		*Alias
	}{
		RetryTimeout:    c.RetryTimeout.String(),
		DeliveryTimeout: c.DeliveryTimeout.String(),
		Alias:           (*Alias)(c),
	})
}

func (cfg *EntryConfig) InitDefaults() {
	cfg.Lifecycle.InitDefaults()

	if cfg.HostConfig.Workers == 0 {
		cfg.HostConfig.Workers = runtime.NumCPU()
	}

	if cfg.HostConfig.StepQueueSize == 0 {
		cfg.HostConfig.StepQueueSize = 5000
	}

	// Messaging defaults
	if cfg.HostConfig.BufferSize == 0 {
		cfg.HostConfig.BufferSize = 1000
	}

	if cfg.HostConfig.WorkerCount == 0 {
		cfg.HostConfig.WorkerCount = runtime.NumCPU()
	}

	if cfg.HostConfig.MessageWorkerCount == 0 {
		cfg.HostConfig.MessageWorkerCount = runtime.NumCPU()
	}

	if cfg.HostConfig.RetryTimeout == 0 {
		cfg.HostConfig.RetryTimeout = 100 * time.Millisecond
	}

	if cfg.HostConfig.DeliveryTimeout == 0 {
		cfg.HostConfig.DeliveryTimeout = 30 * time.Second
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

	if c.StepQueueSize <= 0 {
		return fmt.Errorf("step_queue_size must be greater than 0")
	}

	// Validate messaging settings
	if c.BufferSize <= 0 {
		return fmt.Errorf("buffer_size must be greater than 0")
	}

	if c.WorkerCount <= 0 {
		return fmt.Errorf("worker_count must be greater than 0")
	}

	if c.RetryTimeout <= 0 {
		return fmt.Errorf("retry_timeout must be greater than 0")
	}

	if c.DeliveryTimeout <= 0 {
		return fmt.Errorf("delivery_timeout must be greater than 0")
	}

	return nil
}
