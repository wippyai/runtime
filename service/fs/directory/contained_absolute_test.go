//go:build unix

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestOwnerSafeContainedAbsoluteLink(t *testing.T) {
	parent := t.TempDir()
	if err := os.Chmod(parent, 0777); err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(parent, "volume")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("contained fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	volume, err := NewFactory().CreateFS(CreateFSConfig{DirPath: root, Mode: 0700, LinkPolicy: "owner_safe"})
	if err != nil {
		t.Fatal(err)
	}
	defer volume.(interface{ Close() error }).Close()
	data, err := fs.ReadFile(volume, "link")
	if err != nil || string(data) != "contained fixture" {
		t.Fatalf("contained absolute link: %q, %v", data, err)
	}
}

func TestOwnerSafeContainedAbsoluteLinkFromFilesystemRoot(t *testing.T) {
	root := t.TempDir()
	target := filepath.Join(root, "target")
	if err := os.WriteFile(target, []byte("contained fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link")
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	volume, err := NewFactory().CreateFS(CreateFSConfig{DirPath: "/", Mode: 0700, LinkPolicy: "owner_safe"})
	if err != nil {
		t.Fatal(err)
	}
	defer volume.(interface{ Close() error }).Close()
	relative, err := filepath.Rel("/", link)
	if err != nil {
		t.Fatal(err)
	}
	info, err := fs.Stat(volume, relative)
	if err != nil {
		t.Fatal(err)
	}
	if info.Size() != int64(len("contained fixture")) {
		t.Fatalf("wrong file: %v", info)
	}
}

func TestOwnerSafeStillRejectsForeignOwnerFacts(t *testing.T) {
	foreign := uint32(os.Getuid()) + 1
	if foreign == 0 {
		foreign++
	}
	if err := checkSafeLinkFacts("/outside/target", 0600, foreign); err == nil {
		t.Fatal("foreign-owned external target accepted")
	}
	if err := checkSafeLinkFacts("/outside", 0777, uint32(os.Getuid())); err == nil {
		t.Fatal("writable external parent accepted")
	}
}

func TestOwnerSafeExternalAbsoluteLinkStillChecksOwnership(t *testing.T) {
	parent := t.TempDir()
	root := filepath.Join(parent, "volume")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	target := filepath.Join(parent, "outside")
	if err := os.WriteFile(target, []byte("untrusted fixture"), 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(target, 0666); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, filepath.Join(root, "link")); err != nil {
		t.Fatal(err)
	}
	volume, err := NewFactory().CreateFS(CreateFSConfig{DirPath: root, Mode: 0700, LinkPolicy: "owner_safe"})
	if err != nil {
		t.Fatal(err)
	}
	defer volume.(interface{ Close() error }).Close()
	if data, err := fs.ReadFile(volume, "link"); err == nil {
		t.Fatalf("external writable target accepted: %q", data)
	}
}
