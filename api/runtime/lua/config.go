package lua

import (
	"fmt"

	"github.com/ponyruntime/pony/api/interceptor"

	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for different Lua component types.
// These are used to identify different types of Lua-based components in the registry.
const (
	// KindFunction identifies a Lua function component in the registry
	KindFunction registry.Kind = "function.lua"

	KindBteaApp registry.Kind = "btea.app.lua"
	KindProcess registry.Kind = "process.lua"

	// KindLibrary identifies a Lua library component in the registry
	KindLibrary registry.Kind = "library.lua"

	// KindModule identifies a Lua module component in the registry
	KindModule registry.Kind = "module.lua"

	// DefaultMaxSize how many concurrent executions are allowed in a flex pool by default
	DefaultMaxSize = 100
)

type (
	// PoolConfig defines settings for a pool of Lua VMs.
	// It manages the number of VMs and workers available for executing Lua code.
	PoolConfig struct {
		Size    int `json:"size"`    // Total number of VMs in the pool
		Workers int `json:"workers"` // Number of worker threads
		// lazy/flex pool specifics
		WarmStart bool `json:"warm_start"` // Whether to precompile (default: false)
		MaxSize   int  `json:"max_size"`   // Maximum size for lazy pool / concurrent executions (default: 100)
	}

	FunctionConfigMeta struct {
		Options interceptor.Options `json:"options"`
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
		Meta    FunctionConfigMeta     `json:"meta,omitempty"`    // meta
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

	// BteaConfig defines the configuration for a Lua terminal app, this is custom process with host expectations.
	BteaConfig struct {
		Meta    registry.Metadata      `json:"meta"`              // Metadata for the terminal
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}
)

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
