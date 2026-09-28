//go:build unix

package directory

import (
	"os"
	"path/filepath"
	"testing"
)

func TestAtomicDirectoryAndNoFollowCapabilities(t *testing.T) {
	root := filepath.Join(t.TempDir(), "granted")
	if err := os.Mkdir(root, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "file"), []byte("inside"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(root, "dir", "nested"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "dir", "only-in-dir"), []byte("nested target"), 0600); err != nil {
		t.Fatal(err)
	}
	for link, target := range map[string]string{"file-link": "file", "dir-link": "dir", "nested-link": "dir/nested"} {
		if err := os.Symlink(target, filepath.Join(root, link)); err != nil {
			t.Fatal(err)
		}
	}
	registered, err := NewFS(root, 0700, false)
	if err != nil {
		t.Fatal(err)
	}
	defer registered.Close()
	t.Run("regular-file-nofollow", func(t *testing.T) {
		f, err := registered.OpenFileNoFollow("file", os.O_RDONLY, 0)
		if err != nil {
			t.Fatal(err)
		}
		if err := f.Close(); err != nil {
			t.Fatal(err)
		}
	})
	t.Run("symlink-file-nofollow", func(t *testing.T) {
		f, err := registered.OpenFileNoFollow("file-link", os.O_RDONLY, 0)
		if f != nil {
			f.Close()
		}
		if err == nil {
			t.Fatal("nofollow opened final symlink")
		}
	})
	for _, tc := range []struct {
		name            string
		path            string
		nofollow, allow bool
	}{
		{"directory", "dir", false, true}, {"directory-nofollow", "dir", true, true}, {"directory-link-follow", "dir-link", false, true}, {"directory-link-nofollow", "dir-link", true, false}, {"file-is-not-directory", "file", false, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, err := registered.OpenDirectory(tc.path, tc.nofollow)
			if f != nil {
				defer f.Close()
			}
			if tc.allow {
				if err != nil {
					t.Fatal(err)
				}
				info, err := f.Stat()
				if err != nil || !info.IsDir() {
					t.Fatalf("expected directory handle: %v", err)
				}
			} else if err == nil {
				t.Fatal("invalid directory open succeeded")
			}
		})
	}
	for _, tc := range []struct {
		name, path string
		allow      bool
	}{
		{"symlink-parent-not-cleaned", "nested-link/../only-in-dir", true},
		{"escape-and-reenter", "../granted/file", false},
		{"missing-parent-is-not-cleaned", "missing/../file", false},
		{"internal-parent-file", "dir/../file", true},
		{"file-dot", "file/.", false},
		{"root-dot", ".", true},
		{"root-parent-escape", "..", false}, {"internal-parent", "dir/..", true}, {"file-trailing-slash", "file/", false}, {"directory-trailing-slash", "dir/", true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			file, err := registered.OpenFileNoFollow(tc.path, os.O_RDONLY, 0)
			if file != nil {
				defer file.Close()
			}
			if tc.allow && err != nil {
				t.Fatal(err)
			}
			if !tc.allow && err == nil {
				t.Fatal("invalid rooted path was opened")
			}
		})
	}

	t.Run("directory-needs-exec", func(t *testing.T) {
		denied, err := NewFS(root, 0400, false)
		if err != nil {
			t.Fatal(err)
		}
		defer denied.Close()
		f, err := denied.OpenDirectory("dir", false)
		if f != nil {
			f.Close()
		}
		if err == nil {
			t.Fatal("directory open bypassed registered execute permission")
		}
	})
}
