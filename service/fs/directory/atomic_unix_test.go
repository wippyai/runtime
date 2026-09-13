//go:build linux || darwin

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestAtomicWriteReplaceAndRefuseLinks(t *testing.T) {
	dir := t.TempDir()
	d, err := NewFS(dir, 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
	ro, err := NewFS(dir, 0500, false)
	if err != nil {
		t.Fatal(err)
	}
	defer ro.Close()
	if err := ro.WriteFileAtomic("session/config", nil, 0600); err == nil {
		t.Fatal("read-only write")
	}
	if err := d.WriteFileAtomic("session/config", nil, fs.ModeDir); !errors.Is(err, fsapi.ErrInvalidFileMode) {
		t.Fatal(err)
	}
	entries, _ := os.ReadDir(filepath.Join(dir, "session"))
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".wippy-write-") {
			t.Fatal("temporary leaked")
		}
	}
}

func TestAtomicWritePinsParentAcrossReplacement(t *testing.T) {
	dir := t.TempDir()
	d, err := NewFS(dir, 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
	if err := publishAtomic(rootedAtomicDirectory{parent}, base, []byte("private"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(dir, "session-b/config")); !errors.Is(err, fs.ErrNotExist) {
		t.Fatal("retargeted to sibling", err)
	}
	if content, err := os.ReadFile(filepath.Join(dir, "retained-a/config")); err != nil || string(content) != "private" {
		t.Fatal("lost pinned parent", err)
	}
}

type faultAtomic struct {
	stage, content     string
	published, removed bool
}

var errAtomicFixture = errors.New("fixture failure")

func (f *faultAtomic) create(string, fs.FileMode) (atomicOutput, error) {
	if f.stage == "create" {
		return nil, errAtomicFixture
	}
	return f, nil
}
func (f *faultAtomic) Write(data []byte) (int, error) {
	if f.stage == "write" {
		return 0, errAtomicFixture
	}
	if f.stage == "short" {
		return 0, nil
	}
	f.content = string(data)
	return len(data), nil
}
func (f *faultAtomic) Sync() error {
	if f.stage == "file-sync" {
		return errAtomicFixture
	}
	return nil
}
func (f *faultAtomic) Close() error {
	if f.stage == "close" {
		return errAtomicFixture
	}
	return nil
}
func (f *faultAtomic) regular(string) error { return nil }
func (f *faultAtomic) remove(string) error  { f.removed = true; return nil }
func (f *faultAtomic) rename(string, string) error {
	if f.stage == "rename" {
		return errAtomicFixture
	}
	f.published = true
	return nil
}
func (f *faultAtomic) sync() error {
	if f.stage == "directory-sync" {
		return errAtomicFixture
	}
	return nil
}
func TestAtomicWriteFailureStages(t *testing.T) {
	for _, stage := range []string{"create", "write", "short", "file-sync", "close", "rename", "directory-sync"} {
		t.Run(stage, func(t *testing.T) {
			f := &faultAtomic{stage: stage}
			err := publishAtomic(f, "config", []byte("secret"), 0600)
			if err == nil {
				t.Fatal("missing failure")
			}
			published := stage == "directory-sync"
			if f.published != published || errors.Is(err, fsapi.ErrPublishedSyncFailed) != published {
				t.Fatal("publication outcome wrong", err)
			}
			if f.removed != (stage != "create" && !published) {
				t.Fatal("temporary cleanup wrong")
			}
			if stage == "short" && !errors.Is(err, io.ErrShortWrite) {
				t.Fatal(err)
			}
			if strings.Contains(err.Error(), "secret") {
				t.Fatal("data leaked")
			}
		})
	}
}
func TestAtomicWriteConcurrentWholeFiles(t *testing.T) {
	dir := t.TempDir()
	d, err := NewFS(dir, 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
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
