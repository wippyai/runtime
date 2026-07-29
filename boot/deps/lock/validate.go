// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"strings"
)

// Validate validates the entire lock file structure.
// Returns an error if any validation fails.
func Validate(l *Lock) error {
	rootCount := 0
	for _, mod := range l.data.Modules {
		if err := ValidateModuleName(mod.Name); err != nil {
			return NewInvalidModuleError(mod.Name, err)
		}

		if mod.Version == "" {
			return NewModuleEmptyVersionError(mod.Name)
		}
		if mod.Root {
			if mod.BuildOnly {
				return NewBuildOnlyRootError(mod.Name)
			}
			rootCount++
		}
	}
	if rootCount > 1 {
		return NewMultipleRootModulesError()
	}

	if err := ValidateReplacements(l.path, l.GetReplacements()); err != nil {
		return NewInvalidReplacementsError(err)
	}

	if l.data.Directories.Modules == "" {
		return ErrModulesDirectoryEmpty
	}

	if l.data.Directories.Src == "" {
		return ErrSrcDirectoryEmpty
	}

	return nil
}

// ValidateReplacements checks that all replacement paths exist.
// Paths are resolved relative to the lock file directory.
func ValidateReplacements(lockPath string, replacements []Replacement) error {
	lockDir := filepath.Dir(lockPath)

	for _, r := range replacements {
		if r.From == "" {
			return ErrReplacementFromEmpty
		}

		if r.To == "" {
			return NewReplacementToEmptyError(r.From)
		}

		if err := ValidateModuleName(r.From); err != nil {
			return NewReplacementFromInvalidError(r.From, err)
		}

		replacementPath := ResolveLockPath(lockDir, r.To)

		info, err := os.Stat(replacementPath)
		if err != nil {
			if os.IsNotExist(err) {
				return NewReplacementPathNotExistError(r.To)
			}
			return NewCheckReplacementPathError(r.To, err)
		}
		if !info.IsDir() {
			return NewReplacementPathNotDirectoryError(r.To)
		}
	}

	return nil
}

// ValidateModuleName validates that a module name follows the org/module format.
func ValidateModuleName(name string) error {
	if name == "" {
		return ErrModuleNameEmpty
	}

	parts := strings.Split(name, "/")
	if len(parts) != 2 {
		return NewInvalidFormatError(name)
	}

	if parts[0] == "" {
		return ErrOrganizationEmpty
	}

	if parts[1] == "" {
		return ErrModulePartEmpty
	}

	return nil
}
