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
	Directories  Directories    `yaml:"directories"`
	Modules      []LockedModule `yaml:"modules"`
	Replacements []Replacement  `yaml:"replacements,omitempty"`
}

// Directories represents the directories section in the lock file
type Directories struct {
	Modules string `yaml:"modules"`
	Src     string `yaml:"src"`
}

// LockedModule represents a locked dependency in the lock file
type LockedModule struct {
	Name    string `yaml:"name"`
	Version string `yaml:"version"`
	Hash    string `yaml:"hash,omitempty"`
}

// Replacement represents a module replacement directive
type Replacement struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
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
	// Deduplicate modules by name and version before saving
	moduleMap := make(map[string]LockedModule)
	for _, module := range lf.Modules {
		key := module.Name + "@" + module.Version
		moduleMap[key] = module
	}

	// Convert map back to slice
	lf.Modules = make([]LockedModule, 0, len(moduleMap))
	for _, module := range moduleMap {
		lf.Modules = append(lf.Modules, module)
	}

	// Sort modules by name for consistent output
	sort.Slice(lf.Modules, func(i, j int) bool {
		return lf.Modules[i].Name < lf.Modules[j].Name
	})

	// Sort replacements by from field for consistent output
	sort.Slice(lf.Replacements, func(i, j int) bool {
		return lf.Replacements[i].From < lf.Replacements[j].From
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

// FindLockFile searches for lock file with intelligent path resolution
// It tries multiple strategies to find the lock file:
// 1. If file is already absolute, use it directly
// 2. If file is relative, try to resolve it relative to dir
// 3. If dir is empty, try to resolve relative to current working directory
// 4. For default lock file, return empty string if not found (allows fallback)
func FindLockFile(dir string, file string) (string, error) {
	// If file is already an absolute path, use it directly
	if filepath.IsAbs(file) {
		if _, err := os.Stat(file); err == nil {
			return file, nil
		}
		return "", fmt.Errorf("absolute lock file path does not exist: %s", file)
	}

	// Determine the base directory for relative path resolution
	baseDir := dir
	if baseDir == "" {
		// If no directory specified, use current working directory
		var err error
		baseDir, err = os.Getwd()
		if err != nil {
			return "", fmt.Errorf("failed to get current working directory: %w", err)
		}
	}

	// Try to resolve the file relative to the base directory
	lockPath := filepath.Join(baseDir, file)
	if _, err := os.Stat(lockPath); err == nil {
		return lockPath, nil
	}

	// For default lock file, return empty string if not found (allows fallback)
	if file == DefaultLockFile {
		return "", nil
	}

	return "", fmt.Errorf("lock file %s not found in directory: %s", file, baseDir)
}

// ConvertToLockFile converts LoadResult to LockFile
func ConvertToLockFile(loadResult *LoadResult, modulesDir, srcDir string) *LockFile {
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
		Directories: Directories{
			Modules: modulesDir,
			Src:     srcDir,
		},
		Modules: modules,
		// Replacements are not automatically generated, they must be manually added
	}
}

// ParseName parses a string into a Name struct
func ParseName(nameStr string) (Name, error) {
	var name Name
	err := name.SetName(nameStr)
	return name, err
}

// ConvertFromLockFile converts LockFile to LoadResult
func ConvertFromLockFile(lockFile *LockFile, lockFilePath string) *LoadResult {
	modules := make([]LoadedModule, 0, len(lockFile.Modules))

	// Create a map of replacements for quick lookup
	replacements := make(map[string]string)
	for _, replacement := range lockFile.Replacements {
		replacements[replacement.From] = replacement.To
	}

	for _, module := range lockFile.Modules {
		// Parse the name string back to Name struct
		name, err := ParseName(module.Name)
		if err != nil {
			// Skip invalid names
			continue
		}

		// Check if this module has a replacement
		if customPath, hasReplacement := replacements[module.Name]; hasReplacement {
			// Resolve the custom path to an absolute path relative to the lock file location
			// The customPath is relative to the lock file, so we need to resolve it
			resolvedPath := filepath.Join(filepath.Dir(lockFilePath), customPath)
			// Convert to absolute path
			if absPath, err := filepath.Abs(resolvedPath); err == nil {
				resolvedPath = absPath
			}

			// Use the custom path from replacement
			modules = append(modules, LoadedModule{
				Name:    name,
				Version: module.Version,
				Path:    resolvedPath, // Resolved absolute path
			})
		} else {
			// Use the default module path
			modulePath := filepath.Join(name.Organization, name.Module)
			if module.Hash != "" {
				modulePath = filepath.Join(name.Organization, name.Module+"@"+module.Hash)
			}

			// Use the modules directory from the lock file
			modulesDir := lockFile.Directories.Modules
			if modulesDir == "" {
				modulesDir = VendorFolder // fallback to default
			}

			modules = append(modules, LoadedModule{
				Name:    name,
				Version: module.Version,
				Path:    filepath.Join(modulesDir, modulePath),
			})
		}
	}

	return &LoadResult{
		Modules: modules,
	}
}

// ValidateReplacements validates that all replacement paths exist
func (lf *LockFile) ValidateReplacements(lockFilePath string) error {
	for _, replacement := range lf.Replacements {
		// Resolve the custom path relative to the lock file location
		customPath := filepath.Join(filepath.Dir(lockFilePath), replacement.To)

		// Check if the path exists
		if _, err := os.Stat(customPath); os.IsNotExist(err) {
			return fmt.Errorf("replacement path does not exist: %s -> %s (resolved to: %s)",
				replacement.From, replacement.To, customPath)
		}
	}
	return nil
}
