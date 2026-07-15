// SPDX-License-Identifier: MPL-2.0

//go:build unix

package hub

import (
	"path/filepath"
	"syscall"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestDigestDirectoryTree_RejectsFIFO(t *testing.T) {
	root := t.TempDir()
	require.NoError(t, syscall.Mkfifo(filepath.Join(root, "stream"), 0600))
	_, _, err := digestDirectoryTree(root)
	require.ErrorContains(t, err, "unsupported file type")
}
