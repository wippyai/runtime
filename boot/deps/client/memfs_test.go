package client

import (
	"io/fs"
	"testing"
	"testing/fstest"

	modulev1 "github.com/wippyai/module-registry-proto-go/registry/module/v1"
)

func TestNewMemFS(t *testing.T) {
	t.Run("creates filesystem from yaml files", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "config.yaml", Content: []byte("key: value")},
			{Path: "app.yml", Content: []byte("app: test")},
			{Path: "script.lua", Content: []byte("-- ignored")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		yamlContent, err := fs.ReadFile(memfs, "config.yaml")
		if err != nil {
			t.Errorf("failed to read config.yaml: %v", err)
		}
		if string(yamlContent) != "key: value" {
			t.Errorf("unexpected content: %s", yamlContent)
		}

		ymlContent, err := fs.ReadFile(memfs, "app.yml")
		if err != nil {
			t.Errorf("failed to read app.yml: %v", err)
		}
		if string(ymlContent) != "app: test" {
			t.Errorf("unexpected content: %s", ymlContent)
		}

		_, err = fs.ReadFile(memfs, "script.lua")
		if err == nil {
			t.Error("expected error for non-yaml file, got nil")
		}
	})

	t.Run("handles nested directories", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "main.yaml", Content: []byte("main")},
			{Path: "config/app.yaml", Content: []byte("app")},
			{Path: "config/db.yml", Content: []byte("db")},
			{Path: "lib/helper.yaml", Content: []byte("helper")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		if err := fstest.TestFS(memfs, "main.yaml", "config/app.yaml", "config/db.yml", "lib/helper.yaml"); err != nil {
			t.Errorf("fstest.TestFS failed: %v", err)
		}
	})

	t.Run("filters non-yaml files", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "config.yaml", Content: []byte("yaml")},
			{Path: "script.lua", Content: []byte("lua")},
			{Path: "data.json", Content: []byte("json")},
			{Path: "readme.md", Content: []byte("markdown")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		content, err := fs.ReadFile(memfs, "config.yaml")
		if err != nil {
			t.Fatalf("failed to read config.yaml: %v", err)
		}
		if string(content) != "yaml" {
			t.Errorf("unexpected content: %s", content)
		}

		for _, path := range []string{"script.lua", "data.json", "readme.md"} {
			_, err := fs.ReadFile(memfs, path)
			if err == nil {
				t.Errorf("expected error for %s, got nil", path)
			}
		}
	})

	t.Run("rejects absolute paths", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "/etc/config.yaml", Content: []byte("content")},
		}

		_, err := NewMemFS(files)
		if err == nil {
			t.Fatal("expected error for absolute path, got nil")
		}
	})

	t.Run("rejects invalid paths", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "../escape.yaml", Content: []byte("content")},
		}

		_, err := NewMemFS(files)
		if err == nil {
			t.Fatal("expected error for invalid path, got nil")
		}
	})

	t.Run("skips nil files", func(t *testing.T) {
		files := []*modulev1.File{
			nil,
			{Path: "config.yaml", Content: []byte("content")},
			nil,
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		content, err := fs.ReadFile(memfs, "config.yaml")
		if err != nil {
			t.Fatalf("failed to read config.yaml: %v", err)
		}
		if string(content) != "content" {
			t.Errorf("unexpected content: %s", content)
		}
	})

	t.Run("skips empty paths", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "", Content: []byte("content")},
			{Path: "config.yaml", Content: []byte("valid")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		content, err := fs.ReadFile(memfs, "config.yaml")
		if err != nil {
			t.Fatalf("failed to read config.yaml: %v", err)
		}
		if string(content) != "valid" {
			t.Errorf("unexpected content: %s", content)
		}
	})

	t.Run("creates empty filesystem", func(t *testing.T) {
		memfs, err := NewMemFS([]*modulev1.File{})
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		_, err = fs.ReadFile(memfs, "config.yaml")
		if err == nil {
			t.Error("expected error for missing file, got nil")
		}
	})
}

func TestMemFS_ReadDir(t *testing.T) {
	t.Run("reads root directory", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "a.yaml", Content: []byte("a")},
			{Path: "b.yml", Content: []byte("b")},
			{Path: "config/c.yaml", Content: []byte("c")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		entries, err := fs.ReadDir(memfs, ".")
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}

		if len(entries) != 3 {
			t.Fatalf("expected 3 entries, got %d", len(entries))
		}

		names := make([]string, len(entries))
		for i, entry := range entries {
			names[i] = entry.Name()
		}

		hasFile := func(name string) bool {
			for _, n := range names {
				if n == name {
					return true
				}
			}
			return false
		}

		if !hasFile("a.yaml") || !hasFile("b.yml") || !hasFile("config") {
			t.Errorf("unexpected entries: %v", names)
		}
	})

	t.Run("reads subdirectory", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "config/app.yaml", Content: []byte("app")},
			{Path: "config/db.yml", Content: []byte("db")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		entries, err := fs.ReadDir(memfs, "config")
		if err != nil {
			t.Fatalf("ReadDir failed: %v", err)
		}

		if len(entries) != 2 {
			t.Fatalf("expected 2 entries, got %d", len(entries))
		}
	})

	t.Run("returns error for non-existent directory", func(t *testing.T) {
		memfs, err := NewMemFS([]*modulev1.File{})
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		_, err = fs.ReadDir(memfs, "missing")
		if err == nil {
			t.Error("expected error for non-existent directory, got nil")
		}
	})
}

func TestIsYAMLFile(t *testing.T) {
	tests := []struct {
		path     string
		expected bool
	}{
		{"config.yaml", true},
		{"config.yml", true},
		{"CONFIG.YAML", true},
		{"CONFIG.YML", true},
		{"script.lua", false},
		{"data.json", false},
		{"readme.md", false},
		{"no-extension", false},
		{"path/to/config.yaml", true},
		{"path/to/script.lua", false},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			result := isYAMLFile(tt.path)
			if result != tt.expected {
				t.Errorf("isYAMLFile(%q) = %v, want %v", tt.path, result, tt.expected)
			}
		})
	}
}

func TestMemFile_Read(t *testing.T) {
	t.Run("reads file content in chunks", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "test.yaml", Content: []byte("hello world")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		file, err := memfs.Open("test.yaml")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		buf := make([]byte, 5)
		n, err := file.Read(buf)
		if err != nil {
			t.Fatalf("first Read failed: %v", err)
		}
		if n != 5 {
			t.Errorf("expected 5 bytes, got %d", n)
		}
		if string(buf) != "hello" {
			t.Errorf("expected 'hello', got %q", string(buf))
		}

		n, err = file.Read(buf)
		if err != nil {
			t.Fatalf("second Read failed: %v", err)
		}
		if n != 5 {
			t.Errorf("expected 5 bytes, got %d", n)
		}
		if string(buf[:n]) != " worl" {
			t.Errorf("expected ' worl', got %q", string(buf[:n]))
		}
	})

	t.Run("returns EOF when content exhausted", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "small.yaml", Content: []byte("hi")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		file, err := memfs.Open("small.yaml")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer file.Close()

		buf := make([]byte, 10)
		n, err := file.Read(buf)
		if n != 2 {
			t.Errorf("expected 2 bytes, got %d", n)
		}
		if string(buf[:n]) != "hi" {
			t.Errorf("expected 'hi', got %q", string(buf[:n]))
		}
	})
}

func TestMemDir_Read(t *testing.T) {
	t.Run("returns error when reading directory", func(t *testing.T) {
		files := []*modulev1.File{
			{Path: "dir/file.yaml", Content: []byte("content")},
		}

		memfs, err := NewMemFS(files)
		if err != nil {
			t.Fatalf("NewMemFS failed: %v", err)
		}

		dir, err := memfs.Open("dir")
		if err != nil {
			t.Fatalf("Open failed: %v", err)
		}
		defer dir.Close()

		buf := make([]byte, 10)
		_, err = dir.Read(buf)
		if err == nil {
			t.Error("expected error when reading directory, got nil")
		}
	})
}
