package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Validate validates the entire lock file structure.
// Returns an error if any validation fails.
func Validate(l *Lock) error {
	for _, mod := range l.data.Modules {
		if err := ValidateModuleName(mod.Name); err != nil {
			return fmt.Errorf("invalid module %q: %w", mod.Name, err)
		}

		if mod.Version == "" {
			return fmt.Errorf("module %q has empty version", mod.Name)
		}
	}

	if err := ValidateReplacements(l.path, l.data.Replacements); err != nil {
		return fmt.Errorf("invalid replacements: %w", err)
	}

	if l.data.Directories.Modules == "" {
		return fmt.Errorf("directories.modules cannot be empty")
	}

	if l.data.Directories.Src == "" {
		return fmt.Errorf("directories.src cannot be empty")
	}

	if l.data.Directories.Src == "." {
		return fmt.Errorf("directories.src cannot be \".\" (root directory) - this causes duplicate loading of vendor modules. Use a specific subdirectory like \"./src\" instead")
	}

	return nil
}

// ValidateReplacements checks that all replacement paths exist.
// Paths are resolved relative to the lock file directory.
func ValidateReplacements(lockPath string, replacements []Replacement) error {
	lockDir := filepath.Dir(lockPath)

	for _, r := range replacements {
		if r.From == "" {
			return fmt.Errorf("replacement has empty 'from' field")
		}

		if r.To == "" {
			return fmt.Errorf("replacement %q has empty 'to' field", r.From)
		}

		if err := ValidateModuleName(r.From); err != nil {
			return fmt.Errorf("replacement 'from' field %q: %w", r.From, err)
		}

		replacementPath := r.To
		if !filepath.IsAbs(replacementPath) {
			replacementPath = filepath.Join(lockDir, r.To)
		}

		if _, err := os.Stat(replacementPath); err != nil {
			if os.IsNotExist(err) {
				return fmt.Errorf("replacement path %q does not exist", r.To)
			}
			return fmt.Errorf("check replacement path %q: %w", r.To, err)
		}
	}

	return nil
}

// ValidateModuleName validates that a module name follows the org/module format.
func ValidateModuleName(name string) error {
	if name == "" {
		return fmt.Errorf("module name cannot be empty")
	}

	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return fmt.Errorf("invalid format (expected org/module, got %q)", name)
	}

	if parts[0] == "" {
		return fmt.Errorf("organization part cannot be empty")
	}

	if parts[1] == "" {
		return fmt.Errorf("module part cannot be empty")
	}

	return nil
}
