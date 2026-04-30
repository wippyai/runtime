// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"sort"

	"github.com/wippyai/runtime/boot/deps/graph"
	"gopkg.in/yaml.v3"
)

// DefaultFilename is the standard name for wippy lock files.
const DefaultFilename = "wippy.lock"

// Lock represents a lock file with operations for reading, writing, and querying.
type Lock struct {
	path string
	data File
}

// New creates a new Lock instance from the given path.
// If the file exists, it loads the content. Otherwise, creates an empty lock with default directories.
func New(path string) (*Lock, error) {
	absPath, err := filepath.Abs(path)
	if err != nil {
		return nil, NewResolveAbsolutePathError(err)
	}

	lock := &Lock{
		path: absPath,
		data: File{
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
			return nil, NewReadLockFileError(err)
		}
	} else if !os.IsNotExist(err) {
		return nil, NewStatLockFileError(err)
	}

	return lock, nil
}

// Read loads the lock file from disk.
func (l *Lock) Read() error {
	data, err := os.ReadFile(l.path)
	if err != nil {
		return NewReadFileError(err)
	}

	if err := yaml.Unmarshal(data, &l.data); err != nil {
		return NewUnmarshalYAMLError(err)
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
		return NewMarshalYAMLError(err)
	}

	if err := os.WriteFile(l.path, data, 0600); err != nil {
		return NewWriteFileError(err)
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

// GetOptions returns the options configuration.
func (l *Lock) GetOptions() Options {
	return l.data.Options
}

// SetOptions updates the options configuration.
func (l *Lock) SetOptions(opts Options) {
	l.data.Options = opts
}

// ShouldUnpackModules returns whether modules should be extracted from .wapp files.
func (l *Lock) ShouldUnpackModules() bool {
	return l.data.Options.UnpackModules
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

// GetLoadPaths returns all paths that need to be loaded by the boot pipeline.
// It is the path-only view of GetModuleLoadPaths; keep module path selection in
// one place so replacements, packed modules and unpacked modules cannot drift.
func (l *Lock) GetLoadPaths() []string {
	modulePaths := l.GetModuleLoadPaths()
	paths := make([]string, 0, len(modulePaths))
	for _, mp := range modulePaths {
		paths = append(paths, mp.Path)
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

// ModulePath returns storage path for a module directory (e.g., "org/module").
func ModulePath(name graph.Name) string {
	return filepath.Join(name.Organization, name.Module)
}

// LegacyModulePath returns the old versioned directory path (e.g., "org/module-v1.0.0").
func LegacyModulePath(name graph.Name, version string) string {
	return filepath.Join(name.Organization, name.Module+"-"+version)
}

// WappPath returns storage path for a module wapp file (e.g., "org/module-v1.0.0.wapp").
func WappPath(name graph.Name, version string) string {
	return filepath.Join(name.Organization, name.Module+"-"+version+".wapp")
}

// ModuleLoadPath pairs a filesystem path with its owning module metadata.
type ModuleLoadPath struct {
	Path       string
	Module     string // module name in org/module format, empty for app source
	Version    string // module version, empty for app source
	SourceRoot string // module root for module-relative resources; defaults to Path
}

// GetModuleLoadPaths returns load paths annotated with module ownership.
// App source has empty Module/Version.
// Replacement paths carry Module from replacement "from" and empty Version.
func (l *Lock) GetModuleLoadPaths() []ModuleLoadPath {
	lockDir := filepath.Dir(l.path)
	paths := make([]ModuleLoadPath, 0, 1+len(l.data.Replacements)+len(l.data.Modules))

	if l.data.Directories.Src != "" {
		paths = append(paths, ModuleLoadPath{
			Path: ResolveLockPath(lockDir, l.data.Directories.Src),
		})
	}

	for _, repl := range l.data.Replacements {
		if repl.To != "" {
			root := ResolveLockPath(lockDir, repl.To)
			path := moduleEntryLoadPath(root)
			paths = append(paths, ModuleLoadPath{
				Path:       path,
				Module:     repl.From,
				SourceRoot: root,
			})
		}
	}

	vendorDir := l.GetVendorPath()
	fullVendorDir := ResolveLockPath(lockDir, vendorDir)

	for _, mod := range l.data.Modules {
		if _, hasReplacement := l.GetReplacement(mod.Name); hasReplacement {
			continue
		}

		name, err := graph.ParseName(mod.Name)
		if err != nil {
			continue
		}

		resolved := ResolveModuleDir(fullVendorDir, name, mod.Version)
		paths = append(paths, ModuleLoadPath{
			Path:       resolved.Path,
			Module:     mod.Name,
			Version:    mod.Version,
			SourceRoot: resolved.Path,
		})
	}

	return paths
}

func moduleEntryLoadPath(root string) string {
	src := filepath.Join(root, "src")
	info, err := os.Stat(src)
	if err == nil && info.IsDir() {
		return src
	}
	return root
}

// ResolveLockPath resolves a lock-file path setting against lockDir.
// Absolute values are already complete and must not be joined to lockDir.
func ResolveLockPath(lockDir, value string) string {
	if filepath.IsAbs(value) {
		return filepath.Clean(value)
	}
	return filepath.Join(lockDir, value)
}

// ResolvedPath describes which path was found and its format.
type ResolvedPath struct {
	Path     string
	IsWapp   bool // true if the resolved path is a .wapp file
	IsLegacy bool // true if the resolved path uses the old versioned directory format
}

// ResolveModuleDir finds the actual module path on disk by checking in order:
// extracted directory (org/module), legacy .wapp file, legacy versioned directory.
// Returns the preferred path if nothing exists on disk.
func ResolveModuleDir(vendorDir string, name graph.Name, version string) ResolvedPath {
	preferred := filepath.Join(vendorDir, ModulePath(name))
	if _, err := os.Stat(preferred); err == nil {
		return ResolvedPath{Path: preferred}
	}

	wapp := filepath.Join(vendorDir, WappPath(name, version))
	if _, err := os.Stat(wapp); err == nil {
		return ResolvedPath{Path: wapp, IsWapp: true}
	}

	legacy := filepath.Join(vendorDir, LegacyModulePath(name, version))
	if _, err := os.Stat(legacy); err == nil {
		return ResolvedPath{Path: legacy, IsLegacy: true}
	}

	return ResolvedPath{Path: preferred}
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
