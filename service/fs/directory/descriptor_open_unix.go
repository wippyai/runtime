//go:build unix && !linux

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"os"
	"strings"

	fsapi "github.com/wippyai/runtime/api/fs"
	"golang.org/x/sys/unix"
)

var _ fsapi.DescriptorOpener = (*FS)(nil)

// OpenDescriptorAt opens name below an already-retained directory file. It
// walks each component through a pinned descriptor and refuses symlink
// traversal. This is intentionally stricter than a pathname Open: W1 has no
// portable way to prove that following a symlink remains beneath a descriptor
// opened earlier. Ordinary paths remain usable when link-follow is requested; links are
// rejected instead of risking a check/open race or capability escape.
func (d *FS) OpenDescriptorAt(directory fs.File, name string, request fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	if request.Write || request.Create || request.Truncate || request.Exclusive {
		if err := d.checkPermissions("open-at", name, permWrite); err != nil {
			return nil, err
		}
	}
	if request.Read {
		if err := d.checkPermissions("open-at", name, permRead); err != nil {
			return nil, err
		}
	}
	if err := d.checkPermissions("open-at", name, permExec); err != nil {
		return nil, err
	}

	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	if strings.HasSuffix(name, "/") {
		request.Directory = true
	}
	parts, err := descriptorPathParts(name)
	if err != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
	}

	current := int(fdSource.Fd())
	opened := make([]int, 0, len(parts)-1)
	defer func() {
		for _, fd := range opened {
			_ = unix.Close(fd)
		}
	}()
	if len(parts) == 1 && parts[0] == "." {
		return openDescriptorAtFinal(current, name, ".", request, d.mode)
	}
	for _, part := range parts[:len(parts)-1] {
		fd, openErr := openatRetryUnix(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC, 0)
		if openErr != nil {
			return nil, &fs.PathError{Op: "open-at", Path: name, Err: openErr}
		}
		opened = append(opened, fd)
		current = fd
	}

	return openDescriptorAtFinal(current, name, parts[len(parts)-1], request, d.mode)
}

func openDescriptorAtFinal(current int, name, final string, request fsapi.DescriptorOpenRequest, mode fs.FileMode) (fsapi.File, error) {
	flags := unix.O_CLOEXEC | unix.O_NOFOLLOW | unix.O_NONBLOCK
	if request.Read && request.Write {
		flags |= unix.O_RDWR
	} else if request.Write {
		flags |= unix.O_WRONLY
	} else {
		flags |= unix.O_RDONLY
	}
	if request.Create {
		flags |= unix.O_CREAT
	}
	if request.Exclusive {
		flags |= unix.O_EXCL
	}
	if request.Truncate {
		flags |= unix.O_TRUNC
	}
	if request.Directory {
		flags |= unix.O_DIRECTORY
	}
	fd, openErr := openatRetryUnix(current, final, flags, uint32(0666&mode))
	if openErr != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: openErr}
	}
	file := os.NewFile(uintptr(fd), name)
	info, statErr := file.Stat()
	if statErr != nil {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: statErr}
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	return file, nil
}

// descriptorPathParts preserves harmless dot components while rejecting parent
// traversal. Every remaining component is resolved with O_NOFOLLOW.
func descriptorPathParts(name string) ([]string, error) {
	if name == "" || strings.HasPrefix(name, "/") {
		return nil, fs.ErrInvalid
	}
	parts := make([]string, 0)
	for _, part := range strings.Split(name, "/") {
		if part == ".." {
			return nil, fs.ErrInvalid
		}
		if part != "" && part != "." {
			parts = append(parts, part)
		}
	}
	if len(parts) == 0 {
		return []string{"."}, nil
	}
	return parts, nil
}

func openatRetryUnix(dirfd int, name string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Openat(dirfd, name, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}
