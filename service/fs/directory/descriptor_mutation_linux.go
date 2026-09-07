//go:build linux

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
	parent, base, closeParent, err := descriptorParentLinux(directory, name)
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
	oldParent, oldBase, closeOld, err := descriptorParentLinux(oldDirectory, oldName)
	if err != nil {
		return err
	}
	defer closeOld()
	newParent, newBase, closeNew, err := descriptorParentLinux(newDirectory, newName)
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
	parent, base, closeParent, err := descriptorParentLinux(directory, name)
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
	parent, base, closeParent, err := descriptorParentLinux(directory, name)
	if err != nil {
		return err
	}
	defer closeParent()
	return unix.Unlinkat(parent, base, unix.AT_REMOVEDIR)
}

// descriptorParentLinux resolves the parent beneath the retained directory.
// openat2 permits parent symlinks only when their targets remain in that
// descriptor grant; its conservative fallback rejects every symlink. The
// final entry is deliberately never opened before the mutation:
// mkdirat/renameat/unlinkat perform the decisive operation.
func descriptorParentLinux(directory fs.File, name string) (int, string, func(), error) {
	fdSource, ok := directory.(interface{ Fd() uintptr })
	if !ok {
		return 0, "", func() {}, errors.ErrUnsupported
	}
	if name == "" || strings.HasPrefix(name, "/") {
		return 0, "", func() {}, fs.ErrInvalid
	}
	parts := strings.Split(name, "/")
	if len(parts) == 0 || parts[len(parts)-1] == "" || parts[len(parts)-1] == "." || parts[len(parts)-1] == ".." {
		return 0, "", func() {}, fs.ErrInvalid
	}
	parentPath := "."
	if len(parts) > 1 {
		parentPath = strings.Join(parts[:len(parts)-1], "/")
	}
	how := &unix.OpenHow{
		Flags:   unix.O_RDONLY | unix.O_DIRECTORY | unix.O_CLOEXEC | unix.O_NONBLOCK,
		Resolve: unix.RESOLVE_BENEATH | unix.RESOLVE_NO_MAGICLINKS,
	}
	fd, err := openat2Retry(int(fdSource.Fd()), parentPath, how)
	if err == nil {
		return fd, parts[len(parts)-1], func() { _ = unix.Close(fd) }, nil
	}
	if !errors.Is(err, unix.ENOSYS) {
		return 0, "", func() {}, err
	}
	// Older kernels lack openat2. Preserve capability safety by refusing every
	// symlink rather than reverting to a path rooted at the process directory.
	current := int(fdSource.Fd())
	opened := make([]int, 0, len(parts)-1)
	closeParent := func() {
		for _, openedFD := range opened {
			_ = unix.Close(openedFD)
		}
	}
	for _, part := range parts[:len(parts)-1] {
		if part == "" || part == "." || part == ".." {
			closeParent()
			return 0, "", func() {}, fs.ErrInvalid
		}
		next, openErr := openatRetry(current, part, unix.O_RDONLY|unix.O_DIRECTORY|unix.O_NOFOLLOW|unix.O_CLOEXEC|unix.O_NONBLOCK, 0)
		if openErr != nil {
			closeParent()
			return 0, "", func() {}, openErr
		}
		opened = append(opened, next)
		current = next
	}
	return current, parts[len(parts)-1], closeParent, nil
}
