// SPDX-License-Identifier: MPL-2.0

package config

import (
	"io/fs"
	"path/filepath"
	"testing"
	"testing/fstest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestFilterSourceFSUsesModuleRelativePaths(t *testing.T) {
	cfg := &ModuleConfig{Exclude: []string{
		"test/**",
		"src/**/stubs/**",
		"src/**/*.spec.yaml",
	}}
	base := fstest.MapFS{
		"_index.yaml":              {Data: []byte("root")},
		"feature/_index.yaml":      {Data: []byte("feature")},
		"feature/worker.spec.yaml": {Data: []byte("spec")},
		"feature/stubs/fake.yaml":  {Data: []byte("stub")},
		"test/inside-src.yaml":     {Data: []byte("source test")},
	}

	filtered := cfg.FilterSourceFS(base, "src")
	var paths []string
	require.NoError(t, fs.WalkDir(filtered, ".", func(name string, _ fs.DirEntry, err error) error {
		require.NoError(t, err)
		paths = append(paths, name)
		return nil
	}))

	assert.Equal(t, []string{
		".",
		"_index.yaml",
		"feature",
		"feature/_index.yaml",
		"test",
		"test/inside-src.yaml",
	}, paths)
}

func TestFilterSourceFSAtModuleRoot(t *testing.T) {
	cfg := &ModuleConfig{Exclude: []string{"test/**"}}
	base := fstest.MapFS{
		"src/_index.yaml":  {Data: []byte("source")},
		"test/_index.yaml": {Data: []byte("test")},
	}

	filtered := cfg.FilterSourceFS(base, "")
	_, err := fs.Stat(filtered, "test/_index.yaml")
	assert.ErrorIs(t, err, fs.ErrNotExist)
	data, err := fs.ReadFile(filtered, "src/_index.yaml")
	require.NoError(t, err)
	assert.Equal(t, "source", string(data))
}

func TestSourcePrefix(t *testing.T) {
	root := t.TempDir()
	assert.Equal(t, "src/components", SourcePrefix(root, filepath.Join(root, "src", "components")))
	assert.Empty(t, SourcePrefix(root, root+"-sibling"))
}

func TestNewSourceFSHidesNestedRuntimeState(t *testing.T) {
	base := fstest.MapFS{
		".wippy.yaml":                           {Data: []byte("application")},
		"src/_index.yaml":                       {Data: []byte("source")},
		"src/.wippy/vendor/example/_index.yaml": {Data: []byte("dependency")},
	}

	filtered := NewSourceFS(base, nil, ".", ".")
	var paths []string
	require.NoError(t, fs.WalkDir(filtered, ".", func(name string, _ fs.DirEntry, err error) error {
		require.NoError(t, err)
		paths = append(paths, name)
		return nil
	}))

	assert.Equal(t, []string{
		".",
		".wippy.yaml",
		"src",
		"src/_index.yaml",
	}, paths)
	_, err := fs.Stat(filtered, "src/.wippy/vendor/example/_index.yaml")
	assert.ErrorIs(t, err, fs.ErrNotExist)
}
