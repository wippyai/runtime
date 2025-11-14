package storage

import (
	"io/fs"
	"testing"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

func TestStripLegacyPrefix(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "strips module- prefix with single level",
			input:    "module-llm-v0.0.11/llm.lua",
			expected: "llm.lua",
		},
		{
			name:     "strips module- prefix with nested path",
			input:    "module-actor-v1.2.3/subdir/actor.lua",
			expected: "subdir/actor.lua",
		},
		{
			name:     "strips generic module- prefix",
			input:    "module-test/file.txt",
			expected: "file.txt",
		},
		{
			name:     "preserves path without module- prefix",
			input:    "regular/path.lua",
			expected: "regular/path.lua",
		},
		{
			name:     "preserves single file without prefix",
			input:    "file.lua",
			expected: "file.lua",
		},
		{
			name:     "preserves path with module- in middle",
			input:    "dir/module-test/file.lua",
			expected: "dir/module-test/file.lua",
		},
		{
			name:     "strips only first component",
			input:    "module-a/module-b/file.lua",
			expected: "module-b/file.lua",
		},
		{
			name:     "handles deeply nested paths",
			input:    "module-deep/a/b/c/d/file.lua",
			expected: "a/b/c/d/file.lua",
		},
		{
			name:     "handles module- prefix alone",
			input:    "module-/file.lua",
			expected: "file.lua",
		},
		{
			name:     "preserves empty path",
			input:    "",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := stripLegacyPrefix(tt.input)
			if result != tt.expected {
				t.Errorf("stripLegacyPrefix(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestDetectLegacyPrefix(t *testing.T) {
	tests := []struct {
		name     string
		files    []*modulev1.File
		expected bool
	}{
		{
			name: "detects legacy prefix",
			files: []*modulev1.File{
				{Path: "module-llm-v0.0.11/llm.lua"},
			},
			expected: true,
		},
		{
			name: "detects legacy prefix with multiple files",
			files: []*modulev1.File{
				{Path: "module-actor/actor.lua"},
				{Path: "module-actor/models.lua"},
			},
			expected: true,
		},
		{
			name: "no detection for regular path",
			files: []*modulev1.File{
				{Path: "regular/path.lua"},
			},
			expected: false,
		},
		{
			name: "no detection for single file",
			files: []*modulev1.File{
				{Path: "file.lua"},
			},
			expected: false,
		},
		{
			name: "no detection for module- in middle",
			files: []*modulev1.File{
				{Path: "dir/module-test/file.lua"},
			},
			expected: false,
		},
		{
			name:     "no detection for empty file list",
			files:    []*modulev1.File{},
			expected: false,
		},
		{
			name:     "no detection for nil file list",
			files:    nil,
			expected: false,
		},
		{
			name: "detects legacy prefix even if second file is different",
			files: []*modulev1.File{
				{Path: "module-test/file.lua"},
				{Path: "regular/file.lua"},
			},
			expected: true,
		},
		{
			name: "no detection for module without slash",
			files: []*modulev1.File{
				{Path: "module-test"},
			},
			expected: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := detectLegacyPrefix(tt.files)
			if result != tt.expected {
				t.Errorf("detectLegacyPrefix() = %v, want %v", result, tt.expected)
			}
		})
	}
}

func TestLegacyIntegration(t *testing.T) {
	t.Run("legacy files are stored without prefix", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{
			{Path: "module-integration-v1.0.0/main.lua", Content: []byte("main")},
			{Path: "module-integration-v1.0.0/lib/helper.lua", Content: []byte("helper")},
			{Path: "module-integration-v1.0.0/lib/utils.lua", Content: []byte("utils")},
		}

		if err := storage.StoreProtoFiles("org/module", files); err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		moduleFS, err := storage.ReadFS("org/module")
		if err != nil {
			t.Fatalf("ReadFS failed: %v", err)
		}

		expectedFiles := map[string]string{
			"main.lua":       "main",
			"lib/helper.lua": "helper",
			"lib/utils.lua":  "utils",
		}

		for path, expectedContent := range expectedFiles {
			content, err := fs.ReadFile(moduleFS, path)
			if err != nil {
				t.Errorf("file %s not found: %v", path, err)
				continue
			}
			if string(content) != expectedContent {
				t.Errorf("file %s: expected %q, got %q", path, expectedContent, string(content))
			}
		}
	})

	t.Run("non-legacy files are stored as-is", func(t *testing.T) {
		tmpDir := t.TempDir()
		storage := NewFileSystemStorage(tmpDir)

		files := []*modulev1.File{
			{Path: "main.lua", Content: []byte("main")},
			{Path: "lib/helper.lua", Content: []byte("helper")},
		}

		if err := storage.StoreProtoFiles("org/module", files); err != nil {
			t.Fatalf("StoreProtoFiles failed: %v", err)
		}

		moduleFS, err := storage.ReadFS("org/module")
		if err != nil {
			t.Fatalf("ReadFS failed: %v", err)
		}

		main, err := fs.ReadFile(moduleFS, "main.lua")
		if err != nil || string(main) != "main" {
			t.Errorf("expected main content")
		}

		helper, err := fs.ReadFile(moduleFS, "lib/helper.lua")
		if err != nil || string(helper) != "helper" {
			t.Errorf("expected helper content")
		}
	})
}
