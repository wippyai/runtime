package wasm

import (
	"fmt"

	"github.com/wippyai/runtime/api/funcpool"
	"github.com/wippyai/runtime/api/registry"
)

// Pool type constants for selecting scheduler implementation.
const (
	PoolTypeLazy         = "lazy"
	PoolTypeStatic       = "static"
	PoolTypeElastic      = "elastic"
	PoolTypeWorkStealing = "workstealing"
	PoolTypeInline       = "inline"
)

type (
	// PoolConfig defines settings for a pool of WASM instances.
	PoolConfig struct {
		Type    string `json:"type"`     // Pool type: static, elastic, workstealing, inline
		Size    int    `json:"size"`     // Number of workers
		Buffer  int    `json:"buffer"`   // Task queue buffer size
		MaxSize int    `json:"max_size"` // Maximum workers for elastic pool
	}

	// FunctionConfig defines configuration for a WASM function component.
	FunctionConfig struct {
		Source string            `json:"source"` // Inline WAT source
		Wit    string            `json:"wit"`    // WIT type definitions for function signatures
		Method string            `json:"method"` // Exported function name
		Pool   PoolConfig        `json:"pool,omitempty"`
		Meta   registry.Metadata `json:"meta,omitempty"`
	}
)

// ToFuncpoolConfig converts PoolConfig to funcpool.PoolConfig.
func (c *PoolConfig) ToFuncpoolConfig() funcpool.PoolConfig {
	workers := c.Size
	queueSize := c.Buffer
	if queueSize == 0 && workers > 0 {
		queueSize = workers * 64
	}

	return funcpool.PoolConfig{
		Workers:   workers,
		QueueSize: queueSize,
	}
}

// Validate checks if the FunctionConfig has all required fields.
func (c *FunctionConfig) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("source is required")
	}

	if c.Method == "" {
		return fmt.Errorf("method is required")
	}

	if c.Pool.Size <= 0 && c.Pool.Type != PoolTypeInline && c.Pool.Type != PoolTypeLazy {
		return fmt.Errorf("pool.size must be greater than 0 for non-lazy/inline pools")
	}

	return nil
}
