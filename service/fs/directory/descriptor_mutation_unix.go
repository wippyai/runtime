//go:build unix && !linux

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"strings"

	fsapi "github.com/wippyai/runtime/api/fs"
	"golang.org/x/sys/unix"
)

var _ fsapi.DescriptorMutator = (*FS)(nil)

func (d *FS) CreateDirectoryAt(directory fs.File, name string, mode fs.FileMode) error {
	if err := d.checkPermissions("mkdir-at", name, permWrite|permExec); err != nil {
		return err
	}
	parent, base, closeParent, err := descriptorParentUnix(directory, name)
	if err != nil {
		return err
	}
	defer closeParent()
	return unix.Mkdirat(parent, base, uint32(mode&d.mode&fs.ModePerm))
}

func (d *FS) RenameAt(oldDirectory fs.File, oldName string, newDirectory fs.File, newName string) error {
	if err := d.checkPermissions("rename-at", oldName, permWrite|permExec); err != nil {
		return err
	}
	oldParent, oldBase, closeOld, err := descriptorParentUnix(oldDirectory, oldName)
	if err != nil {
		return err
	}
	defer closeOld()
	newParent, newBase, closeNew, err := descriptorParentUnix(newDirectory, newName)
	if err != nil {
		return err
	}
	defer closeNew()
	return unix.Renameat(oldParent, oldBase, newParent, newBase)
}

func (d *FS) UnlinkFileAt(directory fs.File, name string) error {
	if err := d.checkPermissions("unlink-at", name, permWrite|permExec); err != nil {
		return err
	}
	parent, base, closeParent, err := descriptorParentUnix(directory, name)
	if err != nil {
		return err
	}
	defer closeParent()
	return unix.Unlinkat(parent, base, 0)
}

func (d *FS) RemoveDirectoryAt(directory fs.File, name string) error {
	if err := d.checkPermissions("remove-directory-at", name, permWrite|permExec); err != nil {
		return err
	}
	parent, base, closeParent, err := descriptorParentUnix(directory, name)
	if err != nil {
		return err
	}
	defer closeParent()
	return unix.Unlinkat(parent, base, unix.AT_REMOVEDIR)
}

// descriptorParentUnix pins each parent before mutation. Refusing symlinks is
// conservative on platforms without openat2's beneath-root resolution contract.
func descriptorParentUnix(directory fs.File, name string) (int, string, func(), error) {
	noop := func() {}
	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return 0, "", noop, errors.ErrUnsupported
	}
	// Mutations require a final entry name, never the retained root itself.
	raw := strings.Split(name, "/")
	last := raw[len(raw)-1]
	if last == "" || last == "." || last == ".." {
		return 0, "", noop, fs.ErrInvalid
	}
	parts, err := descriptorPathParts(name)
	if err != nil {
		return 0, "", noop, err
	}
	current := int(fdSource.Fd())
	opened := make([]int, 0, len(parts)-1)
	cleanup := func() {
		for _, fd := range opened {
			_ = unix.Close(fd)
		}
	}
	for _, part := range parts[:len(parts)-1] {
		next, err := openatRetryUnix(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if err != nil {
			cleanup()
			return 0, "", noop, err
		}
		opened = append(opened, next)
		current = next
	}
	return current, parts[len(parts)-1], cleanup, nil
}
