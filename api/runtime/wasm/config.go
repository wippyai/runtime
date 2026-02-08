// Package wasm provides WASM runtime integration.
package wasm

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

type (
	// PoolConfig defines settings for a pool of WASM executors.
	PoolConfig struct {
		Type    string `json:"type"`    // Pool type: static, lazy, inline, adaptive
		Size    int    `json:"size"`    // Total pool size for non-flex pools
		Workers int    `json:"workers"` // Number of worker threads
		Buffer  int    `json:"buffer"`  // Queue buffer size (default: workers * 64)

		// Elastic pool specifics.
		WarmStart bool `json:"warm_start"` // Pre-instantiate workers where applicable
		MaxSize   int  `json:"max_size"`   // Maximum workers for elastic pools
	}

	// LimitsConfig defines execution limits for a WASM function.
	LimitsConfig struct {
		MaxExecutionMS int `json:"max_execution_ms,omitempty"`
	}

	// WATFunctionConfig defines configuration for inline WAT function entries.
	WATFunctionConfig struct {
		Imports   map[string]registry.ID `json:"imports,omitempty"`
		Meta      attrs.Bag              `json:"meta,omitempty"`
		Source    string                 `json:"source"`
		Method    string                 `json:"method"`
		Transport string                 `json:"transport,omitempty"`
		Pool      PoolConfig             `json:"pool,omitempty"`
		Limits    LimitsConfig           `json:"limits,omitempty"`
		WIT       string                 `json:"wit,omitempty"` // Optional for raw/core WASM signatures
	}

	// WASMFunctionConfig defines configuration for precompiled WASM function entries.
	WASMFunctionConfig struct {
		Imports   map[string]registry.ID `json:"imports,omitempty"`
		Meta      attrs.Bag              `json:"meta,omitempty"`
		FS        string                 `json:"fs"`
		Path      string                 `json:"path"`
		Hash      string                 `json:"hash"`
		Method    string                 `json:"method"`
		Transport string                 `json:"transport,omitempty"`
		Pool      PoolConfig             `json:"pool,omitempty"`
		Limits    LimitsConfig           `json:"limits,omitempty"`
		WIT       string                 `json:"wit,omitempty"` // Optional for raw/core WASM signatures
	}
)

// EffectiveTransport returns the transport, defaulting to payload.
func (c *WATFunctionConfig) EffectiveTransport() string {
	if c.Transport == "" {
		return TransportTypePayload
	}
	return c.Transport
}

// Validate checks if the WATFunctionConfig has required fields and valid values.
func (c *WATFunctionConfig) Validate() error {
	if c.Source == "" {
		return ErrSourceRequired
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validateImports(c.Imports); err != nil {
		return err
	}
	if err := validateTransport(c.Transport); err != nil {
		return err
	}
	if err := validatePool(c.Pool); err != nil {
		return err
	}
	if err := validateLimits(c.Limits); err != nil {
		return err
	}
	return nil
}

// EffectiveTransport returns the transport, defaulting to payload.
func (c *WASMFunctionConfig) EffectiveTransport() string {
	if c.Transport == "" {
		return TransportTypePayload
	}
	return c.Transport
}

// Validate checks if the WASMFunctionConfig has required fields and valid values.
func (c *WASMFunctionConfig) Validate() error {
	if c.FS == "" {
		return ErrFSRequired
	}
	if c.Path == "" {
		return ErrPathRequired
	}
	if c.Hash == "" {
		return ErrHashRequired
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validateImports(c.Imports); err != nil {
		return err
	}
	if err := validateTransport(c.Transport); err != nil {
		return err
	}
	if err := validatePool(c.Pool); err != nil {
		return err
	}
	if err := validateLimits(c.Limits); err != nil {
		return err
	}
	return nil
}

func validateImports(imports map[string]registry.ID) error {
	for alias, id := range imports {
		if alias == "" {
			return ErrEmptyImportAlias
		}
		if id.Name == "" {
			return ErrEmptyImportName
		}
	}
	return nil
}

func validateTransport(transport string) error {
	switch transport {
	case "", TransportTypePayload, TransportTypeWASIHTTP:
		return nil
	default:
		return ErrInvalidTransportType
	}
}

func validatePool(pool PoolConfig) error {
	if pool.Size < 0 || pool.Workers < 0 || pool.Buffer < 0 || pool.MaxSize < 0 {
		return ErrInvalidPoolConfig
	}

	if pool.Type != "" {
		switch pool.Type {
		case PoolTypeLazy, PoolTypeStatic, PoolTypeInline, PoolTypeAdaptive:
		default:
			return ErrInvalidPoolType
		}
	}

	// Legacy-compatible validation semantics from lua runtime:
	// - workers=0,size=0 is flex/lazy style
	// - workers>0 requires size>0
	isFlexPool := pool.Workers == 0 && (pool.Size == 0 || pool.MaxSize > 0)
	if !isFlexPool && pool.Size <= 0 {
		return ErrInvalidPoolSize
	}
	if pool.Workers > 0 && pool.Size <= 0 {
		return ErrInvalidWorkerPoolSize
	}

	return nil
}

func validateLimits(limits LimitsConfig) error {
	if limits.MaxExecutionMS < 0 {
		return ErrInvalidExecutionLimit
	}
	return nil
}
