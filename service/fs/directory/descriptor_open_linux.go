//go:build linux

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

// OpenDescriptorAt anchors every lookup at the retained directory descriptor.
// openat2's RESOLVE_BENEATH prevents both '..' and symlinks from escaping that
// descriptor while still allowing an explicitly requested in-tree symlink
// follow. If an older kernel lacks openat2, the conservative fallback refuses
// every symlink but continues to support regular entries safely.
func (d *FS) OpenDescriptorAt(directory fs.File, name string, request fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	if err := d.checkDescriptorOpenPermissions(name, request); err != nil {
		return nil, err
	}
	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	if name == "" || strings.HasPrefix(name, "/") {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: fs.ErrInvalid}
	}

	flags := descriptorOpenFlags(request)
	how := &unix.OpenHow{
		Flags:   uint64(flags),
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	}
	if request.Create {
		how.Mode = uint64(0666 & d.mode)
	}
	if request.NoFollow {
		how.Flags |= unix.O_NOFOLLOW
	}
	fd, err := openat2Retry(int(fdSource.Fd()), name, how)
	if errors.Is(err, unix.ENOSYS) {
		return d.openDescriptorAtConservative(int(fdSource.Fd()), name, request)
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
	}
	return descriptorOpenedFile(fd, name)
}

func (d *FS) checkDescriptorOpenPermissions(name string, request fsapi.DescriptorOpenRequest) error {
	if request.Write || request.Create || request.Truncate || request.Exclusive {
		if err := d.checkPermissions("open-at", name, permWrite); err != nil {
			return err
		}
	}
	if request.Read {
		if err := d.checkPermissions("open-at", name, permRead); err != nil {
			return err
		}
	}
	return d.checkPermissions("open-at", name, permExec)
}

func descriptorOpenFlags(request fsapi.DescriptorOpenRequest) int {
	flags := unix.O_CLOEXEC | unix.O_NONBLOCK
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
	return flags
}

func openat2Retry(dirfd int, name string, how *unix.OpenHow) (int, error) {
	for {
		fd, err := unix.Openat2(dirfd, name, how)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

func (d *FS) openDescriptorAtConservative(dirfd int, name string, request fsapi.DescriptorOpenRequest) (fsapi.File, error) {
	parts := strings.Split(name, "/")
	if len(parts) == 0 {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: fs.ErrInvalid}
	}
	current := dirfd
	opened := make([]int, 0, len(parts)-1)
	defer func() {
		for _, fd := range opened {
			_ = unix.Close(fd)
		}
	}()
	if name != "." {
		for _, part := range parts[:len(parts)-1] {
			if part == "" || part == "." || part == ".." {
				return nil, &fs.PathError{Op: "open-at", Path: name, Err: fs.ErrInvalid}
			}
			fd, err := openatRetry(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
			if err != nil {
				return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
			}
			opened = append(opened, fd)
			current = fd
		}
	}
	final := name
	if name != "." {
		final = parts[len(parts)-1]
		if final == "" || final == "." || final == ".." {
			return nil, &fs.PathError{Op: "open-at", Path: name, Err: fs.ErrInvalid}
		}
	}
	fd, err := openatRetry(current, final, descriptorOpenFlags(request)|unix.O_NOFOLLOW, uint32(0666&d.mode))
	if err != nil {
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
	}
	return descriptorOpenedFile(fd, name)
}

func openatRetry(dirfd int, name string, flags int, mode uint32) (int, error) {
	for {
		fd, err := unix.Openat(dirfd, name, flags, mode)
		if !errors.Is(err, unix.EINTR) {
			return fd, err
		}
	}
}

func descriptorOpenedFile(fd int, name string) (fsapi.File, error) {
	file := os.NewFile(uintptr(fd), name)
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: err}
	}
	if !info.Mode().IsRegular() && !info.IsDir() {
		_ = file.Close()
		return nil, &fs.PathError{Op: "open-at", Path: name, Err: errors.ErrUnsupported}
	}
	return &descriptorFileLinux{File: file}, nil
}

// descriptorFileLinux adds atomic append without changing the open-file
// description's flags or positional writes. RWF_APPEND chooses EOF inside the
// write operation, including against separately opened handles. Unsupported
// kernels/filesystems fail explicitly; seek-to-end/write is not a substitute.
type descriptorFileLinux struct{ *os.File }

func (f *descriptorFileLinux) Append(p []byte) (int, error) {
	raw, err := f.SyscallConn()
	if err != nil {
		return 0, err
	}
	n := 0
	var writeErr error
	err = raw.Control(func(fd uintptr) {
		for {
			n, writeErr = unix.Pwritev2(int(fd), [][]byte{p}, 0, unix.RWF_APPEND)
			if !errors.Is(writeErr, unix.EINTR) {
				break
			}
		}
	})
	if err != nil {
		return 0, err
	}
	if errors.Is(writeErr, unix.ENOSYS) || errors.Is(writeErr, unix.EOPNOTSUPP) {
		return 0, errors.ErrUnsupported
	}
	return max(n, 0), writeErr
}
