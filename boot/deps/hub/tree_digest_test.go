// SPDX-License-Identifier: MPL-2.0

package hub

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestDigestDirectoryTree_EmptyDirectoryChangesIdentity(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "module.lua"), []byte("return true"), 0644))
	before, beforeSize, err := digestDirectoryTree(root)
	require.NoError(t, err)

	require.NoError(t, os.Mkdir(filepath.Join(root, "empty"), 0755))
	after, afterSize, err := digestDirectoryTree(root)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
	assert.Equal(t, beforeSize, afterSize, "artifact size remains the sum of regular file bytes")
}

func TestDigestDirectoryTree_PermissionChangesIdentity(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not preserve Unix permission bits")
	}
	root := t.TempDir()
	path := filepath.Join(root, "module.lua")
	require.NoError(t, os.WriteFile(path, []byte("return true"), 0644))
	before, _, err := digestDirectoryTree(root)
	require.NoError(t, err)

	require.NoError(t, os.Chmod(path, 0755))
	after, _, err := digestDirectoryTree(root)
	require.NoError(t, err)
	assert.NotEqual(t, before, after)
}

func TestDigestDirectoryTree_IsDeterministicAcrossCreationOrder(t *testing.T) {
	first := t.TempDir()
	second := t.TempDir()
	writeDigestFixture(t, first, []string{"b", "a"})
	writeDigestFixture(t, second, []string{"a", "b"})

	firstDigest, firstSize, err := digestDirectoryTree(first)
	require.NoError(t, err)
	secondDigest, secondSize, err := digestDirectoryTree(second)
	require.NoError(t, err)
	assert.Equal(t, firstDigest, secondDigest)
	assert.Equal(t, firstSize, secondSize)
}

func TestDigestReplacementTree_ExcludesNodeModulesSymlinks(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows symlink creation requires additional privileges")
	}

	root := t.TempDir()
	require.NoError(t, os.WriteFile(filepath.Join(root, "wippy.yaml"), []byte(`organization: local
module: mod
version: v0.1.0
exclude:
  - ui/node_modules/**
`), 0644))
	require.NoError(t, os.WriteFile(filepath.Join(root, "module.lua"), []byte("return true"), 0644))
	require.NoError(t, os.Mkdir(filepath.Join(root, "ui"), 0755))

	before, beforeSize, err := digestReplacementTree(root)
	require.NoError(t, err)

	binDir := filepath.Join(root, "ui", "node_modules", ".bin")
	require.NoError(t, os.MkdirAll(binDir, 0755))
	require.NoError(t, os.Symlink("../css-beautify/bin/css-beautify.js", filepath.Join(binDir, "css-beautify")))

	after, afterSize, err := digestReplacementTree(root)
	require.NoError(t, err)
	assert.Equal(t, before, after)
	assert.Equal(t, beforeSize, afterSize)
}

func writeDigestFixture(t *testing.T, root string, names []string) {
	t.Helper()
	for _, name := range names {
		dir := filepath.Join(root, name)
		require.NoError(t, os.Mkdir(dir, 0755))
		require.NoError(t, os.WriteFile(filepath.Join(dir, "value"), []byte(name), 0644))
	}
}
