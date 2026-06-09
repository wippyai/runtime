// SPDX-License-Identifier: MPL-2.0

package lock

// File represents the structure of a wippy.lock file.
type File struct {
	Directories  Directories   `yaml:"directories"`
	Modules      []Module      `yaml:"modules,omitempty"`
	Replacements []Replacement `yaml:"replacements,omitempty"`
	Options      Options       `yaml:"options,omitempty"`
}

// Options specifies runtime behavior for module loading.
type Options struct {
	UnpackModules bool `yaml:"unpack_modules,omitempty"` // Extract .wapp to directories (default: false)
}

// Directories specifies paths for module storage and source scanning.
type Directories struct {
	Modules string `yaml:"modules"` // Base directory for vendor storage (e.g., .wippy)
	Src     string `yaml:"src"`     // Source directory to scan for dependencies (e.g., .)
}

// Module represents a locked dependency.
type Module struct {
	Name      string `yaml:"name"`                 // Module identifier in org/module format
	Version   string `yaml:"version"`              // Semantic version (e.g., v0.0.11)
	Hash      string `yaml:"hash,omitempty"`       // Manifest digest (e.g. sha256:...), populated on install
	LocalHash string `yaml:"local_hash,omitempty"` // Computed hash from loaded entries for verification
}

// Replacement represents a local module override for development.
type Replacement struct {
	From string `yaml:"from"` // Module name to replace
	To   string `yaml:"to"`   // Local filesystem path (relative to lock file)
}

// Changes represents the differences between two lock files.
type Changes struct {
	Installed []Module       // Newly added modules
	Updated   []ModuleChange // Modules with version/hash changes
	Removed   []Module       // Removed modules
}

// ModuleChange represents a module that changed between lock files.
type ModuleChange struct {
	Name       string // Module name
	OldVersion string // Previous version
	NewVersion string // New version
	OldHash    string // Previous hash
	NewHash    string // New hash
}
