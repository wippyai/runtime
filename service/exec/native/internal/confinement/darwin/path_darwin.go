// SPDX-License-Identifier: MPL-2.0

//go:build darwin

package darwin

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"unsafe"

	"golang.org/x/sys/unix"
)

var ErrOutsideRoot = errors.New("path is outside bound root")

type BoundDirectory struct {
	declared  string
	canonical string
	file      *os.File
}

type BoundPath struct {
	Path string
	file *os.File
}

func (p *BoundPath) File() *os.File {
	if p == nil {
		return nil
	}
	return p.file
}

func BindDeclaredDirectory(path string) (*BoundDirectory, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return nil, errors.New("declared directory must be a clean absolute path")
	}
	canonical, err := filepath.EvalSymlinks(path)
	if err != nil {
		return nil, err
	}
	file, actual, err := openDirectory(canonical)
	if err != nil {
		return nil, err
	}
	return &BoundDirectory{declared: path, canonical: actual, file: file}, nil
}

func (b *BoundDirectory) OpenDescendant(path string) (*BoundPath, error) {
	if b == nil || b.file == nil {
		return nil, errors.New("bound root is closed")
	}
	rel, err := filepath.Rel(b.declared, path)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return nil, ErrOutsideRoot
	}
	resolved, err := filepath.EvalSymlinks(filepath.Join(b.canonical, rel))
	if err != nil || !within(resolved, b.canonical) {
		if err != nil {
			return nil, err
		}
		return nil, ErrOutsideRoot
	}
	file, actual, err := openDirectory(resolved)
	if err != nil {
		return nil, err
	}
	if !within(actual, b.canonical) {
		_ = file.Close()
		return nil, ErrOutsideRoot
	}
	return &BoundPath{Path: actual, file: file}, nil
}

func (b *BoundDirectory) CanonicalDescendant(path string) (string, error) {
	bound, err := b.OpenDescendant(path)
	if err != nil {
		return "", err
	}
	defer bound.Close()
	return bound.Path, nil
}

func (b *BoundDirectory) Close() error {
	if b == nil || b.file == nil {
		return nil
	}
	err := b.file.Close()
	b.file = nil
	return err
}

func (p *BoundPath) Close() error {
	if p == nil || p.file == nil {
		return nil
	}
	err := p.file.Close()
	p.file = nil
	return err
}

func openDirectory(path string) (*os.File, string, error) {
	fd, err := unix.Open(path, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
	if err != nil {
		return nil, "", err
	}
	file := os.NewFile(uintptr(fd), path)
	actual, err := pathFromFD(fd)
	if err != nil {
		_ = file.Close()
		return nil, "", err
	}
	return file, filepath.Clean(actual), nil
}

func pathFromFD(fd int) (string, error) {
	buffer := make([]byte, 4096)
	_, _, errno := unix.Syscall(unix.SYS_FCNTL, uintptr(fd), uintptr(unix.F_GETPATH),
		uintptr(unsafe.Pointer(&buffer[0])))
	if errno != 0 {
		return "", errno
	}
	if end := strings.IndexByte(string(buffer), 0); end >= 0 {
		buffer = buffer[:end]
	}
	if len(buffer) == 0 {
		return "", errors.New("empty descriptor path")
	}
	return string(buffer), nil
}

func within(path, root string) bool {
	rel, err := filepath.Rel(root, path)
	return err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
