// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestBindDirectoryPinsObjectAcrossReplacement(t *testing.T) {
	parent := t.TempDir()
	grant := filepath.Join(parent, "grant")
	if err := os.Mkdir(grant, 0700); err != nil {
		t.Fatal(err)
	}
	file, err := BindDirectory(grant)
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if same, err := SameDirectory(int(file.Fd()), grant); err != nil || !same {
		t.Fatalf("original object identity: %t, %v", same, err)
	}
	if err := os.Rename(grant, filepath.Join(parent, "old")); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(grant, 0700); err != nil {
		t.Fatal(err)
	}
	if same, err := SameDirectory(int(file.Fd()), grant); err != nil || same {
		t.Fatalf("replacement acquired pinned authority: %t, %v", same, err)
	}
}

func TestBindDirectoryRejectsSymlink(t *testing.T) {
	parent := t.TempDir()
	if err := os.Mkdir(filepath.Join(parent, "real"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", filepath.Join(parent, "link")); err != nil {
		t.Fatal(err)
	}
	if file, err := BindDirectory(filepath.Join(parent, "link")); err == nil {
		file.Close()
		t.Fatal("symlink grant was accepted")
	}
}
