package lua

import (
	"fmt"
	"github.com/ponyruntime/pony/api/supervisor"

	"github.com/ponyruntime/pony/api/registry"
)

// Registry kind constants for different Lua component types.
// These are used to identify different types of Lua-based components in the registry.
const (
	// KindFunction identifies a Lua function component in the registry
	KindFunction registry.Kind = "function.lua"
	// KindLibrary identifies a Lua library component in the registry
	KindLibrary registry.Kind = "library.lua"
	// KindTerminal identifies a Lua terminal component in the registry
	KindTerminal registry.Kind = "terminal.lua"
)

type (
	// FunctionConfig defines the configuration for a Lua function component.
	// It includes the source code, execution method, required libraries and modules,
	// and VM pool settings.
	FunctionConfig struct {
		Meta      registry.Metadata `json:"meta"`      // Metadata for the function
		Source    string            `json:"source"`    // Lua source code
		Method    string            `json:"method"`    // Name of the Lua method to execute
		Libraries []string          `json:"libraries"` // Required Lua libraries
		Modules   []string          `json:"modules"`   // Required Lua modules
		Pool      PoolConfig        `json:"pool"`      // VM pool configuration
	}

	// TerminalConfig defines the configuration for a Lua terminal component.
	// It extends FunctionConfig with terminal-specific options and lifecycle management.
	TerminalConfig struct {
		Meta      registry.Metadata          `json:"meta"`      // Metadata for the terminal
		Source    string                     `json:"source"`    // Lua source code
		Method    string                     `json:"method"`    // Name of the Lua method to execute
		Libraries []string                   `json:"libraries"` // Required Lua libraries
		Modules   []string                   `json:"modules"`   // Required Lua modules
		Options   TerminalOptions            `json:"options"`   // Terminal-specific options
		Lifecycle supervisor.LifecycleConfig `json:"lifecycle"` // Lifecycle management config
	}

	// PoolConfig defines settings for a pool of Lua VMs.
	// It manages the number of VMs and workers available for executing Lua code.
	PoolConfig struct {
		Size    int `json:"size"`    // Total number of VMs in the pool
		Workers int `json:"workers"` // Number of worker threads
	}

	// LibraryConfig defines the configuration for a Lua library component.
	// It includes the library source code and required modules.
	LibraryConfig struct {
		Meta    registry.Metadata `json:"meta"`    // Metadata for the library
		Source  string            `json:"source"`  // Library source code
		Modules []string          `json:"modules"` // Required Lua modules
	}

	// TerminalOptions provides configuration options for a BubbleTea terminal,
	// controlling display settings and behavior. @todo deprecate?
	TerminalOptions struct {
		// UseAltScreen determines if the terminal should use the alternate screen buffer
		UseAltScreen bool `json:"alt_screen"`

		// Title sets the terminal window title
		Title string `json:"title,omitempty"`

		// MouseMode determines the type of mouse support
		MouseMode string `json:"mouse,omitempty"`

		// DisableSignals prevents handling of signals (ctrl+c, etc)
		DisableSignals bool `json:"disable_signals,omitempty"`
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

	if c.Pool.Size <= 0 {
		return fmt.Errorf("pool.num_vms must be greater than 0")
	}

	return nil
}
