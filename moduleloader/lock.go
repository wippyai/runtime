package moduleloader

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/goccy/go-yaml"
)

const DefaultLockFile = "wippy.lock"

// LockFile represents the structure of wippy.lock file
type LockFile struct {
	Directory string         `yaml:"directory"`
	Modules   []LockedModule `yaml:"modules"`
}

// LockedModule represents a locked dependency in the lock file
type LockedModule struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Hash    string `yaml:"hash,omitempty"`
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

// FindLockFile searches for lock file in the project directory
func FindLockFile(dir string, file string) (string, error) {
	lockPath := filepath.Join(dir, file)
	if _, err := os.Stat(lockPath); err == nil {
		return lockPath, nil
	}

	// when default file not found - start wippy without lock file
	if file == DefaultLockFile {
		return "", nil
	}

	return "", fmt.Errorf("%s not found in project directory: %s", file, dir)
}

// ConvertToLockFile converts LoadResult to LockFile
func ConvertToLockFile(loadResult *LoadResult) *LockFile {
	modules := make([]LockedModule, 0, len(loadResult.Modules))
	for _, module := range loadResult.Modules {
		// Extract hash from the path if it contains "@"
		var hash string
		if strings.Contains(module.Path, "@") {
			parts := strings.Split(filepath.Base(module.Path), "@")
			if len(parts) == 2 {
				hash = parts[1]
			}
		}

		modules = append(modules, LockedModule{
			Name:    module.Name.String(),
			Version: module.Version,
			Hash:    hash,
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

		// Use the full organization structure for the path
		modulePath := filepath.Join(name.Organization, name.Module)
		if module.Hash != "" {
			modulePath = filepath.Join(name.Organization, name.Module+"@"+module.Hash)
		}

		modules = append(modules, LoadedModule{
			Name:    name,
			Version: module.Version,
			Path:    filepath.Join(lockFile.Directory, modulePath),
		})
	}

	return &LoadResult{
		Modules: modules,
	}
}
