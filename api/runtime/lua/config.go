// Package lua provides Lua runtime integration.
package lua

import (
	"fmt"

	"github.com/wippyai/runtime/api/registry"
	"github.com/wippyai/runtime/system/scheduler/pool"
)

// Registry kind constants for different Lua component types.
// These are used to identify different types of Lua-based components in the registry.
const (
	// KindFunction identifies a Lua function component in the registry
	KindFunction registry.Kind = "function.lua"

	KindBteaApp  registry.Kind = "btea.app.lua"
	KindProcess  registry.Kind = "process.lua"
	KindWorkflow registry.Kind = "workflow.lua"

	// KindLibrary identifies a Lua library component in the registry
	KindLibrary registry.Kind = "library.lua"

	// KindModule identifies a Lua module component in the registry
	KindModule registry.Kind = "module.lua"

	// DefaultMaxSize how many concurrent executions are allowed in a flex pool by default
	DefaultMaxSize = 100
)

// Pool type constants for selecting scheduler implementation.
const (
	PoolTypeLazy   = "lazy"   // Zero processes at idle, creates on demand
	PoolTypeStatic = "static" // Fixed-size channel-based pool (default for high load)
	PoolTypeInline = "inline" // Synchronous inline execution
)

type (
	// PoolConfig defines settings for a pool of Lua VMs.
	// It manages the number of VMs and workers available for executing Lua code.
	PoolConfig struct {
		Type    string `json:"type"`    // Pool type: static, lazy, inline
		Size    int    `json:"size"`    // Total number of VMs in the pool / workers for engine2
		Workers int    `json:"workers"` // Number of worker threads
		Buffer  int    `json:"buffer"`  // Task queue buffer size (default: workers * 64)
		// elastic pool specifics
		WarmStart bool `json:"warm_start"` // Whether to precompile (default: false)
		MaxSize   int  `json:"max_size"`   // Maximum workers for elastic pool (default: 16)
	}

	// FunctionConfig defines the configuration for a Lua function component.
	// It includes the source code, execution method, required libraries and modules,
	// and VM pool settings.
	FunctionConfig struct {
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
		Pool    PoolConfig             `json:"pool,omitempty"`    // VM pool configuration
		Meta    registry.Metadata      `json:"meta,omitempty"`    // Metadata including options
	}

	// LibraryConfig defines the configuration for a Lua library component.
	// It includes the library source code and required modules.
	LibraryConfig struct {
		Meta    registry.Metadata      `json:"meta"`              // Metadata for the library
		Source  string                 `json:"source"`            // Library source code
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// ProcessConfig defines the configuration for a Lua processes.
	ProcessConfig struct {
		Meta    registry.Metadata      `json:"meta"`              // Metadata for the terminal
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// WorkflowConfig defines the configuration for a Lua workflow.
	// Workflows have restricted module access for deterministic execution.
	WorkflowConfig struct {
		Meta    registry.Metadata      `json:"meta"`              // Metadata for the workflow
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// BteaConfig defines the configuration for a Lua terminal app, this is custom process with host expectations.
	BteaConfig struct {
		Meta    registry.Metadata      `json:"meta"`              // Metadata for the terminal
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}
)

// ToPoolConfig converts PoolConfig to pool.Config for scheduler pool.
// Uses Workers directly if set, otherwise falls back to Size.
func (c *PoolConfig) ToPoolConfig() pool.Config {
	workers := c.Workers
	if workers == 0 {
		workers = c.Size
	}

	queueSize := c.Buffer
	if queueSize == 0 && workers > 0 {
		queueSize = workers * 64
	}

	return pool.Config{
		Workers:   workers,
		QueueSize: queueSize,
	}
}

// Validate checks if the FunctionConfig has all required fields set to valid values.
// It returns an error if any validation check fails.
func (c *FunctionConfig) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("source is required")
	}

	if c.Method == "" {
		return fmt.Errorf("method is required")
	}

	// Pool validation for different pool types
	isFlexPool := c.Pool.Workers == 0 && (c.Pool.Size == 0 || c.Pool.MaxSize > 0)

	// For non-flex pools, validate Size
	if !isFlexPool && c.Pool.Size <= 0 {
		return fmt.Errorf("pool.size must be greater than 0 for non-flex pools")
	}

	// For flex pools, validate MaxSize
	//nolint:revive,staticcheck // ok for now
	if isFlexPool && c.Pool.MaxSize <= 0 {
		// No validation error since we'll use DefaultMaxSize
	}

	// Worker pools validation
	if c.Pool.Workers > 0 && c.Pool.Size <= 0 {
		return fmt.Errorf("pool.size must be greater than 0 for worker pools")
	}

	// Validate imports
	for alias, id := range c.Imports {
		if alias == "" {
			return fmt.Errorf("import alias cannot be empty")
		}
		if id.Name == "" {
			return fmt.Errorf("import :name cannot be empty")
		}
	}

	// Validate modules
	for _, module := range c.Modules {
		if module == "" {
			return fmt.Errorf("module cannot be empty")
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return fmt.Errorf("module cannot have a namespace")
		}
	}

	return nil
}

// Validate checks if the LibraryConfig has all required fields set to valid values.
// It returns an error if any validation check fails.
func (c *LibraryConfig) Validate() error {
	if c.Source == "" {
		return fmt.Errorf("source is required")
	}

	for alias, id := range c.Imports {
		if alias == "" {
			return fmt.Errorf("import alias cannot be empty")
		}
		if id.Name == "" {
			return fmt.Errorf("import :name cannot be empty")
		}
	}

	for _, module := range c.Modules {
		if module == "" {
			return fmt.Errorf("module cannot be empty")
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return fmt.Errorf("module cannot have a namespace")
		}
	}

	return nil
}
