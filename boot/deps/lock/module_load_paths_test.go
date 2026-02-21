package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLock_GetModuleLoadPaths(t *testing.T) {
	t.Run("includes src replacement and non-replaced module with metadata", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, DefaultFilename)

		l, err := New(lockPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		l.SetDirectories(Directories{
			Modules: ".wippy",
			Src:     "app",
		})
		l.SetReplacement(Replacement{
			From: "userspace/users",
			To:   "local/users",
		})
		l.SetModule(Module{
			Name:    "userspace/users",
			Version: "v1.2.3",
		})
		l.SetModule(Module{
			Name:    "demo/sql",
			Version: "v2.0.0",
		})

		paths := l.GetModuleLoadPaths()
		if len(paths) != 3 {
			t.Fatalf("path count = %d, want 3", len(paths))
		}

		if got := paths[0]; got.Path != filepath.Join(tmpDir, "app") || got.Module != "" || got.Version != "" {
			t.Fatalf("src path = %+v, want app path with empty module/version", got)
		}

		if got := paths[1]; got.Path != filepath.Join(tmpDir, "local/users") || got.Module != "userspace/users" || got.Version != "" {
			t.Fatalf("replacement path = %+v, want replacement module and empty version", got)
		}

		if got := paths[2]; got.Path != filepath.Join(tmpDir, ".wippy", "vendor", "demo", "sql") || got.Module != "demo/sql" || got.Version != "v2.0.0" {
			t.Fatalf("module path = %+v, want vendor module path with metadata", got)
		}
	})

	t.Run("uses .wapp path when module is not unpacked", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, DefaultFilename)

		l, err := New(lockPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		l.SetDirectories(Directories{
			Modules: ".wippy",
			Src:     ".",
		})
		l.SetModule(Module{
			Name:    "userspace/users",
			Version: "v1.2.3",
		})

		wappPath := filepath.Join(tmpDir, ".wippy", "vendor", "userspace", "users-v1.2.3.wapp")
		if err := os.MkdirAll(filepath.Dir(wappPath), 0o755); err != nil {
			t.Fatalf("mkdir vendor path: %v", err)
		}
		if err := os.WriteFile(wappPath, []byte("pack"), 0o644); err != nil {
			t.Fatalf("write wapp file: %v", err)
		}

		paths := l.GetModuleLoadPaths()
		if len(paths) != 2 {
			t.Fatalf("path count = %d, want 2", len(paths))
		}

		got := paths[1]
		if got.Path != wappPath {
			t.Fatalf("module path = %q, want %q", got.Path, wappPath)
		}
		if got.Module != "userspace/users" {
			t.Fatalf("module = %q, want userspace/users", got.Module)
		}
		if got.Version != "v1.2.3" {
			t.Fatalf("version = %q, want v1.2.3", got.Version)
		}
	})
}
