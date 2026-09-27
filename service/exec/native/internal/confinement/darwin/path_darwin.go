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
	fd, err := unix.Dup(int(b.file.Fd()))
	if err != nil {
		return nil, err
	}
	current := os.NewFile(uintptr(fd), b.canonical)
	parts := strings.Split(filepath.Clean(rel), string(filepath.Separator))
	if rel == "." {
		parts = nil
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			_ = current.Close()
			return nil, ErrOutsideRoot
		}
		nextFD, openErr := unix.Openat(int(current.Fd()), part,
			unix.O_RDONLY|unix.O_DIRECTORY|unix.O_CLOEXEC|unix.O_NOFOLLOW, 0)
		if openErr != nil {
			_ = current.Close()
			return nil, openErr
		}
		next := os.NewFile(uintptr(nextFD), part)
		_ = current.Close()
		current = next
	}
	actual, err := pathFromFD(int(current.Fd()))
	if err != nil {
		_ = current.Close()
		return nil, err
	}
	return &BoundPath{Path: filepath.Clean(actual), file: current}, nil
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
