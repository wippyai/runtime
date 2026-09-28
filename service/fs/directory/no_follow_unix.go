//go:build unix

package directory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	fsapi "github.com/wippyai/runtime/api/fs"
	"golang.org/x/sys/unix"
)

// OpenFileNoFollow resolves the parent through the registered root, then opens
// the final entry relative to that pinned directory handle. Root.OpenFile alone
// cannot enforce this: it retries symlinks after receiving kernel ELOOP.
func (d *FS) OpenFileNoFollow(name string, flag int, perm fs.FileMode) (fsapi.File, error) {
	norm, perm, err := d.prepareOpenFile(name, flag, perm)
	if err != nil {
		return nil, err
	}
	if norm == "" {
		return nil, &fs.PathError{Op: "open-nofollow", Path: name, Err: fs.ErrInvalid}
	}
	// Preserve every parent component: lexical cleaning would change traversal
	// through symlinks and could erase an attempted escape followed by re-entry.
	entry := strings.TrimRight(norm, "/")
	if entry != norm {
		flag |= unix.O_DIRECTORY
	}
	parentName, base := ".", entry
	if i := strings.LastIndexByte(entry, '/'); i >= 0 {
		parentName, base = entry[:i+1], entry[i+1:]
	}
	// Dot components are traversal, never final symlink entries. Let Root own
	// their complete resolution; openat(parent, "..") could escape its grant.
	if base == "." || base == ".." || base == "" {
		return d.root.OpenFile(norm, flag, perm)
	}
	parent, err := d.root.Open(parentName)
	if err != nil {
		return nil, err
	}
	defer parent.Close()
	var fd int
	for {
		fd, err = unix.Openat(int(parent.Fd()), base, flag|unix.O_NOFOLLOW|unix.O_CLOEXEC, uint32(perm))
		if !errors.Is(err, unix.EINTR) {
			break
		}
	}
	if err != nil {
		return nil, &fs.PathError{Op: "open-nofollow", Path: name, Err: err}
	}
	return os.NewFile(uintptr(fd), filepath.Join(d.dirPath, norm)), nil
}

// OpenDirectory makes the final directory-type check part of the kernel open.
func (d *FS) OpenDirectory(name string, noFollow bool) (fs.File, error) {
	if err := d.checkPermissions("open-directory", name, permRead|permExec); err != nil {
		return nil, err
	}
	if noFollow {
		return d.OpenFileNoFollow(name, syscall.O_DIRECTORY, 0)
	}
	return d.OpenFile(name, syscall.O_DIRECTORY, 0)
}
