package lock

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/wippyai/runtime/boot/deps/graph"
	"gopkg.in/yaml.v3"
)

// Lock represents a lock file with operations for reading, writing, and querying.
type Lock struct {
	path string
	data LockFile
}

// New creates a new Lock instance from the given path.
// If the file exists, it loads the content. Otherwise, creates an empty lock with default directories.
func New(path string) (*Lock, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, fmt.Errorf("resolve absolute path: %w", err)
	}

	lock := &Lock{
		path: absPath,
		data: LockFile{
			Directories: Directories{
				Modules: ".wippy",
				Src:     ".",
			},
			Modules:      []Module{},
			Replacements: []Replacement{},
		},
	}

	if _, err := os.Stat(absPath); err == nil {
		if err := lock.Read(); err != nil {
			return nil, fmt.Errorf("read lock file: %w", err)
		}
	} else if !os.IsNotExist(err) {
		return nil, fmt.Errorf("stat lock file: %w", err)
	}

	return lock, nil
}

// Read loads the lock file from disk.
func (l *Lock) Read() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return fmt.Errorf("read file: %w", err)
	}

	if err := yaml.Unmarshal(data, &l.data); err != nil {
		return fmt.Errorf("unmarshal yaml: %w", err)
	}

	return nil
}

// Write saves the lock file to disk with deduplication and sorting.
// Modules are sorted alphabetically by name, replacements are sorted by from field.
// File is written with 0600 permissions.
func (l *Lock) Write() error {
	l.deduplicate()
	l.sort()

	data, err := yaml.Marshal(&l.data)
	if err != nil {
		return fmt.Errorf("marshal yaml: %w", err)
	}

	if err := os.WriteFile(l.path, data, 0600); err != nil {
		return fmt.Errorf("write file: %w", err)
	}

	return nil
}

// Path returns the absolute path to the lock file.
func (l *Lock) Path() string {
	return l.path
}

// GetModule retrieves a module by name.
// Returns the module and true if found, zero value and false otherwise.
func (l *Lock) GetModule(name string) (Module, bool) {
	for _, mod := range l.data.Modules {
		if mod.Name == name {
			return mod, true
		}
	}
	return Module{}, false
}

// SetModule adds or updates a module.
// If a module with the same name exists, it is replaced.
func (l *Lock) SetModule(module Module) {
	for i, mod := range l.data.Modules {
		if mod.Name == module.Name {
			l.data.Modules[i] = module
			return
		}
	}
	l.data.Modules = append(l.data.Modules, module)
}

// RemoveModule removes a module by name.
func (l *Lock) RemoveModule(name string) {
	filtered := make([]Module, 0, len(l.data.Modules))
	for _, mod := range l.data.Modules {
		if mod.Name != name {
			filtered = append(filtered, mod)
		}
	}
	l.data.Modules = filtered
}

// GetModules returns all modules.
func (l *Lock) GetModules() []Module {
	return l.data.Modules
}

// GetReplacement retrieves a replacement by from field.
// Returns the replacement and true if found, zero value and false otherwise.
func (l *Lock) GetReplacement(from string) (Replacement, bool) {
	for _, r := range l.data.Replacements {
		if r.From == from {
			return r, true
		}
	}
	return Replacement{}, false
}

// SetReplacement adds or updates a replacement.
// If a replacement with the same from field exists, it is replaced.
func (l *Lock) SetReplacement(replacement Replacement) {
	for i, r := range l.data.Replacements {
		if r.From == replacement.From {
			l.data.Replacements[i] = replacement
			return
		}
	}
	l.data.Replacements = append(l.data.Replacements, replacement)
}

// RemoveReplacement removes a replacement by from field.
func (l *Lock) RemoveReplacement(from string) {
	filtered := make([]Replacement, 0, len(l.data.Replacements))
	for _, r := range l.data.Replacements {
		if r.From != from {
			filtered = append(filtered, r)
		}
	}
	l.data.Replacements = filtered
}

// GetReplacements returns all replacements.
func (l *Lock) GetReplacements() []Replacement {
	return l.data.Replacements
}

// GetDirectories returns the directories configuration.
func (l *Lock) GetDirectories() Directories {
	return l.data.Directories
}

// SetDirectories updates the directories configuration.
func (l *Lock) SetDirectories(dirs Directories) {
	l.data.Directories = dirs
}

// GetVendorPath returns the vendor directory path relative to modules dir.
// Prevents double "vendor" suffix (e.g., ".wippy/vendor" not ".wippy/vendor/vendor").
func (l *Lock) GetVendorPath() string {
	modulesDir := l.data.Directories.Modules
	if modulesDir == "" {
		modulesDir = ".wippy"
	}
	// Check if already ends with vendor to prevent duplication
	if filepath.Base(modulesDir) == "vendor" {
		return modulesDir
	}
	return filepath.Join(modulesDir, "vendor")
}

// GetLoadPaths returns all directories that need to be loaded by the boot pipeline.
// Returns paths in order: app source directory, replacement directories, module vendor directories.
// Paths are relative to the lock file location and do not include @hash suffix.
// Example: [".", "../replacement", ".wippy/vendor/acme/http"]
func (l *Lock) GetLoadPaths() []string {
	lockDir := filepath.Dir(l.path)
	paths := []string{}

	// Add app source directory
	if l.data.Directories.Src != "" {
		srcPath := filepath.Join(lockDir, l.data.Directories.Src)
		paths = append(paths, srcPath)
	}

	// Add replacement paths (local overrides)
	for _, repl := range l.data.Replacements {
		if repl.To != "" {
			replPath := filepath.Join(lockDir, repl.To)
			paths = append(paths, replPath)
		}
	}

	// Add module paths (from vendor directory)
	vendorDir := l.GetVendorPath()

	for _, mod := range l.data.Modules {
		// Skip modules that have replacements
		if _, hasReplacement := l.GetReplacement(mod.Name); hasReplacement {
			continue
		}

		// Parse module name to get org/module structure
		name, err := graph.ParseName(mod.Name)
		if err != nil {
			continue
		}

		// Build path with version: org/module-VERSION
		moduleDir := ModulePath(name, mod.Version)
		modulePath := filepath.Join(vendorDir, moduleDir)
		fullPath := filepath.Join(lockDir, modulePath)
		paths = append(paths, fullPath)
	}

	return paths
}

// deduplicate removes duplicate modules and replacements.
// For modules, deduplication is based on name@version.
// For replacements, deduplication is based on from field.
func (l *Lock) deduplicate() {
	seen := make(map[string]bool)
	unique := make([]Module, 0, len(l.data.Modules))
	for _, mod := range l.data.Modules {
		key := mod.Name + "@" + mod.Version
		if !seen[key] {
			seen[key] = true
			unique = append(unique, mod)
		}
	}
	l.data.Modules = unique

	seenReplacements := make(map[string]bool)
	uniqueReplacements := make([]Replacement, 0, len(l.data.Replacements))
	for _, r := range l.data.Replacements {
		if !seenReplacements[r.From] {
			seenReplacements[r.From] = true
			uniqueReplacements = append(uniqueReplacements, r)
		}
	}
	l.data.Replacements = uniqueReplacements
}

// sort sorts modules by name and replacements by from field.
func (l *Lock) sort() {
	sort.Slice(l.data.Modules, func(i, j int) bool {
		return l.data.Modules[i].Name < l.data.Modules[j].Name
	})

	sort.Slice(l.data.Replacements, func(i, j int) bool {
		return l.data.Replacements[i].From < l.data.Replacements[j].From
	})
}

// ModulePath returns storage path for a module (e.g., "org/module-v1.0.0").
func ModulePath(name graph.Name, version string) string {
	return filepath.Join(name.Organization, name.Module+"-"+version)
}

// Find locates a lock file in the given directory.
// Returns absolute path to the lock file.
func Find(dir, filename string) (string, error) {
	lockPath := filepath.Join(dir, filename)
	absPath, err := filepath.Abs(lockPath)
	if err != nil {
		return "", err
	}

	if _, err := os.Stat(absPath); err != nil {
		return "", err
	}

	return absPath, nil
}
