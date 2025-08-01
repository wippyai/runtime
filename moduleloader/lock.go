package moduleloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/goccy/go-yaml"
)

// LockFile represents the structure of wippy.lock file
type LockFile struct {
	Directory string         `yaml:"directory"`
	Modules   []LockedModule `yaml:"modules"`
}

// LockedModule represents a locked dependency in the lock file
type LockedModule struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
}

// LoadLockFile loads the lock file from the given path
func LoadLockFile(path string) (*LockFile, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read lock file: %w", err)
	}

	var lockFile LockFile
	if err := yaml.Unmarshal(data, &lockFile); err != nil {
		return nil, fmt.Errorf("unmarshal lock file: %w", err)
	}

	return &lockFile, nil
}

// SaveLockFile saves the lock file to the given path
func (lf *LockFile) SaveLockFile(path string) error {
	// Sort modules by name for consistent output
	sort.Slice(lf.Modules, func(i, j int) bool {
		return lf.Modules[i].Name < lf.Modules[j].Name
	})

	data, err := yaml.Marshal(lf)
	if err != nil {
		return fmt.Errorf("marshal lock file: %w", err)
	}

	if err := os.WriteFile(path, data, 0600); err != nil {
		return fmt.Errorf("write lock file: %w", err)
	}

	return nil
}

// FindLockFile searches for wippy.lock file in the given directory and its parents
func FindLockFile(dir string) (string, error) {
	current := dir
	for {
		lockPath := filepath.Join(current, "wippy.lock")
		if _, err := os.Stat(lockPath); err == nil {
			return lockPath, nil
		}

		parent := filepath.Dir(current)
		if parent == current {
			// Reached root directory
			return "", fmt.Errorf("wippy.lock not found in %s or any parent directory", dir)
		}
		current = parent
	}
}

// ConvertToLockFile converts LoadResult to LockFile
func ConvertToLockFile(loadResult *LoadResult) *LockFile {
	modules := make([]LockedModule, 0, len(loadResult.Modules))
	for _, module := range loadResult.Modules {
		modules = append(modules, LockedModule{
			Name:    module.Name.String(),
			Version: module.Version,
		})
	}

	// Sort modules by name for consistent output
	sort.Slice(modules, func(i, j int) bool {
		return modules[i].Name < modules[j].Name
	})

	return &LockFile{
		Directory: VendorFolder,
		Modules:   modules,
	}
}

// ParseName parses a string into a Name struct
func ParseName(nameStr string) (Name, error) {
	var name Name
	err := name.SetName(nameStr)
	return name, err
}

// ConvertFromLockFile converts LockFile to LoadResult
func ConvertFromLockFile(lockFile *LockFile) *LoadResult {
	modules := make([]LoadedModule, 0, len(lockFile.Modules))
	for _, module := range lockFile.Modules {
		// Parse the name string back to Name struct
		name, err := ParseName(module.Name)
		if err != nil {
			// Skip invalid names
			continue
		}
		modules = append(modules, LoadedModule{
			Name:    name,
			Version: module.Version,
			Path:    filepath.Join(lockFile.Directory, module.Name),
		})
	}

	return &LoadResult{
		Modules: modules,
	}
}
