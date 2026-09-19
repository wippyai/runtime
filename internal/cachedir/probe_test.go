// SPDX-License-Identifier: MPL-2.0

package cachedir

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestProbe_WritableDir(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "test-cache")
	assert.NoError(t, Probe(dir))
}

func TestProbe_UnwritableDir(t *testing.T) {
	tempDir := t.TempDir()
	regularFile := filepath.Join(tempDir, "file-not-dir")
	require.NoError(t, os.WriteFile(regularFile, []byte("hello"), 0o644))

	unwritable := filepath.Join(regularFile, "sub-dir")
	assert.Error(t, Probe(unwritable))
}
