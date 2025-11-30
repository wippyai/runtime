// Package filesystem implements wasi:filesystem@0.2.0 for wippy.
// Provides filesystem access through wippy's filesystem registry.
package filesystem

import (
	ctxapi "github.com/wippyai/runtime/api/context"
)

// DefaultFSKey stores the default filesystem ID for a WASM instance.
// Marked Inherit: true so child agents get the same default filesystem.
var DefaultFSKey = &ctxapi.Key{
	Name:    "wasm.filesystem.default",
	Inherit: true,
}

// Config holds filesystem configuration for a WASM instance.
type Config struct {
	// DefaultFS is the filesystem ID to use when no specific path is given.
	// Maps to a filesystem in the registry (e.g., "local", "app", "temp").
	DefaultFS string

	// AllowedFS is the list of filesystem IDs this instance can access.
	// Empty means all filesystems in registry are accessible.
	AllowedFS []string

	// RootPath is the path prefix within the filesystem.
	// Acts as a chroot for this instance.
	RootPath string
}

// Clone implements ctxapi.Cloner for safe inheritance.
func (c *Config) Clone() any {
	if c == nil {
		return nil
	}
	clone := &Config{
		DefaultFS: c.DefaultFS,
		RootPath:  c.RootPath,
	}
	if len(c.AllowedFS) > 0 {
		clone.AllowedFS = make([]string, len(c.AllowedFS))
		copy(clone.AllowedFS, c.AllowedFS)
	}
	return clone
}

// IsAllowed checks if a filesystem ID is allowed for this config.
func (c *Config) IsAllowed(fsID string) bool {
	if c == nil || len(c.AllowedFS) == 0 {
		return true
	}
	for _, allowed := range c.AllowedFS {
		if allowed == fsID {
			return true
		}
	}
	return false
}
