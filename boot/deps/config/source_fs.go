// SPDX-License-Identifier: MPL-2.0

package config

import (
	"io/fs"
	"path"
	"path/filepath"
	"strings"
)

// SourcePrefix returns loadRoot relative to moduleRoot using slash-separated
// paths. An unrelated load root has no safe module-relative prefix and returns
// the empty string; normal module layouts always keep loadRoot below the
// manifest directory.
func SourcePrefix(moduleRoot, loadRoot string) string {
	root, err := filepath.Abs(moduleRoot)
	if err != nil {
		return ""
	}
	load, err := filepath.Abs(loadRoot)
	if err != nil {
		return ""
	}
	relative, err := filepath.Rel(root, load)
	if err != nil || relative == "." || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) {
		return ""
	}
	return filepath.ToSlash(relative)
}

// NewSourceFS applies a module's source policy to a load root. Keeping root
// translation here prevents publish, install, update, and restart from each
// inventing subtly different exclusion behavior.
func NewSourceFS(base fs.FS, config *ModuleConfig, moduleRoot, loadRoot string) fs.FS {
	return config.FilterSourceFS(&sourceBoundaryFS{base: base}, SourcePrefix(moduleRoot, loadRoot))
}

// sourceBoundaryFS hides runtime-owned state from module source traversal.
// A replacement rooted at a working directory may contain .wippy caches and
// unpacked dependencies; those files are not part of the replacement module.
// The boundary is relative to base so a module stored beneath a parent .wippy
// directory remains readable while nested runtime state does not.
type sourceBoundaryFS struct {
	base fs.FS
}

func (f *sourceBoundaryFS) Open(name string) (fs.File, error) {
	if isRuntimeStatePath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f.base.Open(name)
}

func (f *sourceBoundaryFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if isRuntimeStatePath(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(f.base, name)
	if err != nil {
		return nil, err
	}
	visible := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !isRuntimeStatePath(path.Join(name, entry.Name())) {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}

func (f *sourceBoundaryFS) ReadFile(name string) ([]byte, error) {
	if isRuntimeStatePath(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(f.base, name)
}

func (f *sourceBoundaryFS) Stat(name string) (fs.FileInfo, error) {
	if isRuntimeStatePath(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return fs.Stat(f.base, name)
}

func isRuntimeStatePath(name string) bool {
	for _, segment := range strings.Split(cleanSourcePath(name), "/") {
		if segment == ".wippy" {
			return true
		}
	}
	return false
}

// FilterSourceFS returns a view of base that hides paths excluded by the
// module manifest. sourcePrefix is the load root relative to the module root
// (for example "src" when base is os.DirFS(<module>/src)). Keeping the prefix
// explicit makes the same manifest rules apply to publish, update, replacement
// resolution, install, and restart loads.
func (c *ModuleConfig) FilterSourceFS(base fs.FS, sourcePrefix string) fs.FS {
	if c == nil || len(c.SourceExcludes()) == 0 {
		return base
	}
	return &sourceFilterFS{
		base:   base,
		config: c,
		prefix: cleanSourcePath(sourcePrefix),
	}
}

type sourceFilterFS struct {
	base   fs.FS
	config *ModuleConfig
	prefix string
}

func (f *sourceFilterFS) Open(name string) (fs.File, error) {
	if f.excluded(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
	}
	return f.base.Open(name)
}

func (f *sourceFilterFS) ReadDir(name string) ([]fs.DirEntry, error) {
	if f.excluded(name) {
		return nil, &fs.PathError{Op: "readdir", Path: name, Err: fs.ErrNotExist}
	}
	entries, err := fs.ReadDir(f.base, name)
	if err != nil {
		return nil, err
	}
	visible := make([]fs.DirEntry, 0, len(entries))
	for _, entry := range entries {
		if !f.excluded(path.Join(name, entry.Name())) {
			visible = append(visible, entry)
		}
	}
	return visible, nil
}

func (f *sourceFilterFS) ReadFile(name string) ([]byte, error) {
	if f.excluded(name) {
		return nil, &fs.PathError{Op: "readfile", Path: name, Err: fs.ErrNotExist}
	}
	return fs.ReadFile(f.base, name)
}

func (f *sourceFilterFS) Stat(name string) (fs.FileInfo, error) {
	if f.excluded(name) {
		return nil, &fs.PathError{Op: "stat", Path: name, Err: fs.ErrNotExist}
	}
	return fs.Stat(f.base, name)
}

func (f *sourceFilterFS) excluded(name string) bool {
	name = cleanSourcePath(name)
	if name == "" {
		return false
	}
	if f.prefix != "" {
		name = path.Join(f.prefix, name)
	}
	return f.config.ExcludesSourcePath(name)
}
