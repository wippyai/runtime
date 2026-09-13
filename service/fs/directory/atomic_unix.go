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
		if !before.IsDir() || before.Mode()&fs.ModeSymlink != 0 {
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
		if openErr != nil || afterErr != nil || !after.IsDir() || after.Mode()&fs.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(after, opened) {
			child.Close()
			return nil, "", fs.ErrInvalid
		}
		parent = child
	}
	return parent, parts[len(parts)-1], nil
}

type atomicOutput interface {
	io.Writer
	Sync() error
	Close() error
}
type atomicDirectory interface {
	create(string, fs.FileMode) (atomicOutput, error)
	regular(string) error
	remove(string) error
	rename(string, string) error
	sync() error
}
type rootedAtomicDirectory struct{ root *os.Root }

func (p rootedAtomicDirectory) create(name string, mode fs.FileMode) (atomicOutput, error) {
	return p.root.OpenFile(name, os.O_WRONLY|os.O_CREATE|os.O_EXCL, mode)
}
func (p rootedAtomicDirectory) regular(name string) error {
	info, err := p.root.Lstat(name)
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
func (p rootedAtomicDirectory) remove(name string) error     { return p.root.Remove(name) }
func (p rootedAtomicDirectory) rename(old, new string) error { return p.root.Rename(old, new) }
func (p rootedAtomicDirectory) sync() error {
	f, err := p.root.Open(".")
	if err != nil {
		return err
	}
	return errors.Join(f.Sync(), f.Close())
}

func publishAtomic(parent atomicDirectory, name string, data []byte, mode fs.FileMode) (result error) {
	if err := parent.regular(name); err != nil {
		return err
	}
	temporary := ".wippy-write-" + rand.Text()
	f, err := parent.create(temporary, mode)
	if err != nil {
		return err
	}
	unpublished := true
	defer func() {
		if unpublished {
			result = errors.Join(result, parent.remove(temporary))
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
	if err := parent.regular(name); err != nil {
		return err
	}
	if err := parent.rename(temporary, name); err != nil {
		return err
	}
	unpublished = false
	if err := parent.sync(); err != nil {
		return errors.Join(fsapi.ErrPublishedSyncFailed, err)
	}
	return nil
}

func (d *FS) WriteFileAtomic(name string, data []byte, perm fs.FileMode) error {
	if perm&^fs.ModePerm != 0 {
		return fsapi.ErrInvalidFileMode
	}
	if err := d.checkPermissions("writefile_atomic", name, permRead|permWrite|permExec); err != nil {
		return err
	}
	parent, base, err := d.atomicParent(name)
	if err != nil {
		return &fs.PathError{Op: "writefile_atomic", Path: name, Err: err}
	}
	defer parent.Close()
	if err := publishAtomic(rootedAtomicDirectory{parent}, base, data, perm&d.mode); err != nil {
		return &fs.PathError{Op: "writefile_atomic", Path: path.Clean(name), Err: err}
	}
	return nil
}
