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
	selectedModules := make(map[string]struct{}, len(l.data.Modules))
	for _, mod := range l.data.Modules {
		if err := ValidateModuleName(mod.Name); err != nil {
			return NewInvalidModuleError(mod.Name, err)
		}

		if mod.Version == "" {
			return NewModuleEmptyVersionError(mod.Name)
		}
		if mod.Root {
			rootCount++
		}
		selectedModules[mod.Name] = struct{}{}
	}
	if rootCount > 1 {
		return NewMultipleRootModulesError()
	}

	if err := validateReplacements(l.path, l.GetReplacements(), selectedModules); err != nil {
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
	return validateReplacements(lockPath, replacements, nil)
}

// validateReplacements always validates declaration shape. When selected is
// non-nil, only replacements in the selected lock graph require a live source
// path; unselected workspace sources are resolver inputs, not boot inputs.
func validateReplacements(lockPath string, replacements []Replacement, selected map[string]struct{}) error {
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
		if selected != nil {
			if _, ok := selected[r.From]; !ok {
				continue
			}
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
