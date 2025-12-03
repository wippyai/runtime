// Package lua provides Lua runtime integration.
package lua

import (
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

	// Bytecode kinds - precompiled Lua loaded from filesystem
	KindFunctionBytecode registry.Kind = "function.lua.bc"
	KindLibraryBytecode  registry.Kind = "library.lua.bc"
	KindProcessBytecode  registry.Kind = "process.lua.bc"
	KindWorkflowBytecode registry.Kind = "workflow.lua.bc"
	KindBteaAppBytecode  registry.Kind = "btea.app.lua.bc"

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

	// BytecodeFunctionConfig defines configuration for a precompiled Lua function.
	// The bytecode is loaded from a filesystem and verified by hash before use.
	BytecodeFunctionConfig struct {
		FS      string                 `json:"fs"`                // Filesystem entry ID (e.g., "app:lua.bytecode")
		Path    string                 `json:"path"`              // Path within filesystem to .luac file
		Hash    string                 `json:"hash"`              // Required SHA256 hash (e.g., "sha256:abc123...")
		Method  string                 `json:"method"`            // Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Import aliases for libraries
		Modules []string               `json:"modules,omitempty"` // Built-in modules to load
		Pool    PoolConfig             `json:"pool,omitempty"`    // VM pool configuration
		Meta    registry.Metadata      `json:"meta,omitempty"`
	}

	// BytecodeLibraryConfig defines configuration for a precompiled Lua library.
	BytecodeLibraryConfig struct {
		FS      string                 `json:"fs"`                // Filesystem entry ID
		Path    string                 `json:"path"`              // Path within filesystem to .luac file
		Hash    string                 `json:"hash"`              // Required SHA256 hash
		Imports map[string]registry.ID `json:"imports,omitempty"` // Import aliases for libraries
		Modules []string               `json:"modules,omitempty"` // Built-in modules to load
		Meta    registry.Metadata      `json:"meta,omitempty"`
	}

	// BytecodeProcessConfig defines configuration for a precompiled Lua process.
	BytecodeProcessConfig struct {
		FS      string                 `json:"fs"`                // Filesystem entry ID
		Path    string                 `json:"path"`              // Path within filesystem to .luac file
		Hash    string                 `json:"hash"`              // Required SHA256 hash
		Method  string                 `json:"method"`            // Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Import aliases for libraries
		Modules []string               `json:"modules,omitempty"` // Built-in modules to load
		Meta    registry.Metadata      `json:"meta,omitempty"`
	}

	// BytecodeWorkflowConfig defines configuration for a precompiled Lua workflow.
	BytecodeWorkflowConfig struct {
		FS      string                 `json:"fs"`                // Filesystem entry ID
		Path    string                 `json:"path"`              // Path within filesystem to .luac file
		Hash    string                 `json:"hash"`              // Required SHA256 hash
		Method  string                 `json:"method"`            // Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Import aliases for libraries
		Modules []string               `json:"modules,omitempty"` // Built-in modules to load
		Meta    registry.Metadata      `json:"meta,omitempty"`
	}

	// BytecodeBteaConfig defines configuration for a precompiled Lua terminal app.
	BytecodeBteaConfig struct {
		FS      string                 `json:"fs"`                // Filesystem entry ID
		Path    string                 `json:"path"`              // Path within filesystem to .luac file
		Hash    string                 `json:"hash"`              // Required SHA256 hash
		Method  string                 `json:"method"`            // Lua method to execute
		Imports map[string]registry.ID `json:"imports,omitempty"` // Import aliases for libraries
		Modules []string               `json:"modules,omitempty"` // Built-in modules to load
		Meta    registry.Metadata      `json:"meta,omitempty"`
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
		return ErrSourceRequired
	}

	if c.Method == "" {
		return ErrMethodRequired
	}

	// Pool validation for different pool types
	isFlexPool := c.Pool.Workers == 0 && (c.Pool.Size == 0 || c.Pool.MaxSize > 0)

	// For non-flex pools, validate Size
	if !isFlexPool && c.Pool.Size <= 0 {
		return NewInvalidPoolSizeError()
	}

	// For flex pools, validate MaxSize
	//nolint:revive,staticcheck
	if isFlexPool && c.Pool.MaxSize <= 0 {
	}

	// Worker pools validation
	if c.Pool.Workers > 0 && c.Pool.Size <= 0 {
		return NewInvalidWorkerPoolSizeError()
	}

	// Validate imports
	for alias, id := range c.Imports {
		if alias == "" {
			return ErrEmptyImportAlias
		}
		if id.Name == "" {
			return NewEmptyImportNameError()
		}
	}

	// Validate modules
	for _, module := range c.Modules {
		if module == "" {
			return ErrEmptyModule
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return NewModuleNamespaceError()
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
			return NewEmptyImportNameError()
		}
	}

	for _, module := range c.Modules {
		if module == "" {
			return ErrEmptyModule
		}

		id := registry.ParseID(module)
		if id.NS != "" {
			return NewModuleNamespaceError()
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
			return NewEmptyImportNameError()
		}
	}

	for _, module := range modules {
		if module == "" {
			return ErrEmptyModule
		}
		id := registry.ParseID(module)
		if id.NS != "" {
			return NewModuleNamespaceError()
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
		return NewInvalidPoolSizeError()
	}
	if c.Pool.Workers > 0 && c.Pool.Size <= 0 {
		return NewInvalidWorkerPoolSizeError()
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
