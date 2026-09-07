//go:build windows

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDescriptorWindowsPathRejectsEscapesAndDevices(t *testing.T) {
	for _, name := range []string{"", "/absolute", `\absolute`, "../escape", "C:relative", "file:stream", "child//grandchild", "child\\grandchild", "child/./grandchild"} {
		if got, err := descriptorWindowsPath(name); err == nil {
			t.Errorf("descriptorWindowsPath(%q) = %q, nil error; want rejection", name, got)
		}
	}
	for name, want := range map[string]string{
		".":            ".",
		"./child":      "child",
		`.\child`:      "child",
		"child/subdir": `child\subdir`,
	} {
		got, err := descriptorWindowsPath(name)
		if err != nil || got != want {
			t.Errorf("descriptorWindowsPath(%q) = %q, %v; want %q, nil", name, got, err, want)
		}
	}
}

func TestRenameAtRejectsTargetJunctionEscape(t *testing.T) {
	root := t.TempDir()
	outside := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "source"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	junction := filepath.Join(root, "junction")
	command := fmt.Sprintf("mklink /J %q %q", junction, outside)
	if output, err := exec.Command("cmd", "/c", command).CombinedOutput(); err != nil {
		t.Fatalf("create target junction: %v: %s", err, output)
	}
	filesystem, err := NewFS(root, 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = filesystem.Close() })
	directory, err := filesystem.Open(".")
	if err != nil {
		t.Fatal(err)
	}
	defer directory.Close()
	if err := filesystem.RenameAt(directory, "source", directory, `junction\moved`); err == nil {
		t.Fatal("rename through target junction succeeded")
	}
	if _, err := os.Stat(filepath.Join(outside, "moved")); !os.IsNotExist(err) {
		t.Fatalf("target junction received renamed file: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "source")); err != nil {
		t.Fatalf("source vanished after rejected junction rename: %v", err)
	}
}
