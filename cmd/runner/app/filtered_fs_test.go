package app

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"
)

func TestFilteredFS_Open(t *testing.T) {
	// Create a test filesystem
	fsys := fstest.MapFS{
		"file.txt":              &fstest.MapFile{Data: []byte("content")},
		"excluded/file.txt":     &fstest.MapFile{Data: []byte("excluded")},
		"excluded/sub/file.txt": &fstest.MapFile{Data: []byte("excluded sub")},
		"normal/file.txt":       &fstest.MapFile{Data: []byte("normal")},
		"another.txt":           &fstest.MapFile{Data: []byte("another")},
	}

	tests := []struct {
		name        string
		excludeDirs []string
		openPath    string
		wantErr     bool
		errType     error
	}{
		{
			name:        "open normal file",
			excludeDirs: []string{"excluded"},
			openPath:    "file.txt",
			wantErr:     false,
		},
		{
			name:        "open excluded file",
			excludeDirs: []string{"excluded"},
			openPath:    "excluded/file.txt",
			wantErr:     true,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open excluded subdirectory",
			excludeDirs: []string{"excluded"},
			openPath:    "excluded/sub/file.txt",
			wantErr:     true,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open with multiple exclusions",
			excludeDirs: []string{"excluded", "normal"},
			openPath:    "normal/file.txt",
			wantErr:     true,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open non-excluded with multiple exclusions",
			excludeDirs: []string{"excluded", "normal"},
			openPath:    "another.txt",
			wantErr:     false,
		},
		{
			name:        "no exclusions",
			excludeDirs: []string{},
			openPath:    "excluded/file.txt",
			wantErr:     false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := newFilteredFS(fsys, tt.excludeDirs)

			file, err := filtered.Open(tt.openPath)
			if tt.wantErr {
				if err == nil {
					t.Errorf("Open() expected error, got nil")
					return
				}
				if tt.errType != nil && !errors.Is(err, tt.errType) {
					t.Errorf("Open() error = %v, want %v", err, tt.errType)
				}
			} else {
				if err != nil {
					t.Errorf("Open() unexpected error: %v", err)
					return
				}
				if file == nil {
					t.Errorf("Open() returned nil file")
				}
				file.Close()
			}
		})
	}
}

func TestFilteredFS_ReadDir(t *testing.T) {
	// Create a test filesystem
	fsys := fstest.MapFS{
		"file1.txt":          &fstest.MapFile{Data: []byte("file1")},
		"file2.txt":          &fstest.MapFile{Data: []byte("file2")},
		"excluded/file.txt":  &fstest.MapFile{Data: []byte("excluded")},
		"normal/file.txt":    &fstest.MapFile{Data: []byte("normal")},
		"another/file.txt":   &fstest.MapFile{Data: []byte("another")},
		"excluded2/file.txt": &fstest.MapFile{Data: []byte("excluded2")},
	}

	tests := []struct {
		name         string
		excludeDirs  []string
		readPath     string
		wantContains []string
		wantExcludes []string
	}{
		{
			name:         "read root with single exclusion",
			excludeDirs:  []string{"excluded"},
			readPath:     ".",
			wantContains: []string{"file1.txt", "file2.txt", "normal", "another"},
			wantExcludes: []string{"excluded"},
		},
		{
			name:         "read root with multiple exclusions",
			excludeDirs:  []string{"excluded", "excluded2"},
			readPath:     ".",
			wantContains: []string{"file1.txt", "file2.txt", "normal", "another"},
			wantExcludes: []string{"excluded", "excluded2"},
		},
		{
			name:         "read root with no exclusions",
			excludeDirs:  []string{},
			readPath:     ".",
			wantContains: []string{"file1.txt", "file2.txt", "excluded", "normal", "another", "excluded2"},
			wantExcludes: []string{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered := newFilteredFS(fsys, tt.excludeDirs)

			entries, err := filtered.ReadDir(tt.readPath)
			if err != nil {
				t.Errorf("ReadDir() unexpected error: %v", err)
				return
			}

			entryNames := make(map[string]bool)
			for _, entry := range entries {
				entryNames[entry.Name()] = true
			}

			// Check that wanted entries are present
			for _, want := range tt.wantContains {
				if !entryNames[want] {
					t.Errorf("ReadDir() missing expected entry: %s, got entries: %v", want, entryNames)
				}
			}

			// Check that excluded entries are not present
			for _, exclude := range tt.wantExcludes {
				if entryNames[exclude] {
					t.Errorf("ReadDir() contains excluded entry: %s", exclude)
				}
			}
		})
	}
}

func TestFilteredFS_shouldExclude(t *testing.T) {
	tests := []struct {
		name        string
		excludeDirs []string
		path        string
		want        bool
	}{
		{
			name:        "exclude exact match",
			excludeDirs: []string{"excluded"},
			path:        "excluded",
			want:        true,
		},
		{
			name:        "exclude subdirectory",
			excludeDirs: []string{"excluded"},
			path:        "excluded/sub/file.txt",
			want:        true,
		},
		{
			name:        "don't exclude different directory",
			excludeDirs: []string{"excluded"},
			path:        "normal/file.txt",
			want:        false,
		},
		{
			name:        "multiple exclusions - first matches",
			excludeDirs: []string{"dir1", "dir2", "dir3"},
			path:        "dir1/file.txt",
			want:        true,
		},
		{
			name:        "multiple exclusions - last matches",
			excludeDirs: []string{"dir1", "dir2", "dir3"},
			path:        "dir3/file.txt",
			want:        true,
		},
		{
			name:        "multiple exclusions - none match",
			excludeDirs: []string{"dir1", "dir2", "dir3"},
			path:        "other/file.txt",
			want:        false,
		},
		{
			name:        "empty exclusions",
			excludeDirs: []string{},
			path:        "any/path",
			want:        false,
		},
		{
			name:        "path with dots",
			excludeDirs: []string{".wippy"},
			path:        ".wippy/vendor/module",
			want:        true,
		},
		{
			name:        "clean paths comparison",
			excludeDirs: []string{"excluded"},
			path:        "./excluded/file.txt",
			want:        true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFilteredFS(nil, tt.excludeDirs)

			got := f.shouldExclude(tt.path)
			if got != tt.want {
				t.Errorf("shouldExclude() = %v, want %v for path %s with excludeDirs %v",
					got, tt.want, tt.path, tt.excludeDirs)
			}
		})
	}
}

func TestFilteredFS_Integration(t *testing.T) {
	// Create a temporary directory structure for integration testing
	tmpDir := t.TempDir()

	// Create test structure
	dirs := []string{
		".wippy/vendor",
		"replacements/module1",
		"replacements/module2",
		"src",
		"public",
	}

	files := map[string]string{
		"app.yaml":                      "content",
		"src/main.lua":                  "content",
		"public/index.html":             "content",
		".wippy/vendor/test/module.lua": "should be excluded",
		"replacements/module1/code.lua": "should be excluded",
		"replacements/module2/code.lua": "should be excluded",
	}

	for _, dir := range dirs {
		if err := os.MkdirAll(filepath.Join(tmpDir, dir), 0755); err != nil {
			t.Fatal(err)
		}
	}

	for file, content := range files {
		fullPath := filepath.Join(tmpDir, file)
		if err := os.MkdirAll(filepath.Dir(fullPath), 0755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(fullPath, []byte(content), 0600); err != nil {
			t.Fatal(err)
		}
	}

	// Create filtered FS
	osFS := os.DirFS(tmpDir)
	filtered := newFilteredFS(osFS, []string{
		".wippy",
		"replacements/module1",
		"replacements/module2",
	})

	// Test ReadDir at root
	entries, err := filtered.ReadDir(".")
	if err != nil {
		t.Fatalf("ReadDir failed: %v", err)
	}

	// Check that excluded directories are not present
	for _, entry := range entries {
		if entry.Name() == ".wippy" {
			t.Errorf("ReadDir returned excluded directory: .wippy")
		}
		if entry.Name() == "replacements" {
			// Replacements dir itself should be visible, but its subdirs should be filtered
			subEntries, err := filtered.ReadDir("replacements")
			if err != nil {
				t.Fatalf("ReadDir(replacements) failed: %v", err)
			}
			for _, sub := range subEntries {
				if sub.Name() == "module1" || sub.Name() == "module2" {
					t.Errorf("ReadDir returned excluded subdirectory: replacements/%s", sub.Name())
				}
			}
		}
	}

	// Try to open excluded file
	_, err = filtered.Open(".wippy/vendor/test/module.lua")
	if err == nil {
		t.Error("Open() should fail for excluded file")
	}

	// Try to open normal file
	file, err := filtered.Open("app.yaml")
	if err != nil {
		t.Errorf("Open() failed for normal file: %v", err)
	}
	if file != nil {
		file.Close()
	}
}
