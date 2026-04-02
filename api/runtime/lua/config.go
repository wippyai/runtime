// SPDX-License-Identifier: MPL-2.0

// Package lua provides Lua runtime integration.
package lua

import (
	"github.com/wippyai/runtime/api/attrs"
	"github.com/wippyai/runtime/api/registry"
)

// Registry kind constants for different Lua component types.
// These are used to identify different types of Lua-based components in the registry.
const (
	// Function identifies a Lua function component in the registry
	Function registry.Kind = "function.lua"

	Process  registry.Kind = "process.lua"
	Workflow registry.Kind = "workflow.lua"

	// Library identifies a Lua library component in the registry
	Library registry.Kind = "library.lua"

	// ModuleKind identifies a Lua module component in the registry
	ModuleKind registry.Kind = "module.lua"

	// FunctionBytecode is a bytecode kind for precompiled Lua loaded from filesystem
	FunctionBytecode registry.Kind = "function.lua.bc"
	LibraryBytecode  registry.Kind = "library.lua.bc"
	ProcessBytecode  registry.Kind = "process.lua.bc"
	WorkflowBytecode registry.Kind = "workflow.lua.bc"

	// DefaultMaxSize defines how many concurrent executions are allowed in a flex pool by default
	DefaultMaxSize = 100
)

// PoolTypeLazy is a pool type constant for selecting scheduler implementation.
const (
	PoolTypeLazy     = "lazy"     // Zero processes at idle, creates on demand
	PoolTypeStatic   = "static"   // Fixed-size channel-based pool (default for high load)
	PoolTypeInline   = "inline"   // Synchronous inline execution
	PoolTypeAdaptive = "adaptive" // Auto-scaling pool, scales up under load, down when idle
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
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		Source  string                 `json:"source"`
		Method  string                 `json:"method"`
		Network string                 `json:"network,omitempty"`
		Modules []string               `json:"modules,omitempty"`
		Pool    PoolConfig             `json:"pool,omitempty"`
	}

	// LibraryConfig defines the configuration for a Lua library component.
	// It includes the library source code and required modules.
	LibraryConfig struct {
		Meta    attrs.Bag              `json:"meta"`              // Metadata for the library
		Source  string                 `json:"source"`            // Library source code
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// ProcessConfig defines the configuration for a Lua processes.
	ProcessConfig struct {
		Meta    attrs.Bag              `json:"meta"`              // Metadata for the terminal
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Network string                 `json:"network,omitempty"` // Default overlay network ID
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// WorkflowConfig defines the configuration for a Lua workflow.
	// Workflows have restricted module access for deterministic execution.
	WorkflowConfig struct {
		Meta    attrs.Bag              `json:"meta"`              // Metadata for the workflow
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// BteaConfig defines the configuration for a Lua terminal app, this is custom process with host expectations.
	BteaConfig struct {
		Meta    attrs.Bag              `json:"meta"`              // Metadata for the terminal
		Source  string                 `json:"source"`            // Lua source code
		Method  string                 `json:"method"`            // Alias of the Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Imports aliases for the library
		Modules []string               `json:"modules,omitempty"` // Shortcut for importing modules
	}

	// BytecodeFunctionConfig defines configuration for a precompiled Lua function.
	// The bytecode is loaded from a filesystem and verified by hash before use.
	BytecodeFunctionConfig struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		FS      string                 `json:"fs"`
		Path    string                 `json:"path"`
		Hash    string                 `json:"hash"`
		Method  string                 `json:"method"`
		Network string                 `json:"network,omitempty"`
		Modules []string               `json:"modules,omitempty"`
		Pool    PoolConfig             `json:"pool,omitempty"`
	}

	// BytecodeLibraryConfig defines configuration for a precompiled Lua library.
	BytecodeLibraryConfig struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		FS      string                 `json:"fs"`
		Path    string                 `json:"path"`
		Hash    string                 `json:"hash"`
		Modules []string               `json:"modules,omitempty"`
	}

	// BytecodeProcessConfig defines configuration for a precompiled Lua process.
	BytecodeProcessConfig struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		FS      string                 `json:"fs"`
		Path    string                 `json:"path"`
		Hash    string                 `json:"hash"`
		Method  string                 `json:"method"`
		Network string                 `json:"network,omitempty"`
		Modules []string               `json:"modules,omitempty"`
	}

	// BytecodeWorkflowConfig defines configuration for a precompiled Lua workflow.
	BytecodeWorkflowConfig struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		FS      string                 `json:"fs"`
		Path    string                 `json:"path"`
		Hash    string                 `json:"hash"`
		Method  string                 `json:"method"`
		Modules []string               `json:"modules,omitempty"`
	}

	// BytecodeBteaConfig defines configuration for a precompiled Lua terminal app.
	BytecodeBteaConfig struct {
		Imports map[string]registry.ID `json:"imports,omitempty"`
		Meta    attrs.Bag              `json:"meta,omitempty"`
		FS      string                 `json:"fs"`
		Path    string                 `json:"path"`
		Hash    string                 `json:"hash"`
		Method  string                 `json:"method"`
		Modules []string               `json:"modules,omitempty"`
	}
)

// Validate checks if the FunctionConfig has all required fields set to valid values.
// It returns an error if any validation check fails.
func (c *FunctionConfig) Validate() error {
	if c.Source == "" {
		return ErrSourceRequired
	}

	if c.Method == "" {
		return ErrMethodRequired
	}

	// Pool validation for different pool types
	isFlexPool := c.Pool.Workers == 0 && (c.Pool.Size == 0 || c.Pool.MaxSize > 0)

	// For non-flex pools, validate Size
	if !isFlexPool && c.Pool.Size <= 0 {
		return ErrInvalidPoolSize
	}

	// Worker pools validation
	if c.Pool.Workers > 0 && c.Pool.Size <= 0 {
		return ErrInvalidWorkerPoolSize
	}

	// Validate imports
	for alias, id := range c.Imports {
		if alias == "" {
			return ErrEmptyImportAlias
		}
		if id.Name == "" {
			return ErrEmptyImportName
		}
	}

	// Validate modules
	for _, module := range c.Modules {
		if module == "" {
			return ErrEmptyModule
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return ErrModuleNamespace
		}
	}

	return nil
}

// Validate checks if the LibraryConfig has all required fields set to valid values.
// It returns an error if any validation check fails.
func (c *LibraryConfig) Validate() error {
	if c.Source == "" {
		return ErrSourceRequired
	}

	for alias, id := range c.Imports {
		if alias == "" {
			return ErrEmptyImportAlias
		}
		if id.Name == "" {
			return ErrEmptyImportName
		}
	}

	for _, module := range c.Modules {
		if module == "" {
			return ErrEmptyModule
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return ErrModuleNamespace
		}
	}

	return nil
}

// validateBytecodeBase validates common bytecode config fields.
func validateBytecodeBase(fs, path, hash string) error {
	if fs == "" {
		return ErrFSRequired
	}
	if path == "" {
		return ErrPathRequired
	}
	if hash == "" {
		return ErrHashRequired
	}
	return nil
}

// validateImportsAndModules validates imports and modules fields.
func validateImportsAndModules(imports map[string]registry.ID, modules []string) error {
	for alias, id := range imports {
		if alias == "" {
			return ErrEmptyImportAlias
		}
		if id.Name == "" {
			return ErrEmptyImportName
		}
	}

	for _, module := range modules {
		if module == "" {
			return ErrEmptyModule
		}
		id := registry.ParseID(module)
		if id.NS != "" {
			return ErrModuleNamespace
		}
	}

	return nil
}

// Validate checks if the BytecodeFunctionConfig has all required fields.
func (c *BytecodeFunctionConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validateImportsAndModules(c.Imports, c.Modules); err != nil {
		return err
	}

	isFlexPool := c.Pool.Workers == 0 && (c.Pool.Size == 0 || c.Pool.MaxSize > 0)
	if !isFlexPool && c.Pool.Size <= 0 {
		return ErrInvalidPoolSize
	}
	if c.Pool.Workers > 0 && c.Pool.Size <= 0 {
		return ErrInvalidWorkerPoolSize
	}

	return nil
}

// Validate checks if the BytecodeLibraryConfig has all required fields.
func (c *BytecodeLibraryConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	return validateImportsAndModules(c.Imports, c.Modules)
}

// Validate checks if the BytecodeProcessConfig has all required fields.
func (c *BytecodeProcessConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	return validateImportsAndModules(c.Imports, c.Modules)
}

// Validate checks if the BytecodeWorkflowConfig has all required fields.
func (c *BytecodeWorkflowConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	return validateImportsAndModules(c.Imports, c.Modules)
}

// Validate checks if the BytecodeBteaConfig has all required fields.
func (c *BytecodeBteaConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	return validateImportsAndModules(c.Imports, c.Modules)
}
