package client

import (
	"io"
	"io/fs"
	"path/filepath"
	"strings"
	"time"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

// NewMemFS creates an in-memory fs.FS from protobuf files.
// Filters to only include files with .yaml or .yml extensions.
func NewMemFS(files []*modulev1.File) (fs.FS, error) {
	fileMap := make(map[string][]byte)

	for _, file := range files {
		if file == nil {
			continue
		}

		path := file.GetPath()
		if path == "" {
			continue
		}

		if !isYAMLFile(path) {
			continue
		}

		if filepath.IsAbs(path) {
			return nil, NewAbsolutePathNotAllowedError(path)
		}

		if !fs.ValidPath(path) {
			return nil, NewInvalidPathError(path)
		}

		fileMap[path] = file.GetContent()
	}

	return &memFS{files: fileMap}, nil
}

func isYAMLFile(path string) bool {
	ext := strings.ToLower(filepath.Ext(path))
	return ext == ".yaml" || ext == ".yml"
}

type memFS struct {
	files map[string][]byte
}

func (m *memFS) Open(name string) (fs.File, error) {
	if !fs.ValidPath(name) {
		return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrInvalid}
	}

	content, ok := m.files[name]
	if ok {
		return &memFile{
			name:    filepath.Base(name),
			content: content,
		}, nil
	}

	dirEntries := m.readDir(name)
	if len(dirEntries) > 0 {
		return &memDir{
			name:    filepath.Base(name),
			entries: dirEntries,
		}, nil
	}

	return nil, &fs.PathError{Op: "open", Path: name, Err: fs.ErrNotExist}
}

func (m *memFS) readDir(dirPath string) []fs.DirEntry {
	prefix := dirPath
	if prefix != "." && prefix != "" {
		prefix += "/"
	} else {
		prefix = ""
	}

	seen := make(map[string]struct{})
	var entries []fs.DirEntry

	for path := range m.files {
		if !strings.HasPrefix(path, prefix) {
			continue
		}

		remainder := strings.TrimPrefix(path, prefix)
		parts := strings.Split(remainder, "/")
		if len(parts) == 0 {
			continue
		}

		name := parts[0]
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}

		if len(parts) == 1 {
			fullPath := filepath.Join(dirPath, name)
			if dirPath == "." {
				fullPath = name
			}
			entries = append(entries, &memFileInfo{
				name:  name,
				size:  int64(len(m.files[fullPath])),
				isDir: false,
			})
		} else {
			entries = append(entries, &memFileInfo{
				name:  name,
				isDir: true,
			})
		}
	}

	return entries
}

type memFile struct {
	name    string
	content []byte
	offset  int64
}

func (f *memFile) Stat() (fs.FileInfo, error) {
	return &memFileInfo{
		name:  f.name,
		size:  int64(len(f.content)),
		isDir: false,
	}, nil
}

func (f *memFile) Read(b []byte) (int, error) {
	if f.offset >= int64(len(f.content)) {
		return 0, io.EOF
	}

	n := copy(b, f.content[f.offset:])
	f.offset += int64(n)

	if f.offset >= int64(len(f.content)) {
		return n, io.EOF
	}

	return n, nil
}

func (f *memFile) Close() error {
	return nil
}

type memDir struct {
	name    string
	entries []fs.DirEntry
	offset  int
}

func (d *memDir) Stat() (fs.FileInfo, error) {
	return &memFileInfo{
		name:  d.name,
		isDir: true,
	}, nil
}

func (d *memDir) Read(_ []byte) (int, error) {
	return 0, &fs.PathError{Op: "read", Path: d.name, Err: fs.ErrInvalid}
}

func (d *memDir) Close() error {
	return nil
}

func (d *memDir) ReadDir(n int) ([]fs.DirEntry, error) {
	if d.offset >= len(d.entries) {
		if n <= 0 {
			return nil, nil
		}
		return nil, io.EOF
	}

	if n <= 0 {
		entries := d.entries[d.offset:]
		d.offset = len(d.entries)
		return entries, nil
	}

	end := d.offset + n
	if end > len(d.entries) {
		end = len(d.entries)
	}

	entries := d.entries[d.offset:end]
	d.offset = end

	if d.offset >= len(d.entries) {
		return entries, io.EOF
	}

	return entries, nil
}

type memFileInfo struct {
	name  string
	size  int64
	isDir bool
}

func (i *memFileInfo) Name() string { return i.name }
func (i *memFileInfo) Size() int64  { return i.size }
func (i *memFileInfo) Mode() fs.FileMode {
	if i.isDir {
		return 0555 | fs.ModeDir
	}
	return 0444
}
func (i *memFileInfo) ModTime() time.Time { return time.Time{} }
func (i *memFileInfo) IsDir() bool        { return i.isDir }
func (i *memFileInfo) Sys() interface{}   { return nil }
func (i *memFileInfo) Type() fs.FileMode  { return i.Mode().Type() }
func (i *memFileInfo) Info() (fs.FileInfo, error) {
	return i, nil
}
