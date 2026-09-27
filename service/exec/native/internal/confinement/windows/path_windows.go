// SPDX-License-Identifier: MPL-2.0

//go:build windows

package windows

import (
	"errors"
	"path/filepath"
	"strings"

	"golang.org/x/sys/windows"
)

var ErrOutsideRoot = errors.New("path is outside bound root")

// BoundDirectory pins a declared root without FILE_SHARE_DELETE. Windows then
// refuses replacement or rename of the object while an executor entry owns it.
type BoundDirectory struct {
	declared  string
	canonical string
	handle    windows.Handle
}

// BoundPath pins every directory component from an entry root to a launch
// path. Path-based CreateProcess cwd resolution therefore cannot be redirected
// between validation and target startup.
type BoundPath struct {
	Path    string
	handles []windows.Handle
}

func BindDeclaredDirectory(path string) (*BoundDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path || hasDeviceNamespace(path) {
		return nil, errors.New("declared directory must be a clean absolute local path")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	canonical, err = filepath.Abs(canonical)
	if err != nil {
		return nil, err
	}
	handle, err := openPinnedDirectory(canonical)
	if err != nil {
		return nil, err
	}
	actual, err := finalPath(handle)
	if err != nil {
		_ = windows.CloseHandle(handle)
		return nil, err
	}
	return &BoundDirectory{declared: path, canonical: filepath.Clean(actual), handle: handle}, nil
}

func (b *BoundDirectory) OpenDescendant(path string) (*BoundPath, error) {
	if b == nil || b.handle == 0 {
		return nil, errors.New("bound root is closed")
	}
	rel, err := filepath.Rel(b.declared, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, ErrOutsideRoot
	}
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if rel == "." {
		parts = nil
	}
	handles := make([]windows.Handle, 0, len(parts)+1)
	rootHandle, err := openPinnedDirectory(b.canonical)
	if err != nil {
		return nil, err
	}
	handles = append(handles, rootHandle)
	current := b.canonical
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			closeHandles(handles)
			return nil, ErrOutsideRoot
		}
		current = filepath.Join(current, part)
		handle, openErr := openPinnedDirectory(current)
		if openErr != nil {
			closeHandles(handles)
			return nil, openErr
		}
		handles = append(handles, handle)
	}
	actual, err := finalPath(handles[len(handles)-1])
	if err != nil || !within(actual, b.canonical) {
		closeHandles(handles)
		if err != nil {
			return nil, err
		}
		return nil, ErrOutsideRoot
	}
	return &BoundPath{Path: filepath.Clean(actual), handles: handles}, nil
}

func (b *BoundDirectory) Close() error {
	if b == nil || b.handle == 0 {
		return nil
	}
	handle := b.handle
	b.handle = 0
	return windows.CloseHandle(handle)
}

func (p *BoundPath) Close() error {
	if p == nil {
		return nil
	}
	var result error
	for _, handle := range p.handles {
		result = errors.Join(result, windows.CloseHandle(handle))
	}
	p.handles = nil
	return result
}

func openPinnedDirectory(path string) (windows.Handle, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return 0, err
	}
	handle, err := windows.CreateFile(name, windows.FILE_READ_ATTRIBUTES|windows.SYNCHRONIZE,
		windows.FILE_SHARE_READ|windows.FILE_SHARE_WRITE, nil, windows.OPEN_EXISTING,
		windows.FILE_FLAG_BACKUP_SEMANTICS, 0)
	if err != nil {
		return 0, err
	}
	var info windows.ByHandleFileInformation
	if err := windows.GetFileInformationByHandle(handle, &info); err != nil {
		_ = windows.CloseHandle(handle)
		return 0, err
	}
	if info.FileAttributes&windows.FILE_ATTRIBUTE_DIRECTORY == 0 ||
		info.FileAttributes&windows.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
		_ = windows.CloseHandle(handle)
		return 0, errors.New("path is not a plain directory")
	}
	return handle, nil
}

func finalPath(handle windows.Handle) (string, error) {
	buf := make([]uint16, 512)
	for {
		n, err := windows.GetFinalPathNameByHandle(handle, &buf[0], uint32(len(buf)), 0)
		if err != nil {
			return "", err
		}
		if n < uint32(len(buf)) {
			path := windows.UTF16ToString(buf[:n])
			path = strings.TrimPrefix(path, `\\?\`)
			if strings.HasPrefix(path, `UNC\`) {
				return "", errors.New("remote confinement roots are unsupported")
			}
			return path, nil
		}
		buf = make([]uint16, n+1)
	}
}

func hasDeviceNamespace(path string) bool {
	upper := strings.ToUpper(path)
	volume := filepath.VolumeName(path)
	rest := strings.TrimPrefix(path, volume)
	return strings.HasPrefix(upper, `\\.\`) || strings.HasPrefix(upper, `\\?\`) ||
		strings.HasPrefix(upper, `\\`) || strings.Contains(rest, ":")
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}

func closeHandles(handles []windows.Handle) {
	for _, handle := range handles {
		_ = windows.CloseHandle(handle)
	}
}
