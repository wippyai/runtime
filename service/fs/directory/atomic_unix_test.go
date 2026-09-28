//go:build linux || darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	fsapi "github.com/wippyai/runtime/api/fs"
)

func newAtomicFS(t *testing.T, dir string, mode fs.FileMode) *FS {
	t.Helper()
	d, err := NewFS(dir, mode, false)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = d.Close() })
	return d
}

func requireNoTemporaries(t *testing.T, dir string) {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wippy-write-") {
			t.Fatal("temporary leaked")
		}
	}
}

func TestAtomicWriteReplaceAndRefuseLinks(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	if err := os.Mkdir(filepath.Join(dir, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFileAtomic("session/config", []byte("first"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFileAtomic("session/config", []byte("second"), 0600); err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "session/config"))
	if err != nil || string(content) != "second" {
		t.Fatal("replacement", err)
	}
	info, err := os.Stat(filepath.Join(dir, "session/config"))
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("mode", err)
	}
	if err := os.Symlink("session", filepath.Join(dir, "link")); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"link/config", "../escape", "session", "missing/config"} {
		if err := d.WriteFileAtomic(name, []byte("bad"), 0600); err == nil {
			t.Fatalf("accepted %s", name)
		}
	}
	if err := os.Symlink("config", filepath.Join(dir, "session/symlink")); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFileAtomic("session/symlink", []byte("bad"), 0600); err == nil {
		t.Fatal("accepted final symlink")
	}
	content, _ = os.ReadFile(filepath.Join(dir, "session/config"))
	if string(content) != "second" {
		t.Fatal("refusal changed target")
	}
	ro := newAtomicFS(t, dir, 0500)
	if err := ro.WriteFileAtomic("session/config", nil, 0600); err == nil {
		t.Fatal("read-only write")
	}
	if err := d.WriteFileAtomic("session/config", nil, fs.ModeDir); !errors.Is(err, fsapi.ErrInvalidFileMode) {
		t.Fatal(err)
	}
	requireNoTemporaries(t, filepath.Join(dir, "session"))
}

// Atomic publication is a write; it demands the same capability the ordinary
// write path demands and no more.
func TestAtomicWriteDemandsSameCapabilityAsOrdinaryWrite(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0600)

	f, err := d.OpenFile("ordinary", os.O_WRONLY|os.O_CREATE, 0600)
	if err != nil {
		t.Fatal("ordinary write refused", err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := d.WriteFileAtomic("published", []byte("content"), 0600); err != nil {
		t.Fatal("atomic write refused", err)
	}
	content, err := os.ReadFile(filepath.Join(dir, "published"))
	if err != nil || string(content) != "content" {
		t.Fatal("publication", err)
	}
}

func TestAtomicWritePinsParentAcrossReplacement(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	for _, name := range []string{"session-a", "session-b"} {
		if err := os.Mkdir(filepath.Join(dir, name), 0700); err != nil {
			t.Fatal(err)
		}
	}
	parent, base, err := d.atomicParent("session-a/config")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Rename(filepath.Join(dir, "session-a"), filepath.Join(dir, "retained-a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("session-b", filepath.Join(dir, "session-a")); err != nil {
		t.Fatal(err)
	}
	if err := publishAtomic(parent, base, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session-b/config")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("retargeted to sibling", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "retained-a/config")); err != nil || string(content) != "private" {
		t.Fatal("lost pinned parent", err)
	}
}

// A parent that disappears after it is pinned stops publication before any
// temporary exists.
func TestAtomicWriteFailsWhenPinnedParentIsRemoved(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	if err := os.Mkdir(filepath.Join(dir, "session"), 0700); err != nil {
		t.Fatal(err)
	}
	parent, base, err := d.atomicParent("session/config")
	if err != nil {
		t.Fatal(err)
	}
	defer parent.Close()
	if err := os.Remove(filepath.Join(dir, "session")); err != nil {
		t.Fatal(err)
	}
	err = publishAtomic(parent, base, []byte("secret"), 0600)
	if err == nil {
		t.Fatal("published into a removed parent")
	}
	if errors.Is(err, fsapi.ErrPublishedSyncFailed) {
		t.Fatal("reported publication", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("data leaked")
	}
}

// A parent the process cannot write refuses the temporary file outright.
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
func TestAtomicWriteRollsBackTemporaryWhenPublicationFails(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	if err := d.WriteFileAtomic("config", []byte("published"), 0600); err != nil {
		t.Fatal(err)
	}

	fixture := errors.New("rename refused")
	original := atomicRename
	atomicRename = func(*os.Root, string, string) error { return fixture }
	t.Cleanup(func() { atomicRename = original })

	err := d.WriteFileAtomic("config", []byte("secret"), 0600)
	if !errors.Is(err, fixture) {
		t.Fatal("lost cause", err)
	}
	if errors.Is(err, fsapi.ErrPublishedSyncFailed) {
		t.Fatal("reported publication", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("data leaked")
	}
	content, readErr := os.ReadFile(filepath.Join(dir, "config"))
	if readErr != nil || string(content) != "published" {
		t.Fatal("destination changed", readErr)
	}
	requireNoTemporaries(t, dir)
}

// A rename that succeeds ahead of a failing directory sync reports the
// publication as durable-uncertain rather than as a failed write.
func TestAtomicWriteReportsUncertainDurabilityWhenDirectorySyncFails(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)

	fixture := errors.New("directory sync refused")
	original := atomicSyncDirectory
	atomicSyncDirectory = func(*os.Root) error { return fixture }
	t.Cleanup(func() { atomicSyncDirectory = original })

	err := d.WriteFileAtomic("config", []byte("secret"), 0600)
	if !errors.Is(err, fsapi.ErrPublishedSyncFailed) || !errors.Is(err, fixture) {
		t.Fatal("outcome wrong", err)
	}
	if strings.Contains(err.Error(), "secret") {
		t.Fatal("data leaked")
	}
	content, readErr := os.ReadFile(filepath.Join(dir, "config"))
	if readErr != nil || string(content) != "secret" {
		t.Fatal("publication missing", readErr)
	}
	requireNoTemporaries(t, dir)
}

func TestAtomicWriteConcurrentWholeFiles(t *testing.T) {
	dir := t.TempDir()
	d := newAtomicFS(t, dir, 0700)
	a, b := strings.Repeat("a", 32768), strings.Repeat("b", 32768)
	if err := d.WriteFileAtomic("config", []byte(a), 0600); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for _, value := range []string{a, b} {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 30; i++ {
				if err := d.WriteFileAtomic("config", []byte(value), 0600); err != nil {
					t.Error(err)
					return
				}
			}
		}()
	}
	for i := 0; i < 120; i++ {
		v, err := os.ReadFile(filepath.Join(dir, "config"))
		if err != nil || (string(v) != a && string(v) != b) {
			t.Fatal("partial publication", err)
		}
	}
	wg.Wait()
}
