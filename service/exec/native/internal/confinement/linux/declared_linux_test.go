// SPDX-License-Identifier: MPL-2.0

package linux

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDeclaredSymlinkPinsCanonicalDirectory(t *testing.T) {
	parent := t.TempDir()
	real := filepath.Join(parent, "real")
	alias := filepath.Join(parent, "alias")
	if err := os.MkdirAll(filepath.Join(real, "child"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("real", alias); err != nil {
		t.Fatal(err)
	}
	bound, err := BindDeclaredDirectory(alias)
	if err != nil {
		t.Fatal(err)
	}
	defer bound.Close()
	if bound.Canonical != real {
		t.Fatalf("wrong alias target: %q", bound.Canonical)
	}
	if err := os.Remove(alias); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(t.TempDir(), alias); err != nil {
		t.Fatal(err)
	}
	file, canonical, err := bound.OpenDescendant(filepath.Join(alias, "child"))
	if err != nil {
		t.Fatal(err)
	}
	defer file.Close()
	if canonical != filepath.Join(real, "child") {
		t.Fatalf("alias replacement changed pinned source: %q", canonical)
	}
}

func TestDeclaredAliasCannotImportHostControlFilesystem(t *testing.T) {
	for _, source := range []string{"/proc", "/sys", "/sys/fs/cgroup"} {
		alias := filepath.Join(t.TempDir(), "alias")
		if err := os.Symlink(source, alias); err != nil {
			t.Fatal(err)
		}
		if bound, err := BindDeclaredDirectory(alias); err == nil {
			_ = bound.Close()
			t.Fatalf("accepted special host source %s", source)
		}
	}
}
