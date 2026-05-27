// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/wippyai/runtime/boot/deps/graph"
)

func TestNew(t *testing.T) {
	t.Run("creates new lock with defaults when file does not exist", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, err := New(lockPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if lock.Path() != lockPath {
			t.Errorf("expected path %q, got %q", lockPath, lock.Path())
		}

		if lock.data.Directories.Modules != ".wippy" {
			t.Errorf("expected default modules dir, got %q", lock.data.Directories.Modules)
		}

		if lock.data.Directories.Src != "." {
			t.Errorf("expected default src dir, got %q", lock.data.Directories.Src)
		}

		if len(lock.data.Modules) != 0 {
			t.Errorf("expected empty modules, got %d", len(lock.data.Modules))
		}
	})

	t.Run("loads existing lock file", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		content := `directories:
  modules: .custom
  src: ./src
modules:
  - name: wippy/test
    version: v1.0.0
    hash: abc123
`
		if err := os.WriteFile(lockPath, []byte(content), 0600); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		lock, err := New(lockPath)
		if err != nil {
			t.Fatalf("expected no error, got %v", err)
		}

		if lock.data.Directories.Modules != ".custom" {
			t.Errorf("expected custom modules dir, got %q", lock.data.Directories.Modules)
		}

		if len(lock.data.Modules) != 1 {
			t.Fatalf("expected 1 module, got %d", len(lock.data.Modules))
		}

		if lock.data.Modules[0].Name != "wippy/test" {
			t.Errorf("expected module name wippy/test, got %q", lock.data.Modules[0].Name)
		}
	})

	t.Run("returns error on invalid yaml", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		if err := os.WriteFile(lockPath, []byte("invalid: [yaml"), 0600); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		_, err := New(lockPath)
		if err == nil {
			t.Fatal("expected error on invalid yaml")
		}
	})
}

func TestLock_Write(t *testing.T) {
	t.Run("writes lock file with sorted modules", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "wippy/zzz", Version: "v1.0.0"})
		lock.SetModule(Module{Name: "wippy/aaa", Version: "v2.0.0"})

		if err := lock.Write(); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		lock2, err := New(lockPath)
		if err != nil {
			t.Fatalf("reload failed: %v", err)
		}

		if len(lock2.data.Modules) != 2 {
			t.Fatalf("expected 2 modules, got %d", len(lock2.data.Modules))
		}

		if lock2.data.Modules[0].Name != "wippy/aaa" {
			t.Errorf("expected first module to be wippy/aaa, got %q", lock2.data.Modules[0].Name)
		}

		if lock2.data.Modules[1].Name != "wippy/zzz" {
			t.Errorf("expected second module to be wippy/zzz, got %q", lock2.data.Modules[1].Name)
		}
	})

	t.Run("deduplicates modules by name@version", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.data.Modules = []Module{
			{Name: "wippy/test", Version: "v1.0.0", Hash: "abc"},
			{Name: "wippy/test", Version: "v1.0.0", Hash: "def"},
			{Name: "wippy/test", Version: "v2.0.0"},
		}

		if err := lock.Write(); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		lock2, _ := New(lockPath)
		if len(lock2.data.Modules) != 2 {
			t.Fatalf("expected 2 modules after dedup, got %d", len(lock2.data.Modules))
		}
	})

	t.Run("creates file with 0600 permissions", func(t *testing.T) {
		if runtime.GOOS == "windows" {
			t.Skip("Windows does not support Unix file permissions")
		}

		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		if err := lock.Write(); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		info, err := os.Stat(lockPath)
		if err != nil {
			t.Fatalf("stat failed: %v", err)
		}

		if info.Mode().Perm() != 0600 {
			t.Errorf("expected permissions 0600, got %v", info.Mode().Perm())
		}
	})
}

func TestLock_GetModule(t *testing.T) {
	lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))
	lock.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})

	t.Run("returns module when exists", func(t *testing.T) {
		mod, ok := lock.GetModule("wippy/test")
		if !ok {
			t.Fatal("expected module to exist")
		}
		if mod.Name != "wippy/test" {
			t.Errorf("expected name wippy/test, got %q", mod.Name)
		}
		if mod.Version != "v1.0.0" {
			t.Errorf("expected version v1.0.0, got %q", mod.Version)
		}
	})

	t.Run("returns false when module does not exist", func(t *testing.T) {
		_, ok := lock.GetModule("wippy/nonexistent")
		if ok {
			t.Fatal("expected module to not exist")
		}
	})
}

func TestLock_SetModule(t *testing.T) {
	t.Run("adds new module", func(t *testing.T) {
		lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))
		lock.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})

		if len(lock.data.Modules) != 1 {
			t.Fatalf("expected 1 module, got %d", len(lock.data.Modules))
		}
	})

	t.Run("updates existing module", func(t *testing.T) {
		lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))
		lock.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})
		lock.SetModule(Module{Name: "wippy/test", Version: "v2.0.0", Hash: "xyz"})

		if len(lock.data.Modules) != 1 {
			t.Fatalf("expected 1 module, got %d", len(lock.data.Modules))
		}

		mod, _ := lock.GetModule("wippy/test")
		if mod.Version != "v2.0.0" {
			t.Errorf("expected version v2.0.0, got %q", mod.Version)
		}
		if mod.Hash != "xyz" {
			t.Errorf("expected hash xyz, got %q", mod.Hash)
		}
	})
}

func TestLock_RemoveModule(t *testing.T) {
	lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))
	lock.SetModule(Module{Name: "wippy/test1", Version: "v1.0.0"})
	lock.SetModule(Module{Name: "wippy/test2", Version: "v2.0.0"})

	lock.RemoveModule("wippy/test1")

	if len(lock.data.Modules) != 1 {
		t.Fatalf("expected 1 module after removal, got %d", len(lock.data.Modules))
	}

	_, ok := lock.GetModule("wippy/test1")
	if ok {
		t.Error("expected wippy/test1 to be removed")
	}

	_, ok = lock.GetModule("wippy/test2")
	if !ok {
		t.Error("expected wippy/test2 to still exist")
	}
}

func TestLock_Replacements(t *testing.T) {
	lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))

	t.Run("get nonexistent replacement", func(t *testing.T) {
		_, ok := lock.GetReplacement("wippy/test")
		if ok {
			t.Error("expected replacement to not exist")
		}
	})

	t.Run("set and get replacement", func(t *testing.T) {
		lock.SetReplacement(Replacement{From: "wippy/test", To: "./local"})

		r, ok := lock.GetReplacement("wippy/test")
		if !ok {
			t.Fatal("expected replacement to exist")
		}
		if r.From != "wippy/test" || r.To != "./local" {
			t.Errorf("expected wippy/test -> ./local, got %s -> %s", r.From, r.To)
		}
	})

	t.Run("update existing replacement", func(t *testing.T) {
		lock.SetReplacement(Replacement{From: "wippy/test", To: "./updated"})

		if len(lock.data.Replacements) != 1 {
			t.Fatalf("expected 1 replacement, got %d", len(lock.data.Replacements))
		}

		r, _ := lock.GetReplacement("wippy/test")
		if r.To != "./updated" {
			t.Errorf("expected ./updated, got %s", r.To)
		}
	})

	t.Run("remove replacement", func(t *testing.T) {
		lock.RemoveReplacement("wippy/test")

		if len(lock.data.Replacements) != 0 {
			t.Fatalf("expected 0 replacements, got %d", len(lock.data.Replacements))
		}
	})
}

func TestLock_Directories(t *testing.T) {
	lock, _ := New(filepath.Join(t.TempDir(), "test.lock"))

	t.Run("get default directories", func(t *testing.T) {
		dirs := lock.GetDirectories()
		if dirs.Modules != ".wippy" {
			t.Errorf("expected .wippy, got %s", dirs.Modules)
		}
		if dirs.Src != "." {
			t.Errorf("expected ., got %s", dirs.Src)
		}
	})

	t.Run("set directories", func(t *testing.T) {
		lock.SetDirectories(Directories{Modules: ".custom", Src: "./src"})

		dirs := lock.GetDirectories()
		if dirs.Modules != ".custom" {
			t.Errorf("expected .custom, got %s", dirs.Modules)
		}
		if dirs.Src != "./src" {
			t.Errorf("expected ./src, got %s", dirs.Src)
		}
	})
}

func TestLock_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	lock1, _ := New(lockPath)
	lock1.SetDirectories(Directories{Modules: ".vendor", Src: "./source"})
	lock1.SetModule(Module{Name: "wippy/actor", Version: "v1.2.3", Hash: "abc123"})
	lock1.SetModule(Module{Name: "wippy/llm", Version: "v0.0.11"})
	lock1.SetReplacement(Replacement{From: "wippy/local", To: "./local"})

	if err := lock1.Write(); err != nil {
		t.Fatalf("write failed: %v", err)
	}

	lock2, err := New(lockPath)
	if err != nil {
		t.Fatalf("reload failed: %v", err)
	}

	if lock2.GetDirectories() != lock1.GetDirectories() {
		t.Errorf("directories mismatch")
	}

	if len(lock2.GetModules()) != len(lock1.GetModules()) {
		t.Fatalf("module count mismatch: expected %d, got %d",
			len(lock1.GetModules()), len(lock2.GetModules()))
	}

	if len(lock2.GetReplacements()) != len(lock1.GetReplacements()) {
		t.Fatalf("replacement count mismatch")
	}

	mod, ok := lock2.GetModule("wippy/actor")
	if !ok {
		t.Fatal("expected wippy/actor to exist")
	}
	if mod.Hash != "abc123" {
		t.Errorf("expected hash abc123, got %s", mod.Hash)
	}
}

func TestLock_GetLoadPaths(t *testing.T) {
	t.Run("returns app source directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		paths := lock.GetLoadPaths()

		if len(paths) != 1 {
			t.Fatalf("expected 1 path, got %d", len(paths))
		}

		expectedSrc := filepath.Join(tmpDir, ".")
		if paths[0] != expectedSrc {
			t.Errorf("expected %q, got %q", expectedSrc, paths[0])
		}
	})

	t.Run("includes module vendor paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0", Hash: "abc123"})
		lock.SetModule(Module{Name: "demo/sql", Version: "v2.0.0", Hash: "def456"})

		paths := lock.GetLoadPaths()

		if len(paths) != 3 {
			t.Fatalf("expected 3 paths, got %d", len(paths))
		}

		expectedSrc := filepath.Join(tmpDir, ".")
		expectedHTTP := filepath.Join(tmpDir, ".wippy/vendor/acme/http")
		expectedSQL := filepath.Join(tmpDir, ".wippy/vendor/demo/sql")

		if paths[0] != expectedSrc {
			t.Errorf("expected src path %q, got %q", expectedSrc, paths[0])
		}
		if paths[1] != expectedHTTP {
			t.Errorf("expected http path %q, got %q", expectedHTTP, paths[1])
		}
		if paths[2] != expectedSQL {
			t.Errorf("expected sql path %q, got %q", expectedSQL, paths[2])
		}
	})

	t.Run("includes replacement paths", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0"})
		lock.SetReplacement(Replacement{From: "acme/http", To: "../local-http"})

		paths := lock.GetLoadPaths()

		if len(paths) != 2 {
			t.Fatalf("expected 2 paths (src + replacement), got %d", len(paths))
		}

		expectedSrc := filepath.Join(tmpDir, ".")
		expectedRepl := filepath.Join(tmpDir, "../local-http")

		if paths[0] != expectedSrc {
			t.Errorf("expected src path %q, got %q", expectedSrc, paths[0])
		}
		if paths[1] != expectedRepl {
			t.Errorf("expected replacement path %q, got %q", expectedRepl, paths[1])
		}
	})

	t.Run("skips modules with replacements from vendor", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0"})
		lock.SetModule(Module{Name: "demo/sql", Version: "v2.0.0"})
		lock.SetReplacement(Replacement{From: "acme/http", To: "../local-http"})

		paths := lock.GetLoadPaths()

		if len(paths) != 3 {
			t.Fatalf("expected 3 paths, got %d", len(paths))
		}

		expectedSrc := filepath.Join(tmpDir, ".")
		expectedRepl := filepath.Join(tmpDir, "../local-http")
		expectedSQL := filepath.Join(tmpDir, ".wippy/vendor/demo/sql")

		if paths[0] != expectedSrc {
			t.Errorf("expected src %q, got %q", expectedSrc, paths[0])
		}
		if paths[1] != expectedRepl {
			t.Errorf("expected replacement %q, got %q", expectedRepl, paths[1])
		}
		if paths[2] != expectedSQL {
			t.Errorf("expected sql %q, got %q", expectedSQL, paths[2])
		}

		for _, path := range paths {
			if filepath.Base(path) == "http" && filepath.Dir(path) != filepath.Dir(expectedRepl) {
				t.Error("expected acme/http to be loaded from replacement, not vendor")
			}
		}
	})

	t.Run("uses custom modules directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: ".custom", Src: "."})
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0"})

		paths := lock.GetLoadPaths()

		expectedHTTP := filepath.Join(tmpDir, ".custom/vendor/acme/http")
		if paths[1] != expectedHTTP {
			t.Errorf("expected custom path %q, got %q", expectedHTTP, paths[1])
		}
	})

	t.Run("keeps absolute src modules and replacement paths absolute", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")
		absSrc := filepath.Join(tmpDir, "abs-src")
		absModules := filepath.Join(tmpDir, "abs-modules")
		absReplacement := filepath.Join(tmpDir, "abs-replacement")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: absModules, Src: absSrc})
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0"})
		lock.SetModule(Module{Name: "demo/sql", Version: "v2.0.0"})
		lock.SetReplacement(Replacement{From: "demo/sql", To: absReplacement})

		paths := lock.GetLoadPaths()

		expectedHTTP := filepath.Join(absModules, "vendor", "acme", "http")
		if len(paths) != 3 {
			t.Fatalf("expected 3 paths, got %d: %v", len(paths), paths)
		}
		if paths[0] != absSrc {
			t.Fatalf("src path = %q, want %q", paths[0], absSrc)
		}
		if paths[1] != absReplacement {
			t.Fatalf("replacement path = %q, want %q", paths[1], absReplacement)
		}
		if paths[2] != expectedHTTP {
			t.Fatalf("module path = %q, want %q", paths[2], expectedHTTP)
		}
	})

	t.Run("handles empty src directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: ".wippy", Src: ""})
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0"})

		paths := lock.GetLoadPaths()

		if len(paths) != 1 {
			t.Fatalf("expected 1 path (no src), got %d", len(paths))
		}

		expectedHTTP := filepath.Join(tmpDir, ".wippy/vendor/acme/http")
		if paths[0] != expectedHTTP {
			t.Errorf("expected %q, got %q", expectedHTTP, paths[0])
		}
	})

	t.Run("paths do not include version or hash", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "acme/http", Version: "v1.0.0", Hash: "abc123def456"})

		paths := lock.GetLoadPaths()

		expectedHTTP := filepath.Join(tmpDir, ".wippy/vendor/acme/http")
		found := false
		for _, path := range paths {
			if path == expectedHTTP {
				found = true
			}
			if strings.Contains(path, "abc123def456") {
				t.Errorf("path should not contain hash, got %q", path)
			}
			if strings.Contains(path, "v1.0.0") {
				t.Errorf("path should not contain version, got %q", path)
			}
		}
		if !found {
			t.Errorf("expected path %q not found in %v", expectedHTTP, paths)
		}
	})
}

func TestLock_Options(t *testing.T) {
	t.Run("default options", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, err := New(lockPath)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}

		opts := lock.GetOptions()
		if opts.UnpackModules != false {
			t.Errorf("expected UnpackModules to be false by default, got %v", opts.UnpackModules)
		}

		if lock.ShouldUnpackModules() != false {
			t.Errorf("expected ShouldUnpackModules to be false by default")
		}
	})

	t.Run("set and get options", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetOptions(Options{UnpackModules: true})

		opts := lock.GetOptions()
		if opts.UnpackModules != true {
			t.Errorf("expected UnpackModules to be true, got %v", opts.UnpackModules)
		}

		if lock.ShouldUnpackModules() != true {
			t.Errorf("expected ShouldUnpackModules to be true")
		}
	})

	t.Run("options persisted to file", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetOptions(Options{UnpackModules: true})
		if err := lock.Write(); err != nil {
			t.Fatalf("write failed: %v", err)
		}

		lock2, err := New(lockPath)
		if err != nil {
			t.Fatalf("reload failed: %v", err)
		}

		if lock2.ShouldUnpackModules() != true {
			t.Errorf("expected options to persist, got ShouldUnpackModules=%v", lock2.ShouldUnpackModules())
		}
	})

	t.Run("load options from existing file", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		content := `directories:
  modules: .wippy
  src: ./src
options:
  unpack_modules: true
modules: []
`
		if err := os.WriteFile(lockPath, []byte(content), 0600); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		lock, err := New(lockPath)
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}

		if lock.ShouldUnpackModules() != true {
			t.Errorf("expected options to be loaded from file")
		}
	})
}

func TestLock_GetLoadPaths_EdgeCases(t *testing.T) {
	t.Run("skips module with invalid name", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		content := `directories:
  modules: .wippy
  src: ./src
modules:
  - name: invalid-no-slash
    version: v1.0.0
    hash: abc123
  - name: valid/module
    version: v1.0.0
    hash: def456
`
		if err := os.WriteFile(lockPath, []byte(content), 0600); err != nil {
			t.Fatalf("write test file: %v", err)
		}

		lock, err := New(lockPath)
		if err != nil {
			t.Fatalf("load failed: %v", err)
		}

		paths := lock.GetLoadPaths()

		// Should have src + valid module (invalid module silently skipped)
		if len(paths) != 2 {
			t.Errorf("expected 2 paths (src + valid module), got %d: %v", len(paths), paths)
		}

		// Verify invalid module is not in paths
		for _, path := range paths {
			if strings.Contains(path, "invalid-no-slash") {
				t.Errorf("invalid module should not be in paths: %v", paths)
			}
		}
	})

	t.Run("empty modules list", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		paths := lock.GetLoadPaths()

		if len(paths) != 1 {
			t.Errorf("expected 1 path (src only), got %d", len(paths))
		}
	})
}

func TestResolveModuleDir_Scenarios(t *testing.T) {
	t.Run("prefers extracted directory over wapp", func(t *testing.T) {
		tmpDir := t.TempDir()
		vendorDir := filepath.Join(tmpDir, "vendor")

		// Create both directory and wapp file
		dirPath := filepath.Join(vendorDir, "acme/http")
		wappPath := filepath.Join(vendorDir, "acme/http-v1.0.0.wapp")

		if err := os.MkdirAll(dirPath, 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(wappPath, []byte("fake wapp"), 0644); err != nil {
			t.Fatalf("write wapp: %v", err)
		}

		name, _ := graph.ParseName("acme/http")
		resolved := ResolveModuleDir(vendorDir, name, "v1.0.0")

		if resolved.Path != dirPath {
			t.Errorf("expected dir path %q, got %q", dirPath, resolved.Path)
		}
		if resolved.IsWapp {
			t.Error("expected IsWapp to be false when directory exists")
		}
	})

	t.Run("falls back to wapp when no directory", func(t *testing.T) {
		tmpDir := t.TempDir()
		vendorDir := filepath.Join(tmpDir, "vendor")

		// Create only wapp file
		wappPath := filepath.Join(vendorDir, "acme/http-v1.0.0.wapp")
		if err := os.MkdirAll(filepath.Dir(wappPath), 0755); err != nil {
			t.Fatalf("mkdir: %v", err)
		}
		if err := os.WriteFile(wappPath, []byte("fake wapp"), 0644); err != nil {
			t.Fatalf("write wapp: %v", err)
		}

		name, _ := graph.ParseName("acme/http")
		resolved := ResolveModuleDir(vendorDir, name, "v1.0.0")

		if resolved.Path != wappPath {
			t.Errorf("expected wapp path %q, got %q", wappPath, resolved.Path)
		}
		if !resolved.IsWapp {
			t.Error("expected IsWapp to be true for wapp file")
		}
	})

	t.Run("returns preferred path when nothing exists", func(t *testing.T) {
		tmpDir := t.TempDir()
		vendorDir := filepath.Join(tmpDir, "vendor")

		name, _ := graph.ParseName("acme/http")
		resolved := ResolveModuleDir(vendorDir, name, "v1.0.0")

		expectedPath := filepath.Join(vendorDir, "acme/http")
		if resolved.Path != expectedPath {
			t.Errorf("expected preferred path %q, got %q", expectedPath, resolved.Path)
		}
		if resolved.IsWapp || resolved.IsLegacy {
			t.Error("expected neither IsWapp nor IsLegacy for nonexistent path")
		}
	})
}

func TestWappPath(t *testing.T) {
	name, _ := graph.ParseName("acme/http")

	path := WappPath(name, "v1.0.0")
	expected := filepath.Join("acme", "http-v1.0.0.wapp")

	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}

func TestModulePath(t *testing.T) {
	name, _ := graph.ParseName("acme/http")

	path := ModulePath(name)
	expected := filepath.Join("acme", "http")

	if path != expected {
		t.Errorf("expected %q, got %q", expected, path)
	}
}
