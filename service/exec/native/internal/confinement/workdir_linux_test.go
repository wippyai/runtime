// SPDX-License-Identifier: MPL-2.0

package confinement

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/sys/unix"
)

func TestWorkDirRootPinsAndBoundsDirectory(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "approved")
	subPath := filepath.Join(rootPath, "pkg")
	if err := os.MkdirAll(subPath, 0o700); err != nil {
		t.Fatal(err)
	}
	otherPath := filepath.Join(parent, "other")
	if err := os.Mkdir(otherPath, 0o700); err != nil {
		t.Fatal(err)
	}
	root, err := BindWorkDirRoot(rootPath)
	if errors.Is(err, unix.ENOSYS) {
		t.Skip("openat2 is unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()

	bound, err := root.OpenBoundWorkDir(subPath)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	var original unix.Stat_t
	if err := unix.Fstat(int(bound.Fd()), &original); err != nil {
		t.Fatal(err)
	}
	for _, requested := range []string{
		parent, otherPath, filepath.Join(parent, "approved-other"),
		filepath.Join(rootPath, "..", "other"),
	} {
		dir, err := root.OpenBoundWorkDir(requested)
		if dir != nil {
			dir.Close()
		}
		if err == nil {
			t.Fatalf("opened unapproved directory %q", requested)
		}
	}

	// Replacing the reviewed pathname does not retarget the pinned root or
	// the already-bound child descriptor.
	oldPath := filepath.Join(parent, "old-approved")
	if err := os.Rename(rootPath, oldPath); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	var after unix.Stat_t
	if err := unix.Fstat(int(bound.Fd()), &after); err != nil {
		t.Fatal(err)
	}
	if original.Ino != after.Ino || original.Dev != after.Dev {
		t.Fatal("bound directory changed after pathname replacement")
	}
	newBinding, err := root.OpenBoundWorkDir(subPath)
	if err != nil {
		t.Fatal(err)
	}
	defer newBinding.Close()
	if err := unix.Fstat(int(newBinding.Fd()), &after); err != nil {
		t.Fatal(err)
	}
	if original.Ino != after.Ino || original.Dev != after.Dev {
		t.Fatal("root descriptor followed a replaced pathname")
	}
}

func TestWorkDirRootRejectsSymlinkEscape(t *testing.T) {
	parent := t.TempDir()
	rootPath := filepath.Join(parent, "approved")
	otherPath := filepath.Join(parent, "other")
	if err := os.Mkdir(rootPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(otherPath, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(otherPath, filepath.Join(rootPath, "escape")); err != nil {
		t.Fatal(err)
	}
	root, err := BindWorkDirRoot(rootPath)
	if errors.Is(err, unix.ENOSYS) {
		t.Skip("openat2 is unavailable")
	}
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if dir, err := root.OpenBoundWorkDir(filepath.Join(rootPath, "escape")); err == nil {
		dir.Close()
		t.Fatal("followed a symlink outside the approved root")
	}
	if symlinkRoot, err := BindWorkDirRoot(filepath.Join(parent, "root-link")); err == nil {
		symlinkRoot.Close()
		t.Fatal("bound a missing root")
	}
	if err := os.Symlink(rootPath, filepath.Join(parent, "root-link")); err != nil {
		t.Fatal(err)
	}
	if symlinkRoot, err := BindWorkDirRoot(filepath.Join(parent, "root-link")); err == nil {
		symlinkRoot.Close()
		t.Fatal("followed a symlink in entry-owned root")
	}
}
