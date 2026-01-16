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
	Source string     `json:"source"` // Inline WAT source code
	WIT    string     `json:"wit"`    // WIT type definitions (optional)
	Method string     `json:"method"` // Exported function name
	Pool   PoolConfig `json:"pool,omitempty"`
	Meta   attrs.Bag  `json:"meta,omitempty"`
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

// WASMFunctionConfig defines configuration for a precompiled WASM binary.
type WASMFunctionConfig struct {
	FS     string     `json:"fs"`     // Filesystem entry ID (e.g., "app:wasm.files")
	Path   string     `json:"path"`   // Path within filesystem to .wasm file
	Hash   string     `json:"hash"`   // Required hash (e.g., "sha256:abc123...")
	Method string     `json:"method"` // Exported function name
	Pool   PoolConfig `json:"pool,omitempty"`
	Meta   attrs.Bag  `json:"meta,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *WASMFunctionConfig) Validate() error {
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
	FS        string     `json:"fs"`                  // Filesystem entry ID
	Path      string     `json:"path"`                // Path to .wasm component file
	Hash      string     `json:"hash"`                // Required hash
	Method    string     `json:"method"`              // Exported function name
	Transport string     `json:"transport,omitempty"` // Transport type: payload (default), wasi-http
	Pool      PoolConfig `json:"pool,omitempty"`
	Meta      attrs.Bag  `json:"meta,omitempty"`
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

// WASMProcessConfig defines configuration for a long-running WASM process.
type WASMProcessConfig struct {
	FS     string    `json:"fs"`     // Filesystem entry ID
	Path   string    `json:"path"`   // Path to .wasm file
	Hash   string    `json:"hash"`   // Required hash
	Method string    `json:"method"` // Entry point function
	Meta   attrs.Bag `json:"meta,omitempty"`
}

// Validate checks if the config has all required fields.
func (c *WASMProcessConfig) Validate() error {
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
	FS        string    `json:"fs"`                  // Filesystem entry ID
	Path      string    `json:"path"`                // Path to .wasm component file
	Hash      string    `json:"hash"`                // Required hash
	Method    string    `json:"method"`              // Entry point function
	Transport string    `json:"transport,omitempty"` // Transport type
	Meta      attrs.Bag `json:"meta,omitempty"`
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
		return NewInvalidPoolSizeError()
	}
	return nil
}
