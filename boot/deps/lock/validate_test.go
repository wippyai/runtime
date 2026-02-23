// SPDX-License-Identifier: MPL-2.0

package lock

import (
	"os"
	"path/filepath"
	"testing"
)

func TestValidate(t *testing.T) {
	t.Run("valid lock file passes", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "wippy/test", Version: "v1.0.0"})
		lock.SetDirectories(Directories{Modules: ".wippy", Src: "./src"})

		if err := Validate(lock); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("invalid module name fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "invalid", Version: "v1.0.0"})

		if err := Validate(lock); err == nil {
			t.Error("expected error for invalid module name")
		}
	})

	t.Run("empty version fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetModule(Module{Name: "wippy/test", Version: ""})

		if err := Validate(lock); err == nil {
			t.Error("expected error for empty version")
		}
	})

	t.Run("empty directories.modules fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: "", Src: "."})

		if err := Validate(lock); err == nil {
			t.Error("expected error for empty directories.modules")
		}
	})

	t.Run("empty directories.src fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: ".wippy", Src: ""})

		if err := Validate(lock); err == nil {
			t.Error("expected error for empty directories.src")
		}
	})

	t.Run("root directory as src allowed", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetDirectories(Directories{Modules: ".wippy", Src: "."})

		if err := Validate(lock); err != nil {
			t.Errorf("expected no error for src directory set to root '.', got %v", err)
		}
	})

	t.Run("invalid replacement fails", func(t *testing.T) {
		tmpDir := t.TempDir()
		lockPath := filepath.Join(tmpDir, "test.lock")

		lock, _ := New(lockPath)
		lock.SetReplacement(Replacement{From: "wippy/test", To: "./nonexistent"})

		if err := Validate(lock); err == nil {
			t.Error("expected error for nonexistent replacement path")
		}
	})
}

func TestValidateReplacements(t *testing.T) {
	tmpDir := t.TempDir()
	lockPath := filepath.Join(tmpDir, "test.lock")

	t.Run("empty from field fails", func(t *testing.T) {
		replacements := []Replacement{{From: "", To: "./local"}}
		if err := ValidateReplacements(lockPath, replacements); err == nil {
			t.Error("expected error for empty from field")
		}
	})

	t.Run("empty to field fails", func(t *testing.T) {
		replacements := []Replacement{{From: "wippy/test", To: ""}}
		if err := ValidateReplacements(lockPath, replacements); err == nil {
			t.Error("expected error for empty to field")
		}
	})

	t.Run("invalid from field format fails", func(t *testing.T) {
		replacements := []Replacement{{From: "invalid", To: "./local"}}
		if err := ValidateReplacements(lockPath, replacements); err == nil {
			t.Error("expected error for invalid from field format")
		}
	})

	t.Run("nonexistent path fails", func(t *testing.T) {
		replacements := []Replacement{{From: "wippy/test", To: "./nonexistent"}}
		if err := ValidateReplacements(lockPath, replacements); err == nil {
			t.Error("expected error for nonexistent path")
		}
	})

	t.Run("existing relative path succeeds", func(t *testing.T) {
		localDir := filepath.Join(tmpDir, "local")
		if err := os.Mkdir(localDir, 0755); err != nil {
			t.Fatalf("create local dir: %v", err)
		}

		replacements := []Replacement{{From: "wippy/test", To: "./local"}}
		if err := ValidateReplacements(lockPath, replacements); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})

	t.Run("existing absolute path succeeds", func(t *testing.T) {
		localDir := filepath.Join(tmpDir, "absolute")
		if err := os.Mkdir(localDir, 0755); err != nil {
			t.Fatalf("create local dir: %v", err)
		}

		replacements := []Replacement{{From: "wippy/test", To: localDir}}
		if err := ValidateReplacements(lockPath, replacements); err != nil {
			t.Errorf("expected no error, got %v", err)
		}
	})
}

func TestValidateModuleName(t *testing.T) {
	tests := []struct {
		name      string
		input     string
		wantError bool
	}{
		{"valid name", "wippy/actor", false},
		{"valid with numbers", "org123/mod456", false},
		{"valid with hyphens", "my-org/my-module", false},
		{"empty string", "", true},
		{"missing org", "/module", true},
		{"missing module", "org/", true},
		{"no slash", "orgmodule", true},
		{"too many slashes", "org/mod/extra", true},
		{"multiple slashes", "org//module", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := ValidateModuleName(tt.input)
			if tt.wantError && err == nil {
				t.Errorf("expected error for %q", tt.input)
			}
			if !tt.wantError && err != nil {
				t.Errorf("expected no error for %q, got %v", tt.input, err)
			}
		})
	}
}
