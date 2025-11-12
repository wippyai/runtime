package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

const (
	testDirPerm  = 0755
	testFilePerm = 0600
)

func TestFilteredFS_Open(t *testing.T) {
	t.Parallel()

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
		assertErr   assert.ErrorAssertionFunc
		errType     error
	}{
		{
			name:        "open normal file",
			excludeDirs: []string{"excluded"},
			openPath:    "file.txt",
			assertErr:   assert.NoError,
		},
		{
			name:        "open excluded file",
			excludeDirs: []string{"excluded"},
			openPath:    "excluded/file.txt",
			assertErr:   assert.Error,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open excluded subdirectory",
			excludeDirs: []string{"excluded"},
			openPath:    "excluded/sub/file.txt",
			assertErr:   assert.Error,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open with multiple exclusions",
			excludeDirs: []string{"excluded", "normal"},
			openPath:    "normal/file.txt",
			assertErr:   assert.Error,
			errType:     fs.ErrNotExist,
		},
		{
			name:        "open non-excluded with multiple exclusions",
			excludeDirs: []string{"excluded", "normal"},
			openPath:    "another.txt",
			assertErr:   assert.NoError,
		},
		{
			name:        "no exclusions",
			excludeDirs: []string{},
			openPath:    "excluded/file.txt",
			assertErr:   assert.NoError,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			filtered := newFilteredFS(fsys, tt.excludeDirs)

			file, err := filtered.Open(tt.openPath)
			tt.assertErr(t, err)
			if tt.errType != nil && err != nil {
				assert.ErrorIs(t, err, tt.errType)
			}
			if err == nil {
				require.NotNil(t, file)
				file.Close()
			}
		})
	}
}

func TestFilteredFS_ReadDir(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			filtered := newFilteredFS(fsys, tt.excludeDirs)

			entries, err := filtered.ReadDir(tt.readPath)
			require.NoError(t, err)

			entryNames := make(map[string]bool)
			for _, entry := range entries {
				entryNames[entry.Name()] = true
			}

			for _, want := range tt.wantContains {
				assert.True(t, entryNames[want], "ReadDir() missing expected entry: %s", want)
			}

			for _, exclude := range tt.wantExcludes {
				assert.False(t, entryNames[exclude], "ReadDir() contains excluded entry: %s", exclude)
			}
		})
	}
}

func TestFilteredFS_shouldExclude(t *testing.T) {
	t.Parallel()

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
			t.Parallel()

			f := newFilteredFS(nil, tt.excludeDirs)

			got := f.shouldExclude(tt.path)
			assert.Equal(t, tt.want, got)
		})
	}
}

func TestFilteredFS_Integration(t *testing.T) {
	tmpDir := t.TempDir()

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
		require.NoError(t, os.MkdirAll(filepath.Join(tmpDir, dir), testDirPerm))
	}

	for file, content := range files {
		fullPath := filepath.Join(tmpDir, file)
		require.NoError(t, os.MkdirAll(filepath.Dir(fullPath), testDirPerm))
		require.NoError(t, os.WriteFile(fullPath, []byte(content), testFilePerm))
	}

	osFS := os.DirFS(tmpDir)
	filtered := newFilteredFS(osFS, []string{
		".wippy",
		"replacements/module1",
		"replacements/module2",
	})

	entries, err := filtered.ReadDir(".")
	require.NoError(t, err)

	for _, entry := range entries {
		assert.NotEqual(t, ".wippy", entry.Name(), "ReadDir returned excluded directory: .wippy")
		if entry.Name() == "replacements" {
			subEntries, err := filtered.ReadDir("replacements")
			require.NoError(t, err)
			for _, sub := range subEntries {
				assert.NotContains(t, []string{"module1", "module2"}, sub.Name(), "ReadDir returned excluded subdirectory: replacements/%s", sub.Name())
			}
		}
	}

	_, err = filtered.Open(".wippy/vendor/test/module.lua")
	assert.Error(t, err, "Open() should fail for excluded file")

	file, err := filtered.Open("app.yaml")
	require.NoError(t, err)
	require.NotNil(t, file)
	file.Close()
}
