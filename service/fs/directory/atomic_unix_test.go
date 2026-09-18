//go:build linux || darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicWriteFailsWhenParentIsUnwritable(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not restrain the superuser")
	}
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	session := filepath.Join(dir, "session")
	if err := os.Mkdir(session, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(session, 0500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(session, 0700) })
	if err := d.WriteFileAtomic("session/config", []byte("secret"), 0600); !errors.Is(err, fs.ErrPermission) {
		t.Fatal("expected permission refusal", err)
	}
	if err := os.Chmod(session, 0700); err != nil {
		t.Fatal(err)
	}
	requireNoTemporaries(t, session)
}

// A publication that cannot be completed removes its temporary file and leaves
// the destination untouched.

// Permission bits are a POSIX property; a replacement keeps the requested mode
// under the mount's cap.
func TestAtomicWriteAppliesRequestedMode(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	if err := d.WriteFileAtomic("config", []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFileAtomic("config", []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(filepath.Join(dir, "config"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("mode", err)
	}
}
