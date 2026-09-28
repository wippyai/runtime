package directory

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"

	fsapi "github.com/wippyai/runtime/api/fs"
)

func TestOpenFileRegisteredAccessMode(t *testing.T) {
	for _, tc := range []struct {
		name  string
		flag  int
		mode  fs.FileMode
		allow bool
	}{
		{name: "read-denied", mode: 0200, flag: os.O_RDONLY, allow: false},
		{name: "write-denied", mode: 0400, flag: os.O_WRONLY, allow: false},
		{name: "read-write-needs-read", mode: 0200, flag: os.O_RDWR, allow: false},
		{name: "readonly-truncate-denied", mode: 0400, flag: os.O_RDONLY | os.O_TRUNC, allow: false},
		{name: "readonly-create-denied", mode: 0400, flag: os.O_RDONLY | os.O_CREATE, allow: false},
		{name: "readonly-append-denied", mode: 0400, flag: os.O_RDONLY | os.O_APPEND, allow: false},
		{name: "read-allowed", mode: 0400, flag: os.O_RDONLY, allow: true},
		{name: "write-allowed", mode: 0200, flag: os.O_WRONLY, allow: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			root := t.TempDir()
			path := filepath.Join(root, "file")
			if err := os.WriteFile(path, []byte("unchanged"), 0600); err != nil {
				t.Fatal(err)
			}
			registered, err := NewFS(root, tc.mode, false)
			if err != nil {
				t.Fatal(err)
			}
			defer registered.Close()
			file, err := registered.OpenFile("file", tc.flag, 0600)
			if file != nil {
				defer file.Close()
			}
			if tc.allow {
				if err != nil {
					t.Fatal(err)
				}
			} else if !errors.Is(err, fsapi.ErrPermissionDenied) {
				t.Fatalf("expected permission denial, got %v", err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if string(content) != "unchanged" {
				t.Fatalf("denied open mutated file: %q", content)
			}
		})
	}
}
