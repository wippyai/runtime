//go:build linux || darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"crypto/rand"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"strings"

	fsapi "github.com/wippyai/runtime/api/fs"
)

// atomicParent pins each actual directory rather than repeatedly resolving a
// path. OpenRoot can follow links: compare its identity with Lstat, and reject
// links explicitly. A rename after opening does not retarget the held handle.
func (d *FS) atomicParent(name string) (*os.Root, string, error) {
	name = d.normalizePath(name)
	if name == "." || !fs.ValidPath(name) {
		return nil, "", fs.ErrInvalid
	}
	parent, err := d.root.OpenRoot(".")
	if err != nil {
		return nil, "", err
	}
	parts := strings.Split(name, "/")
	for _, part := range parts[:len(parts)-1] {
		before, err := parent.Lstat(part)
		if err != nil {
			parent.Close()
			return nil, "", err
		}
		// Lstat reports a symbolic link as a link, never as a directory.
		if !before.IsDir() {
			parent.Close()
			return nil, "", fs.ErrInvalid
		}
		child, err := parent.OpenRoot(part)
		if err != nil {
			parent.Close()
			return nil, "", err
		}
		opened, openErr := child.Lstat(".")
		after, afterErr := parent.Lstat(part)
		parent.Close()
		if openErr != nil || afterErr != nil || !after.IsDir() || !os.SameFile(before, opened) || !os.SameFile(after, opened) {
			child.Close()
			return nil, "", fs.ErrInvalid
		}
		parent = child
	}
	return parent, parts[len(parts)-1], nil
}

// atomicRename and atomicSyncDirectory are seams for the two outcomes a real
// directory cannot be driven into once the temporary file exists: a refused
// publication that must roll back, and a publication whose durability is
// unknown.
var (
	atomicRename = func(parent *os.Root, oldName, newName string) error {
		return parent.Rename(oldName, newName)
	}
	atomicSyncDirectory = func(parent *os.Root) error {
		f, err := parent.Open(".")
		if err != nil {
			return err
		}
		return errors.Join(f.Sync(), f.Close())
	}
)

// atomicRegular accepts a missing target and a regular one; everything else,
// including a symbolic link, belongs to another owner and is refused.
func atomicRegular(parent *os.Root, name string) error {
	info, err := parent.Lstat(name)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return fs.ErrInvalid
	}
	return nil
}

func publishAtomic(parent *os.Root, name string, data []byte, mode fs.FileMode) (result error) {
	if err := atomicRegular(parent, name); err != nil {
		return err
	}
	temporary := ".wippy-write-" + rand.Text()
	f, err := parent.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
	if err != nil {
		return err
	}
	unpublished := true
	defer func() {
		if unpublished {
			result = errors.Join(result, parent.Remove(temporary))
		}
	}()
	n, writeErr := f.Write(data)
	if writeErr == nil && n != len(data) {
		writeErr = io.ErrShortWrite
	}
	if writeErr != nil {
		return errors.Join(writeErr, f.Close())
	}
	if err := f.Sync(); err != nil {
		return errors.Join(err, f.Close())
	}
	if err := f.Close(); err != nil {
		return err
	}
	// Recheck the target's kind; Rename never follows a final symbolic link.
	if err := atomicRegular(parent, name); err != nil {
		return err
	}
	if err := atomicRename(parent, temporary, name); err != nil {
		return err
	}
	unpublished = false
	if err := atomicSyncDirectory(parent); err != nil {
		return errors.Join(fsapi.ErrPublishedSyncFailed, err)
	}
	return nil
}

func (d *FS) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	if perm&^fs.ModePerm != 0 {
		return fsapi.ErrInvalidFileMode
	}
	// Publication creates, writes and renames within the pinned parent, which is
	// the capability the ordinary write path demands for O_WRONLY|O_CREATE.
	if err := d.checkPermissions("writefile_atomic", name, permWrite); err != nil {
		return err
	}
	parent, base, err := d.atomicParent(name)
	if err != nil {
		return &fs.PathError{Op: "writefile_atomic", Path: name, Err: err}
	}
	defer parent.Close()
	if err := publishAtomic(parent, base, data, perm&d.mode); err != nil {
		return &fs.PathError{Op: "writefile_atomic", Path: path.Clean(name), Err: err}
	}
	return nil
}
