package wasm

import "github.com/wippyai/runtime/api/attrs"

// Pool type constants for selecting scheduler implementation.
const (
	PoolTypeLazy   = "lazy"   // Creates instance on demand
	PoolTypeStatic = "static" // Fixed-size pool (default)
	PoolTypeInline = "inline" // Synchronous inline execution
)

// PoolConfig defines settings for a pool of WASM instances.
type PoolConfig struct {
	Type   string `json:"type"`   // Pool type: static, lazy, inline
	Size   int    `json:"size"`   // Number of workers
	Buffer int    `json:"buffer"` // Task queue buffer size
}

// WATFunctionConfig defines configuration for a WASM function with inline WAT source.
type WATFunctionConfig struct {
	Meta   attrs.Bag  `json:"meta,omitempty"`
	Source string     `json:"source"`
	WIT    string     `json:"wit"`
	Method string     `json:"method"`
	Pool   PoolConfig `json:"pool,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *WATFunctionConfig) Validate() error {
	if c.Source == "" {
		return ErrSourceRequired
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validatePool(&c.Pool); err != nil {
		return err
	}
	return nil
}

// FunctionConfig defines configuration for a precompiled WASM binary.
type FunctionConfig struct {
	Meta   attrs.Bag  `json:"meta,omitempty"`
	FS     string     `json:"fs"`
	Path   string     `json:"path"`
	Hash   string     `json:"hash"`
	Method string     `json:"method"`
	Pool   PoolConfig `json:"pool,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *FunctionConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validatePool(&c.Pool); err != nil {
		return err
	}
	return nil
}

// ComponentFunctionConfig defines configuration for a WebAssembly Component Model binary.
type ComponentFunctionConfig struct {
	Meta      attrs.Bag  `json:"meta,omitempty"`
	FS        string     `json:"fs"`
	Path      string     `json:"path"`
	Hash      string     `json:"hash"`
	Method    string     `json:"method"`
	Transport string     `json:"transport,omitempty"`
	Pool      PoolConfig `json:"pool,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *ComponentFunctionConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	if err := validatePool(&c.Pool); err != nil {
		return err
	}
	return nil
}

// ProcessConfig defines configuration for a long-running WASM process.
type ProcessConfig struct {
	Meta   attrs.Bag `json:"meta,omitempty"`
	FS     string    `json:"fs"`
	Path   string    `json:"path"`
	Hash   string    `json:"hash"`
	Method string    `json:"method"`
}

// Validate checks if the config has all required fields.
func (c *ProcessConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	return nil
}

// ComponentProcessConfig defines configuration for a Component Model process.
type ComponentProcessConfig struct {
	Meta      attrs.Bag `json:"meta,omitempty"`
	FS        string    `json:"fs"`
	Path      string    `json:"path"`
	Hash      string    `json:"hash"`
	Method    string    `json:"method"`
	Transport string    `json:"transport,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *ComponentProcessConfig) Validate() error {
	if err := validateBytecodeBase(c.FS, c.Path, c.Hash); err != nil {
		return err
	}
	if c.Method == "" {
		return ErrMethodRequired
	}
	return nil
}

// validateBytecodeBase validates common fields for filesystem-loaded configs.
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

// validatePool validates pool configuration.
func validatePool(p *PoolConfig) error {
	if p.Size <= 0 && p.Type != PoolTypeInline && p.Type != PoolTypeLazy && p.Type != "" {
		return ErrInvalidPoolSize
	}
	return nil
}
