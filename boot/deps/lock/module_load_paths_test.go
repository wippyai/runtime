// SPDX-License-Identifier: MPL-2.0

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

		if got := paths[1]; got.Path != filepath.Join(tmpDir, "local/users") || got.SourceRoot != filepath.Join(tmpDir, "local/users") || got.Module != "userspace/users" || got.Version != "" {
			t.Fatalf("replacement path = %+v, want replacement module and empty version", got)
		}

		if got := paths[2]; got.Path != filepath.Join(tmpDir, ".wippy", "vendor", "demo", "sql") || got.SourceRoot != filepath.Join(tmpDir, ".wippy", "vendor", "demo", "sql") || got.Module != "demo/sql" || got.Version != "v2.0.0" {
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

	t.Run("uses unpacked directory when module is unpacked", func(t *testing.T) {
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
		l.SetOptions(Options{UnpackModules: true})
		l.SetModule(Module{
			Name:    "userspace/users",
			Version: "v1.2.3",
		})

		unpackedPath := filepath.Join(tmpDir, ".wippy", "vendor", "userspace", "users")
		wappPath := filepath.Join(tmpDir, ".wippy", "vendor", "userspace", "users-v1.2.3.wapp")
		for _, dir := range []string{unpackedPath, filepath.Dir(wappPath)} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir vendor path: %v", err)
			}
		}
		if err := os.WriteFile(wappPath, []byte("stale pack"), 0o644); err != nil {
			t.Fatalf("write wapp file: %v", err)
		}

		paths := l.GetModuleLoadPaths()
		if len(paths) != 2 {
			t.Fatalf("path count = %d, want 2", len(paths))
		}

		got := paths[1]
		if got.Path != unpackedPath {
			t.Fatalf("module path = %q, want unpacked path %q", got.Path, unpackedPath)
		}
		if got.SourceRoot != unpackedPath {
			t.Fatalf("source root = %q, want unpacked path %q", got.SourceRoot, unpackedPath)
		}
	})

	t.Run("absolute replacement path is not joined to lock directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, DefaultFilename)
		replacementDir := filepath.Join(tmpDir, "absolute", "ui")

		l, err := New(lockPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		l.SetDirectories(Directories{
			Modules: ".wippy",
			Src:     "app",
		})
		l.SetModule(Module{
			Name:    "acme/ui",
			Version: "v1.0.0",
		})
		l.SetReplacement(Replacement{
			From: "acme/ui",
			To:   replacementDir,
		})

		if err := os.MkdirAll(filepath.Join(replacementDir, "src"), 0o755); err != nil {
			t.Fatalf("mkdir replacement src: %v", err)
		}

		paths := l.GetModuleLoadPaths()
		if len(paths) != 2 {
			t.Fatalf("path count = %d, want 2", len(paths))
		}

		got := paths[1]
		if got.Path != filepath.Join(replacementDir, "src") {
			t.Fatalf("module path = %q, want %q", got.Path, filepath.Join(replacementDir, "src"))
		}
		if got.SourceRoot != replacementDir {
			t.Fatalf("source root = %q, want %q", got.SourceRoot, replacementDir)
		}
	})

	t.Run("replacement wins over stale unpacked and packed artifacts", func(t *testing.T) {
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
		l.SetModule(Module{
			Name:    "acme/ui",
			Version: "v1.0.0",
		})
		l.SetReplacement(Replacement{
			From: "acme/ui",
			To:   "../local-ui",
		})

		staleDir := filepath.Join(tmpDir, ".wippy", "vendor", "acme", "ui")
		staleWapp := filepath.Join(tmpDir, ".wippy", "vendor", "acme", "ui-v1.0.0.wapp")
		replacementDir := filepath.Join(tmpDir, "..", "local-ui")
		for _, dir := range []string{staleDir, filepath.Dir(staleWapp), filepath.Join(replacementDir, "src")} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("mkdir %s: %v", dir, err)
			}
		}
		if err := os.WriteFile(staleWapp, []byte("stale pack"), 0o644); err != nil {
			t.Fatalf("write stale wapp: %v", err)
		}

		paths := l.GetModuleLoadPaths()
		if len(paths) != 2 {
			t.Fatalf("path count = %d, want 2", len(paths))
		}

		got := paths[1]
		wantReplacement := filepath.Join(tmpDir, "../local-ui")
		if got.Path != filepath.Join(wantReplacement, "src") {
			t.Fatalf("module path = %q, want replacement source %q", got.Path, filepath.Join(wantReplacement, "src"))
		}
		if got.SourceRoot != wantReplacement {
			t.Fatalf("module source root = %q, want replacement root %q", got.SourceRoot, wantReplacement)
		}
		if got.Module != "acme/ui" {
			t.Fatalf("module = %q, want acme/ui", got.Module)
		}
		if got.Version != "" {
			t.Fatalf("replacement version = %q, want empty", got.Version)
		}
	})

	t.Run("replacement without src subdirectory loads from replacement root", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, DefaultFilename)

		l, err := New(lockPath)
		if err != nil {
			t.Fatalf("New failed: %v", err)
		}

		l.SetDirectories(Directories{Modules: ".wippy", Src: "app"})
		l.SetReplacement(Replacement{From: "acme/plain", To: "local/plain"})

		replacementDir := filepath.Join(tmpDir, "local", "plain")
		if err := os.MkdirAll(replacementDir, 0o755); err != nil {
			t.Fatalf("mkdir replacement: %v", err)
		}

		paths := l.GetModuleLoadPaths()
		if len(paths) != 2 {
			t.Fatalf("path count = %d, want 2", len(paths))
		}

		got := paths[1]
		if got.Path != replacementDir {
			t.Fatalf("module path = %q, want replacement root %q", got.Path, replacementDir)
		}
		if got.SourceRoot != replacementDir {
			t.Fatalf("source root = %q, want replacement root %q", got.SourceRoot, replacementDir)
		}
	})
}
