//go:build unix

// SPDX-License-Identifier: MPL-2.0

package directory

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnerSafeLinks(t *testing.T) {
	// TMPDIR must have safe canonical parents, unlike a sticky shared directory.
	homeDir, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	base, err := os.MkdirTemp(homeDir, ".wippy-link-test-")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(base) })
	home := filepath.Join(base, "home")
	store := filepath.Join(base, "store")
	for _, dir := range []string{home, store} {
		if err := os.Mkdir(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	target := filepath.Join(store, "login")
	if err := os.WriteFile(target, []byte("fixture"), 0600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(home, "login")
	if err := os.Symlink("../store/login", link); err != nil {
		t.Fatal(err)
	}
	makeFS := func(policy string) fs.FS {
		d, err := NewFactory().CreateFS(CreateFSConfig{DirPath: home, Mode: 0700, LinkPolicy: policy})
		if err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { d.(interface{ Close() error }).Close() })
		return d
	}
	d := makeFS("owner_safe")
	assertRefused := func(reason, path string) {
		t.Helper()
		for _, read := range []func() error{
			func() error { _, err := fs.Stat(d, "login"); return err },
			func() error {
				f, err := d.Open("login")
				if f != nil {
					f.Close()
				}
				return err
			},
			func() error { _, err := fs.ReadFile(d, "login"); return err },
		} {
			err := read()
			if err == nil || !strings.Contains(err.Error(), reason) || !strings.Contains(err.Error(), path) {
				t.Fatalf("want %q and %q: %v", reason, path, err)
			}
		}
	}
	t.Run("stow-like", func(t *testing.T) {
		b, err := fs.ReadFile(d, "login")
		if err != nil || string(b) != "fixture" {
			t.Fatalf("%q %v", b, err)
		}
	})
	t.Run("read-only OpenFile", func(t *testing.T) {
		f, err := d.(*FS).OpenFile("login", os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		f.Close()
	})
	t.Run("contained default", func(t *testing.T) {
		if _, err := fs.ReadFile(makeFS(""), "login"); err == nil {
			t.Fatal("default escaped")
		}
	})
	for _, mode := range []os.FileMode{0620, 0602} {
		t.Run(fmt.Sprintf("target-%o", mode), func(t *testing.T) {
			os.Chmod(target, mode)
			defer os.Chmod(target, 0600)
			assertRefused("group/other-writable", target)
		})
	}
	for _, mode := range []os.FileMode{0720, 0702, os.ModeSticky | 0777} {
		t.Run(fmt.Sprintf("parent-%o", mode), func(t *testing.T) {
			os.Chmod(store, mode)
			defer os.Chmod(store, 0700)
			assertRefused("group/other-writable", store)
		})
	}
	t.Run("nonregular", func(t *testing.T) {
		os.Remove(link)
		os.Symlink(store, link)
		defer func() { os.Remove(link); os.Symlink(target, link) }()
		assertRefused("not a regular file", store)
	})
	t.Run("dangling", func(t *testing.T) {
		os.Remove(link)
		missing := filepath.Join(store, "missing")
		os.Symlink(missing, link)
		defer func() { os.Remove(link); os.Symlink(target, link) }()
		assertRefused("no such file", missing)
	})
	t.Run("loop", func(t *testing.T) {
		os.Remove(link)
		os.Symlink("login", link)
		defer func() { os.Remove(link); os.Symlink(target, link) }()
		assertRefused("symlink loop", link)
	})
	t.Run("depth", func(t *testing.T) {
		os.Remove(link)
		os.Symlink("../store/chain0", link)
		defer func() { os.Remove(link); os.Symlink(target, link) }()
		for i := 0; i < 41; i++ {
			dest := fmt.Sprintf("chain%d", i+1)
			if i == 40 {
				dest = "login"
			}
			os.Symlink(dest, filepath.Join(store, fmt.Sprintf("chain%d", i)))
		}
		assertRefused("symlink depth", store)
	})
	t.Run("writes-contained", func(t *testing.T) {
		w := d.(*FS)
		if f, err := w.OpenFile("login", os.O_WRONLY|os.O_TRUNC, 0600); err == nil {
			f.Close()
			t.Fatal("write escaped")
		}
		if err := w.Truncate("login", 0); err == nil {
			t.Fatal("truncate escaped")
		}
		b, _ := os.ReadFile(target)
		if string(b) != "fixture" {
			t.Fatal("external target changed")
		}
	})
	t.Run("linked-directory-mutations", func(t *testing.T) {
		directoryLink := filepath.Join(home, "outside")
		if err := os.Symlink(store, directoryLink); err != nil {
			t.Fatal(err)
		}
		w := d.(*FS)
		if f, err := w.OpenFile("outside/new", os.O_CREATE|os.O_WRONLY, 0600); err == nil {
			f.Close()
			t.Fatal("create escaped")
		}
		if err := w.Remove("outside/login"); err == nil {
			t.Fatal("delete escaped")
		}
		if err := w.Rename("outside/login", "moved"); err == nil {
			t.Fatal("rename escaped")
		}
		if err := w.Mkdir("outside/newdir", 0700); err == nil {
			t.Fatal("mkdir escaped")
		}
		if err := w.Remove("outside"); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(target); err != nil {
			t.Fatal("link removal changed target", err)
		}
	})
	t.Run("ordinary missing remains not-found", func(t *testing.T) {
		_, err := fs.Stat(d, "absent")
		if !errors.Is(err, fs.ErrNotExist) {
			t.Fatal(err)
		}
	})
	t.Run("inside links preserve contained behavior", func(t *testing.T) {
		inside := filepath.Join(home, "inside")
		if err := os.WriteFile(inside, []byte("inside"), 0666); err != nil {
			t.Fatal(err)
		}
		os.Chmod(inside, 0666)
		os.Symlink("inside", filepath.Join(home, "inside-link"))
		if _, err := fs.ReadFile(d, "inside-link"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("ownership-facts", func(t *testing.T) {
		if err := checkSafeLinkFacts(target, 0600, uint32(os.Getuid()+1)); err == nil {
			t.Fatal("other owner accepted")
		}
		if err := checkSafeLinkFacts(target, 0600, 0); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("root-owned store", func(t *testing.T) {
		if os.Getuid() != 0 {
			t.Skip("requires root to create real root-owned fixtures; root uid rule covered above")
		}
		if _, err := fs.ReadFile(d, "login"); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("other-owner fixture", func(t *testing.T) {
		if os.Getuid() != 0 {
			t.Skip("requires chown authority; other uid rule covered above")
		}
		os.Chown(target, 12345, -1)
		defer os.Chown(target, 0, -1)
		assertRefused("owner uid", target)
	})
}

func TestInvalidLinkPolicy(t *testing.T) {
	if _, err := NewFactory().CreateFS(CreateFSConfig{DirPath: t.TempDir(), LinkPolicy: "unsafe"}); err == nil {
		t.Fatal("unknown policy accepted")
	}
}
